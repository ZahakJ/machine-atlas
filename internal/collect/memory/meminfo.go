package memory

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	meminfoPath = "/proc/meminfo"
	vmstatPath  = "/proc/vmstat"
	fromKB      = "converted from kB"
)

// meminfoField is one /proc/meminfo line. Value is in bytes when KB is set
// (the kernel's "kB" is KiB), otherwise the bare number printed (the
// HugePages_* page counts).
type meminfoField struct {
	Key   string
	Value int64
	KB    bool
}

// parseMeminfo parses /proc/meminfo. An empty file, a line that is not
// "Key: value", a value with another unit, or a repeated key is an error:
// each would mean the format changed under us.
func parseMeminfo(data []byte) ([]meminfoField, error) {
	fields, err := collect.ParseFields(data)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("no fields")
	}
	seen := map[string]bool{}
	out := make([]meminfoField, 0, len(fields))
	for _, f := range fields {
		if seen[f.Key] {
			return nil, fmt.Errorf("field %q appears twice", f.Key)
		}
		seen[f.Key] = true
		v, kb, err := collect.ParseKB(f.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Key, err)
		}
		if v < 0 {
			return nil, fmt.Errorf("%s: negative value %d", f.Key, v)
		}
		out = append(out, meminfoField{Key: f.Key, Value: v, KB: kb})
	}
	return out, nil
}

func meminfoCollector() collect.Collector {
	const prefix = "mem.meminfo."
	required := declared(prefix)
	return collector{
		id:   "memory.meminfo",
		owns: append([]string{prefix + "*"}, required...),
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(meminfoPath, "")
			data, err := env.ReadFile(meminfoPath)
			if err != nil {
				return unavailableAll(required, src, err, "")
			}
			fields, err := parseMeminfo(data)
			if err != nil {
				return malformedAll(required, src, err)
			}
			var out []obs.Observation
			seen := map[string]bool{}
			for _, f := range fields {
				id := prefix + obs.FieldID(f.Key)
				if seen[id] { // two raw names folding into one id
					continue
				}
				seen[id] = true
				o := obs.OK(id, f.Value, obs.File(meminfoPath, f.Key))
				o.Unit = obs.UnitCount
				if f.KB {
					o.Unit, o.Source.Detail = obs.UnitBytes, fromKB
				}
				out = append(out, o)
			}
			for _, id := range required {
				if !seen[id] {
					out = append(out, obs.Absent(id, src, "this kernel's /proc/meminfo has no such line; which fields exist depends on the kernel version and configuration"))
				}
			}
			return out
		},
	}
}

// maxVmstatKeys bounds /proc/vmstat. Kernels print about 200 keys today; far
// more would mean the file is not what we think it is.
const maxVmstatKeys = 400

// vmstatCounter is one /proc/vmstat line. The kernel prints unsigned longs,
// and a per-CPU counter that drifted below zero prints as a huge unsigned
// number, so values are kept as uint64 here and range-checked per key.
type vmstatCounter struct {
	Key   string
	Value uint64
}

// parseVmstat parses /proc/vmstat: "key value" lines.
func parseVmstat(data []byte) ([]vmstatCounter, error) {
	var out []vmstatCounter
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("line %d: expected \"key value\", got %q", i+1, clipText(line))
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: not a counter: %q", i+1, f[0], clipText(f[1]))
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("key %q appears twice", f[0])
		}
		seen[f[0]] = true
		out = append(out, vmstatCounter{f[0], v})
	}
	switch {
	case len(out) == 0:
		return nil, errors.New("no counters")
	case len(out) > maxVmstatKeys:
		return nil, fmt.Errorf("%d keys, more than the %d expected", len(out), maxVmstatKeys)
	}
	return out, nil
}

func vmstatCollector() collect.Collector {
	const prefix = "mem.vmstat."
	required := declared(prefix)
	return collector{
		id:   "memory.vmstat",
		owns: append([]string{prefix + "*"}, required...),
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(vmstatPath, "")
			data, err := env.ReadFile(vmstatPath)
			if err != nil {
				return unavailableAll(required, src, err, "")
			}
			counters, err := parseVmstat(data)
			if err != nil {
				return malformedAll(required, src, err)
			}
			var out []obs.Observation
			seen := map[string]bool{}
			for _, c := range counters {
				id := prefix + obs.FieldID(c.Key)
				if seen[id] {
					continue
				}
				seen[id] = true
				fsrc := obs.File(vmstatPath, c.Key)
				if c.Value > math.MaxInt64 {
					out = append(out, obs.Failed(id, fsrc, fmt.Sprintf("value %d is out of range; per-CPU counter drift can make a counter wrap below zero", c.Value)))
					continue
				}
				out = append(out, obs.OK(id, int64(c.Value), fsrc))
			}
			for _, id := range required {
				if !seen[id] {
					out = append(out, obs.Absent(id, src, "this kernel's /proc/vmstat has no such counter; counters are added and renamed between kernel versions"))
				}
			}
			return out
		},
	}
}
