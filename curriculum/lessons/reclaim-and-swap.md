---
{
  "id": "reclaim-and-swap",
  "title": "Finding a frame when none are free",
  "subsystem": "memory",
  "kind": "core",
  "order": 8,
  "summary": "How the kernel takes frames back when free memory runs low: watermarks, kswapd and direct reclaim, the LRU lists or Multi-Gen LRU that rank pages, swappiness, swap and the swap cache, refault detection, and what changes without swap.",
  "prerequisites": ["page-cache"],
  "uses": [
    "derived.psi.available",
    "derived.reclaim.allocstalls",
    "derived.reclaim.lru_kind",
    "derived.swap.compressed",
    "derived.swap.configured",
    "derived.swap.total_bytes",
    "derived.swap.used_fraction",
    "derived.zones.high_bytes",
    "derived.zones.low_bytes",
    "derived.zones.min_bytes",
    "kernel.uptime_seconds",
    "mem.lru_gen.enabled",
    "mem.meminfo.AnonPages",
    "mem.meminfo.MemTotal",
    "mem.meminfo.Shmem",
    "mem.meminfo.SwapCached",
    "mem.meminfo.SwapFree",
    "mem.meminfo.SwapTotal",
    "mem.psi.full.avg60",
    "mem.psi.some.avg60",
    "mem.psi.some.total_us",
    "mem.swap.areas",
    "mem.sysctl.vm.page_cluster",
    "mem.sysctl.vm.swappiness",
    "mem.sysctl.vm.watermark_scale_factor",
    "mem.vmstat.pgscan_direct",
    "mem.vmstat.pgscan_kswapd",
    "mem.vmstat.pgsteal_direct",
    "mem.vmstat.pgsteal_kswapd",
    "mem.vmstat.pswpin",
    "mem.vmstat.pswpout",
    "mem.vmstat.workingset_refault_anon",
    "mem.vmstat.workingset_refault_file",
    "mem.vmstat.zswpin",
    "mem.vmstat.zswpout",
    "mem.zram.devices",
    "mem.zswap.enabled"
  ],
  "relevance": [
    {"when": "derived.swap.configured", "is": false, "note": "There is no swap here, so reclaim can only take file pages and kernel caches; the lesson's no-swap branch applies."},
    {"when": "derived.swap.compressed", "is": true, "note": "Compressed swap is in use here (zswap in front of active swap, or a zram swap area), so swapped pages may stay in RAM in compressed form."},
    {"when": "derived.reclaim.lru_kind", "is": "mglru", "note": "This kernel ranks pages with Multi-Gen LRU rather than the classic two lists."}
  ],
  "next": ["memory-pressure-and-oom"],
  "deeper": ["planned:compressed-swap", "planned:mglru-internals", "planned:memory-cgroups"],
  "version_notes": [
    {"applies": "Linux ≥ 3.15", "text": "Workingset detection: evicted file pages leave a shadow entry in the page cache, so a refault can be recognised and the page activated at once. Anonymous pages gained the same in 5.9, when workingset_refault split into _anon and _file counters."},
    {"applies": "Linux ≥ 4.14", "text": "Swap readahead can follow the faulting process's virtual addresses instead of neighbouring swap slots (/sys/kernel/mm/swap/vma_ra_enabled); page-cluster still caps the window."},
    {"applies": "Linux ≥ 4.20", "text": "Pressure stall information (/proc/pressure/memory). Kernels can be built without it (CONFIG_PSI) or boot with it disabled (psi=0)."},
    {"applies": "Linux ≥ 5.8", "text": "vm.swappiness accepts 0 to 200; before, 0 to 100. Values above 100 make anonymous pages cheaper to reclaim than file pages, for fast swap such as zram."},
    {"applies": "Linux ≥ 6.1", "text": "Multi-Gen LRU (CONFIG_LRU_GEN) can replace the two-list LRU; whether it is on is a build default (CONFIG_LRU_GEN_ENABLED) that /sys/kernel/mm/lru_gen/enabled can change at run time."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Concepts overview, Reclaim", "url": "https://docs.kernel.org/admin-guide/mm/concepts.html#reclaim", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/vm (swappiness, page-cluster, watermark_scale_factor)", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Multi-Gen LRU (admin guide)", "url": "https://docs.kernel.org/admin-guide/mm/multigen_lru.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Multi-Gen LRU (design)", "url": "https://docs.kernel.org/mm/multigen_lru.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: PSI, Pressure Stall Information", "url": "https://docs.kernel.org/accounting/psi.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: zswap", "url": "https://docs.kernel.org/admin-guide/mm/zswap.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: zram, compressed RAM-based block devices", "url": "https://docs.kernel.org/admin-guide/blockdev/zram.html", "kind": "kernel-doc"},
    {"title": "swapon(2): swap priorities", "url": "https://man7.org/linux/man-pages/man2/swapon.2.html", "kind": "man-page"},
    {"title": "swapon(8)", "url": "https://man7.org/linux/man-pages/man8/swapon.8.html", "kind": "man-page"},
    {"title": "Kernel source: get_scan_count() in mm/vmscan.c (how swappiness balances anon and file)", "url": "https://github.com/torvalds/linux/blob/master/mm/vmscan.c", "kind": "source"},
    {"title": "LWN: Better active/inactive list balancing (2012)", "url": "https://lwn.net/Articles/495543/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.swap.configured
:::when derived.swap.used_fraction
This machine has {{derived:derived.swap.total_bytes}} of swap, and {{derived:derived.swap.used_fraction|percent}} of it holds pages that were once in RAM. Some programs' memory was swapped out, written to storage or, with zswap or zram, compressed, while other memory stayed. Nobody asked for that. What decided which pages should leave RAM, and when?
:::else
This machine has {{derived:derived.swap.total_bytes}} of swap, and none of it is in use: either the kernel has not needed it since boot, or everything once written there has been read back and released. What would make it start, and how would it decide which pages leave RAM and which stay?
:::end
:::else
There is no swap on this machine (SwapTotal {{obs:mem.meminfo.SwapTotal}}). So when memory runs short, the kernel has nowhere to put a page of heap or stack: those pages exist only in RAM. Yet allocations here keep succeeding long after free memory has dwindled to a small reserve. Where do the frames come from, and what happens when the pages that can be dropped run out?
:::end

## Why it exists

The previous lesson showed the kernel filling idle memory with file data on purpose. That only works if frames can be taken back the moment something needs them, and most allocations must succeed at once: a page fault cannot wait for a human to close a program. So the kernel needs a mechanism that keeps a small amount of memory free at all times, finds frames to refill it, and chooses, among millions of pages, those least likely to be needed soon. Choosing well makes memory look larger than it is; choosing badly fills the machine with reads of data it just threw away.

## How it works

### Watermarks: when reclaim starts

As [physical memory](lesson:physical-memory) showed, each zone has *min*, *low* and *high* watermarks. An allocation that takes a zone below *low* wakes `kswapd`, a kernel thread per NUMA node, which reclaims in the background until every zone is above *high*, then sleeps; the allocation itself proceeds. If allocations outrun `kswapd` and a zone falls to *min*, the allocating task must reclaim for itself before it gets its frame: *direct reclaim*. That task stalls, which is how memory shortage turns into latency.

### What can be reclaimed

The previous two lessons set the price list:

- Clean file pages are free to reclaim: unmap them, drop them, reread the file later.
- Dirty file pages must be written back first, so reclaim starts writeback and comes back for them.
- Anonymous and shmem pages can be reclaimed only by writing them to swap.
- Kernel caches, mainly dentries and inodes in slab memory, are shrunk through callbacks called *shrinkers*.
- Unevictable pages, locked or on ramfs, are not touched.

### Ranking pages: the two-list LRU

Reclaim wants the pages least likely to be used again, but exact recency is too expensive to track: the CPU sets an *accessed* bit in a page-table entry on every use without telling the kernel. So the classic design keeps, per node (and per memory cgroup), an *active* and an *inactive* list for each type, anonymous and file. New pages usually start inactive. Reclaim scans the tail of the inactive list: a page whose accessed bit is clear is evicted; one used repeatedly since it was last looked at moves to the active list. When the active list grows too large, pages from its tail are moved down to be judged. A file read once therefore passes through the inactive list and out, without flushing the pages used all the time.

### Multi-Gen LRU

From Linux 6.1 the kernel can instead keep each type in several *generations*. Aging creates a new youngest generation by walking page tables in bulk, looking for set accessed bits, which is far cheaper than following each page back to its mappings one at a time. Eviction takes pages from the oldest generation, and a feedback loop compares how often recently evicted anonymous and file pages come back, to decide which type to take next. meminfo still shows Active and Inactive: the two youngest generations are reported as Active, the older ones as Inactive.

### Swappiness: the relative price of anon and file

`vm.swappiness` (0 to 200 since Linux 5.8, default 60) tells reclaim how expensive bringing back a swapped-out anonymous page is compared with rereading a file page. At 100 the two cost the same; below 100 file pages are preferred, above it anonymous pages, which suits fast compressed swap. The kernel combines it with how often each type has recently refaulted, so it is a weight, not a threshold. At 0, system-wide reclaim swaps only once free and file pages together fall below the zone's high watermark, a last resort rather than off; reclaim at a memory cgroup's own limit does not swap at all.

### Swap areas and the swap cache

A swap area is a partition, a file or a zram device; higher-priority areas are filled first, and equal priorities are used in turn. To swap out a page, reclaim allocates a slot, places the page in the *swap cache* (an index from slot to page, like a file's page cache), writes it, replaces each page-table entry that mapped it with the slot's address, and frees the frame.

Touching such an address faults. The handler finds the slot, reads the page into a new frame through the swap cache and maps it: a major fault. It reads more than one page, betting that neighbours will be wanted too; `vm.page-cluster` is the base-2 logarithm of the most it reads at once (3 means 8 pages). After a swap-in caused by a read, the slot is normally kept, so if the page is evicted again unchanged it need not be rewritten; such pages are `SwapCached`, in RAM and in swap at once. The slot is freed when the page is written, since the copy in swap is then stale, and on swap-in whenever swap is more than half full, to keep room. Many swapped-out pages are never read back at all, either never touched again, which is the memory swap is for, or freed while in swap, so swap-ins are usually far fewer than swap-outs.

### Refaults: noticing a mistake

When a page cache page is evicted, the kernel leaves a *shadow entry* in its slot, a record of when it was evicted, measured in evictions. If the page is faulted back in, the *refault distance* says how many other pages were evicted in between. If that is less than the size of the active working set, the page would have stayed had the lists been balanced differently, so it goes straight to the active list and counts as a workingset refault (anonymous pages too, since Linux 5.9). A rising refault rate is the kernel's evidence that its working set does not fit.

When it does not fit at all, the machine *thrashes*: every eviction removes something needed within moments. Allocations still succeed and nothing fails, but every program waits for reads that bring back what was just thrown out.

### Compressed swap

zswap and zram swap to RAM instead. zswap sits in front of a swap device: pages on their way out are compressed into a pool in memory. A page reaches the device only if zswap refuses it (the pool is at its size limit, or the page does not compress well) or when zswap later writes cold pages back to make room: once the pool hits its limit, and, where its shrinker is enabled, whenever memory is under pressure. zram is a block device whose contents are compressed in memory, often used as the only swap. Both trade CPU time for capacity; the zram documentation expects about two pages of typical data per frame.

### Measuring the cost: PSI

Counters of scanned and reclaimed pages say how much work reclaim did, not what it cost. Pressure stall information (Linux 4.20 and later) measures the cost: the share of wall time in which at least one task (*some*), or every non-idle task (*full*), was stalled on memory, averaged over 10, 60 and 300 seconds, plus a running total in microseconds. Stalls count time spent reclaiming (by kswapd as well as by allocating tasks) or compacting, throttled above a cgroup's `memory.high`, and waiting for reads of pages the kernel recognises as recently evicted working set, from a file or from swap; an ordinary swap-in of a page that was not recently evicted is not counted.

### Without swap

With no swap, anonymous and shmem pages cannot be reclaimed at all, and reclaim does not even scan them. All pressure falls on file pages and kernel caches, including the cached code of every running program. The machine can then thrash on program code, rereading it from storage again and again, while the OOM killer waits, because reclaim keeps making some progress. That is why a swapless machine often freezes for a long time before anything is killed, and one reason to configure a little swap: cold anonymous memory can then leave RAM so that hot file pages can stay.

## On this machine

:::when derived.swap.configured
:::table mem.swap.areas columns=name,type,size_bytes,used_bytes,priority
:::else
`/proc/swaps` lists no swap area here, and SwapTotal is {{obs:mem.meminfo.SwapTotal}}. Every anonymous page on this machine stays in RAM until it is freed.
:::end

:::evidence Swap
- derived.swap.configured
- derived.swap.total_bytes
- derived.swap.used_fraction
- mem.meminfo.SwapTotal
- mem.meminfo.SwapFree
- mem.meminfo.SwapCached | In RAM and in swap at once
- mem.sysctl.vm.swappiness
- mem.sysctl.vm.page_cluster
- mem.zswap.enabled
- mem.zram.devices
- derived.swap.compressed
:::end

:::when derived.swap.configured
:::when mem.vmstat.pswpout
Since boot this kernel has written {{obs:mem.vmstat.pswpout}} to a swap device and read {{obs:mem.vmstat.pswpin}} back from one. The {{obs:mem.meminfo.SwapCached}} of SwapCached are pages that came back and still have their copy in swap.
:::else
No page has been written to a swap device since boot ({{obs:mem.vmstat.pswpout}}). Either reclaim has always found cheaper pages to take, or every page it swapped out is still held by zswap, compressed in RAM; the zswap figures below, where present, tell which.
:::end
:::else
With swap, the kernel could have moved cold anonymous pages out and kept more file cache. Without it, the file cache is all reclaim can take; the {{obs:mem.meminfo.AnonPages}} of anonymous memory and {{obs:mem.meminfo.Shmem}} of shmem here are beyond its reach.
:::end

:::when derived.swap.compressed
:::when mem.zswap.enabled
Compressed swap is in use here: zswap is enabled with swap active, so pages on their way out are compressed into a pool in RAM, and only pages it refuses or writes back reach the swap device. Each page in its pool still reserves a swap slot, so used swap includes pages that are in fact in RAM, compressed. pswpout and pswpin count only device transfers; the pool's own traffic is counted in zswpout and zswpin (Linux 5.19 and later), listed with the reclaim counters below.
:::else
Compressed swap is in use here: a zram device is one of the active swap areas listed above. Pages "swapped out" to it stay in RAM, compressed, so swap traffic costs CPU time rather than I/O, and pswpout and pswpin count pages compressed into it and back, not transfers to storage.
:::unknown
Compressed swap is in use here: a zram device is one of the active swap areas listed above, so pages "swapped out" to it stay in RAM, compressed. Whether zswap is also enabled, in front of other swap areas, could not be read.
:::end
:::else
:::when mem.zswap.enabled
zswap is enabled here, but with no swap area behind it, it has nothing to do: it only compresses pages on their way to a swap device.
:::else
No compressed swap is in use here: zswap is off, and no zram device is an active swap area. Each page swapped out, whether to swap configured now or added later, is written to a storage device and read back from it, and pswpout and pswpin count every one.
:::unknown
Whether zswap is enabled could not be read here ({{obs:mem.zswap.enabled}}), but with no swap active it would have nothing to do: zswap only compresses pages on their way to a swap device.
:::end
:::unknown
Whether compressed swap is in use could not be determined here, because Machine Atlas could not read whether zswap is enabled or which swap areas are active (the inspector gives the reason). If zswap is enabled, swapped-out pages are first compressed into a pool in RAM, and pswpout and pswpin count only the pages that reach or leave the swap device itself; if swap is a zram device, every swap transfer is compression in RAM.
:::end

### How this kernel ranks pages

:::evidence Reclaim policy
- derived.reclaim.lru_kind
- mem.lru_gen.enabled
- mem.sysctl.vm.watermark_scale_factor
- derived.zones.min_bytes
- derived.zones.low_bytes
- derived.zones.high_bytes
:::end

:::when derived.reclaim.lru_kind is mglru
This kernel has Multi-Gen LRU switched on (`lru_gen/enabled` reads {{obs:mem.lru_gen.enabled}}; bit 0 is the main switch, bits 1 and 2 enable the bulk page-table scanning). It ages pages in generations, and the Active and Inactive figures in meminfo are an approximation of them.
:::else
This kernel uses the classic active and inactive lists: `/sys/kernel/mm/lru_gen/enabled` is missing, which means a kernel older than 6.1 or one built without Multi-Gen LRU, or it is present with the main switch off.
:::unknown
Machine Atlas could not tell which page ranking this kernel uses, because `/sys/kernel/mm/lru_gen/enabled` could not be read ({{obs:mem.lru_gen.enabled}}). On Linux 6.1 and later that file exists when Multi-Gen LRU is built in, and its lowest bit says whether it is on.
:::end

:::when derived.zones.low_bytes
Summed over all zones, kswapd wakes when free memory nears {{derived:derived.zones.low_bytes}} and stops at {{derived:derived.zones.high_bytes}}; tasks reclaim for themselves below {{derived:derived.zones.min_bytes}}. Watermarks are checked per zone, so these totals only give the scale, but they show how small the reserve is that the kernel defends: a sliver of the {{obs:mem.meminfo.MemTotal}} of RAM.
:::unknown
The zone watermarks could not be read here ({{derived:derived.zones.low_bytes}}), so the size of the reserve kswapd defends is unknown. On most machines it is around one percent of RAM or less.
:::end

### What reclaim has done since boot

:::evidence Reclaim activity, cumulative since boot
- kernel.uptime_seconds
- mem.vmstat.pgscan_kswapd
- mem.vmstat.pgsteal_kswapd
- mem.vmstat.pgscan_direct
- mem.vmstat.pgsteal_direct
- derived.reclaim.allocstalls | Times an allocating task entered direct reclaim, all zones
- mem.vmstat.pswpout
- mem.vmstat.pswpin
- mem.vmstat.zswpout | Pages compressed into zswap (Linux 5.19 and later)
- mem.vmstat.zswpin | Pages decompressed from zswap (Linux 5.19 and later)
- mem.vmstat.workingset_refault_file
- mem.vmstat.workingset_refault_anon
:::end

These are totals over {{obs:kernel.uptime_seconds}} of uptime. Two ratios carry meaning: stolen over scanned is how many examined pages reclaim could actually free, and direct over kswapd scanning is how often allocating tasks had to do the work themselves. The kernel files each entry into direct reclaim under the highest zone the allocation could use (most user page faults land in `allocstall_movable`), so the stall count adds up every `allocstall_*` counter this kernel has; reclaim forced by a memory cgroup's own limit is not counted in any of them. A busy desktop or server routinely shows millions of scanned pages; what matters is whether the numbers grow quickly while the machine feels slow.

:::when derived.psi.available
:::evidence Memory pressure now
- mem.psi.some.avg60
- mem.psi.full.avg60
- mem.psi.some.total_us
:::end

Over the last minute, some task was stalled on memory {{obs:mem.psi.some.avg60}} of the time, and all tasks at once {{obs:mem.psi.full.avg60}}. Values near zero mean reclaim is keeping up without anyone waiting; *full* values that stay above a few percent mean every task is regularly losing time to memory.
:::else
Pressure stall information cannot be read here ({{obs:mem.psi.some.avg60}}). If the file is absent, the kernel predates 4.20, was built without CONFIG_PSI, or was booted with psi=0; if permission is denied, a sandbox or security policy hides `/proc/pressure`. Without it, the reclaim counters above say how much work was done but not how long tasks waited.
:::unknown
Whether pressure stall information is available could not be determined: {{obs:mem.psi.some.avg60}}.
:::end

## See it

:::sim reclaim

The simulator runs a toy machine of 96 frames with min, low and high watermarks, two lists per type and optional swap, seeded with this machine's swap presence and swappiness. Its event log explains each decision. It is a textbook two-list model, not this kernel's policy.

:::diagram lru-lists

## Predict

:::predict
In the simulator, remove swap and let a program's anonymous memory keep growing while the page cache holds the files it reads in a loop. What happens to the cache, and when does the machine stop doing useful work?

- Nothing is reclaimed until memory is full; then the OOM killer acts at once.
- The cache shrinks; the machine stays responsive until the cache is gone, then the OOM killer acts.
- The cache shrinks until the files the program loops over no longer fit; from then it rereads them constantly, well before memory is "full".
:::reveal
The third. Without swap, reclaim never touches anonymous pages, so every new anonymous page comes from the file cache. While the cache still holds more than the loop needs, only cold file pages go and nothing is felt. Once it holds less, each pass of the loop evicts pages the next pass needs: refaults climb, every access waits for storage, and the machine is effectively stuck while free memory sits at the watermarks and allocations still succeed. Only when no file pages are left to drop would the OOM killer be invoked, and on a real machine that can take minutes.

The first answer misses kswapd, which starts at the low watermark long before memory is "full". The second misses thrashing: the machine becomes useless before the cache is empty.

:::when derived.swap.configured
With swap, as on this machine ({{derived:derived.swap.total_bytes}}), reclaim would have written the program's coldest anonymous pages out instead, keeping more of the looping files in RAM, until the anonymous working set itself no longer fit.
:::else
This machine has no swap ({{derived:derived.swap.total_bytes}}), so the run without swap is the one that describes it.
:::end
:::end

## Try it

Machine Atlas offers no experiment that creates memory pressure, and you should not provoke it on a machine you care about. Watching is enough:

```sh
cat /proc/pressure/memory
vmstat 1 5
grep -E '^(pgscan|pgsteal|pswp|workingset_refault)' /proc/vmstat; sleep 10; grep -E '^(pgscan|pgsteal|pswp|workingset_refault)' /proc/vmstat
```

On a quiet machine expect the pressure averages at or near zero, the `si` and `so` columns of `vmstat` (pages swapped in and out per second) at zero, and the counters unchanged between the two readings. If they grow while the machine feels slow, the differences divided by ten seconds are the current rates of reclaim, swapping and refaults.

## Limits

:::misconception Swap is only for when RAM is full
Swap lets the kernel move anonymous pages that are never used, such as start-up code's data or an idle program's heap, out of RAM, so the frames can hold file pages that are used. A machine with swap often swaps a little long before memory is short, and is faster for it.
:::end

:::misconception Swap in use means the machine is short of memory now
Pages stay in swap until something touches them. Swap filled during a busy hour stays filled on a quiet evening. What indicates a shortage now is the swap-in rate (`si` in vmstat, the growth of pswpin) and the pressure figures, not the amount used.
:::end

:::misconception Swappiness is a percentage threshold
It is not "swap when memory is 60% full". It is a weight in the comparison between reclaiming anonymous and file pages, applied whenever reclaim runs, and 0 does not turn swap off.
:::end

:::cannot
Every reclaim and swap counter is cumulative since boot. One reading says what happened over the whole uptime, not what is happening now: rates need two readings some seconds apart.

`SwapCached` pages occupy RAM and a swap slot at the same time, so they are counted on both sides, and used swap overstates how much memory is held only in swap.

How much of each process is in swap can be listed without privileges: the `VmSwap` line of every `/proc/<pid>/status` is world-readable unless `/proc` is mounted with `hidepid`. But it excludes shmem, and which of a process's pages are swapped shows only in its `smaps` and `pagemap`, readable only for processes you are allowed to trace.

The simulator's policy is a two-list textbook model with a fixed scan batch, not the heuristics of this kernel, and its numbers are not this machine's.
:::end

## Go deeper

Next, [memory pressure and the OOM killer](lesson:memory-pressure-and-oom) follows what happens when reclaim finds nothing left to take. In Explore, [LRU reclaim](component:lru-reclaim), [kswapd](component:kswapd), [swap areas](component:swap-areas) and [zswap and zram](component:zswap-zram) show how these mechanisms depend on each other. Planned lessons cover compressed swap in depth, Multi-Gen LRU's internals (generations, bloom filters, the refault controller), and memory cgroups, each of which has its own LRU lists and reclaim.
