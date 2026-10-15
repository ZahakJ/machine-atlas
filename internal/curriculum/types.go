// Package curriculum turns authored lessons into text bound to one machine.
//
// A lesson is a Markdown file with a JSON front matter (ARCHITECTURE.md §7). Parse
// reads it into a typed block tree; Bind resolves every reference to an
// observation or derived fact against a snapshot, formats values, picks
// branches and labels each block as observed, derived, general or
// simulated. The same snapshot, curriculum and application version always
// produce byte-identical output: nothing here reads a clock or iterates a
// map in output order.
//
// The package also holds the curriculum as a whole: the prerequisite graph
// and the journey selected for a machine (Select), the Explore component
// graph (Components), the roadmap overview (Roadmap), the reverse index of
// ids (Usage) and the checks every lesson must pass (Lint).
package curriculum

import "encoding/json"

// Section ids, in the order a lesson must use them. Only see-it is optional.
var sectionOrder = []struct{ id, title string }{
	{"question", "Question"},
	{"why-it-exists", "Why it exists"},
	{"how-it-works", "How it works"},
	{"on-this-machine", "On this machine"},
	{"see-it", "See it"},
	{"predict", "Predict"},
	{"try-it", "Try it"},
	{"limits", "Limits"},
	{"go-deeper", "Go deeper"},
}

// Labels say what kind of claim a block makes. The UI shows them as text
// badges.
const (
	LabelObserved  = "observed"
	LabelDerived   = "derived"
	LabelGeneral   = "general"
	LabelSimulated = "simulated"
)

// Reference kinds of chips, evidence rows and uses.
const (
	RefObs     = "obs"
	RefDerived = "derived"
)

// Meta is a lesson's front matter.
type Meta struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Subsystem     string        `json:"subsystem"`
	Kind          string        `json:"kind"` // "core" | "branch"
	Order         int           `json:"order"`
	Summary       string        `json:"summary"`
	Prerequisites []string      `json:"prerequisites"`
	Uses          []string      `json:"uses"`
	Relevance     []Relevance   `json:"relevance"`
	Next          []string      `json:"next"`
	Deeper        []string      `json:"deeper"`
	VersionNotes  []VersionNote `json:"version_notes"`
	References    []Reference   `json:"references"`
}

// Relevance is one rule that makes a lesson or planned concept relevant to
// a machine: when the fact When is truthy, or equals Is when Is is given.
type Relevance struct {
	When string `json:"when"`
	// Is is the expected value as written in JSON (a string, number or
	// boolean); nil means "when the value is truthy".
	Is   json.RawMessage `json:"is,omitempty"`
	Note string          `json:"note"`
}

// VersionNote marks behaviour that depends on the kernel version.
type VersionNote struct {
	Applies string `json:"applies"`
	Text    string `json:"text"`
}

// Reference is a primary source.
type Reference struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
}

// Lesson is a parsed lesson file. Its blocks are unbound: chips have no
// display text and branches have not been chosen.
type Lesson struct {
	Meta
	File     string
	Sections []Section
}

// Section is one "## Heading" of a lesson.
type Section struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

// Inline is a run of text inside a block. T is "text", "code", "em",
// "strong", "link" or "chip".
type Inline struct {
	T    string   `json:"t"`
	S    string   `json:"s,omitempty"`
	Kind string   `json:"kind,omitempty"` // link: url|lesson|component|obs; chip: obs|derived
	Href string   `json:"href,omitempty"`
	Inl  []Inline `json:"inl,omitempty"`
	Ref  string   `json:"ref,omitempty"`
	// Fmt is the chip's format as written ("" means auto); it shapes the
	// display and is not part of the bound output.
	Fmt     string `json:"-"`
	Display string `json:"display,omitempty"`
	Status  string `json:"status,omitempty"`
	Label   string `json:"label,omitempty"`
}

// Block is one node of the lesson tree. Which fields are meaningful depends
// on T; MarshalJSON writes exactly the fields of that type (ARCHITECTURE.md §7,
// "Bound lesson").
type Block struct {
	T     string
	Label string
	Line  int // source line, for error messages and lint

	Level int      // h
	Inl   []Inline // p, h
	Items [][]Block
	Lang  string // code
	Text  string // code

	Head      [][]Inline   // table
	TableRows [][][]Inline // table

	Blocks []Block // quote, callout

	Title    string        // evidence, callout
	Evidence []EvidenceRow // evidence

	Ref     string     // datatable
	Status  string     // datatable
	Display string     // datatable: what to show instead of rows when not ok
	Columns []string   // datatable
	Limit   int        // datatable (0: all rows)
	Total   int        // datatable: rows in the observation
	Rows    [][]string // datatable, formatted cells

	Name string // diagram, sim, experiment id
	Kind string // callout: misconception|cannot

	Fact      string  // branch
	Expect    *string // branch: nil means truthy
	Chosen    string  // branch: then|else|unknown
	Then      []Block // branch
	Else      []Block // branch
	Unknown   []Block // branch
	Generated bool    // branch: the unknown text was generated

	Question []Block    // predict
	Options  [][]Inline // predict
	Reveal   []Block    // predict
}

// EvidenceRow is one line of an evidence table.
type EvidenceRow struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Caption     string `json:"caption"`
	Display     string `json:"display"`
	Unit        string `json:"unit"`
	Status      string `json:"status"`
	Label       string `json:"label"`
	Source      string `json:"source"`
	CollectedAt string `json:"collected_at"`
}

// MarshalJSON writes the block in the shape its type defines, so a
// paragraph never carries empty table fields and the UI can switch on t.
func (b Block) MarshalJSON() ([]byte, error) {
	type p struct {
		T     string   `json:"t"`
		Label string   `json:"label"`
		Inl   []Inline `json:"inl"`
	}
	type h struct {
		T     string   `json:"t"`
		Label string   `json:"label"`
		Level int      `json:"level"`
		Inl   []Inline `json:"inl"`
	}
	type list struct {
		T     string    `json:"t"`
		Label string    `json:"label"`
		Items [][]Block `json:"items"`
	}
	type code struct {
		T     string `json:"t"`
		Label string `json:"label"`
		Lang  string `json:"lang"`
		Text  string `json:"text"`
	}
	type table struct {
		T     string       `json:"t"`
		Label string       `json:"label"`
		Head  [][]Inline   `json:"head"`
		Rows  [][][]Inline `json:"rows"`
	}
	type quote struct {
		T      string  `json:"t"`
		Label  string  `json:"label"`
		Blocks []Block `json:"blocks"`
	}
	type evidence struct {
		T     string        `json:"t"`
		Label string        `json:"label"`
		Title string        `json:"title"`
		Rows  []EvidenceRow `json:"rows"`
	}
	type datatable struct {
		T       string     `json:"t"`
		Label   string     `json:"label"`
		Ref     string     `json:"ref"`
		Status  string     `json:"status"`
		Display string     `json:"display,omitempty"`
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
		Total   int        `json:"total"`
	}
	type named struct {
		T     string `json:"t"`
		Label string `json:"label"`
		Name  string `json:"name"`
	}
	type experiment struct {
		T     string `json:"t"`
		Label string `json:"label"`
		ID    string `json:"id"`
	}
	type predict struct {
		T        string     `json:"t"`
		Label    string     `json:"label"`
		Question []Block    `json:"question"`
		Options  [][]Inline `json:"options"`
		Reveal   []Block    `json:"reveal"`
	}
	type callout struct {
		T      string  `json:"t"`
		Label  string  `json:"label"`
		Kind   string  `json:"kind"`
		Title  string  `json:"title"`
		Blocks []Block `json:"blocks"`
	}
	type branch struct {
		T         string  `json:"t"`
		Label     string  `json:"label"`
		Fact      string  `json:"fact"`
		Expect    *string `json:"expect"`
		Chosen    string  `json:"chosen"`
		Then      []Block `json:"then"`
		Else      []Block `json:"else"`
		Unknown   []Block `json:"unknown"`
		Generated bool    `json:"generated_unknown"`
	}
	var v any
	switch b.T {
	case "p":
		v = p{b.T, b.Label, nonNil(b.Inl)}
	case "h":
		v = h{b.T, b.Label, b.Level, nonNil(b.Inl)}
	case "ul", "ol":
		v = list{b.T, b.Label, nonNil(b.Items)}
	case "code":
		v = code{b.T, b.Label, b.Lang, b.Text}
	case "table":
		v = table{b.T, b.Label, nonNil(b.Head), nonNil(b.TableRows)}
	case "quote":
		v = quote{b.T, b.Label, nonNil(b.Blocks)}
	case "evidence":
		v = evidence{b.T, b.Label, b.Title, nonNil(b.Evidence)}
	case "datatable":
		v = datatable{b.T, b.Label, b.Ref, b.Status, b.Display, nonNil(b.Columns), nonNil(b.Rows), b.Total}
	case "diagram", "sim":
		v = named{b.T, b.Label, b.Name}
	case "experiment":
		v = experiment{b.T, b.Label, b.Name}
	case "predict":
		v = predict{b.T, b.Label, nonNil(b.Question), nonNil(b.Options), nonNil(b.Reveal)}
	case "callout":
		v = callout{b.T, b.Label, b.Kind, b.Title, nonNil(b.Blocks)}
	case "branch":
		v = branch{b.T, b.Label, b.Fact, b.Expect, b.Chosen, nonNil(b.Then), nonNil(b.Else), nonNil(b.Unknown), b.Generated}
	default:
		v = struct {
			T string `json:"t"`
		}{b.T}
	}
	return json.Marshal(v)
}

// nonNil turns a nil slice into an empty one, so arrays encode as [] and
// the UI never has to test for null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
