package network

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestAddressClass(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1": "loopback", "::1": "loopback", "0.0.0.0": "unspecified", "::": "unspecified",
		"169.254.1.2": "link-local", "fe80::1": "link-local", "10.1.2.3": "private", "172.20.0.1": "private",
		"192.168.1.1": "private", "172.32.0.1": "global", "fd12:3456::1": "ula", "100.100.1.1": "cgnat",
		"224.0.0.251": "multicast", "ff02::1": "multicast", "203.0.113.9": "global", "2001:db8::5": "global",
		"::ffff:10.0.0.1": "private",
	} {
		if got := AddressClass(netip.MustParseAddr(in)); got != want {
			t.Errorf("AddressClass(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestMacScope(t *testing.T) {
	for in, want := range map[string]string{
		"00:00:00:00:00:00": "none", "02:42:ac:11:00:02": "local", "52:54:00:12:34:56": "local",
		"a8:00:00:00:00:01": "universal", "0A:1B:2C:3D:4E:5F": "local",
	} {
		if got, ok := macScope(in); !ok || got != want {
			t.Errorf("macScope(%s) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "00:00:00:00", "80:00:00:00:fe:80:00:00:00:00:00:00:00:00:00:00:00:00:00:00", "zz:00:00:00:00:00", "0:0:0:0:0:0"} {
		if _, ok := macScope(bad); ok {
			t.Errorf("macScope(%q) accepted", bad)
		}
	}
}

func TestParseIfInet6(t *testing.T) {
	in := "00000000000000000000000000000001 01 80 10 80       lo\n" +
		"fe800000000000000042acfffe110002 05 40 20 80  docker0\n" +
		"20010db8000000010000000000000042 02 40 00 00   enp3s0\n" +
		"20010db8000000014c3a9e1f0b2d7a61 02 40 00 01   enp3s0\n" +
		"fd000000000000000000000000000002 03 40 00 80    wg0\n"
	rows, err := parseIfInet6([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"interface": "lo", "address": "::1", "prefix_len": int64(128), "scope": "host", "class": "loopback", "dynamic": false, "temporary": false},
		{"interface": "docker0", "address": "fe80::42:acff:fe11:2", "prefix_len": int64(64), "scope": "link", "class": "link-local", "dynamic": false, "temporary": false},
		{"interface": "enp3s0", "address": "2001:db8:0:1::42", "prefix_len": int64(64), "scope": "global", "class": "global", "dynamic": true, "temporary": false},
		{"interface": "enp3s0", "address": "2001:db8:0:1:4c3a:9e1f:b2d:7a61", "prefix_len": int64(64), "scope": "global", "class": "global", "dynamic": true, "temporary": true},
		{"interface": "wg0", "address": "fd00::2", "prefix_len": int64(64), "scope": "global", "class": "ula", "dynamic": false, "temporary": false},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v", rows)
	}
	for _, bad := range []string{"0001 01 80 10 80 lo\n", "00000000000000000000000000000001 01 zz 10 80 lo\n", "00000000000000000000000000000001 01 81 10 80 lo\n"} {
		if _, err := parseIfInet6([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if rows, err := parseIfInet6(nil); err != nil || len(rows) != 0 {
		t.Errorf("empty file: %v %v", rows, err)
	}
}

func TestParseIPAddrJSON(t *testing.T) {
	in := `[{"ifindex":1,"ifname":"lo","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8,"scope":"host","label":"lo"}]},
{"ifindex":2,"ifname":"enp3s0","addr_info":[{"family":"inet","local":"192.168.1.20","prefixlen":24,"broadcast":"192.168.1.255","scope":"global","dynamic":true}]},
{"ifindex":3,"ifname":"wlan0","addr_info":[{}]},
{"ifindex":4,"ifname":"wg0","addr_info":[{"family":"inet6","local":"fd00::1","prefixlen":64}]}]`
	rows, err := parseIPAddrJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"interface": "lo", "address": "127.0.0.1", "prefix_len": int64(8), "scope": "host", "class": "loopback", "dynamic": false},
		{"interface": "enp3s0", "address": "192.168.1.20", "prefix_len": int64(24), "scope": "global", "class": "private", "dynamic": true},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v", rows)
	}
	for _, bad := range []string{`{}`, `[{"addr_info":[]}]`, `[{"ifname":"x","addr_info":[{"family":"inet","local":"nope","prefixlen":8}]}]`,
		`[{"ifname":"x","addr_info":[{"family":"inet","local":"10.0.0.1"}]}]`, `not json`} {
		if _, err := parseIPAddrJSON([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestParseRouteV4(t *testing.T) {
	in := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"enp3s0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"enp3s0\t0001A8C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
		"docker0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n" +
		"lo\t0000000A\t00000000\t0201\t0\t0\t0\t000000FF\t0\t0\t0\n"
	rows, err := parseRouteV4([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"interface": "enp3s0", "destination": "0.0.0.0", "prefix_len": int64(0), "destination_class": "unspecified", "gateway": "192.168.1.1", "via_gateway": true, "metric": int64(100), "default": true, "type": "unicast"},
		{"interface": "enp3s0", "destination": "192.168.1.0", "prefix_len": int64(24), "destination_class": "private", "via_gateway": false, "metric": int64(100), "default": false, "type": "unicast"},
		{"interface": "docker0", "destination": "172.17.0.0", "prefix_len": int64(16), "destination_class": "private", "via_gateway": false, "metric": int64(0), "default": false, "type": "unicast"},
		{"interface": "lo", "destination": "10.0.0.0", "prefix_len": int64(8), "destination_class": "private", "via_gateway": false, "metric": int64(0), "default": false, "type": "unreachable"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v", rows)
	}
	for _, bad := range []string{"", "enp3s0\t00000000\n", "Iface x\neth0\t00000000\t00000000\t0001\t0\t0\t0\t00FF00FF\n", "Iface x\neth0\tzz\t00000000\t0001\t0\t0\t0\t00000000\n"} {
		if _, err := parseRouteV4([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseRouteV6(t *testing.T) {
	z := strings.Repeat("0", 32)
	in := "20010db8000000010000000000000000 40 " + z + " 00 " + z + " 00000100 00000001 00000000 00000001   enp3s0\n" +
		z + " 00 " + z + " 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003   enp3s0\n" +
		z + " 00 " + z + " 00 " + z + " ffffffff 00000001 00000000 00200200       lo\n" +
		"00000000000000000000000000000001 80 " + z + " 00 " + z + " 00000000 00000002 00000000 80200001       lo\n" +
		"ff000000000000000000000000000000 08 " + z + " 00 " + z + " 00000100 00000001 00000000 00000001   enp3s0\n" +
		"20010db8000000020000000000000001 80 " + z + " 00 " + z + " 00000000 00000001 00000000 01000001   enp3s0\n"
	rows, err := parseRouteV6([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want the cached route left out: %v", len(rows), rows)
	}
	def := rows[1]
	if def["default"] != true || def["gateway"] != "fe80::1" || def["metric"] != int64(1024) || def["destination_class"] != "unspecified" {
		t.Errorf("default route %v", def)
	}
	if rows[2]["type"] != "unreachable" || rows[2]["default"] != false {
		t.Errorf("lo reject route %v", rows[2])
	}
	if rows[3]["type"] != "local" || rows[4]["type"] != "multicast" || rows[0]["prefix_len"] != int64(64) || rows[0]["destination"] != "2001:db8:0:1::" {
		t.Errorf("rows %v", rows)
	}
	if _, err := parseRouteV6([]byte("garbage line\n")); err == nil {
		t.Error("accepted garbage")
	}
}

func TestParseSockstat(t *testing.T) {
	in := "sockets: used 1234\nTCP: inuse 20 orphan 1 tw 3 alloc 25 mem 4\nUDP: inuse 5 mem 2\nUDPLITE: inuse 0\nRAW: inuse 0\nFRAG: inuse 0 memory 0\n"
	got, err := parseSockstat([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got["sockets used"] != 1234 || got["TCP tw"] != 3 || got["TCP mem"] != 4 || got["UDP mem"] != 2 || got["FRAG memory"] != 0 {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"TCP: inuse\n", "TCP inuse 3\n", "TCP: inuse x\n"} {
		if _, err := parseSockstat([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCountTCPStates(t *testing.T) {
	in := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:0277 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0 100 0 0 10 0\n" +
		"   1: 0100007F:0277 0100007F:A1B2 01 00000000:00000000 00:00000000 00000000     0        0 2 1 0 20 4 30 10 -1\n" +
		"   2: 0100007F:0277 0100007F:A1B3 06 00000000:00000000 03:00000123 00000000     0        0 0 3 0\n" +
		"   3: 0100007F:0277 0100007F:A1B4 06 00000000:00000000 03:00000123 00000000     0        0 0 3 0\n"
	got, err := countTCPStates([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got[10] != 1 || got[1] != 1 || got[6] != 2 {
		t.Errorf("counts %v", got)
	}
	for _, bad := range []string{"no header\n", "local_address\n 0: a b 00 x\n", "local_address\n 0: a b 7F x\n", "local_address\n 0: a\n"} {
		if _, err := countTCPStates([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseCounters(t *testing.T) {
	in := "Ip: Forwarding DefaultTTL InReceives\nIp: 2 64 12345\nTcp: RtoAlgorithm MaxConn CurrEstab\nTcp: 1 -1 7\n"
	got, err := parseCounters([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got["Ip: Forwarding"] != 2 || got["Ip: InReceives"] != 12345 || got["Tcp: MaxConn"] != -1 || got["Tcp: CurrEstab"] != 7 {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"Ip: a b\n", "Ip: a b\nTcp: 1 2\n", "Ip: a b\nIp: 1\n", "Ip: a\nIp: x\n"} {
		if _, err := parseCounters([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if counterField("net.netstat.", "net.netstat.TcpExt.ListenOverflows") != "TcpExt: ListenOverflows" {
		t.Error("counterField")
	}
}

func TestParseSoftnet(t *testing.T) {
	old := "0000a1b2 00000003 00000010 00000000 00000000 00000000 00000000 00000000 00000000 00000005 00000000\n" +
		"00000100 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000\n"
	rows, indexed, err := parseSoftnet([]byte(old))
	if err != nil || indexed || len(rows) != 2 {
		t.Fatalf("%v %v %v", rows, indexed, err)
	}
	if rows[0]["processed"] != int64(0xa1b2) || rows[0]["dropped"] != int64(3) || rows[0]["time_squeeze"] != int64(16) ||
		rows[0]["received_rps"] != int64(5) || rows[1]["cpu"] != int64(1) || rows[0]["backlog_len"] != nil {
		t.Errorf("old rows %v", rows)
	}
	modern := "00000100 00000000 00000002 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000004 00000006 00000001 00000003\n"
	rows, indexed, err = parseSoftnet([]byte(modern))
	if err != nil || !indexed || rows[0]["cpu"] != int64(6) || rows[0]["backlog_len"] != int64(4) {
		t.Errorf("modern rows %v %v %v", rows, indexed, err)
	}
	for _, bad := range []string{"00000001 00000002\n", strings.Repeat("zzzzzzzz ", 11) + "\n"} {
		if _, _, err := parseSoftnet([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseTCJSON(t *testing.T) {
	in := `[{"kind":"noqueue","handle":"0:","dev":"lo","root":true,"refcnt":2,"options":{},"bytes":0,"packets":0,"drops":0,"overlimits":0,"requeues":0,"backlog":0,"qlen":0},
{"kind":"mq","handle":"0:","dev":"eth0","root":true,"options":{},"bytes":5000,"packets":10,"drops":1,"overlimits":0,"requeues":2,"backlog":0,"qlen":0},
{"kind":"fq_codel","handle":"0:","parent":":1","dev":"eth0","options":{"limit":10240},"bytes":5000,"packets":10,"drops":1,"overlimits":0,"requeues":2,"backlog":1514,"qlen":1}]`
	rows, err := parseTCJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1]["kind"] != "mq" || rows[1]["root"] != true || rows[2]["parent"] != ":1" || rows[2]["root"] != false ||
		rows[2]["backlog_bytes"] != int64(1514) || rows[2]["qlen"] != int64(1) || rows[0]["interface"] != "lo" {
		t.Errorf("rows %v", rows)
	}
	plain := `[{"kind":"pfifo_fast","handle":"0:","dev":"eth0","root":true}]`
	if rows, err := parseTCJSON([]byte(plain)); err != nil || rows[0]["drops"] != nil {
		t.Errorf("without statistics: %v %v", rows, err)
	}
	for _, bad := range []string{`{}`, `[{"dev":"eth0"}]`, `[{"kind":"fq","dev":"eth0","drops":-1}]`} {
		if _, err := parseTCJSON([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestRejectsOption(t *testing.T) {
	for _, s := range []string{
		`Option "-json" is unknown, try "tc -help".`,
		"ip: invalid option -- 'j'\nBusyBox v1.36.1 (2024-06-10 07:11:47 UTC) multi-call binary.\n\nUsage: ip [OPTIONS] address|route|link|tunnel|neigh|rule [ARGS]",
	} {
		if !rejectsOption(s) {
			t.Errorf("did not recognise %q", s)
		}
	}
	if rejectsOption("Cannot open netlink socket: Permission denied") {
		t.Error("a permission failure is not an unknown option")
	}
}

func TestParseInts(t *testing.T) {
	if v, err := parseInts("4096\t131072\t6291456\n", 3); err != nil || !reflect.DeepEqual(v, []int64{4096, 131072, 6291456}) {
		t.Errorf("%v %v", v, err)
	}
	for _, bad := range []string{"1 2", "1 2 3 4", "1 x 3"} {
		if _, err := parseInts(bad, 3); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
