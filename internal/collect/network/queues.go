package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const softnetStat = "/proc/net/softnet_stat"

var softnetCollector = collector{
	id:   "network.softnet",
	owns: []string{"net.softnet"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		const id = "net.softnet"
		src := obs.File(softnetStat, "")
		src.Detail = "hexadecimal columns: processed, dropped, time_squeeze, then received_rps (10th), flow_limit_count (11th), backlog length and CPU number (12th and 13th, Linux 5.10 and later)"
		data, err := env.ReadFile(softnetStat)
		if err != nil {
			return []obs.Observation{unavailable(id, src, err, "")}
		}
		rows, indexed, err := parseSoftnet(data)
		if err != nil {
			return []obs.Observation{malformed(id, src, err)}
		}
		o := obs.OK(id, rows, src)
		if !indexed {
			o.Note = "this kernel does not print the CPU number; cpu is the row index, which differs from the CPU number when some CPUs are offline"
		}
		return []obs.Observation{o}
	},
}

// parseSoftnet reads /proc/net/softnet_stat: one line per online CPU of
// hexadecimal columns. Kernels before 5.10 print 11 columns and no CPU
// number; indexed reports whether the CPU number was printed.
func parseSoftnet(data []byte) (rows []map[string]any, indexed bool, err error) {
	rows = []map[string]any{}
	indexed = true
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 11 {
			return nil, false, fmt.Errorf("line %d: %d columns, want at least 11", i+1, len(f))
		}
		v := make([]int64, len(f))
		for j, s := range f {
			n, err := strconv.ParseUint(s, 16, 32)
			if err != nil {
				return nil, false, fmt.Errorf("line %d: %q is not 32-bit hex", i+1, clip(s))
			}
			v[j] = int64(n)
		}
		row := map[string]any{
			"cpu": int64(len(rows)), "processed": v[0], "dropped": v[1], "time_squeeze": v[2],
			"received_rps": v[9], "flow_limit_count": v[10],
		}
		if len(f) >= 13 {
			row["backlog_len"], row["cpu"] = v[11], v[12]
		} else {
			indexed = false
		}
		rows = append(rows, row)
	}
	return rows, indexed, nil
}

var tcArgv = []string{"tc", "-json", "-statistics", "qdisc", "show"}

var qdiscCollector = collector{
	id:   "network.tc",
	owns: []string{"net.qdiscs"},
	run: func(ctx context.Context, env collect.Env) []obs.Observation {
		const id = "net.qdiscs"
		out, o, ok := runJSON(ctx, env, id,
			"tc (iproute2) is not installed; net.core.default_qdisc still says what new interfaces get",
			"this tc has no JSON output (iproute2 before 4.16, or busybox)", tcArgv...)
		if !ok {
			return []obs.Observation{o}
		}
		src := obs.Command(tcArgv...)
		rows, err := parseTCJSON(out)
		if err != nil {
			return []obs.Observation{malformed(id, src, err)}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// tcQdisc is one qdisc in `tc -json -statistics qdisc show`.
type tcQdisc struct {
	Kind   string `json:"kind"`
	Handle string `json:"handle"`
	Parent string `json:"parent"`
	Dev    string `json:"dev"`
	Root   bool   `json:"root"`
	// Statistics; pointers so a missing counter stays missing.
	Bytes      *int64 `json:"bytes"`
	Packets    *int64 `json:"packets"`
	Drops      *int64 `json:"drops"`
	Overlimits *int64 `json:"overlimits"`
	Requeues   *int64 `json:"requeues"`
	Backlog    *int64 `json:"backlog"`
	Qlen       *int64 `json:"qlen"`
}

// parseTCJSON turns tc's JSON into one row per qdisc. Options, which differ
// by kind, are not kept.
func parseTCJSON(data []byte) ([]map[string]any, error) {
	var qs []tcQdisc
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&qs); err != nil {
		return nil, fmt.Errorf("tc output: %w", err)
	}
	rows := []map[string]any{}
	for _, q := range qs {
		if q.Kind == "" || q.Dev == "" {
			return nil, errors.New("tc output: a qdisc without kind or dev")
		}
		row := map[string]any{"interface": q.Dev, "kind": q.Kind, "root": q.Root}
		if q.Handle != "" {
			row["handle"] = q.Handle
		}
		if q.Parent != "" {
			row["parent"] = q.Parent
		}
		for col, p := range map[string]*int64{"bytes": q.Bytes, "packets": q.Packets, "drops": q.Drops,
			"overlimits": q.Overlimits, "requeues": q.Requeues, "backlog_bytes": q.Backlog, "qlen": q.Qlen} {
			if p != nil {
				if *p < 0 {
					return nil, fmt.Errorf("tc output: negative %s", col)
				}
				row[col] = *p
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

var conntrackCollector = collector{
	id:   "network.conntrack",
	owns: []string{"net.conntrack.count", "net.conntrack.max"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		const why = "connection tracking is not loaded: no firewall or NAT rule in this namespace needs it, or the kernel lacks nf_conntrack"
		return []obs.Observation{
			intFile(env, "net.conntrack.count", "/proc/sys/net/netfilter/nf_conntrack_count", why),
			intFile(env, "net.conntrack.max", "/proc/sys/net/netfilter/nf_conntrack_max", why),
		}
	},
}

// intFile is the observation for a file holding one integer.
func intFile(env collect.Env, id, path, absent string) obs.Observation {
	src := obs.File(path, "")
	s, err := readTrim(env, path)
	if err != nil {
		return unavailable(id, src, err, absent)
	}
	n, err := parseInt(s)
	if err != nil {
		return malformed(id, src, err)
	}
	return obs.OK(id, n, src)
}
