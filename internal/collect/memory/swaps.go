package memory

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const swapsPath = "/proc/swaps"

// swapArea is one line of /proc/swaps, sizes in bytes. Name is the device's
// basename for a partition ("nvme0n1p3", "zram0") and "<file>" for a swap
// file: a file's path can name a user or a project, and the lessons only need
// to know that it is a file.
type swapArea struct {
	Name     string
	Type     string
	Size     int64
	Used     int64
	Priority int64
}

// parseSwaps parses /proc/swaps. A header with no areas is valid and yields
// an empty, non-nil slice: the kernel's way of saying "no swap is active".
// The kernel escapes whitespace in paths as \040, so every line has exactly
// five fields.
func parseSwaps(data []byte) ([]swapArea, error) {
	lines := strings.Split(string(data), "\n")
	if !strings.HasPrefix(lines[0], "Filename") {
		return nil, fmt.Errorf("expected a \"Filename Type Size Used Priority\" header, got %q", clipText(lines[0]))
	}
	out := []swapArea{}
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 5 {
			return nil, fmt.Errorf("line %d: expected 5 fields, got %d", i+2, len(f))
		}
		a := swapArea{Type: f[1]}
		switch a.Type {
		case "partition":
			a.Name = path.Base(f[0])
		case "file":
			a.Name = "<file>"
		default:
			return nil, fmt.Errorf("line %d: unknown swap type %q", i+2, clipText(f[1]))
		}
		var err error
		if a.Size, err = parseKiB(f[2]); err == nil {
			a.Used, err = parseKiB(f[3])
		}
		if err == nil {
			a.Priority, err = strconv.ParseInt(f[4], 10, 64)
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+2, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// parseKiB parses a KiB count as /proc/swaps prints sizes, in bytes.
func parseKiB(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a size in KiB: %q", clipText(s))
	}
	return kibToBytes(n)
}

func swapsCollector() collect.Collector {
	const id = "mem.swap.areas"
	return collector{
		id:   "memory.swaps",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(swapsPath, "")
			src.Detail = "sizes converted from kB; swap file paths are not kept"
			data, err := env.ReadFile(swapsPath)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no /proc/swaps: the kernel was built without swap support (CONFIG_SWAP)")}
			}
			areas, err := parseSwaps(data)
			if err != nil {
				return []obs.Observation{malformed(id, src, err)}
			}
			rows := make([]map[string]any, len(areas))
			for i, a := range areas {
				rows[i] = map[string]any{
					"name": a.Name, "type": a.Type, "size_bytes": a.Size,
					"used_bytes": a.Used, "priority": a.Priority,
				}
			}
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}
