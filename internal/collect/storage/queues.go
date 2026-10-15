package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var queuesCollector = collector{
	id:   "storage.queues",
	owns: []string{"block.queues"},
	read: func(env collect.Env) []obs.Observation {
		const id = "block.queues"
		src := obs.File(sysBlock, "")
		src.Detail = "*/queue/: scheduler, rotational, logical_block_size, physical_block_size, nr_requests, " +
			"max_sectors_kb and max_hw_sectors_kb (KiB, converted to bytes), write_cache, fua, discard_max_bytes, " +
			"read_ahead_kb (KiB, converted to bytes), wbt_lat_usec; hw_queues counts the entries of */mq/"
		names, err := devices(env)
		if err != nil {
			return []obs.Observation{tableError(id, src, err)}
		}
		rows := []map[string]any{}
		for _, name := range names {
			row, err := queueRow(env, name)
			if err != nil {
				return []obs.Observation{tableError(id, src, err)}
			}
			rows = append(rows, row)
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// queueAttr is one integer attribute of queue/: its file, its column, and
// the factor converting it to the column's unit.
type queueAttr struct {
	file, col string
	scale     int64
}

var queueInts = []queueAttr{
	{"logical_block_size", "logical_block_bytes", 1},
	{"physical_block_size", "physical_block_bytes", 1},
	{"nr_requests", "nr_requests", 1},
	{"max_sectors_kb", "max_request_bytes", 1024},
	{"max_hw_sectors_kb", "max_hw_request_bytes", 1024},
	{"discard_max_bytes", "discard_max_bytes", 1},
	{"read_ahead_kb", "read_ahead_bytes", 1024},
	{"wbt_lat_usec", "wbt_latency_us", 1},
}

// queueRow reads one device's queue directory. Every attribute is
// optional, because which exist depends on the kernel version and on
// whether the device has a request queue at all (a bio-based
// device-mapper volume has few); an attribute that exists but cannot be
// read or parsed fails the table.
func queueRow(env collect.Env, name string) (map[string]any, error) {
	dir := sysBlock + "/" + name + "/queue"
	row := map[string]any{"name": name}
	if s, ok, err := optional(env, dir, "scheduler"); err != nil {
		return nil, err
	} else if ok {
		sel, all, err := parseScheduler(s)
		if err != nil {
			return nil, attrError{"queue/scheduler", err}
		}
		row["scheduler"], row["schedulers"] = sel, all
	}
	if s, ok, err := optional(env, dir, "rotational"); err != nil {
		return nil, err
	} else if ok {
		b, err := parseBool01(s)
		if err != nil {
			return nil, attrError{"queue/rotational", err}
		}
		row["rotational"] = b
	}
	for _, a := range queueInts {
		s, ok, err := optional(env, dir, a.file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		n, err := parseCount(s)
		if err == nil && n > (1<<62)/a.scale {
			err = fmt.Errorf("%d is out of range", n)
		}
		if err != nil {
			return nil, attrError{"queue/" + a.file, err}
		}
		row[a.col] = n * a.scale
	}
	if s, ok, err := optional(env, dir, "write_cache"); err != nil {
		return nil, err
	} else if ok {
		if s != "write back" && s != "write through" {
			return nil, attrError{"queue/write_cache", fmt.Errorf("expected \"write back\" or \"write through\", got %q", clip(s))}
		}
		row["write_cache"] = s
	}
	if s, ok, err := optional(env, dir, "fua"); err != nil {
		return nil, err
	} else if ok {
		b, err := parseBool01(s)
		if err != nil {
			return nil, attrError{"queue/fua", err}
		}
		row["fua"] = b
	}
	// blk-mq lists one directory per hardware queue; a device without a
	// request queue of its own has no mq directory.
	entries, err := env.ReadDir(sysBlock + "/" + name + "/mq")
	switch {
	case err == nil:
		var n int64
		for _, e := range entries {
			if hwQueue.MatchString(e) {
				n++
			}
		}
		row["hw_queues"] = n
	case !errors.Is(err, fs.ErrNotExist):
		return nil, attrError{"mq", err}
	}
	return row, nil
}

var hwQueue = regexp.MustCompile(`^[0-9]+$`)

// parseScheduler parses queue/scheduler: the available schedulers with the
// selected one in brackets ("[none] mq-deadline kyber bfq"), or a single
// name without brackets on a queue with no choice (a bio-based device
// prints "none"). It returns the selected name and the space-separated
// list without brackets.
func parseScheduler(s string) (string, string, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", "", errors.New("empty scheduler list")
	}
	var sel string
	names := make([]string, len(f))
	for i, w := range f {
		name := w
		if strings.HasPrefix(w, "[") && strings.HasSuffix(w, "]") {
			name = w[1 : len(w)-1]
			if sel != "" {
				return "", "", fmt.Errorf("two selected schedulers in %q", clip(s))
			}
			sel = name
		}
		if !schedulerName.MatchString(name) {
			return "", "", fmt.Errorf("unexpected scheduler name %q", clip(w))
		}
		names[i] = name
	}
	if sel == "" {
		if len(f) > 1 {
			return "", "", fmt.Errorf("no selected scheduler in %q", clip(s))
		}
		sel = names[0]
	}
	return sel, strings.Join(names, " "), nil
}

var schedulerName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var bdiCollector = collector{
	id:   "storage.bdi",
	owns: []string{"block.bdi"},
	read: func(env collect.Env) []obs.Observation {
		const id = "block.bdi"
		src := obs.File(sysBlock, "")
		src.Detail = "*/bdi/: min_ratio, max_ratio, max_bytes (Linux 6.2 and later), strict_limit, stable_pages_required"
		names, err := devices(env)
		if err != nil {
			return []obs.Observation{tableError(id, src, err)}
		}
		rows := []map[string]any{}
		for _, name := range names {
			row, err := bdiRow(env, name)
			if err != nil {
				return []obs.Observation{tableError(id, src, err)}
			}
			if row != nil {
				rows = append(rows, row)
			}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// bdiRow reads one device's backing device info; a device without one
// (no bdi link) has no row.
func bdiRow(env collect.Env, name string) (map[string]any, error) {
	dir := sysBlock + "/" + name + "/bdi"
	if ok, err := env.Stat(dir); err != nil {
		return nil, attrError{"bdi", err}
	} else if !ok {
		return nil, nil
	}
	row := map[string]any{"name": name}
	for _, col := range []string{"min_ratio", "max_ratio", "max_bytes"} {
		s, ok, err := optional(env, dir, col)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		n, err := parseCount(s)
		if err == nil && col != "max_bytes" && n > 100 {
			err = fmt.Errorf("%d is not a percentage", n)
		}
		if err != nil {
			return nil, attrError{"bdi/" + col, err}
		}
		row[col] = n
	}
	for _, col := range []string{"strict_limit", "stable_pages_required"} {
		s, ok, err := optional(env, dir, col)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		b, err := parseBool01(s)
		if err != nil {
			return nil, attrError{"bdi/" + col, err}
		}
		row[col] = b
	}
	return row, nil
}
