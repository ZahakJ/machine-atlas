//go:build linux

package probe

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// childEnv marks a re-executed test binary as a probe child. In tests the
// executable is the test binary, which has no "__probe" verb, so TestMain
// plays the part of atlas's verb dispatch.
const childEnv = "ATLAS_PROBE_TEST_CHILD=1"

func TestMain(m *testing.M) {
	if os.Getenv("ATLAS_PROBE_TEST_CHILD") == "1" {
		os.Exit(ChildMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func testCollector() probeCollector {
	return probeCollector{
		command: func() ([]string, error) { return []string{os.Args[0]}, nil },
		env:     []string{childEnv},
	}
}

// The live probe on this machine. Only what Linux guarantees on every
// machine is asserted: THP can make the fault delta far smaller than the
// page count, and the anon-read region may or may not show resident memory
// (zero page, huge zero page), so neither is checked.
func TestLiveProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("starts two child processes")
	}
	got := collectProbe(t, collect.LiveEnv{}, testCollector())
	for id, o := range got {
		// A kernel built without THP has no THP counters to read.
		if o.Status != obs.StatusOK && !((id == idTHPAlloc || id == idTHPFall) && o.Status == obs.StatusAbsent) {
			t.Fatalf("%s: %s (%s)", id, o.Status, o.Note)
		}
	}
	ps := int64(os.Getpagesize())
	sizes := map[string]int64{"anon-untouched": 64 << 20, "anon-written": 8 << 20, "anon-read": 4 << 20, "shmem-shared": 4 << 20}
	for _, r := range got[idRegions].Value.([]map[string]any) {
		name := r["name"].(string)
		size := r["size_bytes"].(int64)
		if want, ok := sizes[name]; ok && size != want {
			t.Errorf("%s: size %d, want %d", name, size, want)
		}
		if r["pages_total"].(int64)*ps != size {
			t.Errorf("%s: pages_total %d × %d != size %d", name, r["pages_total"], ps, size)
		}
		switch name {
		case "anon-untouched":
			if r["rss_bytes"] != int64(0) || r["pages_present"] != int64(0) {
				t.Errorf("untouched region is resident: rss %v, present %v", r["rss_bytes"], r["pages_present"])
			}
		case "anon-written":
			if r["pages_present"] != r["pages_total"] || r["rss_bytes"] != size {
				t.Errorf("written region: present %v of %v, rss %v", r["pages_present"], r["pages_total"], r["rss_bytes"])
			}
		case "file-private":
			if size <= 0 || size > fileMapMax || size%ps != 0 {
				t.Errorf("file-private size %d", size)
			}
		}
	}
	before := got["probe.faults.minflt_before_touch"].Value.(int64)
	after := got["probe.faults.minflt_after_touch"].Value.(int64)
	if after <= before {
		t.Errorf("minflt %d -> %d: writing fresh memory must fault", before, after)
	}
	runs := got[idLayout].Value.([]map[string]any)
	if len(runs) != 2 {
		t.Errorf("%d layout runs, want 2: %s", len(runs), got[idLayout].Note)
	}
	for _, r := range runs {
		// stack is the start of the [stack] VMA, so page-aligned.
		if a, err := parseHex(r["stack"].(string)); err != nil || a%uint64(ps) != 0 {
			t.Errorf("run %v: stack %v is not a page-aligned VMA start", r["run"], r["stack"])
		}
		if _, hasHeap := r["heap"]; hasHeap && r["heap_kind"] != HeapBrk && r["heap_kind"] != HeapGoArena {
			t.Errorf("run %v: heap_kind %v", r["run"], r["heap_kind"])
		}
	}
}

func TestChildUsage(t *testing.T) {
	if code := ChildMain([]string{"bogus"}); code != 2 {
		t.Errorf("ChildMain(bogus) = %d, want 2", code)
	}
}

// A child that cannot start leaves every id failed, with the reason.
func TestLiveProbeStartFailure(t *testing.T) {
	c := probeCollector{command: func() ([]string, error) { return []string{"/nonexistent/atlas", Verb}, nil }}
	for id, o := range collectProbe(t, collect.LiveEnv{}, c) {
		if o.Status != obs.StatusFailed || o.Note == "" {
			t.Errorf("%s: %s (%s), want failed", id, o.Status, o.Note)
		}
	}
}

// The fallback for kernels that cannot name mappings: two adjacent
// anonymous regions on 2 MiB boundaries, separate VMAs thanks to guards.
func TestMapGuarded(t *testing.T) {
	ps := os.Getpagesize()
	const rw, anon = unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANONYMOUS
	specs := []regionSpec{{"a", "anon", -1, 4 * mib, rw, anon, 0}, {"b", "anon", -1, 2 * mib, rw, anon, 0}}
	out, err := mapGuarded(specs, ps)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	vmas, err := parseMaps(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out {
		start := uint64(uintptr(unsafe.Pointer(&r.mem[0])))
		if start%regionAlign != 0 {
			t.Errorf("%s at %#x is not on a 2 MiB boundary", r.name, start)
		}
		exact := false
		for _, v := range vmas {
			exact = exact || (v.Start == start && v.End == start+uint64(len(r.mem)))
		}
		if !exact {
			t.Errorf("%s is not a VMA of its own", r.name)
		}
	}
}
