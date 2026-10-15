package curriculum

import (
	"slices"
	"strings"
	"testing"
)

const validExtra = "Runs on {{obs:kernel.arch}}. See [the MMU](component:cpu-mmu)."

const validComponents = `[{"id":"cpu-mmu","title":"MMU","layer":"hardware","subsystem":"cpu","status":"overview",
  "what":"w","why":"y","varies":"v","depends_on":[],"observe":["kernel.arch"],"lessons":["alpha"],
  "instances":{"from":"cpu.caches","key":"level,type","title":"L{level}"}}]`

func validLesson(meta map[string]any, body string) string {
	m := map[string]any{"uses": []string{"kernel.arch"}}
	for k, v := range meta {
		m[k] = v
	}
	return lessonDoc(m, body)
}

func TestLintAcceptsValidCurriculum(t *testing.T) {
	problems := Lint(testFS(map[string]string{"alpha": validLesson(nil, fullBody(validExtra))}, "", validComponents), LintOptions{})
	if len(problems) > 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
}

func TestLintRules(t *testing.T) {
	noCannot := strings.Replace(fullBody(validExtra), ":::cannot\nNothing certain.\n:::end", "Nothing certain.", 1)
	noPredict := strings.Replace(fullBody(validExtra), ":::predict\nGuess.\n\n- a\n- b\n:::reveal\nIt was a.\n:::end", "Guess.", 1)
	noTry := strings.Replace(fullBody(validExtra), "## Try it\nLook.\n", "", 1)
	tests := []struct {
		name       string
		lessons    map[string]string
		roadmap    string // replaces the whole roadmap when set
		components string
		rule       string
	}{
		{"syntax error", map[string]string{"alpha": validLesson(nil, fullBody(":::when"))}, "", "", RuleParse},
		{"unknown section", map[string]string{"alpha": validLesson(nil, "## Background\n")}, "", "", RuleParse},
		{"id differs from file", map[string]string{"alpha": validLesson(map[string]any{"id": "beta"}, fullBody(validExtra))}, "", "", RuleFrontMatter},
		{"bad kind", map[string]string{"alpha": validLesson(map[string]any{"kind": "extra"}, fullBody(validExtra))}, "", "", RuleFrontMatter},
		{"unknown subsystem", map[string]string{"alpha": validLesson(map[string]any{"subsystem": "plumbing"}, fullBody(validExtra))}, "", "", RuleFrontMatter},
		{"http reference", map[string]string{"alpha": validLesson(map[string]any{"references": []any{map[string]string{"title": "x", "url": "http://x", "kind": "spec"}}}, fullBody(validExtra))}, "", "", RuleFrontMatter},
		{"relevance on unknown id", map[string]string{"alpha": validLesson(map[string]any{"relevance": []any{map[string]string{"when": "derived.nothing", "note": "n"}}}, fullBody(validExtra))}, "", "", RuleCatalog},
		{"missing section", map[string]string{"alpha": validLesson(nil, noTry)}, "", "", RuleSection},
		{"cycle", map[string]string{
			"alpha": validLesson(map[string]any{"prerequisites": []string{"beta"}}, fullBody(validExtra)),
			"beta":  validLesson(map[string]any{"id": "beta", "prerequisites": []string{"alpha"}}, fullBody(validExtra)),
		}, "", "", RuleGraph},
		{"dangling prerequisite", map[string]string{"alpha": validLesson(map[string]any{"prerequisites": []string{"nope"}}, fullBody(validExtra))}, "", "", RuleDangling},
		{"dangling planned", map[string]string{"alpha": validLesson(map[string]any{"deeper": []string{"planned:nope"}}, fullBody(validExtra))}, "", "", RuleDangling},
		{"link to missing lesson", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+" [x](lesson:nope)"))}, "", "", RuleDangling},
		{"link to missing component", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+" [x](component:nope)"))}, "", "", RuleDangling},
		{"id used but not declared", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+" {{obs:kernel.release}}"))}, "", "", RuleUses},
		{"id declared but not used", map[string]string{"alpha": validLesson(map[string]any{"uses": []string{"kernel.arch", "kernel.release"}}, fullBody(validExtra))}, "", "", RuleUses},
		{"id not in catalog", map[string]string{"alpha": validLesson(map[string]any{"uses": []string{"kernel.arch", "kernel.flux"}}, fullBody(validExtra+" {{obs:kernel.flux}}"))}, "", "", RuleCatalog},
		{"table of a scalar", map[string]string{"alpha": validLesson(map[string]any{"uses": []string{"kernel.arch"}}, fullBody(validExtra+"\n:::table kernel.arch"))}, "", "", RuleCatalog},
		{"undeclared column", map[string]string{"alpha": validLesson(map[string]any{"uses": []string{"kernel.arch", "mem.zones"}}, fullBody(validExtra+"\n:::table mem.zones columns=node,colour"))}, "", "", RuleCatalog},
		{"unknown diagram", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+"\n:::diagram pie-chart"))}, "", "", RuleName},
		{"unknown sim", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+"\n:::sim weather"))}, "", "", RuleName},
		{"unknown experiment", map[string]string{"alpha": validLesson(nil, fullBody(validExtra+"\n:::experiment fork-bomb"))}, "", "", RuleName},
		{"core without cannot", map[string]string{"alpha": validLesson(nil, noCannot)}, "", "", RuleCore},
		{"core without predict", map[string]string{"alpha": validLesson(nil, noPredict)}, "", "", RuleCore},
		{"core without references", map[string]string{"alpha": validLesson(map[string]any{"references": []any{}}, fullBody(validExtra))}, "", "", RuleCore},
		{"too few subsystems", map[string]string{"alpha": validLesson(nil, fullBody(validExtra))},
			`{"curriculum_version":"t","subsystems":[{"id":"memory","title":"T","layer":"kernel","status":"overview","summary":"S","memory_link":"M","observe":[],"planned_concepts":[]}]}`, "", RuleRoadmap},
		{"roadmap observe id unknown", map[string]string{"alpha": validLesson(nil, fullBody(validExtra))},
			strings.Replace(minimalRoadmap("[]"), `"observe":[]`, `"observe":["mem.bogus"]`, 1), "", RuleCatalog},
		{"component depends on nothing known", map[string]string{"alpha": validLesson(nil, fullBody(validExtra))}, "",
			strings.Replace(validComponents, `"depends_on":[]`, `"depends_on":["ghost"]`, 1), RuleDangling},
		{"component instances from a scalar", map[string]string{"alpha": validLesson(nil, fullBody(validExtra))}, "",
			strings.Replace(validComponents, `"from":"cpu.caches"`, `"from":"kernel.arch"`, 1), RuleComponents},
		{"component with unknown field", map[string]string{"alpha": validLesson(nil, fullBody(validExtra))}, "",
			strings.Replace(validComponents, `"why":"y"`, `"why":"y","colour":"red"`, 1), RuleParse},
	}
	for _, tt := range tests {
		components := validComponents
		if tt.components != "" {
			components = tt.components
		}
		fsys := testFS(tt.lessons, "", components)
		if tt.roadmap != "" {
			fsys["roadmap.json"].Data = []byte(tt.roadmap)
		}
		problems := Lint(fsys, LintOptions{})
		if !slices.ContainsFunc(problems, func(p Problem) bool { return p.Rule == tt.rule && !p.Warning }) {
			t.Errorf("%s: no %s error in %v", tt.name, tt.rule, problems)
		}
		if !Failed(problems) {
			t.Errorf("%s: lint passed", tt.name)
		}
	}
}

func TestLintAllowMissingLessons(t *testing.T) {
	fsys := testFS(map[string]string{"alpha": validLesson(map[string]any{
		"prerequisites": []string{"later"}, "next": []string{"later"}, "deeper": []string{"later"},
	}, fullBody(validExtra+" [x](lesson:later)"))}, "", strings.Replace(validComponents, `"lessons":["alpha"]`, `"lessons":["alpha","later"]`, 1))
	strict := Lint(fsys, LintOptions{})
	if !Failed(strict) || len(strict) != 5 {
		t.Errorf("strict lint: %v, want five dangling errors", strict)
	}
	relaxed := Lint(fsys, LintOptions{AllowMissingLessons: true})
	if Failed(relaxed) || len(relaxed) != 5 {
		t.Errorf("relaxed lint: %v, want five warnings", relaxed)
	}
	for _, p := range relaxed {
		if p.Rule != RuleMissingLesson || !p.Warning {
			t.Errorf("relaxed problem %v", p)
		}
	}
	// The flag tolerates missing lessons only: other problems still fail.
	fsys["lessons/alpha.md"].Data = []byte(validLesson(map[string]any{"prerequisites": []string{"later"}}, fullBody(validExtra+"\n:::sim weather")))
	if !Failed(Lint(fsys, LintOptions{AllowMissingLessons: true})) {
		t.Error("an unknown sim passed lint with --allow-missing-lessons")
	}
}

func TestLintErrorsCarryLines(t *testing.T) {
	problems := Lint(testFS(map[string]string{"alpha": validLesson(nil, fullBody(validExtra+"\n:::diagram pie-chart"))}, "", validComponents), LintOptions{})
	if len(problems) != 1 || problems[0].File != "lessons/alpha.md" || problems[0].Line == 0 ||
		!strings.HasPrefix(problems[0].String(), "lessons/alpha.md:") {
		t.Errorf("problems %v", problems)
	}
}

// TestEmbeddedCurriculum lints the shipped content exactly as
// "atlas curriculum lint" does: every referenced lesson must exist, and
// warnings are reported but do not fail.
func TestEmbeddedCurriculum(t *testing.T) {
	for _, p := range Lint(Embedded(), LintOptions{}) {
		if p.Warning {
			t.Log(p)
		} else {
			t.Error(p)
		}
	}
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lesson("cpu-addressing"); !ok {
		t.Error("cpu-addressing is not embedded")
	}
}
