package derive

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// SchedPrioToWeight is the kernel's sched_prio_to_weight table
// (kernel/sched/core.c): the fair-scheduler weight of nice -20 to 19.
// Each step is about 1.25 times the next, so one nice level moves about
// 10% of the CPU between two competing tasks; nice 0 weighs 1024.
var SchedPrioToWeight = [40]int64{
	88761, 71755, 56483, 46273, 36291,
	29154, 23254, 18705, 14949, 11916,
	9548, 7620, 6100, 4904, 3906,
	3121, 2501, 1991, 1586, 1277,
	1024, 820, 655, 526, 423,
	335, 272, 215, 172, 137,
	110, 87, 70, 56, 45,
	36, 29, 23, 18, 15,
}

// weightIdle is WEIGHT_IDLEPRIO, the weight of a SCHED_IDLE task.
const weightIdle = 3

func schedFacts(in inputs) []Fact {
	out := []Fact{cpuHybrid(in), cpuIsolatedCount(in)}
	out = append(out, freqFacts(in)...)
	out = append(out, loadFacts(in)...)
	out = append(out, schedulerFacts(in)...)
	return append(out, cgroupCPUFacts(in)...)
}

// cpuList reads a CPU-list input and returns its CPUs.
func (c *calc) cpuList(id string) []int {
	s := c.str(id)
	if c.failed() {
		return nil
	}
	cpus, err := collect.ParseCPUList(s)
	if err != nil {
		c.fail("%s %q is not a CPU list", id, s)
	}
	return cpus
}

func cpuHybrid(in inputs) Fact {
	const id = "derived.cpu.hybrid"
	c := in.calc()
	rule := "true when /sys/devices/cpu_core/cpus lists performance cores (hybrid x86), or when cpu_capacity differs between CPUs (arm big.LITTLE and similar); false when neither"
	perf, why := c.presence("cpu.hybrid.performance_cpus", func(v any) bool { s, _ := v.(string); return s != "" })
	if perf == triYes {
		return c.fact(id, rule+"; here the hybrid core lists exist", true)
	}
	topo := c.look("cpu.topology")
	if topo.Status != obs.StatusOK {
		if why != "" {
			c.fail("%s", why)
		}
		c.fail("cpu.topology is %s, so CPU capacities cannot be compared", statusWords(topo.Status))
		return c.fact(id, rule, nil)
	}
	if perf == triUnknown {
		c.fail("%s", why)
	}
	rows, _ := topo.Value.([]map[string]any)
	caps := map[int64]bool{}
	for _, r := range rows {
		if n, ok := obs.Int(r["capacity"]); ok {
			caps[n] = true
		}
	}
	switch {
	case len(caps) > 1:
		return c.fact(id, rule+fmt.Sprintf("; here CPUs have %d different capacities", len(caps)), true)
	case len(caps) == 1:
		return c.fact(id, rule+"; here no hybrid core list exists and every CPU has the same capacity", false)
	}
	return c.fact(id, rule+"; here no hybrid core list exists and the kernel publishes no CPU capacities, so the cores are taken to be alike", false)
}

func cpuIsolatedCount(in inputs) Fact {
	c := in.calc()
	cpus := c.cpuList("cpu.isolated")
	return c.fact("derived.cpu.isolated_count", "number of CPUs in /sys/devices/system/cpu/isolated", int64(len(cpus)))
}

// common returns the value of col shared by every row, "mixed" when rows
// differ, or fails when there is no row.
func (c *calc) common(id string, rows []map[string]any, col string) any {
	if c.failed() {
		return nil
	}
	if len(rows) == 0 {
		c.fail("%s lists no policy", id)
		return nil
	}
	first, _ := rows[0][col].(string)
	for _, r := range rows[1:] {
		if s, _ := r[col].(string); s != first {
			return "mixed"
		}
	}
	if first == "" {
		c.fail("%s has no %s column", id, col)
	}
	return first
}

func freqFacts(in inputs) []Fact {
	const pid = "cpu.freq.policies"
	c := in.calc()
	have, why := c.presence(pid, func(v any) bool { t, _ := v.([]map[string]any); return len(t) > 0 })
	if have == triUnknown {
		c.fail("%s", why)
	}
	out := []Fact{c.fact("derived.cpu.freq.available",
		"true when /sys/devices/system/cpu/cpufreq has at least one policy; a missing or empty directory means the kernel does not control frequency here", have == triYes)}

	c = in.calc()
	rows := c.table(pid)
	out = append(out, c.fact("derived.cpu.freq.driver", "the scaling_driver of every cpufreq policy, or mixed when they differ", c.common(pid, rows, "driver")))
	c = in.calc()
	rows = c.table(pid)
	out = append(out, c.fact("derived.cpu.freq.governor", "the scaling_governor of every cpufreq policy, or mixed when they differ", c.common(pid, rows, "governor")))

	c = in.calc()
	rule := "cpufreq/boost is 1 (acpi-cpufreq, amd-pstate), or else intel_pstate/no_turbo is 0"
	var val any
	if o := c.look("cpu.freq.boost"); o.Status == obs.StatusOK {
		val = o.Value
		rule += "; here from cpufreq/boost"
	} else if t := c.look("cpu.freq.intel_pstate.no_turbo"); t.Status == obs.StatusOK {
		b, _ := t.Value.(bool)
		val = !b
		rule += "; here from intel_pstate/no_turbo"
	} else {
		c.fail("cpu.freq.boost is %s and cpu.freq.intel_pstate.no_turbo is %s", statusWords(o.Status), statusWords(t.Status))
	}
	return append(out, c.fact("derived.cpu.freq.boost_enabled", rule, val))
}

// cpuTimeTotal sums the /proc/stat columns that partition CPU time. guest
// and guest_nice are already included in user and nice.
var cpuTimeParts = []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal"}

func loadFacts(in inputs) []Fact {
	var out []Fact
	c := in.calc()
	avg, n := c.float("sched.loadavg.avg1"), c.int("cpu.logical_count")
	if !c.failed() && n <= 0 {
		c.fail("cpu.logical_count is %d", n)
	}
	var per any
	if !c.failed() {
		per = avg / float64(n)
	}
	out = append(out, c.fact("derived.sched.load_per_cpu", "1-minute load average ÷ online CPUs", per))

	c = in.calc()
	all := c.row("sched.stat.cpu_times", "cpu", "all")
	// Kernels before 2.6.11 print fewer columns; a column that is not
	// there is time this kernel does not separate out, not zero.
	var total, idle int64
	for _, col := range cpuTimeParts {
		if _, ok := all[col]; ok || col == "idle" {
			n := c.cell("sched.stat.cpu_times", all, col)
			total += n
			if col == "idle" || col == "iowait" {
				idle += n
			}
		}
	}
	if !c.failed() && total <= 0 {
		c.fail("no CPU time recorded")
	}
	parts := strings.Join(cpuTimeParts, " + ")
	var busy, steal any
	if !c.failed() {
		busy = 1 - float64(idle)/float64(total)
	}
	out = append(out, c.fact("derived.sched.busy_since_boot_fraction", "1 − (idle + iowait) ÷ ("+parts+"), the all row of /proc/stat; guest time is already inside user and nice", busy))
	s := c.clone()
	st, ok := all["steal"]
	if !s.failed() && !ok {
		s.fail("this kernel's /proc/stat has no steal column")
	}
	if !s.failed() {
		v, _ := obs.Int(st)
		steal = float64(v) / float64(total)
	}
	out = append(out, s.fact("derived.sched.steal_fraction", "steal ÷ ("+parts+"), the all row of /proc/stat", steal))

	c = in.calc()
	ctxt, up := c.int("sched.stat.ctxt"), c.float("kernel.uptime_seconds")
	if !c.failed() && up <= 0 {
		c.fail("kernel.uptime_seconds is %g", up)
	}
	var rate any
	if !c.failed() {
		rate = float64(ctxt) / up
	}
	out = append(out, c.fact("derived.sched.ctxt_per_second", "ctxt from /proc/stat ÷ uptime", rate))

	c = in.calc()
	var run, wait, slices int64
	rows := c.table("sched.schedstat.cpus")
	for _, r := range rows {
		run += c.cell("sched.schedstat.cpus", r, "run_ns")
		wait += c.cell("sched.schedstat.cpus", r, "wait_ns")
		slices += c.cell("sched.schedstat.cpus", r, "timeslices")
	}
	w := c.clone()
	if !w.failed() && run+wait <= 0 {
		w.fail("no run or wait time recorded")
	}
	var wf, ts any
	if !w.failed() {
		wf = float64(wait) / float64(run+wait)
	}
	over := "over the CPUs of /proc/schedstat"
	if !c.failed() {
		over = fmt.Sprintf("over the %d CPUs of /proc/schedstat", len(rows))
	}
	out = append(out, w.fact("derived.sched.wait_fraction", "Σ wait_ns ÷ (Σ run_ns + Σ wait_ns) "+over, wf))
	if !c.failed() && slices <= 0 {
		c.fail("no timeslice recorded")
	}
	if !c.failed() {
		ts = float64(run) / float64(slices) / 1000
	}
	return append(out, c.fact("derived.sched.avg_timeslice_us", "Σ run_ns ÷ Σ timeslices "+over+", in microseconds", ts))
}

// kernelAtLeast reports whether a release string ("6.12.10-arch1-1",
// "6.6.51+rpt-rpi-v8") is at least major.minor.
func kernelAtLeast(release string, major, minor int) (bool, error) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return false, fmt.Errorf("kernel.release %q has no major.minor", release)
	}
	digits := func(s string) (int, error) {
		end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
		if end < 0 {
			end = len(s)
		}
		return strconv.Atoi(s[:end])
	}
	ma, err1 := digits(parts[0])
	mi, err2 := digits(parts[1])
	if err1 != nil || err2 != nil {
		return false, fmt.Errorf("kernel.release %q has no major.minor", release)
	}
	return ma > major || ma == major && mi >= minor, nil
}

func schedulerFacts(in inputs) []Fact {
	var out []Fact
	c := in.calc()
	rule := "eevdf when /proc/self/sched has an se.slice line (only EEVDF gives each task a slice); otherwise from the release: eevdf for Linux 6.6 and later, cfs before"
	var kind any
	if slice := c.look("sched.self.slice_us"); slice.Status == obs.StatusOK {
		kind, rule = "eevdf", rule+"; here se.slice is present"
	} else if rel := c.str("kernel.release"); !c.failed() {
		ge, err := kernelAtLeast(rel, 6, 6)
		switch {
		case err != nil:
			c.fail("%v", err)
		case ge:
			kind, rule = "eevdf", rule+fmt.Sprintf("; here sched.self.slice_us is %s and the release is %s", statusWords(slice.Status), rel)
		default:
			kind, rule = "cfs", rule+fmt.Sprintf("; here sched.self.slice_us is %s and the release is %s", statusWords(slice.Status), rel)
		}
	}
	out = append(out, c.fact("derived.sched.fair_scheduler", rule, kind))

	c = in.calc()
	ext, why := c.presence("sched.ext.state", func(v any) bool { return v == "enabled" })
	if ext == triUnknown {
		c.fail("%s", why)
	}
	out = append(out, c.fact("derived.sched.ext_enabled", "sched.ext.state is enabled; a kernel without /sys/kernel/sched_ext has no BPF scheduler", ext == triYes))

	c = in.calc()
	policy, nice := c.str("sched.self.policy"), c.int("sched.self.nice")
	var w any
	wrule := "sched_prio_to_weight[nice + 20] for SCHED_OTHER and SCHED_BATCH, 3 for SCHED_IDLE"
	if !c.failed() {
		switch {
		case nice < -20 || nice > 19:
			c.fail("sched.self.nice %d is outside -20 to 19", nice)
		case policy == "SCHED_OTHER" || policy == "SCHED_BATCH":
			w = SchedPrioToWeight[nice+20]
			wrule += fmt.Sprintf("; here %s at nice %d", policy, nice)
		case policy == "SCHED_IDLE":
			w = int64(weightIdle)
		default:
			c.fail("atlas runs under %s, which is not scheduled by weight", policy)
		}
	}
	out = append(out, c.fact("derived.sched.self_weight", wrule, w))

	c = in.calc()
	cpus := c.cpuList("sched.self.cpus_allowed_list")
	return append(out, c.fact("derived.sched.self_allowed_count", "number of CPUs in Cpus_allowed_list of /proc/self/status", int64(len(cpus))))
}

func cgroupCPUFacts(in inputs) []Fact {
	const qid, pid = "sched.cgroup.cpu.max_quota_us", "sched.cgroup.cpu.max_period_us"
	var out []Fact
	c := in.calc()
	lim, why := c.presence(qid, func(v any) bool { return v != "max" })
	if lim == triUnknown {
		c.fail("%s", why)
	}
	out = append(out, c.fact("derived.cgroup.cpu_limited", "the quota in cpu.max (or cpu.cfs_quota_us on cgroup v1) is a number rather than max; no cpu controller for the cgroup means no quota at this level", lim == triYes))

	c = in.calc()
	q, _ := c.value(qid)
	period := c.int(pid)
	var cpus any
	if !c.failed() {
		n, isNum := obs.Int(q)
		switch {
		case !isNum:
			c.fail("the cgroup has no CPU quota (cpu.max is %v)", q)
		case period <= 0:
			c.fail("the period is %d", period)
		default:
			cpus = float64(n) / float64(period)
		}
	}
	out = append(out, c.fact("derived.cgroup.cpu_quota_cpus", "quota ÷ period from cpu.max (cpu.cfs_quota_us ÷ cpu.cfs_period_us on cgroup v1)", cpus))

	c = in.calc()
	thr, per := c.int("sched.cgroup.cpu.nr_throttled"), c.int("sched.cgroup.cpu.nr_periods")
	if !c.failed() && per <= 0 {
		c.fail("no quota period has elapsed (nr_periods is %d)", per)
	}
	var frac any
	if !c.failed() {
		frac = float64(thr) / float64(per)
	}
	return append(out, c.fact("derived.cgroup.cpu_throttled_fraction", "nr_throttled ÷ nr_periods from cpu.stat", frac))
}
