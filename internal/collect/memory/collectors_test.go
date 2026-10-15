package memory

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// machine builds a fixture root from machine paths and their contents, plus
// the paths that must fail with EACCES, and opens it.
func machine(t *testing.T, files map[string]string, deny ...string) *collect.FixtureEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test-root")
	meta, _ := json.Marshal(map[string]any{
		"description": "test", "now": "2026-01-15T12:00:00Z", "page_size": 4096,
		"uname": map[string]string{"machine": "x86_64", "release": "6.8.0"}, "deny": deny,
	})
	files = maps.Clone(files)
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

// run collects with every memory collector and fails the test on any
// contract problem the runner found: undeclared or unowned ids, wrong value
// types, owned ids not emitted.
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

func modernMachine(t *testing.T) map[string]string {
	vmstat := "nr_free_pages 300000\nworkingset_refault 30\n"
	for i, id := range declared("mem.vmstat.") {
		vmstat += strings.TrimPrefix(id, "mem.vmstat.") + " " + strconv.Itoa(1000+i) + "\n"
	}
	return map[string]string{
		"/proc/meminfo":                                                                 string(readTestdata(t, "meminfo-6.8.txt")),
		"/proc/vmstat":                                                                  vmstat,
		"/proc/zoneinfo":                                                                string(readTestdata(t, "zoneinfo-numa.txt")),
		"/proc/buddyinfo":                                                               "Node 0, zone      DMA      0      0      0      0      0      0      0      0      1      1      3 \nNode 0, zone   Normal  12000   6000   3000   1500    700    300    100     40     10      4      2 \n",
		"/proc/swaps":                                                                   "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/home/someone/swapfile                  file\t\t8388604\t\t0\t\t-2\n/dev/zram0                              partition\t4194300\t\t0\t\t100\n",
		"/proc/sys/vm/swappiness":                                                       "60\n",
		"/proc/sys/vm/overcommit_memory":                                                "0\n",
		"/proc/sys/vm/overcommit_ratio":                                                 "50\n",
		"/proc/sys/vm/min_free_kbytes":                                                  "67584\n",
		"/proc/sys/vm/watermark_scale_factor":                                           "10\n",
		"/proc/sys/vm/dirty_ratio":                                                      "20\n",
		"/proc/sys/vm/dirty_background_ratio":                                           "10\n",
		"/proc/sys/vm/dirty_bytes":                                                      "0\n",
		"/proc/sys/vm/dirty_background_bytes":                                           "0\n",
		"/proc/sys/vm/dirty_expire_centisecs":                                           "3000\n",
		"/proc/sys/vm/dirty_writeback_centisecs":                                        "500\n",
		"/proc/sys/vm/max_map_count":                                                    "1048576\n",
		"/proc/sys/vm/page-cluster":                                                     "3\n",
		"/proc/sys/vm/vfs_cache_pressure":                                               "100\n",
		"/proc/sys/vm/nr_hugepages":                                                     "0\n",
		"/proc/sys/kernel/randomize_va_space":                                           "2\n",
		"/sys/kernel/mm/transparent_hugepage/enabled":                                   "[always] madvise never\n",
		"/sys/kernel/mm/transparent_hugepage/defrag":                                    "always defer defer+madvise [madvise] never\n",
		"/sys/kernel/mm/transparent_hugepage/shmem_enabled":                             "always within_size advise [never] deny force\n",
		"/sys/kernel/mm/transparent_hugepage/hpage_pmd_size":                            "2097152\n",
		"/sys/kernel/mm/lru_gen/enabled":                                                "0x0007\n",
		"/sys/module/zswap/parameters/enabled":                                          "N\n",
		"/sys/block/zram0/disksize":                                                     "4294967296\n",
		"/sys/block/nvme0n1/size":                                                       "1000\n",
		"/proc/pressure/memory":                                                         "some avg10=0.12 avg60=0.05 avg300=0.01 total=4567890\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1234567\n",
		"/sys/devices/system/node/node0/meminfo":                                        "Node 0 MemTotal:       8159206 kB\nNode 0 MemFree:         601672 kB\n",
		"/sys/devices/system/node/node0/cpulist":                                        "0-7\n",
		"/sys/devices/system/node/node1/meminfo":                                        "Node 1 MemTotal:       8159206 kB\nNode 1 MemFree:         601672 kB\n",
		"/sys/devices/system/node/node1/cpulist":                                        "8-15\n",
		"/sys/devices/system/node/possible":                                             "0-1\n",
		"/sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages":                        "16\n",
		"/sys/kernel/mm/hugepages/hugepages-2048kB/free_hugepages":                      "12\n",
		"/sys/kernel/mm/hugepages/hugepages-1048576kB/nr_hugepages":                     "0\n",
		"/sys/kernel/mm/hugepages/hugepages-1048576kB/free_hugepages":                   "0\n",
		"/sys/devices/system/memory/block_size_bytes":                                   "8000000\n",
		"/proc/slabinfo":                                                                "slabinfo - version: 2.1\n# name <active_objs>\nkmalloc-64 100 100 64 64 1\n",
		"/proc/self/cgroup":                                                             "0::/user.slice/user-4242.slice/session-3.scope\n",
		"/proc/self/mountinfo":                                                          "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw\n35 22 0:30 / /sys/fs/cgroup rw,nosuid shared:9 - cgroup2 cgroup2 rw,nsdelegate\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.max":          "4294967296\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.high":         "max\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.current":      "123456789\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.swap.max":     "max\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.swap.current": "0\n",
		"/sys/fs/cgroup/user.slice/user-4242.slice/session-3.scope/memory.events":       "low 0\nhigh 0\nmax 5\noom 1\noom_kill 1\noom_group_kill 0\n",
	}
}

func TestModernMachine(t *testing.T) {
	got := run(t, machine(t, modernMachine(t)))
	for id, o := range got {
		if o.Status != obs.StatusOK {
			t.Errorf("%s: %s (%s), want ok", id, o.Status, o.Note)
		}
	}
	want := map[string]any{
		"mem.meminfo.MemTotal":          int64(16318412 * 1024),
		"mem.meminfo.Active_anon":       int64(2345678 * 1024),
		"mem.meminfo.HugePages_Total":   int64(0),
		"mem.meminfo.DirectMap1G":       int64(4194304 * 1024),
		"mem.sysctl.vm.min_free_kbytes": int64(67584 * 1024),
		"mem.sysctl.vm.page_cluster":    int64(3),
		"mem.thp.enabled":               "always",
		"mem.thp.shmem_enabled":         "never",
		"mem.lru_gen.enabled":           "0x0007",
		"mem.zswap.enabled":             false,
		"mem.psi.some.avg10":            0.12,
		"mem.psi.full.total_us":         int64(1234567),
		"mem.memory_block_size_bytes":   int64(128 << 20),
		"mem.slabinfo.cache_count":      int64(1),
		"mem.cgroup.version":            "v2",
		"mem.cgroup.path":               "/user.slice/user-4242.slice/session-3.scope",
		"mem.cgroup.memory.max":         int64(4 << 30),
		"mem.cgroup.memory.high":        "max",
		"mem.cgroup.memory.swap_max":    "max",
		"mem.cgroup.events.oom_kill":    int64(1),
		"mem.cgroup.events.max":         int64(5),
		"mem.zram.devices":              []map[string]any{{"name": "zram0", "disksize_bytes": int64(4 << 30)}},
		"mem.swap.areas": []map[string]any{
			{"name": "<file>", "type": "file", "size_bytes": int64(8388604 * 1024), "used_bytes": int64(0), "priority": int64(-2)},
			{"name": "zram0", "type": "partition", "size_bytes": int64(4194300 * 1024), "used_bytes": int64(0), "priority": int64(100)},
		},
		"mem.hugepages.sizes": []map[string]any{
			{"size_bytes": int64(2 << 20), "nr": int64(16), "free": int64(12)},
			{"size_bytes": int64(1 << 30), "nr": int64(0), "free": int64(0)},
		},
	}
	for id, v := range want {
		if !reflect.DeepEqual(got[id].Value, v) {
			t.Errorf("%s = %#v, want %#v", id, got[id].Value, v)
		}
	}
	if o := got["mem.meminfo.Active_anon"]; o.Source.Field != "Active(anon)" || o.Unit != obs.UnitBytes {
		t.Errorf("Active_anon source/unit: %+v", o)
	}
	if o := got["mem.meminfo.HugePages_Total"]; o.Unit != obs.UnitCount {
		t.Errorf("HugePages_Total unit %q, want count", o.Unit)
	}
	if o := got["mem.cgroup.memory.high"]; o.Unit != "" {
		t.Errorf(`"max" must have no unit, got %q`, o.Unit)
	}
	if zones := got["mem.zones"].Value.([]map[string]any); len(zones) != 6 {
		t.Errorf("%d zones, want 6", len(zones))
	}
	if nodes := got["mem.numa.nodes"].Value.([]map[string]any); len(nodes) != 2 || nodes[1]["cpulist"] != "8-15" {
		t.Errorf("numa nodes: %v", nodes)
	}
	// The cgroup path appears only in mem.cgroup.path, which redaction
	// knows to scrub, never in the sources of the other cgroup ids.
	for id, o := range got {
		if id != "mem.cgroup.path" && strings.Contains(o.Source.Path+o.Note, "4242") {
			t.Errorf("%s repeats the cgroup path: %+v", id, o.Source)
		}
	}
}

// An old KVM guest: Linux 4.19 with cgroup v1, no PSI, no MGLRU, no
// MemAvailable-era fields, no swap and no NUMA support.
func TestOldKernelMachine(t *testing.T) {
	files := modernMachine(t)
	files["/proc/meminfo"] = string(readTestdata(t, "meminfo-3.10.txt"))
	files["/proc/zoneinfo"] = string(readTestdata(t, "zoneinfo-4.19.txt"))
	files["/proc/vmstat"] = "nr_free_pages 300000\npgfault 1000\npgmajfault 10\nworkingset_refault 30\n"
	files["/proc/swaps"] = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"
	files["/proc/self/cgroup"] = "11:memory:/system.slice/demo.service\n4:cpu,cpuacct:/system.slice/demo.service\n1:name=systemd:/system.slice/demo.service\n"
	for p := range files {
		if strings.HasPrefix(p, "/proc/pressure/") || strings.HasPrefix(p, "/sys/kernel/mm/lru_gen/") ||
			strings.HasPrefix(p, "/sys/devices/system/node/") || strings.HasSuffix(p, "hpage_pmd_size") ||
			strings.HasPrefix(p, "/sys/fs/cgroup/") {
			delete(files, p)
		}
	}
	got := run(t, machine(t, files))

	statuses := map[string]obs.Status{
		"mem.meminfo.MemTotal":          obs.StatusOK,
		"mem.meminfo.MemAvailable":      obs.StatusAbsent,
		"mem.meminfo.Percpu":            obs.StatusAbsent,
		"mem.vmstat.oom_kill":           obs.StatusAbsent,
		"mem.vmstat.workingset_refault": obs.StatusOK,
		"mem.zones":                     obs.StatusOK,
		"mem.swap.areas":                obs.StatusOK,
		"mem.psi.some.avg10":            obs.StatusAbsent,
		"mem.psi.full.total_us":         obs.StatusAbsent,
		"mem.lru_gen.enabled":           obs.StatusAbsent,
		"mem.thp.enabled":               obs.StatusOK,
		"mem.thp.pmd_size_bytes":        obs.StatusAbsent,
		"mem.numa.nodes":                obs.StatusAbsent,
		"mem.cgroup.version":            obs.StatusOK,
		"mem.cgroup.path":               obs.StatusOK,
		"mem.cgroup.memory.max":         obs.StatusUnsupported,
		"mem.cgroup.events.oom_kill":    obs.StatusUnsupported,
	}
	for id, want := range statuses {
		if o := got[id]; o.Status != want {
			t.Errorf("%s: %s (%s), want %s", id, o.Status, o.Note, want)
		}
	}
	for _, id := range []string{"mem.psi.some.avg10", "mem.lru_gen.enabled", "mem.numa.nodes"} {
		if o := got[id]; o.Value != nil || !strings.Contains(o.Note, "CONFIG_") {
			t.Errorf("%s: want a null value and a note naming the kernel option, got %+v", id, o)
		}
	}
	if v := got["mem.swap.areas"].Value; !reflect.DeepEqual(v, []map[string]any{}) {
		t.Errorf("empty /proc/swaps: value %#v, want an empty table", v)
	}
	if got["mem.cgroup.version"].Value != "v1" || got["mem.cgroup.path"].Value != "/system.slice/demo.service" {
		t.Errorf("cgroup: %v %v", got["mem.cgroup.version"].Value, got["mem.cgroup.path"].Value)
	}
}

// Running in the root cgroup, the v2 limit files do not exist; that is
// absence with an explanation, not failure.
func TestRootCgroup(t *testing.T) {
	files := modernMachine(t)
	files["/proc/self/cgroup"] = "0::/\n"
	files["/sys/fs/cgroup/memory.stat"] = "anon 0\n"
	got := run(t, machine(t, files))
	o := got["mem.cgroup.memory.max"]
	if o.Status != obs.StatusAbsent || !strings.Contains(o.Note, "root cgroup") {
		t.Errorf("memory.max in the root cgroup: %+v", o)
	}
}

func TestPermissionDenied(t *testing.T) {
	denied := []string{
		"/proc/meminfo", "/proc/vmstat", "/proc/zoneinfo", "/proc/buddyinfo", "/proc/swaps",
		"/proc/sys", "/sys/kernel/mm", "/sys/module/zswap", "/sys/block", "/proc/pressure",
		"/sys/devices/system", "/proc/slabinfo", "/proc/self/cgroup",
	}
	got := run(t, machine(t, modernMachine(t), denied...))
	if len(got) == 0 {
		t.Fatal("no observations")
	}
	for id, o := range got {
		if o.Status != obs.StatusPermissionDenied || o.Value != nil {
			t.Errorf("%s: %s, want permission_denied with a null value", id, o.Status)
		}
	}
}

// Denying only the cgroup directory leaves version and path readable.
func TestCgroupFilesDenied(t *testing.T) {
	got := run(t, machine(t, modernMachine(t), "/sys/fs/cgroup"))
	if got["mem.cgroup.version"].Status != obs.StatusOK {
		t.Errorf("version: %+v", got["mem.cgroup.version"])
	}
	for _, id := range cgroupV2IDs() {
		if got[id].Status != obs.StatusPermissionDenied {
			t.Errorf("%s: %s, want permission_denied", id, got[id].Status)
		}
	}
}

// Garbage in every file must give failed observations with the parse error
// in the note: never a panic, never a value.
func TestMalformed(t *testing.T) {
	files := modernMachine(t)
	for p := range files {
		if p != "/proc/self/mountinfo" {
			files[p] = "\x00garbage: [[ 12 ]] lots\n"
		}
	}
	got := run(t, machine(t, files))
	for id, o := range got {
		if o.Status != obs.StatusFailed || o.Value != nil || !strings.Contains(o.Note, "unexpected format") {
			t.Errorf("%s: %s %v (%s), want failed with the parse error", id, o.Status, o.Value, o.Note)
		}
	}
}

// Truncated files, cut mid-line as a racing read might see them, must also
// fail rather than produce partial tables.
func TestTruncated(t *testing.T) {
	files := modernMachine(t)
	// Cut points inside a zone's watermarks, the "full" line, and the last
	// buddyinfo line.
	for p, cut := range map[string]int{"/proc/zoneinfo": 1200, "/proc/pressure/memory": 90, "/proc/buddyinfo": 150} {
		files[p] = files[p][:cut]
	}
	got := run(t, machine(t, files))
	for _, id := range []string{"mem.zones", "mem.psi.full.avg10", "mem.buddyinfo"} {
		if got[id].Status != obs.StatusFailed {
			t.Errorf("%s: %s, want failed", id, got[id].Status)
		}
	}
}

func TestMissingEverything(t *testing.T) {
	got := run(t, machine(t, map[string]string{}))
	for id, o := range got {
		if o.Status != obs.StatusAbsent {
			t.Errorf("%s: %s, want absent", id, o.Status)
		}
	}
}
