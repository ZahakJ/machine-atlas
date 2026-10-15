package curriculum

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// BoundLesson is a lesson resolved against one snapshot: the API shape of
// GET /api/lessons/<id>.
type BoundLesson struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Summary       string        `json:"summary"`
	Subsystem     string        `json:"subsystem"`
	Kind          string        `json:"kind"`
	Order         int           `json:"order"`
	Prerequisites []string      `json:"prerequisites"`
	Next          []string      `json:"next"`
	Deeper        []string      `json:"deeper"`
	VersionNotes  []VersionNote `json:"version_notes"`
	References    []Reference   `json:"references"`
	Sections      []Section     `json:"sections"`
	Uses          []Use         `json:"uses"`
	Coverage      Coverage      `json:"coverage"`
}

// Use is one id the lesson references and its status on this machine.
type Use struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // obs | derived
	Status string `json:"status"`
}

// Coverage counts how many of the lesson's ids have a value here.
type Coverage struct {
	OK    int `json:"ok"`
	Total int `json:"total"`
}

// machine indexes a snapshot's observations and derived facts by id.
type machine struct {
	obs   map[string]obs.Observation
	facts map[string]derive.Fact
}

func newMachine(s *snapshot.Snapshot, facts []derive.Fact) *machine {
	m := &machine{obs: make(map[string]obs.Observation, len(s.Observations)), facts: make(map[string]derive.Fact, len(facts))}
	for _, o := range s.Observations {
		m.obs[o.ID] = o
	}
	for _, f := range facts {
		m.facts[f.ID] = f
	}
	return m
}

// value is an id resolved on a machine.
type value struct {
	id, kind    string
	value       any
	unit        string
	status      string
	source      string // where an observation came from, or a fact's rule
	collectedAt string
	reason      string // why a fact is unknown
}

func (v value) ok() bool { return v.status == string(obs.StatusOK) }

// display is the value formatted for text, or the status wording.
func (v value) display(format string) string {
	if !v.ok() {
		return StatusText(v.status)
	}
	return Format(v.value, v.unit, format)
}

func (v value) label() string {
	if v.kind == RefDerived {
		return LabelDerived
	}
	return LabelObserved
}

// resolve looks id up. An observation missing from the snapshot reads as
// not collected (an older snapshot, or a collector that did not run), and a
// fact this version does not compute as unknown.
func (m *machine) resolve(id string) value {
	if refKind(id) == RefDerived {
		f, ok := m.facts[id]
		if !ok {
			return value{id: id, kind: RefDerived, status: derive.StatusUnknown, reason: "not computed by this version"}
		}
		return value{id: id, kind: RefDerived, value: f.Value, unit: f.Unit, status: f.Status, source: "derived: " + f.Rule, reason: f.Reason}
	}
	o, ok := m.obs[id]
	if !ok {
		return value{id: id, kind: RefObs, status: string(obs.StatusNotCollected), source: "not in this snapshot"}
	}
	unit := o.Unit
	if e, found := catalog.Lookup(id); found && unit == "" {
		unit = e.Unit
	}
	return value{id: id, kind: RefObs, value: o.Value, unit: unit, status: string(o.Status), source: SourceText(o.Source), collectedAt: o.CollectedAt}
}

// title is the catalog title of id, or the id itself.
func title(id string) string {
	if e, ok := catalog.Lookup(id); ok && !e.Family() {
		return e.Title
	}
	return id
}

// Bind resolves lesson l against a snapshot and its derived facts. The
// lesson is not modified; the result shares nothing with it.
func Bind(l *Lesson, s *snapshot.Snapshot, facts []derive.Fact) *BoundLesson {
	b := &binder{m: newMachine(s, facts), used: map[string]bool{}}
	out := &BoundLesson{
		ID: l.ID, Title: l.Title, Summary: l.Summary, Subsystem: l.Subsystem, Kind: l.Kind, Order: l.Order,
		Prerequisites: nonNil(slices.Clone(l.Prerequisites)), Next: nonNil(slices.Clone(l.Next)),
		Deeper: nonNil(slices.Clone(l.Deeper)), VersionNotes: nonNil(slices.Clone(l.VersionNotes)),
		References: nonNil(slices.Clone(l.References)),
	}
	for _, sec := range l.Sections {
		out.Sections = append(out.Sections, Section{ID: sec.ID, Title: sec.Title, Blocks: b.blocks(sec.Blocks)})
	}
	out.Sections = nonNil(out.Sections)
	for _, id := range slices.Sorted(maps.Keys(b.used)) {
		v := b.m.resolve(id)
		out.Uses = append(out.Uses, Use{ID: id, Kind: v.kind, Status: v.status})
		if v.ok() {
			out.Coverage.OK++
		}
	}
	out.Uses = nonNil(out.Uses)
	out.Coverage.Total = len(out.Uses)
	return out
}

type binder struct {
	m    *machine
	used map[string]bool
}

func (b *binder) resolve(id string) value {
	b.used[id] = true
	return b.m.resolve(id)
}

func (b *binder) blocks(in []Block) []Block {
	if in == nil {
		return nil
	}
	out := make([]Block, len(in))
	for i, blk := range in {
		out[i] = b.block(blk)
	}
	return out
}

func (b *binder) block(in Block) Block {
	out := Block{T: in.T, Line: in.Line, Label: LabelGeneral}
	switch in.T {
	case "p", "h":
		out.Level = in.Level
		out.Inl = b.inlines(in.Inl)
		out.Label = inlineLabel(out.Inl)
	case "ul", "ol":
		for _, item := range in.Items {
			out.Items = append(out.Items, b.blocks(item))
		}
	case "code":
		out.Lang, out.Text = in.Lang, in.Text
	case "table":
		for _, h := range in.Head {
			out.Head = append(out.Head, b.inlines(h))
		}
		for _, r := range in.TableRows {
			row := make([][]Inline, len(r))
			for i, c := range r {
				row[i] = b.inlines(c)
			}
			out.TableRows = append(out.TableRows, row)
		}
		var all []Inline
		for _, r := range out.TableRows {
			for _, c := range r {
				all = append(all, c...)
			}
		}
		out.Label = inlineLabel(all)
	case "quote":
		out.Blocks = b.blocks(in.Blocks)
	case "callout":
		out.Kind, out.Title, out.Blocks = in.Kind, in.Title, b.blocks(in.Blocks)
	case "evidence":
		// Rows carry their own labels; the block is observed if any row
		// with a value is, derived if only derived rows have one, and
		// general when nothing could be read.
		out.Title = in.Title
		for _, r := range in.Evidence {
			row := b.evidenceRow(r)
			switch {
			case row.Status != string(obs.StatusOK):
			case row.Label == LabelObserved:
				out.Label = LabelObserved
			case out.Label == LabelGeneral:
				out.Label = LabelDerived
			}
			out.Evidence = append(out.Evidence, row)
		}
	case "datatable":
		b.datatable(in, &out)
	case "diagram":
		out.Name = in.Name
		if slices.Contains(dataDiagrams, in.Name) {
			out.Label = LabelObserved
		}
	case "sim":
		out.Name, out.Label = in.Name, LabelSimulated
	case "experiment":
		// A bound lesson never contains experiment results (they are not
		// part of a snapshot), so the card describes a procedure: general.
		out.Name = in.Name
	case "predict":
		out.Question = b.blocks(in.Question)
		for _, o := range in.Options {
			out.Options = append(out.Options, b.inlines(o))
		}
		out.Reveal = b.blocks(in.Reveal)
	case "branch":
		b.branch(in, &out)
	}
	return out
}

func (b *binder) inlines(in []Inline) []Inline {
	if in == nil {
		return nil
	}
	out := make([]Inline, len(in))
	for i, x := range in {
		x.Inl = b.inlines(x.Inl)
		switch {
		case x.T == "chip":
			v := b.resolve(x.Ref)
			x.Display, x.Status, x.Label = v.display(x.Fmt), v.status, v.label()
		case x.T == "link" && x.Kind == LinkObs:
			b.used[x.Href] = true
		}
		out[i] = x
	}
	return out
}

// inlineLabel labels text by the chips in it that have a value: derived if
// any of them is a derived fact, observed if all of them are observations,
// general if there are none. The weaker label wins, because a sentence that
// uses an inference is an inference however many observations it also
// cites; a chip that reads "not available" supports no claim at all.
func inlineLabel(inl []Inline) string {
	label := LabelGeneral
	var walk func([]Inline)
	walk = func(xs []Inline) {
		for _, x := range xs {
			if x.T == "chip" && x.Status == string(obs.StatusOK) {
				label = weaker(label, x.Label)
			}
			walk(x.Inl)
		}
	}
	walk(inl)
	return label
}

// weaker combines two claim labels: general is no claim, and derived is
// weaker than observed.
func weaker(a, b string) string {
	switch {
	case a == LabelGeneral:
		return b
	case b == LabelGeneral:
		return a
	case a == LabelDerived || b == LabelDerived:
		return LabelDerived
	}
	return a
}

// conditioned relabels the blocks of a chosen branch arm whose condition is
// a fact with label cond: text there is said about this machine because of
// that fact, so a paragraph, heading, table or list item without chips of
// its own takes the fact's label, and one with chips the weaker of the two.
// Callouts, evidence, data tables and other self-describing blocks keep
// their own label. A nested branch that chose on a known fact is
// conditioned too, and so is its chosen arm.
func conditioned(blocks []Block, cond string) {
	for i := range blocks {
		b := &blocks[i]
		switch b.T {
		case "p", "h", "table":
			b.Label = weaker(b.Label, cond)
		case "ul", "ol":
			for _, item := range b.Items {
				conditioned(item, cond)
			}
		case "quote":
			conditioned(b.Blocks, cond)
		case "branch":
			// A nested branch whose own fact is unknown says only that;
			// its text stays as labelled.
			if b.Chosen != "unknown" {
				b.Label = weaker(b.Label, cond)
				conditioned(b.arm(), cond)
			}
		}
	}
}

// arm returns the chosen arm of a bound branch.
func (b *Block) arm() []Block {
	switch b.Chosen {
	case "then":
		return b.Then
	case "else":
		return b.Else
	}
	return b.Unknown
}

func (b *binder) evidenceRow(r EvidenceRow) EvidenceRow {
	v := b.resolve(r.Ref)
	caption := r.Caption
	if caption == "" {
		caption = title(r.Ref)
	}
	source := v.source
	if !v.ok() && v.reason != "" {
		source += " (" + v.reason + ")"
	}
	return EvidenceRow{
		Ref: r.Ref, Kind: v.kind, Caption: caption, Display: v.display(""), Unit: v.unit,
		Status: v.status, Label: v.label(), Source: source, CollectedAt: v.collectedAt,
	}
}

func (b *binder) datatable(in Block, out *Block) {
	v := b.resolve(in.Ref)
	out.Ref, out.Status, out.Limit = in.Ref, v.status, in.Limit
	rows, isTable := v.value.([]map[string]any)
	switch {
	case !v.ok():
		out.Display = StatusText(v.status)
	case !isTable:
		out.Status, out.Display = string(obs.StatusFailed), "not available (not a table)"
	default:
		out.Label = LabelObserved
	}
	out.Columns = slices.Clone(in.Columns)
	if out.Columns == nil {
		out.Columns = tableColumns(in.Ref, rows)
	}
	out.Total = len(rows)
	if in.Limit > 0 && len(rows) > in.Limit {
		rows = rows[:in.Limit]
	}
	for _, r := range rows {
		cells := make([]string, len(out.Columns))
		for i, c := range out.Columns {
			cells[i] = formatCell(c, r[c], v.unit)
		}
		out.Rows = append(out.Rows, cells)
	}
}

// tableColumns is the catalog's column order, with a wildcard column
// ("order*") expanded to the matching columns present, in natural order.
func tableColumns(id string, rows []map[string]any) []string {
	e, _ := catalog.Lookup(id)
	var cols []string
	for _, c := range e.Columns {
		prefix, wild := strings.CutSuffix(c, "*")
		if !wild {
			cols = append(cols, c)
			continue
		}
		seen := map[string]bool{}
		for _, r := range rows {
			for k := range r {
				if strings.HasPrefix(k, prefix) {
					seen[k] = true
				}
			}
		}
		cols = append(cols, slices.SortedFunc(maps.Keys(seen), naturalCompare)...)
	}
	return cols
}

// naturalCompare orders "order2" before "order10".
func naturalCompare(a, b string) int {
	ta, na := splitNumber(a)
	tb, nb := splitNumber(b)
	if ta != tb {
		return strings.Compare(a, b)
	}
	return cmp.Compare(na, nb)
}

func splitNumber(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(s[i:])
	return s[:i], n
}

// formatCell formats one table cell. Columns named *_bytes hold bytes;
// other numbers are counts (in the table's unit when it has one, such as
// the pages of mem.zones, which the column header then states).
func formatCell(col string, v any, tableUnit string) string {
	switch {
	case v == nil:
		return "—"
	case strings.HasSuffix(col, "_bytes"):
		return Format(v, obs.UnitBytes, "")
	}
	if _, ok := obs.Float(v); ok {
		return Format(v, obs.UnitCount, "")
	}
	return Format(v, tableUnit, "raw")
}

func (b *binder) branch(in Block, out *Block) {
	v := b.resolve(in.Fact)
	out.Fact, out.Expect = in.Fact, in.Expect
	out.Then, out.Else = nonNil(b.blocks(in.Then)), nonNil(b.blocks(in.Else))
	switch {
	case !v.ok():
		out.Chosen = "unknown"
	case matches(v.value, in.Expect):
		out.Chosen = "then"
	default:
		out.Chosen = "else"
	}
	if in.Unknown != nil {
		out.Unknown = b.blocks(in.Unknown)
	} else {
		out.Generated = true
		out.Unknown = []Block{{T: "p", Label: LabelGeneral, Line: in.Line, Inl: []Inline{{T: "text", S: couldNotDetermine(v)}}}}
	}
	// A branch decided by a fact with a value makes a claim of that
	// fact's kind, and so does the arm it chose. An unknown fact decides
	// nothing: the unknown arm keeps the labels of its own chips.
	if v.ok() {
		out.Label = v.label()
		conditioned(out.arm(), out.Label)
	}
}

// couldNotDetermine is the text of a generated unknown branch: what could
// not be determined, why, and where Machine Atlas looked. When the value is
// known the branch is not chosen, but the UI can still show it, so it then
// says what would appear instead.
func couldNotDetermine(v value) string {
	what := lowerFirst(title(v.id)) + " (" + v.id + ")"
	switch {
	case v.ok():
		return "When " + what + " cannot be determined, this text names the reason and where Machine Atlas looked."
	case v.kind == RefDerived:
		return "Machine Atlas could not determine " + what + " on this machine: " + cmp.Or(v.reason, "an input is missing") + "."
	}
	return "Machine Atlas could not determine " + what + " on this machine: " + StatusText(v.status) + ", source " + v.source + "."
}

func lowerFirst(s string) string {
	if len(s) > 1 && s[0] >= 'A' && s[0] <= 'Z' && !(s[1] >= 'A' && s[1] <= 'Z') {
		return string(s[0]+'a'-'A') + s[1:]
	}
	return s
}

// matches reports whether a value satisfies a branch or relevance rule:
// truthy when expect is nil (true, a non-zero number, a non-empty string
// or table), otherwise equal to expect as written (rawString).
func matches(v any, expect *string) bool {
	if expect != nil {
		return rawString(v) == *expect
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case []map[string]any:
		return len(x) > 0
	}
	n, ok := obs.Float(v)
	return ok && n != 0
}
