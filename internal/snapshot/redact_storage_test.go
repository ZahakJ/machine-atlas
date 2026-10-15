package snapshot

import (
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Invented block device names: whole disks, partitions, a mapper volume.
var devIdentifying = []string{"nvme0n1", "nvme0n1p2", "sdb", "sdb1", "dm-3", "cryptroot"}

func storageSnapshot() *Snapshot {
	k, s, l := obs.KindStatic, obs.ScopeKernel, obs.KindLive
	observations := []obs.Observation{
		ob("block.devices", []map[string]any{
			{"name": "nvme0n1", "type": "disk", "size_bytes": int64(1 << 40)},
			{"name": "sdb", "type": "disk", "rotational": true},
			{"name": "dm-3", "type": "dm"},
		}, k, s, ""),
		ob("block.lsblk", []map[string]any{
			{"name": "nvme0n1", "type": "disk"}, {"name": "nvme0n1p2", "type": "part"}, {"name": "cryptroot", "type": "crypt"},
		}, k, s, ""),
		ob("block.queues", []map[string]any{
			{"name": "sdb", "scheduler": "bfq", "hw_queues": int64(1)},
			{"name": "nvme0n1", "scheduler": "none", "hw_queues": int64(16)},
		}, k, s, ""),
		ob("block.stats", []map[string]any{{"name": "dm-3", "reads": int64(4)}, {"name": "nvme0n1", "reads": int64(9)}}, l, s, ""),
		ob("block.bdi", []map[string]any{{"name": "nvme0n1", "max_ratio": int64(100)}}, k, s, ""),
		ob("mem.swap.areas", []map[string]any{{"name": "sdb1", "type": "partition"}, {"name": "<file>", "type": "file"}}, l, s, ""),
		ob("mounts.filesystems", []map[string]any{
			{"mount_point": "/", "fstype": "ext4", "source_kind": "block-device", "device": "nvme0n1p2", "disk": "nvme0n1", "options": "rw,relatime"},
			{"mount_point": "<path>", "fstype": "xfs", "source_kind": "block-device", "device": "cryptroot", "disk": "dm-3"},
			{"mount_point": "/proc", "fstype": "proc", "source_kind": "pseudo"},
		}, k, obs.ScopeNamespace, ""),
	}
	rep := collect.Report{Mode: "live", StartedAt: testTime, FinishedAt: testTime,
		Collectors: []collect.CollectorRun{{ID: "test", Status: collect.RunOK, Observations: len(observations)}}}
	return Build(rep, observations)
}

// Device names survive the default policy; under strict every device
// name in every table becomes a <dev-N> placeholder, one per device across
// the snapshot, so the queue, statistics and mount tables still join.
func TestRedactStrictNumbersDevicesAcrossTables(t *testing.T) {
	s := storageSnapshot()
	def, _, err := Redact(s, PolicyDefault)
	if err != nil {
		t.Fatal(err)
	}
	if rowsOf(t, def, "block.queues")[1]["name"] != "nvme0n1" || rowsOf(t, def, "mounts.filesystems")[0]["disk"] != "nvme0n1" {
		t.Error("default redaction must keep device names")
	}
	red, _, err := Redact(s, PolicyStrict)
	if err != nil {
		t.Fatal(err)
	}
	after := encoded(t, red)
	for _, tok := range devIdentifying {
		if strings.Contains(after, `"`+tok+`"`) {
			t.Errorf("%q survived strict redaction", tok)
		}
	}
	devs := rowsOf(t, red, "block.devices")
	nvme, sdb, dm := devs[0]["name"], devs[1]["name"], devs[2]["name"]
	if nvme == sdb || nvme == dm || !strings.HasPrefix(nvme.(string), "<dev-") {
		t.Fatalf("block.devices placeholders %v %v %v", nvme, sdb, dm)
	}
	checks := []struct {
		id, col string
		row     int
		want    any
	}{
		{"block.lsblk", "name", 0, nvme},
		{"block.queues", "name", 0, sdb},
		{"block.queues", "name", 1, nvme},
		{"block.stats", "name", 0, dm},
		{"block.stats", "name", 1, nvme},
		{"block.bdi", "name", 0, nvme},
		{"mounts.filesystems", "disk", 0, nvme},
		{"mounts.filesystems", "disk", 1, dm},
	}
	for _, c := range checks {
		if got := rowsOf(t, red, c.id)[c.row][c.col]; got != c.want {
			t.Errorf("%s row %d %s = %v, want %v (the name block.devices got)", c.id, c.row, c.col, got, c.want)
		}
	}
	m := rowsOf(t, red, "mounts.filesystems")
	if p, _ := m[0]["device"].(string); !strings.HasPrefix(p, "<dev-") || p == nvme || m[0]["options"] != "rw,relatime" {
		t.Errorf("partition placeholder %v, options %v", m[0]["device"], m[0]["options"])
	}
	if sw := rowsOf(t, red, "mem.swap.areas"); !strings.HasPrefix(sw[0]["name"].(string), "<dev-") || sw[1]["name"] != "<file>" {
		t.Errorf("swap areas %v", sw)
	}
	if q := rowsOf(t, red, "block.queues")[1]; q["scheduler"] != "none" || q["hw_queues"] != int64(16) {
		t.Errorf("queue settings must survive strict: %v", q)
	}
	if errs := Validate(red); len(errs) > 0 {
		t.Errorf("strict snapshot is invalid: %v", errs)
	}
	again, more, _ := Redact(red, PolicyStrict)
	if len(more) != 0 || encoded(t, again) != after {
		t.Errorf("second strict redaction changed %d things: %v", len(more), more)
	}
}
