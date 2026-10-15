package storage

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// machine builds a fixture root from machine paths and their contents and
// paths that must fail with EACCES.
func machine(t *testing.T, files map[string]string, deny ...string) *collect.FixtureEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test-root")
	files = maps.Clone(files)
	meta, _ := json.Marshal(map[string]any{
		"description": "test", "now": "2026-01-15T12:00:00Z", "page_size": 4096,
		"uname": map[string]string{"machine": "x86_64", "release": "6.12.0"}, "deny": deny, "commands": map[string]any{},
	})
	files["/_meta.json"] = string(meta)
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, err := collect.OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.Close() })
	return env
}

func run(t *testing.T, env collect.Env) map[string]obs.Observation {
	t.Helper()
	rep, out := collect.Run(context.Background(), env, Collectors())
	for _, r := range rep.Collectors {
		if r.Error != "" {
			t.Errorf("collector %s: %s", r.ID, r.Error)
		}
	}
	byID := map[string]obs.Observation{}
	for _, o := range out {
		byID[o.ID] = o
	}
	return byID
}

// modernFiles is an NVMe desktop on a current kernel: every id ok.
func modernFiles() map[string]string {
	f := map[string]string{
		"/proc/pressure/io":                "some avg10=0.10 avg60=0.20 avg300=0.30 total=4000\nfull avg10=0.00 avg60=0.10 avg300=0.20 total=3000\n",
		"/proc/filesystems":                "nodev\tsysfs\nnodev\ttmpfs\n\text4\nnodev\toverlay\n",
		"/sys/block/loop0/size":            "0\n",
		"/sys/block/loop1/size":            "1024\n",
		"/sys/block/loop1/queue/scheduler": "none\n",
		"/sys/block/loop1/stat":            "1 0 8 0 0 0 0 0 0 1 1 0 0 0 0 0 0\n",
	}
	for _, d := range []string{"nvme0n1", "sda"} {
		q := "/sys/block/" + d + "/queue/"
		f["/sys/block/"+d+"/size"] = "1000\n"
		f[q+"rotational"] = map[string]string{"nvme0n1": "0\n", "sda": "1\n"}[d]
		f[q+"scheduler"] = map[string]string{"nvme0n1": "[none] mq-deadline kyber bfq\n", "sda": "mq-deadline kyber [bfq] none\n"}[d]
		f[q+"logical_block_size"] = "512\n"
		f[q+"physical_block_size"] = "4096\n"
		f[q+"nr_requests"] = "1023\n"
		f[q+"max_sectors_kb"] = "1280\n"
		f[q+"max_hw_sectors_kb"] = "2048\n"
		f[q+"write_cache"] = "write back\n"
		f[q+"fua"] = "1\n"
		f[q+"discard_max_bytes"] = "2199023255040\n"
		f[q+"read_ahead_kb"] = "128\n"
		f[q+"wbt_lat_usec"] = "2000\n"
		f["/sys/block/"+d+"/mq/0/cpu_list"] = "0-7\n"
		f["/sys/block/"+d+"/stat"] = "100 2 800 50 40 1 640 90 0 120 140 3 0 24 1 7 2\n"
		f["/sys/block/"+d+"/bdi/min_ratio"] = "0\n"
		f["/sys/block/"+d+"/bdi/max_ratio"] = "100\n"
		f["/sys/block/"+d+"/bdi/strict_limit"] = "0\n"
		f["/sys/block/"+d+"/bdi/stable_pages_required"] = "0\n"
	}
	f["/sys/block/nvme0n1/mq/1/cpu_list"] = "8-15\n"
	return f
}

func TestModernMachine(t *testing.T) {
	byID := run(t, machine(t, modernFiles()))
	for id, o := range byID {
		if o.Status != obs.StatusOK {
			t.Errorf("%s: %s (%s)", id, o.Status, o.Note)
		}
	}
	q := byID["block.queues"].Value.([]map[string]any)
	if len(q) != 3 || q[0]["name"] != "loop1" || q[1]["name"] != "nvme0n1" {
		t.Fatalf("queues %v: want loop1, nvme0n1 and sda, unused loop0 skipped", q)
	}
	nv, sda := q[1], q[2]
	if nv["scheduler"] != "none" || nv["hw_queues"] != int64(2) || nv["max_request_bytes"] != int64(1280<<10) ||
		nv["read_ahead_bytes"] != int64(128<<10) || nv["fua"] != true || nv["write_cache"] != "write back" || nv["rotational"] != false {
		t.Errorf("nvme queue %v", nv)
	}
	if sda["scheduler"] != "bfq" || sda["schedulers"] != "mq-deadline kyber bfq none" || sda["hw_queues"] != int64(1) {
		t.Errorf("sda queue %v", sda)
	}
	if q[0]["scheduler"] != "none" || q[0]["hw_queues"] != nil {
		t.Errorf("a loop device without mq/ has no hw_queues: %v", q[0])
	}
	s := byID["block.stats"].Value.([]map[string]any)
	if len(s) != 3 || s[1]["read_bytes"] != int64(800*512) || s[1]["flush_ms"] != int64(2) {
		t.Errorf("stats %v", s)
	}
	b := byID["block.bdi"].Value.([]map[string]any)
	if len(b) != 2 || b[0]["name"] != "nvme0n1" || b[0]["max_ratio"] != int64(100) || b[0]["max_bytes"] != nil {
		t.Errorf("bdi %v: loop1 has no bdi directory here, and max_bytes is absent", b)
	}
	if byID["block.psi.full.avg300"].Value != 0.2 || byID["block.psi.some.total_us"].Value != int64(4000) {
		t.Errorf("psi %v %v", byID["block.psi.full.avg300"].Value, byID["block.psi.some.total_us"].Value)
	}
	if fs := byID["fs.types"].Value.([]map[string]any); len(fs) != 4 || fs[2]["nodev"] != false {
		t.Errorf("fs.types %v", fs)
	}
}

// An old kernel: no pressure file, 15-field stat, no fua or wbt, a
// device-mapper volume with a bare "none" and no mq directory.
func TestOldKernel(t *testing.T) {
	f := map[string]string{
		"/proc/filesystems":                "nodev\tsysfs\n\txfs\n",
		"/sys/block/dm-0/size":             "100\n",
		"/sys/block/dm-0/queue/scheduler":  "none\n",
		"/sys/block/dm-0/queue/rotational": "1\n",
		"/sys/block/dm-0/stat":             "1 2 3 4 5 6 7 8 0 10 11 0 0 0 0\n",
		"/sys/block/vda/size":              "100\n",
		"/sys/block/vda/queue/scheduler":   "[mq-deadline] kyber bfq none\n",
		"/sys/block/vda/mq/0/cpu_list":     "0-3\n",
		"/sys/block/vda/stat":              "1 2 3 4 5 6 7 8 0 10 11 0 0 0 0\n",
	}
	byID := run(t, machine(t, f))
	if o := byID["block.psi.some.avg10"]; o.Status != obs.StatusAbsent || !strings.Contains(o.Note, "4.20") {
		t.Errorf("psi without the file: %+v", o)
	}
	q := byID["block.queues"].Value.([]map[string]any)
	if q[0]["scheduler"] != "none" || q[0]["hw_queues"] != nil || q[0]["fua"] != nil || q[1]["hw_queues"] != int64(1) {
		t.Errorf("old queues %v", q)
	}
	if s := byID["block.stats"].Value.([]map[string]any); s[0]["discards"] != int64(0) || s[0]["flushes"] != nil {
		t.Errorf("4.19 stat %v", s)
	}
	if b := byID["block.bdi"]; b.Status != obs.StatusOK || len(b.Value.([]map[string]any)) != 0 {
		t.Errorf("no bdi directories: %+v", b)
	}
}

func TestDeniedAndMalformed(t *testing.T) {
	denied := run(t, machine(t, modernFiles(), "/sys/block", "/proc/pressure"))
	for _, id := range []string{"block.queues", "block.stats", "block.bdi", "block.psi.full.total_us"} {
		if o := denied[id]; o.Status != obs.StatusPermissionDenied || o.Note == "" {
			t.Errorf("%s with /sys/block denied: %+v", id, o)
		}
	}
	oneFile := run(t, machine(t, modernFiles(), "/sys/block/sda/stat", "/sys/block/sda/queue/nr_requests"))
	for _, id := range []string{"block.stats", "block.queues"} {
		o := oneFile[id]
		if o.Status != obs.StatusPermissionDenied || strings.Contains(o.Note, "sda") || o.Note == "" {
			t.Errorf("%s with one device's file denied: %+v (the note must not name the device)", id, o)
		}
	}
	for path, id := range map[string]string{
		"/sys/block/sda/queue/scheduler":   "block.queues",
		"/sys/block/sda/queue/write_cache": "block.queues",
		"/sys/block/sda/queue/nr_requests": "block.queues",
		"/sys/block/sda/stat":              "block.stats",
		"/sys/block/sda/bdi/max_ratio":     "block.bdi",
		"/proc/pressure/io":                "block.psi.some.avg60",
		"/proc/filesystems":                "fs.types",
	} {
		f := modernFiles()
		f[path] = "garbage [x] [y]\n"
		o := run(t, machine(t, f))[id]
		if o.Status != obs.StatusFailed || o.Note == "" || strings.Contains(o.Note, "sda") {
			t.Errorf("garbage in %s: %s = %+v", path, id, o)
		}
	}
}
