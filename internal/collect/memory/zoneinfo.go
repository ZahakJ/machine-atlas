package memory

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	zoneinfoPath  = "/proc/zoneinfo"
	buddyinfoPath = "/proc/buddyinfo"
)

// zone is one zone of /proc/zoneinfo, all counts in pages.
type zone struct {
	Node                      int64
	Name                      string
	Free, Min, Low, High      int64
	Spanned, Present, Managed int64
}

// zoneFields are the per-zone lines the table keeps, in the order the
// kernel prints them. Lines in between ("boost", "promo", "cma", "scanned"
// on old kernels) and the per-node and per-CPU sections are skipped.
var zoneFields = []string{"free", "min", "low", "high", "spanned", "present", "managed"}

// parseZoneHeader parses "Node 0, zone   Normal", the line that starts a zone
// in both zoneinfo and buddyinfo, and returns the node, the zone name and
// the remaining fields of the line.
func parseZoneHeader(line string) (node int64, name string, rest []string, ok bool) {
	f := strings.Fields(line)
	if len(f) < 4 || f[0] != "Node" || f[2] != "zone" {
		return 0, "", nil, false
	}
	num, comma := strings.CutSuffix(f[1], ",")
	n, err := strconv.ParseInt(num, 10, 64)
	if !comma || err != nil || n < 0 {
		return 0, "", nil, false
	}
	return n, f[3], f[4:], true
}

// parseZoneinfo parses /proc/zoneinfo. Within each zone only the first
// occurrence of each wanted line counts: the per-CPU pageset section has
// "high:" lines of its own, which the exact key match already excludes, and
// taking the first occurrence keeps a future duplicate from overwriting the
// watermark.
func parseZoneinfo(data []byte) ([]zone, error) {
	var zones []zone
	var have map[string]bool
	finish := func() error {
		if len(zones) == 0 {
			return nil
		}
		z := zones[len(zones)-1]
		for _, k := range zoneFields {
			if !have[k] {
				return fmt.Errorf("node %d zone %s: no %q line", z.Node, z.Name, k)
			}
		}
		return nil
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if node, name, rest, ok := parseZoneHeader(line); ok && len(rest) == 0 {
			if err := finish(); err != nil {
				return nil, err
			}
			zones = append(zones, zone{Node: node, Name: name})
			have = map[string]bool{}
			continue
		}
		if len(zones) == 0 {
			return nil, fmt.Errorf("line %d: %q before the first zone header", i+1, clipText(line))
		}
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "pages" && f[1] == "free" {
			f = f[1:]
		}
		if len(f) != 2 {
			continue
		}
		z := &zones[len(zones)-1]
		var dst *int64
		switch f[0] {
		case "free":
			dst = &z.Free
		case "min":
			dst = &z.Min
		case "low":
			dst = &z.Low
		case "high":
			dst = &z.High
		case "spanned":
			dst = &z.Spanned
		case "present":
			dst = &z.Present
		case "managed":
			dst = &z.Managed
		default:
			continue
		}
		if have[f[0]] {
			continue
		}
		v, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("line %d: node %d zone %s: %s: not a page count: %q", i+1, z.Node, z.Name, f[0], clipText(f[1]))
		}
		*dst, have[f[0]] = v, true
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(zones) == 0 {
		return nil, errors.New("no zones")
	}
	return zones, nil
}

func zoneinfoCollector() collect.Collector {
	const id = "mem.zones"
	return collector{
		id:   "memory.zoneinfo",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(zoneinfoPath, "")
			data, err := env.ReadFile(zoneinfoPath)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "")}
			}
			zones, err := parseZoneinfo(data)
			if err != nil {
				return []obs.Observation{malformed(id, src, err)}
			}
			rows := make([]map[string]any, len(zones))
			for i, z := range zones {
				rows[i] = map[string]any{
					"node": z.Node, "zone": z.Name, "free": z.Free, "min": z.Min, "low": z.Low,
					"high": z.High, "spanned": z.Spanned, "present": z.Present, "managed": z.Managed,
				}
			}
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}

// buddyZone is one line of /proc/buddyinfo: the number of free blocks of
// 2^order pages for each order, from 0 up.
type buddyZone struct {
	Node   int64
	Name   string
	Orders []int64
}

// parseBuddyinfo parses /proc/buddyinfo. Every line must have the same
// number of orders, which is fixed per kernel build (MAX_ORDER).
func parseBuddyinfo(data []byte) ([]buddyZone, error) {
	var out []buddyZone
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		node, name, rest, ok := parseZoneHeader(line)
		if !ok || len(rest) == 0 {
			return nil, fmt.Errorf("line %d: expected \"Node N, zone NAME counts…\", got %q", i+1, clipText(line))
		}
		if len(out) > 0 && len(rest) != len(out[0].Orders) {
			return nil, fmt.Errorf("line %d: %d orders, earlier lines have %d", i+1, len(rest), len(out[0].Orders))
		}
		z := buddyZone{Node: node, Name: name, Orders: make([]int64, len(rest))}
		for j, s := range rest {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("line %d: order %d: not a count: %q", i+1, j, clipText(s))
			}
			z.Orders[j] = n
		}
		out = append(out, z)
	}
	if len(out) == 0 {
		return nil, errors.New("no zones")
	}
	return out, nil
}

func buddyinfoCollector() collect.Collector {
	const id = "mem.buddyinfo"
	return collector{
		id:   "memory.buddyinfo",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(buddyinfoPath, "")
			data, err := env.ReadFile(buddyinfoPath)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "")}
			}
			zones, err := parseBuddyinfo(data)
			if err != nil {
				return []obs.Observation{malformed(id, src, err)}
			}
			rows := make([]map[string]any, len(zones))
			for i, z := range zones {
				row := map[string]any{"node": z.Node, "zone": z.Name}
				for order, n := range z.Orders {
					row["order"+strconv.Itoa(order)] = n
				}
				rows[i] = row
			}
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}
