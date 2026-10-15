package sched

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	loadavgPath   = "/proc/loadavg"
	statPath      = "/proc/stat"
	schedstatPath = "/proc/schedstat"
	pressurePath  = "/proc/pressure/cpu"
	cpuTimesID    = "sched.stat.cpu_times"
)

// loadavg is /proc/loadavg: three averages, runnable/total entities and the
// last pid (not kept: it says nothing about scheduling).
type loadavg struct {
	avg             [3]float64
	runnable, total int64
}

// parseLoadavg parses "0.31 0.42 0.40 1/236 9132".
func parseLoadavg(data []byte) (loadavg, error) {
	var l loadavg
	f := strings.Fields(string(data))
	if len(f) != 5 {
		return l, fmt.Errorf("expected 5 fields, got %d", len(f))
	}
	for i := range 3 {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil || !(v >= 0 && v < 1e7) {
			return l, fmt.Errorf("field %d: %q is not a load average", i+1, clip(f[i]))
		}
		l.avg[i] = v
	}
	r, t, ok := strings.Cut(f[3], "/")
	var err1, err2 error
	l.runnable, err1 = strconv.ParseInt(r, 10, 64)
	l.total, err2 = strconv.ParseInt(t, 10, 64)
	if !ok || err1 != nil || err2 != nil || l.runnable < 0 || l.total < l.runnable {
		return l, fmt.Errorf("field 4: %q is not runnable/total", clip(f[3]))
	}
	return l, nil
}

// cpuTimeCols are the /proc/stat cpu columns in kernel order.
var cpuTimeCols = []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal", "guest", "guest_nice"}

// procStat is the part of /proc/stat the scheduler lessons use.
type procStat struct {
	times  []map[string]any // "all" first, then cpuN in file order
	scalar map[string]int64 // ctxt, processes, procs_running, procs_blocked
}

var statScalars = []string{"ctxt", "processes", "procs_running", "procs_blocked"}

// parseProcStat parses the cpu lines and the scalar counters of /proc/stat.
// Kernels before 2.6.33 print fewer cpu columns; those cells are left out.
func parseProcStat(data []byte) (procStat, error) {
	ps := procStat{scalar: map[string]int64{}}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		key := f[0]
		switch {
		case key == "cpu" || (strings.HasPrefix(key, "cpu") && isDigits(key[3:])):
			if len(f) < 5 || len(f) > 1+len(cpuTimeCols) {
				return ps, fmt.Errorf("line %d: %s has %d values, want 4 to %d", i+1, key, len(f)-1, len(cpuTimeCols))
			}
			name := key
			if key == "cpu" {
				name = "all"
				if len(ps.times) > 0 {
					return ps, fmt.Errorf("line %d: a second aggregate cpu line", i+1)
				}
			} else if len(ps.times) == 0 {
				return ps, fmt.Errorf("line %d: %s before the aggregate cpu line", i+1, key)
			}
			row := map[string]any{"cpu": name}
			for j, v := range f[1:] {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil || n < 0 {
					return ps, fmt.Errorf("line %d: %s %s: %q is not a tick count", i+1, key, cpuTimeCols[j], clip(v))
				}
				row[cpuTimeCols[j]] = n
			}
			ps.times = append(ps.times, row)
		case len(f) == 2:
			for _, s := range statScalars {
				if key == s {
					n, err := strconv.ParseInt(f[1], 10, 64)
					if err != nil || n < 0 {
						return ps, fmt.Errorf("line %d: %s: %q is not a count", i+1, key, clip(f[1]))
					}
					ps.scalar[key] = n
				}
			}
		}
	}
	if len(ps.times) == 0 {
		return ps, errors.New("no cpu line")
	}
	return ps, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func loadCollector() collect.Collector {
	avgIDs := []string{"sched.loadavg.avg1", "sched.loadavg.avg5", "sched.loadavg.avg15"}
	lavIDs := append(append([]string{}, avgIDs...), "sched.loadavg.runnable", "sched.loadavg.entities")
	statIDs := []string{cpuTimesID}
	for _, s := range statScalars {
		statIDs = append(statIDs, "sched.stat."+s)
	}
	return collector{
		id:   "sched.load",
		owns: append(append([]string{}, lavIDs...), statIDs...),
		read: func(env collect.Env) []obs.Observation {
			var out []obs.Observation
			src := obs.File(loadavgPath, "")
			if data, err := env.ReadFile(loadavgPath); err != nil {
				out = append(out, unavailableAll(lavIDs, src, err, "")...)
			} else if l, err := parseLoadavg(data); err != nil {
				out = append(out, malformedAll(lavIDs, src, err)...)
			} else {
				for i, id := range avgIDs {
					s := src
					s.Field = []string{"1 minute", "5 minutes", "15 minutes"}[i]
					out = append(out, obs.OK(id, l.avg[i], s))
				}
				s := src
				s.Field = "runnable/entities"
				out = append(out, obs.OK("sched.loadavg.runnable", l.runnable, s), obs.OK("sched.loadavg.entities", l.total, s))
			}

			src = obs.File(statPath, "")
			data, err := env.ReadFile(statPath)
			if err != nil {
				return append(out, unavailableAll(statIDs, src, err, "")...)
			}
			ps, err := parseProcStat(data)
			if err != nil {
				return append(out, malformedAll(statIDs, src, err)...)
			}
			ts := src
			ts.Field, ts.Detail = "cpu lines", "USER_HZ ticks (1/100 s); row all is the cpu line, the others cpuN"
			out = append(out, obs.OK(cpuTimesID, ps.times, ts))
			for _, s := range statScalars {
				fs := obs.File(statPath, s)
				if n, ok := ps.scalar[s]; ok {
					out = append(out, obs.OK("sched.stat."+s, n, fs))
				} else {
					out = append(out, obs.Absent("sched.stat."+s, fs, "this kernel's /proc/stat has no "+s+" line"))
				}
			}
			return out
		},
	}
}

// schedstat is the per-CPU part of /proc/schedstat.
type schedstat struct {
	version int64
	cpus    []map[string]any
}

// parseSchedstat parses /proc/schedstat versions 15 to 17. Each "cpuN"
// line has nine numbers; the last three are the time tasks ran on the CPU
// and waited for it (nanoseconds) and the number of timeslices. Domain
// lines are skipped.
func parseSchedstat(data []byte) (schedstat, error) {
	var s schedstat
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case f[0] == "version" && len(f) == 2:
			v, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				return s, fmt.Errorf("line %d: version %q", i+1, clip(f[1]))
			}
			if v < 15 || v > 17 {
				return s, fmt.Errorf("version %d: only versions 15 to 17 are understood", v)
			}
			s.version = v
		case strings.HasPrefix(f[0], "cpu") && isDigits(f[0][3:]):
			if s.version == 0 {
				return s, fmt.Errorf("line %d: cpu line before the version line", i+1)
			}
			if len(f) != 10 {
				return s, fmt.Errorf("line %d: %s has %d values, want 9", i+1, f[0], len(f)-1)
			}
			cpu, _ := strconv.ParseInt(f[0][3:], 10, 64)
			row := map[string]any{"cpu": cpu}
			for j, col := range []string{"run_ns", "wait_ns", "timeslices"} {
				n, err := strconv.ParseInt(f[7+j], 10, 64)
				if err != nil || n < 0 {
					return s, fmt.Errorf("line %d: %s %s: %q is not a count", i+1, f[0], col, clip(f[7+j]))
				}
				row[col] = n
			}
			s.cpus = append(s.cpus, row)
		}
	}
	if s.version == 0 {
		return s, errors.New("no version line")
	}
	if len(s.cpus) == 0 {
		return s, errors.New("no cpu line")
	}
	return s, nil
}

func schedstatCollector() collect.Collector {
	const verID, cpusID = "sched.schedstat.version", "sched.schedstat.cpus"
	return collector{
		id:   "sched.schedstat",
		owns: []string{verID, cpusID},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(schedstatPath, "")
			data, err := env.ReadFile(schedstatPath)
			if err != nil {
				return unavailableAll([]string{verID, cpusID}, src, err, "no /proc/schedstat: the kernel was built without CONFIG_SCHEDSTATS and CONFIG_SCHED_INFO")
			}
			s, err := parseSchedstat(data)
			if err != nil {
				return malformedAll([]string{verID, cpusID}, src, err)
			}
			cs := src
			cs.Field, cs.Detail = "cpuN fields 7 to 9", "rq_cpu_time, run_delay (nanoseconds) and pcount of each cpu line"
			return []obs.Observation{obs.OK(verID, s.version, obs.File(schedstatPath, "version")), obs.OK(cpusID, s.cpus, cs)}
		},
	}
}

// psiLine is one line of a pressure file.
type psiLine struct {
	avg   [3]float64
	total int64
}

// parsePSI parses /proc/pressure/cpu. The some line is required; the full
// line appeared in Linux 5.13 and is optional.
func parsePSI(data []byte) (map[string]psiLine, error) {
	out := map[string]psiLine{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		kind := f[0]
		if kind != "some" && kind != "full" {
			return nil, fmt.Errorf("line %d: expected some or full, got %q", i+1, clip(kind))
		}
		if _, dup := out[kind]; dup {
			return nil, fmt.Errorf("line %q appears twice", kind)
		}
		kv := map[string]string{}
		for _, pair := range f[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("line %d: expected key=value, got %q", i+1, clip(pair))
			}
			kv[k] = v
		}
		var l psiLine
		for j, key := range []string{"avg10", "avg60", "avg300"} {
			v, err := strconv.ParseFloat(kv[key], 64)
			if err != nil || !(v >= 0 && v <= 100) {
				return nil, fmt.Errorf("%s %s: not a percentage: %q", kind, key, clip(kv[key]))
			}
			l.avg[j] = v
		}
		t, err := strconv.ParseInt(kv["total"], 10, 64)
		if err != nil || t < 0 {
			return nil, fmt.Errorf("%s total: not a duration in microseconds: %q", kind, clip(kv["total"]))
		}
		l.total = t
		out[kind] = l
	}
	if _, ok := out["some"]; !ok {
		return nil, errors.New("no some line")
	}
	return out, nil
}

var psiFields = []string{"avg10", "avg60", "avg300", "total_us"}

func psiCollector() collect.Collector {
	var ids []string
	for _, kind := range []string{"some", "full"} {
		for _, f := range psiFields {
			ids = append(ids, "sched.psi."+kind+"."+f)
		}
	}
	return collector{
		id:   "sched.psi",
		owns: ids,
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(pressurePath, "")
			data, err := env.ReadFile(pressurePath)
			if err != nil {
				out := unavailableAll(ids, src, err, "no /proc/pressure/cpu: the kernel is older than 4.20 or was built without pressure stall information (CONFIG_PSI)")
				for i := range out {
					if out[i].Status == obs.StatusUnsupported {
						out[i].Note = "pressure stall information is built in but disabled (psi=0 on the kernel command line, or CONFIG_PSI_DEFAULT_DISABLED): " + out[i].Note
					}
				}
				return out
			}
			lines, err := parsePSI(data)
			if err != nil {
				return malformedAll(ids, src, err)
			}
			var out []obs.Observation
			for _, kind := range []string{"some", "full"} {
				l, ok := lines[kind]
				for j, f := range psiFields {
					id := "sched.psi." + kind + "." + f
					raw := strings.TrimSuffix(f, "_us")
					s := obs.File(pressurePath, kind+" "+raw)
					switch {
					case !ok:
						out = append(out, obs.Absent(id, s, "this kernel's /proc/pressure/cpu has no full line (added in Linux 5.13)"))
					case j < 3:
						out = append(out, obs.OK(id, l.avg[j], s))
					default:
						out = append(out, obs.OK(id, l.total, s))
					}
				}
			}
			return out
		},
	}
}
