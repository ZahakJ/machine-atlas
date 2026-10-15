package derive

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const mi = "mem.meminfo."

// accountedFields are the meminfo consumers whose sum is compared with
// MemTotal. Cached includes Shmem and AnonPages excludes it, so shared
// memory is counted once.
var accountedFields = []string{
	"MemFree", "Buffers", "Cached", "AnonPages", "Slab", "KernelStack", "PageTables", "Percpu", "VmallocUsed",
}

func memFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	total, ps := c.int(mi+"MemTotal"), c.int("kernel.page_size_bytes")
	if !c.failed() && ps <= 0 {
		c.fail("kernel.page_size_bytes is %d, not a page size", ps)
	}
	var pages any
	if !c.failed() {
		pages = total / ps
	}
	out = append(out, c.fact("derived.mem.total_pages", "MemTotal / kernel.page_size_bytes", pages))

	c = in.calc()
	total, avail := c.int(mi+"MemTotal"), c.int(mi+"MemAvailable")
	out = append(out, c.fact("derived.mem.used_estimate_bytes", "MemTotal − MemAvailable", total-avail))
	if !c.failed() && total <= 0 {
		c.fail("MemTotal is %d, so a fraction of it is undefined", total)
	}
	var frac any
	if !c.failed() {
		frac = float64(avail) / float64(total)
	}
	out = append(out, c.fact("derived.mem.available_fraction", "MemAvailable / MemTotal", frac))

	c = in.calc()
	cache := c.int(mi+"Cached") + c.int(mi+"Buffers") - c.int(mi+"Shmem")
	out = append(out, c.fact("derived.mem.file_cache_bytes", "Cached + Buffers − Shmem", cache))

	c = in.calc()
	var sum int64
	for _, f := range accountedFields {
		sum += c.int(mi + f)
	}
	names := strings.Join(accountedFields, " + ")
	out = append(out, c.fact("derived.mem.accounted_bytes", names, sum))
	total = c.int(mi + "MemTotal")
	out = append(out, c.fact("derived.mem.unaccounted_bytes",
		"MemTotal − ("+names+"); what remains is memory meminfo attributes to none of these, typically the hugetlb pool (Hugetlb), swap-cache pages no process maps, the zswap pool (Zswap), driver allocations such as graphics and network buffers, and per-CPU page lists. Overlaps between fields can make it negative",
		total-sum))

	out = append(out, dirtyFacts(in)...)
	return append(out, allocStalls(in))
}

// dirtyIgnored is what the dirty threshold estimates leave out.
const dirtyIgnored = "An estimate of the global limit: the kernel also subtracts each zone's lowmem reserve, leaves out highmem (32-bit kernels, unless vm.highmem_is_dirtyable), gives real-time writers a quarter more, and applies per-cgroup and per-device limits below this one; none of that is counted here"

// dirtyFacts estimates the global dirty thresholds the way
// domain_dirty_limits() in mm/page-writeback.c computes them: a non-zero
// vm.dirty_bytes (vm.dirty_background_bytes) is the threshold itself,
// rounded up to whole pages; otherwise the ratio is a percentage of
// dirtyable memory (global_dirtyable_memory()): free pages minus the
// reserves of the zones, plus the active and inactive file LRUs. The
// estimate uses the summed high watermarks as the reserves. A background
// threshold at or above the dirty threshold is cut to half of it.
func dirtyFacts(in inputs) []Fact {
	c := in.calc()
	thresh, rule := dirtyThreshold(c, "dirty_bytes", "dirty_ratio")
	out := []Fact{c.fact("derived.dirty.threshold_estimate_bytes", rule+". "+dirtyIgnored, thresh)}

	bg, bgRule := dirtyThreshold(c, "dirty_background_bytes", "dirty_background_ratio")
	if !c.failed() && bg >= thresh {
		bg = thresh / 2
		bgRule += "; that is at or above the dirty threshold, so the kernel uses half the dirty threshold instead"
	} else {
		bgRule += ", provided it stays below the dirty threshold (otherwise the kernel uses half of that)"
	}
	return append(out, c.fact("derived.dirty.background_threshold_estimate_bytes",
		bgRule+". The dirty threshold it is compared with: "+rule+". "+dirtyIgnored, bg))
}

// dirtyThreshold estimates one threshold from vm.<bytesName> or, when that
// is 0, vm.<ratioName> of dirtyable memory, and says which applied.
func dirtyThreshold(c *calc, bytesName, ratioName string) (int64, string) {
	const sysctl = "mem.sysctl.vm."
	ps := c.int("kernel.page_size_bytes")
	set := c.int(sysctl + bytesName)
	if c.failed() {
		return 0, "vm." + bytesName + " when non-zero, rounded up to whole pages, otherwise vm." + ratioName + "% × dirtyable memory (" + dirtyableRule + ")"
	}
	if set > 0 && ps > 0 {
		return (set + ps - 1) / ps * ps, "vm." + bytesName + " is non-zero, so it replaces vm." + ratioName + ": the threshold is vm." + bytesName + " rounded up to whole pages"
	}
	ratio, d := c.int(sysctl+ratioName), dirtyable(c, ps)
	return ratio * d / 100, "vm." + bytesName + " is 0, so vm." + ratioName + " applies: vm." + ratioName + "% × dirtyable memory, " + dirtyableRule
}

const dirtyableRule = "dirtyable memory = MemFree − the zones' high watermarks summed (at most MemFree) + Active(file) + Inactive(file)"

// dirtyable estimates global_dirtyable_memory() in bytes.
func dirtyable(c *calc, ps int64) int64 {
	free := c.int(mi + "MemFree")
	var high int64
	for _, r := range c.table("mem.zones") {
		high += c.cell("mem.zones", r, "high")
	}
	return free - min(free, high*ps) + c.int(mi+"Active_file") + c.int(mi+"Inactive_file")
}

// allocStalls sums the per-zone direct-reclaim counters
// (mem.vmstat.allocstall_<zone>) that /proc/vmstat has here. A zone the
// kernel was built without has no counter (absent), which leaves nothing
// out; any other unreadable counter would, so the sum is then unknown.
func allocStalls(in inputs) Fact {
	const prefix = "mem.vmstat.allocstall_"
	c := in.calc()
	var summed []string
	var sum int64
	for _, id := range slices.Sorted(maps.Keys(in)) {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		if o := c.look(id); o.Status == obs.StatusAbsent {
			continue
		}
		sum += c.int(id)
		summed = append(summed, strings.TrimPrefix(id, "mem.vmstat."))
	}
	rule := "sum of the per-zone direct-reclaim counters in /proc/vmstat"
	switch {
	case c.failed():
	case len(summed) == 0:
		c.fail("no mem.vmstat.allocstall_* counter is readable in this snapshot")
	default:
		rule = strings.Join(summed, " + ") + ": every allocstall_* counter /proc/vmstat has here"
	}
	return c.fact("derived.reclaim.allocstalls", rule+"; each counts entries into direct reclaim by allocations whose highest allowed zone is that zone; reclaim forced by a memory cgroup's limit is not counted", sum)
}

func swapFacts(in inputs) []Fact {
	var out []Fact
	c := in.calc()
	total := c.int(mi + "SwapTotal")
	out = append(out,
		c.fact("derived.swap.configured", "SwapTotal > 0", total > 0),
		c.fact("derived.swap.total_bytes", "SwapTotal", total))

	c = in.calc()
	total, free := c.int(mi+"SwapTotal"), c.int(mi+"SwapFree")
	if !c.failed() && total == 0 {
		c.fail("no swap is configured (SwapTotal is 0), so a used fraction is undefined")
	}
	var used any
	if !c.failed() {
		used = float64(total-free) / float64(total)
	}
	out = append(out, c.fact("derived.swap.used_fraction", "(SwapTotal − SwapFree) / SwapTotal", used))

	out = append(out, compressedSwap(in))
	return out
}

// compressedSwap decides derived.swap.compressed: whether pages that go to
// swap here are compressed in RAM. Installed is not active: zswap counts
// only with a swap device behind it, and a zram device only when it is an
// active swap area with a size, not merely present (it may be unconfigured
// or hold a filesystem).
func compressedSwap(in inputs) Fact {
	const id = "derived.swap.compressed"
	const rule = "active swap (SwapTotal > 0) with zswap enabled, or a zram device listed as an active swap area in /proc/swaps"
	c := in.calc()
	total := c.int(mi + "SwapTotal")
	zswap, zswapReason := c.presence("mem.zswap.enabled", func(v any) bool { return v == true })
	areas := c.look("mem.swap.areas")
	devices := c.look("mem.zram.devices")
	if c.failed() {
		return c.fact(id, rule, nil)
	}
	var notes []string
	switch zswap {
	case triYes:
		notes = append(notes, "zswap is enabled")
	case triNo:
		notes = append(notes, "zswap is off or not built (its parameter file is absent only when zswap is not built into the kernel)")
	}
	zramSwap := false
	if rows, ok := areas.Value.([]map[string]any); ok && areas.Status == obs.StatusOK {
		for _, r := range rows {
			name, _ := r["name"].(string)
			size, _ := obs.Int(r["size_bytes"])
			if strings.HasPrefix(name, "zram") && size > 0 {
				zramSwap = true
				notes = append(notes, name+" is an active swap area")
			}
		}
	}
	if !zramSwap {
		switch rows, _ := devices.Value.([]map[string]any); {
		case devices.Status == obs.StatusOK && len(rows) > 0:
			notes = append(notes, fmt.Sprintf("%d zram device(s) exist but none is an active swap area", len(rows)))
		case devices.Status == obs.StatusOK || devices.Status == obs.StatusAbsent:
			notes = append(notes, "no zram block device exists (the zram module may simply not be loaded)")
		}
	}
	explain := func(lead string) string {
		return rule + "; here " + lead + ": " + strings.Join(notes, ", ")
	}

	if total == 0 {
		lead := "no swap is active (SwapTotal is 0), so nothing is compressed on its way to swap"
		if zswap == triYes {
			lead += " (zswap is enabled but inactive without a swap device)"
		}
		return c.fact(id, explain(lead), false)
	}
	switch {
	case zswap == triYes:
		return c.fact(id, explain("swap is active and zswap compresses pages on their way to it"), true)
	case zramSwap:
		return c.fact(id, explain("swap is active on a compressed RAM device"), true)
	case areas.Status != obs.StatusOK:
		c.fail("mem.swap.areas is %s, so a zram swap area cannot be ruled out", statusWords(areas.Status))
	case zswap == triUnknown:
		c.fail("%s", zswapReason)
	}
	return c.fact(id, explain("swap is active, uncompressed"), false)
}

func yesNo(name string, t tri) string {
	switch t {
	case triYes:
		return name + " yes"
	case triNo:
		return name + " no"
	}
	return name + " unknown"
}

func policyFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	mode := c.str("mem.thp.enabled")
	out = append(out, c.fact("derived.thp.mode", "the bracketed choice in /sys/kernel/mm/transparent_hugepage/enabled", mode))

	c = in.calc()
	thp, reason := c.presence("mem.thp.enabled", func(v any) bool { return v != "never" })
	if thp == triUnknown {
		c.fail("%s", reason)
	}
	out = append(out, c.fact("derived.thp.enabled", "THP mode is not never; an absent THP interface means a kernel built without THP, so no", thp == triYes))

	c = in.calc()
	o := c.look("mem.lru_gen.enabled")
	var kind any
	rule := "mglru if bit 0 of /sys/kernel/mm/lru_gen/enabled is set, otherwise classic; an absent file means an older kernel or one built without MGLRU, both classic"
	switch o.Status {
	case obs.StatusOK:
		s, _ := o.Value.(string)
		if n, err := strconv.ParseUint(strings.TrimSpace(s), 0, 64); err != nil {
			c.fail("mem.lru_gen.enabled %q is not a number", s)
		} else if n&1 == 1 {
			kind = "mglru"
		} else {
			kind = "classic"
		}
	case obs.StatusAbsent:
		kind = "classic"
	default:
		c.fail("mem.lru_gen.enabled is %s", statusWords(o.Status))
	}
	out = append(out, c.fact("derived.reclaim.lru_kind", rule, kind))

	c = in.calc()
	tpc := c.int("cpu.threads_per_core")
	out = append(out, c.fact("derived.cpu.smt", "cpu.threads_per_core > 1", tpc > 1))

	c = in.calc()
	pbits := c.int("cpu.address_bits.physical")
	var phys any
	if !c.failed() && pbits > 0 && pbits < 63 {
		phys = int64(1) << pbits
	} else if !c.failed() {
		c.fail("cpu.address_bits.physical %d is out of range", pbits)
	}
	out = append(out, c.fact("derived.cpu.phys_addressable_bytes", "2^cpu.address_bits.physical", phys))

	c = in.calc()
	kills := c.int("mem.vmstat.oom_kill")
	out = append(out,
		c.fact("derived.oom.kills_since_boot", "mem.vmstat.oom_kill", kills),
		c.fact("derived.oom.any", "mem.vmstat.oom_kill > 0", kills > 0))

	c = in.calc()
	limited := func(v any) bool { _, isNum := v.(int64); return isNum }
	max, r1 := c.presence("mem.cgroup.memory.max", limited)
	high, r2 := c.presence("mem.cgroup.memory.high", limited)
	cgRule := "memory.max or memory.high of atlas's cgroup is a number rather than \"max\""
	switch {
	case max == triYes || high == triYes:
		out = append(out, c.fact("derived.cgroup.memory_limited", cgRule, true))
	case max == triNo && high == triNo && c.look("mem.cgroup.memory.max").Status == obs.StatusOK:
		out = append(out, c.fact("derived.cgroup.memory_limited", cgRule, false))
	default:
		for _, r := range []string{r1, r2} {
			if r != "" {
				c.fail("%s", r)
			}
		}
		if !c.failed() {
			c.fail("the cgroup v2 memory interface is not available (mem.cgroup.memory.max is %s)", statusWords(c.look("mem.cgroup.memory.max").Status))
		}
		out = append(out, c.fact("derived.cgroup.memory_limited", cgRule, nil))
	}

	c = in.calc()
	ocm := c.int("mem.sysctl.vm.overcommit_memory")
	names := map[int64]string{0: "heuristic", 1: "always", 2: "strict"}
	name, known := names[ocm]
	if !c.failed() && !known {
		c.fail("mem.sysctl.vm.overcommit_memory %d is not 0, 1 or 2", ocm)
	}
	out = append(out, c.fact("derived.overcommit.mode_name", "overcommit_memory 0 → heuristic, 1 → always, 2 → strict", name))

	c = in.calc()
	committed, limit := c.int(mi+"Committed_AS"), c.int(mi+"CommitLimit")
	out = append(out, c.fact("derived.commit.over_limit", "Committed_AS > CommitLimit", committed > limit))

	c = in.calc()
	psi := c.look("mem.psi.some.avg10")
	var avail any
	switch psi.Status {
	case obs.StatusOK:
		avail = true
	case obs.StatusAbsent, obs.StatusUnsupported, obs.StatusPermissionDenied:
		avail = false
	default:
		c.fail("mem.psi.some.avg10 is %s", statusWords(psi.Status))
	}
	out = append(out, c.fact("derived.psi.available", "/proc/pressure/memory could be read (mem.psi.some.avg10 is ok); absent means a kernel without CONFIG_PSI or booted with psi=0", avail))
	return out
}

func zoneFacts(in inputs) []Fact {
	var out []Fact
	for _, z := range []struct{ col, id, what string }{
		{"min", "derived.zones.min_bytes", "min watermarks"},
		{"low", "derived.zones.low_bytes", "low watermarks"},
		{"high", "derived.zones.high_bytes", "high watermarks"},
		{"free", "derived.zones.free_bytes", "free pages"},
	} {
		c := in.calc()
		rows, ps := c.table("mem.zones"), c.int("kernel.page_size_bytes")
		var sum int64
		for _, r := range rows {
			sum += c.cell("mem.zones", r, z.col)
		}
		rule := fmt.Sprintf("sum of the %s column over every zone in mem.zones × page size: the %s", z.col, z.what)
		if !c.failed() {
			rule = fmt.Sprintf("sum of the %s column over all %d zones × page size: the %s", z.col, len(rows), z.what)
		}
		out = append(out, c.fact(z.id, rule, sum*ps))
	}

	c := in.calc()
	nodes := c.table("mem.numa.nodes")
	n := int64(len(nodes))
	out = append(out,
		c.fact("derived.numa.node_count", "number of rows in mem.numa.nodes", n),
		c.fact("derived.numa.multi", "more than one NUMA node", n > 1))
	return out
}
