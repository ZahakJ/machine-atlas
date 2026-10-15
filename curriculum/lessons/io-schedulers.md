---
{
  "id": "io-schedulers",
  "title": "Who decides the order of disk requests",
  "subsystem": "storage",
  "kind": "core",
  "order": 2,
  "summary": "Why the block layer can hold and reorder requests, what none, mq-deadline, bfq and kyber each do, how the kernel picks a default from the device's queue count, why NVMe usually runs with none, and what a scheduler cannot change.",
  "prerequisites": ["block-devices"],
  "uses": [
    "block.queues",
    "derived.storage.root_disk",
    "derived.storage.root_disk_kind",
    "derived.storage.root_hw_queues",
    "derived.storage.root_rotational",
    "derived.storage.root_scheduler",
    "derived.storage.root_schedulers"
  ],
  "relevance": [
    {"when": "derived.storage.root_rotational", "is": true, "note": "The root disk reports itself as rotational, the kind of device whose throughput depends most on request order."},
    {"when": "derived.storage.root_scheduler", "is": "bfq", "note": "The root disk runs bfq, the scheduler that shares a device between processes by budget."}
  ],
  "next": ["io-statistics"],
  "deeper": ["planned:memory-cgroups"],
  "version_notes": [
    {"applies": "Linux ≥ 4.11", "text": "mq-deadline, the first scheduler for blk-mq; bfq and kyber followed in 4.12."},
    {"applies": "Linux ≥ 5.0", "text": "The single-queue schedulers noop, deadline and cfq were removed with the single-queue block layer; none, mq-deadline, bfq and kyber remain."},
    {"applies": "Linux ≥ 5.0", "text": "A device with one hardware queue gets mq-deadline by default and one with several gets none, unless a udev rule or an administrator selects another; the elevator= boot option is ignored since Linux 5.4."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Switching Scheduler", "url": "https://docs.kernel.org/block/switching-sched.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Deadline IO scheduler tunables", "url": "https://docs.kernel.org/block/deadline-iosched.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: BFQ (Budget Fair Queueing)", "url": "https://docs.kernel.org/block/bfq-iosched.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Kyber I/O scheduler tunables", "url": "https://docs.kernel.org/block/kyber-iosched.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Control Group v2, IO controller (io.weight, io.max, io.latency)", "url": "https://docs.kernel.org/admin-guide/cgroup-v2.html#io", "kind": "kernel-doc"},
    {"title": "Kernel source: block/elevator.c (elevator_get_default)", "url": "https://github.com/torvalds/linux/blob/master/block/elevator.c", "kind": "source"},
    {"title": "Kernel source: block/mq-deadline.c", "url": "https://github.com/torvalds/linux/blob/master/block/mq-deadline.c", "kind": "source"},
    {"title": "LWN: Two new block I/O schedulers for 4.12 (2017)", "url": "https://lwn.net/Articles/720675/", "kind": "lwn"},
    {"title": "LWN: A block layer introduction part 2: the request layer (2017)", "url": "https://lwn.net/Articles/738449/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.storage.root_disk_kind is dm
The disk under / on this machine, {{derived:derived.storage.root_disk}}, is a device-mapper volume, and its scheduler file offers exactly one choice: {{derived:derived.storage.root_scheduler}}. The disk beneath it offers several, and one of them, `none`, means the kernel passes requests to the device in the order they arrive. Why would the kernel ever do otherwise, what do the other schedulers reorder for, who chooses, and why does a volume like this one get no choice at all?
:::else
The disk under / on this machine, {{derived:derived.storage.root_disk}}, runs the I/O scheduler {{derived:derived.storage.root_scheduler}}; the kernel offers {{derived:derived.storage.root_schedulers}} for its queue. One of those choices, `none`, means the kernel passes requests to the device in the order they arrive. Why would it ever do otherwise, what do the others reorder for, and who chose this one?
:::unknown
Every block device with a request queue has an I/O scheduler, which may hold requests back and send them to the device in a different order from the one they arrived in. On many machines the choice for a fast drive is `none`, no reordering at all. Why would the kernel ever reorder requests, what do the schedulers reorder for, and who chooses? The scheduler of the disk under / is {{derived:derived.storage.root_scheduler}} here; the devices that could be read are listed below.
:::end

## Why it exists

On a spinning disk, the order of requests decides the throughput. Reading the next sector costs a few microseconds; reading one on the far side of the platter costs a seek and half a rotation, several milliseconds. A disk given requests in arrival order from several programs spends most of its time moving the head. Sorting them by position, as an elevator serves floors, can multiply the rate.

Sorting alone has two costs. A request far from the others can wait forever while nearer ones keep arriving, and one process streaming a large file can crowd out everyone else. So schedulers also bound how long a request may wait, and some divide the device between processes. Each of these needs the kernel to hold requests in a queue it controls rather than handing them straight to the device, and holding them costs CPU time and some latency. On a device with no seek time and many internal channels, that cost buys almost nothing.

## How it works

### Where the scheduler sits

Requests leave a CPU's software queue (the previous lesson) either straight for a hardware queue or into the scheduler, which decides when each is dispatched. Merging happens either way: bios from one task are combined in its plug list before they become requests. What the scheduler adds is holding requests back, choosing among them, and bounding their wait. Devices then reorder again on their own: SATA's NCQ lets a disk run its 32 queued commands in the order its head favours, and an SSD spreads commands over its flash channels.

### none

No scheduler. Requests go to the hardware queue as soon as a tag is free. It costs the least CPU per request and adds no queueing delay, which is what an NVMe drive doing hundreds of thousands of operations per second needs. Fairness then comes from the device and, if configured, the cgroup I/O controller.

### mq-deadline

Keeps reads and writes in two lists sorted by sector and two first-in, first-out lists by arrival. Normally it dispatches batches (`fifo_batch`, 16 requests) in sector order, continuing from where the last batch ended. Each request also gets a deadline, by default 500 ms for reads and 5 s for writes; when the oldest one passes its deadline, dispatch jumps to it. Reads are preferred, because a program usually waits for a read while writes are mostly background writeback, but writes are not starved: after `writes_starved` (2) read batches, writes get a turn. It is cheap and protects reads, which is why the kernel uses it by default where a device has a single queue.

### bfq

Budget Fair Queueing gives each process its own queue (and each cgroup a share, by its `io.bfq.weight`) and serves them one at a time, each for a *budget* of sectors, in proportion to their weights. While serving a queue it may idle briefly to wait for the next request of the same process, which keeps a sequential reader sequential instead of interleaving it with others. It detects interactive and soft real-time tasks (a program starting up, a media player) and raises their weight. The result is low latency for the desktop under heavy background I/O and fair sharing between processes, at a CPU cost per request that limits it to devices doing up to the low hundreds of thousands of operations per second. When bfq is selected, the kernel switches writeback throttling off for that queue, since bfq protects latency in its own way.

### kyber

Designed for fast devices. It does not sort; it divides requests into domains (reads, synchronous writes, other writes, discards) and limits how many of each are in flight, adjusting those limits so that the observed latency of reads stays near a target (2 ms by default) and of synchronous writes near another (10 ms). Throttling background writes keeps the device's internal queues short, so reads behind them are not delayed.

### The default and who changes it

When a queue is created, the kernel picks mq-deadline if the device has one hardware queue and none if it has several. Distributions then apply udev rules: many choose bfq for spinning disks and SD cards and leave NVMe with none, and an administrator can write another name to `/sys/block/<dev>/queue/scheduler` at any time. Schedulers built as modules appear in the list only once loaded. A bio-based device (device-mapper, md) shows `none` and nothing else: it has no request queue to schedule, and scheduling happens on the disks beneath it.

## On this machine

:::table block.queues columns=name,scheduler,schedulers,hw_queues,rotational,nr_requests,wbt_latency_us

:::evidence The scheduler of the disk under /
- derived.storage.root_disk
- derived.storage.root_disk_kind
- derived.storage.root_scheduler
- derived.storage.root_schedulers | Every scheduler offered for this queue
- derived.storage.root_hw_queues
- derived.storage.root_rotational
:::end

:::when derived.storage.root_scheduler is none
:::when derived.storage.root_disk_kind is dm
The root disk is a device-mapper volume, so `none` is not a choice but the absence of one: the volume has no request queue, and each request is scheduled on the disk beneath it. That disk's scheduler is in the table above.
:::else
The root disk runs without a scheduler. With {{derived:derived.storage.root_hw_queues}} hardware queues, that is the kernel's default for a multiqueue device: reordering would cost CPU time on every request and save almost nothing on a device with no seek time and many internal channels. If several processes compete for it, their shares are decided by the device and by any cgroup I/O limits, not by the kernel's queue.
:::unknown
The root disk runs without a scheduler. Its hardware queue count, which decides whether that is the kernel's default, is {{derived:derived.storage.root_hw_queues}} here.
:::end
:::else
:::when derived.storage.root_scheduler is mq-deadline
The root disk runs mq-deadline, the kernel's default for a device with a single hardware queue. Reads that have waited 500 ms, and writes that have waited 5 s, are dispatched ahead of the sorted order, and reads are preferred over writes otherwise.
:::else
The root disk runs {{derived:derived.storage.root_scheduler}}, which is not the kernel's default for any device: a udev rule of the distribution, or an administrator, selected it. bfq is the usual choice for spinning disks and SD cards on desktops, where it keeps the system responsive while something writes in the background; kyber is chosen for fast devices that need a read latency target.
:::unknown
The root disk's scheduler is {{derived:derived.storage.root_scheduler}} here.
:::end
:::unknown
The scheduler of the disk under / is {{derived:derived.storage.root_scheduler}} here. In a container the root is an overlay of host directories, so no disk of its own holds it, and changing a scheduler needs privileges on the host; where `/sys` is restricted, the queue settings cannot be read at all. On a typical machine an NVMe drive shows `[none]`, and a SATA disk or SD card `[mq-deadline]` or `[bfq]`.
:::end

:::when derived.storage.root_rotational
The kernel treats this disk as rotational, the case schedulers were invented for: sorting requests by position saves seeks, and bfq's idling keeps a sequential reader's requests together.
:::else
The kernel treats this disk as non-rotational. Request order matters to it only through fairness and latency, not seek time, which is why the cheapest scheduler that keeps reads responsive is usually the right one.
:::unknown
Whether the disk under / rotates is {{derived:derived.storage.root_rotational}} here.
:::end

Rows with an empty `wbt_latency_us`, or 0, have writeback throttling off: under bfq because bfq replaces it, on bio-based volumes because they have no request queue, and on kernels built without it.

## Predict

:::predict
A spinning disk serves two programs at once: a backup tool reading one large file sequentially, and an editor opening a few small files scattered over the disk. Which scheduler gives the editor its files soonest, and which gives the backup the highest throughput?

- none for both: no scheduler adds no delay.
- mq-deadline for both: it sorts by position and bounds every wait.
- bfq for the editor's latency; mq-deadline or none for the backup's raw throughput, at the editor's expense.
:::reveal
The third. Under none and mq-deadline the backup's long stream of sorted, mergeable requests fills the queue, and the editor's reads, far away on the platter, wait behind batches of them; mq-deadline only guarantees they start within about 500 ms. bfq gives each process its own queue and budget, notices that the editor is interactive and raises its weight, so its few reads are served almost at once; the backup loses a little throughput to the seeks that interrupt its stream and to the idling between budgets.

The first answer is right only for devices without seek time and with many channels, where the editor's reads go to another channel and nobody waits. The second is half right: mq-deadline bounds the wait, but a bound of half a second per read is the latency a person notices when opening a file.
:::end

## Try it

```sh
grep . /sys/block/*/queue/scheduler            # [the selected one] and the alternatives
ls /sys/block/sda/queue/iosched/                # the selected scheduler's tunables (not for none)
cat /sys/block/sda/queue/iosched/read_expire    # mq-deadline's read deadline, in ms
```

Changing a scheduler is `echo bfq | sudo tee /sys/block/sda/queue/scheduler`; it takes effect for new requests at once and is lost at reboot. Machine Atlas never changes it. To compare schedulers, run the same workload under each and measure the latency of the requests you care about, not only throughput: [I/O statistics](lesson:io-statistics) shows what the counters can and cannot tell.

## Limits

:::misconception none means requests are not queued
With none the kernel does not hold or reorder requests, but they still wait for a tag, in the hardware queue and inside the device, which reorders them itself. none removes one layer of queueing, not queueing.
:::end

:::misconception A scheduler decides the order the disk executes requests
It decides the order the kernel dispatches them. A SATA disk with NCQ, an SSD and an NVMe drive all reorder the commands they hold; the scheduler's influence ends where the device's queue begins, which is why bfq and kyber control how many requests are dispatched as well as which.
:::end

:::misconception bfq is too slow for SSDs
bfq's CPU cost per request limits it on devices doing hundreds of thousands of operations per second, but a SATA SSD or an SD card rarely reaches that, and on a desktop its latency for interactive tasks under background writes is often worth the cost. It is a trade-off to measure, not a rule.
:::end

:::cannot
Which processes got how much of a device, and how long requests waited in the scheduler rather than in the device: the counters do not separate them, and tracing needs privileges.

The tunables of the selected scheduler (`queue/iosched/`) and the cgroup I/O controller's settings (`io.weight`, `io.max`, `io.latency`): Machine Atlas does not collect them.

Whether a different scheduler would serve this machine's workload better: that is a measurement of the workload under each, which no reading of the configuration can replace.
:::end

## Go deeper

Next, [how busy is a disk, really?](lesson:io-statistics) reads the counters every request leaves behind, and what they say about latency and saturation. In Explore, [request queues and I/O schedulers](component:request-queues) lists the queues found here. The planned lesson on memory cgroups touches the cgroup I/O controller, which bfq's weights and the io.max limits belong to.
