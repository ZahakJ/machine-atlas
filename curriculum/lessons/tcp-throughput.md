---
{
  "id": "tcp-throughput",
  "title": "How fast can one connection go?",
  "subsystem": "networking",
  "kind": "core",
  "order": 4,
  "summary": "Why a TCP connection's speed is a window divided by a round trip: the bandwidth-delay product, receive and send buffer autotuning and their ceilings, TCP's machine-wide memory limits, and congestion control from slow start to CUBIC and BBR, with this machine's settings and retransmission counters.",
  "prerequisites": ["sockets-and-connections"],
  "uses": [
    "derived.net.default_route_bdp_100ms_bytes",
    "derived.net.default_route_speed_mbps",
    "derived.net.tcp.bbr_available",
    "derived.net.tcp.mem_pressure_bytes",
    "derived.net.tcp.mem_used_bytes",
    "derived.net.tcp.retrans_fraction",
    "derived.net.tcp.rmem_covers_bdp",
    "derived.net.tcp.rmem_max_mbps_at_100ms",
    "kernel.page_size_bytes",
    "net.netstat.TcpExt.TCPFastRetrans",
    "net.netstat.TcpExt.TCPLossProbes",
    "net.netstat.TcpExt.TCPTimeouts",
    "net.snmp.Tcp.OutSegs",
    "net.snmp.Tcp.RetransSegs",
    "net.sockstat.tcp_mem",
    "net.sysctl.core.rmem_max",
    "net.sysctl.core.wmem_max",
    "net.sysctl.ipv4.tcp_allowed_congestion_control",
    "net.sysctl.ipv4.tcp_available_congestion_control",
    "net.sysctl.ipv4.tcp_congestion_control",
    "net.sysctl.ipv4.tcp_ecn",
    "net.sysctl.ipv4.tcp_mem.high",
    "net.sysctl.ipv4.tcp_mem.low",
    "net.sysctl.ipv4.tcp_mem.pressure",
    "net.sysctl.ipv4.tcp_rmem.default",
    "net.sysctl.ipv4.tcp_rmem.max",
    "net.sysctl.ipv4.tcp_rmem.min",
    "net.sysctl.ipv4.tcp_slow_start_after_idle",
    "net.sysctl.ipv4.tcp_wmem.default",
    "net.sysctl.ipv4.tcp_wmem.max"
  ],
  "relevance": [
    {"when": "net.sysctl.ipv4.tcp_congestion_control", "is": "bbr", "note": "This machine uses BBR congestion control, which paces its sending from a model of the path instead of reacting to loss."},
    {"when": "derived.net.tcp.rmem_covers_bdp", "is": false, "note": "One TCP connection's receive buffer here cannot fill the default route's link over a 100 ms round trip."}
  ],
  "next": ["packet-queues"],
  "deeper": ["planned:firewalls-and-nat"],
  "version_notes": [
    {"applies": "Linux ≥ 2.6.19", "text": "CUBIC is the default congestion control; before, BIC."},
    {"applies": "Linux ≥ 3.0", "text": "The initial congestion window is 10 segments (RFC 6928), so a new connection may send about 14 KiB before the first acknowledgement."},
    {"applies": "Linux ≥ 4.9", "text": "BBR congestion control (tcp_bbr). Since 4.13 TCP paces itself when the qdisc does not, so BBR no longer requires the fq qdisc."},
    {"applies": "Linux ≥ 6.6", "text": "The receive window is computed from the measured ratio of payload to buffer memory (scaling_ratio) instead of the fixed tcp_adv_win_scale, so more of tcp_rmem becomes usable window."}
  ],
  "references": [
    {"title": "tcp(7): tcp_rmem, tcp_wmem, tcp_mem, tcp_congestion_control, tcp_slow_start_after_idle", "url": "https://man7.org/linux/man-pages/man7/tcp.7.html", "kind": "man-page"},
    {"title": "socket(7): SO_RCVBUF and SO_SNDBUF", "url": "https://man7.org/linux/man-pages/man7/socket.7.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: IP sysctl (TCP variables)", "url": "https://docs.kernel.org/networking/ip-sysctl.html", "kind": "kernel-doc"},
    {"title": "RFC 9293: Transmission Control Protocol (TCP)", "url": "https://www.rfc-editor.org/rfc/rfc9293", "kind": "spec"},
    {"title": "RFC 5681: TCP Congestion Control", "url": "https://www.rfc-editor.org/rfc/rfc5681", "kind": "spec"},
    {"title": "RFC 9438: CUBIC for Fast and Long-Distance Networks", "url": "https://www.rfc-editor.org/rfc/rfc9438", "kind": "spec"},
    {"title": "RFC 7323: TCP Extensions for High Performance (window scaling)", "url": "https://www.rfc-editor.org/rfc/rfc7323", "kind": "spec"},
    {"title": "RFC 6928: Increasing TCP's Initial Window", "url": "https://www.rfc-editor.org/rfc/rfc6928", "kind": "spec"},
    {"title": "RFC 8985: The RACK-TLP Loss Detection Algorithm for TCP", "url": "https://www.rfc-editor.org/rfc/rfc8985", "kind": "spec"},
    {"title": "Cardwell, Cheng, Gunn, Hassas Yeganeh, Jacobson: BBR: Congestion-Based Congestion Control, ACM Queue 14(5), 2016", "url": "https://queue.acm.org/detail.cfm?id=3022184", "kind": "paper"},
    {"title": "Kernel source: net/ipv4/tcp_cubic.c and tcp_bbr.c", "url": "https://github.com/torvalds/linux/tree/master/net/ipv4", "kind": "source"},
    {"title": "LWN: BBR congestion control (2016)", "url": "https://lwn.net/Articles/701165/", "kind": "lwn"}
  ]
}
---
## Question

A TCP connection may have at most a *window* of data in flight: sent, but not yet acknowledged. On this machine, autotuning may grow a connection's receive buffer to {{obs:net.sysctl.ipv4.tcp_rmem.max}}. If the server is 100 ms away, even a window that large lets one connection receive at no more than about {{derived:derived.net.tcp.rmem_max_mbps_at_100ms}} megabits per second, whatever the links on the way could carry. Where does that ceiling come from, what keeps a connection below it, and why does a new connection start slowly?

## Why it exists

A sender cannot know how fast it may send. The receiver may be a slow phone, the path may cross a congested Wi-Fi link, and thousands of other connections share the same routers. Sending too slowly wastes the link; sending too fast fills the queues of the slowest router on the path until it drops packets, which then have to be sent again, and every other connection through it suffers.

TCP answers with two limits on how much may be in flight. The **receive window**, which the receiver advertises, protects the receiver: it never offers more than it has buffer for. The **congestion window**, which the sender computes, protects the network: it grows while acknowledgements arrive and shrinks when loss or delay says the path is full. Neither is a rate. Both are amounts of data, and data takes one round trip to be acknowledged, so the speed of a connection is at most a window divided by the round-trip time.

## How it works

### The bandwidth-delay product

To keep a link busy, the sender must have as much data in flight as the link carries during one round trip: the *bandwidth-delay product* (BDP). A 1 Gbit/s path with a 100 ms round trip needs 12.5 MB in flight; the same link to a server 1 ms away needs 125 KB. If the window is smaller than the BDP, the sender stops and waits for acknowledgements every round trip, and throughput is window ÷ RTT. That is why a download from a distant server is slower than one from a nearby server over the same links, and why the 16-bit window field of the original TCP (64 KiB) was extended by *window scaling* (RFC 7323), negotiated in the handshake, up to 1 GiB.

### Buffers and autotuning

The receive window comes from the receive buffer: the kernel advertises the free space it has, minus a share reserved for its own per-packet bookkeeping. Each socket starts with the middle value of `tcp_rmem`. While the connection runs, the kernel measures how many bytes the program reads per round trip and grows the buffer to about twice that, up to the third value of `tcp_rmem`. The send buffer, which holds data not yet acknowledged, grows the same way up to the third value of `tcp_wmem`. A connection to a nearby server never needs much; one across an ocean grows to the ceiling.

A program can set its own buffer size with `setsockopt(SO_RCVBUF)`. That turns autotuning off for the socket and is capped by `net.core.rmem_max`, which is why setting it by hand often makes things slower, not faster.

All TCP buffers on the machine together are limited by `tcp_mem`, three values in pages: below the first, no limit; above the second, *memory pressure*: buffers stop growing and are trimmed; at the third, new data is refused. This keeps a machine with a million connections from spending all its memory on socket buffers.

### Congestion control

A new connection starts with a congestion window of 10 segments and doubles it every round trip while every segment is acknowledged: *slow start*, slow only in its first step. Slow start ends at a threshold, or at the first sign of loss.

Loss is detected two ways. Acknowledgements that keep reporting a gap (duplicate ACKs, selective acknowledgements, and RACK's timing) let the sender retransmit just the missing segment and reduce its window: *fast retransmit*. When nothing comes back at all, the retransmission timer fires, at least 200 ms after the segment was sent: the sender retransmits from the oldest unacknowledged segment and starts again from a small window. A *tail loss probe* sent after a short quiet period avoids most of those timeouts.

How the window grows and shrinks between losses is the congestion control algorithm, selected per connection:

- **CUBIC** (the default since 2.6.19, RFC 9438) cuts the window to 70% at a loss. It then grows it as a cubic function of the time since that loss: quickly towards the size at which the loss happened, slowly near it, then faster again beyond it, probing for more. Its growth depends on time, not on the round trip, so connections with long and short round trips share a link more evenly than the older Reno did. Loss is its signal that the path is full, so it keeps the bottleneck's queue full until a packet is dropped.
- **BBR** (Linux 4.9 and later) does not wait for loss. It estimates the path's bottleneck bandwidth (the highest delivery rate seen over recent round trips) and its minimum round trip (the lowest seen over about 10 seconds), sends at that rate, *paced* evenly instead of in bursts, and keeps about two BDPs in flight. Every few round trips it probes for more bandwidth, then drains whatever queue that probe built. It keeps queues short and tolerates random loss, at the cost of being less fair to CUBIC flows in some conditions.

After an idle period longer than the retransmission timeout, a connection by default restarts from the initial window (`tcp_slow_start_after_idle`), because the path may have changed while it was silent.

## On this machine

:::evidence Buffer ceilings
- net.sysctl.ipv4.tcp_rmem.min
- net.sysctl.ipv4.tcp_rmem.default
- net.sysctl.ipv4.tcp_rmem.max
- net.sysctl.ipv4.tcp_wmem.default
- net.sysctl.ipv4.tcp_wmem.max
- net.sysctl.core.rmem_max | Ceiling for buffers a program sets itself
- net.sysctl.core.wmem_max
- derived.net.tcp.rmem_max_mbps_at_100ms | Receive ceiling at 100 ms round trip, Mbit/s
:::end

A connection here starts with a receive buffer of {{obs:net.sysctl.ipv4.tcp_rmem.default}} and a send buffer of {{obs:net.sysctl.ipv4.tcp_wmem.default}}, and autotuning may grow them to {{obs:net.sysctl.ipv4.tcp_rmem.max}} and {{obs:net.sysctl.ipv4.tcp_wmem.max}}.

:::when net.sysctl.core.rmem_max
A program that sets its own receive buffer is limited to {{obs:net.sysctl.core.rmem_max}} instead, which the kernel then doubles to cover its bookkeeping, and loses autotuning.
:::else
A program that sets its own receive buffer is limited to {{obs:net.sysctl.core.rmem_max}} instead, and loses autotuning.
:::unknown
The ceiling for buffers that programs set themselves, `net.core.rmem_max`, is not readable here ({{obs:net.sysctl.core.rmem_max}}). It is a machine-wide setting that exists only in the host's initial network namespace, so inside a container the host's value applies without being visible.
:::end

:::when derived.net.tcp.rmem_covers_bdp
The default route's link runs at {{derived:derived.net.default_route_speed_mbps}} Mbit/s, so filling it across a 100 ms round trip takes {{derived:derived.net.default_route_bdp_100ms_bytes}} in flight. The receive ceiling is at least that, so one connection here could in principle receive at the link's full rate from a distant server; in practice the congestion window, the sender's buffer or a slower link on the path decide first.
:::else
The default route's link runs at {{derived:derived.net.default_route_speed_mbps}} Mbit/s, so filling it across a 100 ms round trip takes {{derived:derived.net.default_route_bdp_100ms_bytes}} in flight, more than the receive ceiling of {{obs:net.sysctl.ipv4.tcp_rmem.max}}. One connection from a distant server cannot fill this link: it is limited to about {{derived:derived.net.tcp.rmem_max_mbps_at_100ms}} Mbit/s by the window alone, which is why downloaders open several connections in parallel. From a server 10 ms away the same window allows ten times as much.
:::unknown
Whether the receive ceiling covers this machine's link cannot be computed here ({{derived:derived.net.tcp.rmem_covers_bdp}}): the default route's link speed is unknown. Wireless and most virtual NICs report none, and where the routing table or the interface list is unreadable the link itself is unknown; without a speed there is no bandwidth-delay product to compare with. The ceiling alone allows about {{derived:derived.net.tcp.rmem_max_mbps_at_100ms}} Mbit/s per connection at a 100 ms round trip.
:::end

### Memory for all connections

:::evidence TCP memory, machine-wide
- net.sysctl.ipv4.tcp_mem.low
- net.sysctl.ipv4.tcp_mem.pressure
- net.sysctl.ipv4.tcp_mem.high
- net.sockstat.tcp_mem | Pages in TCP buffers now
- derived.net.tcp.mem_used_bytes
- derived.net.tcp.mem_pressure_bytes
- kernel.page_size_bytes
:::end

:::when derived.net.tcp.mem_pressure_bytes
All TCP sockets on this machine together hold {{derived:derived.net.tcp.mem_used_bytes}} of buffers now. Memory pressure would begin at {{derived:derived.net.tcp.mem_pressure_bytes}} (tcp_mem's middle value, {{obs:net.sysctl.ipv4.tcp_mem.pressure}}). The kernel sizes these limits from RAM at boot, to a few percent of it.
:::unknown
`tcp_mem` is not readable here ({{obs:net.sysctl.ipv4.tcp_mem.pressure}}). It is one limit for the whole machine, and a container's network namespace does not have the file: the host's value applies to the container's sockets too. TCP buffers here use {{derived:derived.net.tcp.mem_used_bytes}} right now, counted over every namespace.
:::end

### Congestion control here

:::evidence Congestion control
- net.sysctl.ipv4.tcp_congestion_control | Algorithm for new connections
- net.sysctl.ipv4.tcp_available_congestion_control
- net.sysctl.ipv4.tcp_allowed_congestion_control | Choosable without privileges
- derived.net.tcp.bbr_available
- net.sysctl.ipv4.tcp_slow_start_after_idle
- net.sysctl.ipv4.tcp_ecn | ECN (0 off, 1 request it, 2 accept when asked)
:::end

:::when net.sysctl.ipv4.tcp_congestion_control is bbr
New connections here use BBR. They pace their sending at the rate BBR estimates for the path, and do not treat each lost packet as a sign of congestion. With a qdisc that paces (fq), the pacing happens there; otherwise TCP paces itself with a timer per socket.
:::else
New connections here use {{obs:net.sysctl.ipv4.tcp_congestion_control}}, which reduces its window when packets are lost and grows it back between losses. The algorithms loaded are {{obs:net.sysctl.ipv4.tcp_available_congestion_control}}; a program may switch its own sockets to any of {{obs:net.sysctl.ipv4.tcp_allowed_congestion_control}} with the TCP_CONGESTION socket option.
:::unknown
Which congestion control new connections use could not be read here ({{obs:net.sysctl.ipv4.tcp_congestion_control}}).
:::end

### Loss and retransmission since boot

:::evidence Retransmissions
- net.snmp.Tcp.OutSegs
- net.snmp.Tcp.RetransSegs
- derived.net.tcp.retrans_fraction
- net.netstat.TcpExt.TCPFastRetrans | Repaired from acknowledgements
- net.netstat.TcpExt.TCPLossProbes
- net.netstat.TcpExt.TCPTimeouts | Retransmission timer expiries
:::end

:::when derived.net.tcp.retrans_fraction
Since boot, {{derived:derived.net.tcp.retrans_fraction}} of the TCP segments this machine sent were retransmissions. Acknowledgements triggered {{obs:net.netstat.TcpExt.TCPFastRetrans}} fast retransmits, {{obs:net.netstat.TcpExt.TCPLossProbes}} tail loss probes were sent, and {{obs:net.netstat.TcpExt.TCPTimeouts}} times the retransmission timer expired, each time stalling a connection for at least 200 ms. A fraction well below one percent is typical for wired links, and a few percent for Wi-Fi and mobile paths.
:::else
No TCP segment has been retransmitted since boot.
:::unknown
The retransmission ratio cannot be computed here ({{derived:derived.net.tcp.retrans_fraction}}).
:::end

## Predict

:::predict
This machine downloads a large file over one TCP connection from a server 100 ms away. The bottleneck link on the path is upgraded to twice its speed; nothing else changes and nothing is lost. How does the download speed change?

- It doubles: the bottleneck is now twice as fast.
- It may not change at all, if the connection was already limited by its receive window rather than by the link.
- It halves, because the congestion window has to start again.
:::reveal
Either of the first two, depending on which limit was reached first, and on long paths it is often the second. Without loss, the congestion window grows until the receive window caps it. With the window at this machine's ceiling of {{obs:net.sysctl.ipv4.tcp_rmem.max}} (somewhat less, after the kernel's bookkeeping share), one connection receives at most about {{derived:derived.net.tcp.rmem_max_mbps_at_100ms}} Mbit/s at 100 ms. If the old link was already faster than that, doubling it changes nothing; if it was slower, the download now speeds up until the window becomes the limit.

The third would happen only if the connection had sat idle across the upgrade with `tcp_slow_start_after_idle` at 1 (here {{obs:net.sysctl.ipv4.tcp_slow_start_after_idle}}), which a running download does not.
:::end

## Try it

```sh
ss -tin state established       # per connection: cwnd, rtt, rcv_space, pacing_rate, retrans
sysctl net.ipv4.tcp_rmem net.ipv4.tcp_wmem net.ipv4.tcp_congestion_control
nstat -az TcpRetransSegs TcpOutSegs TcpExtTCPTimeouts
```

During a large download, `ss -tin` shows the connection's round trip (`rtt`), congestion window in segments (`cwnd`), the receive buffer autotuning chose (`rcv_space`) and, with BBR, its estimate of the bottleneck bandwidth. Multiply `cwnd` by the MSS (`mss`) and divide by `rtt` to see the rate the congestion window allows. `ss` shows other users' connections without their owners unless run as root.

## Limits

:::misconception Bigger buffers always make TCP faster
Only a connection whose window is limited by the buffer gains from a larger one. Beyond the bandwidth-delay product, extra buffer only lets more data sit in queues, adding latency, and setting SO_RCVBUF by hand disables the autotuning that would have grown it anyway.
:::end

:::misconception Packet loss means the network is broken
For CUBIC and its predecessors, loss is the signal that tells the sender it has found the path's capacity; a long transfer through a full bottleneck loses a few packets by design. Persistent timeouts, not a low rate of fast retransmits, indicate a problem.
:::end

:::misconception The link speed is the connection speed
A 1 Gbit/s Ethernet port is only the first hop. The slowest link on the path, the round trip, both ends' buffers and the congestion window decide the rate of one connection.
:::end

:::cannot
The round trip, window or rate of any particular connection: those are per-socket values (`ss -tin`) that Machine Atlas does not collect, because they belong to other processes' connections.

Where loss happens. Retransmission counters say that segments were lost or presumed lost, not on which link, nor whether they were lost or only delayed.

The bandwidth-delay products in this lesson assume a 100 ms round trip and that the default route's link speed is available end to end; real paths are usually slower and nearer.

Whether the peer's buffers or congestion control allow as much: a connection is limited by both ends.
:::end

## Go deeper

Next, [where packets wait](lesson:packet-queues) looks at the queues between TCP and the wire, where congestion control's decisions turn into latency or loss, and why BBR is often paired with the fq queueing discipline. In Explore, [TCP](component:tcp) and [sockets](component:socket-layer) connect this lesson to the rest of the stack. TCP's buffers are ordinary kernel memory: the [memory pressure and OOM](lesson:memory-pressure-and-oom) lesson describes what happens to them, and everything else, when memory runs short.
