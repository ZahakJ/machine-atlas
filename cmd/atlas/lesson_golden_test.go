package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/derive"
)

var update = flag.Bool("update", false, "rewrite the bound-lesson goldens in testdata/lessons")

// goldenLessons are byte-compared against testdata/lessons/<fixture>/<id>.json.
// Only lessons whose text is settled belong here; the determinism test
// below covers every lesson.
var goldenLessons = []struct{ fixture, lesson string }{
	{"desktop-x86_64", "cpu-addressing"},
	{"rpi-arm64-noswap", "cpu-addressing"},
	{"desktop-x86_64", "cpu-topology"},
}

// lessonJSON is the bound lesson exactly as "atlas lesson --json" prints it.
func lessonJSON(t *testing.T, cur *curriculum.Curriculum, id, fixture string) []byte {
	t.Helper()
	l, ok := cur.Lesson(id)
	if !ok {
		t.Fatalf("no lesson %q", id)
	}
	s, err := loadFixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(curriculum.Bind(l, s, derive.All(s))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Bound lessons are a pure function of the snapshot, the curriculum and the
// app version; these goldens catch any change to that output. After an
// intended change run: go test ./cmd/atlas -run TestBoundLessonGoldens -update
func TestBoundLessonGoldens(t *testing.T) {
	cur, err := curriculum.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range goldenLessons {
		t.Run(g.fixture+"/"+g.lesson, func(t *testing.T) {
			got := lessonJSON(t, cur, g.lesson, g.fixture)
			file := filepath.Join("testdata", "lessons", g.fixture, g.lesson+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v; run with -update", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s is out of date; review the change and run with -update", file)
			}
		})
	}
}

// Every lesson bound to every embedded snapshot twice gives the same bytes.
func TestBindingIsDeterministic(t *testing.T) {
	cur, err := curriculum.Default()
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil || len(names) == 0 {
		t.Fatalf("no embedded snapshots (%v)", err)
	}
	for _, name := range names {
		fixture := strings.TrimSuffix(path.Base(name), ".json")
		for _, l := range cur.Lessons {
			a := lessonJSON(t, cur, l.ID, fixture)
			b := lessonJSON(t, cur, l.ID, fixture)
			if !bytes.Equal(a, b) {
				t.Errorf("%s on %s: two bindings differ", l.ID, fixture)
			}
		}
	}
}

// Every block of every lesson bound to every embedded snapshot carries a
// label its content supports: a block labelled observed or derived cites a
// value of that kind that was actually read (a chip, an evidence row or
// table with status ok), or sits in the chosen arm of a branch decided by
// such a value. Text where nothing was read is general, which the
// restricted fixture, with its permission failures, exercises most.
func TestLabelsAreSupportedByValues(t *testing.T) {
	cur, err := curriculum.Default()
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil || len(names) == 0 {
		t.Fatalf("no embedded snapshots (%v)", err)
	}
	for _, name := range names {
		fixture := strings.TrimSuffix(path.Base(name), ".json")
		s, err := loadFixture(fixture)
		if err != nil {
			t.Fatal(err)
		}
		facts := derive.All(s)
		for _, l := range cur.Lessons {
			b := curriculum.Bind(l, s, facts)
			for _, sec := range b.Sections {
				checkLabels(t, fixture+"/"+l.ID+"#"+sec.ID, sec.Blocks, curriculum.LabelGeneral)
			}
		}
	}
}

// okLabels collects the labels of the chips with a value in inl.
func okLabels(inl []curriculum.Inline, into map[string]bool) {
	for _, x := range inl {
		if x.T == "chip" && x.Status == "ok" {
			into[x.Label] = true
		}
		okLabels(x.Inl, into)
	}
}

// checkLabels checks blocks against the label rule; cond is the label of
// the fact that chose the enclosing arm, general outside chosen arms.
func checkLabels(t *testing.T, where string, blocks []curriculum.Block, cond string) {
	t.Helper()
	for _, b := range blocks {
		has := map[string]bool{}
		switch b.T {
		case "p", "h":
			okLabels(b.Inl, has)
		case "table":
			for _, r := range b.TableRows {
				for _, c := range r {
					okLabels(c, has)
				}
			}
		case "evidence":
			for _, r := range b.Evidence {
				if r.Status == "ok" {
					has[r.Label] = true
				}
			}
		case "datatable":
			if b.Status == "ok" {
				has[curriculum.LabelObserved] = true
			}
		case "branch":
			// The fact that decided it had a value; its kind is checked
			// against the fact id.
			if b.Chosen != "unknown" {
				kind := curriculum.LabelObserved
				if strings.HasPrefix(b.Fact, "derived.") {
					kind = curriculum.LabelDerived
				}
				has[kind] = true
			}
		}
		has[cond] = true
		switch b.Label {
		case curriculum.LabelObserved, curriculum.LabelDerived:
			if !has[b.Label] && b.T != "diagram" {
				t.Errorf("%s line %d: %s block labelled %s with no %s value behind it", where, b.Line, b.T, b.Label, b.Label)
			}
		}
		if b.Label == curriculum.LabelObserved && has[curriculum.LabelDerived] && (b.T == "p" || b.T == "h" || b.T == "table") {
			t.Errorf("%s line %d: %s cites a derived value but is labelled observed", where, b.Line, b.T)
		}
		switch b.T {
		case "ul", "ol":
			for _, item := range b.Items {
				checkLabels(t, where, item, cond)
			}
		case "quote", "callout":
			checkLabels(t, where, b.Blocks, cond)
		case "predict":
			checkLabels(t, where, b.Question, cond)
			checkLabels(t, where, b.Reveal, cond)
		case "branch":
			inner := curriculum.LabelGeneral
			if b.Chosen != "unknown" {
				inner = b.Label
			}
			for arm, blocks := range map[string][]curriculum.Block{"then": b.Then, "else": b.Else, "unknown": b.Unknown} {
				c := curriculum.LabelGeneral
				if arm == b.Chosen {
					c = inner
				}
				checkLabels(t, where, blocks, c)
			}
		}
	}
}
