package network

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

// sockstatField maps one "PROTO: key value" pair to an id.
type sockstatField struct{ proto, key, id string }

var sockstatFields = []sockstatField{
	{"sockets", "used", "net.sockstat.sockets_used"},
	{"TCP", "inuse", "net.sockstat.tcp_inuse"},
	{"TCP", "orphan", "net.sockstat.tcp_orphan"},
	{"TCP", "tw", "net.sockstat.tcp_tw"},
	{"TCP", "alloc", "net.sockstat.tcp_alloc"},
	{"TCP", "mem", "net.sockstat.tcp_mem"},
	{"UDP", "inuse", "net.sockstat.udp_inuse"},
	{"UDP", "mem", "net.sockstat.udp_mem"},
}

var sockstat6Fields = []sockstatField{
	{"TCP6", "inuse", "net.sockstat6.tcp6_inuse"},
	{"UDP6", "inuse", "net.sockstat6.udp6_inuse"},
}

func fieldIDs(fs []sockstatField) []string {
	ids := make([]string, len(fs))
	for i, f := range fs {
		ids[i] = f.id
	}
	return ids
}

var sockstatCollector = collector{
	id:   "network.sockstat",
	owns: append(fieldIDs(sockstatFields), fieldIDs(sockstat6Fields)...),
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		out := readSockstat(env, "/proc/net/sockstat", sockstatFields, "")
		return append(out, readSockstat(env, "/proc/net/sockstat6", sockstat6Fields,
			"no IPv6 here: the kernel was built without it or booted with ipv6.disable=1")...)
	},
}

func readSockstat(env collect.Env, path string, fields []sockstatField, absent string) []obs.Observation {
	data, err := env.ReadFile(path)
	if err != nil {
		out := make([]obs.Observation, len(fields))
		for i, f := range fields {
			out[i] = unavailable(f.id, obs.File(path, f.proto+" "+f.key), err, absent)
		}
		return out
	}
	vals, perr := parseSockstat(data)
	out := make([]obs.Observation, 0, len(fields))
	for _, f := range fields {
		src := obs.File(path, f.proto+" "+f.key)
		switch v, ok := vals[f.proto+" "+f.key]; {
		case perr != nil:
			out = append(out, malformed(f.id, src, perr))
		case !ok:
			out = append(out, obs.Absent(f.id, src, "this kernel does not print "+f.proto+" "+f.key))
		default:
			out = append(out, obs.OK(f.id, v, src))
		}
	}
	return out
}

// parseSockstat reads lines of the form "TCP: inuse 20 orphan 0 tw 3" into
// "TCP inuse" → 20.
func parseSockstat(data []byte) (map[string]int64, error) {
	out := map[string]int64{}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		proto, rest, ok := strings.Cut(line, ":")
		f := strings.Fields(rest)
		if !ok || len(f)%2 != 0 {
			return nil, fmt.Errorf("line %d: want \"PROTO: key value ...\", got %q", i+1, clip(line))
		}
		for j := 0; j < len(f); j += 2 {
			n, err := strconv.ParseInt(f[j+1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s is not an integer", i+1, f[j])
			}
			out[strings.TrimSpace(proto)+" "+f[j]] = n
		}
	}
	return out, nil
}

// tcpStates names the st column of /proc/net/tcp (include/net/tcp_states.h).
var tcpStates = []string{"", "ESTABLISHED", "SYN_SENT", "SYN_RECV", "FIN_WAIT1", "FIN_WAIT2", "TIME_WAIT",
	"CLOSE", "CLOSE_WAIT", "LAST_ACK", "LISTEN", "CLOSING", "NEW_SYN_RECV"}

var socketsCollector = collector{
	id:   "network.sockets",
	owns: []string{"net.sockets.tcp_states", "net.sockets.udp_ipv4", "net.sockets.udp_ipv6", "net.sockets.unix"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{
			tcpStatesObs(env),
			countObs(env, "net.sockets.udp_ipv4", "/proc/net/udp", ""),
			countObs(env, "net.sockets.udp_ipv6", "/proc/net/udp6", "no IPv6 here: the kernel was built without it or booted with ipv6.disable=1"),
			countObs(env, "net.sockets.unix", "/proc/net/unix", "the kernel has no Unix domain socket support"),
		}
	},
}

const tooManySockets = "more sockets than atlas reads (the file exceeds 1 MiB); sockstat still has the totals"

func tcpStatesObs(env collect.Env) obs.Observation {
	const id = "net.sockets.tcp_states"
	src := obs.File("/proc/net/tcp", "st")
	src.Detail = "and /proc/net/tcp6; only the state column is used, and only counts are kept"
	v4data, err := env.ReadFile("/proc/net/tcp")
	if err != nil {
		o := unavailable(id, src, err, "the kernel has no IPv4 TCP /proc interface here")
		if errors.Is(err, collect.ErrTooLarge) {
			o.Note = tooManySockets
		}
		return o
	}
	v4, err := countTCPStates(v4data)
	if err != nil {
		return malformed(id, src, err)
	}
	var v6 []int64
	var note string
	switch data, err := env.ReadFile("/proc/net/tcp6"); {
	case errors.Is(err, fs.ErrNotExist):
		note = "no /proc/net/tcp6: this kernel has no IPv6, so the ipv6 column is missing"
	case errors.Is(err, collect.ErrTooLarge):
		o := obs.Failed(id, src, tooManySockets)
		return o
	case err != nil:
		return obs.Errored(id, obs.File("/proc/net/tcp6", "st"), err)
	default:
		if v6, err = countTCPStates(data); err != nil {
			return malformed(id, obs.File("/proc/net/tcp6", "st"), err)
		}
	}
	rows := []map[string]any{}
	for st := 1; st < len(tcpStates); st++ {
		if st == 12 && v4[st] == 0 && (v6 == nil || v6[st] == 0) {
			continue // NEW_SYN_RECV is listed as SYN_RECV; keep the row only if a kernel prints it
		}
		row := map[string]any{"state": tcpStates[st], "ipv4": v4[st]}
		if v6 != nil {
			row["ipv6"] = v6[st]
		}
		rows = append(rows, row)
	}
	o := obs.OK(id, rows, src)
	o.Note = note
	return o
}

// countTCPStates counts the lines of /proc/net/tcp or tcp6 by their st
// column (hexadecimal). Nothing else on the line is looked at.
func countTCPStates(data []byte) ([]int64, error) {
	counts := make([]int64, len(tcpStates))
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "local_address") {
		return nil, errors.New("first line is not the sl/local_address header")
	}
	for i, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 4 {
			return nil, fmt.Errorf("line %d: too few fields", i+2)
		}
		st, err := strconv.ParseUint(f[3], 16, 8)
		if err != nil || st == 0 || int(st) >= len(tcpStates) {
			return nil, fmt.Errorf("line %d: %q is not a TCP state", i+2, clip(f[3]))
		}
		counts[st]++
	}
	return counts, nil
}

// countObs counts the sockets listed in a /proc/net table: the lines after
// the header.
func countObs(env collect.Env, id, path, absent string) obs.Observation {
	src := obs.File(path, "")
	src.Detail = "lines counted; nothing else is kept"
	data, err := env.ReadFile(path)
	if err != nil {
		o := unavailable(id, src, err, absent)
		if errors.Is(err, collect.ErrTooLarge) {
			o.Note = tooManySockets
		}
		return o
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return obs.Failed(id, src, path+" has no header line")
	}
	var n int64
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return obs.OK(id, n, src)
}
