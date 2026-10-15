package network

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const sysNet = "/sys/class/net"

var interfacesCollector = collector{
	id:   "network.interfaces",
	owns: []string{"net.interfaces"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{interfaces(env)}
	},
}

// counterColumns are the statistics/ files kept, by column name.
var counterColumns = []string{"rx_bytes", "tx_bytes", "rx_packets", "tx_packets", "rx_errors", "tx_errors", "rx_dropped", "tx_dropped"}

// interfaces lists every interface with its attributes. The source path is
// the directory, never a per-interface path, and a failure inside one
// interface's directory is reported without its name.
func interfaces(env collect.Env) obs.Observation {
	const id = "net.interfaces"
	src := obs.File(sysNet, "")
	src.Detail = "*/type (ARPHRD number), operstate, carrier, mtu, speed, duplex, tx_queue_len, address, addr_assign_type, " +
		"queues/, queues/tx-*/byte_queue_limits/limit, statistics/, device/driver and master links, and whether wireless, bridge, bonding and device exist"
	names, err := env.ReadDir(sysNet)
	if err != nil {
		return obs.Errored(id, src, err)
	}
	rows := []map[string]any{}
	for _, name := range names {
		dir := sysNet + "/" + name
		// bonding_masters is a plain file in the same directory.
		if ok, err := env.Stat(dir + "/type"); err != nil {
			return ifaceError(id, src, "type", err)
		} else if !ok {
			continue
		}
		row, attr, err := ifaceRow(env, name, dir)
		if err != nil {
			return ifaceError(id, src, attr, err)
		}
		rows = append(rows, row)
	}
	return obs.OK(id, rows, src)
}

// ifaceError is the observation when an attribute every interface has
// could not be read: the status the error implies, with a note that names
// the attribute but not the interface.
func ifaceError(id string, src obs.Source, attr string, err error) obs.Observation {
	o := obs.Errored(id, src, err)
	if o.Status == obs.StatusOK {
		o.Status = obs.StatusFailed
	}
	o.Note = "could not read an interface's " + attr + ": " + errnoText(err)
	return o
}

// ifaceRow reads one interface. Attributes that some interfaces lack
// (speed of a virtual device, a MAC on a tunnel, statistics) are left out
// of the row; only type and operstate, which every interface has, fail the
// whole table, and attr names which one did.
func ifaceRow(env collect.Env, name, dir string) (map[string]any, string, error) {
	typ, err := interfaceType(env, dir)
	if err != nil {
		return nil, "type", err
	}
	row := map[string]any{"name": name, "type": typ}
	if s, err := readTrim(env, dir+"/operstate"); err == nil {
		row["operstate"] = s
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, "operstate", err
	}
	// Attributes the kernel refuses to report for some devices (speed on a
	// link that is down returns EINVAL) are simply missing from the row.
	optInt := func(file, col string, keep func(int64) bool) {
		s, err := readTrim(env, dir+"/"+file)
		if err != nil {
			return
		}
		if n, err := parseInt(s); err == nil && keep(n) {
			row[col] = n
		}
	}
	nonNeg := func(n int64) bool { return n >= 0 }
	optInt("mtu", "mtu", nonNeg)
	optInt("speed", "speed_mbps", func(n int64) bool { return n > 0 && n != 0xffffffff })
	optInt("tx_queue_len", "tx_queue_len", nonNeg)
	if s, err := readTrim(env, dir+"/carrier"); err == nil {
		switch s {
		case "1":
			row["carrier"] = true
		case "0":
			row["carrier"] = false
		}
	}
	if s, err := readTrim(env, dir+"/duplex"); err == nil && (s == "full" || s == "half") {
		row["duplex"] = s
	}
	if s, err := readTrim(env, dir+"/address"); err == nil && s != "" {
		if scope, ok := macScope(s); ok {
			row["mac"], row["mac_scope"] = strings.ToLower(s), scope
		}
	}
	if s, err := readTrim(env, dir+"/addr_assign_type"); err == nil {
		if o, ok := macOrigins[s]; ok {
			row["mac_origin"] = o
		}
	}
	if target, err := env.Readlink(dir + "/device/driver"); err == nil {
		row["driver"] = path.Base(target)
	}
	if target, err := env.Readlink(dir + "/master"); err == nil {
		row["master"] = path.Base(target)
	}
	queues(env, dir, row)
	for _, c := range counterColumns {
		optInt("statistics/"+c, c, nonNeg)
	}
	return row, "", nil
}

// queues counts receive and transmit queues and sums the byte queue limits
// of the transmit queues that have one.
func queues(env collect.Env, dir string, row map[string]any) {
	names, err := env.ReadDir(dir + "/queues")
	if err != nil {
		return
	}
	var rx, tx, bql int64
	var haveBQL bool
	for _, q := range names {
		switch {
		case strings.HasPrefix(q, "rx-"):
			rx++
		case strings.HasPrefix(q, "tx-"):
			tx++
			s, err := readTrim(env, dir+"/queues/"+q+"/byte_queue_limits/limit")
			if err != nil {
				continue
			}
			if n, err := parseInt(s); err == nil && n >= 0 {
				bql += n
				haveBQL = true
			}
		}
	}
	row["rx_queues"], row["tx_queues"] = rx, tx
	if haveBQL {
		row["bql_limit_bytes"] = bql
	}
}

// macOrigins names the values of addr_assign_type.
var macOrigins = map[string]string{"0": "permanent", "1": "random", "2": "stolen", "3": "set"}

// macScope classifies a hardware address: "none" when it is all zeros
// (loopback), "local" when the locally administered bit is set (random,
// virtual and container interfaces), "universal" for an address assigned by
// the manufacturer. Only 6-byte (Ethernet-style) addresses are classified.
func macScope(s string) (string, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return "", false
	}
	var b [6]byte
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 16, 8)
		if err != nil || len(p) != 2 {
			return "", false
		}
		b[i] = byte(n)
	}
	switch {
	case b == [6]byte{}:
		return "none", true
	case b[0]&0x02 != 0:
		return "local", true
	}
	return "universal", true
}

// interfaceType names an interface's kind from its ARPHRD hardware type,
// refined for Ethernet-framed interfaces: Wi-Fi, bridges, bonds and virtual
// devices (veth, tap, docker0's members) all report type 1.
func interfaceType(env collect.Env, dir string) (string, error) {
	s, err := readTrim(env, dir+"/type")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return "", fmt.Errorf("not a number: %q", clip(s))
	}
	switch n {
	case 1:
		for _, c := range []struct{ sub, typ string }{{"wireless", "wireless"}, {"bridge", "bridge"}, {"bonding", "bond"}, {"device", "ethernet"}} {
			ok, err := env.Stat(dir + "/" + c.sub)
			if err != nil {
				return "", err
			}
			if ok {
				return c.typ, nil
			}
		}
		return "virtual", nil
	case 772:
		return "loopback", nil
	case 65534, 768, 769, 776, 778, 823:
		// No link-layer header: WireGuard, tun, and IP-in-IP tunnels.
		return "tunnel", nil
	case 512:
		return "ppp", nil
	case 32:
		return "infiniband", nil
	case 280:
		return "can", nil
	}
	return "arphrd-" + s, nil
}
