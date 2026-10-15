package snapshot

import (
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Invented identifying values a network snapshot holds: interface names
// that are not patterns, addresses, MACs, gateways and a driver.
var netIdentifying = []string{
	"wlp3s0", "enp5s0", "wg-office", "192.168.31.24", "192.168.31.1", "192.168.31.0", "10.13.0.0", "2001:db8:85a3::8a2e:370:7334",
	"fe80::1c2b:3aff:fe4d:5e6f", "0a:1b:2c:3d:4e:5f", "02:42:ac:11:00:02", "iwlwifi",
}

func networkSnapshot() *Snapshot {
	n, l := obs.ScopeNamespace, obs.KindLive
	observations := []obs.Observation{
		ob("net.interfaces", []map[string]any{
			{"name": "enp5s0", "type": "ethernet", "operstate": "up", "mac": "0a:1b:2c:3d:4e:5f", "mac_scope": "local", "driver": "e1000e", "mtu": int64(1500)},
			{"name": "wlp3s0", "type": "wireless", "operstate": "up", "mac": "02:42:ac:11:00:02", "mac_scope": "local", "driver": "iwlwifi"},
			{"name": "wg-office", "type": "tunnel", "operstate": "unknown"},
			{"name": "lo", "type": "loopback", "operstate": "unknown", "mac": "00:00:00:00:00:00", "mac_scope": "none"},
		}, l, n, ""),
		ob("net.addresses.ipv4", []map[string]any{
			{"interface": "wlp3s0", "address": "192.168.31.24", "prefix_len": int64(24), "scope": "global", "class": "private"},
			{"interface": "lo", "address": "127.0.0.1", "prefix_len": int64(8), "scope": "host", "class": "loopback"},
		}, l, n, ""),
		ob("net.addresses.ipv6", []map[string]any{
			{"interface": "wlp3s0", "address": "2001:db8:85a3::8a2e:370:7334", "prefix_len": int64(64), "scope": "global", "class": "global", "temporary": true},
			{"interface": "wlp3s0", "address": "fe80::1c2b:3aff:fe4d:5e6f", "prefix_len": int64(64), "scope": "link", "class": "link-local"},
		}, l, n, ""),
		ob("net.routes.ipv4", []map[string]any{
			{"interface": "wlp3s0", "destination": "0.0.0.0", "prefix_len": int64(0), "gateway": "192.168.31.1", "via_gateway": true, "default": true, "destination_class": "unspecified", "metric": int64(600), "type": "unicast"},
			{"interface": "wlp3s0", "destination": "192.168.31.0", "prefix_len": int64(24), "via_gateway": false, "default": false, "destination_class": "private", "metric": int64(600), "type": "unicast"},
			{"interface": "wg-office", "destination": "10.13.0.0", "prefix_len": int64(16), "via_gateway": false, "default": false, "destination_class": "private", "type": "unicast"},
		}, l, n, ""),
		ob("net.qdiscs", []map[string]any{
			{"interface": "wg-office", "kind": "noqueue", "root": true},
			{"interface": "enp5s0", "kind": "fq_codel", "root": true},
		}, l, n, ""),
		ob("os.hostname", "atlas-test-box", obs.KindStatic, n, ""),
	}
	rep := collect.Report{Mode: "live", StartedAt: testTime, FinishedAt: testTime,
		Collectors: []collect.CollectorRun{{ID: "test", Status: collect.RunOK, Observations: len(observations)}}}
	return Build(rep, observations)
}

func rowsOf(t *testing.T, s *Snapshot, id string) []map[string]any {
	t.Helper()
	return value(t, s, id).([]map[string]any)
}

// Under the default policy every address, MAC, gateway and destination cell
// is replaced, whatever its value, and the computed columns survive.
func TestRedactDefaultReplacesAddressColumns(t *testing.T) {
	red, _, err := Redact(networkSnapshot(), PolicyDefault)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"net.addresses.ipv4", "net.addresses.ipv6", "net.routes.ipv4", "net.interfaces"} {
		for i, row := range rowsOf(t, red, id) {
			for _, col := range []string{"address", "gateway", "destination", "mac"} {
				if v, ok := row[col]; ok && v != "<redacted:address>" {
					t.Errorf("%s row %d: %s = %v survived default redaction", id, i, col, v)
				}
			}
		}
	}
	v4 := rowsOf(t, red, "net.addresses.ipv4")
	if v4[0]["class"] != "private" || v4[1]["class"] != "loopback" || v4[0]["scope"] != "global" || v4[0]["interface"] != "wlp3s0" {
		t.Errorf("default must keep class, scope and interface names: %v", v4)
	}
	routes := rowsOf(t, red, "net.routes.ipv4")
	if routes[0]["default"] != true || routes[0]["via_gateway"] != true || routes[1]["destination_class"] != "private" {
		t.Errorf("route flags and classes lost: %v", routes)
	}
	ifs := rowsOf(t, red, "net.interfaces")
	if ifs[0]["mac_scope"] != "local" || ifs[0]["driver"] != "e1000e" || ifs[3]["mac_scope"] != "none" {
		t.Errorf("interfaces: %v", ifs)
	}
	again, more, _ := Redact(red, PolicyDefault)
	if len(more) != 0 || encoded(t, again) != encoded(t, red) {
		t.Errorf("second default redaction changed %d things: %v", len(more), more)
	}
}

// Under strict, nothing identifying survives anywhere, and one interface
// gets one placeholder in every table, so tables still join.
func TestRedactStrictNumbersInterfacesAcrossTables(t *testing.T) {
	s := networkSnapshot()
	before := encoded(t, s)
	for _, tok := range netIdentifying {
		if !strings.Contains(before, tok) {
			t.Fatalf("seed does not contain %q", tok)
		}
	}
	red, _, err := Redact(s, PolicyStrict)
	if err != nil {
		t.Fatal(err)
	}
	after := encoded(t, red)
	for _, tok := range netIdentifying {
		if strings.Contains(after, tok) {
			t.Errorf("%q survived strict redaction", tok)
		}
	}
	name := map[string]any{} // original name → placeholder, from net.interfaces
	orig := rowsOf(t, s, "net.interfaces")
	for i, row := range rowsOf(t, red, "net.interfaces") {
		name[orig[i]["name"].(string)] = row["name"]
		if p, _ := row["name"].(string); !strings.HasPrefix(p, "<if-") {
			t.Errorf("interface %d named %v, want an <if-N> placeholder", i, row["name"])
		}
		if d, ok := row["driver"]; ok && d != "<redacted:freetext>" {
			t.Errorf("driver %v survived strict", d)
		}
	}
	if name["wlp3s0"] == name["enp5s0"] || name["lo"] == name["wg-office"] {
		t.Errorf("distinct interfaces share a placeholder: %v", name)
	}
	for _, id := range []string{"net.addresses.ipv4", "net.addresses.ipv6", "net.routes.ipv4", "net.qdiscs"} {
		origRows := rowsOf(t, s, id)
		for i, row := range rowsOf(t, red, id) {
			if want := name[origRows[i]["interface"].(string)]; row["interface"] != want {
				t.Errorf("%s row %d: interface %v, but net.interfaces calls it %v", id, i, row["interface"], want)
			}
		}
	}
	if errs := Validate(red); len(errs) > 0 {
		t.Errorf("strict snapshot is invalid: %v", errs)
	}
	again, more, _ := Redact(red, PolicyStrict)
	if len(more) != 0 || encoded(t, again) != after {
		t.Errorf("second strict redaction changed %d things: %v", len(more), more)
	}
}
