package derive

import "fmt"

const regions = "probe.regions"

func probeFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	rss := c.cell(regions, c.row(regions, "name", "anon-untouched"), "rss_bytes")
	out = append(out, c.fact("derived.probe.untouched_rss_bytes", "rss_bytes of the anon-untouched region (64 MiB mapped, never touched)", rss))

	c = in.calc()
	written := c.row(regions, "name", "anon-written")
	size := c.cell(regions, written, "size_bytes")
	ps := c.int("kernel.page_size_bytes")
	if !c.failed() && ps <= 0 {
		c.fail("kernel.page_size_bytes is %d, not a page size", ps)
	}
	var pages any
	if !c.failed() {
		pages = size / ps
	}
	out = append(out, c.fact("derived.probe.written_pages_expected", "size_bytes of anon-written / kernel.page_size_bytes: one fault per page if every page faulted separately", pages))

	c = in.calc()
	delta := c.int("probe.faults.minflt_after_touch") - c.int("probe.faults.minflt_before_touch")
	out = append(out, c.fact("derived.probe.written_minflt_delta", "minflt_after_touch − minflt_before_touch (getrusage around the write loop)", delta))

	c = in.calc()
	written = c.row(regions, "name", "anon-written")
	huge, size := c.cell(regions, written, "anon_huge_bytes"), c.cell(regions, written, "size_bytes")
	if !c.failed() && size <= 0 {
		c.fail("the anon-written region's size_bytes is %d, so a fraction of it is undefined", size)
	}
	var frac any
	if !c.failed() {
		frac = float64(huge) / float64(size)
	}
	out = append(out, c.fact("derived.probe.written_huge_fraction", "anon_huge_bytes / size_bytes of the anon-written region", frac))

	c = in.calc()
	rss = c.cell(regions, c.row(regions, "name", "anon-read"), "rss_bytes")
	out = append(out, c.fact("derived.probe.read_region_rss_bytes", "rss_bytes of the anon-read region (4 MiB only read from)", rss))

	c = in.calc()
	runs := c.table("probe.layout.runs")
	if !c.failed() && len(runs) < 2 {
		c.fail("probe.layout.runs has %d runs, two are needed to compare", len(runs))
	}
	var differs any
	var changed []string
	if !c.failed() {
		for _, col := range []string{"stack", "mmap_base", "heap", "exe"} {
			if fmt.Sprint(runs[0][col]) != fmt.Sprint(runs[1][col]) {
				changed = append(changed, col)
			}
		}
		differs = len(changed) > 0
	}
	rule := "any of stack, mmap_base, heap, exe differs between the probe's two runs"
	if len(changed) > 0 {
		rule += fmt.Sprintf(" (here: %v)", changed)
	}
	out = append(out, c.fact("derived.probe.aslr_differs", rule, differs))
	return out
}
