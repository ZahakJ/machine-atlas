package curriculum

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// Journey is the curriculum selected for one machine: GET /api/journey.
type Journey struct {
	// Core is every core lesson, track by track; Tracks says which run of
	// it belongs to which subsystem.
	Core     []JourneyLesson `json:"core"`
	Tracks   []JourneyTrack  `json:"tracks"`
	Branches Split           `json:"branches"`
	Planned  Split           `json:"planned"`
	Graph    Graph           `json:"graph"`
}

// JourneyTrack is one subsystem's sequence of core lessons.
type JourneyTrack struct {
	ID      string   `json:"id"` // the subsystem id
	Title   string   `json:"title"`
	Lede    string   `json:"lede"`
	Lessons []string `json:"lessons"` // core lesson ids in order
}

// JourneyLesson is one core lesson in journey order.
type JourneyLesson struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Subsystem     string   `json:"subsystem"`
	Order         int      `json:"order"`
	Prerequisites []string `json:"prerequisites"`
	Notes         []Note   `json:"notes"`
	Coverage      Coverage `json:"coverage"`
}

// Split divides optional material by whether this machine makes it
// relevant. Available items are still there to read; they are not hidden.
type Split struct {
	Relevant  []Option `json:"relevant"`
	Available []Option `json:"available"`
}

// Option is a branch lesson or a planned concept.
type Option struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Subsystem     string   `json:"subsystem"`
	Written       bool     `json:"written"` // false for planned concepts
	Prerequisites []string `json:"prerequisites"`
	Notes         []Note   `json:"notes"`
}

// Note is one relevance rule evaluated on this machine, with the value that
// decided it.
type Note struct {
	When    string  `json:"when"`
	Is      *string `json:"is"`
	Matched bool    `json:"matched"`
	Status  string  `json:"status"`
	Value   string  `json:"value"` // the fact's value, formatted, or its status wording
	Text    string  `json:"text"`
	Reason  string  `json:"reason,omitempty"` // why an unknown fact did not match
}

// Graph is the prerequisite graph of lessons and planned concepts.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode is a lesson (core or branch) or a planned concept.
type GraphNode struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"` // core | branch | planned
	Written bool   `json:"written"`
}

// GraphEdge says From must be understood before To.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Select builds the journey for a snapshot: core lessons in prerequisite
// order (ties broken by order, then id), each with its relevance notes and
// evidence coverage; branch lessons and planned concepts split into
// relevant and available. A rule whose fact is unknown does not match, and
// its note says why. Foundational lessons are never dropped.
func (c *Curriculum) Select(s *snapshot.Snapshot, facts []derive.Fact) Journey {
	m := newMachine(s, facts)
	j := Journey{Branches: emptySplit(), Planned: emptySplit()}
	for _, l := range c.coreOrder() {
		j.Core = append(j.Core, JourneyLesson{
			ID: l.ID, Title: l.Title, Summary: l.Summary, Subsystem: l.Subsystem, Order: l.Order,
			Prerequisites: nonNil(slices.Clone(l.Prerequisites)),
			Notes:         notes(m, l.Relevance), Coverage: coverage(m, l.Uses),
		})
	}
	j.Core = nonNil(j.Core)
	j.Tracks = c.tracks()
	for _, l := range c.Lessons {
		if l.Kind != "branch" {
			continue
		}
		j.Branches.add(Option{ID: l.ID, Title: l.Title, Summary: l.Summary, Subsystem: l.Subsystem, Written: true,
			Prerequisites: nonNil(slices.Clone(l.Prerequisites)), Notes: notes(m, l.Relevance)})
	}
	for _, sub := range c.RoadmapData.Subsystems {
		for _, p := range sub.PlannedConcepts {
			j.Planned.add(Option{ID: p.ID, Title: p.Title, Summary: p.Summary, Subsystem: sub.ID,
				Prerequisites: nonNil(slices.Clone(p.Prerequisites)), Notes: notes(m, p.Relevance)})
		}
	}
	j.Graph = c.graph()
	return j
}

func emptySplit() Split { return Split{Relevant: []Option{}, Available: []Option{}} }

func (s *Split) add(o Option) {
	if slices.ContainsFunc(o.Notes, func(n Note) bool { return n.Matched }) {
		s.Relevant = append(s.Relevant, o)
	} else {
		s.Available = append(s.Available, o)
	}
}

// notes evaluates relevance rules on the machine.
func notes(m *machine, rules []Relevance) []Note {
	out := []Note{}
	for _, r := range rules {
		expect, _ := expectOf(r.Is) // validated at load
		v := m.resolve(r.When)
		n := Note{When: r.When, Is: expect, Status: v.status, Value: v.display(""), Text: r.Note}
		if v.ok() {
			n.Matched = matches(v.value, expect)
		} else {
			why := v.reason
			if why == "" {
				why = StatusText(v.status)
			}
			n.Reason = r.When + " could not be determined (" + why + "), so this rule does not match"
		}
		out = append(out, n)
	}
	return out
}

// coverage counts the ids a lesson uses that have a value on the machine.
func coverage(m *machine, uses []string) Coverage {
	cov := Coverage{Total: len(uses)}
	for _, id := range uses {
		if m.resolve(id).ok() {
			cov.OK++
		}
	}
	return cov
}

// tracks lists the subsystems that have a lesson track, in track order,
// each with its core lessons in journey order.
func (c *Curriculum) tracks() []JourneyTrack {
	out := []JourneyTrack{}
	for _, sub := range c.trackSubsystems() {
		t := JourneyTrack{ID: sub.ID, Title: sub.Track.Title, Lede: sub.Track.Lede, Lessons: []string{}}
		for _, l := range c.coreOrder() {
			if l.Subsystem == sub.ID {
				t.Lessons = append(t.Lessons, l.ID)
			}
		}
		out = append(out, t)
	}
	return out
}

// trackSubsystems returns the roadmap subsystems that have a track, by
// track order then id.
func (c *Curriculum) trackSubsystems() []Subsystem {
	var subs []Subsystem
	for _, s := range c.RoadmapData.Subsystems {
		if s.Track != nil {
			subs = append(subs, s)
		}
	}
	slices.SortFunc(subs, func(a, b Subsystem) int {
		return cmp.Or(cmp.Compare(a.Track.Order, b.Track.Order), strings.Compare(a.ID, b.ID))
	})
	return subs
}

// trackRank is the position of a subsystem's track; subsystems without a
// track come after every track.
func (c *Curriculum) trackRank(subsystem string) int {
	subs := c.trackSubsystems()
	if i := slices.IndexFunc(subs, func(s Subsystem) bool { return s.ID == subsystem }); i >= 0 {
		return i
	}
	return len(subs)
}

// coreOrder returns the core lessons track by track, and within the
// journey in topological order of their prerequisites among core lessons;
// among lessons whose prerequisites are all placed, the earliest track,
// then the lowest order (then id) comes first. Prerequisites that
// are not core lessons do not constrain the order, and Load has rejected
// cycles.
func (c *Curriculum) coreOrder() []*Lesson {
	var pending []*Lesson
	for _, l := range c.Lessons {
		if l.Kind == "core" {
			pending = append(pending, l)
		}
	}
	rank := map[string]int{}
	for _, l := range pending {
		rank[l.Subsystem] = c.trackRank(l.Subsystem)
	}
	slices.SortFunc(pending, func(a, b *Lesson) int {
		return cmp.Or(cmp.Compare(rank[a.Subsystem], rank[b.Subsystem]), cmp.Compare(a.Order, b.Order), strings.Compare(a.ID, b.ID))
	})
	placed := map[string]bool{}
	waiting := func(l *Lesson) bool {
		return slices.ContainsFunc(l.Prerequisites, func(p string) bool {
			pre, ok := c.byID[p]
			return ok && pre.Kind == "core" && !placed[p]
		})
	}
	var out []*Lesson
	for len(pending) > 0 {
		next := slices.IndexFunc(pending, func(l *Lesson) bool { return !waiting(l) })
		if next < 0 {
			break // a cycle; unreachable after Load
		}
		placed[pending[next].ID] = true
		out = append(out, pending[next])
		pending = slices.Delete(pending, next, next+1)
	}
	return out
}

// graph lists lessons and planned concepts with their prerequisite edges,
// sorted so the output is stable.
func (c *Curriculum) graph() Graph {
	g := Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, l := range c.Lessons {
		g.Nodes = append(g.Nodes, GraphNode{ID: l.ID, Title: l.Title, Kind: l.Kind, Written: true})
		for _, p := range l.Prerequisites {
			g.Edges = append(g.Edges, GraphEdge{From: p, To: l.ID})
		}
	}
	for _, s := range c.RoadmapData.Subsystems {
		for _, p := range s.PlannedConcepts {
			g.Nodes = append(g.Nodes, GraphNode{ID: p.ID, Title: p.Title, Kind: "planned"})
			for _, pre := range p.Prerequisites {
				g.Edges = append(g.Edges, GraphEdge{From: pre, To: p.ID})
			}
		}
	}
	slices.SortFunc(g.Nodes, func(a, b GraphNode) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(g.Edges, func(a, b GraphEdge) int {
		return cmp.Or(strings.Compare(a.From, b.From), strings.Compare(a.To, b.To))
	})
	return g
}
