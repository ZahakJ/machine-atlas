package curriculum

import "slices"

// Users lists what refers to one id.
type Users struct {
	Lessons    []string `json:"lessons"`
	Components []string `json:"components"`
}

// Usage is the reverse index GET /api/usage serves: for every observation
// or derived id, the lessons that reference it (in text, evidence, tables,
// branches or relevance rules) and the components that observe it. It does
// not depend on a snapshot. Encoded as JSON, the map's keys are sorted.
func (c *Curriculum) Usage() map[string]Users {
	out := map[string]Users{}
	add := func(id string, lesson, component string) {
		u := out[id]
		if lesson != "" && !slices.Contains(u.Lessons, lesson) {
			u.Lessons = append(u.Lessons, lesson)
		}
		if component != "" && !slices.Contains(u.Components, component) {
			u.Components = append(u.Components, component)
		}
		out[id] = u
	}
	for _, l := range c.Lessons {
		for _, id := range references(l) {
			add(id, l.ID, "")
		}
		for _, r := range l.Relevance {
			add(r.When, l.ID, "")
		}
	}
	for _, comp := range c.ComponentData {
		for _, id := range comp.Observe {
			add(id, "", comp.ID)
		}
		if comp.Instances != nil {
			add(comp.Instances.From, "", comp.ID)
		}
	}
	for id, u := range out {
		slices.Sort(u.Lessons)
		slices.Sort(u.Components)
		out[id] = Users{Lessons: nonNil(u.Lessons), Components: nonNil(u.Components)}
	}
	return out
}
