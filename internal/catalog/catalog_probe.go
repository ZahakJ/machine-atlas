package catalog

import "github.com/ZahakJ/machine-atlas/internal/obs"

// The probe is a short-lived child process (atlas __probe) that makes five
// known mappings and lets the parent read its /proc files, so lessons can
// show the kernel's accounting for memory whose history is known exactly.

const (
	probeCaveat     = "Describes one small Go process at one moment; the Go runtime adds mappings of its own, and results vary with THP availability and fragmentation."
	probePrivileged = "Root would see physical frame numbers in pagemap (CAP_SYS_ADMIN); everything else is visible to the process itself."
)

// probeEntry declares one probe measurement.
func probeEntry(id, typ, unit, title, desc string) Entry {
	return Entry{
		ID: id, Title: title, Description: desc, Unit: unit, Type: typ,
		Kind: obs.KindLive, Scope: obs.ScopeProcess, Caveat: probeCaveat, Privileged: probePrivileged,
	}
}

// probeTable declares one probe table with its columns.
func probeTable(id, title, desc string, columns ...string) Entry {
	e := probeEntry(id, obs.TypeTable, "", title, desc)
	e.Columns = columns
	return e
}

// probeStatus declares one /proc/<probe>/status line.
func probeStatus(field, title, desc string) Entry {
	return probeEntry("probe.status."+field, obs.TypeInt, obs.UnitBytes, title, desc+" Read from the probe's /proc/<pid>/status.")
}

// probeFault declares one getrusage fault counter of the probe.
func probeFault(name, title, desc string) Entry {
	return probeEntry("probe.faults."+name, obs.TypeInt, obs.UnitCount, title, desc)
}

// probeTHP declares the growth of a system-wide /proc/vmstat THP counter
// across the probe's write loop.
func probeTHP(name, counter, title, desc string) Entry {
	e := probeEntry("probe.thp."+name, obs.TypeInt, obs.UnitCount, title, desc+" Read by the probe from /proc/vmstat outside its getrusage window.")
	e.Caveat = "The counter is system-wide, so faults any other process took in the same instant are included; on a busy machine the difference can exceed what the probe caused. Counts huge pages, not faults or bytes. Absent on a kernel built without THP (no " + counter + " line)."
	return e
}

var probeEntries = []Entry{
	probeTable("probe.regions", "Probe regions",
		"The five mappings the probe made (untouched, written and read anonymous memory, a private file mapping, a shared memfd) with their smaps accounting and pagemap page counts. Each region differs from the others in exactly one respect, so the columns show what that respect costs.",
		"name", "start", "end", "size_bytes", "perms", "backing", "touched_bytes", "rss_bytes", "pss_bytes",
		"anonymous_bytes", "private_clean_bytes", "private_dirty_bytes", "shared_clean_bytes", "shared_dirty_bytes",
		"swap_bytes", "anon_huge_bytes", "pages_total", "pages_present", "pages_swapped", "path"),
	probeTable("probe.maps.sample", "Probe mappings (first 64)",
		"The first 64 lines of the probe's /proc/<pid>/maps: address range, permissions, offset, kind and file basename of each VMA.",
		"start", "end", "perms", "offset", "kind", "path"),
	probeEntry("probe.maps.vma_count", obs.TypeInt, obs.UnitCount, "Probe VMA count",
		"How many separate mappings (VMAs) the probe's address space has; max_map_count limits this number."),
	probeEntry("probe.maps.lowest_start", obs.TypeString, "", "Lowest mapped address",
		"The start of the probe's lowest mapping, in hex: usually the executable."),
	probeEntry("probe.maps.highest_end", obs.TypeString, "", "Highest mapped address",
		"The end of the probe's highest user mapping, in hex: usually the stack or the vDSO, near the top of user space."),
	probeStatus("VmSize", "Probe virtual size",
		"Total size of every mapping in the probe's address space: promised, not used."),
	probeStatus("VmRSS", "Probe resident size",
		"Memory of the probe that is resident in RAM: RssAnon + RssFile + RssShmem."),
	probeStatus("RssAnon", "Probe resident anonymous memory", "Resident anonymous pages of the probe."),
	probeStatus("RssFile", "Probe resident file memory", "Resident file-backed pages of the probe, including its own executable."),
	probeStatus("RssShmem", "Probe resident shared memory", "Resident shmem pages of the probe, such as its memfd region."),
	probeStatus("VmSwap", "Probe swapped memory", "The probe's anonymous memory currently in swap."),
	probeStatus("VmPTE", "Probe page tables", "Memory used by the probe's page tables."),
	probeFault("minflt_before_touch", "Minor faults before touch",
		"The probe's minor fault count (getrusage) just before it wrote to its anon-written region."),
	probeFault("minflt_after_touch", "Minor faults after touch",
		"The probe's minor fault count just after writing one byte to every page of anon-written; the difference is the faults those writes took."),
	probeFault("majflt_before_touch", "Major faults before touch",
		"The probe's major fault count before the write loop."),
	probeFault("majflt_after_touch", "Major faults after touch",
		"The probe's major fault count after the write loop; fresh anonymous memory should need no I/O, so it normally does not change."),
	probeEntry("probe.pagemap.pfn_visible", obs.TypeBool, "", "Frame numbers visible",
		"Whether /proc/self/pagemap showed the probe real physical frame numbers. Since Linux 4.2 they read as zero without CAP_SYS_ADMIN, so presence is visible but location is not."),
	probeTHP("fault_alloc_delta", "thp_fault_alloc", "Huge pages allocated during the write loop",
		"How much the system-wide thp_fault_alloc counter grew between a reading just before and one just after the probe's write loop over anon-written: transparent huge pages allocated in the fault handler in that interval."),
	probeTHP("fault_fallback_delta", "thp_fault_fallback", "Huge page fallbacks during the write loop",
		"How much the system-wide thp_fault_fallback counter grew across the probe's write loop: faults that wanted a transparent huge page, could not get one, and fell back to small pages."),
	probeTable("probe.layout.runs", "Address layout of two runs",
		"Stack, mmap base, heap and executable addresses of the probe in two separate executions. With ASLR they differ between runs. stack is the start of the [stack] mapping; heap_kind says which mapping heap is: brk for a [heap] mapping (the brk area C malloc grows), go-arena for the Go runtime's mmap'd heap arena.",
		"run", "stack", "mmap_base", "heap", "heap_kind", "exe"),
}
