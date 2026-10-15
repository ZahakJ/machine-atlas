package sched

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// The cpu controller of atlas's own cgroup. Locating the cgroup follows
// internal/collect/memory/cgroup.go: /proc/self/cgroup names it, and
// /proc/self/mountinfo says where its hierarchy is mounted. Unlike memory,
// the v1 cpu controller's quota files mean exactly what cpu.max means on
// v2, so the same ids report them, and the source names the v1 file.

const (
	procCgroupPath = "/proc/self/cgroup"
	mountinfoPath  = "/proc/self/mountinfo"
	cg             = "sched.cgroup."
)

var (
	cgQuotaIDs  = []string{cg + "cpu.max_quota_us", cg + "cpu.max_period_us"}
	cgWeightID  = cg + "cpu.weight"
	cgSharesID  = cg + "cpu.shares_v1"
	cgStatKeys  = []string{"usage_usec", "user_usec", "system_usec", "nr_periods", "nr_throttled", "throttled_usec"}
	cgStatIDs   = []string{cg + "cpu.usage_us", cg + "cpu.user_us", cg + "cpu.system_us", cg + "cpu.nr_periods", cg + "cpu.nr_throttled", cg + "cpu.throttled_us"}
	cgCpusetID  = cg + "cpuset.cpus_effective"
	cgAllCPUIDs = slices.Concat(cgQuotaIDs, []string{cgWeightID, cgSharesID}, cgStatIDs)
)

// cgLine is one line of /proc/self/cgroup.
type cgLine struct {
	hierarchy   int
	controllers []string
	path        string
}

func parseProcCgroup(data []byte) ([]cgLine, error) {
	var out []cgLine
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], "/") {
			return nil, fmt.Errorf("line %d: expected \"id:controllers:/path\", got %q", i+1, clip(line))
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil || id < 0 {
			return nil, fmt.Errorf("line %d: bad hierarchy id %q", i+1, clip(parts[0]))
		}
		l := cgLine{hierarchy: id, path: parts[2]}
		if parts[1] != "" {
			l.controllers = strings.Split(parts[1], ",")
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, errors.New("no cgroup lines")
	}
	return out, nil
}

// cgMount is the part of a mountinfo line needed to find a hierarchy.
type cgMount struct {
	root, point, fstype string
	options             []string // super options
}

func parseMountinfo(data []byte) ([]cgMount, error) {
	var out []cgMount
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		sep := slices.Index(f, "-")
		if sep < 6 || sep+1 >= len(f) {
			return nil, fmt.Errorf("line %d: no \" - fstype\" separator in %q", i+1, clip(line))
		}
		m := cgMount{root: unescapeOctal(f[3]), point: unescapeOctal(f[4]), fstype: f[sep+1]}
		if sep+3 < len(f) {
			m.options = strings.Split(f[sep+3], ",")
		}
		out = append(out, m)
	}
	return out, nil
}

func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// hierarchy is where one controller's view of atlas's cgroup lives.
type hierarchy struct {
	v1        bool
	path      string // the cgroup path from /proc/self/cgroup
	point     string // the mount point of the hierarchy
	dir       string // the directory, when a mount shows it
	mountNote string // why dir is empty
}

// findHierarchy returns the hierarchy carrying controller: a v1 hierarchy
// that lists it, otherwise the v2 one. ok is false when neither exists.
func findHierarchy(lines []cgLine, mounts []cgMount, controller string) (h hierarchy, ok bool) {
	var v2 *cgLine
	for i := range lines {
		l := &lines[i]
		if l.hierarchy == 0 {
			v2 = l
			continue
		}
		if slices.Contains(l.controllers, controller) {
			h = hierarchy{v1: true, path: l.path}
			ok = true
			break
		}
	}
	if !ok {
		if v2 == nil {
			return h, false
		}
		h = hierarchy{path: v2.path}
	}
	for _, m := range mounts {
		if h.v1 && (m.fstype != "cgroup" || !slices.Contains(m.options, controller)) {
			continue
		}
		if !h.v1 && m.fstype != "cgroup2" {
			continue
		}
		rel, inside := h.path, true
		if m.root != "/" {
			rel, inside = strings.CutPrefix(h.path, m.root)
		}
		if inside && (rel == "" || strings.HasPrefix(rel, "/")) {
			h.point, h.dir = m.point, path.Join(m.point, rel)
			return h, true
		}
	}
	h.mountNote = "no cgroup mount in this mount namespace shows this process's cgroup (typical of a container without its own cgroup namespace)"
	return h, true
}

func cgroupCollector() collect.Collector {
	return collector{
		id:   "sched.cgroup",
		owns: append(slices.Clone(cgAllCPUIDs), cgCpusetID),
		read: func(env collect.Env) []obs.Observation {
			all := append(slices.Clone(cgAllCPUIDs), cgCpusetID)
			src := obs.File(procCgroupPath, "")
			data, err := env.ReadFile(procCgroupPath)
			if err != nil {
				return unavailableAll(all, src, err, "no /proc/self/cgroup: the kernel was built without control groups (CONFIG_CGROUPS)")
			}
			lines, err := parseProcCgroup(data)
			if err != nil {
				return malformedAll(all, src, err)
			}
			msrc := obs.File(mountinfoPath, "")
			mdata, err := env.ReadFile(mountinfoPath)
			if err != nil {
				return unavailableAll(all, msrc, err, "")
			}
			mounts, err := parseMountinfo(mdata)
			if err != nil {
				return malformedAll(all, msrc, err)
			}
			out := cpuController(env, lines, mounts)
			return append(out, cpusetObs(env, lines, mounts))
		},
	}
}

// cgReader reads files of the cgroup at h, with sources that name the
// mount point and a placeholder for the cgroup itself: the path can
// identify a user (mem.cgroup.path carries it, redacted on export).
type cgReader struct {
	env collect.Env
	h   hierarchy
}

func (r cgReader) source(file string) obs.Source {
	s := obs.File(path.Join(r.h.point, "<cgroup>", file), "")
	s.Detail = "<cgroup> is this process's cgroup, mem.cgroup.path"
	return s
}

func (r cgReader) read(id, file, missing string) (string, obs.Observation, bool) {
	s, err := readTrim(r.env, path.Join(r.h.dir, file))
	if err == nil {
		return s, obs.Observation{}, true
	}
	o := obs.Errored(id, r.source(file), err)
	o.Note = errnoText(err)
	if o.Status == obs.StatusAbsent && missing != "" {
		o.Note = missing + " (" + o.Note + ")"
	}
	return "", o, false
}

func noController(controller string) string {
	return "no such file: the " + controller + " controller is not enabled for this cgroup (see the parent's cgroup.subtree_control), so nothing is limited at this level"
}

func cpuController(env collect.Env, lines []cgLine, mounts []cgMount) []obs.Observation {
	h, ok := findHierarchy(lines, mounts, "cpu")
	if !ok {
		out := make([]obs.Observation, len(cgAllCPUIDs))
		for i, id := range cgAllCPUIDs {
			out[i] = obs.Absent(id, obs.File(procCgroupPath, ""), "no cgroup hierarchy carries the cpu controller")
		}
		return out
	}
	if h.dir == "" {
		out := make([]obs.Observation, len(cgAllCPUIDs))
		for i, id := range cgAllCPUIDs {
			out[i] = obs.Absent(id, obs.File(mountinfoPath, ""), h.mountNote)
		}
		return out
	}
	r := cgReader{env, h}
	if h.v1 {
		return cpuV1(r)
	}
	return cpuV2(r)
}

// parseCPUMax parses cpu.max: "max 100000" or "150000 100000".
func parseCPUMax(s string) (quota any, period int64, err error) {
	f := strings.Fields(s)
	if len(f) != 2 {
		return nil, 0, fmt.Errorf("expected \"quota period\", got %q", clip(s))
	}
	period, err = strconv.ParseInt(f[1], 10, 64)
	if err != nil || period <= 0 {
		return nil, 0, fmt.Errorf("period %q is not a positive number of microseconds", clip(f[1]))
	}
	if f[0] == "max" {
		return "max", period, nil
	}
	q, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || q <= 0 {
		return nil, 0, fmt.Errorf("quota %q is neither max nor a positive number of microseconds", clip(f[0]))
	}
	return q, period, nil
}

// parseFlatKeyed parses a "key value" file such as cpu.stat.
func parseFlatKeyed(s string) (map[string]int64, error) {
	out := map[string]int64{}
	for i, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 2 {
			return nil, fmt.Errorf("line %d: expected \"key value\", got %q", i+1, clip(line))
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("line %d: %s: not a count: %q", i+1, f[0], clip(f[1]))
		}
		out[f[0]] = n
	}
	return out, nil
}

func cpuV2(r cgReader) []obs.Observation {
	var out []obs.Observation
	missing := noController("cpu")
	if r.h.path == "/" {
		missing = "atlas runs in the root cgroup, which has no cpu.max or cpu.weight: those exist only for child cgroups"
	}
	if s, failed, ok := r.read(cgQuotaIDs[0], "cpu.max", missing); !ok {
		f2 := failed
		f2.ID = cgQuotaIDs[1]
		out = append(out, failed, f2)
	} else if q, p, err := parseCPUMax(s); err != nil {
		out = append(out, malformed(cgQuotaIDs[0], r.source("cpu.max"), err), malformed(cgQuotaIDs[1], r.source("cpu.max"), err))
	} else {
		qs, ps := r.source("cpu.max"), r.source("cpu.max")
		qs.Field, ps.Field = "quota", "period"
		if q == "max" {
			qs.Detail += "; \"max\" means no quota"
		}
		out = append(out, obs.OK(cgQuotaIDs[0], q, qs), obs.OK(cgQuotaIDs[1], p, ps))
	}
	if s, failed, ok := r.read(cgWeightID, "cpu.weight", missing); !ok {
		out = append(out, failed)
	} else if w, err := parseInt(s); err != nil || w < 1 || w > 10000 {
		out = append(out, malformed(cgWeightID, r.source("cpu.weight"), fmt.Errorf("%q is not a weight from 1 to 10000", clip(s))))
	} else {
		out = append(out, obs.OK(cgWeightID, w, r.source("cpu.weight")))
	}
	out = append(out, obs.Unsupported(cgSharesID, r.source("cpu.shares"), "cgroup v2: cpu.shares is a v1 file, replaced by cpu.weight"))

	s, failed, ok := r.read(cgStatIDs[0], "cpu.stat", "")
	if !ok {
		for _, id := range cgStatIDs {
			f := failed
			f.ID = id
			out = append(out, f)
		}
		return out
	}
	stat, err := parseFlatKeyed(s)
	for i, key := range cgStatKeys {
		src := r.source("cpu.stat")
		src.Field = key
		switch n, have := stat[key]; {
		case err != nil:
			out = append(out, malformed(cgStatIDs[i], src, err))
		case !have:
			out = append(out, obs.Absent(cgStatIDs[i], src, "this cgroup's cpu.stat has no "+key+" line: the cpu controller is not enabled for it, so only the usage figures are kept"))
		default:
			out = append(out, obs.OK(cgStatIDs[i], n, src))
		}
	}
	return out
}

func cpuV1(r cgReader) []obs.Observation {
	var out []obs.Observation
	q, qf, qok := r.read(cgQuotaIDs[0], "cpu.cfs_quota_us", "no cpu.cfs_quota_us: the kernel was built without CONFIG_CFS_BANDWIDTH")
	p, pf, pok := r.read(cgQuotaIDs[1], "cpu.cfs_period_us", "no cpu.cfs_period_us: the kernel was built without CONFIG_CFS_BANDWIDTH")
	switch {
	case !qok:
		out = append(out, qf)
	default:
		n, err := parseInt(q)
		src := r.source("cpu.cfs_quota_us")
		switch {
		case err != nil || n == 0 || n < -1:
			out = append(out, malformed(cgQuotaIDs[0], src, fmt.Errorf("%q is neither -1 nor a positive number of microseconds", clip(q))))
		case n == -1:
			src.Detail += "; cgroup v1 writes -1 for no quota, reported as \"max\" like cpu.max"
			out = append(out, obs.OK(cgQuotaIDs[0], "max", src))
		default:
			out = append(out, obs.OK(cgQuotaIDs[0], n, src))
		}
	}
	if !pok {
		out = append(out, pf)
	} else if n, err := parseInt(p); err != nil || n <= 0 {
		out = append(out, malformed(cgQuotaIDs[1], r.source("cpu.cfs_period_us"), fmt.Errorf("%q is not a period", clip(p))))
	} else {
		out = append(out, obs.OK(cgQuotaIDs[1], n, r.source("cpu.cfs_period_us")))
	}
	out = append(out, obs.Unsupported(cgWeightID, r.source("cpu.weight"), "cgroup v1: the cpu controller has cpu.shares (sched.cgroup.cpu.shares_v1) instead of cpu.weight"))
	if s, failed, ok := r.read(cgSharesID, "cpu.shares", ""); !ok {
		out = append(out, failed)
	} else if n, err := parseInt(s); err != nil || n < 2 {
		out = append(out, malformed(cgSharesID, r.source("cpu.shares"), fmt.Errorf("%q is not a share count", clip(s))))
	} else {
		out = append(out, obs.OK(cgSharesID, n, r.source("cpu.shares")))
	}

	// Usage comes from the cpuacct controller, usually mounted together
	// with cpu ("cpu,cpuacct"); it counts nanoseconds.
	if s, failed, ok := r.read(cgStatIDs[0], "cpuacct.usage", "no cpuacct.usage: the cpuacct controller is not mounted together with cpu"); !ok {
		out = append(out, failed)
	} else if n, err := parseInt(s); err != nil || n < 0 {
		out = append(out, malformed(cgStatIDs[0], r.source("cpuacct.usage"), fmt.Errorf("%q is not a count of nanoseconds", clip(s))))
	} else {
		src := r.source("cpuacct.usage")
		src.Detail += "; converted from nanoseconds"
		out = append(out, obs.OK(cgStatIDs[0], n/1000, src))
	}
	for _, i := range []int{1, 2} {
		out = append(out, obs.Unsupported(cgStatIDs[i], r.source("cpuacct.stat"), "cgroup v1: user and system time are in cpuacct.stat, in USER_HZ ticks rather than microseconds, and are not converted"))
	}
	s, failed, ok := r.read(cgStatIDs[3], "cpu.stat", "")
	if !ok {
		for _, id := range cgStatIDs[3:] {
			f := failed
			f.ID = id
			out = append(out, f)
		}
		return out
	}
	stat, err := parseFlatKeyed(s)
	for i, key := range []string{"nr_periods", "nr_throttled", "throttled_time"} {
		id := cgStatIDs[3+i]
		src := r.source("cpu.stat")
		src.Field = key
		switch n, have := stat[key]; {
		case err != nil:
			out = append(out, malformed(id, src, err))
		case !have:
			out = append(out, obs.Absent(id, src, "this cgroup's cpu.stat has no "+key+" line"))
		case key == "throttled_time":
			src.Detail += "; converted from nanoseconds"
			out = append(out, obs.OK(id, n/1000, src))
		default:
			out = append(out, obs.OK(id, n, src))
		}
	}
	return out
}

func cpusetObs(env collect.Env, lines []cgLine, mounts []cgMount) obs.Observation {
	h, ok := findHierarchy(lines, mounts, "cpuset")
	switch {
	case !ok:
		return obs.Absent(cgCpusetID, obs.File(procCgroupPath, ""), "no cgroup hierarchy carries the cpuset controller")
	case h.dir == "":
		return obs.Absent(cgCpusetID, obs.File(mountinfoPath, ""), h.mountNote)
	}
	r := cgReader{env, h}
	file, missing := "cpuset.cpus.effective", noController("cpuset")
	if h.v1 {
		file, missing = "cpuset.effective_cpus", "no cpuset.effective_cpus: the kernel predates it (Linux 4.6 for v1)"
	} else if h.path == "/" {
		missing = "atlas runs in the root cgroup, whose cpuset.cpus.effective is every online CPU; the file exists only where the cpuset controller is enabled"
	}
	s, failed, ok := r.read(cgCpusetID, file, missing)
	if !ok {
		return failed
	}
	if _, err := collect.ParseCPUList(s); err != nil {
		return malformed(cgCpusetID, r.source(file), err)
	}
	return obs.OK(cgCpusetID, s, r.source(file))
}
