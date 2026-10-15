package curriculum

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/derive"
)

// journeyFS is a small curriculum: three core lessons whose prerequisite
// order disagrees with their order numbers, a branch lesson on THP, and
// planned concepts with relevance rules.
func journeyFS(t *testing.T) *Curriculum {
	t.Helper()
	lesson := func(id, kind string, order int, prereq []string, relevance []any) string {
		return lessonDoc(map[string]any{"id": id, "kind": kind, "order": order, "prerequisites": prereq, "relevance": relevance}, fullBody("x"))
	}
	planned := `[
	  {"id":"numa-placement","title":"NUMA","summary":"s","prerequisites":["base"],
	   "relevance":[{"when":"derived.numa.multi","note":"More than one node."}]},
	  {"id":"compressed-swap","title":"Compressed","summary":"s","prerequisites":["base"],
	   "relevance":[{"when":"derived.swap.compressed","note":"zswap or zram."}]},
	  {"id":"memory-cgroups","title":"Cgroups","summary":"s","prerequisites":["base"],
	   "relevance":[{"when":"derived.cgroup.memory_limited","note":"Limited."},{"when":"derived.env.kind","is":"container","note":"Container."}]},
	  {"id":"swap-tuning","title":"Swap tuning","summary":"s","prerequisites":["base"],
	   "relevance":[{"when":"derived.swap.configured","note":"Swap is configured."}]}
	]`
	c, err := Load(testFS(map[string]string{
		"base":   lesson("base", "core", 3, nil, nil),
		"middle": lesson("middle", "core", 1, []string{"base"}, []any{map[string]any{"when": "derived.numa.multi", "note": "Several nodes."}}),
		"top":    lesson("top", "core", 2, []string{"middle"}, nil),
		"aside":  lesson("aside", "core", 2, []string{"base"}, nil),
		"thp":    lesson("thp", "branch", 0, []string{"middle"}, []any{map[string]any{"when": "derived.thp.enabled", "note": "THP is on."}}),
	}, planned, ""))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ids(opts []Option) []string {
	out := []string{}
	for _, o := range opts {
		out = append(out, o.ID)
	}
	return out
}

func TestSelect(t *testing.T) {
	c := journeyFS(t)
	tests := []struct {
		name               string
		vals               map[string]any
		branchesRelevant   []string
		plannedRelevant    []string
		middleNoteMatched  bool
		unknownPlannedNote string
	}{
		{"single node with swap", x86Desktop(), []string{"thp"}, []string{"swap-tuning"}, false, ""},
		{"two nodes without swap", numaNoSwap(), []string{}, []string{"numa-placement"}, true, ""},
	}
	for _, tt := range tests {
		s := snap(t, tt.vals)
		j := c.Select(s, derive.All(s))
		var core []string
		for _, l := range j.Core {
			core = append(core, l.ID)
		}
		// base first (prerequisite of all); then middle (order 1) before
		// aside (order 2) and top (order 2, after middle); aside < top by id.
		if want := []string{"base", "middle", "aside", "top"}; !slices.Equal(core, want) {
			t.Errorf("%s: core order %v, want %v", tt.name, core, want)
		}
		if got := ids(j.Branches.Relevant); !slices.Equal(got, tt.branchesRelevant) {
			t.Errorf("%s: relevant branches %v, want %v", tt.name, got, tt.branchesRelevant)
		}
		if len(j.Branches.Relevant)+len(j.Branches.Available) != 1 {
			t.Errorf("%s: the branch lesson must be listed exactly once", tt.name)
		}
		if got := ids(j.Planned.Relevant); !slices.Equal(got, tt.plannedRelevant) {
			t.Errorf("%s: relevant planned %v, want %v", tt.name, got, tt.plannedRelevant)
		}
		if len(j.Planned.Relevant)+len(j.Planned.Available) != 4 {
			t.Errorf("%s: planned concepts must all be listed", tt.name)
		}
		note := j.Core[1].Notes[0]
		if note.Matched != tt.middleNoteMatched || note.When != "derived.numa.multi" || note.Status != "ok" || note.Value == "" {
			t.Errorf("%s: middle note %+v", tt.name, note)
		}
	}
}

func TestSelectUnknownFactDoesNotMatch(t *testing.T) {
	c := journeyFS(t)
	vals := x86Desktop()
	vals["mem.cgroup.memory.max"] = missing("permission_denied")
	vals["mem.cgroup.memory.high"] = missing("permission_denied")
	vals["virt.container.dockerenv"] = missing("permission_denied")
	s := snap(t, vals)
	j := c.Select(s, derive.All(s))
	i := slices.IndexFunc(j.Planned.Available, func(o Option) bool { return o.ID == "memory-cgroups" })
	if i < 0 {
		t.Fatalf("memory-cgroups should be available, not relevant: %+v", j.Planned)
	}
	for _, n := range j.Planned.Available[i].Notes {
		if n.Matched || n.Status != "unknown" || !strings.Contains(n.Reason, "could not be determined") || n.Value != "not determined" {
			t.Errorf("unknown note %+v", n)
		}
	}
}

func TestSelectIsDeterministic(t *testing.T) {
	c := journeyFS(t)
	s := snap(t, numaNoSwap())
	a, _ := json.Marshal(c.Select(s, derive.All(s)))
	b, _ := json.Marshal(c.Select(s, derive.All(s)))
	if !bytes.Equal(a, b) {
		t.Fatal("selection differs between runs")
	}
	g := c.Select(s, derive.All(s)).Graph
	if len(g.Nodes) != 9 || !slices.Contains(g.Edges, GraphEdge{From: "base", To: "middle"}) || !slices.Contains(g.Edges, GraphEdge{From: "base", To: "numa-placement"}) {
		t.Errorf("graph %+v", g)
	}
}

func TestLoadRejectsCycle(t *testing.T) {
	doc := func(id, pre string) string {
		return lessonDoc(map[string]any{"id": id, "prerequisites": []string{pre}}, fullBody("x"))
	}
	_, err := Load(testFS(map[string]string{"a": doc("a", "b"), "b": doc("b", "c"), "c": doc("c", "a")}, "", ""))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Load = %v, want a cycle error", err)
	}
}

// Core lessons are grouped by track in track order, whatever their order
// numbers, and a cross-track prerequisite still holds.
func TestSelectTracks(t *testing.T) {
	lesson := func(id, sub string, order int, prereq []string) string {
		return lessonDoc(map[string]any{"id": id, "subsystem": sub, "order": order, "prerequisites": prereq}, fullBody("x"))
	}
	fsys := testFS(map[string]string{
		"m1": lesson("m1", "memory", 2, nil),
		"m2": lesson("m2", "memory", 5, []string{"m1"}),
		"c1": lesson("c1", "cpu", 1, []string{"m1"}),
		"c2": lesson("c2", "cpu", 2, nil),
	}, "", "")
	road := strings.Replace(minimalRoadmap("[]"), `{"id":"cpu","title":"T","layer":"kernel","status":"overview","summary":"S","memory_link":"M","observe":[],`,
		`{"id":"cpu","title":"T","layer":"kernel","status":"overview","summary":"S","memory_link":"M","observe":[],"track":{"title":"CPU","order":2,"lede":"C"},`, 1)
	fsys["roadmap.json"].Data = []byte(road)
	c, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if probs := Lint(fsys, LintOptions{}); len(probs) != 0 {
		t.Fatalf("lint: %v", probs)
	}
	j := c.Select(snap(t, nil), nil)
	var core []string
	for _, l := range j.Core {
		core = append(core, l.ID)
	}
	if want := []string{"m1", "m2", "c1", "c2"}; !slices.Equal(core, want) {
		t.Errorf("core = %v, want %v", core, want)
	}
	if len(j.Tracks) != 2 || j.Tracks[0].ID != "memory" || j.Tracks[1].Title != "CPU" ||
		!slices.Equal(j.Tracks[1].Lessons, []string{"c1", "c2"}) {
		t.Errorf("tracks = %+v", j.Tracks)
	}
}

func TestLintTracks(t *testing.T) {
	fsys := testFS(map[string]string{"alpha": lessonDoc(map[string]any{"subsystem": "storage"}, fullBody("x"))}, "", "")
	if probs := Lint(fsys, LintOptions{}); len(probs) != 1 || !strings.Contains(probs[0].Message, "has no track") {
		t.Errorf("core lesson outside a track: %v", probs)
	}
	fsys = testFS(map[string]string{"alpha": lessonDoc(nil, fullBody("x"))}, "", "")
	fsys["roadmap.json"].Data = []byte(strings.Replace(string(fsys["roadmap.json"].Data), `"lede":"L"`, `"lede":""`, 1))
	if probs := Lint(fsys, LintOptions{}); len(probs) != 1 || probs[0].Rule != RuleRoadmap {
		t.Errorf("track without a lede: %v", probs)
	}
}
