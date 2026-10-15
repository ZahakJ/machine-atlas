package network

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// AddressClass names the kind of an address the way the lessons discuss
// it: unspecified, loopback, link-local, private (RFC 1918), ula (fc00::/7),
// cgnat (100.64.0.0/10, shared by carrier-grade NAT), multicast, or global.
// The documentation ranges (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24,
// 2001:db8::/32) stand in for public addresses in examples and in the
// fixture machines, and are classed as global.
func AddressClass(a netip.Addr) string {
	a = a.Unmap()
	switch {
	case !a.IsValid():
		return ""
	case a.IsUnspecified():
		return "unspecified"
	case a.IsLoopback():
		return "loopback"
	case a.IsLinkLocalUnicast():
		return "link-local"
	case a.IsMulticast():
		return "multicast"
	case a.Is4() && a.IsPrivate():
		return "private"
	case a.Is6() && a.IsPrivate():
		return "ula"
	case cgnat.Contains(a):
		return "cgnat"
	}
	return "global"
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

var ipv4AddrArgv = []string{"ip", "-json", "-4", "address", "show"}

var ipv4AddrCollector = collector{
	id:   "network.ip_address",
	owns: []string{"net.addresses.ipv4"},
	run: func(ctx context.Context, env collect.Env) []obs.Observation {
		const id = "net.addresses.ipv4"
		out, o, ok := runJSON(ctx, env, id,
			"ip (iproute2) is not installed, and no file under /proc lists IPv4 addresses by interface",
			"this ip has no JSON output (busybox, or iproute2 before 4.13)", ipv4AddrArgv...)
		if !ok {
			return []obs.Observation{o}
		}
		src := obs.Command(ipv4AddrArgv...)
		rows, err := parseIPAddrJSON(out)
		if err != nil {
			return []obs.Observation{malformed(id, src, err)}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// ipLink is one interface in `ip -json address show`.
type ipLink struct {
	IfName   string `json:"ifname"`
	AddrInfo []struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		PrefixLen *int   `json:"prefixlen"`
		Scope     string `json:"scope"`
		Dynamic   bool   `json:"dynamic"`
	} `json:"addr_info"`
}

// parseIPAddrJSON turns `ip -json -4 address show` into one row per IPv4
// address. Entries of other families, and the empty objects some iproute2
// versions print for interfaces without an address, are skipped.
func parseIPAddrJSON(data []byte) ([]map[string]any, error) {
	var links []ipLink
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&links); err != nil {
		return nil, fmt.Errorf("ip output: %w", err)
	}
	rows := []map[string]any{}
	for _, l := range links {
		if l.IfName == "" {
			return nil, errors.New("ip output: an interface without ifname")
		}
		for _, a := range l.AddrInfo {
			if a.Family != "inet" {
				continue
			}
			addr, err := netip.ParseAddr(a.Local)
			if err != nil || !addr.Is4() {
				return nil, fmt.Errorf("ip output: %q is not an IPv4 address", clip(a.Local))
			}
			if a.PrefixLen == nil || *a.PrefixLen < 0 || *a.PrefixLen > 32 {
				return nil, errors.New("ip output: an IPv4 address without a valid prefixlen")
			}
			rows = append(rows, map[string]any{
				"interface": l.IfName, "address": addr.String(), "prefix_len": int64(*a.PrefixLen),
				"scope": a.Scope, "class": AddressClass(addr), "dynamic": a.Dynamic,
			})
		}
	}
	return rows, nil
}

const ifInet6 = "/proc/net/if_inet6"

var ipv6AddrCollector = collector{
	id:   "network.if_inet6",
	owns: []string{"net.addresses.ipv6"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		const id = "net.addresses.ipv6"
		src := obs.File(ifInet6, "")
		data, err := env.ReadFile(ifInet6)
		if err != nil {
			return []obs.Observation{unavailable(id, src, err, "no IPv6 here: the kernel was built without it, booted with ipv6.disable=1, or every interface has it disabled")}
		}
		rows, err := parseIfInet6(data)
		if err != nil {
			return []obs.Observation{malformed(id, src, err)}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// IPv6 address flags (include/uapi/linux/if_addr.h).
const (
	ifaFTemporary = 0x01
	ifaFPermanent = 0x80
)

// ipv6Scopes names the scope values of if_inet6 (IPV6_ADDR_SCOPE_*, shifted
// as the file prints them).
var ipv6Scopes = map[uint64]string{0x00: "global", 0x10: "host", 0x20: "link", 0x40: "site"}

// parseIfInet6 reads /proc/net/if_inet6: per line the address as 32 hex
// digits, the interface index, prefix length, scope and flags in hex, and
// the interface name.
func parseIfInet6(data []byte) ([]map[string]any, error) {
	rows := []map[string]any{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 6 || len(f[0]) != 32 {
			return nil, fmt.Errorf("line %d: want 6 fields starting with 32 hex digits, got %q", i+1, clip(line))
		}
		raw, err := hex.DecodeString(f[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: address: %v", i+1, err)
		}
		addr := netip.AddrFrom16([16]byte(raw))
		var n [3]uint64
		for j, s := range f[2:5] {
			if n[j], err = strconv.ParseUint(s, 16, 32); err != nil {
				return nil, fmt.Errorf("line %d: %q is not hex", i+1, s)
			}
		}
		plen, scope, flags := n[0], n[1], n[2]
		if plen > 128 {
			return nil, fmt.Errorf("line %d: prefix length %d", i+1, plen)
		}
		scopeName, ok := ipv6Scopes[scope]
		if !ok {
			scopeName = fmt.Sprintf("0x%02x", scope)
		}
		rows = append(rows, map[string]any{
			"interface": f[5], "address": addr.String(), "prefix_len": int64(plen), "scope": scopeName,
			"class": AddressClass(addr), "dynamic": flags&ifaFPermanent == 0, "temporary": flags&ifaFTemporary != 0,
		})
	}
	return rows, nil
}
