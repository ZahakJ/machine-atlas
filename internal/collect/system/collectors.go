package system

import (
	"context"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors returns the system collectors in the order they run: what the
// machine is (OS, kernel, CPU) before what runs on it and what it is
// attached to.
func Collectors() []collect.Collector {
	return []collect.Collector{
		osReleaseCollector,
		hostnameCollector,
		kernelCollector,
		cpuCollector,
		initCollector,
		virtCollector,
		blockCollector,
		lsblkCollector,
		mountsCollector,
		sessionCollector,
	}
}

// collector adapts a function to collect.Collector. Every system collector
// owns a fixed list of exact ids, so none needs more than this.
type collector struct {
	id   string
	owns []string
	run  func(ctx context.Context, env collect.Env) []obs.Observation
}

func (c collector) ID() string     { return c.id }
func (c collector) Owns() []string { return c.owns }
func (c collector) Collect(ctx context.Context, env collect.Env) []obs.Observation {
	return c.run(ctx, env)
}

// readTrim reads a small text file and trims surrounding whitespace, the
// shape of nearly every sysfs and procfs value file.
func readTrim(env collect.Env, path string) (string, error) {
	data, err := env.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// stringFile is the observation for a whole file read as one string.
// An empty file is failed: every file read this way always has content
// when the interface works.
func stringFile(env collect.Env, id, path string) obs.Observation {
	src := obs.File(path, "")
	s, err := readTrim(env, path)
	switch {
	case err != nil:
		return obs.Errored(id, src, err)
	case s == "":
		return obs.Failed(id, src, path+" is empty")
	}
	return obs.OK(id, s, src)
}

// existsObs is the observation for a marker path whose existence is the
// value (a container marker file, /run/systemd/system).
func existsObs(env collect.Env, id, path string) obs.Observation {
	src := obs.File(path, "")
	src.Detail = "existence checked; the contents are not read"
	ok, err := env.Stat(path)
	if err != nil {
		return obs.Errored(id, src, err)
	}
	return obs.OK(id, ok, src)
}
