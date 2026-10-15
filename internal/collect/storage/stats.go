package storage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var statsCollector = collector{
	id:   "storage.stats",
	owns: []string{"block.stats"},
	read: func(env collect.Env) []obs.Observation {
		const id = "block.stats"
		src := obs.File(sysBlock, "")
		src.Detail = "*/stat (Documentation/block/stat.rst): 11 fields, 15 since Linux 4.18 (discards), 17 since 5.5 (flushes); sectors are 512 bytes, converted to bytes"
		names, err := devices(env)
		if err != nil {
			return []obs.Observation{tableError(id, src, err)}
		}
		rows := []map[string]any{}
		for _, name := range names {
			s, ok, err := optional(env, sysBlock+"/"+name, "stat")
			if err != nil {
				return []obs.Observation{tableError(id, src, err)}
			}
			if !ok {
				continue
			}
			row, err := parseDiskStat(s)
			if err != nil {
				return []obs.Observation{tableError(id, src, attrError{"stat", err})}
			}
			row["name"] = name
			rows = append(rows, row)
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// statFields names the fields of a block device's stat file in order
// (Documentation/block/stat.rst). Sector counts become bytes.
var statFields = []string{
	"reads", "reads_merged", "read_bytes", "read_ms",
	"writes", "writes_merged", "write_bytes", "write_ms",
	"in_flight", "io_ms", "weighted_ms",
	"discards", "discards_merged", "discard_bytes", "discard_ms",
	"flushes", "flush_ms",
}

// parseDiskStat parses /sys/block/<dev>/stat: 11, 15 or 17 unsigned
// decimal fields depending on the kernel version. Fields a newer kernel
// adds beyond these are ignored.
func parseDiskStat(s string) (map[string]any, error) {
	f := strings.Fields(s)
	switch {
	case len(f) == 11, len(f) == 15, len(f) >= 17:
	default:
		return nil, fmt.Errorf("expected 11, 15 or 17 fields, got %d", len(f))
	}
	row := map[string]any{}
	for i, col := range statFields {
		if i >= len(f) {
			break
		}
		n, err := parseCount(f[i])
		if err != nil {
			return nil, errors.New(col + ": " + err.Error())
		}
		if strings.HasSuffix(col, "_bytes") {
			if n > (1<<63-1)/512 {
				return nil, fmt.Errorf("%s: %d sectors is out of range", col, n)
			}
			n *= 512
		}
		row[col] = n
	}
	return row, nil
}
