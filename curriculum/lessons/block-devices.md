---
{
  "id": "block-devices",
  "title": "Where a read goes when the cache misses",
  "subsystem": "storage",
  "kind": "core",
  "order": 1,
  "summary": "Block devices as arrays of sectors, logical and physical block sizes, how a page cache miss becomes a bio and then a request, the per-CPU and hardware queues of blk-mq, and why NVMe, SATA, SD cards, virtio disks and device-mapper volumes look so different to the kernel.",
  "prerequisites": ["page-cache"],
  "uses": [
    "block.queues",
    "cpu.logical_count",
    "derived.env.kind",
    "derived.storage.root_disk",
    "derived.storage.root_disk_kind",
    "derived.storage.root_hw_queues",
    "derived.storage.root_max_request_bytes",
    "derived.storage.root_nr_requests",
    "derived.storage.root_rotational",
    "derived.storage.root_write_cache"
  ],
  "relevance": [
    {"when": "derived.storage.root_disk_kind", "is": "nvme", "note": "The root filesystem is on an NVMe drive, the device blk-mq's many queues were built for."},
    {"when": "derived.env.kind", "is": "container", "note": "Atlas runs in a container, which sees the host's block devices in /sys but not which of them holds its files."}
  ],
  "next": ["io-schedulers"],
  "deeper": ["io-statistics", "planned:buses-and-device-model", "planned:dma-and-iommu"],
  "version_notes": [
    {"applies": "Linux ≥ 3.13", "text": "blk-mq, the multi-queue block layer, was merged: per-CPU software queues mapped onto the device's hardware queues. NVMe used it from 3.19 and SCSI could from 3.17."},
    {"applies": "Linux ≥ 4.18", "text": "/sys/block/<dev>/queue/fua reports whether the driver accepts Force Unit Access writes."},
    {"applies": "Linux ≥ 5.0", "text": "The single-queue block layer and its schedulers (noop, deadline, cfq) were removed; every request-based driver uses blk-mq."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Multi-Queue Block IO Queueing Mechanism (blk-mq)", "url": "https://docs.kernel.org/block/blk-mq.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Queue sysfs files", "url": "https://docs.kernel.org/block/queue-sysfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Block layer biovecs and bios", "url": "https://docs.kernel.org/block/biovecs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Device Mapper", "url": "https://docs.kernel.org/admin-guide/device-mapper/index.html", "kind": "kernel-doc"},
    {"title": "NVM Express Base Specification (queues and the submission/completion model)", "url": "https://nvmexpress.org/specifications/", "kind": "spec"},
    {"title": "Kernel source: block/blk-mq.c", "url": "https://github.com/torvalds/linux/blob/master/block/blk-mq.c", "kind": "source"},
    {"title": "Kernel source: block/blk-merge.c (splitting and merging bios)", "url": "https://github.com/torvalds/linux/blob/master/block/blk-merge.c", "kind": "source"},
    {"title": "LWN: The multiqueue block layer (2013)", "url": "https://lwn.net/Articles/552904/", "kind": "lwn"},
    {"title": "Bjørling, Axboe, Nellans, Bonnet: Linux Block IO: Introducing Multi-queue SSD Access on Multi-core Systems (SYSTOR 2013)", "url": "https://kernel.dk/blk-mq.pdf", "kind": "paper"},
    {"title": "lsblk(8)", "url": "https://man7.org/linux/man-pages/man8/lsblk.8.html", "kind": "man-page"}
  ]
}
---
## Question

:::when derived.storage.root_disk
The files under / on this machine live on {{derived:derived.storage.root_disk}}, a device of the kind the kernel calls {{derived:derived.storage.root_disk_kind}}. When a program reads a file whose pages are not in the page cache, the kernel has to ask that device for them. It does not hand the device a file name or a page: it builds *requests*, may split or merge them, and puts them in a queue that leads to the device. What is a request, why would a disk need more than one queue, and what does the kernel know about the device it is talking to?
:::else
The files under / are stored somewhere the kernel reaches through a block device. When a program reads a file whose pages are not in the page cache, the kernel has to ask that device for them. It does not hand the device a file name or a page: it builds *requests*, may split or merge them, and puts them in a hardware queue. What is a request, why would a disk need more than one queue, and what does the kernel know about the device it is talking to?
:::unknown
When a program reads a file whose pages are not in the page cache, the kernel has to ask a storage device for them. It does not hand the device a file name or a page: it builds *requests*, may split or merge them, and puts them in a hardware queue. What is a request, why would a disk need more than one queue, and what does the kernel know about the device it is talking to? Which disk holds / is {{derived:derived.storage.root_disk}} here; the devices this machine shows are listed below all the same.
:::end

## Why it exists

Storage devices differ by orders of magnitude. A spinning disk moves a head across a platter and answers in milliseconds, one request at a time from the head's point of view. A SATA SSD has no head but speaks a protocol designed for one, with room for 32 commands in flight. An NVMe drive sits on PCIe and accepts thousands of commands at once on many independent queues, answering in tens of microseconds. An SD card does one thing at a time and may pause for hundreds of milliseconds to erase flash. A virtual disk is whatever the hypervisor does with the request.

Filesystems, swap and the page cache should not have to care. So the kernel presents every one of them as the same thing, a *block device*: an array of sectors that can be read, written, flushed and discarded in any order. Underneath that interface the block layer does the work that depends on the device: it cuts transfers to sizes the device accepts, glues neighbouring ones together, keeps as many in flight as the device can take, and spreads that work across CPUs without making them contend for a lock.

## How it works

### Sectors and block sizes

The kernel addresses every block device in 512-byte *sectors*, whatever the hardware uses; the `size` and `stat` files in `/sys/block` count in them too. Two sizes come from the device:

- The *logical block size* is the smallest unit the device can address: a read or write must start and end on it. It is 512 bytes on most disks and 4096 on "4Kn" drives.
- The *physical block size* is what the device reads and writes internally. Many drives are "512e": they accept 512-byte writes but store 4 KiB sectors, so a write that covers part of a physical sector forces the drive to read, modify and rewrite the whole one. Partitions and filesystem blocks aligned to the physical size avoid that.

A filesystem picks its own block size, usually 4 KiB, which must be a multiple of the logical one. A page cache folio is one or more pages; for a disk this is all just a range of sectors.

### From a cache miss to a request

When the page cache misses, the filesystem maps the file range to sectors (the [filesystems lesson](lesson:filesystems-and-the-vfs) shows how) and builds a *bio*: one transfer in one direction, described as a start sector and a list of memory segments, here the folios that will receive the data. Then `submit_bio()` hands it to the block layer:

1. **Split.** A bio larger than the device's limits is cut: `max_sectors_kb` is the largest request the kernel builds (at most `max_hw_sectors_kb`, what the hardware allows), and there are limits on the number of memory segments and on boundaries some devices cannot cross.
2. **Plug and merge.** A task submitting several bios in a row holds them in a per-task *plug* list for a moment. A bio that continues an earlier one, sector after sector, is merged into it, so a sequential stream of 4 KiB pages leaves as a few large requests rather than many small ones.
3. **Allocate a request.** Each request takes a *tag* from a fixed set; when the tags run out, the submitting task sleeps until one is freed. `nr_requests` sets how many requests the queue may hold: with an I/O scheduler, by default twice the device's own queue depth (up to 256); without one, the device's depth itself.
4. **Schedule and dispatch.** An I/O scheduler, if one is selected, may hold and reorder requests (the [next lesson](lesson:io-schedulers)); then the driver translates each into the device's command and posts it.
5. **Complete.** The device signals completion, usually by an interrupt; the block layer ends the bio, the page cache marks the folios up to date, and the task that was waiting for them wakes.

### blk-mq: one queue per CPU, one or many per device

Until Linux 3.13 every device had one request queue protected by one lock. With a disk that did a few hundred operations per second that cost nothing; with flash doing hundreds of thousands, dozens of CPUs spent their time fighting over the lock and bouncing its cache line. The multi-queue block layer, *blk-mq*, replaced it with two levels:

- a *software queue* per CPU, where requests are staged and merged without any lock shared with other CPUs;
- one or more *hardware queues*, each mapped to a set of CPUs and to one queue of the device.

NVMe drives offer up to 65,535 submission and completion queue pairs, so the driver usually creates one per CPU, or one per group of CPUs when the drive has fewer. A request then travels from the submitting CPU to that CPU's queue on the device, and its completion interrupt comes back to the same CPU. A SATA disk behind AHCI has one queue of 32 commands; an SD card has one, effectively a single command at a time; a virtio disk has as many as the hypervisor offers, often one.

Not every block device has a request queue of its own. A device-mapper volume (LVM, dm-crypt) and software RAID (md) are *bio-based*: they remap each bio to the disks beneath them and submit it again, so the queueing happens on those disks. zram compresses pages in memory and finishes a bio as soon as it is submitted.

### What the device tells the kernel

Besides sizes, each queue directory under `/sys/block/<dev>/queue` exposes what the driver reported:

- `rotational`: whether the device has seek time. It changes the scheduler and writeback-throttling defaults, and some filesystems allocate differently. Virtual disks often report 1 whatever is behind them.
- `write_cache`: "write back" when the device has a volatile cache, so a completed write may still be lost on power failure unless the kernel asks for a flush; "write through" when it does not. `fua` says whether single writes can bypass the cache. The [writeback lesson](lesson:writeback-and-durability) builds durability on these two.
- `discard_max_bytes`: the largest discard (TRIM) the device accepts, 0 when it accepts none. Discards tell flash which blocks no longer hold data.

## On this machine

:::table block.queues columns=name,rotational,logical_block_bytes,physical_block_bytes,hw_queues,nr_requests,max_request_bytes,write_cache,fua,discard_max_bytes

:::evidence The disk that holds /
- derived.storage.root_disk
- derived.storage.root_disk_kind
- derived.storage.root_rotational
- derived.storage.root_hw_queues | Hardware queues blk-mq maps CPUs onto
- cpu.logical_count
- derived.storage.root_nr_requests | Requests the queue may hold
- derived.storage.root_max_request_bytes | Largest request the kernel builds
- derived.storage.root_write_cache
:::end

:::when derived.storage.root_disk_kind is nvme
/ is on an NVMe drive with {{derived:derived.storage.root_hw_queues}} hardware queues for {{obs:cpu.logical_count}} logical CPUs. When the counts are equal, every CPU submits to a queue of its own and takes its completions there, so CPUs never wait for each other to queue I/O; when the drive offers fewer queues, a few CPUs share each one. Its queue may hold {{derived:derived.storage.root_nr_requests}} requests.
:::else
:::when derived.storage.root_disk_kind is dm
/ is on a device-mapper volume ({{derived:derived.storage.root_disk}}), most likely an LVM logical volume or an encrypted one. It is bio-based, so it has no hardware queue of its own: each bio is remapped to the disk beneath and queued there. That disk is in the table above with its own queues.
:::else
/ is on {{derived:derived.storage.root_disk}}, whose hardware queue count is {{derived:derived.storage.root_hw_queues}} for {{obs:cpu.logical_count}} logical CPUs. A count of one is the shape of a SATA disk, an SD card or a single-queue virtual disk: every CPU stages its requests in its own software queue, and they all feed that one queue of {{derived:derived.storage.root_nr_requests}} requests. The device behind it takes only so many commands at once (32 for SATA with NCQ, about one for an SD card), so more queues would buy nothing.
:::unknown
/ is on {{derived:derived.storage.root_disk}}, but its hardware queue count is {{derived:derived.storage.root_hw_queues}} here: the queue settings could not be read.
:::end
:::unknown
Which disk holds / is {{derived:derived.storage.root_disk}} here. In a container the root is usually an overlay of directories on a filesystem of the host, and no block device holds it in this mount namespace; on a machine where `/sys` is restricted, the mount cannot be traced to a disk. The mechanism is the same on every machine: a device-mapper volume has no queue of its own, a SATA disk or SD card has one, and an NVMe drive usually one per CPU.
:::end

:::when derived.storage.root_rotational
The kernel treats this disk as rotational: it may have a seek time, so its scheduler and writeback defaults favour long sequential runs. A virtual disk often says this even when the host stores it on flash.
:::else
The kernel treats this disk as non-rotational: no seek time, so request order matters little to the device itself.
:::unknown
Whether the disk under / rotates is {{derived:derived.storage.root_rotational}} here.
:::end

:::when derived.env.kind is container
Atlas runs in a container. `/sys/block` is not separated per container, so the table above lists the host's devices and their settings; the container's own files are in a directory on one of them, and nothing here says which. Changing these settings needs privileges on the host.
:::else
The table lists every block device this kernel has, except unused loop and ram devices. Partitions do not appear: they share their disk's queue, and a request to a partition is a request to the disk with an offset added.
:::unknown
Whether Atlas runs in a container could not be determined; in one, `/sys/block` would list the host's devices rather than the container's.
:::end

## See it

:::diagram io-stack

The diagram follows a read or write from the system call down to the device, with the settings this machine uses for the disk that holds /.

## Predict

:::predict
A program writes 1 MiB to a file opened with `O_DIRECT` in a single `write()` call, so the page cache is bypassed and the whole range is contiguous on disk. How many requests reach the device?

- One request of 1 MiB: it was one system call.
- 1 MiB divided by the largest request size, rounded up.
- 256 requests, one per 4 KiB page.
:::reveal
1 MiB divided by the largest request the kernel builds, `max_sectors_kb`, rounded up.

:::when derived.storage.root_max_request_bytes
Here that limit is {{derived:derived.storage.root_max_request_bytes}} for the root disk, so 1 MiB leaves as one request if the limit is 1 MiB or more and as several otherwise. The device may still execute them in any order.
:::unknown
The limit is {{derived:derived.storage.root_max_request_bytes}} here; it is typically between 128 KiB and a few MiB.
:::end

The first answer would be right if the device took transfers of any size; the block layer splits every bio to the device's limits, and segment counts or alignment rules can split it further. The third confuses pages with bios: the program's buffer spans 256 pages, but they are segments of one bio, not separate ones. Even page cache writeback, which does submit page by page, sends large requests because consecutive bios are merged in the plug. A device-mapper volume adds its own boundaries (a striped volume splits at each stripe) before the disk's limits apply.
:::end

## Try it

```sh
lsblk -t                                   # per device: alignment, MIN-IO, PHY-SEC, LOG-SEC, ROTA, SCHED, RQ-SIZE, RA
grep . /sys/block/*/queue/{logical,physical}_block_size
ls /sys/block/*/mq                          # one directory per hardware queue
cat /sys/block/*/mq/0/cpu_list              # which CPUs submit to the first queue
```

On an NVMe drive the `mq` directory has one entry per queue, and each `cpu_list` names the CPUs mapped to it; on a SATA disk there is a single entry listing every CPU. A device-mapper volume has no `mq` directory at all.

## Limits

:::misconception Non-rotational means an SSD
`rotational` is what the driver reports. A virtual disk can claim either value whatever stores it, and a USB enclosure can hide a spinning disk behind a bridge that reports 0. The kernel uses the flag for defaults, not as a fact about the hardware.
:::end

:::misconception More hardware queues make one program faster
A thread submits from one CPU, so it uses one hardware queue. The queues remove contention between CPUs submitting at the same time; a single sequential reader goes no faster on 64 queues than on one. What lets one program use a fast device is keeping many requests in flight: asynchronous I/O, readahead or several threads.
:::end

:::misconception nr_requests is the device's queue depth
`nr_requests` bounds the requests the kernel allocates per hardware queue, which is where an I/O scheduler can sort them. How many commands the device accepts at once is a separate limit of the hardware and driver (32 for SATA NCQ, up to thousands per NVMe queue), which `/sys` does not report uniformly.
:::end

:::cannot
Which process issued which requests, and how long each one waited in the kernel versus inside the device: that needs tracing (blktrace, BPF tools such as biolatency), which needs privileges.

What the device does inside: the flash translation layer, write amplification, the drive's own cache and error counters. SMART data and NVMe logs need root, and Machine Atlas does not read them.

Partitions and the disks beneath a device-mapper or md volume: the tables list whole devices, and only the root mount is traced to its disk.
:::end

## Go deeper

Next, [I/O schedulers](lesson:io-schedulers) looks at what happens between the software queues and the device: whether requests are reordered, and for whom. In Explore, [block devices](component:block-devices) and [request queues and I/O schedulers](component:request-queues) list the devices found here. Planned lessons on buses and the device model, and on DMA and the IOMMU, cover how a driver finds its device and how the device reaches memory.
