//go:build !linux

package probe

import (
	"context"
	"fmt"
	"os"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// live reports every probe id as unsupported: the probe's mappings and
// /proc files are Linux interfaces.
func (probeCollector) live(context.Context, collect.Env) []obs.Observation {
	out := make([]obs.Observation, 0, len(ids()))
	for _, id := range ids() {
		out = append(out, obs.Unsupported(id, obs.Probe("atlas "+Verb), "the probe uses Linux interfaces (mmap, memfd_create, /proc/<pid>/smaps, pagemap)"))
	}
	return out
}

// ChildMain is the entry point of "atlas __probe"; outside Linux there is
// no probe to run.
func ChildMain([]string) int {
	fmt.Fprintln(os.Stderr, "atlas __probe: the probe runs only on Linux")
	return 2
}
