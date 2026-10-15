package curriculum

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// Edge bases. An architecture edge is authored general knowledge ("page
// tables depend on physical frames"); an observed edge connects instances
// found in this snapshot ("zone Normal lies within node 0"). The UI never
// draws one as the other.
const (
	BasisArchitecture = "architecture"
	BasisObserved     = "observed"
)

// ComponentGraph is the Explore view for one machine: GET /api/components.
type ComponentGraph struct {
	Nodes []ComponentNode `json:"nodes"`
	Edges []ComponentEdge `json:"edges"`
}

// ComponentNode is a component with its values on this machine.
type ComponentNode struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Layer     string     `json:"layer"`
	Subsystem string     `json:"subsystem"`
	Status    string     `json:"status"`
	What      string     `json:"what"`
	Why       string     `json:"why"`
	Varies    string     `json:"varies"`
	DependsOn []string   `json:"depends_on"`
	UsedBy    []string   `json:"used_by"`
	Observe   []Value    `json:"observe"`
	Lessons   []string   `json:"lessons"`
	Instances []Instance `json:"instances"`
	// InstanceSource is the table the instances were read from, with its
	// status, so "no instances" can be told apart from "could not look".
	InstanceSource *Value `json:"instance_source,omitempty"`
}

// Instance is one occurrence of a component found in a table observation.
type Instance struct {
	ID    string            `json:"id"`
	Key   string            `json:"key"`
	Title string            `json:"title"`
	Cells map[string]string `json:"cells"` // formatted; encoded with sorted keys
}

// ComponentEdge says From depends on, or lies within, To.
type ComponentEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Basis    string `json:"basis"`
	Relation string `json:"relation"`
}

// Value is one id resolved for display outside a lesson: on component
// cards, the roadmap overview and journey notes.
type Value struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Display string `json:"display"`
	Unit    string `json:"unit"`
	Status  string `json:"status"`
	Label   string `json:"label"`
}

func (m *machine) value(id string) Value {
	v := m.resolve(id)
	return Value{ID: id, Kind: v.kind, Title: title(id), Display: v.display(""), Unit: v.unit, Status: v.status, Label: v.label()}
}

// Components resolves the component graph against a snapshot: each node's
// observations and discovered instances, the authored dependency edges
// (basis architecture), and edges between instances that the data itself
// shows (basis observed).
func (c *Curriculum) Components(s *snapshot.Snapshot, facts []derive.Fact) ComponentGraph {
	m := newMachine(s, facts)
	g := ComponentGraph{Nodes: []ComponentNode{}, Edges: []ComponentEdge{}}
	usedBy := map[string][]string{}
	for _, comp := range c.ComponentData {
		for _, d := range comp.DependsOn {
			usedBy[d] = append(usedBy[d], comp.ID)
			g.Edges = append(g.Edges, ComponentEdge{From: comp.ID, To: d, Basis: BasisArchitecture, Relation: "depends on"})
		}
	}
	instances := map[string][]Instance{} // component id → instances, for observed edges
	rows := map[string][]map[string]any{}
	for _, comp := range c.ComponentData {
		n := ComponentNode{
			ID: comp.ID, Title: comp.Title, Layer: comp.Layer, Subsystem: comp.Subsystem, Status: comp.Status,
			What: comp.What, Why: comp.Why, Varies: comp.Varies,
			DependsOn: nonNil(slices.Clone(comp.DependsOn)), Lessons: nonNil(slices.Clone(comp.Lessons)),
			Observe: []Value{}, Instances: []Instance{},
		}
		ub := slices.Clone(usedBy[comp.ID])
		slices.Sort(ub)
		n.UsedBy = nonNil(ub)
		for _, id := range comp.Observe {
			n.Observe = append(n.Observe, m.value(id))
		}
		if spec := comp.Instances; spec != nil {
			src := m.value(spec.From)
			n.InstanceSource = &src
			table, _ := m.resolve(spec.From).value.([]map[string]any)
			for _, r := range table {
				n.Instances = append(n.Instances, instance(comp.ID, spec, r))
			}
			instances[comp.ID] = n.Instances
			rows[comp.ID] = table
		}
		g.Nodes = append(g.Nodes, n)
	}
	for _, comp := range c.ComponentData {
		spec := comp.Instances
		if spec == nil || spec.Within == nil {
			continue
		}
		for i, r := range rows[comp.ID] {
			key := rawString(r[spec.Within.Column])
			for _, parent := range instances[spec.Within.Component] {
				if parent.Key == key {
					g.Edges = append(g.Edges, ComponentEdge{From: instances[comp.ID][i].ID, To: parent.ID, Basis: BasisObserved, Relation: spec.Within.Relation})
				}
			}
		}
	}
	slices.SortFunc(g.Edges, func(a, b ComponentEdge) int {
		return cmp.Or(strings.Compare(a.From, b.From), strings.Compare(a.To, b.To), strings.Compare(a.Basis, b.Basis))
	})
	return g
}

// instance builds the instance of component id described by table row r.
func instance(id string, spec *InstanceSpec, r map[string]any) Instance {
	var parts []string
	for _, k := range strings.Split(spec.Key, ",") {
		parts = append(parts, rawString(r[k]))
	}
	key := strings.Join(parts, "/")
	cells := make(map[string]string, len(r))
	for col, v := range r {
		cells[col] = formatCell(col, v, "")
	}
	return Instance{ID: id + "/" + key, Key: key, Title: fillTemplate(spec.Title, r), Cells: cells}
}

// fillTemplate replaces {column} with the row's cell as stored, and
// {column|fmt} with the cell in a chip format ({size_bytes|bytes}).
func fillTemplate(t string, r map[string]any) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(t, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(t[open:], '}')
		if end < 0 {
			break
		}
		b.WriteString(t[:open])
		col, format, _ := strings.Cut(t[open+1:open+end], "|")
		if v, ok := r[col]; ok {
			b.WriteString(Format(v, "", cmp.Or(format, "raw")))
		} else {
			b.WriteString("?")
		}
		t = t[open+end+1:]
	}
	b.WriteString(t)
	return b.String()
}
