package probe

import (
	"fmt"
	"slices"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Ids the probe collector owns.
const (
	idRegions    = "probe.regions"
	idMapsSample = "probe.maps.sample"
	idVMACount   = "probe.maps.vma_count"
	idLowest     = "probe.maps.lowest_start"
	idHighest    = "probe.maps.highest_end"
	idPFN        = "probe.pagemap.pfn_visible"
	idLayout     = "probe.layout.runs"
	idTHPAlloc   = "probe.thp.fault_alloc_delta"
	idTHPFall    = "probe.thp.fault_fallback_delta"
)

// statusFields are the /proc/<pid>/status lines reported, all in kB there.
var statusFields = []string{"VmSize", "VmRSS", "RssAnon", "RssFile", "RssShmem", "VmSwap", "VmPTE"}

// faultIDs pairs each fault id with its counter in the report.
var faultIDs = []struct {
	id    string
	value func(*Faults) int64
}{
	{"probe.faults.minflt_before_touch", func(f *Faults) int64 { return f.MinfltBefore }},
	{"probe.faults.minflt_after_touch", func(f *Faults) int64 { return f.MinfltAfter }},
	{"probe.faults.majflt_before_touch", func(f *Faults) int64 { return f.MajfltBefore }},
	{"probe.faults.majflt_after_touch", func(f *Faults) int64 { return f.MajfltAfter }},
}

// ids returns every id the probe owns, in a fixed order.
func ids() []string {
	out := []string{idRegions, idMapsSample, idVMACount, idLowest, idHighest}
	for _, f := range statusFields {
		out = append(out, "probe.status."+f)
	}
	for _, f := range faultIDs {
		out = append(out, f.id)
	}
	return append(out, idTHPAlloc, idTHPFall, idPFN, idLayout)
}

// Sources. The child's pid means nothing outside one run, so paths name it
// <probe>; a fixture's _probe/ files stand for the same paths.
var (
	srcReport  = obs.Probe("atlas __probe report")
	srcMaps    = obs.Probe("/proc/<probe>/maps")
	srcSmaps   = obs.Probe("/proc/<probe>/smaps")
	srcStatus  = obs.Probe("/proc/<probe>/status")
	srcRusage  = obs.Probe("getrusage(RUSAGE_THREAD)")
	srcPagemap = obs.Probe("/proc/self/pagemap")
	srcVmstat  = obs.Probe("/proc/vmstat")
)

// part is one input of the probe: the bytes read, or why they could not be.
type part struct {
	data []byte
	err  error
}

// inputs are everything one probe run produced.
type inputs struct {
	// report is the child's decoded report, its layout_runs already
	// holding both executions; reportErr says why there is none.
	report    *Report
	reportErr error
	maps      part
	smaps     part
	status    part
	// layoutNote explains a layout table with fewer than two runs.
	layoutNote string
}

// observe turns the probe's outputs into observations. Each id depends
// only on the parts it is computed from, so an unreadable smaps fails
// probe.regions and nothing else.
func observe(in inputs) []obs.Observation {
	var out []obs.Observation
	r, repErr := in.report, in.reportErr

	out = append(out, regions(r, repErr, in.smaps))
	out = append(out, mapsObservations(in.maps)...)
	out = append(out, statusObservations(in.status)...)

	for _, f := range faultIDs {
		if repErr != nil {
			out = append(out, obs.Errored(f.id, srcRusage, repErr))
			continue
		}
		o := obs.OK(f.id, f.value(r.Faults), srcRusage)
		o.Source.Detail = "the probe touches anon-written between the two readings"
		out = append(out, o)
	}

	for _, t := range []struct {
		id, field string
		value     func(*THPFaults) int64
	}{
		{idTHPAlloc, "thp_fault_alloc", func(t *THPFaults) int64 { return t.FaultAllocDelta }},
		{idTHPFall, "thp_fault_fallback", func(t *THPFaults) int64 { return t.FaultFallbackDelta }},
	} {
		src := srcVmstat
		src.Field = t.field
		switch {
		case repErr != nil:
			out = append(out, obs.Errored(t.id, src, repErr))
		case r.THPFaults == nil:
			out = append(out, obs.Absent(t.id, src, "the probe could not read thp_fault_alloc and thp_fault_fallback from /proc/vmstat around its write loop; a kernel built without transparent huge pages has neither"))
		default:
			o := obs.OK(t.id, t.value(r.THPFaults), src)
			o.Source.Detail = "system-wide counter read just before and just after the write loop over anon-written; the difference includes other processes' faults in that instant"
			out = append(out, o)
		}
	}

	switch {
	case repErr != nil:
		out = append(out, obs.Errored(idPFN, srcPagemap, repErr))
	case !r.Pagemap.Readable:
		out = append(out, obs.Failed(idPFN, srcPagemap, "the probe could not read its own /proc/self/pagemap"))
	default:
		out = append(out, obs.OK(idPFN, r.Pagemap.PFNVisible, srcPagemap))
	}

	if repErr != nil {
		out = append(out, obs.Errored(idLayout, srcReport, repErr))
	} else {
		rows := make([]map[string]any, len(r.LayoutRuns))
		for i, l := range r.LayoutRuns {
			row := map[string]any{"run": int64(l.Run), "stack": l.Stack, "mmap_base": l.MmapBase, "exe": l.Exe}
			if l.Heap != "" {
				row["heap"], row["heap_kind"] = l.Heap, l.HeapKind
			}
			rows[i] = row
		}
		o := obs.OK(idLayout, rows, srcReport)
		o.Source.Detail = "mmap_base is approximated by the end of the highest mapping below the stack"
		o.Note = in.layoutNote
		out = append(out, o)
	}
	return out
}

// smapsColumns maps probe.regions columns to smaps fields.
var smapsColumns = []struct{ column, field string }{
	{"rss_bytes", "Rss"},
	{"pss_bytes", "Pss"},
	{"anonymous_bytes", "Anonymous"},
	{"private_clean_bytes", "Private_Clean"},
	{"private_dirty_bytes", "Private_Dirty"},
	{"shared_clean_bytes", "Shared_Clean"},
	{"shared_dirty_bytes", "Shared_Dirty"},
	{"swap_bytes", "Swap"},
	{"anon_huge_bytes", "AnonHugePages"},
}

// regions joins the report's regions with the smaps VMAs that cover them.
// The child names each anonymous region (or, on a kernel that cannot name
// mappings, surrounds it with inaccessible guard pages) so the kernel cannot
// merge it with a neighbour, but a region may still be split into several
// VMAs, so the fields of every VMA inside it are summed, and the VMAs must
// cover it exactly.
func regions(r *Report, repErr error, smaps part) obs.Observation {
	if repErr != nil {
		return obs.Errored(idRegions, srcReport, repErr)
	}
	if smaps.err != nil {
		return obs.Errored(idRegions, srcSmaps, smaps.err)
	}
	vmas, err := parseSmaps(smaps.data)
	if err != nil {
		return obs.Failed(idRegions, srcSmaps, err.Error())
	}
	rows := make([]map[string]any, 0, len(r.Regions))
	for _, g := range r.Regions {
		start, _ := parseHex(g.Start) // checked by decodeReport
		end, _ := parseHex(g.End)
		row := map[string]any{
			"name": g.Name, "start": g.Start, "end": g.End, "size_bytes": g.SizeBytes,
			"backing": g.Backing, "touched_bytes": g.TouchedBytes, "pages_total": g.PagesTotal,
		}
		if g.PagesPresent != nil {
			row["pages_present"] = *g.PagesPresent
		}
		if g.PagesSwapped != nil {
			row["pages_swapped"] = *g.PagesSwapped
		}
		var covered uint64
		for _, v := range vmas {
			if v.Start < start || v.End > end {
				continue
			}
			if covered == 0 {
				row["perms"], row["path"] = v.Perms, displayPath(v.Path)
			}
			covered += v.End - v.Start
			for _, c := range smapsColumns {
				n, ok := v.Fields[c.field]
				if !ok {
					return obs.Failed(idRegions, srcSmaps, fmt.Sprintf("smaps has no %s line for region %s", c.field, g.Name))
				}
				sum, _ := row[c.column].(int64)
				row[c.column] = sum + n
			}
		}
		if covered != end-start {
			return obs.Failed(idRegions, srcSmaps, fmt.Sprintf("smaps mappings cover %d of the %d bytes of region %s: the kernel merged it with a neighbouring mapping, so its figures cannot be separated", covered, end-start, g.Name))
		}
		rows = append(rows, row)
	}
	o := obs.OK(idRegions, rows, srcSmaps)
	o.Source.Detail = "sizes from smaps (converted from kB); pages_* from the probe's own pagemap; " + placementText[r.Placement]
	return o
}

// placementText says how the child laid its regions out.
var placementText = map[string]string{
	PlacementMmap:    "each region is one plain mmap placed by the kernel, the anonymous ones named \"atlas probe: <region>\"",
	PlacementGuarded: "this kernel cannot name anonymous mappings, so the probe placed each region itself on a 2 MiB boundary between guard pages",
}

// maxSample is how many VMAs probe.maps.sample keeps.
const maxSample = 64

func mapsObservations(maps part) []obs.Observation {
	var vmas []vma
	err := maps.err
	if err == nil {
		vmas, err = parseMaps(maps.data)
	}
	if err != nil {
		// A read error keeps its errno status; a parse error is failed.
		var out []obs.Observation
		for _, id := range []string{idMapsSample, idVMACount, idLowest, idHighest} {
			out = append(out, obs.Errored(id, srcMaps, err))
		}
		return out
	}
	var rows []map[string]any
	var lowest, highest uint64 = vmas[0].Start, 0
	for i, v := range vmas {
		if i < maxSample {
			rows = append(rows, map[string]any{
				"start": hex(v.Start), "end": hex(v.End), "perms": v.Perms,
				"offset": hex(v.Offset), "kind": vmaKind(v.Path), "path": displayPath(v.Path),
			})
		}
		// The x86-64 vsyscall page sits in the kernel half of the address
		// space; it is not a user mapping.
		if v.Path != "[vsyscall]" {
			highest = max(highest, v.End)
		}
	}
	return []obs.Observation{
		obs.OK(idMapsSample, rows, srcMaps),
		obs.OK(idVMACount, int64(len(vmas)), srcMaps),
		obs.OK(idLowest, hex(lowest), srcMaps),
		obs.OK(idHighest, hex(highest), srcMaps),
	}
}

func statusObservations(status part) []obs.Observation {
	var fields []collect.Field
	err := status.err
	if err == nil {
		fields, err = collect.ParseFields(status.data)
	}
	out := make([]obs.Observation, 0, len(statusFields))
	for _, name := range statusFields {
		id := "probe.status." + name
		src := srcStatus
		src.Field = name
		if err != nil {
			// A read error keeps its errno status; a parse error is failed.
			out = append(out, obs.Errored(id, src, err))
			continue
		}
		i := slices.IndexFunc(fields, func(f collect.Field) bool { return f.Key == name })
		if i < 0 {
			out = append(out, obs.Absent(id, src, "this kernel's /proc/<pid>/status has no "+name+" line"))
			continue
		}
		v, kb, perr := collect.ParseKB(fields[i].Value)
		if perr == nil && (!kb || v < 0) {
			perr = fmt.Errorf("%s: expected a size in kB, got %q", name, clipText(fields[i].Value))
		}
		if perr != nil {
			out = append(out, obs.Failed(id, src, perr.Error()))
			continue
		}
		src.Detail = "converted from kB"
		out = append(out, obs.OK(id, v, src))
	}
	return out
}
