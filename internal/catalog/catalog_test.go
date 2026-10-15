package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var validTypes = []string{obs.TypeInt, obs.TypeFloat, obs.TypeString, obs.TypeBool, obs.TypeTable, TypeLimit}

func TestEntriesAreComplete(t *testing.T) {
	entries := append(All(), Families()...)
	if len(entries) < 150 {
		t.Fatalf("only %d entries; did a namespace slice drop out of index?", len(entries))
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.ID, ".*")
		if !obs.ValidID(id) {
			t.Errorf("%s: invalid id", e.ID)
		}
		if e.Title == "" || e.Description == "" || e.Caveat == "" || e.Privileged == "" {
			t.Errorf("%s: title, description, caveat and privileged are all required", e.ID)
		}
		if !slices.Contains(validTypes, e.Type) {
			t.Errorf("%s: invalid type %q", e.ID, e.Type)
		}
		if !obs.ValidUnit(e.Unit) {
			t.Errorf("%s: invalid unit %q", e.ID, e.Unit)
		}
		if (e.Type == obs.TypeTable) != (len(e.Columns) > 0) {
			t.Errorf("%s: tables and only tables declare columns", e.ID)
		}
		for _, c := range e.IdentifyingColumns {
			if !e.hasColumn(c) {
				t.Errorf("%s: identifying column %q is not a column", e.ID, c)
			}
		}
		if !e.Sensitivity.Valid() {
			t.Errorf("%s: invalid sensitivity %q", e.ID, e.Sensitivity)
		}
		// Redaction replaces sensitive values with placeholder strings, which
		// only type-checks for strings and tables.
		if e.Sensitivity != None && e.Type != obs.TypeString && e.Type != obs.TypeTable {
			t.Errorf("%s: sensitive values must be strings or tables", e.ID)
		}
		if strings.HasPrefix(e.ID, "derived.") {
			if e.Basis != Arithmetic && e.Basis != Inference {
				t.Errorf("%s: derived facts need a basis", e.ID)
			}
			if e.Kind != "" || e.Scope != "" {
				t.Errorf("%s: derived facts have no kind or scope", e.ID)
			}
			// A ratio from 0 to 1 is stored as such and shown as a
			// percentage; the unit is what tells the formatters so.
			if strings.HasSuffix(e.ID, "_fraction") != (e.Unit == obs.UnitFraction) {
				t.Errorf("%s: unit %q; derived *_fraction facts, and only they, have unit fraction", e.ID, e.Unit)
			}
		} else {
			if !e.Kind.Valid() || !e.Scope.Valid() {
				t.Errorf("%s: invalid kind %q or scope %q", e.ID, e.Kind, e.Scope)
			}
			if e.Basis != "" {
				t.Errorf("%s: only derived facts have a basis", e.ID)
			}
		}
	}
}

// The ids ARCHITECTURE.md §4 and §6 require, spelled out so a deletion is noticed.
func TestRequiredIDsDeclared(t *testing.T) {
	required := strings.Fields(`
		mem.zones mem.buddyinfo mem.swap.areas
		mem.sysctl.vm.swappiness mem.sysctl.vm.overcommit_memory mem.sysctl.vm.overcommit_ratio
		mem.sysctl.vm.min_free_kbytes mem.sysctl.vm.watermark_scale_factor mem.sysctl.vm.dirty_ratio
		mem.sysctl.vm.dirty_background_ratio mem.sysctl.vm.max_map_count mem.sysctl.vm.page_cluster
		mem.sysctl.vm.vfs_cache_pressure mem.sysctl.vm.nr_hugepages mem.sysctl.kernel.randomize_va_space
		mem.thp.enabled mem.thp.defrag mem.thp.shmem_enabled mem.thp.pmd_size_bytes mem.lru_gen.enabled
		mem.zswap.enabled mem.zram.devices
		mem.psi.some.avg10 mem.psi.some.avg60 mem.psi.some.avg300 mem.psi.some.total_us
		mem.psi.full.avg10 mem.psi.full.avg60 mem.psi.full.avg300 mem.psi.full.total_us
		mem.numa.nodes mem.hugepages.sizes mem.memory_block_size_bytes mem.slabinfo.cache_count
		mem.cgroup.version mem.cgroup.path mem.cgroup.memory.max mem.cgroup.memory.high
		mem.cgroup.memory.current mem.cgroup.memory.swap_max mem.cgroup.memory.swap_current
		mem.cgroup.events.oom mem.cgroup.events.oom_kill mem.cgroup.events.max mem.cgroup.events.high
		os.release.id os.release.id_like os.release.name os.release.version_id os.hostname
		kernel.release kernel.version_string kernel.arch kernel.page_size_bytes kernel.boot_time kernel.uptime_seconds
		cpu.model_name cpu.vendor cpu.logical_count cpu.core_count cpu.socket_count cpu.threads_per_core
		cpu.address_bits.virtual cpu.address_bits.physical cpu.flags.la57 cpu.flags.pse cpu.flags.pdpe1gb
		cpu.flags.hypervisor cpu.caches cpu.online init.pid1_comm init.systemd_booted
		virt.dmi.sys_vendor virt.dmi.product_name virt.container.dockerenv virt.container.containerenv
		virt.pid1_cgroup_root virt.devicetree.model block.devices block.lsblk mounts.filesystems net.interfaces
		session.type session.desktop session.wayland_display_set session.x11_display_set
		probe.regions probe.maps.sample probe.maps.vma_count probe.maps.lowest_start probe.maps.highest_end
		probe.status.VmSize probe.status.VmRSS probe.status.RssAnon probe.status.RssFile probe.status.RssShmem
		probe.status.VmSwap probe.status.VmPTE probe.faults.minflt_before_touch probe.faults.minflt_after_touch
		probe.faults.majflt_before_touch probe.faults.majflt_after_touch probe.pagemap.pfn_visible probe.layout.runs
		derived.env.kind derived.env.arch_family derived.paging.levels derived.paging.va_bits
		derived.paging.index_bits derived.paging.user_space_bytes derived.paging.profile derived.mem.total_pages
		derived.mem.used_estimate_bytes derived.mem.available_fraction derived.mem.file_cache_bytes
		derived.mem.accounted_bytes derived.mem.unaccounted_bytes derived.swap.configured derived.swap.total_bytes
		derived.swap.used_fraction derived.swap.compressed derived.thp.mode derived.thp.enabled
		derived.reclaim.lru_kind derived.zones.min_bytes derived.zones.low_bytes derived.zones.high_bytes
		derived.zones.free_bytes derived.numa.node_count derived.numa.multi derived.cpu.smt
		derived.oom.kills_since_boot derived.oom.any derived.cgroup.memory_limited derived.overcommit.mode_name
		derived.commit.over_limit derived.probe.untouched_rss_bytes derived.probe.written_pages_expected
		derived.probe.written_minflt_delta derived.probe.written_huge_fraction derived.probe.read_region_rss_bytes
		derived.probe.aslr_differs derived.psi.available derived.dirty.threshold_estimate_bytes
		mem.meminfo.MemTotal mem.meminfo.HugePages_Total mem.vmstat.pgfault mem.vmstat.workingset_refault_file`)
	for _, id := range required {
		e, ok := Lookup(id)
		if !ok || e.Family() {
			t.Errorf("%s: not declared as an exact entry", id)
		}
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		id, want string
	}{
		{"mem.meminfo.MemTotal", "mem.meminfo.MemTotal"},
		{"mem.meminfo.DirectMap4k", "mem.meminfo.*"},
		{"mem.vmstat.nr_free_pages", "mem.vmstat.*"},
		{"mem.vmstat.pgfault", "mem.vmstat.pgfault"},
		{"mem.meminfo.", ""},
		{"mem.meminfo", ""},
		{"mem.unknown", ""},
		{"derived.thp.mode", "derived.thp.mode"},
	}
	for _, tt := range tests {
		e, ok := Lookup(tt.id)
		if ok != (tt.want != "") || e.ID != tt.want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", tt.id, e.ID, ok, tt.want)
		}
	}
	if e, _ := Lookup("mem.meminfo.HugePages_Free"); e.Unit != obs.UnitCount {
		t.Errorf("HugePages_Free unit = %q, want count", e.Unit)
	}
}

func TestCheck(t *testing.T) {
	entry := func(id string) Entry {
		e, ok := Lookup(id)
		if !ok {
			t.Fatalf("%s not declared", id)
		}
		return e
	}
	tests := []struct {
		id string
		v  any
		ok bool
	}{
		{"mem.meminfo.MemTotal", int64(1), true},
		{"mem.meminfo.MemTotal", 1.5, false},
		{"mem.meminfo.MemTotal", "1", false},
		{"mem.psi.some.avg10", 0.25, true},
		{"mem.psi.some.avg10", int64(0), true}, // 0.0 encodes as 0
		{"mem.thp.enabled", "always", true},
		{"mem.zswap.enabled", true, true},
		{"mem.zswap.enabled", "Y", false},
		{"mem.cgroup.memory.max", "max", true},
		{"mem.cgroup.memory.max", int64(1 << 30), true},
		{"mem.cgroup.memory.max", "unlimited", false},
		{"mem.buddyinfo", []map[string]any{{"node": int64(0), "zone": "Normal", "order10": int64(3)}}, true},
		{"mem.buddyinfo", []map[string]any{{"node": int64(0), "nodes": int64(1)}}, false},
		{"net.interfaces", []map[string]any{{"name": "lo", "type": "loopback", "operstate": "unknown"}}, true},
		{"net.interfaces", []map[string]any{{"address": "x"}}, false},
		{"net.interfaces", "lo", false},
		{"kernel.release", nil, false},
	}
	for _, tt := range tests {
		err := entry(tt.id).Check(tt.v)
		if (err == nil) != tt.ok {
			t.Errorf("%s.Check(%#v) = %v, want ok=%v", tt.id, tt.v, err, tt.ok)
		}
	}
}

func TestCheckRejectsImplausibleIntegers(t *testing.T) {
	for _, c := range []struct {
		id string
		v  any
		ok bool
	}{
		{"mem.meminfo.MemTotal", int64(-5), false},
		{"mem.meminfo.MemTotal", int64(0), true},
		{"kernel.page_size_bytes", int64(0), false},
		{"kernel.page_size_bytes", int64(1) << 62, false},
		{"kernel.page_size_bytes", int64(6000), false},
		{"kernel.page_size_bytes", int64(16384), true},
		{"cpu.address_bits.virtual", int64(-1), false},
	} {
		e, found := Lookup(c.id)
		if !found {
			t.Fatalf("no entry %s", c.id)
		}
		if err := e.Check(c.v); (err == nil) != c.ok {
			t.Errorf("%s = %v: Check returned %v", c.id, c.v, err)
		}
	}
}
