package derive

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// missing marks an observation that exists with a non-ok status.
type missing obs.Status

// snap builds a snapshot from id → value; a missing(...) value makes the
// observation non-ok with a null value. Collectors are written elsewhere, so
// tests describe machines directly.
func snap(t *testing.T, vals map[string]any) *snapshot.Snapshot {
	t.Helper()
	s := &snapshot.Snapshot{}
	for _, id := range slices.Sorted(maps.Keys(vals)) {
		o := obs.Observation{ID: id, Status: obs.StatusOK, Kind: obs.KindStatic, Scope: obs.ScopeKernel}
		if m, ok := vals[id].(missing); ok {
			o.Status = obs.Status(m)
		} else {
			v, err := obs.Normalize(vals[id])
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			o.Value = v
		}
		s.Observations = append(s.Observations, o)
	}
	return s
}

// desktop is an x86-64 desktop with swap, THP always, MGLRU and some OOM
// kills: every required input present.
func desktop() map[string]any {
	gib := int64(1) << 30
	return map[string]any{
		"kernel.arch":                          "x86_64",
		"kernel.page_size_bytes":               4096,
		"cpu.flags.la57":                       false,
		"cpu.flags.hypervisor":                 false,
		"cpu.address_bits.virtual":             48,
		"cpu.address_bits.physical":            39,
		"cpu.threads_per_core":                 2,
		"virt.container.dockerenv":             false,
		"virt.container.containerenv":          false,
		"virt.container.systemd":               missing(obs.StatusAbsent),
		"virt.pid1_cgroup_root":                true,
		"init.pid1_comm":                       "systemd",
		"virt.dmi.sys_vendor":                  "Example Computers Ltd.",
		"virt.dmi.product_name":                "Workstation 7",
		"mem.meminfo.MemTotal":                 16 * gib,
		"mem.meminfo.MemFree":                  2 * gib,
		"mem.meminfo.MemAvailable":             10 * gib,
		"mem.meminfo.Cached":                   7 * gib,
		"mem.meminfo.Buffers":                  gib / 4,
		"mem.meminfo.Shmem":                    gib / 2,
		"mem.meminfo.AnonPages":                4 * gib,
		"mem.meminfo.Slab":                     gib,
		"mem.meminfo.KernelStack":              int64(32 << 20),
		"mem.meminfo.PageTables":               int64(96 << 20),
		"mem.meminfo.Percpu":                   int64(16 << 20),
		"mem.meminfo.VmallocUsed":              int64(112 << 20),
		"mem.meminfo.SwapTotal":                8 * gib,
		"mem.meminfo.SwapFree":                 6 * gib,
		"mem.meminfo.CommitLimit":              16 * gib,
		"mem.meminfo.Committed_AS":             20 * gib,
		"mem.sysctl.vm.dirty_ratio":            20,
		"mem.sysctl.vm.dirty_background_ratio": 10,
		"mem.sysctl.vm.dirty_bytes":            0,
		"mem.sysctl.vm.dirty_background_bytes": 0,
		"mem.meminfo.Active_file":              3 * gib,
		"mem.meminfo.Inactive_file":            2 * gib,
		"mem.vmstat.allocstall_dma":            missing(obs.StatusAbsent),
		"mem.vmstat.allocstall_normal":         7,
		"mem.vmstat.allocstall_movable":        35,
		"mem.sysctl.vm.overcommit_memory":      0,
		"mem.zswap.enabled":                    false,
		"mem.zram.devices":                     []map[string]any{},
		"mem.swap.areas":                       []map[string]any{{"name": "nvme0n1p3", "type": "partition", "size_bytes": 8 * gib, "used_bytes": 2 * gib, "priority": -2}},
		"mem.thp.enabled":                      "always",
		"mem.lru_gen.enabled":                  "0x0007",
		"mem.vmstat.oom_kill":                  3,
		"mem.cgroup.memory.max":                "max",
		"mem.cgroup.memory.high":               "max",
		"mem.psi.some.avg10":                   0.12,
		"mem.zones": []map[string]any{
			{"node": 0, "zone": "DMA32", "free": 1000, "min": 10, "low": 20, "high": 30},
			{"node": 0, "zone": "Normal", "free": 3000, "min": 100, "low": 200, "high": 300},
		},
		"mem.numa.nodes": []map[string]any{{"node": 0, "mem_total_bytes": 16 * gib, "mem_free_bytes": 2 * gib, "cpulist": "0-15"}},
		"probe.regions": []map[string]any{
			{"name": "anon-untouched", "size_bytes": 64 << 20, "rss_bytes": 0},
			{"name": "anon-written", "size_bytes": 8 << 20, "rss_bytes": 8 << 20, "anon_huge_bytes": 4 << 20},
			{"name": "anon-read", "size_bytes": 4 << 20, "rss_bytes": 0},
		},
		"probe.faults.minflt_before_touch": 301,
		"probe.faults.minflt_after_touch":  2349,
		"probe.maps.highest_end":           "0x7ffd5e8c4000",
		"probe.layout.runs": []map[string]any{
			{"run": 1, "stack": "0x7ffd10000000", "mmap_base": "0x7f1a00000000", "heap": "0x55aa000000", "exe": "0x55a9f0000000"},
			{"run": 2, "stack": "0x7ffc20000000", "mmap_base": "0x7f2b00000000", "heap": "0x55aa000000", "exe": "0x56c100000000"},
		},
	}
}

func byID(facts []Fact) map[string]Fact {
	m := map[string]Fact{}
	for _, f := range facts {
		m[f.ID] = f
	}
	return m
}

func TestRequiredFactsOnDesktop(t *testing.T) {
	gib := int64(1) << 30
	facts := byID(All(snap(t, desktop())))
	want := map[string]any{
		"derived.env.kind":                      "no-virtualization-evidence",
		"derived.env.arch_family":               "x86",
		"derived.paging.levels":                 int64(4),
		"derived.paging.va_bits":                int64(48),
		"derived.paging.index_bits":             int64(9),
		"derived.paging.offset_bits":            int64(12),
		"derived.paging.leaf_table_span_bytes":  int64(2 << 20),
		"derived.paging.upper_entry_span_bytes": gib,
		"derived.paging.user_space_bytes":       int64(1) << 47,
		"derived.paging.profile":                "x86_64-4level",
		"derived.mem.total_pages":               int64(4 << 20),
		"derived.mem.used_estimate_bytes":       6 * gib,
		"derived.mem.available_fraction":        0.625,
		"derived.mem.file_cache_bytes":          7*gib + gib/4 - gib/2,
		"derived.mem.accounted_bytes":           2*gib + gib/4 + 7*gib + 4*gib + gib + (256 << 20),
		"derived.mem.unaccounted_bytes":         16*gib - (2*gib + gib/4 + 7*gib + 4*gib + gib + (256 << 20)),
		"derived.swap.configured":               true,
		"derived.swap.total_bytes":              8 * gib,
		"derived.swap.used_fraction":            0.25,
		"derived.swap.compressed":               false,
		"derived.thp.mode":                      "always",
		"derived.thp.enabled":                   true,
		"derived.reclaim.lru_kind":              "mglru",
		"derived.zones.min_bytes":               int64(110 * 4096),
		"derived.zones.low_bytes":               int64(220 * 4096),
		"derived.zones.high_bytes":              int64(330 * 4096),
		"derived.zones.free_bytes":              int64(4000 * 4096),
		"derived.numa.node_count":               int64(1),
		"derived.numa.multi":                    false,
		"derived.cpu.smt":                       true,
		"derived.cpu.phys_addressable_bytes":    int64(512) * gib,
		"derived.oom.kills_since_boot":          int64(3),
		"derived.oom.any":                       true,
		"derived.cgroup.memory_limited":         false,
		"derived.overcommit.mode_name":          "heuristic",
		"derived.commit.over_limit":             true,
		"derived.probe.untouched_rss_bytes":     int64(0),
		"derived.probe.written_pages_expected":  int64(2048),
		"derived.probe.written_minflt_delta":    int64(2048),
		"derived.probe.written_huge_fraction":   0.5,
		"derived.probe.read_region_rss_bytes":   int64(0),
		"derived.probe.aslr_differs":            true,
		"derived.psi.available":                 true,
		// Dirtyable: MemFree − 330 high-watermark pages + Active(file) + Inactive(file).
		"derived.dirty.threshold_estimate_bytes":            (7*gib - 330*4096) * 20 / 100,
		"derived.dirty.background_threshold_estimate_bytes": (7*gib - 330*4096) * 10 / 100,
		"derived.reclaim.allocstalls":                       int64(42),
	}
	for id, v := range want {
		f, ok := facts[id]
		if !ok {
			t.Errorf("%s: not produced", id)
			continue
		}
		if f.Status != StatusOK || f.Value != v {
			t.Errorf("%s = %#v (%s: %s), want %#v", id, f.Value, f.Status, f.Reason, v)
		}
	}
}

func TestFactsAreDeclaredSortedAndExplained(t *testing.T) {
	facts := All(snap(t, desktop()))
	if !slices.IsSortedFunc(facts, func(a, b Fact) int { return strings.Compare(a.ID, b.ID) }) {
		t.Error("facts are not sorted by id")
	}
	produced := map[string]bool{}
	for _, f := range facts {
		produced[f.ID] = true
		e, ok := catalog.Lookup(f.ID)
		if !ok || e.Family() {
			t.Errorf("%s is not declared in the catalog", f.ID)
			continue
		}
		if f.Basis != e.Basis || f.Unit != e.Unit {
			t.Errorf("%s: basis %q unit %q, catalog says %q %q", f.ID, f.Basis, f.Unit, e.Basis, e.Unit)
		}
		if f.Rule == "" || len(f.Inputs) == 0 {
			t.Errorf("%s: missing rule or inputs", f.ID)
		}
		if f.Status == StatusOK && f.Value != nil {
			if err := e.Check(f.Value); err != nil {
				t.Errorf("%s: %v", f.ID, err)
			}
		}
	}
	for _, e := range catalog.All() {
		if strings.HasPrefix(e.ID, "derived.") && !produced[e.ID] {
			t.Errorf("%s is declared but never computed", e.ID)
		}
	}
}

func TestUnknownPropagation(t *testing.T) {
	tests := []struct {
		name    string
		change  map[string]any
		unknown map[string]string // fact → substring of reason
		stillOK []string
	}{
		{"MemAvailable denied",
			map[string]any{"mem.meminfo.MemAvailable": missing(obs.StatusPermissionDenied)},
			map[string]string{
				"derived.mem.used_estimate_bytes": "mem.meminfo.MemAvailable is permission denied",
				"derived.mem.available_fraction":  "permission denied",
			},
			[]string{"derived.mem.total_pages", "derived.mem.file_cache_bytes"}},
		{"probe not collected",
			map[string]any{"probe.regions": missing(obs.StatusNotCollected), "probe.layout.runs": missing(obs.StatusNotCollected)},
			map[string]string{
				"derived.probe.untouched_rss_bytes":    "probe.regions is not collected",
				"derived.probe.written_pages_expected": "not collected",
				"derived.probe.aslr_differs":           "probe.layout.runs is not collected",
			},
			[]string{"derived.probe.written_minflt_delta", "derived.paging.levels"}},
		{"id missing from snapshot",
			map[string]any{"mem.meminfo.Percpu": nil},
			map[string]string{
				"derived.mem.accounted_bytes":   "mem.meminfo.Percpu is not collected",
				"derived.mem.unaccounted_bytes": "Percpu",
			},
			[]string{"derived.mem.used_estimate_bytes"}},
		{"no swap",
			map[string]any{"mem.meminfo.SwapTotal": 0, "mem.meminfo.SwapFree": 0},
			map[string]string{"derived.swap.used_fraction": "SwapTotal is 0"},
			[]string{"derived.swap.configured", "derived.swap.total_bytes"}},
		{"file LRU denied",
			map[string]any{"mem.meminfo.Active_file": missing(obs.StatusPermissionDenied)},
			map[string]string{
				"derived.dirty.threshold_estimate_bytes":            "mem.meminfo.Active_file is permission denied",
				"derived.dirty.background_threshold_estimate_bytes": "permission denied",
			},
			[]string{"derived.mem.used_estimate_bytes"}},
		{"dirty_bytes not collected",
			map[string]any{"mem.sysctl.vm.dirty_bytes": nil},
			map[string]string{"derived.dirty.threshold_estimate_bytes": "mem.sysctl.vm.dirty_bytes is not collected"},
			[]string{"derived.mem.used_estimate_bytes"}},
		{"allocstall counter failed",
			map[string]any{"mem.vmstat.allocstall_movable": missing(obs.StatusFailed)},
			map[string]string{"derived.reclaim.allocstalls": "mem.vmstat.allocstall_movable is failed"},
			nil},
		{"no allocstall counter",
			map[string]any{"mem.vmstat.allocstall_normal": nil, "mem.vmstat.allocstall_movable": nil},
			map[string]string{"derived.reclaim.allocstalls": "no mem.vmstat.allocstall_* counter is readable"},
			nil},
		{"cgroup v1",
			map[string]any{"mem.cgroup.memory.max": missing(obs.StatusUnsupported), "mem.cgroup.memory.high": missing(obs.StatusUnsupported)},
			map[string]string{"derived.cgroup.memory_limited": "not available"},
			nil},
		{"zone table failed",
			map[string]any{"mem.zones": missing(obs.StatusFailed)},
			map[string]string{"derived.zones.min_bytes": "mem.zones is failed", "derived.zones.free_bytes": "failed"},
			[]string{"derived.numa.node_count"}},
		{"page size missing",
			map[string]any{"kernel.page_size_bytes": missing(obs.StatusFailed)},
			map[string]string{"derived.paging.index_bits": "failed", "derived.mem.total_pages": "failed"},
			[]string{"derived.paging.levels"}},
	}
	for _, tt := range tests {
		vals := desktop()
		for id, v := range tt.change {
			if v == nil {
				delete(vals, id)
			} else {
				vals[id] = v
			}
		}
		facts := byID(All(snap(t, vals)))
		for id, sub := range tt.unknown {
			f := facts[id]
			if f.Status != StatusUnknown || f.Value != nil || !strings.Contains(f.Reason, sub) {
				t.Errorf("%s: %s = %+v, want unknown with reason containing %q", tt.name, id, f, sub)
			}
		}
		for _, id := range tt.stillOK {
			if f := facts[id]; f.Status != StatusOK {
				t.Errorf("%s: %s became %s (%s), want ok", tt.name, id, f.Status, f.Reason)
			}
		}
	}
}

func TestPaging(t *testing.T) {
	tests := []struct {
		name    string
		vals    map[string]any
		levels  int64
		va      int64
		user    int64
		profile string
		basis   string
	}{
		{"x86 4-level", map[string]any{"cpu.flags.la57": false}, 4, 48, 1 << 47, "x86_64-4level", catalog.Inference},
		{"x86 la57 with 57 bits", map[string]any{"cpu.flags.la57": true, "cpu.address_bits.virtual": 57}, 5, 57, 1 << 56, "x86_64-5level", catalog.Inference},
		// The flag is listed only when the kernel enabled 5 levels; the CPU's
		// reported width is a CPUID property and does not decide.
		{"x86 la57 with 48 bits", map[string]any{"cpu.flags.la57": true, "cpu.address_bits.virtual": 48}, 5, 57, 1 << 56, "x86_64-5level", catalog.Inference},
		{"x86 la57 width denied", map[string]any{"cpu.flags.la57": true, "cpu.address_bits.virtual": missing(obs.StatusPermissionDenied)}, 5, 57, 1 << 56, "x86_64-5level", catalog.Inference},
		{"x86 no la57 with 57 bits", map[string]any{"cpu.flags.la57": false, "cpu.address_bits.virtual": 57}, 4, 48, 1 << 47, "x86_64-4level", catalog.Inference},
		{"arm64 4k 39-bit", arm("aarch64", 4096, "0x7fc8a1b000"), 3, 39, 1 << 39, "arm64-4k-39", ""},
		{"arm64 4k 48-bit", arm("aarch64", 4096, "0xffffd2a71000"), 4, 48, 1 << 48, "arm64-4k-48", ""},
		{"arm64 16k 47-bit", arm("aarch64", 16384, "0x7fffd2a70000"), 3, 47, 1 << 47, "arm64-16k-47", ""},
		{"arm64 16k 48-bit", arm("aarch64", 16384, "0xffffd2a70000"), 4, 48, 1 << 48, "arm64-16k-48", ""},
		{"arm64 64k 48-bit", arm("aarch64", 65536, "0xffffd2a70000"), 3, 48, 1 << 48, "arm64-64k-48", ""},
		{"riscv Sv39", arm("riscv64", 4096, "0x3fffd2a000"), 3, 39, 1 << 38, "riscv-sv39", ""},
		{"riscv Sv48", arm("riscv64", 4096, "0x7fffd2a71000"), 4, 48, 1 << 47, "riscv-sv48", ""},
	}
	for _, tt := range tests {
		vals := desktop()
		for k, v := range tt.vals {
			vals[k] = v
		}
		facts := byID(All(snap(t, vals)))
		got := []any{facts["derived.paging.levels"].Value, facts["derived.paging.va_bits"].Value,
			facts["derived.paging.user_space_bytes"].Value, facts["derived.paging.profile"].Value}
		want := []any{tt.levels, tt.va, tt.user, tt.profile}
		if !slices.Equal(got, want) {
			t.Errorf("%s: levels, va, user, profile = %v, want %v (%s)", tt.name, got, want, facts["derived.paging.levels"].Reason)
		}
		if f := facts["derived.paging.levels"]; f.Basis != catalog.Inference || !strings.Contains(strings.Join(f.Inputs, " "), "kernel.arch") {
			t.Errorf("%s: levels basis %q inputs %v", tt.name, f.Basis, f.Inputs)
		}
		if strings.HasPrefix(tt.name, "arm64") && !strings.Contains(facts["derived.paging.va_bits"].Rule, "highest user address") {
			t.Errorf("%s: rule does not say the width is inferred from the probe: %s", tt.name, facts["derived.paging.va_bits"].Rule)
		}
	}
}

// arm describes a non-x86 machine whose VA width must come from the probe.
func arm(arch string, pageSize int, highest string) map[string]any {
	return map[string]any{
		"kernel.arch": arch, "kernel.page_size_bytes": pageSize, "probe.maps.highest_end": highest,
		"cpu.flags.la57": missing(obs.StatusUnsupported), "cpu.address_bits.virtual": missing(obs.StatusAbsent),
	}
}

func TestPagingUnknown(t *testing.T) {
	tests := []struct {
		name   string
		vals   map[string]any
		reason string
	}{
		{"arm64 without probe", map[string]any{"kernel.arch": "aarch64", "probe.maps.highest_end": missing(obs.StatusNotCollected)}, "probe.maps.highest_end is not collected"},
		{"x86 la57 denied", map[string]any{"cpu.flags.la57": missing(obs.StatusPermissionDenied)}, "cpu.flags.la57 is permission denied"},
		{"unmodelled arch", map[string]any{"kernel.arch": "mips64"}, "not modelled"},
		{"arm64 16k 36-bit has no simulator profile", arm("aarch64", 16384, "0xfffff0000"), ""},
	}
	for _, tt := range tests {
		vals := desktop()
		for k, v := range tt.vals {
			vals[k] = v
		}
		facts := byID(All(snap(t, vals)))
		if tt.reason == "" {
			if f := facts["derived.paging.levels"]; f.Status != StatusOK || f.Value != int64(2) {
				t.Errorf("%s: levels %+v, want 2", tt.name, f)
			}
			if f := facts["derived.paging.profile"]; f.Status != StatusUnknown || !strings.Contains(f.Reason, "no profile") {
				t.Errorf("%s: profile %+v, want unknown", tt.name, f)
			}
			continue
		}
		for _, id := range []string{"derived.paging.levels", "derived.paging.va_bits", "derived.paging.user_space_bytes", "derived.paging.profile"} {
			if f := facts[id]; f.Status != StatusUnknown || !strings.Contains(f.Reason, tt.reason) {
				t.Errorf("%s: %s = %+v, want unknown with %q", tt.name, id, f, tt.reason)
			}
		}
	}
}

func TestEnvKind(t *testing.T) {
	tests := []struct {
		name     string
		vals     map[string]any
		want     any
		evidence []string
	}{
		{"desktop", nil, "no-virtualization-evidence", []string{"not proof of bare metal"}},
		{"docker", map[string]any{"virt.container.dockerenv": true, "virt.pid1_cgroup_root": false, "init.pid1_comm": "sh"},
			"container", []string{"/.dockerenv exists", "below the cgroup root", `"sh"`}},
		// containerd, Kubernetes and LXC create no marker file; with a
		// private cgroup namespace PID 1 appears at the root, as a host's
		// non-systemd init does, so the name of PID 1 decides.
		{"cgroup namespace without marker", map[string]any{"virt.pid1_cgroup_root": true, "init.pid1_comm": "sh"},
			"container", []string{`PID 1 is "sh", not an init system`, "own cgroup namespace"}},
		{"systemd in a container", map[string]any{"virt.container.systemd": "lxc"}, "container", []string{`"lxc"`}},
		{"host with another init at the root", map[string]any{"virt.pid1_cgroup_root": true, "init.pid1_comm": "runit"},
			"no-virtualization-evidence", []string{`"runit"`}},
		{"PID 1 hidden", map[string]any{"init.pid1_comm": missing(obs.StatusPermissionDenied)}, nil, nil},
		{"podman marker only", map[string]any{"virt.container.containerenv": true}, "container", []string{"/run/.containerenv exists"}},
		{"systemd outside root is not evidence", map[string]any{"virt.pid1_cgroup_root": false}, "no-virtualization-evidence", nil},
		{"kvm guest", map[string]any{"cpu.flags.hypervisor": true, "virt.dmi.sys_vendor": "QEMU", "virt.dmi.product_name": "Standard PC (Q35 + ICH9, 2009)"},
			"vm-guest", []string{"hypervisor flag", `"QEMU"`}},
		{"container in a VM", map[string]any{"virt.container.dockerenv": true, "cpu.flags.hypervisor": true},
			"container", []string{"/.dockerenv", "also runs in a VM"}},
		{"arm board", map[string]any{"cpu.flags.hypervisor": missing(obs.StatusUnsupported), "virt.dmi.sys_vendor": missing(obs.StatusAbsent),
			"virt.dmi.product_name": missing(obs.StatusAbsent), "virt.devicetree.model": "Example Single-Board Computer Rev 1.2"},
			"no-virtualization-evidence", []string{"not available"}},
		{"restricted", map[string]any{"virt.container.dockerenv": missing(obs.StatusPermissionDenied)}, nil, nil},
	}
	for _, tt := range tests {
		vals := desktop()
		for k, v := range tt.vals {
			vals[k] = v
		}
		f := byID(All(snap(t, vals)))["derived.env.kind"]
		if f.Value != tt.want || f.Basis != catalog.Inference {
			t.Errorf("%s: %v (%s), want %v", tt.name, f.Value, f.Reason, tt.want)
		}
		for _, e := range tt.evidence {
			if !strings.Contains(f.Rule, e) {
				t.Errorf("%s: rule %q does not mention %q", tt.name, f.Rule, e)
			}
		}
		if strings.Contains(f.Rule, "bare metal") && !strings.Contains(f.Rule, "not proof of bare metal") {
			t.Errorf("%s: rule claims bare metal: %s", tt.name, f.Rule)
		}
	}
}

func TestPresenceFacts(t *testing.T) {
	tests := []struct {
		name string
		vals map[string]any
		id   string
		want any
	}{
		{"zswap on", map[string]any{"mem.zswap.enabled": true}, "derived.swap.compressed", true},
		// Installed is not active: zswap without swap, and a zram device
		// that is not a swap area, compress nothing.
		{"zswap on without swap", map[string]any{"mem.zswap.enabled": true, "mem.meminfo.SwapTotal": 0, "mem.swap.areas": []map[string]any{}}, "derived.swap.compressed", false},
		{"zram device not swap", map[string]any{"mem.zram.devices": []map[string]any{{"name": "zram0", "disksize_bytes": 1 << 30}}}, "derived.swap.compressed", false},
		{"zram swap area", map[string]any{
			"mem.zram.devices": []map[string]any{{"name": "zram0", "disksize_bytes": 1 << 30}},
			"mem.swap.areas":   []map[string]any{{"name": "zram0", "type": "partition", "size_bytes": 1 << 30, "used_bytes": 0, "priority": 100}}}, "derived.swap.compressed", true},
		{"zswap absent counts as no", map[string]any{"mem.zswap.enabled": missing(obs.StatusAbsent), "mem.zram.devices": missing(obs.StatusAbsent)}, "derived.swap.compressed", false},
		{"zswap denied is unknown", map[string]any{"mem.zswap.enabled": missing(obs.StatusPermissionDenied)}, "derived.swap.compressed", nil},
		{"zswap denied without swap", map[string]any{"mem.zswap.enabled": missing(obs.StatusPermissionDenied), "mem.meminfo.SwapTotal": 0}, "derived.swap.compressed", false},
		{"swap areas unreadable", map[string]any{"mem.swap.areas": missing(obs.StatusPermissionDenied)}, "derived.swap.compressed", nil},
		{"no THP support", map[string]any{"mem.thp.enabled": missing(obs.StatusAbsent)}, "derived.thp.enabled", false},
		{"THP never", map[string]any{"mem.thp.enabled": "never"}, "derived.thp.enabled", false},
		{"THP madvise", map[string]any{"mem.thp.enabled": "madvise"}, "derived.thp.enabled", true},
		{"MGLRU off", map[string]any{"mem.lru_gen.enabled": "0x0000"}, "derived.reclaim.lru_kind", "classic"},
		{"MGLRU absent", map[string]any{"mem.lru_gen.enabled": missing(obs.StatusAbsent)}, "derived.reclaim.lru_kind", "classic"},
		{"MGLRU unreadable", map[string]any{"mem.lru_gen.enabled": missing(obs.StatusFailed)}, "derived.reclaim.lru_kind", nil},
		{"PSI absent", map[string]any{"mem.psi.some.avg10": missing(obs.StatusAbsent)}, "derived.psi.available", false},
		{"memory.max set", map[string]any{"mem.cgroup.memory.max": 512 << 20}, "derived.cgroup.memory_limited", true},
		{"two nodes", map[string]any{"mem.numa.nodes": []map[string]any{{"node": 0}, {"node": 1}}}, "derived.numa.multi", true},
		{"strict overcommit", map[string]any{"mem.sysctl.vm.overcommit_memory": 2}, "derived.overcommit.mode_name", "strict"},
		{"same layout", map[string]any{"probe.layout.runs": []map[string]any{
			{"run": 1, "stack": "0x1", "mmap_base": "0x2", "heap": "0x3", "exe": "0x4"},
			{"run": 2, "stack": "0x1", "mmap_base": "0x2", "heap": "0x3", "exe": "0x4"}}}, "derived.probe.aslr_differs", false},
	}
	for _, tt := range tests {
		vals := desktop()
		for k, v := range tt.vals {
			vals[k] = v
		}
		f := byID(All(snap(t, vals)))[tt.id]
		if f.Value != tt.want || (tt.want == nil) != (f.Status == StatusUnknown) {
			t.Errorf("%s: %s = %v (%s %s), want %v", tt.name, tt.id, f.Value, f.Status, f.Reason, tt.want)
		}
	}
}

// A fact whose value cannot be computed is unknown with a reason, never ok
// with a null value, whatever an imported snapshot holds.
func TestNoOKFactIsNull(t *testing.T) {
	for _, c := range []struct {
		name string
		vals map[string]any
		ids  []string
	}{
		{"MemTotal 0", map[string]any{"mem.meminfo.MemTotal": 0}, []string{"derived.mem.available_fraction"}},
		{"page size 0", map[string]any{"kernel.page_size_bytes": 0}, []string{"derived.mem.total_pages", "derived.probe.written_pages_expected", "derived.paging.leaf_table_span_bytes"}},
		{"page size 2^62", map[string]any{"kernel.page_size_bytes": int64(1) << 62}, []string{"derived.paging.leaf_table_span_bytes", "derived.paging.upper_entry_span_bytes"}},
	} {
		facts := byID(All(snap(t, mergeVals(desktop(), c.vals))))
		for _, id := range c.ids {
			if f := facts[id]; f.Status != StatusUnknown || f.Reason == "" {
				t.Errorf("%s: %s = %+v, want unknown with a reason", c.name, id, f)
			}
		}
		for _, f := range facts {
			if f.Status == StatusOK && f.Value == nil {
				t.Errorf("%s: %s is ok with a null value", c.name, f.ID)
			}
		}
	}
}

func mergeVals(base, over map[string]any) map[string]any {
	for k, v := range over {
		base[k] = v
	}
	return base
}

// TestDirtyThresholds follows domain_dirty_limits() in mm/page-writeback.c:
// a non-zero dirty_bytes replaces the ratio and is rounded up to whole
// pages, and a background threshold at or above the dirty threshold is cut
// to half of it. The rules say which case applied.
func TestDirtyThresholds(t *testing.T) {
	gib := int64(1) << 30
	dirtyable := 7*gib - 330*4096
	tests := []struct {
		name         string
		change       map[string]any
		thresh, bg   int64
		ruleContains string
	}{
		{"ratios", nil, dirtyable * 20 / 100, dirtyable * 10 / 100, "vm.dirty_bytes is 0, so vm.dirty_ratio applies"},
		{"dirty_bytes set", map[string]any{"mem.sysctl.vm.dirty_bytes": 10<<20 + 1, "mem.sysctl.vm.dirty_ratio": 0},
			10<<20 + 4096, (10<<20 + 4096) / 2, "vm.dirty_bytes is non-zero"},
		{"background above dirty", map[string]any{"mem.sysctl.vm.dirty_background_ratio": 30},
			dirtyable * 20 / 100, dirtyable * 20 / 100 / 2, "vm.dirty_bytes is 0"},
		{"background bytes set", map[string]any{"mem.sysctl.vm.dirty_background_bytes": 64 << 20},
			dirtyable * 20 / 100, 64 << 20, "vm.dirty_ratio applies"},
	}
	for _, tt := range tests {
		vals := desktop()
		maps.Copy(vals, tt.change)
		facts := byID(All(snap(t, vals)))
		th, bg := facts["derived.dirty.threshold_estimate_bytes"], facts["derived.dirty.background_threshold_estimate_bytes"]
		if th.Value != tt.thresh || bg.Value != tt.bg {
			t.Errorf("%s: thresholds %v (%s) and %v (%s), want %d and %d", tt.name, th.Value, th.Reason, bg.Value, bg.Reason, tt.thresh, tt.bg)
		}
		if !strings.Contains(th.Rule, tt.ruleContains) || !strings.Contains(th.Rule, "estimate") {
			t.Errorf("%s: rule %q does not say %q and that it is an estimate", tt.name, th.Rule, tt.ruleContains)
		}
	}
}

// TestAllocStallsRuleNamesKeys checks the rule lists exactly the counters
// summed, leaving out a zone the kernel does not have.
func TestAllocStallsRuleNamesKeys(t *testing.T) {
	f := byID(All(snap(t, desktop())))["derived.reclaim.allocstalls"]
	if !strings.HasPrefix(f.Rule, "allocstall_movable + allocstall_normal:") || strings.Contains(f.Rule, "allocstall_dma") {
		t.Errorf("rule %q should name allocstall_movable and allocstall_normal only", f.Rule)
	}
}

// TestRulesNeverCountUnavailableInputs: a rule must not state a count it
// could not take ("over all 0 zones") when the input is unavailable.
func TestRulesNeverCountUnavailableInputs(t *testing.T) {
	vals := desktop()
	vals["mem.zones"] = missing(obs.StatusPermissionDenied)
	vals["mem.numa.nodes"] = missing(obs.StatusPermissionDenied)
	vals["kernel.arch"] = missing(obs.StatusFailed)
	for _, f := range All(snap(t, vals)) {
		if f.Status == StatusUnknown && (strings.Contains(f.Rule, " 0 ") || strings.Contains(f.Rule, "2^0 ") || strings.Contains(f.Rule, " 0-bit")) {
			t.Errorf("%s: unknown, yet its rule %q states a count of zero", f.ID, f.Rule)
		}
	}
}
