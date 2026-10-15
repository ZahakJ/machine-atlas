package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// netSysctl is one file under /proc/sys/net. A file holding several
// integers (tcp_rmem: min, default, max) yields one id per value; text
// files are kept as strings.
type netSysctl struct {
	path string
	ids  []string // one id per integer, or one id for text
	text bool
}

const sysNetDir = "/proc/sys/net/"

var netSysctls = []netSysctl{
	{"core/default_qdisc", []string{"net.sysctl.core.default_qdisc"}, true},
	{"core/somaxconn", []string{"net.sysctl.core.somaxconn"}, false},
	{"core/netdev_max_backlog", []string{"net.sysctl.core.netdev_max_backlog"}, false},
	{"core/netdev_budget", []string{"net.sysctl.core.netdev_budget"}, false},
	{"core/rmem_default", []string{"net.sysctl.core.rmem_default"}, false},
	{"core/rmem_max", []string{"net.sysctl.core.rmem_max"}, false},
	{"core/wmem_default", []string{"net.sysctl.core.wmem_default"}, false},
	{"core/wmem_max", []string{"net.sysctl.core.wmem_max"}, false},
	{"ipv4/tcp_congestion_control", []string{"net.sysctl.ipv4.tcp_congestion_control"}, true},
	{"ipv4/tcp_available_congestion_control", []string{"net.sysctl.ipv4.tcp_available_congestion_control"}, true},
	{"ipv4/tcp_allowed_congestion_control", []string{"net.sysctl.ipv4.tcp_allowed_congestion_control"}, true},
	{"ipv4/tcp_rmem", []string{"net.sysctl.ipv4.tcp_rmem.min", "net.sysctl.ipv4.tcp_rmem.default", "net.sysctl.ipv4.tcp_rmem.max"}, false},
	{"ipv4/tcp_wmem", []string{"net.sysctl.ipv4.tcp_wmem.min", "net.sysctl.ipv4.tcp_wmem.default", "net.sysctl.ipv4.tcp_wmem.max"}, false},
	{"ipv4/tcp_mem", []string{"net.sysctl.ipv4.tcp_mem.low", "net.sysctl.ipv4.tcp_mem.pressure", "net.sysctl.ipv4.tcp_mem.high"}, false},
	{"ipv4/tcp_max_syn_backlog", []string{"net.sysctl.ipv4.tcp_max_syn_backlog"}, false},
	{"ipv4/tcp_syncookies", []string{"net.sysctl.ipv4.tcp_syncookies"}, false},
	{"ipv4/tcp_fin_timeout", []string{"net.sysctl.ipv4.tcp_fin_timeout"}, false},
	{"ipv4/tcp_tw_reuse", []string{"net.sysctl.ipv4.tcp_tw_reuse"}, false},
	{"ipv4/tcp_ecn", []string{"net.sysctl.ipv4.tcp_ecn"}, false},
	{"ipv4/tcp_slow_start_after_idle", []string{"net.sysctl.ipv4.tcp_slow_start_after_idle"}, false},
	{"ipv4/tcp_keepalive_time", []string{"net.sysctl.ipv4.tcp_keepalive_time"}, false},
	{"ipv4/ip_local_port_range", []string{"net.sysctl.ipv4.ip_local_port_range.low", "net.sysctl.ipv4.ip_local_port_range.high"}, false},
	{"ipv4/ip_forward", []string{"net.sysctl.ipv4.ip_forward"}, false},
	{"ipv6/conf/all/disable_ipv6", []string{"net.sysctl.ipv6.conf.all.disable_ipv6"}, false},
}

func sysctlCollector() collect.Collector {
	var ids []string
	for _, s := range netSysctls {
		ids = append(ids, s.ids...)
	}
	return collector{
		id:   "network.sysctl",
		owns: ids,
		run: func(_ context.Context, env collect.Env) []obs.Observation {
			var out []obs.Observation
			for _, s := range netSysctls {
				out = append(out, readNetSysctl(env, s)...)
			}
			return out
		},
	}
}

func readNetSysctl(env collect.Env, s netSysctl) []obs.Observation {
	path := sysNetDir + s.path
	src := func(i int) obs.Source {
		src := obs.File(path, "")
		if len(s.ids) > 1 {
			src.Field = fmt.Sprintf("value %d of %d", i+1, len(s.ids))
		}
		return src
	}
	all := func(f func(i int, id string) obs.Observation) []obs.Observation {
		out := make([]obs.Observation, len(s.ids))
		for i, id := range s.ids {
			out[i] = f(i, id)
		}
		return out
	}
	data, err := env.ReadFile(path)
	if err != nil {
		// Several exist only with a feature (default_qdisc since 3.12,
		// disable_ipv6 only with IPv6), and the machine-wide ones (the
		// net.core buffer and backlog limits, tcp_mem) only in the
		// initial network namespace, not in a container's own.
		return all(func(i int, id string) obs.Observation {
			return unavailable(id, src(i), err, "not provided here: machine-wide net sysctls exist only in the host's initial network namespace, and others depend on the kernel version and configuration")
		})
	}
	text := strings.TrimSpace(string(data))
	if s.text {
		if text == "" {
			return all(func(i int, id string) obs.Observation { return obs.Failed(id, src(i), path+" is empty") })
		}
		// The available and allowed lists are space-separated; a tab or
		// repeated space is normalised.
		return all(func(i int, id string) obs.Observation {
			return obs.OK(id, strings.Join(strings.Fields(text), " "), src(i))
		})
	}
	vals, err := parseInts(text, len(s.ids))
	if err != nil {
		return all(func(i int, id string) obs.Observation { return malformed(id, src(i), err) })
	}
	return all(func(i int, id string) obs.Observation { return obs.OK(id, vals[i], src(i)) })
}

// parseInts parses exactly n whitespace-separated decimal integers.
func parseInts(s string, n int) ([]int64, error) {
	f := strings.Fields(s)
	if len(f) != n {
		return nil, fmt.Errorf("want %d integers, got %q", n, clip(s))
	}
	out := make([]int64, n)
	for i, w := range f {
		v, err := strconv.ParseInt(w, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("want %d integers, got %q", n, clip(s))
		}
		out[i] = v
	}
	return out, nil
}
