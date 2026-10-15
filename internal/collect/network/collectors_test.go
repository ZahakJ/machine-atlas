package network

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// machine builds a fixture root from machine paths and their contents,
// canned commands (argv → output file content, or a map for _meta.json),
// and paths that must fail with EACCES.
func machine(t *testing.T, files map[string]string, commands map[string]any, deny ...string) *collect.FixtureEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test-root")
	cmds := map[string]any{}
	files = maps.Clone(files)
	i := 0
	for argv, c := range commands {
		if out, ok := c.(string); ok {
			i++
			name := "_cmd/out" + string(rune('a'+i))
			files["/"+name] = out
			cmds[argv] = map[string]string{"stdout": name}
		} else {
			cmds[argv] = c
		}
	}
	meta, _ := json.Marshal(map[string]any{
		"description": "test", "now": "2026-01-15T12:00:00Z", "page_size": 4096,
		"uname": map[string]string{"machine": "x86_64", "release": "6.12.0"}, "deny": deny, "commands": cmds,
	})
	files["/_meta.json"] = string(meta)
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(content, "->") {
			if err := os.Symlink(strings.TrimPrefix(content, "->"), full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, err := collect.OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.Close() })
	return env
}

func run(t *testing.T, env collect.Env) map[string]obs.Observation {
	t.Helper()
	rep, out := collect.Run(context.Background(), env, Collectors())
	for _, r := range rep.Collectors {
		if r.Error != "" {
			t.Errorf("collector %s: %s", r.ID, r.Error)
		}
	}
	byID := map[string]obs.Observation{}
	for _, o := range out {
		byID[o.ID] = o
	}
	return byID
}

const (
	ipArgv  = "ip -json -4 address show"
	tcArgvS = "tc -json -statistics qdisc show"
)

func modernFiles() map[string]string {
	f := map[string]string{
		"/sys/class/net/lo/type":                                  "772\n",
		"/sys/class/net/lo/operstate":                             "unknown\n",
		"/sys/class/net/lo/mtu":                                   "65536\n",
		"/sys/class/net/lo/address":                               "00:00:00:00:00:00\n",
		"/sys/class/net/eth0/type":                                "1\n",
		"/sys/class/net/eth0/operstate":                           "up\n",
		"/sys/class/net/eth0/carrier":                             "1\n",
		"/sys/class/net/eth0/mtu":                                 "1500\n",
		"/sys/class/net/eth0/speed":                               "1000\n",
		"/sys/class/net/eth0/duplex":                              "full\n",
		"/sys/class/net/eth0/tx_queue_len":                        "1000\n",
		"/sys/class/net/eth0/address":                             "02:00:00:aa:bb:01\n",
		"/sys/class/net/eth0/addr_assign_type":                    "0\n",
		"/sys/class/net/eth0/device/vendor":                       "0x8086\n",
		"/sys/class/net/eth0/device/driver":                       "->../../../bus/pci/drivers/e1000e",
		"/sys/class/net/eth0/queues/rx-0/rps_cpus":                "0\n",
		"/sys/class/net/eth0/queues/tx-0/byte_queue_limits/limit": "3028\n",
		"/sys/class/net/eth0/queues/tx-1/byte_queue_limits/limit": "1514\n",
		"/sys/class/net/eth0/statistics/rx_bytes":                 "123456\n",
		"/sys/class/net/eth0/statistics/tx_dropped":               "0\n",
		"/sys/class/net/bonding_masters":                          "\n",
		"/proc/net/if_inet6":                                      "00000000000000000000000000000001 01 80 10 80       lo\n",
		"/proc/net/route":                                         "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\neth0\t00000000\t0102000A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
		"/proc/net/ipv6_route":                                    "",
		"/proc/net/sockstat":                                      "sockets: used 50\nTCP: inuse 3 orphan 0 tw 1 alloc 4 mem 1\nUDP: inuse 2 mem 0\n",
		"/proc/net/sockstat6":                                     "TCP6: inuse 1\nUDP6: inuse 1\n",
		"/proc/net/tcp":                                           "  sl  local_address rem_address   st\n   0: 00000000:0016 00000000:0000 0A 0\n",
		"/proc/net/tcp6":                                          "  sl  local_address rem_address   st\n",
		"/proc/net/udp":                                           "  sl  local_address rem_address   st\n   0: x y 07\n",
		"/proc/net/udp6":                                          "  sl  local_address rem_address   st\n",
		"/proc/net/unix":                                          "Num       RefCount Protocol Flags    Type St Inode Path\n0: 2 0 10000 0001 01 1 /run/x\n",
		"/proc/net/softnet_stat":                                  "00000100 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000\n",
		"/proc/sys/net/netfilter/nf_conntrack_count":              "12\n",
		"/proc/sys/net/netfilter/nf_conntrack_max":                "65536\n",
		"/proc/sys/net/core/default_qdisc":                        "fq_codel\n",
		"/proc/sys/net/ipv4/tcp_available_congestion_control":     "reno cubic\tbbr\n",
		"/proc/sys/net/ipv4/tcp_allowed_congestion_control":       "reno cubic\n",
		"/proc/sys/net/ipv4/tcp_congestion_control":               "cubic\n",
		"/proc/sys/net/ipv4/tcp_rmem":                             "4096\t131072\t6291456\n",
		"/proc/sys/net/ipv4/tcp_wmem":                             "4096\t16384\t4194304\n",
		"/proc/sys/net/ipv4/tcp_mem":                              "188049\t250733\t376098\n",
		"/proc/sys/net/ipv4/ip_local_port_range":                  "32768\t60999\n",
	}
	snmp, netstat := "", ""
	var names, vals []string
	group := ""
	flush := func(dst *string) {
		if group != "" {
			*dst += group + ": " + strings.Join(names, " ") + "\n" + group + ": " + strings.Join(vals, " ") + "\n"
		}
		names, vals = nil, nil
	}
	for _, prefix := range []string{"net.snmp.", "net.netstat."} {
		dst := &snmp
		if prefix == "net.netstat." {
			dst = &netstat
		}
		group = ""
		for _, id := range declared(prefix) {
			g, n, _ := strings.Cut(strings.TrimPrefix(id, prefix), ".")
			if g != group {
				flush(dst)
				group = g
			}
			names, vals = append(names, n), append(vals, "7")
		}
		flush(dst)
	}
	f["/proc/net/snmp"], f["/proc/net/netstat"] = snmp, netstat
	for _, s := range netSysctls {
		if _, ok := f[sysNetDir+s.path]; !ok {
			f[sysNetDir+s.path] = "1\n"
		}
	}
	return f
}

func modernCommands() map[string]any {
	return map[string]any{
		ipArgv:  `[{"ifindex":2,"ifname":"eth0","addr_info":[{"family":"inet","local":"10.0.2.15","prefixlen":24,"scope":"global","dynamic":true}]}]`,
		tcArgvS: `[{"kind":"fq_codel","handle":"0:","dev":"eth0","root":true,"bytes":1,"packets":1,"drops":0,"overlimits":0,"requeues":0,"backlog":0,"qlen":0}]`,
	}
}

func TestModernMachine(t *testing.T) {
	got := run(t, machine(t, modernFiles(), modernCommands()))
	for id, o := range got {
		if o.Status != obs.StatusOK {
			t.Errorf("%s: %s (%s), want ok", id, o.Status, o.Note)
		}
	}
	ifs := got["net.interfaces"].Value.([]map[string]any)
	if len(ifs) != 2 {
		t.Fatalf("interfaces %v; bonding_masters must not be one", ifs)
	}
	eth := ifs[0]
	want := map[string]any{
		"name": "eth0", "type": "ethernet", "operstate": "up", "carrier": true, "mtu": int64(1500), "speed_mbps": int64(1000),
		"duplex": "full", "tx_queue_len": int64(1000), "rx_queues": int64(1), "tx_queues": int64(2), "bql_limit_bytes": int64(4542),
		"driver": "e1000e", "mac": "02:00:00:aa:bb:01", "mac_scope": "local", "mac_origin": "permanent",
		"rx_bytes": int64(123456), "tx_dropped": int64(0),
	}
	if !reflect.DeepEqual(eth, want) {
		t.Errorf("eth0 = %v", eth)
	}
	if lo := ifs[1]; lo["type"] != "loopback" || lo["mac_scope"] != "none" || lo["speed_mbps"] != nil {
		t.Errorf("lo = %v", lo)
	}
	vals := map[string]any{
		"net.sysctl.ipv4.tcp_rmem.max":                     int64(6291456),
		"net.sysctl.ipv4.tcp_mem.pressure":                 int64(250733),
		"net.sysctl.ipv4.ip_local_port_range.high":         int64(60999),
		"net.sysctl.ipv4.tcp_available_congestion_control": "reno cubic bbr",
		"net.sockstat.tcp_tw":                              int64(1),
		"net.sockets.udp_ipv4":                             int64(1),
		"net.sockets.unix":                                 int64(1),
		"net.snmp.Tcp.RetransSegs":                         int64(7),
		"net.netstat.TcpExt.ListenOverflows":               int64(7),
		"net.conntrack.count":                              int64(12),
	}
	for id, v := range vals {
		if got[id].Value != v {
			t.Errorf("%s = %#v, want %#v", id, got[id].Value, v)
		}
	}
	states := got["net.sockets.tcp_states"].Value.([]map[string]any)
	if len(states) != 11 || states[9]["state"] != "LISTEN" || states[9]["ipv4"] != int64(1) || states[9]["ipv6"] != int64(0) {
		t.Errorf("tcp states %v", states)
	}
	if r := got["net.routes.ipv4"].Value.([]map[string]any); r[0]["gateway"] != "10.0.2.1" || r[0]["default"] != true {
		t.Errorf("routes %v", r)
	}
}

func TestStatusPaths(t *testing.T) {
	files := modernFiles()
	for _, p := range []string{"/proc/net/if_inet6", "/proc/net/tcp6", "/proc/net/sockstat6", "/proc/net/udp6",
		"/proc/sys/net/netfilter/nf_conntrack_count", "/proc/sys/net/netfilter/nf_conntrack_max", "/proc/sys/net/core/default_qdisc"} {
		delete(files, p)
	}
	files["/proc/net/softnet_stat"] = "00000100 00000002 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000\n"
	files["/proc/net/netstat"] = "TcpExt: ListenOverflows\nTcpExt: 4\n"
	cmds := map[string]any{
		ipArgv:  map[string]any{"exit": 255, "stderr": `Option "-json" is unknown, try "ip -help".`},
		tcArgvS: map[string]any{"missing": true},
	}
	got := run(t, machine(t, files, cmds, "/proc/net/udp"))
	wants := map[string]obs.Status{
		"net.addresses.ipv4":                     obs.StatusUnsupported,
		"net.addresses.ipv6":                     obs.StatusAbsent,
		"net.qdiscs":                             obs.StatusAbsent,
		"net.sockstat6.tcp6_inuse":               obs.StatusAbsent,
		"net.sockets.udp_ipv4":                   obs.StatusPermissionDenied,
		"net.sockets.udp_ipv6":                   obs.StatusAbsent,
		"net.conntrack.count":                    obs.StatusAbsent,
		"net.sysctl.core.default_qdisc":          obs.StatusAbsent,
		"net.netstat.TcpExt.ListenOverflows":     obs.StatusOK,
		"net.netstat.TcpExt.TCPLossProbes":       obs.StatusAbsent,
		"net.sockets.tcp_states":                 obs.StatusOK,
		"net.softnet":                            obs.StatusOK,
		"net.sysctl.ipv4.tcp_congestion_control": obs.StatusOK,
	}
	for id, w := range wants {
		o := got[id]
		if o.Status != w {
			t.Errorf("%s: %s (%s), want %s", id, o.Status, o.Note, w)
		}
		if o.Status != obs.StatusOK && o.Note == "" {
			t.Errorf("%s: %s without a note", id, o.Status)
		}
	}
	if n := got["net.qdiscs"].Note; !strings.Contains(n, "not installed") {
		t.Errorf("tc missing note %q", n)
	}
	if n := got["net.addresses.ipv6"].Note; !strings.Contains(n, "IPv6") {
		t.Errorf("if_inet6 note %q", n)
	}
	if o := got["net.sockets.tcp_states"]; !strings.Contains(o.Note, "ipv6 column is missing") || o.Value.([]map[string]any)[0]["ipv6"] != nil {
		t.Errorf("tcp states without tcp6: %+v", o)
	}
	if o := got["net.softnet"]; !strings.Contains(o.Note, "row index") || o.Value.([]map[string]any)[0]["dropped"] != int64(2) {
		t.Errorf("old softnet: %+v", o)
	}

	// busybox ip prints its usage; a tc that cannot reach netlink is denied.
	cmds = map[string]any{
		ipArgv:  map[string]any{"exit": 1, "stderr": "ip: invalid option -- 'j'\nBusyBox v1.36.1 multi-call binary.\n\nUsage: ip [OPTIONS] address|route|link|tunnel|neigh|rule [ARGS]"},
		tcArgvS: map[string]any{"exit": 2, "stderr": "Cannot open netlink socket: Permission denied"},
	}
	got = run(t, machine(t, modernFiles(), cmds))
	if o := got["net.addresses.ipv4"]; o.Status != obs.StatusUnsupported || !strings.Contains(o.Note, "busybox") {
		t.Errorf("busybox ip: %+v", o)
	}
	if o := got["net.qdiscs"]; o.Status != obs.StatusPermissionDenied {
		t.Errorf("tc denied: %+v", o)
	}
}

func TestMalformed(t *testing.T) {
	files := modernFiles()
	for _, p := range []string{"/proc/net/if_inet6", "/proc/net/route", "/proc/net/ipv6_route", "/proc/net/sockstat",
		"/proc/net/tcp", "/proc/net/snmp", "/proc/net/softnet_stat", "/proc/sys/net/ipv4/tcp_rmem",
		"/proc/sys/net/netfilter/nf_conntrack_count", "/proc/sys/net/core/somaxconn"} {
		files[p] = "garbage that is not the format\n"
	}
	cmds := map[string]any{ipArgv: "not json", tcArgvS: "[{}]"}
	got := run(t, machine(t, files, cmds))
	for _, id := range []string{"net.addresses.ipv4", "net.addresses.ipv6", "net.routes.ipv4", "net.routes.ipv6",
		"net.sockstat.tcp_inuse", "net.sockets.tcp_states", "net.snmp.Tcp.InSegs", "net.softnet", "net.sysctl.ipv4.tcp_rmem.max",
		"net.conntrack.count", "net.sysctl.core.somaxconn", "net.qdiscs"} {
		if o := got[id]; o.Status != obs.StatusFailed || o.Note == "" {
			t.Errorf("%s: %s (%s), want failed with a note", id, o.Status, o.Note)
		}
	}
}

// An interface's name must never reach a note or a source path, where
// redaction cannot number it consistently.
func TestInterfaceNamesStayInTables(t *testing.T) {
	files := modernFiles()
	files["/sys/class/net/secretlink7/type"] = "1\n"
	files["/sys/class/net/secretlink7/operstate"] = "up\n"
	got := run(t, machine(t, files, modernCommands(), "/sys/class/net/secretlink7/operstate"))
	o := got["net.interfaces"]
	if o.Status != obs.StatusPermissionDenied || !strings.Contains(o.Note, "operstate") {
		t.Errorf("denied operstate: %+v", o)
	}
	files["/sys/class/net/secretlink7/type"] = "ether\n"
	got2 := run(t, machine(t, files, modernCommands()))
	if o := got2["net.interfaces"]; o.Status != obs.StatusFailed {
		t.Errorf("bad type: %+v", o)
	}
	for _, m := range []map[string]obs.Observation{got, got2} {
		for id, o := range m {
			s := o.Note + o.Source.Path + o.Source.Field + o.Source.Detail
			if strings.Contains(s, "secretlink7") || strings.Contains(s, "eth0") {
				t.Errorf("%s: an interface name in note or source: %+v", id, o)
			}
		}
	}
}
