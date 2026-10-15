// Package experiment holds the opt-in experiments (ARCHITECTURE.md §8). Each one
// changes the machine a little, so it is never started by collection or by
// loading a page: a person asks for it, after reading what it does. It runs
// in a child process, "atlas __experiment <id>", under a hard time bound,
// and returns timestamped measurements plus the outcome a fixed rule picked.
//
// The parent side (List, Run) is portable; the children are Linux only.
package experiment

import (
	"context"
	"errors"
)

// Resources states the most an experiment will use.
type Resources struct {
	MemoryBytes      int64 `json:"memory_bytes"`
	DiskBytes        int64 `json:"disk_bytes"`
	TimeBoundSeconds int   `json:"time_bound_seconds"`
}

// Outcome is one possible result of an experiment and what it means.
type Outcome struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Interpretation string `json:"interpretation"`
}

// Info describes an experiment before it is run: what it asks, what it
// does step by step, what it uses, what it does not do, and the outcomes it
// can have.
type Info struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Question          string    `json:"question"`
	Steps             []string  `json:"steps"`
	Resources         Resources `json:"resources"`
	DoesNot           []string  `json:"does_not"`
	Outcomes          []Outcome `json:"outcomes"`
	Available         bool      `json:"available"`
	UnavailableReason string    `json:"unavailable_reason"`
}

// Measurement is one value recorded during a run. Value is a number, a
// string or a bool; At is RFC 3339 UTC with nanoseconds.
type Measurement struct {
	ID    string `json:"id"`
	Step  string `json:"step"`
	Label string `json:"label"`
	Value any    `json:"value"`
	Unit  string `json:"unit"`
	At    string `json:"at"`
}

// Result is what one run produced. Outcome is the id of the matched
// Outcome; Notes say what the measurements cannot establish and which rule
// chose the outcome.
type Result struct {
	ID           string        `json:"id"`
	StartedAt    string        `json:"started_at"`
	FinishedAt   string        `json:"finished_at"`
	Measurements []Measurement `json:"measurements"`
	Outcome      string        `json:"outcome"`
	Notes        []string      `json:"notes"`
}

// Errors Run returns, for callers that map them to a status.
var (
	ErrUnknown     = errors.New("no such experiment")
	ErrUnavailable = errors.New("experiment unavailable")
	ErrBusy        = errors.New("another experiment is running")
)

// List returns every experiment in a stable order. On a build that cannot
// run them, each is listed with Available false and the reason.
func List() []Info {
	out := make([]Info, len(experiments))
	for i, e := range experiments {
		out[i] = e.info()
	}
	return out
}

// Run runs experiment id in a child process ("atlas __experiment <id>")
// and returns its validated result. Only the ids List returns can be run,
// one at a time per process; the child is killed when its time bound
// passes or ctx is cancelled.
func Run(ctx context.Context, id string) (Result, error) {
	return defaultRunner().run(ctx, id)
}

// lookup returns the experiment with id.
func lookup(id string) (Info, bool) {
	for _, e := range experiments {
		if e.ID == id {
			return e.info(), true
		}
	}
	return Info{}, false
}
