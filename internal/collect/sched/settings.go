package sched

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// schedSysctls are the /proc/sys/kernel scheduler tunables that are
// world-readable, with what a missing file means.
var schedSysctls = []struct{ name, absent string }{
	{"sched_autogroup_enabled", "no sched_autogroup_enabled: the kernel was built without CONFIG_SCHED_AUTOGROUP"},
	{"sched_rt_runtime_us", ""},
	{"sched_rt_period_us", ""},
	{"sched_rr_timeslice_ms", ""},
	{"sched_util_clamp_min", "no sched_util_clamp_min: the kernel predates utilization clamping (Linux 5.3) or was built without CONFIG_UCLAMP_TASK"},
	{"sched_util_clamp_max", "no sched_util_clamp_max: the kernel predates utilization clamping (Linux 5.3) or was built without CONFIG_UCLAMP_TASK"},
	{"sched_cfs_bandwidth_slice_us", "no sched_cfs_bandwidth_slice_us: the kernel was built without CONFIG_CFS_BANDWIDTH, so cgroup CPU quotas are not available"},
}

const extStatePath = "/sys/kernel/sched_ext/state"

var extStates = []string{"disabled", "enabling", "enabled", "disabling"}

func settingsCollector() collect.Collector {
	var owns []string
	for _, s := range schedSysctls {
		owns = append(owns, "sched.sysctl.kernel."+s.name)
	}
	owns = append(owns, "sched.ext.state")
	return collector{
		id:   "sched.settings",
		owns: owns,
		read: func(env collect.Env) []obs.Observation {
			var out []obs.Observation
			for _, s := range schedSysctls {
				id := "sched.sysctl.kernel." + s.name
				src := obs.File("/proc/sys/kernel/"+s.name, "")
				v, err := readTrim(env, src.Path)
				if err != nil {
					out = append(out, unavailable(id, src, err, s.absent))
					continue
				}
				n, err := parseInt(v)
				if err != nil {
					out = append(out, malformed(id, src, err))
					continue
				}
				out = append(out, obs.OK(id, n, src))
			}
			out = append(out, choiceFile(env, "sched.ext.state", extStatePath, extStates,
				"no sched_ext directory: the kernel predates the extensible scheduler class (Linux 6.12) or was built without CONFIG_SCHED_CLASS_EXT"))
			return out
		},
	}
}

// ------------------------------------------------------------- atlas itself

const (
	selfStatus    = "/proc/self/status"
	selfStat      = "/proc/self/stat"
	selfSchedstat = "/proc/self/schedstat"
	selfSchedPath = "/proc/self/sched"
)

// policyNames maps the policy field of /proc/<pid>/stat to its name.
var policyNames = map[int64]string{
	0: "SCHED_OTHER", 1: "SCHED_FIFO", 2: "SCHED_RR", 3: "SCHED_BATCH",
	5: "SCHED_IDLE", 6: "SCHED_DEADLINE", 7: "SCHED_EXT",
}

// statSched is the part of /proc/self/stat the lessons use.
type statSched struct {
	nice   int64
	policy string
}

// parseSelfStat parses /proc/<pid>/stat. The command name, in parentheses,
// may contain spaces and parentheses itself, so fields are counted from the
// last ")". nice is field 19 and policy field 41.
func parseSelfStat(data []byte) (statSched, error) {
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return statSched{}, fmt.Errorf("no \")\" closing the command name")
	}
	f := strings.Fields(s[i+1:])
	// f[0] is field 3 (state), so field n is f[n-3].
	if len(f) < 41-2 {
		return statSched{}, fmt.Errorf("%d fields after the command name, want at least 39", len(f))
	}
	nice, err := strconv.ParseInt(f[19-3], 10, 64)
	if err != nil || nice < -20 || nice > 19 {
		return statSched{}, fmt.Errorf("field 19 (nice) %q is not a nice value", clip(f[19-3]))
	}
	p, err := strconv.ParseInt(f[41-3], 10, 64)
	name, known := policyNames[p]
	if err != nil || !known {
		return statSched{}, fmt.Errorf("field 41 (policy) %q is not a scheduling policy", clip(f[41-3]))
	}
	return statSched{nice: nice, policy: name}, nil
}

// parseSelfSchedstat parses /proc/<pid>/schedstat: "run wait timeslices",
// times in nanoseconds.
func parseSelfSchedstat(data []byte) ([3]int64, error) {
	var out [3]int64
	f := strings.Fields(string(data))
	if len(f) != 3 {
		return out, fmt.Errorf("expected 3 fields, got %d", len(f))
	}
	for i := range f {
		n, err := strconv.ParseInt(f[i], 10, 64)
		if err != nil || n < 0 {
			return out, fmt.Errorf("field %d: %q is not a count", i+1, clip(f[i]))
		}
		out[i] = n
	}
	return out, nil
}

// parseSelfSched parses the "key : value" lines of /proc/<pid>/sched into a
// map; the header line and anything without " : " are skipped.
func parseSelfSched(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.Contains(k, "(") {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" || strings.ContainsAny(k, " \t") && !strings.HasPrefix(k, "effective ") {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	if _, ok := out["se.load.weight"]; !ok {
		return nil, fmt.Errorf("no se.load.weight line")
	}
	return out, nil
}

// schedNanos parses a /proc/<pid>/sched duration. The kernel prints some
// as integer nanoseconds (se.slice) and some as milliseconds with six
// decimals (se.vruntime); both are returned in nanoseconds.
func schedNanos(s string) (int64, error) {
	if whole, frac, ok := strings.Cut(s, "."); ok {
		if len(frac) != 6 {
			return 0, fmt.Errorf("%q is not milliseconds with six decimals", clip(s))
		}
		w, err1 := strconv.ParseInt(whole, 10, 64)
		f, err2 := strconv.ParseInt(frac, 10, 64)
		if err1 != nil || err2 != nil || w < 0 {
			return 0, fmt.Errorf("%q is not a duration", clip(s))
		}
		return w*1_000_000 + f, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a duration", clip(s))
	}
	return n, nil
}

func selfCollector() collect.Collector {
	statusIDs := []string{"sched.self.voluntary_ctxt_switches", "sched.self.nonvoluntary_ctxt_switches", "sched.self.cpus_allowed_list"}
	statIDs := []string{"sched.self.policy", "sched.self.nice"}
	schedstatIDs := []string{"sched.self.run_us", "sched.self.wait_us", "sched.self.timeslices"}
	schedIDs := []string{"sched.self.slice_us", "sched.self.load_weight"}
	var owns []string
	for _, g := range [][]string{statusIDs, statIDs, schedstatIDs, schedIDs} {
		owns = append(owns, g...)
	}
	return collector{
		id:   "sched.self",
		owns: owns,
		read: func(env collect.Env) []obs.Observation {
			var out []obs.Observation
			out = append(out, selfStatusObs(env, statusIDs)...)

			src := obs.File(selfStat, "")
			if data, err := env.ReadFile(selfStat); err != nil {
				out = append(out, unavailableAll(statIDs, src, err, "")...)
			} else if st, err := parseSelfStat(data); err != nil {
				out = append(out, malformedAll(statIDs, src, err)...)
			} else {
				out = append(out, obs.OK(statIDs[0], st.policy, obs.File(selfStat, "field 41 (policy)")), obs.OK(statIDs[1], st.nice, obs.File(selfStat, "field 19 (nice)")))
			}

			src = obs.File(selfSchedstat, "")
			if data, err := env.ReadFile(selfSchedstat); err != nil {
				out = append(out, unavailableAll(schedstatIDs, src, err, "no /proc/self/schedstat: the kernel was built without CONFIG_SCHED_INFO")...)
			} else if v, err := parseSelfSchedstat(data); err != nil {
				out = append(out, malformedAll(schedstatIDs, src, err)...)
			} else {
				for i, id := range schedstatIDs {
					s := obs.File(selfSchedstat, fmt.Sprintf("field %d", i+1))
					val := v[i]
					if i < 2 {
						s.Detail = "converted from nanoseconds"
						val /= 1000
					}
					out = append(out, obs.OK(id, val, s))
				}
			}
			return append(out, selfSchedObs(env, schedIDs)...)
		},
	}
}

func selfStatusObs(env collect.Env, ids []string) []obs.Observation {
	src := obs.File(selfStatus, "")
	data, err := env.ReadFile(selfStatus)
	if err != nil {
		return unavailableAll(ids, src, err, "")
	}
	fields, err := collect.ParseFields(data)
	if err != nil {
		return malformedAll(ids, src, err)
	}
	get := func(k string) (string, bool) {
		for _, f := range fields {
			if f.Key == k {
				return f.Value, true
			}
		}
		return "", false
	}
	var out []obs.Observation
	for i, key := range []string{"voluntary_ctxt_switches", "nonvoluntary_ctxt_switches", "Cpus_allowed_list"} {
		s := obs.File(selfStatus, key)
		v, ok := get(key)
		switch {
		case !ok:
			out = append(out, obs.Absent(ids[i], s, "this kernel's /proc/self/status has no "+key+" line"))
		case i == 2:
			if _, err := collect.ParseCPUList(v); err != nil {
				out = append(out, malformed(ids[i], s, err))
			} else {
				out = append(out, obs.OK(ids[i], v, s))
			}
		default:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				out = append(out, malformed(ids[i], s, fmt.Errorf("%s: %q is not a count", key, clip(v))))
			} else {
				out = append(out, obs.OK(ids[i], n, s))
			}
		}
	}
	return out
}

func selfSchedObs(env collect.Env, ids []string) []obs.Observation {
	src := obs.File(selfSchedPath, "")
	data, err := env.ReadFile(selfSchedPath)
	if err != nil {
		return unavailableAll(ids, src, err, "no /proc/self/sched: the kernel was built without CONFIG_SCHED_DEBUG, which Linux 6.14 made unconditional")
	}
	kv, err := parseSelfSched(data)
	if err != nil {
		return malformedAll(ids, src, err)
	}
	var out []obs.Observation
	s := obs.File(selfSchedPath, "se.slice")
	if v, ok := kv["se.slice"]; !ok {
		out = append(out, obs.Absent(ids[0], s, "this kernel's /proc/self/sched has no se.slice line: CFS kernels (before Linux 6.6) have no per-task slice, and early EEVDF kernels do not print it"))
	} else if ns, err := schedNanos(v); err != nil {
		out = append(out, malformed(ids[0], s, err))
	} else {
		s.Detail = "converted from nanoseconds"
		out = append(out, obs.OK(ids[0], ns/1000, s))
	}
	s = obs.File(selfSchedPath, "se.load.weight")
	if w, err := parseInt(kv["se.load.weight"]); err != nil || w <= 0 {
		out = append(out, malformed(ids[1], s, fmt.Errorf("se.load.weight %q is not a weight", clip(kv["se.load.weight"]))))
	} else {
		out = append(out, obs.OK(ids[1], w, s))
	}
	return out
}
