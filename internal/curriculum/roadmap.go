package curriculum

import (
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// RoadmapView is the roadmap resolved for one machine: GET /api/roadmap.
type RoadmapView struct {
	CurriculumVersion string          `json:"curriculum_version"`
	Subsystems        []SubsystemView `json:"subsystems"`
}

// SubsystemView is one overview card.
type SubsystemView struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Layer           string   `json:"layer"`
	Status          string   `json:"status"`
	Summary         string   `json:"summary"`
	MemoryLink      string   `json:"memory_link"`
	Observe         []Value  `json:"observe"`
	Lessons         []string `json:"lessons"` // written lessons of this subsystem
	PlannedConcepts []Option `json:"planned_concepts"`
}

// Roadmap resolves roadmap.json against a snapshot: each card's
// observations with display values and statuses, its written lessons, and
// its planned concepts with their relevance on this machine.
func (c *Curriculum) Roadmap(s *snapshot.Snapshot, facts []derive.Fact) RoadmapView {
	m := newMachine(s, facts)
	v := RoadmapView{CurriculumVersion: c.RoadmapData.CurriculumVersion, Subsystems: []SubsystemView{}}
	for _, sub := range c.RoadmapData.Subsystems {
		sv := SubsystemView{
			ID: sub.ID, Title: sub.Title, Layer: sub.Layer, Status: sub.Status, Summary: sub.Summary,
			MemoryLink: sub.MemoryLink, Observe: []Value{}, Lessons: []string{}, PlannedConcepts: []Option{},
		}
		for _, id := range sub.Observe {
			sv.Observe = append(sv.Observe, m.value(id))
		}
		for _, l := range c.coreOrder() {
			if l.Subsystem == sub.ID {
				sv.Lessons = append(sv.Lessons, l.ID)
			}
		}
		for _, l := range c.Lessons {
			if l.Kind != "core" && l.Subsystem == sub.ID {
				sv.Lessons = append(sv.Lessons, l.ID)
			}
		}
		for _, p := range sub.PlannedConcepts {
			sv.PlannedConcepts = append(sv.PlannedConcepts, Option{
				ID: p.ID, Title: p.Title, Summary: p.Summary, Subsystem: sub.ID,
				Prerequisites: nonNil(p.Prerequisites), Notes: notes(m, p.Relevance),
			})
		}
		v.Subsystems = append(v.Subsystems, sv)
	}
	return v
}
