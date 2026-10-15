---
{
  "id": "packet-path",
  "title": "What happens between send() and the wire?",
  "subsystem": "networking",
  "kind": "core",
  "order": 2,
  "summary": "The path of a packet through the kernel: the socket, TCP or UDP, the routing decision, netfilter and connection tracking, the neighbour table, the queueing discipline and the driver's ring on the way out; the NIC's ring, NAPI polling in the receive softirq and the per-CPU backlog on the way in.",
  "prerequisites": ["network-interfaces"],
  "uses": [
    "derived.net.conntrack.fill_fraction",
    "derived.net.default_route",
    "derived.net.default_route_interface",
    "derived.net.default_route_mtu",
    "derived.net.softnet.dropped",
    "derived.net.softnet.time_squeeze",
    "net.conntrack.count",
    "net.conntrack.max",
    "net.routes.ipv4",
    "net.snmp.Ip.ForwDatagrams",
    "net.snmp.Ip.InReceives",
    "net.snmp.Ip.OutRequests",
    "net.softnet",
    "net.sysctl.core.netdev_budget",
    "net.sysctl.core.netdev_max_backlog",
    "net.sysctl.ipv4.ip_forward"
  ],
  "relevance": [
    {"when": "net.sysctl.ipv4.ip_forward", "is": 1, "note": "This kernel forwards packets between interfaces, so some packets here take the forwarding path, not the local one."},
    {"when": "derived.net.softnet.dropped", "note": "Some CPU's receive backlog has overflowed since boot, dropping packets."}
  ],
  "next": ["sockets-and-connections"],
  "deeper": ["planned:firewalls-and-nat", "planned:namespaces-and-isolation"],
  "version_notes": [
    {"applies": "Linux ≥ 2.6", "text": "NAPI: drivers poll their receive rings from a softirq while packets keep arriving, instead of taking one interrupt per packet."},
    {"applies": "Linux ≥ 3.6", "text": "The IPv4 routing cache was removed; each lookup walks the FIB trie, and sockets cache the result (the dst entry) instead."},
    {"applies": "Linux ≥ 5.10", "text": "/proc/net/softnet_stat prints each CPU's number and backlog length; before, a line per online CPU with no number, so offline CPUs shift the rows."},
    {"applies": "Linux ≥ 5.12", "text": "Threaded NAPI (/sys/class/net/<if>/threaded) can run a device's polling in a kernel thread the scheduler sees, instead of in softirq context."}
  ],
  "references": [
    {"title": "Linux kernel documentation: NAPI", "url": "https://docs.kernel.org/networking/napi.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Scaling in the Linux Networking Stack (RSS, RPS, RFS, XPS)", "url": "https://docs.kernel.org/networking/scaling.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Segmentation Offloads (TSO, GSO, GRO)", "url": "https://docs.kernel.org/networking/segmentation-offloads.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/net/core (netdev_max_backlog, netdev_budget)", "url": "https://docs.kernel.org/admin-guide/sysctl/net.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: IP sysctl (ip_forward)", "url": "https://docs.kernel.org/networking/ip-sysctl.html", "kind": "kernel-doc"},
    {"title": "ip-route(8)", "url": "https://man7.org/linux/man-pages/man8/ip-route.8.html", "kind": "man-page"},
    {"title": "ip(7)", "url": "https://man7.org/linux/man-pages/man7/ip.7.html", "kind": "man-page"},
    {"title": "Kernel source: net/core/dev.c (__dev_queue_xmit, net_rx_action, enqueue_to_backlog)", "url": "https://github.com/torvalds/linux/blob/master/net/core/dev.c", "kind": "source"},
    {"title": "Kernel source: net/ipv4/fib_trie.c (the IPv4 routing table)", "url": "https://github.com/torvalds/linux/blob/master/net/ipv4/fib_trie.c", "kind": "source"},
    {"title": "LWN: Generic receive offload (2008)", "url": "https://lwn.net/Articles/358910/", "kind": "lwn"},
    {"title": "LWN: The IPv4 routing cache removal (2012)", "url": "https://lwn.net/Articles/507651/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.net.default_route
A program on this machine writes a few kilobytes to a TCP connection with one `send()`. The call returns at once, and moments later the data leaves through {{derived:derived.net.default_route_interface}} as frames of at most {{derived:derived.net.default_route_mtu}} bytes. Between the call and the wire the kernel chose the interface, may have consulted a firewall, found the next hop's hardware address and queued the packets twice. Who decided each of those, and where can a packet be dropped on the way, in either direction?
:::else
A program writes a few kilobytes to a TCP connection with one `send()`. The call returns at once, and moments later the data leaves through some interface as frames no larger than its MTU. This machine has no default route ({{derived:derived.net.default_route}}), so the kernel can only reach networks it is directly attached to. Between the call and the wire the kernel chose the interface, may have consulted a firewall, found the next hop's hardware address and queued the packets twice. Who decided each of those, and where can a packet be dropped on the way?
:::unknown
A program writes a few kilobytes to a TCP connection with one `send()`. The call returns at once, and moments later the data leaves through some interface as frames no larger than its MTU. Between the call and the wire the kernel chose the interface, may have consulted a firewall, found the next hop's hardware address and queued the packets twice. Who decided each of those, and where can a packet be dropped on the way, in either direction? The routing tables that would show the first decision could not be read here ({{derived:derived.net.default_route}}).
:::end

## Why it exists

A program wants to say "send these bytes to that address" and nothing more. Between that wish and a voltage on a cable lie a dozen decisions: which interface and which next hop, whether the firewall allows it, what hardware address to put in the frame, when the link is free, how to cut a large write into frames. Each decision belongs to a different layer, and each layer must also work in the other direction, at rates of millions of packets per second, without one CPU interrupt per packet.

The kernel solves this with one object that travels down and up the stack, the *socket buffer* (`struct sk_buff`): the packet's data plus a descriptor that each layer fills in, adds a header to or strips one from. Each layer is a function call on that object; queues appear only where two sides run at different speeds.

## How it works

### Down: from send() to the driver

1. **Socket.** `send()` copies the data into the socket's send buffer and returns. If the buffer is full, a blocking call sleeps until acknowledgements free space. Nothing has been transmitted yet.
2. **TCP or UDP.** TCP decides how much it may send now (the receiver's window and its own congestion window allow it), numbers the bytes and builds segments. It builds them large: with segmentation offload a single segment can carry up to 64 KiB, cut into MTU-sized frames only at the end. UDP sends each datagram as it is.
3. **Routing.** The IP layer asks where the destination goes. The kernel looks the address up in the routing tables: the *longest matching prefix* wins, so a route for `192.168.1.0/24` beats the default route `0.0.0.0/0`, and among equal prefixes the lowest metric. The answer is an output interface and, if the destination is not on a directly attached network, a gateway. A connected socket caches it, so most packets of a connection skip the lookup.
4. **Netfilter.** At fixed hooks (output, then postrouting) the firewall's rules run. If connection tracking is loaded, every packet is matched against the table of known flows, which is what lets rules say "replies to connections I opened" and NAT rewrite addresses.
5. **Neighbour.** To build a frame the kernel needs the next hop's MAC address. The neighbour table caches them; on a miss the kernel asks with ARP (IPv4) or neighbour solicitation (IPv6) and holds the packet until the answer comes.
6. **Queueing discipline.** `dev_queue_xmit()` places the packet in the interface's qdisc, which decides the order in which waiting packets leave. On a multiqueue NIC each transmit queue has its own qdisc and the sending CPU picks a queue (XPS). Devices with no queue of their own (loopback, veth, bridges) pass packets straight on.
7. **Driver and NIC.** The driver writes a descriptor pointing at the packet into the NIC's transmit ring. The NIC reads the data by DMA, cuts large segments into frames if it offers TSO (otherwise software did, just before the driver), computes checksums and transmits. When it is done it signals completion, and the driver frees the buffers, which frees space in the socket's send buffer.

### Up: from the NIC to recv()

1. **Ring and interrupt.** The driver keeps the NIC's receive ring filled with empty buffers. The NIC writes arriving frames into them by DMA and raises an interrupt. A multiqueue NIC hashes each flow to one of its queues (RSS), each with its own interrupt and usually its own CPU.
2. **NAPI.** The interrupt handler does almost nothing: it masks the device's interrupt and schedules a poll. The network receive *softirq* (`NET_RX`) then calls the driver's poll function, which takes packets off the ring in batches. One run of the softirq may process at most `netdev_budget` packets, and stops after about 2 ms. If work is left, it counts a *time squeeze* and continues later, in the next softirq run or in the `ksoftirqd` thread. When the ring is empty, the driver unmasks the interrupt again. Under load, the CPU polls without interrupts; when idle, the first packet still gets an interrupt.
3. **GRO.** Consecutive segments of one TCP flow are merged into one large packet before going up, the mirror of segmentation offload: the stack above does its per-packet work once per merged packet.
4. **Backlog.** Normally the packet goes up on the CPU that polled it. When Receive Packet Steering (RPS) moves it to another CPU, or a device has no NAPI (loopback, some virtual devices), it waits in that CPU's input backlog. A backlog longer than `netdev_max_backlog` drops the packet, and `/proc/net/softnet_stat` counts it.
5. **IP and routing again.** The receive path makes its own routing decision: is the destination one of this machine's addresses (deliver locally), or another (forward it, if `ip_forward` allows, otherwise drop)? Netfilter's prerouting and input (or forward) hooks run around this decision.
6. **TCP and socket.** TCP checks the segment against the connection, acknowledges it, and appends the data to the socket's receive queue. The reading program is woken and `recv()` copies the data out.

Drops can happen at every step: a full transmit ring or qdisc, a firewall rule, a neighbour that never answers, a full receive ring (counted by the NIC), a full backlog (softnet_stat), a full socket receive buffer (UDP's `RcvbufErrors`, TCP's pruning).

## On this machine

:::table net.routes.ipv4 columns=interface,destination,prefix_len,destination_class,gateway,via_gateway,metric,default

:::evidence The routing decision
- derived.net.default_route
- derived.net.default_route_interface
- net.sysctl.ipv4.ip_forward | IPv4 forwarding (1: routes between interfaces)
- net.snmp.Ip.OutRequests | IPv4 packets sent by local programs since boot
- net.snmp.Ip.InReceives | IPv4 packets received since boot
- net.snmp.Ip.ForwDatagrams | IPv4 packets forwarded since boot
:::end

:::when derived.net.default_route
Every destination that no more specific route covers goes to {{derived:derived.net.default_route_interface}}. The other rows are directly connected networks, created when an address was configured on an interface: packets to them go straight to the destination's own MAC address, with no gateway.
:::else
There is no default route here, so packets to any address outside the directly connected networks fail at the routing step with "Network is unreachable", before anything is queued.
:::unknown
Whether there is a default route could not be determined here ({{derived:derived.net.default_route}}). The routing tables under `/proc/net` are unreadable, so neither the default route nor the connected networks can be shown.
:::end

`/proc/net/route` shows only the *main* table. Policy routing rules can send some traffic, such as a VPN's, through another table this file does not list; `ip rule` and `ip route show table all` show them.

:::when net.sysctl.ipv4.ip_forward is 1
This kernel forwards IPv4 packets between its interfaces (`ip_forward` is {{obs:net.sysctl.ipv4.ip_forward}}), so it acts as a router: container runtimes and VPN servers turn this on so that packets from containers or clients reach the outside world. It has forwarded {{obs:net.snmp.Ip.ForwDatagrams}} packets since boot.
:::else
This kernel does not forward packets between interfaces (`ip_forward` is {{obs:net.sysctl.ipv4.ip_forward}}): a packet arriving for an address that is not this machine's is dropped at the receive path's routing decision.
:::unknown
Whether this kernel forwards packets could not be read ({{obs:net.sysctl.ipv4.ip_forward}}).
:::end

### Connection tracking

:::when net.conntrack.count
Connection tracking is loaded and follows {{obs:net.conntrack.count}} flows, {{derived:derived.net.conntrack.fill_fraction}} of its limit of {{obs:net.conntrack.max}}. Every packet in either direction is looked up in this table. When it is full, packets of new flows are dropped, whatever the firewall rules say.
:::else
Connection tracking is loaded, but no flow is being tracked right now.
:::unknown
The connection tracking count is {{obs:net.conntrack.count}}. When the file is absent, nf_conntrack is not loaded: no firewall or NAT rule in this network namespace needs it, and packets pass the netfilter hooks without being looked up in a flow table.
:::end

### Receive processing per CPU

:::table net.softnet columns=cpu,processed,dropped,time_squeeze,received_rps limit=16

:::evidence Receive softirq
- derived.net.softnet.dropped | Packets dropped from a full backlog, all CPUs
- derived.net.softnet.time_squeeze | Softirq runs that ran out of budget or time
- net.sysctl.core.netdev_max_backlog | Backlog limit per CPU, in packets
- net.sysctl.core.netdev_budget | Packets per softirq run
:::end

The `processed` column shows where receive work lands. A single-queue NIC without RPS puts it all on the CPU that takes the interrupt; a multiqueue NIC spreads it over the CPUs its queues' interrupts are bound to.

:::when derived.net.softnet.dropped
Some backlog has overflowed: {{derived:derived.net.softnet.dropped}} packets were dropped since boot because a CPU's input queue held {{obs:net.sysctl.core.netdev_max_backlog}} packets already. Those drops happen after the NIC delivered the packets, so the NIC's own counters do not show them.
:::else
No packet has been dropped from a full backlog since boot. The softirq ran out of budget or time {{derived:derived.net.softnet.time_squeeze}} times; occasional squeezes are normal, and only a count that keeps rising under steady traffic means receive processing cannot keep up.
:::unknown
Whether packets were dropped from a full backlog could not be read here ({{derived:derived.net.softnet.dropped}}).
:::end

## See it

:::diagram packet-path

The diagram draws both directions with this machine's default route, MTU, queueing discipline, transmit queues and receive backlog where they could be read.

## Predict

:::predict
A program sends 64 KiB with one `send()` on an established TCP connection over an interface with a 1500-byte MTU. About 45 frames leave the machine. How many times does the kernel run the routing lookup, netfilter and the qdisc for this data?

- About 45 times: once per frame.
- Once or a few times: TCP hands down large segments, and the routing decision is cached in the socket.
- Never for this data: the routing decision was made when the connection opened, and the NIC does the rest.
:::reveal
Once or a few times. TCP builds segments as large as segmentation offload allows, up to 64 KiB, so the 64 KiB can go down the stack as one or a few large packets. The route is cached in the socket since the connection was established, so there is no table lookup, but each packet still passes the netfilter hooks and the qdisc. The cut into about 45 frames of up to 1448 bytes of payload each happens at the bottom: in the NIC if it offers TSO, otherwise in software just before the driver (GSO). On receive, GRO glues them back together, so the receiver's stack also sees a few large packets.

The first answer is how the stack worked before offloads, and still how it works for UDP sending datagrams of MTU size. The third is half right: routing is cached, but netfilter and the qdisc see every packet that goes down, however large.
:::end

## Try it

```sh
ip route                           # the main table, as the kernel prints it
ip route get 192.0.2.1             # the decision for one destination: interface, gateway, source
ip neigh                           # the neighbour table: next hops and their MAC addresses
cat /proc/net/softnet_stat         # per CPU, hexadecimal: processed, dropped, squeezed, ...
ip -s link                         # per interface: bytes, packets, errors, drops
```

`ip route get` runs exactly the lookup described above without sending anything; try a local address, an address on a connected network and one far away, and watch the interface and gateway change. Run the `softnet_stat` command twice under a large download and subtract: the first column grows on the CPUs that handle the NIC's queues.

## Limits

:::misconception The routing table is searched for every packet
Established sockets cache their route, and since 3.6 the kernel has no global routing cache: lookups walk a compressed trie, fast enough to do for each new flow. Forwarded packets are looked up each time.
:::end

:::misconception A dropped packet shows up in the interface's drop counter
Drops happen in many places, each with its own counter: the NIC's ring (driver statistics, `ethtool -S`), the receive backlog (softnet_stat), the qdisc (`tc -s qdisc`), netfilter (rule counters), sockets (`RcvbufErrors`, TCP's prune counters). The interface's `rx_dropped` covers only some of them.
:::end

:::misconception Every packet costs one interrupt
With NAPI, interrupts are switched off while the softirq polls a busy ring, and GRO and offloads make one pass of the stack carry many frames. Packet rates in the millions per second are possible on one machine because of this.
:::end

:::cannot
The neighbour table, the policy routing rules and tables other than main: Machine Atlas does not read them.

Where packets of one particular connection are dropped. These counters are totals over all traffic since boot; following one flow needs tracing (`perf trace`, `dropwatch`, eBPF), which needs privileges.

Drops inside the NIC and its driver's rings: those counters are driver-specific (`ethtool -S`) and not collected.

The connection tracking table itself: `/proc/net/nf_conntrack` lists every tracked flow, with addresses and ports, and only root may read it.
:::end

## Go deeper

Next, [sockets and connections](lesson:sockets-and-connections) looks at the top of this path: what a socket is and the states a TCP connection passes through. [Where packets wait](lesson:packet-queues) returns to the qdisc and the driver ring in detail. In Explore, [routing tables](component:routing-table), [connection tracking](component:netfilter-conntrack) and [receive processing](component:napi-softirq) connect these steps. Firewalls and address translation, the rules that run at the netfilter hooks, are a planned lesson.
