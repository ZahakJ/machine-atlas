package memory

import (
	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// sysctl is one integer tunable under /proc/sys.
type sysctl struct {
	id   string
	path string
	kb   bool // the file is in KiB; the observation is in bytes
}

// sysctls are the tunables the lessons discuss. page-cluster is the one vm
// sysctl whose file name has a dash; its id uses an underscore.
var sysctls = []sysctl{
	{"mem.sysctl.vm.swappiness", "/proc/sys/vm/swappiness", false},
	{"mem.sysctl.vm.overcommit_memory", "/proc/sys/vm/overcommit_memory", false},
	{"mem.sysctl.vm.overcommit_ratio", "/proc/sys/vm/overcommit_ratio", false},
	{"mem.sysctl.vm.min_free_kbytes", "/proc/sys/vm/min_free_kbytes", true},
	{"mem.sysctl.vm.watermark_scale_factor", "/proc/sys/vm/watermark_scale_factor", false},
	{"mem.sysctl.vm.dirty_ratio", "/proc/sys/vm/dirty_ratio", false},
	{"mem.sysctl.vm.dirty_background_ratio", "/proc/sys/vm/dirty_background_ratio", false},
	{"mem.sysctl.vm.dirty_bytes", "/proc/sys/vm/dirty_bytes", false},
	{"mem.sysctl.vm.dirty_background_bytes", "/proc/sys/vm/dirty_background_bytes", false},
	{"mem.sysctl.vm.dirty_expire_centisecs", "/proc/sys/vm/dirty_expire_centisecs", false},
	{"mem.sysctl.vm.dirty_writeback_centisecs", "/proc/sys/vm/dirty_writeback_centisecs", false},
	{"mem.sysctl.vm.max_map_count", "/proc/sys/vm/max_map_count", false},
	{"mem.sysctl.vm.page_cluster", "/proc/sys/vm/page-cluster", false},
	{"mem.sysctl.vm.vfs_cache_pressure", "/proc/sys/vm/vfs_cache_pressure", false},
	{"mem.sysctl.vm.nr_hugepages", "/proc/sys/vm/nr_hugepages", false},
	{"mem.sysctl.kernel.randomize_va_space", "/proc/sys/kernel/randomize_va_space", false},
}

func sysctlCollector() collect.Collector {
	ids := make([]string, len(sysctls))
	for i, s := range sysctls {
		ids[i] = s.id
	}
	return collector{
		id:   "memory.sysctl",
		owns: ids,
		read: func(env collect.Env) []obs.Observation {
			out := make([]obs.Observation, 0, len(sysctls))
			for _, s := range sysctls {
				out = append(out, readSysctl(env, s))
			}
			return out
		},
	}
}

func readSysctl(env collect.Env, s sysctl) obs.Observation {
	src := obs.File(s.path, "")
	data, err := env.ReadFile(s.path)
	if err != nil {
		// Several of these exist only with a feature built in: nr_hugepages
		// needs CONFIG_HUGETLB_PAGE, watermark_scale_factor Linux 4.6.
		return unavailable(s.id, src, err, "this kernel does not provide the tunable; it depends on the kernel version and configuration")
	}
	v, err := parseInt(data)
	if err != nil {
		return malformed(s.id, src, err)
	}
	if s.kb {
		if v, err = kibToBytes(v); err != nil {
			return malformed(s.id, src, err)
		}
		src.Detail = fromKB
	}
	return obs.OK(s.id, v, src)
}
