package system

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var kernelCollector = collector{
	id: "system.kernel",
	owns: []string{
		"kernel.release", "kernel.version_string", "kernel.arch",
		"kernel.page_size_bytes", "kernel.boot_time", "kernel.uptime_seconds",
	},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		out := []obs.Observation{
			stringFile(env, "kernel.release", "/proc/sys/kernel/osrelease"),
			stringFile(env, "kernel.version_string", "/proc/version"),
		}

		src := obs.Syscall("uname")
		src.Field = "machine"
		if u, err := env.Uname(); err != nil {
			out = append(out, obs.Errored("kernel.arch", src, err))
		} else if u.Machine == "" {
			out = append(out, obs.Failed("kernel.arch", src, "uname returned an empty machine field"))
		} else {
			out = append(out, obs.OK("kernel.arch", u.Machine, src))
		}

		src = obs.Syscall("getpagesize")
		if ps, err := env.PageSize(); err != nil {
			out = append(out, obs.Errored("kernel.page_size_bytes", src, err))
		} else {
			out = append(out, obs.OK("kernel.page_size_bytes", ps, src))
		}

		src = obs.File("/proc/stat", "btime")
		src.Detail = "seconds since the Unix epoch, converted to RFC 3339 UTC"
		if btime, err := readParsed(env, "/proc/stat", parseBtime); err != nil {
			out = append(out, obs.Errored("kernel.boot_time", src, err))
		} else {
			out = append(out, obs.OK("kernel.boot_time", time.Unix(btime, 0).UTC().Format(time.RFC3339), src))
		}

		src = obs.File("/proc/uptime", "")
		src.Detail = "first field"
		if up, err := readParsed(env, "/proc/uptime", parseUptime); err != nil {
			out = append(out, obs.Errored("kernel.uptime_seconds", src, err))
		} else {
			out = append(out, obs.OK("kernel.uptime_seconds", up, src))
		}
		return out
	},
}

// readParsed reads path and applies parse, so read errors and parse errors
// reach obs.Errored the same way (the second maps to failed).
func readParsed[T any](env collect.Env, path string, parse func([]byte) (T, error)) (T, error) {
	data, err := env.ReadFile(path)
	if err != nil {
		var zero T
		return zero, err
	}
	return parse(data)
}

// parseBtime returns the "btime" line of /proc/stat: the boot time in
// seconds since the epoch.
func parseBtime(data []byte) (int64, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "btime" {
			continue
		}
		if len(f) != 2 {
			return 0, fmt.Errorf("malformed btime line %q", line)
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("malformed btime value %q", f[1])
		}
		return n, nil
	}
	return 0, fmt.Errorf("/proc/stat has no btime line")
}

// parseUptime returns the first field of /proc/uptime, seconds since boot
// (the second is aggregate idle time across all CPUs).
func parseUptime(data []byte) (float64, error) {
	f := strings.Fields(string(data))
	if len(f) != 2 {
		return 0, fmt.Errorf("expected two fields in /proc/uptime, got %q", strings.TrimSpace(string(data)))
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("malformed uptime %q", f[0])
	}
	return v, nil
}
