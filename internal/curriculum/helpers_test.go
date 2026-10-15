package curriculum

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// missing marks an observation present with a non-ok status.
type missing obs.Status

// snap builds a snapshot from id → value (or missing(status)). Collectors
// and fixture roots are written elsewhere, so tests describe machines
// directly. Units and sources come from the catalog, as the runner would
// fill them.
func snap(t *testing.T, vals map[string]any) *snapshot.Snapshot {
	t.Helper()
	s := &snapshot.Snapshot{}
	for _, id := range slices.Sorted(maps.Keys(vals)) {
		o := obs.Observation{ID: id, Status: obs.StatusOK, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
			Source: obs.File("/proc/example", id), CollectedAt: "2026-01-15T12:00:00Z", Collector: "test"}
		if e, ok := catalog.Lookup(id); ok {
			o.Unit = e.Unit
		}
		if m, ok := vals[id].(missing); ok {
			o.Status = obs.Status(m)
		} else {
			v, err := obs.Normalize(vals[id])
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			o.Value = v
		}
		s.Observations = append(s.Observations, o)
	}
	return s
}

const gib = int64(1) << 30

// x86Desktop is a single-node x86-64 machine with swap and THP always.
func x86Desktop() map[string]any {
	return map[string]any{
		"kernel.arch": "x86_64", "kernel.page_size_bytes": 4096,
		"cpu.model_name": "Example CPU 9000", "cpu.address_bits.virtual": 48, "cpu.address_bits.physical": 39,
		"cpu.flags.la57": false, "cpu.flags.pdpe1gb": true, "cpu.flags.hypervisor": false, "cpu.threads_per_core": 2,
		"virt.container.dockerenv": false, "virt.container.containerenv": false, "virt.pid1_cgroup_root": true,
		"init.pid1_comm": "systemd", "virt.dmi.sys_vendor": "Example Computers Ltd.",
		"mem.meminfo.MemTotal": 16 * gib, "mem.meminfo.MemAvailable": 10 * gib, "mem.meminfo.MemFree": 2 * gib,
		"mem.meminfo.SwapTotal": 8 * gib, "mem.meminfo.SwapFree": 6 * gib,
		"mem.zswap.enabled": false, "mem.zram.devices": []map[string]any{},
		"mem.thp.enabled": "always", "mem.lru_gen.enabled": "0x0007",
		"mem.numa.nodes": []map[string]any{{"node": 0, "mem_total_bytes": 16 * gib, "mem_free_bytes": 2 * gib, "cpulist": "0-15"}},
		"mem.zones": []map[string]any{
			{"node": 0, "zone": "DMA32", "free": 1000, "min": 10, "low": 20, "high": 30, "spanned": 1048576, "present": 1000000, "managed": 990000},
			{"node": 0, "zone": "Normal", "free": 3000, "min": 100, "low": 200, "high": 300, "spanned": 3145728, "present": 3100000, "managed": 3000000},
		},
		"mem.swap.areas":         []map[string]any{{"name": "nvme0n1p3", "type": "partition", "size_bytes": 8 * gib, "used_bytes": 2 * gib, "priority": -2}},
		"block.devices":          []map[string]any{{"name": "nvme0n1p3", "size_bytes": 8 * gib, "rotational": false, "removable": false, "type": "partition"}},
		"probe.maps.highest_end": "0x7ffd5e8c4000",
	}
}

// numaNoSwap is a two-node machine without swap.
func numaNoSwap() map[string]any {
	v := x86Desktop()
	v["mem.meminfo.SwapTotal"], v["mem.meminfo.SwapFree"] = 0, 0
	v["mem.swap.areas"] = []map[string]any{}
	v["mem.numa.nodes"] = []map[string]any{
		{"node": 0, "mem_total_bytes": 64 * gib, "mem_free_bytes": 8 * gib, "cpulist": "0-31"},
		{"node": 1, "mem_total_bytes": 64 * gib, "mem_free_bytes": 9 * gib, "cpulist": "32-63"},
	}
	v["mem.zones"] = []map[string]any{
		{"node": 0, "zone": "Normal", "free": 2000, "min": 100, "low": 200, "high": 300},
		{"node": 1, "zone": "Normal", "free": 2500, "min": 100, "low": 200, "high": 300},
	}
	v["mem.thp.enabled"] = "never"
	return v
}

// lessonDoc assembles a lesson file from front matter fields and a body.
func lessonDoc(meta map[string]any, body string) string {
	m := map[string]any{
		"id": "alpha", "title": "Alpha", "subsystem": "memory", "kind": "core", "order": 1,
		"summary": "A test lesson.", "prerequisites": []string{}, "uses": []string{},
		"relevance": []any{}, "next": []string{}, "deeper": []string{}, "version_notes": []any{},
		"references": []any{map[string]string{"title": "Doc", "url": "https://docs.kernel.org/", "kind": "kernel-doc"}},
	}
	maps.Copy(m, meta)
	js, _ := json.MarshalIndent(m, "", "  ")
	return "---\n" + string(js) + "\n---\n" + body
}

// fullBody has every required section with a :::predict and a :::cannot,
// and extra inserted into How it works.
func fullBody(extra string) string {
	return strings.Join([]string{
		"## Question", "Why?", "",
		"## Why it exists", "Because.", "",
		"## How it works", extra, "",
		"## On this machine", "Here.", "",
		"## Predict", ":::predict", "Guess.", "", "- a", "- b", ":::reveal", "It was a.", ":::end", "",
		"## Try it", "Look.", "",
		"## Limits", ":::cannot", "Nothing certain.", ":::end", "",
		"## Go deeper", "More.", "",
	}, "\n")
}

// minimalRoadmap has the nine subsystems, a memory track, and the planned
// concepts given for memory.
func minimalRoadmap(planned string) string {
	ids := []string{"cpu", "memory", "devices-and-drivers", "storage", "boot-and-init", "processes", "networking", "graphics-and-desktop", "distribution"}
	var subs []string
	for _, id := range ids {
		pc, track := "[]", ""
		if id == "memory" {
			pc = planned
			track = `"track":{"title":"Memory","order":1,"lede":"L"},`
		}
		subs = append(subs, `{"id":"`+id+`","title":"T","layer":"kernel","status":"overview","summary":"S","memory_link":"M","observe":[],`+track+`"planned_concepts":`+pc+`}`)
	}
	return `{"curriculum_version":"test","subsystems":[` + strings.Join(subs, ",") + `]}`
}

// testFS builds a curriculum file system from lesson files and optional
// roadmap planned concepts and components.
func testFS(lessons map[string]string, planned, components string) fstest.MapFS {
	if planned == "" {
		planned = "[]"
	}
	if components == "" {
		components = "[]"
	}
	fsys := fstest.MapFS{
		"roadmap.json":    {Data: []byte(minimalRoadmap(planned))},
		"components.json": {Data: []byte(components)},
	}
	for name, text := range lessons {
		fsys["lessons/"+name+".md"] = &fstest.MapFile{Data: []byte(text)}
	}
	return fsys
}

func bindJSON(t *testing.T, l *Lesson, vals map[string]any) ([]byte, *BoundLesson) {
	t.Helper()
	s := snap(t, vals)
	b := Bind(l, s, derive.All(s))
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return data, b
}

func jsonOf(v any) ([]byte, error) { return json.Marshal(v) }
