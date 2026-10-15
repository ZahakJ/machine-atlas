---
{
  "id": "page-cache",
  "title": "Why free memory is nearly zero and that is fine",
  "subsystem": "memory",
  "kind": "core",
  "order": 7,
  "summary": "The page cache keeps file contents in otherwise idle RAM; how reads, mmap, readahead and deferred writeback use it, and why MemAvailable rather than MemFree says whether memory is short.",
  "prerequisites": ["anonymous-and-file-pages"],
  "uses": [
    "derived.cgroup.memory_limited",
    "derived.dirty.background_threshold_estimate_bytes",
    "derived.dirty.threshold_estimate_bytes",
    "derived.mem.available_fraction",
    "derived.mem.file_cache_bytes",
    "derived.zones.low_bytes",
    "mem.cgroup.memory.current",
    "mem.cgroup.memory.max",
    "mem.meminfo.Active_file",
    "mem.meminfo.Buffers",
    "mem.meminfo.Cached",
    "mem.meminfo.Dirty",
    "mem.meminfo.Inactive_file",
    "mem.meminfo.MemAvailable",
    "mem.meminfo.MemFree",
    "mem.meminfo.MemTotal",
    "mem.meminfo.SReclaimable",
    "mem.meminfo.Shmem",
    "mem.meminfo.Writeback",
    "mem.sysctl.vm.dirty_background_bytes",
    "mem.sysctl.vm.dirty_background_ratio",
    "mem.sysctl.vm.dirty_bytes",
    "mem.sysctl.vm.dirty_ratio",
    "mem.sysctl.vm.vfs_cache_pressure",
    "mem.vmstat.pgpgin",
    "mem.vmstat.pgpgout"
  ],
  "relevance": [
    {"when": "derived.cgroup.memory_limited", "is": true, "note": "Atlas runs under a cgroup memory limit, which MemAvailable does not take into account."}
  ],
  "next": ["reclaim-and-swap"],
  "deeper": ["block-devices", "filesystems-and-the-vfs", "writeback-and-durability", "planned:slab-allocator"],
  "version_notes": [
    {"applies": "Linux ≥ 3.14", "text": "MemAvailable was added to /proc/meminfo. Before it, tools estimated available memory as MemFree + Cached, which counts shmem as droppable and ignores reclaimable slab."},
    {"applies": "Linux ≥ 4.20", "text": "The page cache of each file is indexed by an XArray; before, by a radix tree with the same shape. Since 5.16 its entries are folios, which may span several pages, rather than single pages."},
    {"applies": "Linux ≥ 6.5", "text": "The cachestat() system call reports, for a range of a file, how many pages are cached, dirty, under writeback and recently evicted, without mapping the file as mincore() requires."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Concepts overview, Page cache", "url": "https://docs.kernel.org/admin-guide/mm/concepts.html#page-cache", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Overview of the Linux Virtual File System (struct address_space and its operations)", "url": "https://docs.kernel.org/filesystems/vfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/vm (dirty_ratio, dirty_background_ratio, dirty_expire_centisecs, vfs_cache_pressure)", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: The /proc filesystem, /proc/meminfo", "url": "https://docs.kernel.org/filesystems/proc.html#meminfo", "kind": "kernel-doc"},
    {"title": "Commit 34e431b0ae39: /proc/meminfo: provide estimated available memory", "url": "https://github.com/torvalds/linux/commit/34e431b0ae398fc54ea69ff85ec700722c9da773", "kind": "source"},
    {"title": "Kernel source: si_mem_available() in mm/show_mem.c", "url": "https://github.com/torvalds/linux/blob/master/mm/show_mem.c", "kind": "source"},
    {"title": "mincore(2)", "url": "https://man7.org/linux/man-pages/man2/mincore.2.html", "kind": "man-page"},
    {"title": "posix_fadvise(2)", "url": "https://man7.org/linux/man-pages/man2/posix_fadvise.2.html", "kind": "man-page"},
    {"title": "cachestat(2)", "url": "https://man7.org/linux/man-pages/man2/cachestat.2.html", "kind": "man-page"}
  ]
}
---
## Question

This machine has {{obs:mem.meminfo.MemTotal}} of RAM, and only {{obs:mem.meminfo.MemFree}} of it is free: holding nothing at all. Yet the kernel estimates that {{obs:mem.meminfo.MemAvailable}} could be handed to new programs without swapping, {{derived:derived.mem.available_fraction|percent}} of the total. The two figures describe the same memory at the same moment. Which of them tells you whether this machine is running out, and why would a kernel keep so little memory free on purpose?

## Why it exists

Storage is slow next to RAM: an SSD answers in tens of microseconds and a disk in milliseconds, where RAM answers in about a hundred nanoseconds. Programs read the same files again and again (executables, libraries, configuration, the data they work on), so a copy kept in memory saves the slow read every time it is reused.

Keeping that copy costs nothing as long as nothing else wants the frame. An empty frame does no work; a frame holding a clean copy of a file still does, and can be emptied in an instant when something needs it, because the file is still on storage. So Linux sends every file read and write through memory and keeps the result until the frame is wanted. Free memory falls towards a small reserve and stays there, and that is the design working, not a shortage.

## How it works

### One cache per file

Each open file's inode has an *address space* (`struct address_space`), an index from file offset to the memory holding that part of the file. Since Linux 4.20 the index is an XArray, and since 5.16 its entries are *folios*: one or more contiguous pages managed as a unit. Together these indexes are the *page cache*.

`read()` looks up the folio covering the requested offset. On a hit it copies the bytes to the caller's buffer; on a miss it allocates a folio, inserts it, asks the filesystem to fill it from storage, waits, and then copies. A file mapped with `mmap` uses the very same folios: a page fault on the mapping finds the folio in the cache and maps it into the process's page table. One copy of the data serves both paths, which is why a library mapped by a hundred processes and read by a backup tool occupies its memory once.

### Readahead

A miss that leads to a slow device read is expensive, so the kernel tries not to have them. When reads of a file look sequential, it reads beyond what was asked: a first window sized from the request, then larger windows as the pattern continues, up to the device's limit (`read_ahead_kb`, often 128 KiB). It also marks a page inside each window; reaching that page starts the next read in the background, so a program streaming a file rarely waits. Random access gets no readahead.

### Writes are deferred

`write()` copies data into the cache, marks those folios *dirty*, and returns. The data is not on storage yet. Flusher threads write dirty pages back later, for three reasons: data has been dirty longer than `dirty_expire_centisecs` (30 seconds by default); dirty memory has passed `dirty_background_ratio` percent of dirtyable memory, roughly free plus file cache; or reclaim needs the frames. If programs dirty memory faster than storage takes it, the kernel slows them down. Above the midpoint between the background threshold and `dirty_ratio` it pauses writing tasks in proportion to how far over they are, and at `dirty_ratio` a writer waits for writeback. This is why `write()` is fast and why a full disk cache can make a large copy suddenly crawl.

A program that needs the data on storage calls `fsync()`, which writes the file's dirty pages and waits for the device to confirm. `O_DIRECT` bypasses the cache altogether: the transfer goes between the device and the program's buffer, which databases that manage their own cache use to avoid holding everything twice.

### Buffers and the kernel's other caches

`Buffers` is the page cache of block devices themselves rather than of files: filesystem metadata that some filesystems read through the device, and anything read from a device node directly. It is usually small.

The kernel also caches the results of path lookups (*dentries*) and file metadata (*inodes*) in slab memory, counted in `SReclaimable`. Under pressure, shrinkers free unused entries. `vfs_cache_pressure` (default 100) sets how hard reclaim pushes on these caches relative to the page cache: lower keeps them longer, higher frees them sooner.

### What MemAvailable estimates

MemFree counts frames holding nothing. A program that needs memory does not need empty frames; it needs frames the kernel can empty quickly without swapping. Since Linux 3.14 the kernel publishes its own estimate of that, MemAvailable. Today it is computed as:

1. MemFree minus the reserves the allocator keeps for itself (the high watermarks and the zones' lowmem reserves);
2. plus the file pages on the LRU lists (`Active(file) + Inactive(file)`), minus the smaller of half of them and the sum of the zones' low watermarks, on the assumption that some cache must stay or the system starts to thrash;
3. plus reclaimable slab and other reclaimable kernel memory, minus the same kind of margin, because some of those objects are in use.

Shmem is left out, because it is on the anonymous lists and cannot be dropped. Dirty file pages are counted although they need writeback first, and nothing in the formula knows whether the cached data is about to be needed again. It is a fixed formula, not a prediction.

## On this machine

:::evidence Free, cached and available
- mem.meminfo.MemTotal
- mem.meminfo.MemFree | Frames holding nothing
- mem.meminfo.MemAvailable | The kernel's estimate of what new work could get
- mem.meminfo.Cached | Page cache, including shmem
- mem.meminfo.Buffers
- mem.meminfo.Shmem | Shmem, in Cached but not droppable
- derived.mem.file_cache_bytes
- mem.meminfo.Active_file
- mem.meminfo.Inactive_file
- mem.meminfo.SReclaimable
- derived.zones.low_bytes | Sum of the zones' low watermarks
:::end

The gap between MemFree and MemAvailable here is mostly the file cache: {{derived:derived.mem.file_cache_bytes}} of file contents the kernel kept after use. Of that, the file LRU lists ({{obs:mem.meminfo.Active_file}} active, {{obs:mem.meminfo.Inactive_file}} inactive) enter the estimate, together with {{obs:mem.meminfo.SReclaimable}} of reclaimable slab. The {{obs:mem.meminfo.Shmem}} of shmem inside Cached is excluded.

:::when derived.zones.low_bytes
Each of the two, file pages and reclaimable slab, is reduced by a margin of at most {{derived:derived.zones.low_bytes}}, the zones' low watermarks added up.
:::unknown
Each of the two, file pages and reclaimable slab, is reduced by a margin no larger than the zones' low watermarks, which cannot be read here ({{derived:derived.zones.low_bytes}}).
:::end

Recomputing the formula from these chips will not land exactly on MemAvailable: the reserves of step 1 are not exported to user space, and every field was read at a slightly different instant.

:::when derived.cgroup.memory_limited
:::when mem.cgroup.memory.max is max
Atlas runs in a cgroup whose `memory.high` boundary is set, so its processes are throttled and pushed into reclaim above that boundary whatever MemAvailable says. That figure describes the whole kernel, not this cgroup.
:::else
Atlas runs in a cgroup limited to {{obs:mem.cgroup.memory.max}}, which currently charges {{obs:mem.cgroup.memory.current}}, including page cache its processes caused. For programs in this cgroup the limit, not MemAvailable, decides when reclaim starts: the kernel's estimate describes the whole machine, and here it may describe a host this environment cannot use.
:::end
:::else
Atlas's cgroup has no memory limit of its own, so for it MemAvailable is the relevant estimate, although an ancestor cgroup could still impose one that Machine Atlas does not read.
:::unknown
Whether Atlas runs under a cgroup memory limit could not be determined here (cgroup v1, or the cgroup files are unreadable). Where such a limit exists, it decides when reclaim starts for the processes inside it, whatever MemAvailable says for the machine.
:::end

### Writes in flight

:::evidence Dirty data and writeback
- mem.meminfo.Dirty
- mem.meminfo.Writeback
- mem.sysctl.vm.dirty_background_ratio
- mem.sysctl.vm.dirty_background_bytes | Replaces dirty_background_ratio when non-zero
- mem.sysctl.vm.dirty_ratio
- mem.sysctl.vm.dirty_bytes | Replaces dirty_ratio when non-zero
- derived.dirty.background_threshold_estimate_bytes | Where background writeback starts (estimate)
- derived.dirty.threshold_estimate_bytes | Where writers are held (estimate)
- mem.vmstat.pgpgin | KiB read from block devices since boot
- mem.vmstat.pgpgout | KiB written to block devices since boot
- mem.sysctl.vm.vfs_cache_pressure
:::end

{{obs:mem.meminfo.Dirty}} of file data is written in memory and not yet on storage, and {{obs:mem.meminfo.Writeback}} is being written right now. A few megabytes of dirty data on an idle machine is normal: it is the last half minute of writes waiting for the flusher.

Machine Atlas estimates the two thresholds the way the kernel computes them. A non-zero vm.dirty_bytes (here {{obs:mem.sysctl.vm.dirty_bytes}}) is the dirty threshold itself, rounded up to whole pages. Otherwise the threshold is dirty_ratio, {{obs:mem.sysctl.vm.dirty_ratio}}, of *dirtyable memory*: MemFree minus the zones' high watermarks summed (never below zero), plus `Active(file)` and `Inactive(file)`, with no slab. The background threshold comes the same way from vm.dirty_background_bytes ({{obs:mem.sysctl.vm.dirty_background_bytes}}) or dirty_background_ratio ({{obs:mem.sysctl.vm.dirty_background_ratio}}), and when it comes out at or above the dirty threshold the kernel uses half the dirty threshold instead.

:::when derived.dirty.threshold_estimate_bytes
Here background writeback starts at about {{derived:derived.dirty.background_threshold_estimate_bytes}} of dirty data and writers are held at about {{derived:derived.dirty.threshold_estimate_bytes}}. Both are estimates of the global limits: the kernel also subtracts each zone's lowmem reserve, gives real-time writers a quarter more, and applies per-cgroup and per-device limits below these, none of which is counted here.
:::else
Here background writeback starts at about {{derived:derived.dirty.background_threshold_estimate_bytes}} of dirty data and writers are held at about {{derived:derived.dirty.threshold_estimate_bytes}}. Both are estimates of the global limits: the kernel also subtracts each zone's lowmem reserve, gives real-time writers a quarter more, and applies per-cgroup and per-device limits below these, none of which is counted here.
:::unknown
The estimates cannot be made here: the inputs they need, such as the zone watermarks or the meminfo figures, could not be read (the inspector names which). On a typical machine with the default ratios, background writeback starts at a few hundred megabytes to a few gigabytes of dirty data, and writers are held at twice that.
:::end

Since boot, block devices have delivered {{obs:mem.vmstat.pgpgin}} KiB and absorbed {{obs:mem.vmstat.pgpgout}} KiB. These counts include swap traffic and readahead, so they are not file reads and writes as programs issued them.

## Predict

:::predict
A program reads a 16 MiB file from start to end, then reads it again. How will the second read's time compare with the first's?

- About the same: the same work is done twice.
- Much faster: the second read is a copy from memory.
- Slower: the first read filled memory, so the second must make room.
:::reveal
Usually much faster. The first read missed in the cache, so each readahead window waited for the device; the second found every folio in the cache and only copied memory, which takes well under a millisecond per megabyte.

The times can come out about the same in three situations, each with a reason:

- The file was already cached before the first read, because it had just been written or read. Then both reads are copies from memory. That is why the experiment below first asks the kernel to drop the file's pages and checks that it did.
- The file is on tmpfs. Its pages are the only copy, so nothing is read from a device either time.
- The program opens the file with `O_DIRECT`. Both reads go to the device, and nothing is kept.

On a fast NVMe drive the cold read of 16 MiB itself takes only milliseconds, so the difference may be a few times rather than a hundred. A slower second read would mean the cached pages were evicted in between, which happens only under heavy memory pressure or a tight cgroup limit.
:::end

## Try it

:::experiment page-cache-residency

The experiment writes a 16 MiB file in Atlas's cache directory, asks `mincore` how much of it is in the page cache, advises the kernel to drop it with `posix_fadvise(POSIX_FADV_DONTNEED)`, and reads it twice, timing both reads, before deleting it. On a disk filesystem expect the file fully resident after the write and `fsync`, almost none of it after the advice, and fully resident again after the cold read. On tmpfs the advice drops nothing, because there is nowhere to reread the data from. If only part of the file leaves the cache, some pages were busy at that instant: advice is allowed to skip them.

Without the experiment, the effect of writes is easy to watch with read-only commands:

```sh
grep -E '^(MemFree|MemAvailable|Cached|Dirty|Writeback):' /proc/meminfo
free -h
```

`free` prints MemAvailable in its "available" column and Cached plus Buffers (and reclaimable slab) as "buff/cache". Run the first command while copying a large file, and `Dirty` grows, then falls in steps as the flusher writes it back.

## Limits

:::misconception Linux ate my RAM
Memory holding cached file data is not lost. The kernel gives it back as soon as anything needs it, by dropping clean pages, which costs nothing but a reread if the data is wanted again. The figure that answers "how much can programs still get" is MemAvailable, here {{obs:mem.meminfo.MemAvailable}}, not MemFree.
:::end

:::misconception Dropping caches frees memory for applications
Writing to `/proc/sys/vm/drop_caches` (root only) turns cached file pages into free ones. Applications could already have had those frames: reclaim would have handed them over on demand. All the drop achieves is that the next reads of those files go to storage again. It is a tool for benchmarking cold starts, not for tuning.
:::end

:::misconception Written data is on disk when write() returns
`write()` returns once the data is in the page cache. It reaches storage within about half a minute, or later if the device is slow, and a crash in between loses it. Only `fsync()` (or `O_SYNC`, or `O_DIRECT` together with a device cache flush) says the data is durable.
:::end

:::cannot
meminfo counts the page cache as a whole; it does not say which files are cached. That needs a per-file question: `mincore()` on a mapping of the file, as the experiment does for its own file, or `cachestat()` on Linux 6.5 and later. Doing it for every file would mean opening every file.

MemAvailable is the kernel's estimate for the whole machine. It ignores cgroup limits, so a process in a limited cgroup can run short while it looks large, and it cannot know which cached pages are about to be needed again, or how much a workload is about to dirty.

`posix_fadvise(POSIX_FADV_DONTNEED)` is advice: the kernel may write dirty pages back first, but skips pages that are still dirty, under writeback, mapped by a process or otherwise in use. Residency after the advice is a measurement, not a guarantee.
:::end

## Go deeper

Next, [reclaim and swap](lesson:reclaim-and-swap) shows what happens when an allocation finds the free reserve exhausted and the cache has to give way. In Explore, the [page cache](component:page-cache), [filesystems](component:filesystems) and [writeback](component:writeback) components connect this lesson to storage. The storage track starts from here: [where a read goes when the cache misses](lesson:block-devices) follows a miss down to the device, [how a path becomes blocks on a disk](lesson:filesystems-and-the-vfs) covers the filesystem and readahead, and [when written data is safe](lesson:writeback-and-durability) the dirty thresholds, writeback and what fsync promises. The planned slab allocator lesson covers the dentry and inode caches.
