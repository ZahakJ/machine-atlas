package network

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const rootsDir = "../../../fixtures/roots"

type want struct {
	status obs.Status
	value  any
}

func ok(v any) want { return want{obs.StatusOK, v} }

var (
	absent      = want{status: obs.StatusAbsent}
	denied      = want{status: obs.StatusPermissionDenied}
	unsupported = want{status: obs.StatusUnsupported}
	present     = want{status: obs.StatusOK}
)

// fixtureWants lists, per root, the network observations that make that
// root interesting. Every other observation only has to be not failed.
var fixtureWants = map[string]map[string]want{
	"desktop-x86_64": {
		"net.addresses.ipv4":                     present,
		"net.qdiscs":                             present,
		"net.sysctl.core.default_qdisc":          ok("fq_codel"),
		"net.sysctl.ipv4.tcp_congestion_control": ok("cubic"),
		"net.sysctl.ipv4.ip_forward":             ok(int64(1)),
		"net.conntrack.max":                      ok(int64(262144)),
		"net.sockstat.tcp_tw":                    ok(int64(23)),
		"net.sysctl.ipv6.conf.all.disable_ipv6":  ok(int64(0)),
		"net.sysctl.ipv4.tcp_rmem.max":           ok(int64(6291456)),
		"net.netstat.TcpExt.ListenOverflows":     ok(int64(0)),
	},
	"rpi-arm64-noswap": {
		"net.sysctl.core.default_qdisc": ok("pfifo_fast"),
		"net.conntrack.count":           absent,
		"net.conntrack.max":             absent,
		"net.qdiscs":                    present,
	},
	"alpine-container": {
		"net.interfaces": ok([]map[string]any{
			{"name": "eth0", "type": "virtual", "operstate": "up", "carrier": true, "mtu": int64(1500), "speed_mbps": int64(10000),
				"duplex": "full", "tx_queue_len": int64(1000), "rx_queues": int64(1), "tx_queues": int64(1),
				"mac": "02:42:ac:11:00:02", "mac_scope": "local", "mac_origin": "set",
				"rx_bytes": int64(0), "tx_bytes": int64(0), "rx_packets": int64(0), "tx_packets": int64(0), "rx_errors": int64(0),
				"tx_errors": int64(0), "rx_dropped": int64(0), "tx_dropped": int64(0)},
			{"name": "lo"},
		}),
		"net.addresses.ipv4":                    unsupported,
		"net.addresses.ipv6":                    ok([]map[string]any{}),
		"net.qdiscs":                            absent,
		"net.sysctl.core.default_qdisc":         absent,
		"net.sysctl.core.rmem_max":              absent,
		"net.sysctl.ipv4.tcp_mem.high":          absent,
		"net.sysctl.core.somaxconn":             ok(int64(4096)),
		"net.sysctl.ipv6.conf.all.disable_ipv6": ok(int64(1)),
	},
	"numa-server-la57": {
		"net.sysctl.core.default_qdisc":          ok("fq"),
		"net.sysctl.ipv4.tcp_congestion_control": ok("bbr"),
		"net.sysctl.core.somaxconn":              ok(int64(65535)),
		"net.netstat.TcpExt.ListenOverflows":     ok(int64(18342)),
		"net.conntrack.count":                    ok(int64(182340)),
	},
	"old-vm-cgroupv1": {
		"net.addresses.ipv4":            unsupported,
		"net.qdiscs":                    unsupported,
		"net.sysctl.core.somaxconn":     ok(int64(128)),
		"net.sysctl.core.default_qdisc": ok("pfifo_fast"),
		"net.softnet":                   present,
	},
	"restricted": {
		"net.interfaces":         denied,
		"net.addresses.ipv4":     denied,
		"net.qdiscs":             denied,
		"net.routes.ipv4":        denied,
		"net.routes.ipv6":        denied,
		"net.sockets.tcp_states": denied,
		"net.sockets.unix":       denied,
		"net.addresses.ipv6":     present,
		"net.sockstat.tcp_inuse": present,
		"net.snmp.Tcp.OutSegs":   present,
	},
}

func runFixture(t *testing.T, name string) map[string]obs.Observation {
	t.Helper()
	env, err := collect.OpenFixture(filepath.Join(rootsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	return run(t, env)
}

func TestFixtureRoots(t *testing.T) {
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n++
		t.Run(e.Name(), func(t *testing.T) {
			byID := runFixture(t, e.Name())
			for id, o := range byID {
				if o.Status == obs.StatusFailed {
					t.Errorf("%s failed: %s", id, o.Note)
				}
				if o.Status != obs.StatusOK && o.Note == "" {
					t.Errorf("%s is %s without a note", id, o.Status)
				}
			}
			for id, w := range fixtureWants[e.Name()] {
				o := byID[id]
				switch {
				case o.Status != w.status:
					t.Errorf("%s: status %s (%s), want %s", id, o.Status, o.Note, w.status)
				case id == "net.interfaces" && w.value != nil:
					// Only the first row is compared in full; the
					// others by name.
					got, want := o.Value.([]map[string]any), w.value.([]map[string]any)
					if len(got) != len(want) || !reflect.DeepEqual(withoutCounters(got[0]), withoutCounters(want[0])) || got[1]["name"] != want[1]["name"] {
						t.Errorf("%s = %v", id, got)
					}
				case w.value != nil && !reflect.DeepEqual(o.Value, w.value):
					t.Errorf("%s = %#v, want %#v", id, o.Value, w.value)
				}
			}
		})
	}
	if n != len(fixtureWants) {
		t.Errorf("found %d roots, want %d", n, len(fixtureWants))
	}
}

func withoutCounters(r map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range r {
		out[k] = v
	}
	for _, c := range counterColumns {
		delete(out, c)
	}
	return out
}

// TestFixtureShapes checks the network shapes the lessons rely on.
func TestFixtureShapes(t *testing.T) {
	numa := runFixture(t, "numa-server-la57")
	byName := map[string]map[string]any{}
	for _, r := range numa["net.interfaces"].Value.([]map[string]any) {
		byName[r["name"].(string)] = r
	}
	if _, ok := byName["bonding_masters"]; ok {
		t.Error("bonding_masters reported as an interface")
	}
	if b := byName["bond0"]; b["type"] != "bond" || b["speed_mbps"] != int64(50000) {
		t.Errorf("bond0 = %v", b)
	}
	if s := byName["ens1f0np0"]; s["master"] != "bond0" || s["tx_queues"] != int64(64) || s["driver"] != "mlx5_core" || s["bql_limit_bytes"] != int64(64*61740) {
		t.Errorf("bond slave = %v", s)
	}
	var fq int
	for _, q := range numa["net.qdiscs"].Value.([]map[string]any) {
		if q["interface"] == "ens1f0np0" && q["kind"] == "fq" {
			fq++
		}
	}
	if fq != 64 {
		t.Errorf("ens1f0np0 has %d fq qdiscs, want one per queue", fq)
	}

	desk := runFixture(t, "desktop-x86_64")
	routes := desk["net.routes.ipv4"].Value.([]map[string]any)
	if routes[0]["default"] != true || routes[0]["interface"] != "enp3s0" || routes[0]["gateway"] != "192.168.1.1" {
		t.Errorf("desktop routes %v", routes)
	}
	var temporary bool
	for _, a := range desk["net.addresses.ipv6"].Value.([]map[string]any) {
		if a["temporary"] == true && a["class"] == "global" {
			temporary = true
		}
	}
	if !temporary {
		t.Error("desktop has no temporary global IPv6 address")
	}
	for _, r := range desk["net.interfaces"].Value.([]map[string]any) {
		if r["name"] == "wlp4s0" && (r["carrier"] != nil || r["speed_mbps"] != nil || r["type"] != "wireless") {
			t.Errorf("Wi-Fi switched off: %v", r)
		}
		if r["name"] == "veth3f9c2a1" && r["master"] != "docker0" {
			t.Errorf("veth not enslaved to docker0: %v", r)
		}
	}

	old := runFixture(t, "old-vm-cgroupv1")
	if rows := old["net.softnet"].Value.([]map[string]any); len(rows) != 2 || rows[0]["backlog_len"] != nil {
		t.Errorf("4.19 softnet %v", rows)
	}
	for _, r := range old["net.interfaces"].Value.([]map[string]any) {
		if r["name"] == "eth0" && (r["speed_mbps"] != nil || r["driver"] != "virtio_net") {
			t.Errorf("virtio eth0 %v", r)
		}
	}
}
