package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// declared returns the exact catalog ids under prefix: the counters the
// lessons use, which a collector must account for even when the kernel does
// not print them.
func declared(prefix string) []string {
	var ids []string
	for _, e := range catalog.All() {
		if strings.HasPrefix(e.ID, prefix) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

var snmpCollector = collector{
	id:   "network.snmp",
	owns: append(declared("net.snmp."), declared("net.netstat.")...),
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		out := readCounters(env, "/proc/net/snmp", "net.snmp.")
		return append(out, readCounters(env, "/proc/net/netstat", "net.netstat.")...)
	},
}

// readCounters emits every declared id under prefix from one counter file;
// an id is net.<file>.<Group>.<Name>.
func readCounters(env collect.Env, path, prefix string) []obs.Observation {
	ids := declared(prefix)
	data, err := env.ReadFile(path)
	if err != nil {
		out := make([]obs.Observation, len(ids))
		for i, id := range ids {
			out[i] = unavailable(id, obs.File(path, counterField(prefix, id)), err, "")
		}
		return out
	}
	vals, perr := parseCounters(data)
	out := make([]obs.Observation, 0, len(ids))
	for _, id := range ids {
		field := counterField(prefix, id)
		src := obs.File(path, field)
		switch v, ok := vals[field]; {
		case perr != nil:
			out = append(out, malformed(id, src, perr))
		case !ok:
			out = append(out, obs.Absent(id, src, "this kernel does not count "+field))
		default:
			out = append(out, obs.OK(id, v, src))
		}
	}
	return out
}

// counterField turns net.snmp.Tcp.ActiveOpens into "Tcp: ActiveOpens".
func counterField(prefix, id string) string {
	group, name, _ := strings.Cut(strings.TrimPrefix(id, prefix), ".")
	return group + ": " + name
}

// parseCounters reads the paired-line format of /proc/net/snmp and
// /proc/net/netstat: a line of names, then a line of values, both starting
// with the same "Group:" prefix. Keys are "Group: Name".
func parseCounters(data []byte) (map[string]int64, error) {
	out := map[string]int64{}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines)%2 != 0 {
		return nil, fmt.Errorf("%d lines; names and values come in pairs", len(lines))
	}
	for i := 0; i < len(lines); i += 2 {
		g1, names, ok1 := strings.Cut(lines[i], ":")
		g2, vals, ok2 := strings.Cut(lines[i+1], ":")
		n, v := strings.Fields(names), strings.Fields(vals)
		if !ok1 || !ok2 || g1 != g2 || len(n) != len(v) {
			return nil, fmt.Errorf("lines %d and %d do not pair up as %q names and values", i+1, i+2, clip(g1))
		}
		for j := range n {
			x, err := strconv.ParseInt(v[j], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %q is not an integer", g1, n[j], clip(v[j]))
			}
			out[g1+": "+n[j]] = x
		}
	}
	return out, nil
}
