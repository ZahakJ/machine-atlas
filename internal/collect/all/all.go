// Package all is the explicit list of registered collectors. Registration is
// a list in code rather than init-time side effects, so the order collectors
// run in, and therefore the run report, is visible in one place.
package all

import (
	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/collect/memory"
	"github.com/ZahakJ/machine-atlas/internal/collect/network"
	"github.com/ZahakJ/machine-atlas/internal/collect/probe"
	"github.com/ZahakJ/machine-atlas/internal/collect/sched"
	"github.com/ZahakJ/machine-atlas/internal/collect/storage"
	"github.com/ZahakJ/machine-atlas/internal/collect/system"
)

// Options select which collectors run.
type Options struct {
	// NoProbe disables the probe child; its ids are reported as
	// not_collected instead.
	NoProbe bool
}

// All returns every collector in run order: system first (the lessons'
// context), then the CPU and scheduler, then memory, then storage, then network, then the
// probe last, so its child process runs after the cheap file reads are done.
func All(opts Options) []collect.Collector {
	var cs []collect.Collector
	cs = append(cs, system.Collectors()...)
	cs = append(cs, sched.Collectors()...)
	cs = append(cs, memory.Collectors()...)
	cs = append(cs, storage.Collectors()...)
	cs = append(cs, network.Collectors()...)
	p := probe.Collector()
	if opts.NoProbe {
		p = collect.Skip(p, "probe disabled with --no-probe")
	}
	return append(cs, p)
}
