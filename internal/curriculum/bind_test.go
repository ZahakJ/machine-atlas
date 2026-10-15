package curriculum

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/derive"
)

// embeddedLesson returns a lesson shipped in the binary.
func embeddedLesson(t *testing.T, id string) *Lesson {
	t.Helper()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	l, ok := c.Lesson(id)
	if !ok {
		t.Fatalf("no embedded lesson %q", id)
	}
	return l
}

// chips returns every chip of a bound lesson, in document order.
func chips(b *BoundLesson) []Inline {
	var out []Inline
	for _, s := range b.Sections {
		walk(s.Blocks, func(*Block) {}, func(_ *Block, x *Inline) {
			if x.T == "chip" {
				out = append(out, *x)
			}
		})
	}
	return out
}

// TestChipDisplayEqualsFormattedValue is the binding accuracy check: every
// chip shows exactly Format applied to the value it names.
func TestChipDisplayEqualsFormattedValue(t *testing.T) {
	l := embeddedLesson(t, "cpu-addressing")
	for name, vals := range map[string]map[string]any{"x86": x86Desktop(), "numa": numaNoSwap()} {
		s := snap(t, vals)
		facts := derive.All(s)
		m := newMachine(s, facts)
		b := Bind(l, s, facts)
		cs := chips(b)
		if len(cs) < 10 {
			t.Fatalf("%s: only %d chips bound", name, len(cs))
		}
		for _, c := range cs {
			v := m.resolve(c.Ref)
			want := StatusText(v.status)
			if v.ok() {
				want = Format(v.value, v.unit, c.Fmt)
			}
			if c.Display != want || c.Status != v.status {
				t.Errorf("%s: chip %s shows %q (%s), want %q (%s)", name, c.Ref, c.Display, c.Status, want, v.status)
			}
			if (c.Kind == RefObs) != (c.Label == LabelObserved) {
				t.Errorf("%s: chip %s kind %s has label %s", name, c.Ref, c.Kind, c.Label)
			}
		}
	}
}

// TestOneObservationChangesOnlyItsChips changes one observation and checks
// that exactly the chips naming it, or a fact computed from it, change.
func TestOneObservationChangesOnlyItsChips(t *testing.T) {
	l := embeddedLesson(t, "cpu-addressing")
	for _, change := range []struct {
		id    string
		value any
	}{
		{"mem.meminfo.MemTotal", 8 * gib},
		{"cpu.address_bits.physical", 46},
		{"kernel.page_size_bytes", 16384},
	} {
		before := x86Desktop()
		after := x86Desktop()
		after[change.id] = change.value
		_, b1 := bindJSON(t, l, before)
		_, b2 := bindJSON(t, l, after)
		dependent := map[string]bool{change.id: true}
		for _, f := range derive.All(snap(t, after)) {
			if slices.Contains(f.Inputs, change.id) {
				dependent[f.ID] = true
			}
		}
		c1, c2 := chips(b1), chips(b2)
		if len(c1) != len(c2) {
			t.Fatalf("%s: chip count changed", change.id)
		}
		changed := map[string]bool{}
		for i := range c1 {
			if c1[i].Display != c2[i].Display {
				changed[c1[i].Ref] = true
				if !dependent[c1[i].Ref] {
					t.Errorf("%s: chip %s changed (%q → %q) but does not depend on it", change.id, c1[i].Ref, c1[i].Display, c2[i].Display)
				}
			}
		}
		if !changed[change.id] && slices.ContainsFunc(c1, func(c Inline) bool { return c.Ref == change.id }) {
			t.Errorf("%s: its own chip did not change", change.id)
		}
	}
}

func TestBindIsByteIdentical(t *testing.T) {
	l := embeddedLesson(t, "cpu-addressing")
	a, _ := bindJSON(t, l, x86Desktop())
	b, _ := bindJSON(t, l, x86Desktop())
	if !bytes.Equal(a, b) {
		t.Fatal("binding the same snapshot twice gave different bytes")
	}
	// Observation order must not matter either.
	s := snap(t, x86Desktop())
	slices.Reverse(s.Observations)
	facts := derive.All(s)
	slices.Reverse(facts)
	c, _ := jsonOf(Bind(l, s, facts))
	if !bytes.Equal(a, c) {
		t.Fatal("binding depends on observation or fact order")
	}
	// Binding must not mutate the parsed lesson, which is shared.
	other := x86Desktop()
	other["kernel.arch"], other["probe.maps.highest_end"] = "aarch64", "0xffffd2a71000"
	d, _ := bindJSON(t, l, other)
	e, _ := bindJSON(t, l, x86Desktop())
	if bytes.Equal(a, d) || !bytes.Equal(a, e) {
		t.Fatal("a bind on another snapshot changed later results")
	}
}

func TestBindBranchesLabelsAndUses(t *testing.T) {
	body := strings.Join([]string{
		"Swap is {{derived:derived.swap.total_bytes}} on {{obs:kernel.arch}}.",
		"",
		"Plain text with [a ref](obs:mem.meminfo.MemFree).",
		"",
		":::when derived.swap.configured",
		"Has swap.",
		":::else",
		"No swap.",
		":::end",
		":::when mem.slabinfo.cache_count",
		"Slab.",
		":::end",
		":::when derived.thp.mode is always",
		"Always.",
		":::unknown",
		"Author's unknown text.",
		":::end",
		":::evidence E",
		"- mem.meminfo.MemTotal",
		"- derived.mem.total_pages",
		"- mem.slabinfo.cache_count | Slab caches",
		":::end",
		":::table mem.zones columns=node,zone,free,min limit=1",
		":::table mem.slabinfo.cache_count",
		":::sim reclaim",
		":::diagram zone-watermarks",
		":::diagram fault-flow",
		":::experiment fault-count",
	}, "\n")
	l, err := Parse("lessons/alpha.md", []byte(lessonDoc(nil, fullBody(body))))
	if err != nil {
		t.Fatal(err)
	}
	vals := x86Desktop()
	vals["mem.slabinfo.cache_count"] = missing("permission_denied")
	vals["kernel.page_size_bytes"] = 4096
	_, b := bindJSON(t, l, vals)
	var how []Block
	for _, s := range b.Sections {
		if s.ID == "how-it-works" {
			how = s.Blocks
		}
	}
	labels := []string{}
	for _, blk := range how {
		labels = append(labels, blk.T+":"+blk.Label)
	}
	// A paragraph with a derived chip is derived however many observations
	// it cites; a branch on a known fact takes that fact's label, one on an
	// unknown fact stays general; a table that could not be read and an
	// experiment that has not run claim nothing.
	want := []string{"p:derived", "p:general", "branch:derived", "branch:general", "branch:derived", "evidence:observed",
		"datatable:observed", "datatable:general", "sim:simulated", "diagram:observed", "diagram:general", "experiment:general"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels %v\nwant   %v", labels, want)
	}
	// The chosen arm of a known fact says something about this machine
	// because of it; the arm not chosen and an unknown arm do not.
	if sw := how[2]; sw.Then[0].Label != LabelDerived || sw.Else[0].Label != LabelGeneral || sw.Unknown[0].Label != LabelGeneral {
		t.Errorf("swap branch arm labels then=%s else=%s unknown=%s", sw.Then[0].Label, sw.Else[0].Label, sw.Unknown[0].Label)
	}
	if sl := how[3]; sl.Unknown[0].Label != LabelGeneral {
		t.Errorf("unknown arm of an unknown fact labelled %s", sl.Unknown[0].Label)
	}
	if br := how[2]; br.Chosen != "then" || !br.Generated || len(br.Else) != 1 ||
		!strings.Contains(inlineText(br.Unknown[0].Inl), "cannot be determined") {
		t.Errorf("swap branch %+v", br)
	}
	slab := how[3]
	if slab.Chosen != "unknown" || !slab.Generated || len(slab.Unknown) != 1 {
		t.Fatalf("slab branch %+v", slab)
	}
	if got := inlineText(slab.Unknown[0].Inl); !strings.Contains(got, "could not determine") ||
		!strings.Contains(got, "mem.slabinfo.cache_count") || !strings.Contains(got, "permission denied") || !strings.Contains(got, "/proc/example") {
		t.Errorf("generated unknown text %q must name the fact, its status and its source", got)
	}
	if thp := how[4]; thp.Chosen != "then" || thp.Generated || inlineText(thp.Unknown[0].Inl) != "Author's unknown text." {
		t.Errorf("thp branch %+v", thp)
	}
	ev := how[5].Evidence
	if ev[0].Label != LabelObserved || ev[1].Label != LabelDerived || ev[2].Display != "not available (permission denied)" ||
		ev[2].Caption != "Slab caches" || ev[0].Caption != "Usable RAM" || ev[0].CollectedAt == "" || !strings.HasPrefix(ev[1].Source, "derived: ") {
		t.Errorf("evidence rows %+v", ev)
	}
	dt := how[6]
	if dt.Total != 2 || len(dt.Rows) != 1 || !slices.Equal(dt.Rows[0], []string{"0", "DMA32", "1 000", "10"}) {
		t.Errorf("datatable %+v", dt)
	}
	if bad := how[7]; bad.Status != "permission_denied" || bad.Display != "not available (permission denied)" || len(bad.Rows) != 0 {
		t.Errorf("unavailable datatable %+v", bad)
	}
	var uses []string
	for _, u := range b.Uses {
		uses = append(uses, u.ID+"="+u.Status)
	}
	wantUses := []string{"derived.mem.total_pages=ok", "derived.swap.configured=ok", "derived.swap.total_bytes=ok", "derived.thp.mode=ok",
		"kernel.arch=ok", "mem.meminfo.MemFree=ok", "mem.meminfo.MemTotal=ok", "mem.slabinfo.cache_count=permission_denied", "mem.zones=ok"}
	if !slices.Equal(uses, wantUses) {
		t.Errorf("uses %v\nwant %v", uses, wantUses)
	}
	if b.Coverage != (Coverage{OK: 8, Total: 9}) {
		t.Errorf("coverage %+v", b.Coverage)
	}
}

func TestEmbeddedLessonBranchesOnArm(t *testing.T) {
	l := embeddedLesson(t, "cpu-addressing")
	vals := x86Desktop()
	maps.Copy(vals, map[string]any{
		"kernel.arch": "aarch64", "cpu.model_name": missing("absent"), "cpu.address_bits.virtual": missing("absent"),
		"cpu.address_bits.physical": missing("absent"), "cpu.flags.la57": missing("unsupported"),
		"cpu.flags.pdpe1gb": missing("unsupported"), "cpu.flags.hypervisor": missing("unsupported"),
		"probe.maps.highest_end": "0x7fc8a1b000",
	})
	_, b := bindJSON(t, l, vals)
	text := RenderText(b)
	for _, want := range []string{"publishes no address widths", "39 bits", "does not report its physical address width", "arm64-4k-39"} {
		if !strings.Contains(text, want) {
			t.Errorf("arm64 rendering lacks %q", want)
		}
	}
	if strings.Contains(text, "This CPU has no la57 flag") {
		t.Error("arm64 rendering shows the x86 branch")
	}
}

func TestRenderText(t *testing.T) {
	l := embeddedLesson(t, "cpu-addressing")
	vals := x86Desktop()
	vals["cpu.model_name"] = "Evil\x1b[2J CPU"
	_, b := bindJSON(t, l, vals)
	text := RenderText(b)
	for _, want := range []string{"[OBSERVED] ", "[DERIVED] ", "[GENERAL] ", "[SIMULATED] ", "Evidence: ", "References"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q", want)
		}
	}
	if strings.ContainsRune(text, '\x1b') {
		t.Error("a control character from the snapshot reached the terminal text")
	}
	if strings.Contains(text, ":::") || strings.Contains(text, "{{") {
		t.Error("directive syntax leaked into the text")
	}
}

func TestInlineLabelCountsOnlyChipsWithValues(t *testing.T) {
	chip := func(kind, status string) Inline {
		label := LabelObserved
		if kind == RefDerived {
			label = LabelDerived
		}
		return Inline{T: "chip", Kind: kind, Status: status, Label: label}
	}
	for _, c := range []struct {
		inl  []Inline
		want string
	}{
		{[]Inline{{T: "text", S: "x"}}, LabelGeneral},
		{[]Inline{chip(RefObs, "permission_denied")}, LabelGeneral},
		{[]Inline{chip(RefDerived, "unknown"), chip(RefObs, "absent")}, LabelGeneral},
		{[]Inline{chip(RefObs, "ok"), chip(RefDerived, "unknown")}, LabelObserved},
		{[]Inline{chip(RefObs, "ok"), chip(RefDerived, "ok")}, LabelDerived},
		{[]Inline{{T: "em", Inl: []Inline{chip(RefDerived, "ok")}}, chip(RefObs, "ok")}, LabelDerived},
	} {
		if got := inlineLabel(c.inl); got != c.want {
			t.Errorf("inlineLabel(%+v) = %s, want %s", c.inl, got, c.want)
		}
	}
}
