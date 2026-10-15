package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Report is what "atlas __probe" prints on stdout (ARCHITECTURE.md §12): the five
// regions with the child's own pagemap counts, its fault counters around
// the write loop, whether pagemap showed frame numbers, and its address
// layout. The minimal second execution prints only LayoutRuns.
type Report struct {
	Regions []Region `json:"regions,omitempty"`
	Faults  *Faults  `json:"faults,omitempty"`
	// THPFaults are the system-wide THP fault counters around the write
	// loop; nil when /proc/vmstat could not be read or lacks them (a
	// kernel without THP).
	THPFaults *THPFaults `json:"thp_faults,omitempty"`
	Pagemap   *Pagemap   `json:"pagemap,omitempty"`
	// Placement says how the regions were laid out: PlacementMmap or
	// PlacementGuarded. Empty in the minimal second execution.
	Placement  string      `json:"placement,omitempty"`
	LayoutRuns []LayoutRun `json:"layout_runs"`
}

// Region placements.
const (
	// PlacementMmap: one plain mmap per region, the kernel choosing the
	// address and alignment, anonymous regions named so they stay
	// separate VMAs.
	PlacementMmap = "mmap"
	// PlacementGuarded: a kernel without anonymous mapping names; the
	// probe placed the regions itself on 2 MiB boundaries between guard
	// pages.
	PlacementGuarded = "guarded"
)

// Region is one mapping the child made. Addresses are hex strings ("0x…")
// because JSON numbers cannot hold every 64-bit address exactly.
// PagesPresent and PagesSwapped are absent when pagemap could not be read:
// an unknown count is not zero.
type Region struct {
	Name         string `json:"name"`
	Start        string `json:"start"`
	End          string `json:"end"`
	SizeBytes    int64  `json:"size_bytes"`
	TouchedBytes int64  `json:"touched_bytes"`
	Backing      string `json:"backing"`
	PagesTotal   int64  `json:"pages_total"`
	PagesPresent *int64 `json:"pages_present,omitempty"`
	PagesSwapped *int64 `json:"pages_swapped,omitempty"`
}

// Faults are the child's getrusage counters just before and just after it
// wrote one byte to every page of anon-written.
type Faults struct {
	MinfltBefore int64 `json:"minflt_before_touch"`
	MinfltAfter  int64 `json:"minflt_after_touch"`
	MajfltBefore int64 `json:"majflt_before_touch"`
	MajfltAfter  int64 `json:"majflt_after_touch"`
}

// THPFaults are how much the system-wide /proc/vmstat counters
// thp_fault_alloc and thp_fault_fallback grew between a reading just before
// and one just after the write loop over anon-written (outside the
// getrusage window). They are system-wide: faults other processes took in
// the same instant are included.
type THPFaults struct {
	FaultAllocDelta    int64 `json:"fault_alloc_delta"`
	FaultFallbackDelta int64 `json:"fault_fallback_delta"`
}

// Pagemap says whether the child could read /proc/self/pagemap and whether
// any present page showed a non-zero physical frame number.
type Pagemap struct {
	Readable   bool `json:"readable"`
	PFNVisible bool `json:"pfn_visible"`
}

// LayoutRun is the address layout of one execution, for comparing runs
// under ASLR. Stack is the start of the [stack] VMA. Heap is empty when the
// process has no heap mapping to name; HeapKind says which mapping Heap is
// (HeapBrk or HeapGoArena) and is empty exactly when Heap is.
type LayoutRun struct {
	Run      int    `json:"run"`
	Stack    string `json:"stack"`
	MmapBase string `json:"mmap_base"`
	Heap     string `json:"heap,omitempty"`
	HeapKind string `json:"heap_kind,omitempty"`
	Exe      string `json:"exe"`
}

// Heap kinds.
const (
	// HeapBrk: Heap is the [heap] VMA, the brk area C malloc grows.
	HeapBrk = "brk"
	// HeapGoArena: Heap is the Go runtime's mmap'd heap arena.
	HeapGoArena = "go-arena"
)

// The regions, in the order the child makes them, with their backing.
var regionBacking = map[string]string{
	"anon-untouched": "anon",
	"anon-written":   "anon",
	"anon-read":      "anon",
	"file-private":   "file",
	"shmem-shared":   "shmem",
}

// maxReportBytes bounds what the parent reads from the child.
const maxReportBytes = 64 << 10

// decodeReport parses and checks a report. full requires everything a
// complete probe prints; otherwise only the layout is required, as the
// minimal second execution prints.
func decodeReport(data []byte, full bool) (*Report, error) {
	if len(data) > maxReportBytes {
		return nil, fmt.Errorf("report of %d bytes exceeds %d", len(data), maxReportBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r Report
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	if dec.More() {
		return nil, errors.New("report: trailing data after the JSON object")
	}
	if err := r.check(full); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	return &r, nil
}

func (r *Report) check(full bool) error {
	if len(r.LayoutRuns) == 0 {
		return errors.New("no layout_runs")
	}
	for i, l := range r.LayoutRuns {
		for _, a := range []string{l.Stack, l.MmapBase, l.Exe} {
			if _, err := parseHex(a); err != nil {
				return fmt.Errorf("layout_runs[%d]: %w", i, err)
			}
		}
		if l.Heap != "" {
			if _, err := parseHex(l.Heap); err != nil {
				return fmt.Errorf("layout_runs[%d]: heap: %w", i, err)
			}
		}
		switch {
		case l.Heap == "" && l.HeapKind != "":
			return fmt.Errorf("layout_runs[%d]: heap_kind %q without a heap", i, l.HeapKind)
		case l.Heap != "" && l.HeapKind != HeapBrk && l.HeapKind != HeapGoArena:
			return fmt.Errorf("layout_runs[%d]: heap_kind %q, want %q or %q", i, l.HeapKind, HeapBrk, HeapGoArena)
		}
	}
	if !full {
		return nil
	}
	if r.Faults == nil || r.Pagemap == nil {
		return errors.New("faults or pagemap missing")
	}
	if t := r.THPFaults; t != nil && (t.FaultAllocDelta < 0 || t.FaultFallbackDelta < 0) {
		return fmt.Errorf("thp_faults: negative delta %d/%d of counters that only grow", t.FaultAllocDelta, t.FaultFallbackDelta)
	}
	if r.Placement != PlacementMmap && r.Placement != PlacementGuarded {
		return fmt.Errorf("placement %q, want %q or %q", r.Placement, PlacementMmap, PlacementGuarded)
	}
	if len(r.Regions) != len(regionBacking) {
		return fmt.Errorf("%d regions, want %d", len(r.Regions), len(regionBacking))
	}
	seen := map[string]bool{}
	for _, g := range r.Regions {
		if err := g.check(); err != nil {
			return fmt.Errorf("region %q: %w", g.Name, err)
		}
		if seen[g.Name] {
			return fmt.Errorf("region %q twice", g.Name)
		}
		seen[g.Name] = true
	}
	return nil
}

func (g Region) check() error {
	backing, ok := regionBacking[g.Name]
	switch {
	case !ok:
		return errors.New("unknown region")
	case g.Backing != backing:
		return fmt.Errorf("backing %q, want %q", g.Backing, backing)
	}
	start, err := parseHex(g.Start)
	if err != nil {
		return err
	}
	end, err := parseHex(g.End)
	if err != nil {
		return err
	}
	switch {
	case end <= start || end-start != uint64(g.SizeBytes):
		return fmt.Errorf("start %s, end %s and size %d disagree", g.Start, g.End, g.SizeBytes)
	case g.PagesTotal <= 0 || g.SizeBytes%g.PagesTotal != 0:
		return fmt.Errorf("pages_total %d does not divide size %d", g.PagesTotal, g.SizeBytes)
	case g.TouchedBytes < 0 || g.TouchedBytes > g.SizeBytes:
		return fmt.Errorf("touched_bytes %d outside the region", g.TouchedBytes)
	}
	for _, p := range []*int64{g.PagesPresent, g.PagesSwapped} {
		if p != nil && (*p < 0 || *p > g.PagesTotal) {
			return fmt.Errorf("page count %d outside 0..%d", *p, g.PagesTotal)
		}
	}
	return nil
}

// parseHex parses an address written as "0x…".
func parseHex(s string) (uint64, error) {
	h, ok := strings.CutPrefix(s, "0x")
	if !ok || h == "" {
		return 0, fmt.Errorf("address %q is not 0x-prefixed hex", s)
	}
	v, err := strconv.ParseUint(h, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("address %q is not 0x-prefixed hex", s)
	}
	return v, nil
}

// hex formats an address as the report and the tables write it.
func hex(v uint64) string { return "0x" + strconv.FormatUint(v, 16) }
