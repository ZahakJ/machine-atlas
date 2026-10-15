package derive

import (
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// disks is a desktop whose root is a partition of an NVMe disk, with a
// second, spinning disk, and the dirty counters of /proc/vmstat.
func disks() map[string]any {
	return map[string]any{
		"kernel.page_size_bytes": 4096,
		"kernel.uptime_seconds":  1000.0,
		"mounts.filesystems": []map[string]any{
			{"mount_point": "/", "fstype": "ext4", "source_kind": "block-device", "device": "nvme0n1p2", "disk": "nvme0n1", "options": "rw,relatime"},
			{"mount_point": "/proc", "fstype": "proc", "source_kind": "pseudo", "options": "rw,nosuid"},
			{"mount_point": "<path>", "fstype": "xfs", "source_kind": "block-device", "device": "sdb1", "disk": "sdb"},
		},
		"block.queues": []map[string]any{
			{"name": "sdb", "scheduler": "bfq", "schedulers": "mq-deadline kyber bfq none", "rotational": true, "hw_queues": 1, "nr_requests": 64},
			{"name": "nvme0n1", "scheduler": "none", "schedulers": "none mq-deadline kyber bfq", "rotational": false, "hw_queues": 16,
				"nr_requests": 1023, "read_ahead_bytes": 131072, "max_request_bytes": 131072, "write_cache": "write back"},
		},
		"block.stats": []map[string]any{
			{"name": "nvme0n1", "reads": 4000, "read_ms": 1000, "writes": 2000, "write_ms": 6000, "read_bytes": 1 << 30,
				"write_bytes": 2 << 30, "io_ms": 250000, "weighted_ms": 500000},
		},
		"mem.vmstat.nr_dirty":                      300,
		"mem.vmstat.nr_writeback":                  100,
		"mem.vmstat.nr_dirty_threshold":            1000,
		"mem.vmstat.nr_dirty_background_threshold": 500,
	}
}

func TestStorageFacts(t *testing.T) {
	facts := byID(All(snap(t, disks())))
	want := map[string]any{
		"derived.storage.root_fstype":                     "ext4",
		"derived.storage.root_options":                    "rw,relatime",
		"derived.storage.root_disk":                       "nvme0n1",
		"derived.storage.root_disk_kind":                  "nvme",
		"derived.storage.root_scheduler":                  "none",
		"derived.storage.root_schedulers":                 "none mq-deadline kyber bfq",
		"derived.storage.root_write_cache":                "write back",
		"derived.storage.root_rotational":                 false,
		"derived.storage.root_hw_queues":                  int64(16),
		"derived.storage.root_nr_requests":                int64(1023),
		"derived.storage.root_read_ahead_bytes":           int64(131072),
		"derived.storage.root_max_request_bytes":          int64(131072),
		"derived.storage.root_read_bytes":                 int64(1 << 30),
		"derived.storage.root_written_bytes":              int64(2 << 30),
		"derived.storage.root_read_latency_seconds":       0.00025,
		"derived.storage.root_write_latency_seconds":      0.003,
		"derived.storage.root_util_fraction":              0.25,
		"derived.storage.root_busy_queue_depth":           2.0,
		"derived.dirty.kernel_threshold_bytes":            int64(4096000),
		"derived.dirty.kernel_background_threshold_bytes": int64(2048000),
		"derived.dirty.threshold_used_fraction":           0.4,
	}
	for id, w := range want {
		f := facts[id]
		if f.Status != StatusOK {
			t.Errorf("%s: %s (%s)", id, f.Status, f.Reason)
			continue
		}
		if a, ok := w.(float64); ok {
			if b, _ := f.Value.(float64); math.Abs(a-b) > 1e-12 {
				t.Errorf("%s = %v, want %v", id, f.Value, w)
			}
		} else if f.Value != w {
			t.Errorf("%s = %#v, want %#v", id, f.Value, w)
		}
	}
	if r := facts["derived.storage.root_disk"].Rule; !strings.Contains(r, "nvme0n1, which holds / through nvme0n1p2") {
		t.Errorf("rule %q", r)
	}
}

func TestAtimePolicy(t *testing.T) {
	for opts, want := range map[string]string{"rw,relatime": "relatime", "ro,noatime,nodiratime": "noatime", "rw": "strictatime", "rw,lazytime": "strictatime"} {
		if got := atimePolicy(opts); got != want {
			t.Errorf("atimePolicy(%q) = %s, want %s", opts, got, want)
		}
	}
}

func TestDiskKind(t *testing.T) {
	for name, want := range map[string]string{"nvme1n1": "nvme", "sda": "scsi", "vda": "virtio", "xvda": "xen", "mmcblk0": "mmc",
		"dm-0": "dm", "md127": "md", "zram0": "zram", "loop3": "loop", "nbd0": "nbd", "pmem0": "other"} {
		if got := diskKind(name); got != want {
			t.Errorf("diskKind(%s) = %s, want %s", name, got, want)
		}
	}
}

// A container's root is overlay; a root whose disk could not be traced;
// a denied queue table; a disk that never read.
func TestStorageUnknown(t *testing.T) {
	overlay := disks()
	overlay["mounts.filesystems"] = []map[string]any{{"mount_point": "/", "fstype": "overlay", "source_kind": "overlay", "options": "rw,relatime"}}
	f := byID(All(snap(t, overlay)))
	if f["derived.storage.root_fstype"].Value != "overlay" {
		t.Errorf("overlay root fstype %v", f["derived.storage.root_fstype"])
	}
	for _, id := range []string{"derived.storage.root_disk", "derived.storage.root_scheduler", "derived.storage.root_util_fraction"} {
		if f[id].Status != StatusUnknown || !strings.Contains(f[id].Reason, "overlay") {
			t.Errorf("%s on an overlay root: %+v", id, f[id])
		}
	}

	untraced := disks()
	untraced["mounts.filesystems"] = []map[string]any{{"mount_point": "/", "fstype": "xfs", "source_kind": "block-device", "device": "centos-root"}}
	if g := byID(All(snap(t, untraced)))["derived.storage.root_disk"]; g.Status != StatusUnknown || !strings.Contains(g.Reason, "traced") {
		t.Errorf("untraced root: %+v", g)
	}

	denied := maps.Clone(disks())
	denied["block.queues"] = missing("permission_denied")
	g := byID(All(snap(t, denied)))
	if s := g["derived.storage.root_scheduler"]; s.Status != StatusUnknown || !strings.Contains(s.Reason, "block.queues is permission denied") {
		t.Errorf("denied queues: %+v", s)
	}
	if g["derived.storage.root_read_bytes"].Status != StatusOK {
		t.Errorf("stats do not depend on the queue table: %+v", g["derived.storage.root_read_bytes"])
	}

	idle := disks()
	idle["block.stats"] = []map[string]any{{"name": "nvme0n1", "reads": 0, "read_ms": 0, "writes": 0, "write_ms": 0, "io_ms": 0, "weighted_ms": 0}}
	h := byID(All(snap(t, idle)))
	for _, id := range []string{"derived.storage.root_read_latency_seconds", "derived.storage.root_busy_queue_depth"} {
		if h[id].Status != StatusUnknown {
			t.Errorf("%s with no I/O: %+v", id, h[id])
		}
	}
	if h["derived.storage.root_util_fraction"].Value != 0.0 {
		t.Errorf("util with no I/O: %+v", h["derived.storage.root_util_fraction"])
	}

	noThreshold := disks()
	noThreshold["mem.vmstat.nr_dirty_threshold"] = missing("absent")
	if g := byID(All(snap(t, noThreshold)))["derived.dirty.threshold_used_fraction"]; g.Status != StatusUnknown {
		t.Errorf("fraction without a threshold: %+v", g)
	}
}

// Strict redaction numbers device names in one namespace, so the root disk
// is still found, and its queue and counters still joined, on a strict
// snapshot.
func TestStorageFactsSurviveStrictRedaction(t *testing.T) {
	red, _, err := snapshot.Redact(snap(t, disks()), snapshot.PolicyStrict)
	if err != nil {
		t.Fatal(err)
	}
	f := byID(All(red))
	disk, _ := f["derived.storage.root_disk"].Value.(string)
	if !strings.HasPrefix(disk, "<dev-") {
		t.Fatalf("root disk on a strict snapshot: %+v", f["derived.storage.root_disk"])
	}
	if f["derived.storage.root_scheduler"].Value != "none" || f["derived.storage.root_util_fraction"].Value != 0.25 {
		t.Errorf("joins broke under strict: %+v %+v", f["derived.storage.root_scheduler"], f["derived.storage.root_util_fraction"])
	}
}
