// Package derive computes derived facts: values that no interface reports
// directly but that follow from observations by arithmetic (MemTotal minus
// MemAvailable) or by inference (the page-table depth from the architecture,
// a CPU flag and an address width).
//
// Facts are recomputed from a snapshot every time and never stored, so a
// snapshot and an application version fully determine them. Every fact
// carries the rule that produced it and the ids it read, and a fact whose
// inputs are not all available is "unknown" with the reason: a missing input
// is never replaced by a guess.
package derive

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// Fact statuses.
const (
	StatusOK      = "ok"
	StatusUnknown = "unknown"
)

// Fact is one derived value with its provenance.
type Fact struct {
	ID     string   `json:"id"`
	Value  any      `json:"value"`
	Unit   string   `json:"unit,omitempty"`
	Status string   `json:"status"`           // "ok" | "unknown"
	Reason string   `json:"reason,omitempty"` // when unknown: which input was missing and its status
	Rule   string   `json:"rule"`             // human-readable formula or inference rule
	Inputs []string `json:"inputs"`           // observation ids
	Basis  string   `json:"basis"`            // "arithmetic" | "inference"
}

// All computes every derived fact from s, sorted by id. It is a pure
// function of the observations: no clock, no environment, no map order.
func All(s *snapshot.Snapshot) []Fact {
	in := make(inputs, len(s.Observations))
	for _, o := range s.Observations {
		in[o.ID] = o
	}
	var facts []Fact
	for _, group := range groups {
		facts = append(facts, group(in)...)
	}
	slices.SortFunc(facts, func(a, b Fact) int { return strings.Compare(a.ID, b.ID) })
	return facts
}

// groups lists the fact families. Each returns its facts in any order; All
// sorts them.
var groups = []func(inputs) []Fact{
	envFacts, pagingFacts, memFacts, swapFacts, policyFacts, zoneFacts, probeFacts, schedFacts, storageFacts, netFacts,
}

// inputs indexes a snapshot's observations by id.
type inputs map[string]obs.Observation

// calc gathers the inputs one fact reads. Accessors record every id they
// are asked for and, when an input is unusable, note why; a fact built from
// a calc with any such note is unknown. Accessors return zero values for
// unusable inputs, so fact code must check failed() before dividing.
type calc struct {
	in      inputs
	ids     []string
	reasons []string
}

func (in inputs) calc() *calc { return &calc{in: in} }

// look records id as an input and returns its observation; an id missing
// from the snapshot reads as not collected.
func (c *calc) look(id string) obs.Observation {
	if !slices.Contains(c.ids, id) {
		c.ids = append(c.ids, id)
	}
	o, ok := c.in[id]
	if !ok {
		return obs.Observation{ID: id, Status: obs.StatusNotCollected, Note: "not in this snapshot"}
	}
	return o
}

// fail records a reason the fact cannot be determined.
func (c *calc) fail(format string, args ...any) {
	c.reasons = append(c.reasons, fmt.Sprintf(format, args...))
}

// clone returns an independent copy, for facts that share inputs but add a
// reason of their own.
func (c *calc) clone() *calc {
	return &calc{in: c.in, ids: slices.Clone(c.ids), reasons: slices.Clone(c.reasons)}
}

func (c *calc) failed() bool { return len(c.reasons) > 0 }

// value returns the value of a required input, recording its status as the
// reason when it is not ok.
func (c *calc) value(id string) (any, bool) {
	o := c.look(id)
	if o.Status != obs.StatusOK {
		c.fail("%s is %s", id, statusWords(o.Status))
		return nil, false
	}
	return o.Value, true
}

func statusWords(s obs.Status) string { return strings.ReplaceAll(string(s), "_", " ") }

func (c *calc) int(id string) int64 {
	v, ok := c.value(id)
	if !ok {
		return 0
	}
	n, ok := obs.Int(v)
	if !ok {
		c.fail("%s is not an integer", id)
	}
	return n
}

func (c *calc) float(id string) float64 {
	v, ok := c.value(id)
	if !ok {
		return 0
	}
	f, ok := obs.Float(v)
	if !ok {
		c.fail("%s is not a number", id)
	}
	return f
}

func (c *calc) str(id string) string {
	v, ok := c.value(id)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		c.fail("%s is not a string", id)
	}
	return s
}

func (c *calc) bool(id string) bool {
	v, ok := c.value(id)
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		c.fail("%s is not a boolean", id)
	}
	return b
}

func (c *calc) table(id string) []map[string]any {
	v, ok := c.value(id)
	if !ok {
		return nil
	}
	t, ok := v.([]map[string]any)
	if !ok {
		c.fail("%s is not a table", id)
	}
	return t
}

// row returns the row of table id whose key column equals name.
func (c *calc) row(id, key, name string) map[string]any {
	t := c.table(id)
	if c.failed() {
		return nil
	}
	for _, r := range t {
		if r[key] == name {
			return r
		}
	}
	c.fail("%s has no %s row", id, name)
	return nil
}

// cell returns an integer cell of row, which came from table id.
func (c *calc) cell(id string, row map[string]any, col string) int64 {
	if row == nil {
		return 0
	}
	n, ok := obs.Int(row[col])
	if !ok {
		c.fail("%s: column %s is missing or not an integer", id, col)
	}
	return n
}

// fact builds the fact id from what the calc gathered: unknown with the
// recorded reasons if any input was unusable, otherwise value. Unit and
// basis come from the catalog, which declares every fact.
func (c *calc) fact(id, rule string, value any) Fact {
	e, ok := catalog.Lookup(id)
	if !ok || e.Basis == "" {
		panic("derive: fact " + id + " is not declared in the catalog")
	}
	f := Fact{ID: id, Unit: e.Unit, Basis: e.Basis, Rule: rule, Inputs: slices.Clone(c.ids), Status: StatusOK, Value: value}
	if f.Inputs == nil {
		f.Inputs = []string{}
	}
	if c.failed() {
		f.Status, f.Value, f.Reason = StatusUnknown, nil, strings.Join(c.reasons, "; ")
	}
	return f
}

// tri is a three-valued answer for presence questions, where an interface
// that is absent answers "no" rather than "unknown".
type tri int

const (
	triUnknown tri = iota
	triNo
	triYes
)

// presence reads a presence-type input: ok → test(value), absent or
// unsupported → no (the kernel lacks the feature), anything else unknown.
// It records the input but never fails the calc; the caller decides.
func (c *calc) presence(id string, test func(any) bool) (tri, string) {
	o := c.look(id)
	switch o.Status {
	case obs.StatusOK:
		if test(o.Value) {
			return triYes, ""
		}
		return triNo, ""
	case obs.StatusAbsent, obs.StatusUnsupported:
		return triNo, ""
	}
	return triUnknown, fmt.Sprintf("%s is %s", id, statusWords(o.Status))
}
