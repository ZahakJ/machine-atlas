// Package storage collects what an unprivileged process can read about the
// machine's storage stack beyond the device list: each block device's
// request queue and I/O scheduler, its I/O counters, its writeback settings
// (the backing device info, BDI), I/O pressure, and the filesystem types the
// kernel knows (ARCHITECTURE.md §4, "Storage").
//
// Like every collector, these read only through collect.Env, own a fixed
// list of exact ids, and turn every failure into a status with a note.
// Device names are identifying under strict redaction, so they never appear
// in a source path or a note: a failure inside one device's directory is
// reported by attribute, not by device.
package storage

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

// Collectors returns the storage collectors in the order they run: how
// each device is set up, what it has done, and what waiting on it cost.
func Collectors() []collect.Collector {
	return []collect.Collector{
		queuesCollector,
		statsCollector,
		bdiCollector,
		psiCollector(),
		fsTypesCollector,
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

const sysBlock = "/sys/block"

// devices lists /sys/block the way block.devices does: unused loop and ram
// devices (size 0) are skipped, so every table names the same devices.
func devices(env collect.Env) ([]string, error) {
	names, err := env.ReadDir(sysBlock)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range names {
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			s, err := readTrim(env, sysBlock+"/"+name+"/size")
			if err != nil {
				return nil, attrError{"size", err}
			}
			if s == "0" {
				continue
			}
		}
		out = append(out, name)
	}
	return out, nil
}

// attrError is a failure to read one attribute of some device; its text
// names the attribute and the errno, never the device.
type attrError struct {
	attr string
	err  error
}

func (e attrError) Error() string {
	return "could not read a device's " + e.attr + ": " + errnoText(e.err)
}
func (e attrError) Unwrap() error { return e.err }

// tableError is the observation for a table that could not be built: the
// status the error implies, with a note that never names a device.
func tableError(id string, src obs.Source, err error) obs.Observation {
	o := obs.Errored(id, src, err)
	var ae attrError
	switch {
	case errors.As(err, &ae):
		if o.Status == obs.StatusOK {
			o.Status = obs.StatusFailed
		}
		o.Note = ae.Error()
	case o.Status == obs.StatusFailed:
		o.Note = "unexpected format: " + errnoText(err)
	}
	return o
}

// readTrim reads a small text file and trims surrounding whitespace.
func readTrim(env collect.Env, path string) (string, error) {
	data, err := env.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// optional reads an attribute some devices or kernels lack. A missing file
// returns ok false and no error; any other failure is an attrError.
func optional(env collect.Env, dir, attr string) (string, bool, error) {
	s, err := readTrim(env, dir+"/"+attr)
	switch {
	case err == nil:
		return s, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	}
	return "", false, attrError{attr, err}
}

// parseInt parses one decimal integer.
func parseInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("expected one integer, got " + strconv.Quote(clip(s)))
	}
	return n, nil
}

// parseCount parses a non-negative integer.
func parseCount(s string) (int64, error) {
	n, err := parseInt(s)
	if err == nil && n < 0 {
		err = fmt.Errorf("expected a non-negative integer, got %d", n)
	}
	return n, err
}

// parseBool01 parses a sysfs flag holding 0 or 1.
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

// errnoText describes err without the path it carries, which would name
// the device.
func errnoText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}
