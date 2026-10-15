---
{
  "id": "sockets-and-connections",
  "title": "What is a connection, to the kernel?",
  "subsystem": "networking",
  "kind": "core",
  "order": 3,
  "summary": "Sockets as the kernel keeps them, the TCP state machine from the handshake to TIME_WAIT, the SYN and accept queues of a listening socket and how they overflow, ephemeral ports, and what this machine's socket tables and counters show.",
  "prerequisites": ["packet-path"],
  "uses": [
    "derived.net.ephemeral_ports",
    "kernel.uptime_seconds",
    "net.netstat.TcpExt.ListenDrops",
    "net.netstat.TcpExt.ListenOverflows",
    "net.netstat.TcpExt.SyncookiesSent",
    "net.netstat.TcpExt.TW",
    "net.snmp.Tcp.ActiveOpens",
    "net.snmp.Tcp.AttemptFails",
    "net.snmp.Tcp.CurrEstab",
    "net.snmp.Tcp.EstabResets",
    "net.snmp.Tcp.OutRsts",
    "net.snmp.Tcp.PassiveOpens",
    "net.sockets.tcp_states",
    "net.sockets.udp_ipv4",
    "net.sockets.udp_ipv6",
    "net.sockets.unix",
    "net.sockstat.sockets_used",
    "net.sockstat.tcp_inuse",
    "net.sockstat.tcp_orphan",
    "net.sockstat.tcp_tw",
    "net.sockstat6.tcp6_inuse",
    "net.sysctl.core.somaxconn",
    "net.sysctl.ipv4.ip_local_port_range.high",
    "net.sysctl.ipv4.ip_local_port_range.low",
    "net.sysctl.ipv4.tcp_fin_timeout",
    "net.sysctl.ipv4.tcp_max_syn_backlog",
    "net.sysctl.ipv4.tcp_syncookies",
    "net.sysctl.ipv4.tcp_tw_reuse"
  ],
  "relevance": [
    {"when": "net.netstat.TcpExt.ListenOverflows", "note": "A listening socket's accept queue has overflowed here since boot, so connections were dropped or delayed."}
  ],
  "next": ["tcp-throughput"],
  "deeper": ["planned:name-resolution", "planned:firewalls-and-nat"],
  "version_notes": [
    {"applies": "Linux ≥ 4.4", "text": "Connection requests are small request sockets in the listener's hash table (TCP_NEW_SYN_RECV) rather than entries in a per-listener list, so a SYN flood no longer contends on one lock; /proc/net/tcp still shows them as SYN_RECV."},
    {"applies": "Linux ≥ 4.19", "text": "net.ipv4.tcp_tw_reuse defaults to 2: TIME_WAIT reuse for outgoing connections on loopback only."},
    {"applies": "Linux ≥ 5.4", "text": "net.core.somaxconn defaults to 4096; before, 128, which silently capped the backlog of busy servers."}
  ],
  "references": [
    {"title": "RFC 9293: Transmission Control Protocol (TCP), section 3.3.2 State Machine Overview", "url": "https://www.rfc-editor.org/rfc/rfc9293", "kind": "spec"},
    {"title": "tcp(7)", "url": "https://man7.org/linux/man-pages/man7/tcp.7.html", "kind": "man-page"},
    {"title": "socket(7)", "url": "https://man7.org/linux/man-pages/man7/socket.7.html", "kind": "man-page"},
    {"title": "listen(2)", "url": "https://man7.org/linux/man-pages/man2/listen.2.html", "kind": "man-page"},
    {"title": "ss(8)", "url": "https://man7.org/linux/man-pages/man8/ss.8.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: The /proc filesystem, /proc/net (tcp, sockstat, snmp)", "url": "https://docs.kernel.org/filesystems/proc.html#networking-info-in-proc-net", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: IP sysctl (tcp_max_syn_backlog, tcp_syncookies, tcp_fin_timeout, tcp_tw_reuse, ip_local_port_range)", "url": "https://docs.kernel.org/networking/ip-sysctl.html", "kind": "kernel-doc"},
    {"title": "Kernel source: include/net/tcp_states.h", "url": "https://github.com/torvalds/linux/blob/master/include/net/tcp_states.h", "kind": "source"},
    {"title": "Kernel source: net/ipv4/tcp_minisocks.c (TIME_WAIT)", "url": "https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_minisocks.c", "kind": "source"},
    {"title": "LWN: SYN cookies ported to IPv6 (2013), and how cookies work", "url": "https://lwn.net/Articles/548075/", "kind": "lwn"}
  ]
}
---
## Question

:::when net.sockets.tcp_states
At the moment Machine Atlas looked, this machine's network namespace had {{obs:net.snmp.Tcp.CurrEstab}} established TCP connections, and {{obs:net.sockstat.tcp_tw}} more in a state called TIME_WAIT: connections that were already closed, kept by the kernel although no program holds them. Since boot, programs here have opened {{obs:net.snmp.Tcp.ActiveOpens}} connections and accepted {{obs:net.snmp.Tcp.PassiveOpens}}. What does the kernel keep for each connection, why does it hold on to closed ones, and what happens when connections arrive faster than a program accepts them?
:::else
At the moment Machine Atlas looked, this machine's network namespace had {{obs:net.snmp.Tcp.CurrEstab}} established TCP connections, and {{obs:net.sockstat.tcp_tw}} more in TIME_WAIT: closed connections the kernel keeps although no program holds them. What does the kernel keep for each connection, why does it hold on to closed ones, and what happens when connections arrive faster than a program accepts them?
:::unknown
Every machine on a network keeps TCP connections in the kernel: open ones, connections still being set up, and closed ones in a state called TIME_WAIT that no program holds any more. The per-socket tables could not be read here ({{obs:net.sockets.tcp_states}}), but the protocol counters show {{obs:net.snmp.Tcp.CurrEstab}} established connections and {{obs:net.sockstat.tcp_tw}} in TIME_WAIT. What does the kernel keep for each connection, why does it hold on to closed ones, and what happens when connections arrive faster than a program accepts them?
:::end

## Why it exists

IP moves packets between addresses; it knows nothing about programs. Two programs talking over the network need more: an endpoint each, so that packets for this conversation reach this process and not another, and, for TCP, an agreement on where the byte stream starts, what has arrived and what must be sent again. Both ends must remember that agreement, and must also agree when it ends, even though either end's last packet can be lost.

The kernel keeps that state in a *socket*. To the program it is a file descriptor; to the kernel it is a structure with addresses and ports, buffers, timers, sequence numbers and a state. A connection is a pair of sockets, one per machine, whose states move in step through the TCP state machine.

## How it works

### Endpoints and the four-tuple

A TCP or UDP socket is identified by its local address and port and, once connected, the remote address and port. Arriving segments are matched to a socket by this *four-tuple* in a hash table. A server binds a well-known port and calls `listen()`; a client calls `connect()` without binding, and the kernel picks a free local port from the *ephemeral* range `ip_local_port_range`. The same local port can be used for connections to different remote endpoints, because the four-tuples still differ.

UDP sockets have no connection and no state machine: each datagram is matched by destination port and delivered whole, or dropped if the receive buffer is full. Unix domain sockets use the same API between processes on one machine, without IP at all.

### The handshake

1. The client sends SYN with its initial sequence number: CLOSED → SYN_SENT.
2. The listener's kernel answers SYN-ACK and records a small *request socket*: the connection is in SYN_RECV. The program is not involved.
3. The client acknowledges: both ends are ESTABLISHED. The server's kernel turns the request into a full socket and appends it to the listener's *accept queue*, where it waits until the program calls `accept()`.

So a listening socket has two queues. Half-open requests are bounded by `tcp_max_syn_backlog`, completed connections by the `backlog` argument of `listen()`, capped at `net.core.somaxconn`. When the accept queue is full, the kernel drops the client's final ACK (counted in ListenOverflows and ListenDrops); the client believes it is connected, retransmits, and its first request waits for a retry. When the SYN queue overflows, the kernel can answer with a *SYN cookie*: a sequence number that encodes the request, so no state is kept until the ACK comes back. This defeats SYN floods at the cost of some TCP options.

### Closing, and TIME_WAIT

Each direction is closed separately with a FIN, so closing takes four segments:

- The side that closes first (*active close*) goes FIN_WAIT1 → FIN_WAIT2 when its FIN is acknowledged, then waits for the peer's FIN, acknowledges it and enters **TIME_WAIT**.
- The other side (*passive close*) is in CLOSE_WAIT from the moment the peer's FIN arrives until its own program calls `close()`, then LAST_ACK until its FIN is acknowledged, then CLOSED.

TIME_WAIT exists for two reasons. The final ACK may be lost; the peer will then resend its FIN, and a TIME_WAIT socket can acknowledge it again instead of answering with a reset. And old duplicate segments of this connection may still be in the network; while the four-tuple is held, they cannot be mistaken for data of a new connection with the same addresses and ports. RFC 9293 asks for twice the maximum segment lifetime; Linux uses a fixed 60 seconds. The kernel keeps only a small time-wait record, not the full socket, so thousands cost little memory. Their real cost is the four-tuple: a client making many short connections to one server can run out of ephemeral ports.

A connection stuck in CLOSE_WAIT is almost always a program that never called `close()` on a connection its peer ended. FIN_WAIT2 after the program closed is limited by `tcp_fin_timeout`. A connection the program closed before the kernel finished sending is *orphaned*: the kernel completes it alone.

## On this machine

:::table net.sockets.tcp_states

:::evidence Sockets right now
- net.sockstat.sockets_used | Sockets of every kind in this namespace
- net.sockstat.tcp_inuse | IPv4 TCP sockets, TIME_WAIT excluded
- net.sockstat6.tcp6_inuse | IPv6 TCP sockets, TIME_WAIT excluded
- net.sockstat.tcp_tw
- net.sockstat.tcp_orphan
- net.sockets.udp_ipv4
- net.sockets.udp_ipv6
- net.sockets.unix
:::end

The table counts sockets from `/proc/net/tcp` and `/proc/net/tcp6` by their state column; Machine Atlas keeps nothing else from those files, which list every connection's addresses and ports. LISTEN rows are servers waiting for connections. A dual-stack server that listens on IPv6 also accepts IPv4 clients, whose connections then appear in the ipv6 column with IPv4-mapped addresses.

:::when net.sockets.unix
There are also {{obs:net.sockets.unix}} Unix domain sockets: the display server, D-Bus, the journal and other local services talk over them, never touching IP.
:::else
There are no Unix domain sockets in this namespace.
:::unknown
The Unix socket list could not be read here ({{obs:net.sockets.unix}}).
:::end

### Connections since boot

:::evidence TCP counters since boot
- kernel.uptime_seconds
- net.snmp.Tcp.ActiveOpens
- net.snmp.Tcp.PassiveOpens
- net.snmp.Tcp.AttemptFails
- net.snmp.Tcp.EstabResets
- net.snmp.Tcp.OutRsts
- net.netstat.TcpExt.TW | TIME_WAIT records that expired
:::end

Over {{obs:kernel.uptime_seconds}} of uptime, {{obs:net.snmp.Tcp.AttemptFails}} connection attempts failed during the handshake (refused or timed out) and {{obs:net.snmp.Tcp.EstabResets}} established connections ended with a reset instead of an orderly close. Resets are routine: browsers abort connections they no longer need, and servers reset idle clients.

### Listening sockets under load

:::evidence Accept and SYN queues
- net.sysctl.core.somaxconn | Ceiling on any listen() backlog
- net.sysctl.ipv4.tcp_max_syn_backlog | Half-open requests per listener
- net.sysctl.ipv4.tcp_syncookies | SYN cookies (0 never, 1 on overflow, 2 always)
- net.netstat.TcpExt.ListenOverflows
- net.netstat.TcpExt.ListenDrops
- net.netstat.TcpExt.SyncookiesSent
:::end

:::when net.netstat.TcpExt.ListenOverflows
Accept queues have overflowed {{obs:net.netstat.TcpExt.ListenOverflows}} times since boot: some server here accepted connections more slowly than they arrived, for a moment or for long, and each overflow delayed a client by at least one retransmission timeout, a second or more. Raising the program's `listen()` backlog helps only up to somaxconn, here {{obs:net.sysctl.core.somaxconn}}; a program that accepts too slowly overflows any queue eventually.
:::else
No accept queue has overflowed since boot ({{obs:net.netstat.TcpExt.ListenOverflows}}): every server here has kept up with its incoming connections. Each listener's queue is capped at somaxconn, here {{obs:net.sysctl.core.somaxconn}}.
:::unknown
Whether accept queues have overflowed could not be read ({{obs:net.netstat.TcpExt.ListenOverflows}}).
:::end

### Ephemeral ports and TIME_WAIT

:::evidence Outgoing connections
- net.sysctl.ipv4.ip_local_port_range.low
- net.sysctl.ipv4.ip_local_port_range.high
- derived.net.ephemeral_ports
- net.sysctl.ipv4.tcp_tw_reuse | TIME_WAIT reuse (0 never, 1 yes, 2 loopback only)
- net.sysctl.ipv4.tcp_fin_timeout
:::end

Outgoing connections here take local ports from {{obs:net.sysctl.ipv4.ip_local_port_range.low}} to {{obs:net.sysctl.ipv4.ip_local_port_range.high}}, {{derived:derived.net.ephemeral_ports}} ports. Each connection to one server address and port holds one of them until its TIME_WAIT ends, 60 seconds after this side closed it.

## Predict

:::predict
A load-testing client on this machine opens a connection to one server address and port, sends a request, reads the reply and closes the connection first, as fast as it can, over and over. What limits it first?

- The server's accept queue: connections arrive faster than it accepts them.
- The client's ephemeral ports: every closed connection keeps its local port in TIME_WAIT for 60 seconds.
- Nothing on the client: TIME_WAIT records are tiny, so their number does not matter.
:::reveal
Usually the ephemeral ports. The client closes first, so its side of every connection enters TIME_WAIT and keeps that four-tuple for 60 seconds. With one server address and port, only the local port varies, so at most {{derived:derived.net.ephemeral_ports}} connections can be open or in TIME_WAIT at once: a sustained rate of that number divided by 60 per second. Beyond it `connect()` fails with EADDRNOTAVAIL. With `tcp_tw_reuse` at 1 (here {{obs:net.sysctl.ipv4.tcp_tw_reuse}}), the kernel may reuse a TIME_WAIT four-tuple for a new outgoing connection once TCP timestamps prove the old segments cannot be confused with new ones, which removes this limit.

The first happens if the server is slower than the client, and then the server's ListenOverflows rises. The third is right about memory but misses that the limit is the four-tuple, not the bytes. If the server closed first, the TIME_WAIT records would be on the server, which can afford them, because its four-tuples vary in the client's address and port.
:::end

## Try it

```sh
ss -s                    # totals by state and protocol
ss -tan state time-wait | wc -l
ss -ltn                  # listening TCP sockets with their queues
nstat -az TcpExtListenOverflows TcpExtListenDrops
```

For a listening socket, `ss -ltn` prints the current accept queue length in `Recv-Q` and its limit, the effective backlog, in `Send-Q`. A `Send-Q` of 4096 next to a program that asked for more shows somaxconn's cap. `ss` reads the same tables over netlink, and lists only your own processes' sockets with `-p`; reading other users' needs root.

## Limits

:::misconception TIME_WAIT sockets are a leak
They are the protocol working: the side that closes first must remember the connection for a while. They cost a small record each and expire after 60 seconds. Thousands on a busy server are normal; only a client that exhausts its ports, or a firewall with a small table, notices them.
:::end

:::misconception tcp_fin_timeout shortens TIME_WAIT
It limits FIN_WAIT2, waiting for the peer's FIN after an orphaned close. TIME_WAIT's 60 seconds are a constant in the kernel source.
:::end

:::misconception A connection in the accept queue has been accepted
The kernel completed the handshake, and the client may already be sending data, but no program has called `accept()` yet. If the program never does, the client waits with its data in the server's receive buffer.
:::end

:::cannot
Which program owns which socket, and which hosts this machine talks to. The socket tables list both, for every process; Machine Atlas deliberately keeps only counts by state.

The queue depth of each listener. `/proc/net/tcp` shows it per listening socket, but that would be per-process data; `ss -ltn` shows it for your own inspection.

Sockets of other network namespaces: containers on this machine have their own tables.

The counters are totals since boot (or since this namespace was created); a single reading cannot say whether overflows are happening now. Two readings a minute apart can.
:::end

## Go deeper

Next, [how fast one connection can go](lesson:tcp-throughput) follows an established connection: its buffers, its window and the congestion control algorithm that decides how much it may send. In Explore, [sockets](component:socket-layer) and [TCP](component:tcp) show where this state lives. Name resolution, which turns a host name into the address a client connects to, is a planned lesson.
