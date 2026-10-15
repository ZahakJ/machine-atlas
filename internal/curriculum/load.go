package curriculum

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	content "github.com/ZahakJ/machine-atlas/curriculum"
)

// File names inside a curriculum file system.
const (
	lessonGlob     = "lessons/*.md"
	roadmapFile    = "roadmap.json"
	componentsFile = "components.json"
)

// Curriculum is the authored content, parsed: every lesson, the roadmap and
// the component graph.
type Curriculum struct {
	Lessons       []*Lesson // sorted by id
	RoadmapData   RoadmapFile
	ComponentData []Component
	byID          map[string]*Lesson
}

// RoadmapFile is curriculum/roadmap.json.
type RoadmapFile struct {
	CurriculumVersion string      `json:"curriculum_version"`
	Subsystems        []Subsystem `json:"subsystems"`
}

// Subsystem is one card of the roadmap overview.
type Subsystem struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Layer           string           `json:"layer"`  // hardware | kernel | userspace
	Status          string           `json:"status"` // implemented | overview | planned
	Summary         string           `json:"summary"`
	MemoryLink      string           `json:"memory_link"`
	Observe         []string         `json:"observe"`
	Track           *Track           `json:"track,omitempty"` // set when the subsystem has a lesson track
	PlannedConcepts []PlannedConcept `json:"planned_concepts"`
}

// Track is a subsystem's sequence of core lessons, shown as its own journey.
type Track struct {
	Title string `json:"title"`
	Order int    `json:"order"`
	Lede  string `json:"lede"`
}

// PlannedConcept is a lesson not written yet, shown so the reader knows it
// exists and whether it matters on this machine.
type PlannedConcept struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Summary       string      `json:"summary"`
	Prerequisites []string    `json:"prerequisites"`
	Relevance     []Relevance `json:"relevance"`
}

// Component is one node of the Explore graph as authored.
type Component struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Layer     string        `json:"layer"`
	Subsystem string        `json:"subsystem"`
	Status    string        `json:"status"`
	What      string        `json:"what"`
	Why       string        `json:"why"`
	Varies    string        `json:"varies"`
	DependsOn []string      `json:"depends_on"`
	Observe   []string      `json:"observe"`
	Lessons   []string      `json:"lessons"`
	Instances *InstanceSpec `json:"instances,omitempty"`
}

// InstanceSpec says how to discover a component's instances on a machine:
// one per row of the table observation From, identified by the Key columns
// (comma-separated) and titled by Title, where {column} is replaced by the
// row's cell. Within, when set, links each instance to the instance of
// another component whose key equals the named column: zones lie within a
// NUMA node, swap areas on a block device.
type InstanceSpec struct {
	From   string  `json:"from"`
	Key    string  `json:"key"`
	Title  string  `json:"title"`
	Within *Within `json:"within,omitempty"`
}

// Within names the component an instance belongs to and the column of this
// table that holds that component's key.
type Within struct {
	Component string `json:"component"`
	Column    string `json:"column"`
	Relation  string `json:"relation"`
}

var (
	defaultOnce sync.Once
	defaultCur  *Curriculum
	defaultErr  error
)

// Default returns the curriculum embedded in this binary, parsed once.
func Default() (*Curriculum, error) {
	defaultOnce.Do(func() { defaultCur, defaultErr = Load(content.FS) })
	return defaultCur, defaultErr
}

// Embedded returns the file system of the embedded curriculum, for lint.
func Embedded() fs.FS { return content.FS }

// Load parses a curriculum from fsys. It fails on any parse error and on a
// prerequisite cycle; dangling references and the other content rules are
// Lint's job, so a curriculum under construction still loads.
func Load(fsys fs.FS) (*Curriculum, error) {
	c, errs := load(fsys)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if cycle := c.cycle(); cycle != nil {
		return nil, fmt.Errorf("prerequisite cycle: %s", strings.Join(cycle, " → "))
	}
	return c, nil
}

// load parses everything it can and returns every error it met.
func load(fsys fs.FS) (*Curriculum, []error) {
	c := &Curriculum{byID: map[string]*Lesson{}}
	var errs []error
	names, err := fs.Glob(fsys, lessonGlob)
	if err != nil {
		return c, []error{err}
	}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		l, err := Parse(name, data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := c.byID[l.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: lesson id %q is also used by %s", name, l.ID, prev.File))
			continue
		}
		c.byID[l.ID] = l
		c.Lessons = append(c.Lessons, l)
	}
	slices.SortFunc(c.Lessons, func(a, b *Lesson) int { return strings.Compare(a.ID, b.ID) })
	if err := decodeFile(fsys, roadmapFile, &c.RoadmapData); err != nil {
		errs = append(errs, err)
	}
	if err := decodeFile(fsys, componentsFile, &c.ComponentData); err != nil {
		errs = append(errs, err)
	}
	for _, sub := range c.RoadmapData.Subsystems {
		for i, pc := range sub.PlannedConcepts {
			for j, r := range pc.Relevance {
				if _, err := expectOf(r.Is); err != nil {
					errs = append(errs, fmt.Errorf("%s: %s.planned_concepts[%d].relevance[%d].is: %v", roadmapFile, sub.ID, i, j, err))
				}
			}
		}
	}
	return c, errs
}

// decodeFile decodes a JSON file strictly: unknown fields are typos.
func decodeFile(fsys fs.FS, name string, v any) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if dec.More() {
		return fmt.Errorf("%s: unexpected data after the JSON value", name)
	}
	return nil
}

// Lesson returns the lesson with this id.
func (c *Curriculum) Lesson(id string) (*Lesson, bool) {
	l, ok := c.byID[id]
	return l, ok
}

// planned returns the planned concept with this id and its subsystem.
func (c *Curriculum) planned(id string) (PlannedConcept, string, bool) {
	for _, s := range c.RoadmapData.Subsystems {
		for _, p := range s.PlannedConcepts {
			if p.ID == id {
				return p, s.ID, true
			}
		}
	}
	return PlannedConcept{}, "", false
}

// cycle returns one prerequisite cycle among the lessons, or nil. Missing
// prerequisites are ignored here; Lint reports them.
func (c *Curriculum) cycle() []string {
	const (
		unvisited = iota
		active
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) []string
	visit = func(id string) []string {
		switch state[id] {
		case active:
			i := slices.Index(stack, id)
			return append(slices.Clone(stack[i:]), id)
		case done:
			return nil
		}
		l, ok := c.byID[id]
		if !ok {
			return nil
		}
		state[id] = active
		stack = append(stack, id)
		for _, p := range l.Prerequisites {
			if cyc := visit(p); cyc != nil {
				return cyc
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return nil
	}
	for _, l := range c.Lessons {
		if cyc := visit(l.ID); cyc != nil {
			return cyc
		}
	}
	return nil
}

// lessonFileID is the id a lesson file must declare: its base name.
func lessonFileID(file string) string { return strings.TrimSuffix(path.Base(file), ".md") }
