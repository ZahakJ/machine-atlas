package collect

import (
	"context"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collector reads one interface (or a few closely related ones) and returns
// observations for the ids it owns.
type Collector interface {
	// ID names the collector in the run report, e.g. "memory.meminfo".
	ID() string
	// Owns lists the ids this collector is responsible for. An entry ending
	// in ".*" owns every id with that prefix (open-ended families such as
	// "mem.meminfo.*"); any other entry is an exact id the collector must
	// always emit, with a non-ok status if it cannot read it.
	Owns() []string
	// Collect reads through env. It should return promptly when ctx is done;
	// the runner stops waiting at the deadline either way.
	Collect(ctx context.Context, env Env) []obs.Observation
}

// TimeoutCollector is a Collector that needs a different time limit from
// DefaultTimeout (the probe, which starts a child process).
type TimeoutCollector interface {
	Collector
	Timeout() time.Duration
}

// owns reports whether the ownership patterns cover id.
func owns(patterns []string, id string) bool {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(id, prefix) && len(id) > len(prefix) {
				return true
			}
		} else if p == id {
			return true
		}
	}
	return false
}

// exactIDs returns the patterns that name single ids.
func exactIDs(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if !strings.HasSuffix(p, "*") {
			out = append(out, p)
		}
	}
	return out
}

// Skip wraps c so that it reads nothing and reports every exact id it owns
// as not_collected with reason as the note. It is how a disabled collector
// (atlas collect --no-probe) still accounts for its ids.
func Skip(c Collector, reason string) Collector { return skipped{c, reason} }

type skipped struct {
	Collector
	reason string
}

func (s skipped) Collect(context.Context, Env) []obs.Observation {
	var out []obs.Observation
	for _, id := range exactIDs(s.Owns()) {
		out = append(out, obs.NotCollected(id, obs.Source{}, s.reason))
	}
	return out
}
