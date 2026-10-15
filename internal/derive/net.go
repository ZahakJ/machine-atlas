package derive

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	routes4    = "net.routes.ipv4"
	routes6    = "net.routes.ipv6"
	ifaces     = "net.interfaces"
	qdiscs     = "net.qdiscs"
	refRTT     = 0.1 // seconds: the round trip the buffer facts are stated at
	refRTTText = "100 ms"
)

func netFacts(in inputs) []Fact {
	var out []Fact
	out = append(out, defaultRouteFacts(in)...)
	out = append(out, addressFacts(in)...)
	out = append(out, tcpFacts(in)...)
	out = append(out, softnetFacts(in)...)

	c := in.calc()
	count, max := c.int("net.conntrack.count"), c.int("net.conntrack.max")
	if !c.failed() && max <= 0 {
		c.fail("net.conntrack.max is %d, so a fraction of it is undefined", max)
	}
	var fill any
	if !c.failed() {
		fill = float64(count) / float64(max)
	}
	out = append(out, c.fact("derived.net.conntrack.fill_fraction", "nf_conntrack_count / nf_conntrack_max", fill))

	c = in.calc()
	lo, hi := c.int("net.sysctl.ipv4.ip_local_port_range.low"), c.int("net.sysctl.ipv4.ip_local_port_range.high")
	if !c.failed() && hi < lo {
		c.fail("ip_local_port_range ends (%d) below its start (%d)", hi, lo)
	}
	out = append(out, c.fact("derived.net.ephemeral_ports", "ip_local_port_range high − low + 1", hi-lo+1))
	return out
}

// defaultRoute finds the route the kernel uses for an address no other
// route covers: the IPv4 default route with the lowest metric, or the IPv6
// one when IPv4 has none. It records its inputs in c and fails c when the
// answer cannot be known. The returned family is "IPv4" or "IPv6".
func defaultRoute(c *calc) (iface, family string, found bool) {
	pick := func(id string) (string, bool, bool) {
		o := c.look(id)
		switch o.Status {
		case obs.StatusOK:
		case obs.StatusAbsent: // no IPv6 in this kernel: no IPv6 routes
			return "", false, true
		default:
			return "", false, false
		}
		rows, _ := o.Value.([]map[string]any)
		var best map[string]any
		for _, r := range rows {
			if r["default"] != true {
				continue
			}
			if best == nil || metric(r) < metric(best) {
				best = r
			}
		}
		if best == nil {
			return "", false, true
		}
		name, _ := best["interface"].(string)
		return name, true, true
	}
	if name, ok, read := pick(routes4); ok {
		return name, "IPv4", true
	} else if name6, ok6, read6 := pick(routes6); ok6 {
		return name6, "IPv6", true
	} else if read && read6 {
		return "", "", false
	} else {
		for _, id := range []string{routes4, routes6} {
			if o := c.look(id); o.Status != obs.StatusOK && o.Status != obs.StatusAbsent {
				c.fail("%s is %s", id, statusWords(o.Status))
			}
		}
		return "", "", false
	}
}

func metric(r map[string]any) int64 {
	n, _ := obs.Int(r["metric"])
	return n
}

const defaultRule = "the interface of the IPv4 default route with the lowest metric (the IPv6 one when IPv4 has none); policy routing rules, which atlas cannot read, can send some traffic elsewhere"

func defaultRouteFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	_, family, found := defaultRoute(c)
	rule := "a route for every destination (prefix length zero) exists, IPv4 or IPv6, in the routing tables atlas can read"
	if found {
		rule += " (here " + family + ")"
	}
	out = append(out, c.fact("derived.net.default_route", rule, found))

	// lookup returns the default route's interface or fails c.
	lookup := func() (*calc, string) {
		c := in.calc()
		name, _, found := defaultRoute(c)
		if !found && !c.failed() {
			c.fail("there is no default route")
		}
		return c, name
	}

	c, name := lookup()
	out = append(out, c.fact("derived.net.default_route_interface", defaultRule, name))

	c, name = lookup()
	row := c.row(ifaces, "name", name)
	typ, _ := row["type"].(string)
	if row != nil && typ == "" {
		c.fail("%s has no type for the default route's interface", ifaces)
	}
	out = append(out, c.fact("derived.net.default_route_type", "the type column of net.interfaces for "+defaultRule, typ))

	for _, f := range []struct{ id, col, what string }{
		{"derived.net.default_route_mtu", "mtu", "MTU"},
		{"derived.net.default_route_tx_queues", "tx_queues", "transmit queue count"},
	} {
		c, name = lookup()
		v := c.cell(ifaces, c.row(ifaces, "name", name), f.col)
		out = append(out, c.fact(f.id, "the "+f.what+" in net.interfaces of the default route's interface", v))
	}

	c, name = lookup()
	speed := linkSpeed(c, name)
	out = append(out, c.fact("derived.net.default_route_speed_mbps", "the link speed (Mbit/s) in net.interfaces of the default route's interface", speed))

	c, name = lookup()
	speed = linkSpeed(c, name)
	out = append(out, c.fact("derived.net.default_route_bdp_100ms_bytes",
		"speed_mbps × 10⁶ / 8 × 0.1 s: the bytes in flight that fill the default route's link at a "+refRTTText+" round trip", int64(float64(speed)*1e6/8*refRTT)))

	c, name = lookup()
	kind, how := schedulingQdisc(c, name)
	out = append(out, c.fact("derived.net.default_route_qdisc", "the queueing discipline that schedules the default route's interface in net.qdiscs: "+how, kind))
	return out
}

// linkSpeed is the speed_mbps of an interface, failing c with the reason
// when the driver reports none.
func linkSpeed(c *calc, name string) int64 {
	row := c.row(ifaces, "name", name)
	if c.failed() {
		return 0
	}
	if row["speed_mbps"] == nil {
		c.fail("the default route's interface reports no link speed (wireless, virtual and down links report none)")
		return 0
	}
	return c.cell(ifaces, row, "speed_mbps")
}

// schedulingQdisc returns the kind of qdisc that orders packets leaving
// through an interface: its root qdisc; under a multiqueue root (mq,
// mqprio) the kind of the per-queue children when they agree; and for a
// bond or bridge with no queue of its own (noqueue) the qdisc of its
// members, which is where the packets wait. how says which case applied.
func schedulingQdisc(c *calc, name string) (string, string) {
	const how = "its root qdisc; under a multiqueue root (mq) the per-queue qdiscs; for a bond or bridge without a queue (noqueue), its members' qdiscs"
	t := c.table(qdiscs)
	if c.failed() {
		return "", how
	}
	kind, detail, ok := ownQdisc(t, name)
	if !ok {
		c.fail("%s lists no root qdisc for the default route's interface", qdiscs)
		return "", how
	}
	if kind != "noqueue" {
		return kind, how + " (here " + detail + ")"
	}
	// noqueue: look for members (net.interfaces rows whose master is name).
	// The interface list is optional here; without it noqueue stands.
	var members []string
	if o := c.look(ifaces); o.Status == obs.StatusOK {
		rows, _ := o.Value.([]map[string]any)
		for _, r := range rows {
			if r["master"] == name {
				m, _ := r["name"].(string)
				members = append(members, m)
			}
		}
	}
	if len(members) == 0 {
		return "noqueue", how + " (here the root, noqueue, with no members)"
	}
	var kinds []string
	for _, m := range members {
		k, _, ok := ownQdisc(t, m)
		if ok {
			kinds = append(kinds, k)
		}
	}
	slices.Sort(kinds)
	switch {
	case len(kinds) == 0:
		return "noqueue", how + fmt.Sprintf(" (here noqueue; its %d members have no qdisc listed)", len(members))
	case kinds[0] != kinds[len(kinds)-1]:
		return "mixed", how + fmt.Sprintf(" (here noqueue; its %d members use different qdiscs)", len(members))
	}
	return kinds[0], how + fmt.Sprintf(" (here noqueue; its %d members each schedule with %s)", len(members), kinds[0])
}

// ownQdisc is the scheduling qdisc of one interface from the qdisc table,
// looking through a multiqueue root to its children.
func ownQdisc(t []map[string]any, name string) (kind, detail string, ok bool) {
	var root string
	var children []string
	for _, r := range t {
		if r["interface"] != name {
			continue
		}
		k, _ := r["kind"].(string)
		if r["root"] == true {
			root = k
		} else if p, _ := r["parent"].(string); p != "" && p != "ffff:fff1" && k != "ingress" && k != "clsact" {
			children = append(children, k)
		}
	}
	if root == "" {
		return "", "", false
	}
	if root != "mq" && root != "mqprio" || len(children) == 0 {
		return root, "the root, " + root, true
	}
	slices.Sort(children)
	if children[0] != children[len(children)-1] {
		return "mixed", fmt.Sprintf("%s over %d queues with different qdiscs", root, len(children)), true
	}
	return children[0], fmt.Sprintf("%s over %d queues, each with %s", root, len(children), children[0]), true
}

func addressFacts(in inputs) []Fact {
	var out []Fact
	global := func(r map[string]any) bool { return r["class"] == "global" }
	for _, f := range []struct {
		id, rule string
		tables   []string
	}{
		{"derived.net.global_address", "an IPv4 or IPv6 address of class global exists on some interface", []string{"net.addresses.ipv4", "net.addresses.ipv6"}},
		{"derived.net.global_ipv6", "an IPv6 address of class global exists on some interface", []string{"net.addresses.ipv6"}},
	} {
		c := in.calc()
		v, ok := someRow(c, f.tables, global)
		var val any
		if ok {
			val = v
		}
		out = append(out, c.fact(f.id, f.rule, val))
	}

	c := in.calc()
	t := c.table(ifaces)
	out = append(out, c.fact("derived.net.interface_count", "rows of net.interfaces", int64(len(t))))

	c = in.calc()
	t = c.table(ifaces)
	var n int64
	for _, r := range t {
		if r["type"] == "ethernet" || r["type"] == "wireless" {
			n++
		}
	}
	out = append(out, c.fact("derived.net.physical_interfaces", "rows of net.interfaces whose type is ethernet or wireless", n))

	c = in.calc()
	t = c.table(ifaces)
	multi := false
	for _, r := range t {
		if q, _ := obs.Int(r["tx_queues"]); q > 1 {
			multi = true
		}
	}
	out = append(out, c.fact("derived.net.multiqueue", "some interface in net.interfaces has more than one transmit queue", multi))
	return out
}

// someRow answers "does any row of these tables satisfy test": yes as soon
// as a readable table has such a row, no when every table was read and none
// has, unknown (c failed) otherwise. An absent table (no IPv6) counts as
// read and empty.
func someRow(c *calc, ids []string, test func(map[string]any) bool) (bool, bool) {
	var unread []string
	for _, id := range ids {
		o := c.look(id)
		switch o.Status {
		case obs.StatusOK:
			rows, _ := o.Value.([]map[string]any)
			if slices.ContainsFunc(rows, test) {
				return true, true
			}
		case obs.StatusAbsent:
		default:
			unread = append(unread, fmt.Sprintf("%s is %s", id, statusWords(o.Status)))
		}
	}
	for _, r := range unread {
		c.fail("%s", r)
	}
	return false, len(unread) == 0
}

func tcpFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	re, sent := c.int("net.snmp.Tcp.RetransSegs"), c.int("net.snmp.Tcp.OutSegs")
	if !c.failed() && sent <= 0 {
		c.fail("no TCP segment has been sent (OutSegs is %d)", sent)
	}
	var frac any
	if !c.failed() {
		frac = float64(re) / float64(sent)
	}
	out = append(out, c.fact("derived.net.tcp.retrans_fraction", "RetransSegs / OutSegs since boot", frac))

	c = in.calc()
	rmax := c.int("net.sysctl.ipv4.tcp_rmem.max")
	out = append(out, c.fact("derived.net.tcp.rmem_max_mbps_at_100ms",
		"tcp_rmem max × 8 / "+refRTTText+" / 10⁶: the receive rate, in Mbit/s, of one connection whose window is the whole buffer ceiling, at a "+refRTTText+" round trip",
		int64(float64(rmax)*8/refRTT/1e6)))

	c = in.calc()
	rmax = c.int("net.sysctl.ipv4.tcp_rmem.max")
	name, _, found := defaultRoute(c)
	if !found && !c.failed() {
		c.fail("there is no default route")
	}
	speed := linkSpeed(c, name)
	bdp := int64(float64(speed) * 1e6 / 8 * refRTT)
	out = append(out, c.fact("derived.net.tcp.rmem_covers_bdp",
		"tcp_rmem max ≥ the default route link's bandwidth-delay product at "+refRTTText+" (speed_mbps × 10⁶ / 8 × 0.1 s)", rmax >= bdp))

	c = in.calc()
	avail := c.str("net.sysctl.ipv4.tcp_available_congestion_control")
	out = append(out, c.fact("derived.net.tcp.bbr_available", "bbr is listed in tcp_available_congestion_control (a module not yet loaded is not listed)",
		slices.Contains(strings.Fields(avail), "bbr")))

	ps := func(c *calc) int64 {
		p := c.int("kernel.page_size_bytes")
		if !c.failed() && p <= 0 {
			c.fail("kernel.page_size_bytes is %d, not a page size", p)
		}
		return p
	}
	c = in.calc()
	used := c.int("net.sockstat.tcp_mem")
	p := ps(c)
	out = append(out, c.fact("derived.net.tcp.mem_used_bytes", "sockstat TCP mem (pages) × kernel.page_size_bytes", used*p))

	c = in.calc()
	pressure := c.int("net.sysctl.ipv4.tcp_mem.pressure")
	p = ps(c)
	out = append(out, c.fact("derived.net.tcp.mem_pressure_bytes", "tcp_mem pressure (pages) × kernel.page_size_bytes", pressure*p))
	return out
}

func softnetFacts(in inputs) []Fact {
	var out []Fact
	for _, f := range []struct{ id, col string }{
		{"derived.net.softnet.dropped", "dropped"},
		{"derived.net.softnet.time_squeeze", "time_squeeze"},
	} {
		c := in.calc()
		t := c.table("net.softnet")
		var sum int64
		for _, r := range t {
			n, _ := obs.Int(r[f.col])
			sum += n
		}
		rule := "sum of the " + f.col + " column of net.softnet"
		if !c.failed() {
			rule += fmt.Sprintf(" over %d CPUs", len(t))
		}
		out = append(out, c.fact(f.id, rule, sum))
	}
	return out
}
