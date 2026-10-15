package derive

import (
	"maps"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// server is a machine with two NICs, a multiqueue default route under mq
// with fq per queue, and every network input readable.
func server() map[string]any {
	return map[string]any{
		"kernel.page_size_bytes": 4096,
		"net.interfaces": []map[string]any{
			{"name": "lo", "type": "loopback", "mtu": 65536, "tx_queues": 1},
			{"name": "eno1", "type": "ethernet", "mtu": 9000, "speed_mbps": 25000, "tx_queues": 8},
			{"name": "eno2", "type": "ethernet", "mtu": 1500, "speed_mbps": 1000, "tx_queues": 1},
			{"name": "wlan0", "type": "wireless", "mtu": 1500, "tx_queues": 4},
		},
		"net.routes.ipv4": []map[string]any{
			{"interface": "eno2", "default": true, "metric": 200},
			{"interface": "eno1", "default": true, "metric": 100},
			{"interface": "eno1", "default": false, "metric": 0},
		},
		"net.routes.ipv6": []map[string]any{},
		"net.qdiscs": []map[string]any{
			{"interface": "eno1", "kind": "mq", "root": true},
			{"interface": "eno1", "kind": "fq", "parent": ":1", "root": false},
			{"interface": "eno1", "kind": "fq", "parent": ":2", "root": false},
			{"interface": "eno2", "kind": "fq_codel", "root": true},
		},
		"net.addresses.ipv4":                               []map[string]any{{"interface": "eno1", "class": "private"}},
		"net.addresses.ipv6":                               []map[string]any{{"interface": "eno1", "class": "global"}},
		"net.snmp.Tcp.RetransSegs":                         50,
		"net.snmp.Tcp.OutSegs":                             10000,
		"net.sysctl.ipv4.tcp_rmem.max":                     6291456,
		"net.sysctl.ipv4.tcp_available_congestion_control": "reno cubic bbr",
		"net.sockstat.tcp_mem":                             100,
		"net.sysctl.ipv4.tcp_mem.pressure":                 250000,
		"net.conntrack.count":                              1000,
		"net.conntrack.max":                                4000,
		"net.softnet": []map[string]any{
			{"cpu": 0, "dropped": 3, "time_squeeze": 10},
			{"cpu": 1, "dropped": 0, "time_squeeze": 5},
		},
		"net.sysctl.ipv4.ip_local_port_range.low":  32768,
		"net.sysctl.ipv4.ip_local_port_range.high": 60999,
	}
}

func TestNetFacts(t *testing.T) {
	facts := byID(All(snap(t, server())))
	want := map[string]any{
		"derived.net.default_route":                 true,
		"derived.net.default_route_interface":       "eno1",
		"derived.net.default_route_type":            "ethernet",
		"derived.net.default_route_mtu":             int64(9000),
		"derived.net.default_route_tx_queues":       int64(8),
		"derived.net.default_route_speed_mbps":      int64(25000),
		"derived.net.default_route_bdp_100ms_bytes": int64(312500000),
		"derived.net.default_route_qdisc":           "fq",
		"derived.net.global_address":                true,
		"derived.net.global_ipv6":                   true,
		"derived.net.physical_interfaces":           int64(3),
		"derived.net.interface_count":               int64(4),
		"derived.net.multiqueue":                    true,
		"derived.net.tcp.retrans_fraction":          0.005,
		"derived.net.tcp.rmem_max_mbps_at_100ms":    int64(503),
		"derived.net.tcp.rmem_covers_bdp":           false,
		"derived.net.tcp.bbr_available":             true,
		"derived.net.tcp.mem_used_bytes":            int64(409600),
		"derived.net.tcp.mem_pressure_bytes":        int64(1024000000),
		"derived.net.conntrack.fill_fraction":       0.25,
		"derived.net.softnet.dropped":               int64(3),
		"derived.net.softnet.time_squeeze":          int64(15),
		"derived.net.ephemeral_ports":               int64(28232),
	}
	for id, v := range want {
		f := facts[id]
		if f.Status != StatusOK || f.Value != v {
			t.Errorf("%s = %#v (%s: %s), want %#v", id, f.Value, f.Status, f.Reason, v)
		}
	}
	if r := facts["derived.net.default_route_qdisc"].Rule; !strings.Contains(r, "mq over 2 queues, each with fq") {
		t.Errorf("qdisc rule %q", r)
	}
}

func TestNetFactsBranches(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]any
		id     string
		want   any    // nil: unknown
		reason string // substring of the reason when unknown
	}{
		{"IPv6 default only", map[string]any{
			"net.routes.ipv4": []map[string]any{{"interface": "eno1", "default": false}},
			"net.routes.ipv6": []map[string]any{{"interface": "wlan0", "default": true, "metric": 1024}},
		}, "derived.net.default_route_type", "wireless", ""},
		{"wireless has no speed", map[string]any{
			"net.routes.ipv4": []map[string]any{{"interface": "wlan0", "default": true}},
		}, "derived.net.default_route_bdp_100ms_bytes", nil, "reports no link speed"},
		{"no default route", map[string]any{
			"net.routes.ipv4": []map[string]any{{"interface": "eno1", "default": false}},
		}, "derived.net.default_route", false, ""},
		{"no default route: interface", map[string]any{
			"net.routes.ipv4": []map[string]any{},
		}, "derived.net.default_route_interface", nil, "there is no default route"},
		{"no IPv6 kernel, no default", map[string]any{
			"net.routes.ipv4": []map[string]any{}, "net.routes.ipv6": missing(obs.StatusAbsent),
		}, "derived.net.default_route", false, ""},
		{"routes denied", map[string]any{
			"net.routes.ipv4": missing(obs.StatusPermissionDenied), "net.routes.ipv6": missing(obs.StatusAbsent),
		}, "derived.net.default_route", nil, "net.routes.ipv4 is permission denied"},
		{"IPv4 denied but IPv6 has one", map[string]any{
			"net.routes.ipv4": missing(obs.StatusPermissionDenied),
			"net.routes.ipv6": []map[string]any{{"interface": "eno2", "default": true}},
		}, "derived.net.default_route_interface", "eno2", ""},
		{"tc missing", map[string]any{"net.qdiscs": missing(obs.StatusAbsent)},
			"derived.net.default_route_qdisc", nil, "net.qdiscs is absent"},
		{"bond: the members' qdisc", map[string]any{
			"net.interfaces": []map[string]any{
				{"name": "bond0", "type": "bond"}, {"name": "ens1", "type": "ethernet", "master": "bond0"},
				{"name": "ens2", "type": "ethernet", "master": "bond0"},
			},
			"net.routes.ipv4": []map[string]any{{"interface": "bond0", "default": true}},
			"net.qdiscs": []map[string]any{
				{"interface": "bond0", "kind": "noqueue", "root": true},
				{"interface": "ens1", "kind": "mq", "root": true}, {"interface": "ens1", "kind": "fq", "parent": ":1"},
				{"interface": "ens2", "kind": "fq", "root": true},
			},
		}, "derived.net.default_route_qdisc", "fq", ""},
		{"noqueue without members", map[string]any{
			"net.routes.ipv4": []map[string]any{{"interface": "wlan0", "default": true}},
			"net.qdiscs":      []map[string]any{{"interface": "wlan0", "kind": "noqueue", "root": true}},
		}, "derived.net.default_route_qdisc", "noqueue", ""},
		{"single-queue root", map[string]any{
			"net.routes.ipv4": []map[string]any{{"interface": "eno2", "default": true}},
		}, "derived.net.default_route_qdisc", "fq_codel", ""},
		{"no IPv6 at all", map[string]any{"net.addresses.ipv6": missing(obs.StatusAbsent)},
			"derived.net.global_ipv6", false, ""},
		{"IPv4 addresses unsupported, IPv6 global", map[string]any{"net.addresses.ipv4": missing(obs.StatusUnsupported)},
			"derived.net.global_address", true, ""},
		{"IPv4 addresses unsupported, IPv6 not global", map[string]any{
			"net.addresses.ipv4": missing(obs.StatusUnsupported),
			"net.addresses.ipv6": []map[string]any{{"class": "link-local"}},
		}, "derived.net.global_address", nil, "net.addresses.ipv4 is unsupported"},
		{"no segments sent", map[string]any{"net.snmp.Tcp.OutSegs": 0}, "derived.net.tcp.retrans_fraction", nil, "no TCP segment"},
		{"conntrack not loaded", map[string]any{"net.conntrack.max": missing(obs.StatusAbsent)},
			"derived.net.conntrack.fill_fraction", nil, "net.conntrack.max is absent"},
		{"no bbr", map[string]any{"net.sysctl.ipv4.tcp_available_congestion_control": "reno cubic"},
			"derived.net.tcp.bbr_available", false, ""},
		{"big buffers", map[string]any{"net.sysctl.ipv4.tcp_rmem.max": 536870912},
			"derived.net.tcp.rmem_covers_bdp", true, ""},
		{"softnet denied", map[string]any{"net.softnet": missing(obs.StatusPermissionDenied)},
			"derived.net.softnet.dropped", nil, "permission denied"},
	}
	for _, tt := range tests {
		vals := server()
		maps.Copy(vals, tt.change)
		f := byID(All(snap(t, vals)))[tt.id]
		switch {
		case tt.want == nil && (f.Status != StatusUnknown || !strings.Contains(f.Reason, tt.reason)):
			t.Errorf("%s: %s = %+v, want unknown because %q", tt.name, tt.id, f, tt.reason)
		case tt.want != nil && (f.Status != StatusOK || f.Value != tt.want):
			t.Errorf("%s: %s = %#v (%s: %s), want %#v", tt.name, tt.id, f.Value, f.Status, f.Reason, tt.want)
		}
	}
}
