---
{
  "id": "io-statistics",
  "title": "How busy is a disk, really?",
  "subsystem": "storage",
  "kind": "core",
  "order": 3,
  "summary": "What the block layer counts for every device, how iostat turns two readings into rates, latencies, queue sizes and %util, why %util says little about a device that serves many requests at once, and how I/O pressure measures the time tasks lose waiting.",
  "prerequisites": ["io-schedulers"],
  "uses": [
    "block.psi.full.avg60",
    "block.psi.some.avg10",
    "block.psi.some.avg60",
    "block.psi.some.total_us",
    "block.stats",
    "derived.env.kind",
    "derived.storage.root_busy_queue_depth",
    "derived.storage.root_disk",
    "derived.storage.root_read_bytes",
    "derived.storage.root_read_latency_seconds",
    "derived.storage.root_util_fraction",
    "derived.storage.root_write_latency_seconds",
    "derived.storage.root_written_bytes",
    "kernel.uptime_seconds",
    "mem.vmstat.pgpgin",
    "mem.vmstat.pgpgout"
  ],
  "relevance": [
    {"when": "derived.storage.root_disk_kind", "is": "nvme", "note": "The root disk is an NVMe drive, which serves many requests at once: the case in which %util misleads most."}
  ],
  "next": ["filesystems-and-the-vfs"],
  "deeper": ["run-queues-and-load"],
  "version_notes": [
    {"applies": "Linux ≥ 4.18", "text": "Block device statistics gained four discard fields, 15 in all."},
    {"applies": "Linux ≥ 4.20", "text": "Pressure stall information (/proc/pressure/io, memory, cpu); kernels can be built without it (CONFIG_PSI) or boot with it disabled (psi=0)."},
    {"applies": "Linux ≥ 5.5", "text": "Block device statistics gained two flush fields, 17 in all."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Block layer statistics in /sys/block/<dev>/stat", "url": "https://docs.kernel.org/block/stat.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: I/O statistics fields (/proc/diskstats)", "url": "https://docs.kernel.org/admin-guide/iostats.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: PSI, Pressure Stall Information", "url": "https://docs.kernel.org/accounting/psi.html", "kind": "kernel-doc"},
    {"title": "iostat(1) (sysstat)", "url": "https://man7.org/linux/man-pages/man1/iostat.1.html", "kind": "man-page"},
    {"title": "proc_pid_io(5): per-process I/O counters", "url": "https://man7.org/linux/man-pages/man5/proc_pid_io.5.html", "kind": "man-page"},
    {"title": "Kernel source: block/genhd.c (diskstats_show) and block/blk-core.c (update_io_ticks)", "url": "https://github.com/torvalds/linux/blob/master/block/genhd.c", "kind": "source"},
    {"title": "Little, J. D. C.: A Proof for the Queuing Formula L = λW (Operations Research, 1961)", "url": "https://doi.org/10.1287/opre.9.3.383", "kind": "paper"},
    {"title": "LWN: Tracking pressure-stall information (2018)", "url": "https://lwn.net/Articles/759781/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.storage.root_util_fraction
Since this machine booted, {{obs:kernel.uptime_seconds}} ago, the disk under / ({{derived:derived.storage.root_disk}}) has read {{derived:derived.storage.root_read_bytes}} and written {{derived:derived.storage.root_written_bytes}}. It had at least one request in flight {{derived:derived.storage.root_util_fraction}} of that time, the figure `iostat` would call its utilisation. Does that say how close the disk is to its limit? And how long did each request take, how many were in flight at once, and how much did programs lose waiting for them?
:::unknown
Every block device counts what it does: requests completed, bytes moved, milliseconds spent. From those counters `iostat` derives a utilisation, the share of time the device was busy, and many people read 100% as "this disk is at its limit". Does it say that? And how long did each request take, how many were in flight at once, and how much did programs lose waiting for them? The busy fraction of the disk under / is {{derived:derived.storage.root_util_fraction}} here; the devices whose counters could be read are listed below.
:::end

## Why it exists

The kernel sees every request start and finish, so it can count them almost for free, and anyone who wants to know whether storage is the bottleneck needs those counts. But a storage device is not one server with one queue. It may finish requests in a different order from the one they arrived in, and it may work on many at once. Numbers that are exact descriptions of what happened can then be misleading answers to the question people actually ask: can this device do more?

## How it works

### What each device counts

`/sys/block/<dev>/stat` (and one line per device and partition in `/proc/diskstats`) holds counters since the device appeared, normally at boot:

| fields | meaning |
|---|---|
| reads, writes (completed) | requests finished, after merging |
| reads merged, writes merged | requests that were merged into a neighbour instead of being sent separately |
| sectors read, written | 512-byte sectors moved, whatever the device's block size |
| read ms, write ms | the time each request spent from entering the block layer to completion, summed over requests |
| in flight | requests issued and not yet completed, now (the only field that is not a counter) |
| io ms (io_ticks) | milliseconds during which at least one request was in flight |
| weighted ms (time in queue) | each millisecond multiplied by the requests in flight during it |
| discards, flushes | the same counts for discard (4.18) and cache flush (5.5) requests |

Two kinds of time are mixed here. `read ms` and `weighted ms` add up time over requests, so overlapping requests count several times; `io ms` counts wall time, once, however many requests overlap.

### From counters to iostat

One reading gives totals since boot; rates need two readings Δt apart. `iostat -x` computes, for each device:

- **r/s, w/s, rkB/s, wkB/s**: Δcompleted and Δsectors divided by Δt.
- **r_await, w_await**: Δread ms / Δreads, the average time a request took, including the time it waited in the kernel's queues. It is not the device's own service time.
- **aqu-sz**: Δweighted ms / Δt, the average number of requests in flight. This is Little's law: the number in a system equals the arrival rate times the time each spends there.
- **%util**: Δio ms / Δt, the share of time the device had anything to do.

### Why %util lies on parallel devices

For a disk with one head that serves one request at a time, busy all the time does mean saturated. An SSD has many flash channels and an NVMe drive accepts thousands of commands; such a device can be busy 100% of the time with one request in flight and still have most of its capacity unused. A second program would not wait; it would run in parallel. %util cannot tell the two cases apart. Saturation shows instead as latency: when await rises as the request rate rises, requests are queueing. The average number in flight while busy, weighted ms divided by io ms, says how much parallelism the device was actually given.

### Pressure: what waiting cost

The counters describe the device; they do not say whether anyone was waiting. Pressure stall information (Linux 4.20 and later) measures that, in `/proc/pressure/io`. A task is stalled on I/O while it sleeps in an I/O wait: reading a page it needs, waiting for a write it must see finish, or being paused by the kernel because it dirties pages faster than they are written back. *some* is the share of time at least one task was stalled; *full* the share in which every task that wanted to run was stalled, so the CPUs did nothing useful for them. Each comes averaged over 10, 60 and 300 seconds, with a total in microseconds since boot. A device can be 100% busy with zero pressure (a background copy nobody waits for) and a mostly idle device can cause pressure (one slow read that a program blocks on).

## On this machine

:::table block.stats columns=name,reads,read_bytes,read_ms,writes,write_bytes,write_ms,in_flight,io_ms,weighted_ms

:::evidence The disk under /, averaged since boot
- derived.storage.root_disk
- kernel.uptime_seconds
- derived.storage.root_read_bytes
- derived.storage.root_written_bytes
- derived.storage.root_read_latency_seconds | Average read, submission to completion
- derived.storage.root_write_latency_seconds | Average write, submission to completion
- derived.storage.root_util_fraction | Share of time busy (what iostat calls %util)
- derived.storage.root_busy_queue_depth | Requests in flight on average while busy
- mem.vmstat.pgpgin | KiB read from all block devices since boot
- mem.vmstat.pgpgout | KiB written to all block devices since boot
:::end

:::when derived.storage.root_util_fraction
Averaged over the whole uptime, a read on {{derived:derived.storage.root_disk}} took {{derived:derived.storage.root_read_latency_seconds}} and a write {{derived:derived.storage.root_write_latency_seconds}}, and the disk was busy {{derived:derived.storage.root_util_fraction}} of the time. While it was busy it had {{derived:derived.storage.root_busy_queue_depth}} requests in flight on average: near 1, requests came mostly one at a time; well above 1, the device worked on several at once, and the busy fraction alone cannot show how much of its parallel capacity that used.

Most writes were background writeback that no program waited for, so the write time is not a latency anyone felt. And a long-run average hides everything that matters for a slow afternoon: a disk idle all night and saturated for an hour averages to "mostly idle".
:::unknown
The busy fraction of the disk under / is {{derived:derived.storage.root_util_fraction}} here.

:::when derived.env.kind is container
In a container the table above still shows the host's devices: `/sys/block` is not separated per container, and the counters include every container's and the host's own I/O. What this container itself read and wrote is not in them; its cgroup's `io.stat` would say, where the io controller is enabled.
:::else
When / is a device-mapper volume, its counters and those of the disk beneath it both count the same data; when the root disk cannot be identified (a restricted `/sys`, a root on a network filesystem), the table, if readable, still shows what every device did.
:::unknown
Whether Atlas runs in a container could not be determined; in one, the table would show the host's devices and the counters every container's I/O.
:::end
:::end

pgpgin and pgpgout count KiB moved to and from all block devices together, swap included. Summing the devices' read and write bytes in the table gives about the same totals, or more where a device-mapper volume and the disk beneath it both count the same data.

### Pressure now

:::when block.psi.some.total_us
:::evidence I/O pressure
- block.psi.some.avg10 | Some task stalled on I/O, last 10 s
- block.psi.some.avg60 | Some task stalled on I/O, last 60 s
- block.psi.full.avg60 | Every task stalled on I/O, last 60 s
- block.psi.some.total_us | Total stall time since boot
:::end

Over the last minute, some task here was waiting for I/O {{obs:block.psi.some.avg60}} of the time, and every runnable task at once {{obs:block.psi.full.avg60}}. Since boot, the total is {{obs:block.psi.some.total_us}} out of {{obs:kernel.uptime_seconds}}. A *some* figure of a few percent that comes and goes is ordinary; a *full* figure that stays above a few percent means work on this machine regularly stops for storage.
:::else
No task has stalled on I/O since boot by pressure stall information's count ({{obs:block.psi.some.total_us}}): nothing here has waited for storage, which on a machine that has run for a while is rare.
:::unknown
Pressure stall information here is {{obs:block.psi.some.total_us}}. If the file is absent, the kernel predates 4.20, was built without CONFIG_PSI or was booted with psi=0; if permission is denied, a sandbox or security policy hides `/proc/pressure`. Without it, the counters above say what the devices did but not what anyone lost waiting for them; the "procs_blocked" line of `/proc/stat`, the number of tasks in uninterruptible sleep now, is a coarse substitute.
:::end

## Predict

:::predict
`iostat -x 1` shows an NVMe drive at 100% util, serving 20,000 reads per second with an r_await of 0.4 ms and an aqu-sz of 8. Is the drive saturated?

- Yes: 100% util means it cannot do more.
- Probably not: it is busy all the time, but working on 8 requests at once at a latency no higher than when idle.
- It cannot be told from these numbers alone.
:::reveal
Probably not, with a caveat. Little's law ties the numbers together: 20,000 requests per second each taking 0.4 ms means 8 in flight on average, exactly the aqu-sz shown. A device that accepts thousands of commands and still answers in 0.4 ms with 8 in flight is not queueing; give it more parallel work and it will usually serve more at a similar latency. %util is 100% because at every millisecond at least one request was in flight, which says nothing about the other channels.

The first answer is the single-disk intuition, right for a spinning disk that serves one request at a time. The third has a point: the decisive evidence is how latency changes as load rises, which one snapshot cannot show. If r_await were climbing from 0.1 ms to 0.4 ms as the rate grew, the drive would be approaching its limit.

:::when derived.storage.root_busy_queue_depth
On this machine's root disk the since-boot equivalent of aqu-sz while busy is {{derived:derived.storage.root_busy_queue_depth}} requests.
:::unknown
The since-boot equivalent for this machine's root disk is {{derived:derived.storage.root_busy_queue_depth}} here.
:::end
:::end

## Try it

```sh
cat /sys/block/nvme0n1/stat; sleep 10; cat /sys/block/nvme0n1/stat   # subtract, divide by 10 s
iostat -x 1 5                                                          # the same, computed (sysstat)
cat /proc/pressure/io
grep -E 'read_bytes|write_bytes' /proc/self/io                         # this shell's own I/O
```

Replace `nvme0n1` with a device from the table. Run a large `cp` in another terminal and watch r/s, w/s, await and util move; then run `cat /proc/pressure/io` during the copy, and again after it, to see whether anything was waiting.

## Limits

:::misconception 100% util means the disk is saturated
It means at least one request was in flight all the time. For a device that serves one request at a time that is saturation; for an SSD or NVMe drive serving many in parallel it is not. Latency rising with load is the sign of saturation.
:::end

:::misconception await is how long the device takes
await is measured from the moment a request enters the block layer, so it includes time waiting for a tag, in the scheduler and in the hardware queue. The device's own service time is shorter, and only tracing separates the two.
:::end

:::misconception iowait is CPU time spent doing I/O
`iowait` in `/proc/stat` and `top` is time a CPU was idle while some task that last ran on it waited for I/O. The CPU did no work then, and could have run anything else; a busy machine can have high I/O pressure and zero iowait. Pressure stall information measures the waiting directly.
:::end

:::cannot
Rates from one reading: every counter here is a total since the device appeared. Rates and current latencies need two readings and a subtraction.

Which process or cgroup caused the I/O: `/proc/<pid>/io` gives a process's own totals (readable only for your own processes), the cgroup io controller's `io.stat` gives per-cgroup totals, and Machine Atlas collects neither.

How a request's time divides between the kernel's queues and the device, or the distribution of latencies rather than their average: both need tracing (blktrace, BPF tools such as biolatency), which needs privileges.
:::end

## Go deeper

Next, [how a path becomes blocks on a disk](lesson:filesystems-and-the-vfs) climbs back up to the filesystem that decides which blocks these requests touch. The CPU track's [load average lesson](lesson:run-queues-and-load) explains why Linux counts tasks waiting for disks in the load average. In Explore, [block devices](component:block-devices) shows each device with its counters.
