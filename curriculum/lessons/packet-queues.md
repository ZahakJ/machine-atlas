---
{
  "id": "packet-queues",
  "title": "Where do packets wait?",
  "subsystem": "networking",
  "kind": "core",
  "order": 5,
  "summary": "The queues between a socket and the wire: TCP small queues, queueing disciplines (pfifo_fast, fq_codel, fq, mq, noqueue), transmit queue length, the driver ring and byte queue limits, multiqueue NICs, and why a queue that is too long costs latency rather than saving packets.",
  "prerequisites": ["tcp-throughput"],
  "uses": [
    "derived.net.default_route_interface",
    "derived.net.default_route_qdisc",
    "derived.net.default_route_tx_queues",
    "derived.net.multiqueue",
    "net.interfaces",
    "net.qdiscs",
    "net.sysctl.core.default_qdisc",
    "net.sysctl.ipv4.tcp_congestion_control"
  ],
  "relevance": [
    {"when": "derived.net.multiqueue", "is": true, "note": "An interface here has several transmit queues, each with its own qdisc under mq."},
    {"when": "derived.net.default_route_qdisc", "is": "pfifo_fast", "note": "The default route's interface uses pfifo_fast, a plain FIFO that lets bulk traffic delay everything else."}
  ],
  "next": [],
  "deeper": ["planned:firewalls-and-nat"],
  "version_notes": [
    {"applies": "Linux ≥ 3.3", "text": "Byte queue limits (BQL) cap the bytes queued in a driver's transmit ring, for drivers that report completions to it."},
    {"applies": "Linux ≥ 3.5", "text": "The fq_codel and codel qdiscs; fq (per-flow pacing) followed in 3.12, together with net.core.default_qdisc."},
    {"applies": "Linux ≥ 3.6", "text": "TCP Small Queues limit how much data one TCP socket may have in the qdisc and driver at once."},
    {"applies": "systemd ≥ 217", "text": "systemd sets net.core.default_qdisc to fq_codel through its sysctl defaults, so most distributions use it unless they override it."}
  ],
  "references": [
    {"title": "tc(8)", "url": "https://man7.org/linux/man-pages/man8/tc.8.html", "kind": "man-page"},
    {"title": "tc-fq_codel(8)", "url": "https://man7.org/linux/man-pages/man8/tc-fq_codel.8.html", "kind": "man-page"},
    {"title": "tc-fq(8)", "url": "https://man7.org/linux/man-pages/man8/tc-fq.8.html", "kind": "man-page"},
    {"title": "tc-pfifo_fast(8)", "url": "https://man7.org/linux/man-pages/man8/tc-pfifo_fast.8.html", "kind": "man-page"},
    {"title": "RFC 8290: The Flow Queue CoDel Packet Scheduler and Active Queue Management Algorithm", "url": "https://www.rfc-editor.org/rfc/rfc8290", "kind": "spec"},
    {"title": "RFC 8289: Controlled Delay Active Queue Management", "url": "https://www.rfc-editor.org/rfc/rfc8289", "kind": "spec"},
    {"title": "Linux kernel documentation: Scaling in the Linux Networking Stack (XPS)", "url": "https://docs.kernel.org/networking/scaling.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/net/core (default_qdisc)", "url": "https://docs.kernel.org/admin-guide/sysctl/net.html", "kind": "kernel-doc"},
    {"title": "Kernel source: net/sched/sch_fq_codel.c, sch_fq.c, sch_generic.c (pfifo_fast, mq)", "url": "https://github.com/torvalds/linux/tree/master/net/sched", "kind": "source"},
    {"title": "LWN: Network transmit queue limits (byte queue limits, 2011)", "url": "https://lwn.net/Articles/454390/", "kind": "lwn"},
    {"title": "LWN: TCP small queues (2012)", "url": "https://lwn.net/Articles/507065/", "kind": "lwn"},
    {"title": "Gettys, Nichols: Bufferbloat: Dark Buffers in the Internet, ACM Queue 9(11), 2011", "url": "https://queue.acm.org/detail.cfm?id=2071893", "kind": "paper"}
  ]
}
---
## Question

:::when derived.net.default_route_qdisc
Packets leaving this machine through {{derived:derived.net.default_route_interface}} wait in a queue run by a *queueing discipline*, here {{derived:derived.net.default_route_qdisc}}, and then in the network card's own ring. A queue lets a burst of packets go out at the link's pace instead of being dropped, yet a long queue is also how one large upload makes a video call stutter. How long should a queue be, and who decides which packet leaves next?
:::else
Packets leaving this machine wait in queues before they reach the wire: one run by a *queueing discipline* attached to the interface, then the network card's own ring. A queue lets a burst of packets go out at the link's pace instead of being dropped, yet a long queue is also how one large upload makes a video call stutter. How long should a queue be, and who decides which packet leaves next?
:::unknown
Packets leaving a machine wait in queues before they reach the wire: one run by a *queueing discipline* attached to the interface, then the network card's own ring. Which discipline the default route's interface uses could not be read here ({{derived:derived.net.default_route_qdisc}}). A queue lets a burst of packets go out at the link's pace instead of being dropped, yet a long queue is also how one large upload makes a video call stutter. How long should a queue be, and who decides which packet leaves next?
:::end

## Why it exists

Programs produce data in bursts, and the stack hands it down faster than a link can send it: TCP releases a whole window at once, and a CPU can build packets far faster than a 1 Gbit/s port transmits them. Something must hold the excess for the moments the link is busy. Without a queue, every burst beyond what the hardware can take at that instant would be dropped.

But a queue that never empties only adds delay. A packet behind a full queue waits for everything ahead of it to be sent: 1000 packets of 1500 bytes take 12 ms on a 1 Gbit/s link and 1.2 seconds at 10 Mbit/s. Loss-based congestion control keeps increasing its window until a queue overflows, so a large FIFO on the slowest link of a path fills and stays full. That standing queue, called *bufferbloat*, is what makes an upload ruin interactive traffic. The kernel's job is to absorb bursts while keeping the standing queue short, and to keep one flow from delaying the others.

## How it works

### The layers of queueing on the way out

1. **The socket.** Data a program has sent waits in the socket's send buffer until TCP may send it. *TCP Small Queues* then limit how much of one socket's data may sit in the layers below at once, about a millisecond of its sending rate, so one bulk flow cannot fill them.
2. **The queueing discipline.** Every interface that queues has a root qdisc. It accepts packets from the stack and hands them to the driver when the driver's ring has room, choosing which goes next.
3. **The driver ring.** The NIC fetches packets from a ring of descriptors. A ring of hundreds of large packets is another hidden FIFO, so *byte queue limits* (BQL) let each transmit queue hold only as many bytes as needed to keep the NIC busy until the next completion interrupt, adjusting the limit as it measures. Everything else waits in the qdisc, where it can be scheduled.

### The disciplines

- **pfifo_fast** was the default for decades: three FIFO bands, chosen by the packet's priority, each holding up to `tx_queue_len` packets (1000 by default), with the tail dropped when full. Fair to nobody: a bulk flow fills its band, and everything in that band waits behind it.
- **fq_codel** (RFC 8290) hashes packets into 1024 per-flow queues and serves them in turn, so a sparse flow such as a video call or a DNS query overtakes a bulk upload. Within each queue CoDel measures how long packets have waited; when the shortest wait stays above 5 ms for 100 ms, it drops or ECN-marks packets to tell the sender to slow down. Queues stay short without a fixed length to tune.
- **fq** keeps one queue per socket and releases each flow's packets at that socket's *pacing rate*, which BBR sets, so a flow's packets are spread evenly instead of leaving in bursts. Servers sending a lot of locally generated traffic use it, usually with BBR.
- **mq** queues nothing itself. On a NIC with several transmit queues it attaches one child qdisc (the default qdisc) per queue, and each CPU sends through the queue that transmit packet steering (XPS) assigns it, so CPUs do not contend for one lock.
- **noqueue** means none: loopback, veth, bridges, bonds and VLANs pass each packet straight on to the device below, where the real queue is. Some Wi-Fi drivers also register no qdisc, because the wireless stack keeps its own fair queues per station.

`net.core.default_qdisc` decides what an interface gets when nobody configured one; distributions set it, systemd to fq_codel. An administrator can attach anything else with `tc`, including shapers that make this machine the bottleneck on purpose so its queue, not the modem's, is the one that fills.

### Where the queue that matters is

A queue builds only where packets arrive faster than they leave: at the *bottleneck* of the path. For a desktop on gigabit Ethernet behind a 40 Mbit/s upstream connection, that is the home router or modem, not this machine. This machine's qdisc then stays nearly empty, and only the router's queue management decides the latency of an upload. A server whose 25 Gbit/s NIC is faster than any single client is rarely its own bottleneck either; its queues matter for fairness between thousands of flows and for pacing.

## On this machine

:::table net.qdiscs columns=interface,kind,parent,root,packets,drops,requeues,backlog_bytes,qlen limit=24

:::evidence Queueing on the default route
- derived.net.default_route_interface
- derived.net.default_route_qdisc | The discipline that schedules its packets (its members' for a bond or bridge)
- derived.net.default_route_tx_queues
- net.sysctl.core.default_qdisc | What new interfaces get
- derived.net.multiqueue | Some interface has several transmit queues
- net.sysctl.ipv4.tcp_congestion_control
:::end

:::when derived.net.default_route_qdisc is noqueue
The default route's interface, {{derived:derived.net.default_route_interface}}, has no qdisc of its own (noqueue) and no member interfaces whose qdisc would take over. Packets pass straight to the driver: a tunnel hands them to the VPN program or the device it runs over, which queues them there, and some Wi-Fi drivers keep fair queues per station inside the wireless stack instead of a qdisc.
:::else
Packets to the internet are scheduled by {{derived:derived.net.default_route_qdisc}} on {{derived:derived.net.default_route_interface}} (transmit queues: {{derived:derived.net.default_route_tx_queues}}); for a bond or bridge, which has no queue of its own, the qdisc named is its members'. The `drops` column counts packets each qdisc discarded since it was attached: overflows of a FIFO, or CoDel's deliberate drops, and `requeues` the times the driver's ring was full when the qdisc tried to hand a packet over.
:::unknown
Which discipline schedules the default route's packets could not be determined here ({{derived:derived.net.default_route_qdisc}}): `tc` is missing, has no JSON output or cannot reach the kernel, or the routing table is unreadable.
:::when net.sysctl.core.default_qdisc
New interfaces here get {{obs:net.sysctl.core.default_qdisc}} unless something configures another, so a physical interface most likely runs it, or mq with it on every queue.
:::else
New interfaces here get {{obs:net.sysctl.core.default_qdisc}} unless something configures another.
:::unknown
Even the default, `net.core.default_qdisc`, is not visible here ({{obs:net.sysctl.core.default_qdisc}}): it exists only in the host's initial network namespace. In a container, `eth0` is one end of a veth pair with no queue of its own, and packets wait in the host's qdisc on its physical NIC.
:::end
:::end

:::when derived.net.default_route_qdisc is pfifo_fast
pfifo_fast keeps up to `tx_queue_len` packets per band in strict FIFO order. On this machine's link a bulk upload can therefore delay every other packet in its band by the time it takes to send a full queue, if this link is the bottleneck. Switching to fq_codel (`tc qdisc replace dev <if> root fq_codel`, as root) removes that delay without changing throughput.
:::end

:::when net.sysctl.ipv4.tcp_congestion_control is bbr
:::when derived.net.default_route_qdisc is fq
BBR here is paired with fq, which releases each connection's packets at the rate BBR computed: the combination BBR was designed for.
:::else
BBR here runs without fq on the default route, so TCP paces each connection itself with a high-resolution timer, which costs a little more CPU than letting fq do it.
:::unknown
BBR is in use here; whether fq paces its packets could not be determined.
:::end
:::end

### Transmit queues and byte queue limits

:::table net.interfaces columns=name,type,tx_queue_len,tx_queues,rx_queues,bql_limit_bytes

:::when derived.net.multiqueue
At least one interface here has several transmit queues, each with its own qdisc under `mq` and its own byte queue limit, so CPUs sending at the same time do not wait for one another. `bql_limit_bytes` sums the current limits of all of an interface's transmit queues: BQL keeps each one at a few packets' worth when the link keeps up, and the limits stay at zero on a queue that has carried no traffic.
:::else
Every interface here has a single transmit queue, so every CPU sending through one interface takes turns on the same queue and lock, which is fine at the rates a single-queue NIC carries.
:::unknown
Queue counts could not be read here ({{derived:derived.net.multiqueue}}): the interface list is unavailable.
:::end

## Predict

:::predict
A desktop on gigabit Ethernet, behind a home connection with a 20 Mbit/s upstream, uploads a large file while a video call runs. The call stutters. The desktop's qdisc is switched from pfifo_fast to fq_codel. What happens?

- The call recovers: fq_codel gives the call's packets their own queue.
- Little changes: the queue that fills is in the home router, where the link narrows to 20 Mbit/s, not on the desktop.
- The upload becomes much faster, because fq_codel drops fewer packets.
:::reveal
Little changes. The desktop sends at up to 1 Gbit/s into a link that drains at 20 Mbit/s, so packets pile up at the router, not on the desktop: the desktop's qdisc stays almost empty and has nothing to reorder. Fixing it means queue management on the router (fq_codel or cake there), or shaping the desktop's own output to just below 20 Mbit/s so that its queue becomes the bottleneck and fq_codel can work.

The first answer is what happens when the desktop's own link is the bottleneck, for instance on slow Wi-Fi, where the queue does build up on this side. The third confuses scheduling with capacity: fq_codel changes the order and the waiting time of packets, not how much the link can carry.
:::end

## Try it

```sh
tc -s qdisc show                     # every qdisc with bytes, drops, overlimits, backlog
tc -s qdisc show dev <interface>     # one interface
cat /sys/class/net/<interface>/queues/tx-0/byte_queue_limits/limit
ping -c 20 192.0.2.1                 # replace with your router; run once idle, once during an upload
```

Listing qdiscs needs no privileges; changing them needs root. The `ping` comparison is the classic bufferbloat test: if round trips to the first router grow from a few milliseconds to hundreds during an upload, a queue on the path fills. During a large upload, `backlog` in `tc -s` shows how much waits on this machine; zero means the queue is elsewhere.

## Limits

:::misconception A longer queue means fewer lost packets
A longer queue delays losses, it does not prevent them: a sender using loss-based congestion control grows its window until the queue overflows, however long it is. What a longer queue adds is waiting time for every packet behind it.
:::end

:::misconception Dropping packets on purpose is a failure
Active queue management such as CoDel drops or marks a few packets early so senders slow down before a long queue forms. Throughput stays the same and latency falls.
:::end

:::misconception The qdisc on a bridge or bond is where packets wait
These virtual devices have noqueue and pass packets on at once; the queue is on the physical interface below, or, for a container's veth, on the host's side.
:::end

:::cannot
Where the bottleneck of a path is. These counters describe only this machine's queues; a queue in a router or modem is invisible from here, and only latency measurements under load reveal it.

Queues inside the NIC and in the Wi-Fi stack: the hardware's own buffers, and the per-station queues of mac80211, are not exposed through these files.

What each queue held at any moment other than the instant of collection. `backlog` and `qlen` are instantaneous; drops and requeues are totals since the qdisc was attached, which may have been at boot or a minute ago.

Classes and filters of a configured hierarchy (HTB, cake's tins): Machine Atlas lists qdiscs only.
:::end

## Go deeper

This is the last lesson of the network track. [The path of a packet](lesson:packet-path) shows where these queues sit among the other steps, and [how fast one connection can go](lesson:tcp-throughput) why loss-based congestion control fills them. In Explore, [queueing disciplines](component:qdisc) and [network interface controllers](component:nic) connect to the rest of the stack. Firewalls and address translation, which also run on every packet, are a planned lesson.
