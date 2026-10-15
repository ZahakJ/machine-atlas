package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const rootsDir = "../../../fixtures/roots"

type want struct {
	status obs.Status
	value  any
}

func ok(v any) want { return want{obs.StatusOK, v} }

var (
	absent  = want{status: obs.StatusAbsent}
	denied  = want{status: obs.StatusPermissionDenied}
	present = want{status: obs.StatusOK}
)

// fixtureWants lists, per root, the storage observations that make that
// root interesting. Every other observation only has to be not failed.
var fixtureWants = map[string]map[string]want{
	"desktop-x86_64": {
		"block.queues":         present,
		"block.stats":          present,
		"block.psi.some.avg60": ok(0.04),
	},
	"rpi-arm64-noswap": {
		"block.queues": present,
		"fs.types":     present,
	},
	"alpine-container": {
		"block.queues": present,
		"block.bdi":    present,
	},
	"numa-server-la57": {
		"block.queues": present,
	},
	"old-vm-cgroupv1": {
		"block.psi.some.avg10":    absent,
		"block.psi.full.total_us": absent,
		"block.stats":             present,
	},
	"restricted": {
		"block.queues":         denied,
		"block.stats":          denied,
		"block.bdi":            denied,
		"block.psi.some.avg10": denied,
		"fs.types":             present,
	},
}

func runFixture(t *testing.T, name string) map[string]obs.Observation {
	t.Helper()
	env, err := collect.OpenFixture(filepath.Join(rootsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	return run(t, env)
}

func TestFixtureRoots(t *testing.T) {
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n++
		t.Run(e.Name(), func(t *testing.T) {
			byID := runFixture(t, e.Name())
			for id, o := range byID {
				if o.Status == obs.StatusFailed {
					t.Errorf("%s failed: %s", id, o.Note)
				}
				if o.Status != obs.StatusOK && o.Note == "" {
					t.Errorf("%s is %s without a note", id, o.Status)
				}
			}
			for id, w := range fixtureWants[e.Name()] {
				o := byID[id]
				switch {
				case o.Status != w.status:
					t.Errorf("%s: status %s (%s), want %s", id, o.Status, o.Note, w.status)
				case w.value != nil && !reflect.DeepEqual(o.Value, w.value):
					t.Errorf("%s = %#v, want %#v", id, o.Value, w.value)
				}
			}
		})
	}
	if n != len(fixtureWants) {
		t.Errorf("found %d roots, want %d", n, len(fixtureWants))
	}
}

func rows(t *testing.T, byID map[string]obs.Observation, id string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, r := range byID[id].Value.([]map[string]any) {
		out[r["name"].(string)] = r
	}
	return out
}

// TestFixtureShapes checks the storage shapes the lessons rely on: the
// devices of each machine and the kernel-version differences between them.
func TestFixtureShapes(t *testing.T) {
	desk := runFixture(t, "desktop-x86_64")
	q := rows(t, desk, "block.queues")
	if q["sda"]["scheduler"] != "mq-deadline" || q["sdb"]["scheduler"] != "bfq" || q["sdb"]["wbt_latency_us"] != int64(0) ||
		q["sdb"]["physical_block_bytes"] != int64(4096) || q["sda"]["write_cache"] != "write back" || q["sda"]["fua"] != false {
		t.Errorf("desktop queues %v", q)
	}
	if s := rows(t, desk, "block.stats")["sda"]; s["flushes"] == nil || s["weighted_ms"].(int64) < s["io_ms"].(int64) {
		t.Errorf("desktop sda stats %v", s)
	}

	pi := rows(t, runFixture(t, "rpi-arm64-noswap"), "block.queues")["mmcblk0"]
	if pi["write_cache"] != "write through" || pi["scheduler"] != "mq-deadline" || pi["hw_queues"] != int64(1) {
		t.Errorf("pi queue %v", pi)
	}

	numa := rows(t, runFixture(t, "numa-server-la57"), "block.queues")
	if numa["nvme0n1"]["hw_queues"] != int64(64) || numa["nvme0n1"]["scheduler"] != "none" || numa["nvme1n1"]["write_cache"] != "write through" {
		t.Errorf("numa queues %v", numa)
	}

	ctr := runFixture(t, "alpine-container")
	if c := rows(t, ctr, "block.queues")["nvme0n1"]; c["hw_queues"] != int64(2) || c["write_cache"] != "write through" {
		t.Errorf("container's view of the host disk %v", c)
	}

	old := runFixture(t, "old-vm-cgroupv1")
	oq := rows(t, old, "block.queues")
	if oq["dm-0"]["hw_queues"] != nil || oq["dm-0"]["schedulers"] != "none" || oq["vda"]["hw_queues"] != int64(1) || oq["vda"]["wbt_latency_us"] != int64(75000) {
		t.Errorf("4.19 queues %v", oq)
	}
	if s := rows(t, old, "block.stats")["vda"]; s["discards"] == nil || s["flushes"] != nil {
		t.Errorf("4.19 stat has discards and no flushes: %v", s)
	}
	if b := rows(t, old, "block.bdi")["vda"]; b["max_bytes"] != nil || b["strict_limit"] != nil || b["max_ratio"] != int64(100) {
		t.Errorf("4.19 bdi %v", b)
	}
}
