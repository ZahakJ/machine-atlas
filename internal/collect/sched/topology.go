package sched

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	cpuDir      = "/sys/devices/system/cpu"
	onlinePath  = cpuDir + "/online"
	coreCPUs    = "/sys/devices/cpu_core/cpus"
	atomCPUs    = "/sys/devices/cpu_atom/cpus"
	smtDir      = cpuDir + "/smt"
	topoID      = "cpu.topology"
	cachesID    = "cpu.cache_domains"
	perfCPUsID  = "cpu.hybrid.performance_cpus"
	effCPUsID   = "cpu.hybrid.efficiency_cpus"
	smtCtrlID   = "cpu.smt.control"
	smtActiveID = "cpu.smt.active"
)

// cpuLists are the /sys/devices/system/cpu list files, with what a missing
// file means.
var cpuLists = []struct{ id, file, absent string }{
	{"cpu.present", "present", ""},
	{"cpu.possible", "possible", ""},
	{"cpu.offline", "offline", ""},
	{"cpu.isolated", "isolated", "no isolated file: the kernel predates it (Linux 4.2)"},
	{"cpu.nohz_full", "nohz_full", "no nohz_full file: the kernel was built without CONFIG_NO_HZ_FULL, so every CPU keeps its scheduler tick"},
}

const noHybrid = "no hybrid core list: the processor's cores are all of one kind, it is not an x86 hybrid part, or the kernel predates hybrid support (Linux 5.13)"

func topologyCollector() collect.Collector {
	owns := []string{topoID, cachesID, perfCPUsID, effCPUsID, smtCtrlID, smtActiveID}
	for _, l := range cpuLists {
		owns = append(owns, l.id)
	}
	return collector{
		id:   "sched.topology",
		owns: owns,
		read: func(env collect.Env) []obs.Observation {
			perf := cpuListFile(env, perfCPUsID, coreCPUs, noHybrid)
			eff := cpuListFile(env, effCPUsID, atomCPUs, noHybrid)
			out := []obs.Observation{perf, eff}
			online, onlineErr := readOnline(env)
			out = append(out, topologyTable(env, online, onlineErr, perf, eff))
			out = append(out, cacheDomains(env, online, onlineErr))
			out = append(out, smtObs(env)...)
			for _, l := range cpuLists {
				out = append(out, cpuListFile(env, l.id, cpuDir+"/"+l.file, l.absent))
			}
			return out
		},
	}
}

func readOnline(env collect.Env) ([]int, error) {
	s, err := readTrim(env, onlinePath)
	if err != nil {
		return nil, err
	}
	cpus, err := collect.ParseCPUList(s)
	if err != nil {
		return nil, err
	}
	if len(cpus) == 0 {
		return nil, errors.New(onlinePath + " lists no CPU")
	}
	return cpus, nil
}

// errOrMalformed turns an error from reading the online list into an
// observation: read errors keep their status, parse errors are failed.
func errOrMalformed(id string, src obs.Source, err error) obs.Observation {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return obs.Errored(id, src, err)
	}
	return malformed(id, src, err)
}

// topoFile is one per-CPU topology file and its column.
type topoFile struct {
	file, col string
	required  bool
	list      bool // a CPU list kept as a string
}

var topoFiles = []topoFile{
	{"physical_package_id", "package", true, false},
	{"die_id", "die", false, false},
	{"cluster_id", "cluster", false, false},
	{"core_id", "core_id", true, false},
	{"thread_siblings_list", "thread_siblings", true, true},
}

// topologyTable reads every online CPU's topology directory. Optional
// files (die_id since 5.2, cluster_id since 5.16, cpu_capacity where the
// architecture publishes it) leave their cell out when missing.
func topologyTable(env collect.Env, online []int, onlineErr error, perf, eff obs.Observation) obs.Observation {
	src := obs.File(cpuDir, "")
	src.Detail = "online; cpuN/topology/physical_package_id, die_id, cluster_id, core_id, thread_siblings_list; cpuN/cpu_capacity; /sys/devices/cpu_core|cpu_atom/cpus"
	if onlineErr != nil {
		return errOrMalformed(topoID, src, onlineErr)
	}
	kind := map[int]string{}
	for _, h := range []struct {
		o    obs.Observation
		name string
	}{{perf, "performance"}, {eff, "efficiency"}} {
		if h.o.Status != obs.StatusOK {
			continue
		}
		cpus, _ := collect.ParseCPUList(h.o.Value.(string))
		for _, c := range cpus {
			kind[c] = h.name
		}
	}
	rows := make([]map[string]any, 0, len(online))
	for _, c := range online {
		dir := fmt.Sprintf("%s/cpu%d", cpuDir, c)
		row := map[string]any{"cpu": int64(c)}
		for _, f := range topoFiles {
			s, err := readTrim(env, dir+"/topology/"+f.file)
			if err != nil {
				if !f.required && errors.Is(err, fs.ErrNotExist) {
					continue
				}
				o := obs.Errored(topoID, src, err)
				if o.Status == obs.StatusAbsent {
					o.Note = fmt.Sprintf("cpu%d has no topology/%s (%s)", c, f.file, o.Note)
				}
				return o
			}
			if f.list {
				cpus, err := collect.ParseCPUList(s)
				if err != nil || !slices.Contains(cpus, c) {
					return malformed(topoID, src, fmt.Errorf("cpu%d/topology/%s: %q does not list the CPU itself", c, f.file, clip(s)))
				}
				row[f.col] = s
				continue
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return malformed(topoID, src, fmt.Errorf("cpu%d/topology/%s: %q is not an integer", c, f.file, clip(s)))
			}
			row[f.col] = n
		}
		if s, err := readTrim(env, dir+"/cpu_capacity"); err == nil {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n < 0 {
				return malformed(topoID, src, fmt.Errorf("cpu%d/cpu_capacity: %q is not a capacity", c, clip(s)))
			}
			row["capacity"] = n
		} else if !errors.Is(err, fs.ErrNotExist) {
			return obs.Errored(topoID, src, err)
		}
		if k, ok := kind[c]; ok {
			row["core_type"] = k
		}
		rows = append(rows, row)
	}
	return obs.OK(topoID, rows, src)
}

// cacheKey identifies one cache: the CPUs that share it decide which copy
// it is.
type cacheKey struct {
	level  int64
	typ    string
	shared string
}

// cacheDomains lists every distinct cache of the online CPUs once. A CPU
// without a cache directory contributes nothing; if none has one, the id is
// absent.
func cacheDomains(env collect.Env, online []int, onlineErr error) obs.Observation {
	src := obs.File(cpuDir, "")
	src.Detail = "cpu*/cache/index*/level, type, size, shared_cpu_list; one row per distinct cache"
	if onlineErr != nil {
		return errOrMalformed(cachesID, src, onlineErr)
	}
	seen := map[cacheKey]bool{}
	var rows []map[string]any
	var firsts []int
	for _, c := range online {
		dir := fmt.Sprintf("%s/cpu%d/cache", cpuDir, c)
		names, err := env.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return obs.Errored(cachesID, src, err)
		}
		for _, n := range names {
			if !strings.HasPrefix(n, "index") {
				continue
			}
			row, first, err := readCache(env, dir+"/"+n)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				var pe *fs.PathError
				if errors.As(err, &pe) {
					return obs.Errored(cachesID, src, err)
				}
				return malformed(cachesID, src, err)
			}
			k := cacheKey{row["level"].(int64), row["type"].(string), row["shared_cpu_list"].(string)}
			if seen[k] {
				continue
			}
			seen[k] = true
			rows = append(rows, row)
			firsts = append(firsts, first)
		}
	}
	if len(rows) == 0 {
		return obs.Absent(cachesID, src, "no online CPU exports cache descriptions (common in virtual machines and on arm64 firmware without cache tables)")
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		ra, rb := rows[a], rows[b]
		if d := ra["level"].(int64) - rb["level"].(int64); d != 0 {
			return int(d)
		}
		if d := strings.Compare(ra["type"].(string), rb["type"].(string)); d != 0 {
			return d
		}
		return firsts[a] - firsts[b]
	})
	sorted := make([]map[string]any, len(rows))
	for i, j := range idx {
		sorted[i] = rows[j]
	}
	return obs.OK(cachesID, sorted, src)
}

// readCache reads one cache index directory. level, type and
// shared_cpu_list are required; size may be missing.
func readCache(env collect.Env, dir string) (map[string]any, int, error) {
	row := map[string]any{}
	lv, err := readTrim(env, dir+"/level")
	if err != nil {
		return nil, 0, err
	}
	level, err := strconv.ParseInt(lv, 10, 64)
	if err != nil || level < 1 || level > 9 {
		return nil, 0, fmt.Errorf("%s/level: %q is not a cache level", dir, clip(lv))
	}
	row["level"] = level
	typ, err := readTrim(env, dir+"/type")
	if err != nil {
		return nil, 0, err
	}
	switch typ {
	case "Data", "Instruction", "Unified":
	default:
		return nil, 0, fmt.Errorf("%s/type: unknown cache type %q", dir, clip(typ))
	}
	row["type"] = typ
	shared, err := readTrim(env, dir+"/shared_cpu_list")
	if err != nil {
		return nil, 0, err
	}
	cpus, err := collect.ParseCPUList(shared)
	if err != nil || len(cpus) == 0 {
		return nil, 0, fmt.Errorf("%s/shared_cpu_list: malformed %q", dir, clip(shared))
	}
	row["shared_cpu_list"] = shared
	row["cpus"] = int64(len(cpus))
	if s, err := readTrim(env, dir+"/size"); err == nil {
		n, err := collect.ParseSize(s)
		if err != nil {
			return nil, 0, fmt.Errorf("%s/size: %w", dir, err)
		}
		row["size_bytes"] = n
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, 0, err
	}
	return row, cpus[0], nil
}

var smtControls = []string{"on", "off", "forceoff", "notsupported", "notimplemented"}

func smtObs(env collect.Env) []obs.Observation {
	const noSMT = "no /sys/devices/system/cpu/smt: the kernel predates SMT control (Linux 4.19) or was built without CPU hotplug"
	src := obs.File(smtDir+"/control", "")
	var out []obs.Observation
	if s, err := readTrim(env, src.Path); err != nil {
		out = append(out, unavailable(smtCtrlID, src, err, noSMT))
	} else if !slices.Contains(smtControls, s) {
		out = append(out, malformed(smtCtrlID, src, fmt.Errorf("unknown SMT control %q", clip(s))))
	} else {
		out = append(out, obs.OK(smtCtrlID, s, src))
	}
	src = obs.File(smtDir+"/active", "")
	if s, err := readTrim(env, src.Path); err != nil {
		out = append(out, unavailable(smtActiveID, src, err, noSMT))
	} else if b, err := parseBool01(s); err != nil {
		out = append(out, malformed(smtActiveID, src, err))
	} else {
		out = append(out, obs.OK(smtActiveID, b, src))
	}
	return out
}
