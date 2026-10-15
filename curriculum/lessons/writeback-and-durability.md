---
{
  "id": "writeback-and-durability",
  "title": "When written data is safe",
  "subsystem": "storage",
  "kind": "core",
  "order": 5,
  "summary": "How dirty pages are written back (flusher threads per device, expiry and background thresholds, balance_dirty_pages throttling each writer to its device's pace, writeback throttling for reads), and what write, fsync, fdatasync, O_SYNC and O_DIRECT promise once the filesystem's journal and the device's volatile cache are counted, with the rename-and-fsync pattern.",
  "prerequisites": ["filesystems-and-the-vfs"],
  "uses": [
    "block.bdi",
    "block.queues",
    "derived.dirty.kernel_background_threshold_bytes",
    "derived.dirty.kernel_threshold_bytes",
    "derived.dirty.threshold_estimate_bytes",
    "derived.dirty.threshold_used_fraction",
    "derived.storage.root_disk",
    "derived.storage.root_fstype",
    "derived.storage.root_write_cache",
    "mem.meminfo.Dirty",
    "mem.meminfo.Writeback",
    "mem.sysctl.vm.dirty_bytes",
    "mem.sysctl.vm.dirty_expire_centisecs",
    "mem.sysctl.vm.dirty_ratio",
    "mem.sysctl.vm.dirty_writeback_centisecs",
    "mem.vmstat.nr_dirtied",
    "mem.vmstat.nr_written"
  ],
  "relevance": [
    {"when": "derived.storage.root_write_cache", "is": "write back", "note": "The root disk has a volatile write cache, so durability here depends on the kernel sending cache flushes."}
  ],
  "next": [],
  "deeper": ["planned:memory-cgroups"],
  "version_notes": [
    {"applies": "Linux ≥ 3.2", "text": "IO-less dirty throttling: balance_dirty_pages pauses a writer for a computed time instead of making it write pages back itself, and divides the dirty limit between devices by their measured writeback bandwidth."},
    {"applies": "Linux ≥ 4.10", "text": "Writeback throttling (wbt) limits how many background writes are in flight on a device when reads on it take longer than wbt_lat_usec."},
    {"applies": "Linux ≥ 4.13", "text": "Writeback errors are recorded per file with an errseq_t: every file descriptor open when an error occurs sees it once at its next fsync, instead of whichever caller happened to ask first."},
    {"applies": "Linux ≥ 6.2", "text": "Per-device writeback limits can be set in bytes (min_bytes, max_bytes) and with finer ratios, and strict_limit is exposed in sysfs."}
  ],
  "references": [
    {"title": "Linux kernel documentation: /proc/sys/vm (dirty_*, dirty_expire_centisecs, dirty_writeback_centisecs)", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Writeback throttling (wbt_lat_usec) in the queue sysfs files", "url": "https://docs.kernel.org/block/queue-sysfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Explicit volatile write back cache control (REQ_PREFLUSH, REQ_FUA)", "url": "https://docs.kernel.org/block/writeback_cache_control.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: ext4 journal (data=ordered, writeback, journal)", "url": "https://docs.kernel.org/filesystems/ext4/journal.html", "kind": "kernel-doc"},
    {"title": "fsync(2)", "url": "https://man7.org/linux/man-pages/man2/fsync.2.html", "kind": "man-page"},
    {"title": "open(2): O_SYNC, O_DSYNC and O_DIRECT", "url": "https://man7.org/linux/man-pages/man2/open.2.html", "kind": "man-page"},
    {"title": "sync_file_range(2)", "url": "https://man7.org/linux/man-pages/man2/sync_file_range.2.html", "kind": "man-page"},
    {"title": "rename(2)", "url": "https://man7.org/linux/man-pages/man2/rename.2.html", "kind": "man-page"},
    {"title": "Kernel source: mm/page-writeback.c (balance_dirty_pages)", "url": "https://github.com/torvalds/linux/blob/master/mm/page-writeback.c", "kind": "source"},
    {"title": "LWN: No-I/O dirty throttling (2011)", "url": "https://lwn.net/Articles/456904/", "kind": "lwn"},
    {"title": "LWN: Toward less-annoying background writeback (2016)", "url": "https://lwn.net/Articles/682582/", "kind": "lwn"},
    {"title": "LWN: PostgreSQL's fsync() surprise (2018)", "url": "https://lwn.net/Articles/752063/", "kind": "lwn"},
    {"title": "Pillai et al.: All File Systems Are Not Created Equal: On the Complexity of Crafting Crash-Consistent Applications (OSDI 2014)", "url": "https://www.usenix.org/conference/osdi14/technical-sessions/presentation/pillai", "kind": "paper"}
  ]
}
---
## Question

:::when derived.dirty.kernel_threshold_bytes
Right now {{obs:mem.meminfo.Dirty}} of file data on this machine exists only in memory, and {{obs:mem.meminfo.Writeback}} is on its way to a device. The kernel lets dirty data grow to {{derived:derived.dirty.kernel_threshold_bytes}} before it makes writers wait. A program's `write()` returned long ago for all of it. If the power failed now, what would be lost, what decides when each page reaches its disk, and what exactly does a program have to call to know its data is safe?
:::unknown
A program's `write()` returns as soon as the data is in the page cache; the data reaches storage later. If the power failed in between, what would be lost, what decides when each page reaches its disk, and what exactly does a program have to call to know its data is safe? This machine's dirty threshold is {{derived:derived.dirty.kernel_threshold_bytes}} here.
:::end

## Why it exists

Deferring writes is what makes them fast. A program's `write()` costs a memory copy; the kernel collects the dirty pages, lets later writes overwrite earlier ones in memory, chooses disk blocks for whole runs at once (delayed allocation), and sends large, ordered requests when it suits the device. A temporary file that is deleted within seconds may never be written at all.

Each of those benefits is also a risk. Dirty data is lost in a crash. It occupies memory that cannot be reclaimed until it is written. If programs dirty memory faster than a device can take it, the backlog grows until memory runs out, and the device's queue fills with writes that make every read wait. So the kernel must bound how much is dirty, how old it gets, and how fast each writer may go; and it must give programs a way to say "this, now, durably" for the data they cannot lose.

## How it works

### Flushers and the thresholds

Each backing device (*BDI*, one per block device, plus one per network or FUSE filesystem) has a writeback context, served by a pool of kernel worker threads, the *flushers*. They write back for four reasons:

- **Age.** Every `dirty_writeback_centisecs` (500, 5 s) the flushers wake and write data that has been dirty longer than `dirty_expire_centisecs` (3000, 30 s).
- **Amount.** When dirty data passes the background threshold, they write until it falls below, whatever its age.
- **Request.** `fsync()`, `sync()` and `syncfs()` write what they cover at once.
- **Memory.** Reclaim needs the frames, as the [reclaim lesson](lesson:reclaim-and-swap) showed.

The thresholds come from `vm.dirty_background_ratio` and `vm.dirty_ratio` (10 and 20 by default) as a percentage of *dirtyable* memory, roughly free memory plus the file cache, or from the `_bytes` variants when those are set. The kernel recomputes them all the time and publishes its current values in `/proc/vmstat` as `nr_dirty_background_threshold` and `nr_dirty_threshold`.

### Throttling writers: balance_dirty_pages

Every task that dirties pages passes through `balance_dirty_pages()` now and then. Below the midpoint between the two thresholds it runs free. Above it, the kernel computes a pause and puts the task to sleep for it, so that the task's dirtying rate matches the rate its device has been writing back; the closer dirty data is to the dirty threshold, the longer the pauses, until at the threshold writers effectively wait for writeback. This is why a large copy runs at memory speed for its first gigabytes and then suddenly at disk speed.

The limit is divided between devices. Each BDI's share follows its recent share of completed writeback, so a fast NVMe drive gets most of it and a slow USB stick little, and one slow device cannot fill the whole limit with dirty pages that would take minutes to write. `min_ratio` and `max_ratio` bound a device's share; `strict_limit`, set for FUSE, throttles a device to its share even when the global total is low. With cgroup v2, writeback is also charged to the cgroup that dirtied the pages, and each cgroup is throttled against its own memory.

### Writeback throttling: keeping reads responsive

A burst of background writes can fill a device's queue, and a read issued behind them waits for all of them. *Writeback throttling* (wbt) watches the latency of reads on each device; when it exceeds `wbt_lat_usec` (by default 2 ms on non-rotational devices, 75 ms on rotational ones) it shrinks the number of background writes allowed in flight, and grows it again when reads are fast. It is aimed at background writeback; writes a program is waiting for are given more room.

### What each call promises

- **`write()`**: the data is in the page cache. Nothing is promised about the device.
- **`fsync(fd)`**: the file's data and the metadata needed to read it back (size, block mapping) are on stable storage before it returns, including a cache flush on a device with a volatile cache. It does not cover the directory entry that names the file.
- **`fdatasync(fd)`**: the same, minus metadata that is not needed to read the data back, such as the modification time, so it can avoid a journal commit when only data changed in place.
- **`O_SYNC`, `O_DSYNC`**: every `write()` behaves as if followed by `fsync()` or `fdatasync()`.
- **`O_DIRECT`**: the transfer bypasses the page cache. It is *not* a durability promise: the data may sit in the device's volatile cache, and allocating writes leave metadata that is only durable after `fsync()`. Databases combine it with `O_DSYNC` or `fdatasync()`.
- **`sync_file_range()`**: starts or waits for writeback of a range, with no metadata and no cache flush; a scheduling hint, not a durability call.

If writeback of a page fails, the error is reported to `fsync()` (since 4.13, once to each file descriptor that was open), and the page may be marked clean anyway: a second `fsync()` that succeeds does not mean the data was written. Programs that cannot lose data must treat a failed `fsync()` as lost data.

### The device's cache, flush and FUA

Many drives acknowledge a write as soon as it is in their own RAM. The kernel knows this from `write_cache`: with "write back", durability needs a *flush* (`REQ_PREFLUSH`), which makes the device write out its cache, or a *Force Unit Access* write (`REQ_FUA`), which goes to stable media by itself. A journaling filesystem's commit uses both: flush, so the data and journal blocks before the commit record are stable, then the commit record with FUA. Without FUA support the block layer sends a second flush after the write. With "write through" both are unnecessary and the block layer drops them. A drive with power-loss protection can report write through, or report write back and flush in microseconds; a drive that acknowledges flushes it has not done breaks every guarantee above.

### The journal, and replacing a file safely

A filesystem must not be left inconsistent by a crash halfway through an update that touches several blocks. ext4 and xfs write metadata changes to a *journal* first, and commit them as a unit (ext4 every `commit=` seconds, 5 by default, or at fsync). ext4's `data=` option says how file data relates to that:

- `data=ordered` (the default): data blocks are written before the metadata that points at them is committed, so after a crash a file never shows blocks of someone else's old data.
- `data=writeback`: only metadata is journaled; a crash can leave a file whose new size includes blocks never written.
- `data=journal`: data goes through the journal too, written twice, which makes data and metadata change together.

xfs journals only metadata and updates a file's size on disk only after its data is written; btrfs never overwrites in place and keeps a log tree to make fsync fast.

To replace a file so that a crash leaves either the old or the new version: write the new content to a temporary file in the same directory, `fsync()` it, `rename()` it over the old name, then `fsync()` the directory so the rename itself is durable. Skipping the first fsync can leave the new name pointing at an empty or partial file on some filesystems; ext4 detects this pattern and starts writing the data at the rename, but portable programs cannot rely on it.

## On this machine

:::evidence Dirty data and the kernel's thresholds
- mem.meminfo.Dirty
- mem.meminfo.Writeback
- derived.dirty.kernel_background_threshold_bytes | Where flushers start, as the kernel computed it
- derived.dirty.kernel_threshold_bytes | Where writers are held, as the kernel computed it
- derived.dirty.threshold_used_fraction | Dirty and writeback data against that limit
- derived.dirty.threshold_estimate_bytes | Machine Atlas's own estimate (page cache lesson)
- mem.sysctl.vm.dirty_ratio
- mem.sysctl.vm.dirty_bytes | Replaces dirty_ratio when non-zero
- mem.sysctl.vm.dirty_expire_centisecs | Age after which dirty data is written (1/100 s)
- mem.sysctl.vm.dirty_writeback_centisecs | Flusher wake-up interval (1/100 s)
- mem.vmstat.nr_dirtied | Pages dirtied since boot
- mem.vmstat.nr_written | Pages written back since boot
:::end

:::when derived.dirty.threshold_used_fraction
Dirty and in-flight data now stand at {{derived:derived.dirty.threshold_used_fraction}} of the kernel's dirty threshold of {{derived:derived.dirty.kernel_threshold_bytes}}; flushers write in the background above {{derived:derived.dirty.kernel_background_threshold_bytes}}, and writers start to be paused halfway between the two. The [page cache lesson](lesson:page-cache) estimated the same threshold from meminfo at {{derived:derived.dirty.threshold_estimate_bytes}}; the kernel's own figure differs because it subtracts reserves the estimate cannot see, and because the two were computed at different instants from memory that changes every moment.
:::unknown
The share of the dirty threshold in use is {{derived:derived.dirty.threshold_used_fraction}} here: the kernel's thresholds could not be read. With the default ratios, background writeback starts at 10% and writers are held at 20% of the memory that is free or file cache.
:::end

Since boot, pages have been dirtied {{obs:mem.vmstat.nr_dirtied|count}} times and written back {{obs:mem.vmstat.nr_written|count}} times. The first can exceed the second for good reasons: data deleted or truncated before it was written, and pages dirtied again before writeback reached them, never need writing. Data is written once it has been dirty for {{obs:mem.sysctl.vm.dirty_expire_centisecs}} hundredths of a second, checked every {{obs:mem.sysctl.vm.dirty_writeback_centisecs}}.

### Each device's share and its cache

:::table block.bdi columns=name,min_ratio,max_ratio,max_bytes,strict_limit

:::table block.queues columns=name,write_cache,fua,wbt_latency_us

:::when derived.storage.root_write_cache is write back
The disk under / ({{derived:derived.storage.root_disk}}) reports a volatile write cache. A write it has acknowledged may still be in its RAM, so every `fsync()` there costs a cache flush, and every journal commit a flush and a FUA write (or a second flush where `fua` is false). On a SATA SSD a flush takes from microseconds to milliseconds; on a spinning disk it can take tens of milliseconds, which is why a program calling fsync after every small write runs so slowly there.
:::else
The disk under / ({{derived:derived.storage.root_disk}}) reports no volatile write cache ("write through"): a completed write is on stable media, and the block layer drops flushes as unnecessary. Enterprise drives with power-loss protection, many cloud volumes and SD cards report this. fsync still has to wait for the writes themselves.
:::unknown
The write cache of the disk under / is {{derived:derived.storage.root_write_cache}} here. In a container, durability depends on the host's disk and its cache, which the container can neither see nor change; a volatile cache is common on consumer drives and absent on drives with power-loss protection.
:::end

:::when derived.storage.root_fstype is ext4
/ is ext4. Unless its mount options (in the [filesystems lesson](lesson:filesystems-and-the-vfs)) name another `data=` mode, it journals metadata in ordered mode, the default, which the kernel does not print: file data is written before the metadata that refers to it commits, at least every `commit=` seconds (5 when not shown).
:::else
:::when derived.storage.root_fstype is overlay
/ is an overlay in a container. An `fsync()` there is passed to the file in the writable upper layer, on a filesystem of the host, so its guarantees are those of the host's filesystem and disk. Container runtimes sometimes mount overlays with `volatile`, which turns fsync into nothing at all; that option is not among those Machine Atlas keeps.
:::else
/ is {{derived:derived.storage.root_fstype}}. xfs journals only metadata and extends a file's size on disk only after the data is written; btrfs writes every change to new blocks and keeps a log tree for fsync; f2fs logs its writes. All of them make fsync durable through the same flush and FUA requests.
:::unknown
/ is {{derived:derived.storage.root_fstype}}, which this lesson does not single out.
:::end
:::unknown
The filesystem of / is {{derived:derived.storage.root_fstype}} here.
:::end

## Predict

:::predict
Two programs each write 100 MiB to new files on the root filesystem. The first then calls `fsync()` and it returns; the second calls nothing and exits. Ten seconds later the power fails. What survives?

- Both files, completely: 10 s is plenty of time to write 200 MiB.
- The first file completely; of the second, nothing or only part.
- Neither file is safe without unmounting.
:::reveal
The first file completely, if the drive honours flushes: `fsync()` returned only after the data, the metadata needed to read it back and a cache flush were done. Its directory entry is durable too in practice on ext4 and xfs, because the journal commit that fsync forces includes the file's creation; strictly, POSIX asks for an fsync of the directory as well.

The second file depends on the thresholds. Its data is younger than the 30-second expiry, so periodic writeback has not touched it. If 100 MiB pushed dirty data above the background threshold, flushers started writing at once and part of it, possibly all, reached the disk; if not, nothing did, and after the crash the file may be missing, or present but empty, depending on whether the journal committed its creation. Either way no part of it is guaranteed.

:::when derived.dirty.kernel_background_threshold_bytes
On this machine the second file adds 100 MiB to the {{obs:mem.meminfo.Dirty}} already dirty, and flushers start before the 30 seconds are up only if that sum passes the background threshold of {{derived:derived.dirty.kernel_background_threshold_bytes}}.
:::unknown
This machine's background threshold is {{derived:derived.dirty.kernel_background_threshold_bytes}} here; on most machines it is far above 100 MiB, so nothing of the second file would be written in 10 s.
:::end

The first answer confuses "could be written" with "is written": nothing forces writeback before the expiry age or the background threshold. The third is too pessimistic: fsync is exactly the promise that unmounting would give, for one file.
:::end

## Try it

```sh
grep -E '^(nr_dirty|nr_writeback|nr_dirty_threshold|nr_dirty_background_threshold) ' /proc/vmstat
dd if=/dev/zero of="$HOME/atlas-dd.tmp" bs=1M count=512 status=progress   # watch the rate fall
grep -E '^(Dirty|Writeback):' /proc/meminfo; sync; grep -E '^(Dirty|Writeback):' /proc/meminfo
dd if=/dev/zero of="$HOME/atlas-dd.tmp" bs=4k count=1000 oflag=dsync      # every write waits for the device
rm "$HOME/atlas-dd.tmp"
```

The first `dd` runs at memory speed until dirty data nears the threshold, then slows to the disk's speed; `Dirty` grows, then falls when `sync` forces writeback. The last `dd` makes each 4 KiB write durable before the next, and on a disk with a volatile cache it shows what a cache flush costs. Use a directory on a disk, not `/tmp` if that is tmpfs.

## Limits

:::misconception O_DIRECT writes are durable
`O_DIRECT` only bypasses the page cache. The data can still sit in the device's volatile cache, and a write that allocated blocks or extended the file has metadata that is not durable until `fsync()`. Only `O_SYNC`, `O_DSYNC` or an explicit fsync make it durable.
:::end

:::misconception fsync on a new file makes its name durable
fsync covers the file's data and the metadata needed to read it, not the directory entry. A newly created or renamed file needs `fsync()` on its directory too; ext4 and xfs often commit it along the way, but that is a property of their journals, not a promise.
:::end

:::misconception Retrying a failed fsync makes the data safe
After a writeback error the kernel may mark the pages clean and drop them, so a second fsync has nothing left to write and can succeed. The data from before the error is lost; the program must write it again from its own copy.
:::end

:::cannot
Which files hold dirty pages, and how much each device has dirty or under writeback: the per-file view needs `cachestat()` or mincore on each file, and the per-device figures are in debugfs, readable only by root.

Whether the device really honours flushes, or how its firmware orders writes: no interface reports that; only a power-cut test shows it.

Writeback per cgroup: cgroup v2 charges dirty pages to the cgroup that wrote them and throttles each against its own memory; Machine Atlas collects the cgroup's memory limits but not its dirty or writeback figures.
:::end

## Go deeper

This is the last lesson of the storage track. In Explore, [writeback and backing devices](component:writeback) shows each device's share of the dirty limit, and [filesystems](component:filesystems) the mounts this applies to. The planned lesson on memory cgroups covers writeback charged to cgroups, and how a container's memory limit lowers its dirty limits with it.
