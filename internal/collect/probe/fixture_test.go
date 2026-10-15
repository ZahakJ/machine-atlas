package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// sampleRegion is one invented probe region with the smaps figures a kernel
// with THP enabled might report for it, in KiB.
type sampleRegion struct {
	name, start, end, perms, path string
	rss, anon, privDirty, shDirty int
	huge                          int
}

var sampleRegions = []sampleRegion{
	{"anon-untouched", "7f3a00000000", "7f3a04000000", "rw-p", "[anon: atlas probe: anon-untouched]", 0, 0, 0, 0, 0},
	{"anon-written", "7f3a04200000", "7f3a04a00000", "rw-p", "[anon: atlas probe: anon-written]", 8192, 8192, 8192, 0, 8192},
	{"anon-read", "7f3a04c00000", "7f3a05000000", "rw-p", "[anon: atlas probe: anon-read]", 0, 0, 0, 0, 0},
	{"file-private", "7f3a05200000", "7f3a05600000", "r--p", "/opt/tools/bin/atlas", 4096, 0, 0, 0, 0},
	{"shmem-shared", "7f3a05800000", "7f3a05c00000", "rw-s", "/memfd:atlas-probe (deleted)", 1024, 0, 1024, 0, 0},
}

func sampleReport() string {
	return `{"regions":[` +
		`{"name":"anon-untouched","start":"0x7f3a00000000","end":"0x7f3a04000000","size_bytes":67108864,"touched_bytes":0,"backing":"anon","pages_total":16384,"pages_present":0,"pages_swapped":0},` +
		`{"name":"anon-written","start":"0x7f3a04200000","end":"0x7f3a04a00000","size_bytes":8388608,"touched_bytes":8388608,"backing":"anon","pages_total":2048,"pages_present":2048,"pages_swapped":0},` +
		`{"name":"anon-read","start":"0x7f3a04c00000","end":"0x7f3a05000000","size_bytes":4194304,"touched_bytes":4194304,"backing":"anon","pages_total":1024,"pages_present":1024,"pages_swapped":0},` +
		`{"name":"file-private","start":"0x7f3a05200000","end":"0x7f3a05600000","size_bytes":4194304,"touched_bytes":4194304,"backing":"file","pages_total":1024,"pages_present":1024,"pages_swapped":0},` +
		`{"name":"shmem-shared","start":"0x7f3a05800000","end":"0x7f3a05c00000","size_bytes":4194304,"touched_bytes":1048576,"backing":"shmem","pages_total":1024,"pages_present":256,"pages_swapped":0}],` +
		`"faults":{"minflt_before_touch":301,"minflt_after_touch":305,"majflt_before_touch":0,"majflt_after_touch":0},` +
		`"thp_faults":{"fault_alloc_delta":4,"fault_fallback_delta":0},` +
		`"pagemap":{"readable":true,"pfn_visible":false},"placement":"mmap",` +
		`"layout_runs":[{"run":1,"stack":"0x7ffd5a3c1000","mmap_base":"0x7f3a2c040000","heap":"0x3a51c8400000","heap_kind":"go-arena","exe":"0x400000"},` +
		`{"run":2,"stack":"0x7ffc11a20000","mmap_base":"0x7f61b0e2a000","heap":"0x1c07a2400000","heap_kind":"go-arena","exe":"0x400000"}]}` + "\n"
}

// sampleSmaps renders the sample regions, with a guard mapping between each
// and the executable at the bottom, as smaps text.
func sampleSmaps() string {
	var b strings.Builder
	b.WriteString("00400000-004ab000 r-xp 00000000 103:02 1234567                            /opt/tools/bin/atlas\nSize: 684 kB\nRss: 684 kB\n")
	for _, r := range sampleRegions {
		inode := "0"
		if r.path != "" && !strings.HasPrefix(r.path, "[") {
			inode = "2048"
		}
		fmt.Fprintf(&b, "%s-%s %s 00000000 00:01 %s %s\n", r.start, r.end, r.perms, inode, r.path)
		fmt.Fprintf(&b, "Rss: %d kB\nPss: %d kB\nShared_Clean: 0 kB\nShared_Dirty: %d kB\nPrivate_Clean: %d kB\nPrivate_Dirty: %d kB\n",
			r.rss, r.rss, r.shDirty, r.rss-r.privDirty-r.shDirty, r.privDirty)
		fmt.Fprintf(&b, "Anonymous: %d kB\nAnonHugePages: %d kB\nSwap: 0 kB\nTHPeligible: 1\nVmFlags: rd wr mr mw me ac\n", r.anon, r.huge)
	}
	b.WriteString("7ffd5a3c1000-7ffd5a3e2000 rw-p 00000000 00:00 0                          [stack]\nRss: 16 kB\n")
	return b.String()
}

// sampleMapsFromSmaps keeps only the mapping lines of smaps.
func sampleMapsFromSmaps() string {
	var b strings.Builder
	for _, line := range strings.Split(sampleSmaps(), "\n") {
		if _, err := parseMapsLine(line); err == nil {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

const sampleStatus = "Name:\tatlas\nVmPeak:\t 1392000 kB\nVmSize:\t 1360304 kB\nVmRSS:\t   21140 kB\nRssAnon:\t   12640 kB\nRssFile:\t    4100 kB\nRssShmem:\t    4400 kB\nVmPTE:\t     120 kB\nVmSwap:\t       0 kB\nThreads:\t5\n"

func probeFiles() map[string]string {
	return map[string]string{
		"_probe/report.json": sampleReport(),
		"_probe/maps":        sampleMapsFromSmaps(),
		"_probe/smaps":       sampleSmaps(),
		"_probe/status":      sampleStatus,
	}
}

// fixture writes a root with files (relative paths) and opens it.
func fixture(t *testing.T, files map[string]string, deny ...string) *collect.FixtureEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "probe-root")
	meta, _ := json.Marshal(map[string]any{
		"description": "test", "now": "2026-01-15T12:00:00Z", "page_size": 4096,
		"uname": map[string]string{"machine": "x86_64", "release": "6.8.0"}, "deny": deny,
	})
	files = maps.Clone(files)
	files["_meta.json"] = string(meta)
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, err := collect.OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.Close() })
	return env
}

// collectProbe runs the probe collector through the runner, which checks
// every id against the catalog and the collector's ownership.
func collectProbe(t *testing.T, env collect.Env, c collect.Collector) map[string]obs.Observation {
	t.Helper()
	rep, out := collect.Run(context.Background(), env, []collect.Collector{c})
	for _, r := range rep.Collectors {
		if r.Error != "" {
			t.Errorf("contract problem: %s", r.Error)
		}
	}
	byID := map[string]obs.Observation{}
	for _, o := range out {
		byID[o.ID] = o
	}
	if len(byID) != len(ids()) {
		t.Errorf("%d observations, want %d", len(byID), len(ids()))
	}
	return byID
}

func TestFixture(t *testing.T) {
	got := collectProbe(t, fixture(t, probeFiles()), Collector())
	for id, o := range got {
		if o.Status != obs.StatusOK {
			t.Errorf("%s: %s (%s)", id, o.Status, o.Note)
		}
	}
	rows := got[idRegions].Value.([]map[string]any)
	byName := map[string]map[string]any{}
	for _, r := range rows {
		byName[r["name"].(string)] = r
	}
	w := byName["anon-written"]
	if w["rss_bytes"] != int64(8<<20) || w["anon_huge_bytes"] != int64(8<<20) || w["pages_present"] != int64(2048) || w["perms"] != "rw-p" {
		t.Errorf("anon-written row: %v", w)
	}
	if p := byName["file-private"]["path"]; p != "atlas" {
		t.Errorf("file-private path %q, want the basename", p)
	}
	if p := byName["shmem-shared"]["path"]; p != "memfd:atlas-probe (deleted)" {
		t.Errorf("shmem path %q", p)
	}
	if got[idVMACount].Value != int64(7) || got[idLowest].Value != "0x400000" || got[idHighest].Value != "0x7ffd5a3e2000" {
		t.Errorf("maps: %v %v %v", got[idVMACount].Value, got[idLowest].Value, got[idHighest].Value)
	}
	if got["probe.status.VmRSS"].Value != int64(21140*1024) {
		t.Errorf("VmRSS = %v", got["probe.status.VmRSS"].Value)
	}
	if got["probe.faults.minflt_after_touch"].Value != int64(305) || got[idPFN].Value != false {
		t.Errorf("faults/pfn: %v %v", got["probe.faults.minflt_after_touch"].Value, got[idPFN].Value)
	}
	if got[idTHPAlloc].Value != int64(4) || got[idTHPFall].Value != int64(0) {
		t.Errorf("thp deltas: %v %v", got[idTHPAlloc].Value, got[idTHPFall].Value)
	}
	if runs := got[idLayout].Value.([]map[string]any); len(runs) != 2 || runs[1]["stack"] != "0x7ffc11a20000" || runs[1]["heap_kind"] != HeapGoArena {
		t.Errorf("layout runs: %v", runs)
	}
	for _, r := range got[idMapsSample].Value.([]map[string]any) {
		if strings.Contains(r["path"].(string), "/") {
			t.Errorf("maps sample keeps a path: %v", r["path"])
		}
	}
}

func TestFixtureWithoutProbe(t *testing.T) {
	got := collectProbe(t, fixture(t, map[string]string{"proc/meminfo": "MemTotal: 1 kB\n"}), Collector())
	for id, o := range got {
		if o.Status != obs.StatusNotCollected {
			t.Errorf("%s: %s, want not_collected", id, o.Status)
		}
	}
}

func TestFixtureDenied(t *testing.T) {
	got := collectProbe(t, fixture(t, probeFiles(), "/_probe/smaps", "/_probe/status"), Collector())
	if got[idRegions].Status != obs.StatusPermissionDenied || got["probe.status.VmRSS"].Status != obs.StatusPermissionDenied {
		t.Errorf("regions %s, VmRSS %s; want permission_denied", got[idRegions].Status, got["probe.status.VmRSS"].Status)
	}
	if got[idVMACount].Status != obs.StatusOK || got[idLayout].Status != obs.StatusOK {
		t.Error("readable parts must stay ok")
	}
}

// Each malformed part fails exactly the ids computed from it.
func TestFixtureMalformedParts(t *testing.T) {
	tests := []struct {
		file, content string
		failed        []string
		ok            []string
	}{
		{"_probe/smaps", "garbage\n", []string{idRegions}, []string{idVMACount, "probe.status.VmRSS"}},
		{"_probe/smaps", strings.Replace(sampleSmaps(), "Pss: 8192 kB\n", "", 1), []string{idRegions}, []string{idLayout}},
		{"_probe/maps", "00400000-004ab000 r-xp 0000", []string{idMapsSample, idVMACount, idLowest, idHighest}, []string{idRegions}},
		{"_probe/status", "VmRSS 12 kB\n", []string{"probe.status.VmRSS", "probe.status.VmPTE"}, []string{idRegions}},
		{"_probe/report.json", `{"regions":[`, []string{idRegions, idPFN, idLayout, "probe.faults.minflt_after_touch"}, []string{idVMACount}},
	}
	for _, tt := range tests {
		files := probeFiles()
		files[tt.file] = tt.content
		got := collectProbe(t, fixture(t, files), Collector())
		for _, id := range tt.failed {
			if o := got[id]; o.Status != obs.StatusFailed || o.Value != nil || o.Note == "" {
				t.Errorf("%s malformed: %s is %s (%s), want failed with a note", tt.file, id, o.Status, o.Note)
			}
		}
		for _, id := range tt.ok {
			if got[id].Status != obs.StatusOK {
				t.Errorf("%s malformed: %s is %s, want ok", tt.file, id, got[id].Status)
			}
		}
	}
}

// A status file from a kernel before 4.5 has no RssAnon/RssFile/RssShmem.
func TestFixtureOldStatus(t *testing.T) {
	files := probeFiles()
	files["_probe/status"] = "Name:\tatlas\nVmSize:\t 1360304 kB\nVmRSS:\t   21140 kB\nVmPTE:\t     120 kB\nVmSwap:\t       0 kB\n"
	got := collectProbe(t, fixture(t, files), Collector())
	if got["probe.status.RssAnon"].Status != obs.StatusAbsent || got["probe.status.VmRSS"].Status != obs.StatusOK {
		t.Errorf("RssAnon %s, VmRSS %s", got["probe.status.RssAnon"].Status, got["probe.status.VmRSS"].Status)
	}
}

func TestPagemapUnreadable(t *testing.T) {
	files := probeFiles()
	r := sampleReport()
	r = strings.Replace(r, `"readable":true`, `"readable":false`, 1)
	r = strings.NewReplacer(`,"pages_present":0`, "", `,"pages_present":2048`, "", `,"pages_present":1024`, "", `,"pages_present":256`, "", `,"pages_swapped":0`, "").Replace(r)
	files["_probe/report.json"] = r
	got := collectProbe(t, fixture(t, files), Collector())
	if got[idPFN].Status != obs.StatusFailed {
		t.Errorf("pfn_visible: %s", got[idPFN].Status)
	}
	for _, row := range got[idRegions].Value.([]map[string]any) {
		if _, ok := row["pages_present"]; ok {
			t.Errorf("pages_present invented for %v", row["name"])
		}
	}
}

// A report without thp_faults (a kernel without THP counters) gives absent
// deltas, never zeros; a negative delta or a heap without its kind is a
// malformed report.
func TestTHPFaultsAndHeapKind(t *testing.T) {
	files := probeFiles()
	files["_probe/report.json"] = strings.Replace(sampleReport(), `"thp_faults":{"fault_alloc_delta":4,"fault_fallback_delta":0},`, "", 1)
	got := collectProbe(t, fixture(t, files), Collector())
	for _, id := range []string{idTHPAlloc, idTHPFall} {
		if got[id].Status != obs.StatusAbsent || got[id].Value != nil {
			t.Errorf("%s without thp_faults: %s %v", id, got[id].Status, got[id].Value)
		}
	}
	for name, report := range map[string]string{
		"negative delta":    strings.Replace(sampleReport(), `"fault_alloc_delta":4`, `"fault_alloc_delta":-1`, 1),
		"heap without kind": strings.Replace(sampleReport(), `,"heap_kind":"go-arena"`, "", 1),
		"unknown heap kind": strings.Replace(sampleReport(), `"heap_kind":"go-arena"`, `"heap_kind":"sbrk"`, 1),
		"kind without heap": strings.Replace(sampleReport(), `"heap":"0x3a51c8400000",`, "", 1),
	} {
		if _, err := decodeReport([]byte(report), true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseTHPCounters(t *testing.T) {
	got, ok := parseTHPCounters([]byte("nr_free_pages 12\nthp_fault_alloc 35518\nthp_fault_fallback 1065\nthp_fault_fallback_charge 0\n"))
	if !ok || got != [2]int64{35518, 1065} {
		t.Errorf("got %v %v", got, ok)
	}
	if _, ok := parseTHPCounters([]byte("nr_free_pages 12\nthp_fault_alloc 3\n")); ok {
		t.Error("thp_fault_fallback missing: want not ok")
	}
}
