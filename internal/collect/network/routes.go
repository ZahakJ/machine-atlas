package network

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"net/netip"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	routeV4 = "/proc/net/route"
	routeV6 = "/proc/net/ipv6_route"
)

var routesCollector = collector{
	id:   "network.routes",
	owns: []string{"net.routes.ipv4", "net.routes.ipv6"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{
			routeObs(env, "net.routes.ipv4", routeV4, parseRouteV4, "the kernel has no IPv4 /proc interface here"),
			routeObs(env, "net.routes.ipv6", routeV6, parseRouteV6, "no IPv6 here: the kernel was built without it or booted with ipv6.disable=1"),
		}
	},
}

func routeObs(env collect.Env, id, path string, parse func([]byte) ([]map[string]any, error), absent string) obs.Observation {
	src := obs.File(path, "")
	data, err := env.ReadFile(path)
	if err != nil {
		return unavailable(id, src, err, absent)
	}
	rows, err := parse(data)
	if err != nil {
		return malformed(id, src, err)
	}
	o := obs.OK(id, rows, src)
	if len(rows) > obs.MaxTableRows {
		o.Value = rows[:obs.MaxTableRows]
		o.Note = fmt.Sprintf("the first %d of %d routes; a machine with a full routing table holds far more than these lessons need", obs.MaxTableRows, len(rows))
	}
	return o
}

// Route flags (include/uapi/linux/route.h, ipv6_route.h).
const (
	rtfGateway = 0x0002
	rtfReject  = 0x0200
	rtfCache   = 0x01000000
	rtfAnycast = 0x00100000
	rtfLocal   = 0x80000000
)

// parseRouteV4 reads /proc/net/route: a header line, then per route the
// interface and, in hexadecimal, destination, gateway, flags, reference
// count, use count, metric (decimal), mask, MTU, window and IRTT.
// Addresses are the four network-order bytes printed as one native integer,
// so on a little-endian machine, which every architecture Machine Atlas
// collects on is, 0100A8C0 is 192.168.0.1.
func parseRouteV4(data []byte) ([]map[string]any, error) {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Iface") {
		return nil, fmt.Errorf("first line is not the Iface header: %q", clip(lines[0]))
	}
	rows := []map[string]any{}
	for i, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 8 {
			return nil, fmt.Errorf("line %d: %d fields, want at least 8", i+2, len(f))
		}
		dst, err1 := hexIPv4(f[1])
		gw, err2 := hexIPv4(f[2])
		mask, err3 := hexIPv4(f[7])
		flags, err4 := strconv.ParseUint(f[3], 16, 32)
		metric, err5 := strconv.ParseInt(f[6], 10, 64)
		for _, err := range []error{err1, err2, err3, err4, err5} {
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", i+2, err)
			}
		}
		m := binary.BigEndian.Uint32(mask.AsSlice())
		plen := bits.OnesCount32(m)
		if m != ^uint32(0)<<(32-plen) {
			return nil, fmt.Errorf("line %d: mask %s is not contiguous", i+2, mask)
		}
		row := map[string]any{
			"interface": f[0], "destination": dst.String(), "prefix_len": int64(plen),
			"destination_class": AddressClass(dst), "via_gateway": flags&rtfGateway != 0,
			"metric": metric, "default": plen == 0 && flags&rtfReject == 0, "type": routeType(flags, dst),
		}
		if flags&rtfGateway != 0 {
			row["gateway"] = gw.String()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// hexIPv4 decodes an address as /proc/net/route prints it.
func hexIPv4(s string) (netip.Addr, error) {
	if len(s) != 8 {
		return netip.Addr{}, fmt.Errorf("%q is not 8 hex digits", clip(s))
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%q is not hex", s)
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(n))
	return netip.AddrFrom4(b), nil
}

func routeType(flags uint64, dst netip.Addr) string {
	switch {
	case flags&rtfReject != 0:
		return "unreachable"
	case flags&rtfLocal != 0:
		return "local"
	case flags&rtfAnycast != 0:
		return "anycast"
	case dst.IsMulticast():
		return "multicast"
	}
	return "unicast"
}

// parseRouteV6 reads /proc/net/ipv6_route: per route the destination (32
// hex digits) and prefix length, source and its prefix length, next hop,
// then metric, reference count, use count and flags in hex, and the
// interface. Cached routes (RTF_CACHE) are left out.
func parseRouteV6(data []byte) ([]map[string]any, error) {
	rows := []map[string]any{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 10 || len(f[0]) != 32 || len(f[4]) != 32 {
			return nil, fmt.Errorf("line %d: want 10 fields with 32-digit addresses, got %q", i+1, clip(line))
		}
		dst, err := hexIPv6(f[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		gw, err := hexIPv6(f[4])
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		var n [3]uint64
		for j, s := range []string{f[1], f[5], f[8]} {
			if n[j], err = strconv.ParseUint(s, 16, 32); err != nil {
				return nil, fmt.Errorf("line %d: %q is not hex", i+1, s)
			}
		}
		plen, metric, flags := n[0], n[1], n[2]
		if plen > 128 {
			return nil, fmt.Errorf("line %d: prefix length %d", i+1, plen)
		}
		if flags&rtfCache != 0 {
			continue
		}
		typ := routeType(flags, dst)
		row := map[string]any{
			"interface": f[9], "destination": dst.String(), "prefix_len": int64(plen),
			"destination_class": AddressClass(dst), "via_gateway": flags&rtfGateway != 0,
			"metric": int64(metric), "default": plen == 0 && typ == "unicast", "type": typ,
		}
		if flags&rtfGateway != 0 {
			row["gateway"] = gw.String()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func hexIPv6(s string) (netip.Addr, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 16 {
		return netip.Addr{}, fmt.Errorf("%q is not 32 hex digits", clip(s))
	}
	return netip.AddrFrom16([16]byte(raw)), nil
}
