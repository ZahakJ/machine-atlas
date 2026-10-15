package curriculum

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// unavailableWords mark text that is explicitly about a value being
// unavailable, where a chip reading "not available (…)" is the point.
var unavailableWords = []string{
	"not available", "unavailable", "not determined", "could not", "cannot", "can't",
	"unknown", "permission denied", "denied", "not readable", "unreadable", "not collected", "absent",
	"has no ", "missing",
}

// knownUnavailableChips are chosen-arm chips the check below may flag in
// the lessons as written, keyed "<lesson>.md:<line> <chip id>", with the
// reason each is tolerated. It is empty: every lesson passes. An entry is a
// last resort while a lesson is being fixed, and an entry that no longer
// fires fails the test.
var knownUnavailableChips = map[string]string{}

// TestChosenArmsDoNotShowUnavailableChips binds every lesson to every
// embedded snapshot (the fixtures and live-sample.json) and checks that the
// arm a known fact chose does not state a value that is not there: text
// written for "this machine has X" should not read "X is not available
// (permission denied)". A chip inside a nested branch is judged by that
// branch, and a paragraph or list item that itself says the value may be
// unavailable is allowed to show the status.
func TestChosenArmsDoNotShowUnavailableChips(t *testing.T) {
	cur, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil || len(names) == 0 {
		t.Fatalf("no embedded snapshots (%v)", err)
	}
	found := map[string][]string{} // key → fixtures
	for _, name := range names {
		f, err := fixtures.FS.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		s, err := snapshot.Load(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fixture := strings.TrimSuffix(path.Base(name), ".json")
		facts := derive.All(s)
		for _, l := range cur.Lessons {
			b := Bind(l, s, facts)
			for _, sec := range b.Sections {
				for _, v := range unavailableInChosenArms(sec.Blocks) {
					key := fmt.Sprintf("%s.md:%d %s", l.ID, v.line, v.ref)
					if !slices.Contains(found[key], fixture) {
						found[key] = append(found[key], fixture)
					}
				}
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(found)) {
		if _, known := knownUnavailableChips[key]; !known {
			t.Errorf("curriculum/lessons/%s: a chip that is not available sits in a branch arm chosen by a known fact (on %s); say so in the text, move it into a nested branch on its own value, or choose another value", key, strings.Join(found[key], ", "))
		}
	}
	for key := range knownUnavailableChips {
		if _, still := found[key]; !still {
			t.Errorf("known failure %q no longer occurs; remove it from knownUnavailableChips", key)
		}
	}
}

// unavailableChip is one flagged chip: its id and the line of its block.
type unavailableChip struct {
	ref  string
	line int
}

// unavailableInChosenArms walks blocks for branches decided by a known
// fact and returns the flagged chips of their chosen arms.
func unavailableInChosenArms(blocks []Block) []unavailableChip {
	var out []unavailableChip
	for _, b := range blocks {
		for _, child := range childBlocks(b) {
			out = append(out, unavailableInChosenArms(child)...)
		}
		if b.T == "branch" && b.Chosen != "unknown" {
			out = append(out, armChips(b.arm())...)
		}
	}
	return out
}

// childBlocks are the block lists directly inside b, all arms of a branch
// included, so every branch at any depth is visited once.
func childBlocks(b Block) [][]Block {
	switch b.T {
	case "ul", "ol":
		return b.Items
	case "quote", "callout":
		return [][]Block{b.Blocks}
	case "predict":
		return [][]Block{b.Question, b.Reveal}
	case "branch":
		return [][]Block{b.Then, b.Else, b.Unknown}
	}
	return nil
}

// armChips returns the not-ok chips of a chosen arm, outside nested
// branches, in blocks whose own text does not speak of unavailability.
func armChips(blocks []Block) []unavailableChip {
	var out []unavailableChip
	for _, b := range blocks {
		switch b.T {
		case "branch":
			continue // judged as a branch of its own
		case "p", "h":
			out = append(out, flagged(b.Line, b.Inl)...)
		case "table":
			var cells []Inline
			for _, r := range append(slices.Clone(b.TableRows), b.Head) {
				for _, c := range r {
					cells = append(cells, c...)
				}
			}
			out = append(out, flagged(b.Line, cells)...)
		}
		for _, child := range childBlocks(b) {
			out = append(out, armChips(child)...)
		}
	}
	return out
}

// flagged returns the not-ok chips of inl unless the text around them
// says the value may be unavailable.
func flagged(line int, inl []Inline) []unavailableChip {
	var chips []unavailableChip
	var text strings.Builder
	var walk func([]Inline)
	walk = func(xs []Inline) {
		for _, x := range xs {
			switch x.T {
			case "chip":
				if x.Status != "ok" {
					chips = append(chips, unavailableChip{x.Ref, line})
				}
			case "text", "code":
				text.WriteString(x.S)
			}
			walk(x.Inl)
		}
	}
	walk(inl)
	lower := strings.ToLower(text.String())
	for _, w := range unavailableWords {
		if strings.Contains(lower, w) {
			return nil
		}
	}
	return chips
}
