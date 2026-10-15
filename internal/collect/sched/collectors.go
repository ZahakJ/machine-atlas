// Package sched holds the collectors for the CPU and scheduler track: the
// per-CPU topology and cache sharing, frequency scaling, load and CPU time,
// run-queue statistics, CPU pressure, scheduler settings, atlas's own
// scheduling state and the cpu controller of its cgroup (ARCHITECTURE.md §4,
// "CPU and scheduler").
//
// Like every collector, these read only through collect.Env, own a fixed
// list of exact ids, and turn every failure into a status with a note.
// Parsers are pure functions of the bytes read and reject what they do not
// understand. Nothing here reads another process: the only per-process
// files are atlas's own /proc/self entries.
package sched

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors returns the scheduler collectors in the order they run: what
// the CPUs are, how fast they run, how busy they are, how the scheduler is
// set up, and finally atlas's own view.
func Collectors() []collect.Collector {
	return []collect.Collector{
		topologyCollector(),
		cpufreqCollector(),
		loadCollector(),
		schedstatCollector(),
		psiCollector(),
		settingsCollector(),
		selfCollector(),
		cgroupCollector(),
	}
}

// collector adapts a function to collect.Collector. These interfaces are
// small files read in microseconds, so read ignores the context.
type collector struct {
	id   string
	owns []string
	read func(env collect.Env) []obs.Observation
}

func (c collector) ID() string     { return c.id }
func (c collector) Owns() []string { return c.owns }
func (c collector) Collect(_ context.Context, env collect.Env) []obs.Observation {
	return c.read(env)
}

// readTrim reads a small text file and trims surrounding whitespace.
func readTrim(env collect.Env, path string) (string, error) {
	data, err := env.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// unavailable is the observation for id when reading src failed; for a
// missing file, absentNote (when set) says what the absence means, ahead of
// the error text.
func unavailable(id string, src obs.Source, err error, absentNote string) obs.Observation {
	o := obs.Errored(id, src, err)
	if o.Status == obs.StatusAbsent && absentNote != "" {
		o.Note = absentNote + " (" + o.Note + ")"
	}
	return o
}

func unavailableAll(ids []string, src obs.Source, err error, absentNote string) []obs.Observation {
	out := make([]obs.Observation, len(ids))
	for i, id := range ids {
		out[i] = unavailable(id, src, err, absentNote)
	}
	return out
}

// malformed is the observation for content that did not parse.
func malformed(id string, src obs.Source, err error) obs.Observation {
	return obs.Failed(id, src, "unexpected format: "+err.Error())
}

func malformedAll(ids []string, src obs.Source, err error) []obs.Observation {
	out := make([]obs.Observation, len(ids))
	for i, id := range ids {
		out[i] = malformed(id, src, err)
	}
	return out
}

// parseInt parses a file holding one decimal integer.
func parseInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("expected one integer, got " + strconv.Quote(clip(s)))
	}
	return n, nil
}

// parseBool01 parses a sysfs flag file holding 0 or 1.
func parseBool01(s string) (bool, error) {
	switch strings.TrimSpace(s) {
	case "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, fmt.Errorf("expected 0 or 1, got %q", clip(strings.TrimSpace(s)))
}

// clip shortens s for an error message so a garbage file cannot produce an
// enormous note.
func clip(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + "…"
}

// errnoText describes err without the path it carries, for notes about
// cgroup files whose path could identify a user.
func errnoText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}

// cpuListFile reads a file in the kernel's CPU list format. An empty file
// is a valid empty list.
func cpuListFile(env collect.Env, id, path, absentNote string) obs.Observation {
	src := obs.File(path, "")
	s, err := readTrim(env, path)
	if err != nil {
		return unavailable(id, src, err, absentNote)
	}
	if _, err := collect.ParseCPUList(s); err != nil {
		return malformed(id, src, err)
	}
	return obs.OK(id, s, src)
}
