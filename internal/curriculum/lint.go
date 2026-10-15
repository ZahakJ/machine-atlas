package curriculum

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// LintOptions relax lint for a curriculum under construction.
type LintOptions struct {
	// AllowMissingLessons reports references to lessons that do not exist
	// yet (prerequisites, next, deeper, lesson: links, component lessons) as
	// warnings instead of errors. Meant for building the curriculum one
	// lesson at a time; releases lint strictly.
	AllowMissingLessons bool
}

// Problem is one lint finding.
type Problem struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
	Warning bool   `json:"warning,omitempty"`
}

func (p Problem) String() string {
	where := p.File
	if p.Line > 0 {
		where = fmt.Sprintf("%s:%d", p.File, p.Line)
	}
	sev := "error"
	if p.Warning {
		sev = "warning"
	}
	return fmt.Sprintf("%s: %s [%s] %s", where, sev, p.Rule, p.Message)
}

// Lint rules, named in each Problem.
const (
	RuleParse         = "parse"        // the file does not parse: syntax, front matter, unknown or misordered section
	RuleFrontMatter   = "front-matter" // a front matter field is missing or invalid
	RuleSection       = "section"      // a required section is missing
	RuleGraph         = "graph"        // a prerequisite cycle
	RuleDangling      = "dangling"     // a reference to a lesson, planned concept or component that does not exist
	RuleUses          = "uses"         // uses differs from the ids the text references
	RuleCatalog       = "catalog"      // an id not in the catalog, or a table directive on a non-table
	RuleName          = "name"         // an unknown diagram, sim or experiment
	RuleCore          = "core"         // a core lesson without :::cannot, :::predict or references
	RuleRoadmap       = "roadmap"      // roadmap.json is incomplete or invalid
	RuleComponents    = "components"   // components.json is incomplete or invalid
	RuleMissingLesson = "missing-lesson"
)

var (
	layers         = []string{"hardware", "kernel", "userspace"}
	statuses       = []string{"implemented", "overview", "planned"}
	lessonKinds    = []string{"core", "branch"}
	referenceKinds = []string{"kernel-doc", "man-page", "spec", "source", "lwn", "book", "paper", "other"}
)

// Lint checks a curriculum file system against every rule of ARCHITECTURE.md §7
// "Lint" and returns the problems sorted by file and line. A curriculum
// passes when no problem is an error.
func Lint(fsys fs.FS, opt LintOptions) []Problem {
	l := &linter{opt: opt}
	c, errs := load(fsys)
	for _, err := range errs {
		var pe *ParseError
		if errors.As(err, &pe) {
			l.add(pe.File, pe.Line, RuleParse, "%s", pe.Msg)
		} else {
			l.add("", 0, RuleParse, "%v", err)
		}
	}
	l.c = c
	subsystems := map[string]bool{}
	tracked := map[string]bool{}
	for _, s := range c.RoadmapData.Subsystems {
		subsystems[s.ID] = true
		tracked[s.ID] = s.Track != nil
	}
	for _, lesson := range c.Lessons {
		l.lesson(lesson, subsystems)
		if lesson.Kind == "core" && subsystems[lesson.Subsystem] && !tracked[lesson.Subsystem] {
			l.add(lesson.File, 2, RuleFrontMatter, "core lesson in subsystem %q, which has no track in the roadmap", lesson.Subsystem)
		}
	}
	if cyc := c.cycle(); cyc != nil {
		l.add(lessonGlob, 0, RuleGraph, "prerequisite cycle: %s", strings.Join(cyc, " → "))
	}
	l.roadmap()
	l.components()
	slices.SortStableFunc(l.problems, func(a, b Problem) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	return l.problems
}

// Failed reports whether any problem is an error.
func Failed(problems []Problem) bool {
	return slices.ContainsFunc(problems, func(p Problem) bool { return !p.Warning })
}

type linter struct {
	opt      LintOptions
	c        *Curriculum
	problems []Problem
}

func (l *linter) add(file string, line int, rule, format string, args ...any) {
	l.problems = append(l.problems, Problem{File: file, Line: line, Rule: rule, Message: fmt.Sprintf(format, args...)})
}

// lessonRef checks a reference to a lesson ("planned:" names a planned
// concept). Missing lessons are errors unless the options allow them.
func (l *linter) lessonRef(file string, line int, what, id string) {
	if pid, ok := strings.CutPrefix(id, "planned:"); ok {
		if _, _, found := l.c.planned(pid); !found {
			l.add(file, line, RuleDangling, "%s %q is not a planned concept in %s", what, id, roadmapFile)
		}
		return
	}
	if _, ok := l.c.Lesson(id); ok {
		return
	}
	if _, _, ok := l.c.planned(id); ok && what == "prerequisite" {
		l.add(file, line, RuleDangling, "%s %q is a planned concept; a lesson cannot require what is not written", what, id)
		return
	}
	l.add(file, line, RuleDangling, "%s %q: no such lesson", what, id)
	if l.opt.AllowMissingLessons {
		p := &l.problems[len(l.problems)-1]
		p.Rule, p.Warning = RuleMissingLesson, true
	}
}

// catalogID checks that id is declared in the catalog.
func (l *linter) catalogID(file string, line int, id string) {
	if _, ok := catalog.Lookup(id); !ok {
		l.add(file, line, RuleCatalog, "%s is not in the catalog", id)
	}
}

func (l *linter) lesson(les *Lesson, subsystems map[string]bool) {
	f := les.File
	m := les.Meta
	if m.ID != lessonFileID(f) {
		l.add(f, 2, RuleFrontMatter, "id %q does not match the file name", m.ID)
	}
	if !lessonID.MatchString(m.ID) {
		l.add(f, 2, RuleFrontMatter, "id %q must be lower-case words joined by hyphens", m.ID)
	}
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Summary) == "" {
		l.add(f, 2, RuleFrontMatter, "title and summary must not be empty")
	}
	if !subsystems[m.Subsystem] {
		l.add(f, 2, RuleFrontMatter, "subsystem %q is not a roadmap subsystem", m.Subsystem)
	}
	if !slices.Contains(lessonKinds, m.Kind) {
		l.add(f, 2, RuleFrontMatter, "kind %q is not core or branch", m.Kind)
	}
	if m.Kind == "core" && m.Order < 1 {
		l.add(f, 2, RuleFrontMatter, "a core lesson needs an order of 1 or more")
	}
	for _, r := range m.References {
		if r.Title == "" || !strings.HasPrefix(r.URL, "https://") || !slices.Contains(referenceKinds, r.Kind) {
			l.add(f, 2, RuleFrontMatter, "reference %q needs a title, an https URL and a kind (%s)", r.Title, strings.Join(referenceKinds, ", "))
		}
	}
	for _, v := range m.VersionNotes {
		if v.Applies == "" || v.Text == "" {
			l.add(f, 2, RuleFrontMatter, "a version note needs applies and text")
		}
	}
	for _, r := range m.Relevance {
		l.catalogID(f, 2, r.When)
		if r.Note == "" {
			l.add(f, 2, RuleFrontMatter, "relevance rule on %s has no note", r.When)
		}
	}
	for _, p := range m.Prerequisites {
		l.lessonRef(f, 2, "prerequisite", p)
	}
	for _, n := range m.Next {
		l.lessonRef(f, 2, "next", n)
	}
	for _, d := range m.Deeper {
		l.lessonRef(f, 2, "deeper", d)
	}

	present := map[string]bool{}
	for _, s := range les.Sections {
		present[s.ID] = true
	}
	for _, s := range sectionOrder {
		if s.id != "see-it" && !present[s.id] {
			l.add(f, 0, RuleSection, "missing section %q", s.title)
		}
	}

	refs := references(les)
	declared := slices.Sorted(slices.Values(m.Uses))
	for _, id := range refs {
		l.catalogID(f, 0, id)
		if !slices.Contains(declared, id) {
			l.add(f, 2, RuleUses, "%s is referenced but not listed in uses", id)
		}
	}
	for i, id := range declared {
		if !slices.Contains(refs, id) {
			l.add(f, 2, RuleUses, "%s is listed in uses but not referenced", id)
		}
		if i > 0 && declared[i-1] == id {
			l.add(f, 2, RuleUses, "%s is listed in uses twice", id)
		}
	}

	var cannot, predict bool
	for _, s := range les.Sections {
		walk(s.Blocks, func(b *Block) {
			switch b.T {
			case "diagram":
				l.name(f, b.Line, "diagram", b.Name, Diagrams)
			case "sim":
				l.name(f, b.Line, "sim", b.Name, Sims)
			case "experiment":
				l.name(f, b.Line, "experiment", b.Name, Experiments)
			case "datatable":
				l.datatable(f, b)
			case "predict":
				predict = true
			case "callout":
				cannot = cannot || b.Kind == "cannot"
			}
		}, func(b *Block, x *Inline) {
			if x.T != "link" {
				return
			}
			switch x.Kind {
			case LinkLesson:
				l.lessonRef(f, b.Line, "link to lesson", x.Href)
			case LinkComponent:
				if !slices.ContainsFunc(l.c.ComponentData, func(c Component) bool { return c.ID == x.Href }) {
					l.add(f, b.Line, RuleDangling, "link to component %q: no such component", x.Href)
				}
			}
		})
	}
	if m.Kind == "core" {
		if !cannot {
			l.add(f, 0, RuleCore, "a core lesson needs a :::cannot callout")
		}
		if !predict {
			l.add(f, 0, RuleCore, "a core lesson needs a :::predict")
		}
		if len(m.References) == 0 {
			l.add(f, 2, RuleCore, "a core lesson needs references")
		}
	}
}

func (l *linter) name(file string, line int, kind, name string, known []string) {
	if !slices.Contains(known, name) {
		l.add(file, line, RuleName, "unknown %s %q (known: %s)", kind, name, strings.Join(known, ", "))
	}
}

func (l *linter) datatable(file string, b *Block) {
	e, ok := catalog.Lookup(b.Ref)
	if !ok {
		return // reported by the catalog check on references
	}
	if e.Type != obs.TypeTable {
		l.add(file, b.Line, RuleCatalog, ":::table %s: the catalog declares type %s, not a table", b.Ref, e.Type)
		return
	}
	for _, col := range b.Columns {
		if !e.HasColumn(col) {
			l.add(file, b.Line, RuleCatalog, ":::table %s: column %q is not declared (columns: %s)", b.Ref, col, strings.Join(e.Columns, ", "))
		}
	}
}

func (l *linter) roadmap() {
	const f = roadmapFile
	r := l.c.RoadmapData
	if r.CurriculumVersion == "" {
		l.add(f, 0, RuleRoadmap, "curriculum_version is empty")
	}
	if len(r.Subsystems) != 9 {
		l.add(f, 0, RuleRoadmap, "%d subsystems, the roadmap has nine", len(r.Subsystems))
	}
	seen := map[string]bool{}
	for _, s := range r.Subsystems {
		if seen[s.ID] {
			l.add(f, 0, RuleRoadmap, "subsystem %q appears twice", s.ID)
		}
		seen[s.ID] = true
		if s.Title == "" || s.Summary == "" || s.MemoryLink == "" {
			l.add(f, 0, RuleRoadmap, "subsystem %q needs a title, summary and memory_link", s.ID)
		}
		if !slices.Contains(layers, s.Layer) || !slices.Contains(statuses, s.Status) {
			l.add(f, 0, RuleRoadmap, "subsystem %q: layer must be one of %v and status one of %v", s.ID, layers, statuses)
		}
		for _, id := range s.Observe {
			l.catalogID(f, 0, id)
		}
		if t := s.Track; t != nil && (t.Title == "" || t.Lede == "" || t.Order < 1) {
			l.add(f, 0, RuleRoadmap, "subsystem %q: a track needs a title, a lede and an order of 1 or more", s.ID)
		}
		for _, p := range s.PlannedConcepts {
			if seen["planned:"+p.ID] {
				l.add(f, 0, RuleRoadmap, "planned concept %q appears twice", p.ID)
			}
			seen["planned:"+p.ID] = true
			if !lessonID.MatchString(p.ID) || p.Title == "" || p.Summary == "" {
				l.add(f, 0, RuleRoadmap, "planned concept %q needs a lower-case id, a title and a summary", p.ID)
			}
			if _, written := l.c.Lesson(p.ID); written {
				l.add(f, 0, RuleRoadmap, "planned concept %q is a written lesson; remove it from the roadmap", p.ID)
			}
			for _, pre := range p.Prerequisites {
				if _, _, ok := l.c.planned(pre); !ok {
					l.lessonRef(f, 0, "prerequisite of "+p.ID, pre)
				}
			}
			for _, rel := range p.Relevance {
				l.catalogID(f, 0, rel.When)
				if rel.Note == "" {
					l.add(f, 0, RuleRoadmap, "relevance rule of %q on %s has no note", p.ID, rel.When)
				}
			}
		}
	}
}

func (l *linter) components() {
	const f = componentsFile
	ids := map[string]bool{}
	for _, c := range l.c.ComponentData {
		if ids[c.ID] {
			l.add(f, 0, RuleComponents, "component %q appears twice", c.ID)
		}
		ids[c.ID] = true
	}
	subsystems := map[string]bool{}
	for _, s := range l.c.RoadmapData.Subsystems {
		subsystems[s.ID] = true
	}
	for _, c := range l.c.ComponentData {
		if !lessonID.MatchString(c.ID) || c.Title == "" || c.What == "" || c.Why == "" || c.Varies == "" {
			l.add(f, 0, RuleComponents, "component %q needs a lower-case id, title, what, why and varies", c.ID)
		}
		if !slices.Contains(layers, c.Layer) || !slices.Contains(statuses, c.Status) || !subsystems[c.Subsystem] {
			l.add(f, 0, RuleComponents, "component %q: layer must be one of %v, status one of %v, subsystem a roadmap subsystem", c.ID, layers, statuses)
		}
		for _, d := range c.DependsOn {
			if !ids[d] {
				l.add(f, 0, RuleDangling, "component %q depends on %q: no such component", c.ID, d)
			}
		}
		for _, id := range c.Observe {
			l.catalogID(f, 0, id)
		}
		for _, les := range c.Lessons {
			l.lessonRef(f, 0, "lesson of component "+c.ID, les)
		}
		if spec := c.Instances; spec != nil {
			l.instances(c.ID, spec, ids)
		}
	}
}

func (l *linter) instances(id string, spec *InstanceSpec, ids map[string]bool) {
	const f = componentsFile
	e, ok := catalog.Lookup(spec.From)
	if !ok || e.Type != obs.TypeTable {
		l.add(f, 0, RuleComponents, "component %q: instances.from %q is not a table in the catalog", id, spec.From)
		return
	}
	cols := strings.Split(spec.Key, ",")
	if spec.Within != nil {
		cols = append(cols, spec.Within.Column)
		if !ids[spec.Within.Component] {
			l.add(f, 0, RuleDangling, "component %q: instances.within names %q: no such component", id, spec.Within.Component)
		}
	}
	for _, col := range cols {
		if !e.HasColumn(col) {
			l.add(f, 0, RuleComponents, "component %q: column %q is not a column of %s", id, col, spec.From)
		}
	}
	if spec.Title == "" {
		l.add(f, 0, RuleComponents, "component %q: instances need a title template", id)
	}
}
