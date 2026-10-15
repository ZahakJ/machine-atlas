// Package probe is the controlled child process: atlas re-executes itself as
// "atlas __probe", the child makes five known mappings, and the parent reads
// the child's /proc files to show how the kernel accounts for memory whose
// history is known exactly (ARCHITECTURE.md §4, "Probe"). Against a fixture root it
// reads the same data from _probe/.
//
// The child is the one place collection changes the machine: it allocates
// about 14 MiB for under a second and writes no files. It is disabled with
// --no-probe, which reports every probe id as not_collected.
package probe

import (
	"context"
	"os"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Verb is the hidden atlas verb that runs ChildMain.
const Verb = "__probe"

// layoutArg asks the child for the minimal second execution: print the
// address layout and exit.
const layoutArg = "layout"

// Collector returns the probe collector, which re-executes the running
// binary.
func Collector() collect.Collector {
	return probeCollector{command: func() ([]string, error) {
		exe, err := os.Executable()
		return []string{exe, Verb}, err
	}}
}

// probeCollector runs the probe. command returns the argv that starts the
// child; env is the child's entire environment. Tests replace both, because
// there the executable is the test binary, which becomes a child through
// TestMain and an environment variable instead of a verb.
type probeCollector struct {
	command func() ([]string, error)
	env     []string
}

func (probeCollector) ID() string     { return "probe" }
func (probeCollector) Owns() []string { return ids() }

// Timeout gives the probe the longer limit ARCHITECTURE.md §4 allows: it starts
// two processes.
func (probeCollector) Timeout() time.Duration { return 5 * time.Second }

// Collect runs the child on the live machine, or reads _probe/ in a fixture.
func (c probeCollector) Collect(ctx context.Context, env collect.Env) []obs.Observation {
	if !env.Live() {
		return fromFixture(env)
	}
	return c.live(ctx, env)
}

// fixtureDir is where a fixture root keeps what a probe child on that
// machine would have produced.
const fixtureDir = "/_probe/"

// fromFixture reads _probe/report.json, maps, smaps and status through env.
// A root without _probe/ models a machine where the probe did not run.
func fromFixture(env collect.Env) []obs.Observation {
	exists, err := env.Stat(fixtureDir)
	if err != nil || !exists {
		out := make([]obs.Observation, 0, len(ids()))
		for _, id := range ids() {
			if err != nil {
				out = append(out, obs.Errored(id, obs.Probe(fixtureDir), err))
			} else {
				out = append(out, obs.NotCollected(id, obs.Probe(fixtureDir), "this fixture has no probe data"))
			}
		}
		return out
	}
	read := func(name string) part {
		b, err := env.ReadFile(fixtureDir + name)
		return part{b, err}
	}
	in := inputs{maps: read("maps"), smaps: read("smaps"), status: read("status")}
	rep := read("report.json")
	in.reportErr = rep.err
	if rep.err == nil {
		in.report, in.reportErr = decodeReport(rep.data, true)
	}
	return observe(in)
}
