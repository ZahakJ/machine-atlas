---
{
  "id": "transparent-huge-pages",
  "title": "One fault, two megabytes",
  "subsystem": "memory",
  "kind": "branch",
  "order": 1,
  "summary": "How the kernel backs ordinary memory with huge pages without being asked: the modes, allocation at fault time and later collapse, the compaction it may wait for, and what it costs.",
  "prerequisites": ["page-faults"],
  "uses": [
    "derived.probe.written_huge_fraction",
    "derived.thp.enabled",
    "derived.thp.mode",
    "kernel.page_size_bytes",
    "mem.buddyinfo",
    "mem.meminfo.AnonHugePages",
    "mem.meminfo.FileHugePages",
    "mem.meminfo.ShmemHugePages",
    "mem.thp.defrag",
    "mem.thp.enabled",
    "mem.thp.pmd_size_bytes",
    "mem.thp.shmem_enabled",
    "mem.vmstat.thp_collapse_alloc",
    "mem.vmstat.thp_fault_alloc",
    "mem.vmstat.thp_fault_fallback",
    "mem.vmstat.thp_split_page",
    "probe.regions",
    "probe.thp.fault_alloc_delta",
    "probe.thp.fault_fallback_delta"
  ],
  "relevance": [
    {"when": "derived.thp.enabled", "note": "Transparent huge pages are enabled here, for programs that ask (madvise mode) or for all anonymous memory (always mode), so some memory can be mapped 2 MiB at a time."}
  ],
  "next": ["physical-memory"],
  "deeper": ["planned:hugetlbfs", "planned:page-table-internals", "planned:tlb-and-pcid"],
  "version_notes": [
    {"applies": "Linux ≥ 2.6.38", "text": "Transparent huge pages for anonymous memory, with madvise(MADV_HUGEPAGE) and MADV_NOHUGEPAGE."},
    {"applies": "Linux ≥ 4.8", "text": "Huge pages for shmem and tmpfs, controlled by shmem_enabled and the tmpfs huge= mount option; counted in ShmemHugePages."},
    {"applies": "Linux ≥ 6.1", "text": "madvise(MADV_COLLAPSE) lets a program collapse a range into huge pages synchronously, whatever the sysfs mode."},
    {"applies": "Linux ≥ 6.8", "text": "Multi-size THP: per-size controls under /sys/kernel/mm/transparent_hugepage/hugepages-<size>kB/ allow anonymous folios between one page and PMD size. They are mapped with ordinary page-table entries and are not counted in AnonHugePages."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Transparent Hugepage Support (administration)", "url": "https://docs.kernel.org/admin-guide/mm/transhuge.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Transparent Hugepage Support (design: splitting and refcounting)", "url": "https://docs.kernel.org/mm/transhuge.html", "kind": "kernel-doc"},
    {"title": "Linux 6.8 documentation: Transparent Hugepage Support, introducing multi-size THP", "url": "https://docs.kernel.org/6.8/admin-guide/mm/transhuge.html", "kind": "kernel-doc"},
    {"title": "madvise(2): MADV_HUGEPAGE, MADV_NOHUGEPAGE, MADV_COLLAPSE", "url": "https://man7.org/linux/man-pages/man2/madvise.2.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: Physical Memory", "url": "https://docs.kernel.org/mm/physical_memory.html", "kind": "kernel-doc"}
  ]
}
---
## Question

:::when derived.thp.enabled
The transparent huge page mode here is {{obs:mem.thp.enabled}}, and right now {{obs:mem.meminfo.AnonHugePages}} of this machine's anonymous memory (heaps, stacks and the like) is mapped in huge pages of {{obs:mem.thp.pmd_size_bytes}}, each a single page-table entry. Under `always` that figure grows without any program asking; under `madvise` only programs that asked add to it. Who decides which memory gets huge pages, and why is it never all of it?
:::else
Transparent huge pages are off for ordinary anonymous memory here: the mode reads {{obs:mem.thp.enabled}} (an absent setting means a kernel built without the feature), and {{obs:mem.meminfo.AnonHugePages}} of anonymous memory is mapped in huge pages. On many other machines that figure runs to gigabytes, without any program having asked. Who decides which memory gets huge pages, why would an administrator turn that off, and what does this machine give up by doing so?
:::unknown
Machine Atlas could not read the transparent huge page settings here, but the kernel's accounting is readable: {{obs:mem.meminfo.AnonHugePages}} of anonymous memory is mapped in huge pages right now. If that is more than nothing, the kernel is using them, and almost no program asked. Who decides which memory gets huge pages, and why is it not all of it?
:::end

## Why it exists

The [cpu-addressing](lesson:cpu-addressing) lesson showed that a TLB of a few thousand entries covers only a few megabytes when each entry maps 4 KiB. A database or a virtual machine working across many gigabytes misses the TLB constantly, and each miss is a page walk of up to four or five memory reads. One entry that maps 2 MiB covers 512 times as much, and the walk that fills it is one level shorter. The same holds for faults: one fault can supply 2 MiB instead of 4 KiB.

The cost is that a huge page needs 2 MiB of physically contiguous, aligned free memory, and that it is all-or-nothing: a program that touches one byte of it gets all of it. Explicit huge pages (hugetlbfs) leave that trade to the programmer and the administrator, who must reserve pools in advance. Transparent huge pages make it automatically, for programs that never heard of huge pages.

## How it works

### A leaf one level up

A transparent huge page is a block of frames mapped by a single entry at the PMD level, the level above the last page tables, instead of by 512 entries in a page table below it. Its size is the range one last-level table would cover: 2 MiB with 4 KiB pages, 32 MiB with 16 KiB, 512 MiB with 64 KiB. The physical block must be aligned to its size, so it comes from the buddy allocator as one order-9 block (with 4 KiB pages), and the virtual range must be aligned too and lie entirely inside one VMA with the same permissions.

### The modes

`/sys/kernel/mm/transparent_hugepage/enabled` sets the policy for anonymous memory:

- `always`: any anonymous VMA large enough may get huge pages.
- `madvise`: only ranges a program marked with `madvise(MADV_HUGEPAGE)`.
- `never`: none, although since 6.1 a program can still collapse a range explicitly with `MADV_COLLAPSE`.

In every mode, `MADV_NOHUGEPAGE` opts a range out, and `prctl(PR_SET_THP_DISABLE)` opts a whole process out. Distributions choose the default when they build the kernel; both `always` and `madvise` are common.

### At fault time, or later

When a page fault lands in an eligible, aligned 2 MiB range that has no page table yet, the kernel tries to allocate a huge page on the spot. If it succeeds, it zeroes 2 MiB, maps it with one entry and counts thp_fault_alloc; the rest of the range then never faults. If no order-9 block is free, it counts thp_fault_fallback and maps an ordinary page, and the range fills in page by page.

What happens when no block is free is the *defrag* setting. `always` makes the faulting thread compact memory synchronously, migrating movable pages until a 2 MiB block is free: it gets its huge page, after a stall that can last milliseconds. `defer` wakes kswapd and the background compaction thread and falls back at once. `madvise`, the kernel's default, stalls only for ranges marked `MADV_HUGEPAGE`; `defer+madvise` stalls for those and defers for the rest; `never` just falls back.

Memory that fell back is not lost to huge pages for good. The `khugepaged` thread scans eligible VMAs and *collapses* runs of small pages into a huge page by allocating one and copying them across (thp_collapse_alloc). Its `max_ptes_none` setting, by default 511, is how many of the 512 pages may be missing; the missing ones become zero-filled memory that nobody asked for.

A read fault in an untouched eligible range maps the shared *huge zero page*, so reading never allocates.

### Splitting

A huge page becomes 512 small ones when something needs to treat part of it differently: `munmap` or `mprotect` of part of the range, `MADV_DONTNEED` on part of it, or reclaim that wants to swap out only some of it. Splitting the page-table entry always succeeds; splitting the underlying page can be deferred, and a partly unmapped huge page waits on a queue until memory pressure makes it worth splitting and freeing the unused parts (thp_split_page).

### The costs

Internal fragmentation: a program that touches one byte in each 2 MiB stretch gets 2 MiB of resident memory for each, 512 times what it used, and khugepaged can add more. Latency: a huge-page fault zeroes 2 MiB at once, and with synchronous defrag it may first stall on compaction. Background CPU: khugepaged and compaction run whether or not anything benefits. For large, densely used heaps the trade pays; for sparse ones, and for latency-sensitive allocators that free memory in small pieces, it may not, which is why some databases recommend `madvise` or `never`.

### Beyond anonymous memory, and beyond 2 MiB

`shmem_enabled` applies the same idea to shared memory and `memfd` (mounted tmpfs instances use their `huge=` option), counted in ShmemHugePages. Page-cache pages can also live in huge folios, counted in FileHugePages, for filesystems that support large folios. Since Linux 6.8, *multi-size THP* adds intermediate sizes for anonymous memory (16 KiB to 1 MiB with 4 KiB pages), each with its own `enabled` setting under `hugepages-<size>kB/`. They are mapped with ordinary page-table entries, so they save faults and allocator work but no page-walk levels, unless the CPU can coalesce adjacent entries in the TLB as arm64's contiguous bit and some AMD processors do. AnonHugePages counts only the PMD-sized ones.

### Not hugetlbfs

Explicit huge pages (hugetlbfs) are a different mechanism: a pool reserved in advance, used only by programs that map it deliberately, never split, never swapped, and offered in larger sizes such as 1 GiB. Transparent huge pages need no reservation and no program changes, and in exchange guarantee nothing.

## On this machine

:::evidence Transparent huge page settings and totals
- mem.thp.enabled
- mem.thp.defrag
- mem.thp.shmem_enabled
- mem.thp.pmd_size_bytes
- mem.meminfo.AnonHugePages
- mem.meminfo.ShmemHugePages
- mem.meminfo.FileHugePages
- mem.vmstat.thp_fault_alloc
- mem.vmstat.thp_fault_fallback
- mem.vmstat.thp_collapse_alloc
- mem.vmstat.thp_split_page
:::end

Since boot, faults here allocated {{obs:mem.vmstat.thp_fault_alloc}} huge pages directly and fell back to small pages {{obs:mem.vmstat.thp_fault_fallback}} times; khugepaged built {{obs:mem.vmstat.thp_collapse_alloc}} more by collapsing small pages, and {{obs:mem.vmstat.thp_split_page}} were split back. The defrag policy is {{obs:mem.thp.defrag}}.

### The probe's 8 MiB

:::table probe.regions columns=name,size_bytes,rss_bytes,anon_huge_bytes

The probe maps its written region with one plain `mmap`, as an ordinary program would, and does not mark it with `MADV_HUGEPAGE`. Where it lands is the kernel's choice: since Linux 6.7, a kernel with transparent huge pages places a large anonymous mapping like this one on a 2 MiB boundary by itself, while an older kernel may place it across boundaries. (A kernel that cannot name the probe's regions, one older than 5.17 or built without anonymous mapping names, gets a fallback layout instead: the probe places them itself, on 2 MiB boundaries between guard pages.) Aligned, with 4 KiB pages, its 8 MiB are four 2 MiB stretches, and each gets a huge page at its first write only if this machine's mode allows that for unmarked memory and the allocation succeeds at that instant; each stretch succeeds or falls back on its own. With larger pages a PMD huge page is bigger than the whole region, so it cannot get one.

:::when probe.thp.fault_fallback_delta is 0
Just before and just after its write loop, the probe read the THP fault counters: thp_fault_alloc rose by {{obs:probe.thp.fault_alloc_delta}} and thp_fault_fallback by {{obs:probe.thp.fault_fallback_delta}}. No huge-page fault anywhere on the machine fell back to small pages in that instant, so a stretch of the probe's region without a huge page was one the kernel never tried to give one. The counters are system-wide: another process's huge-page faults at the same moment would be counted too.
:::else
Just before and just after its write loop, the probe read the THP fault counters: thp_fault_alloc rose by {{obs:probe.thp.fault_alloc_delta}} and thp_fault_fallback by {{obs:probe.thp.fault_fallback_delta}}. So in that instant some huge-page fault on this machine tried for a huge page and took small pages instead. The counters are system-wide, so they cannot say whose faults those were; the region's own huge-page share below says how the probe's stretches fared.
:::unknown
The probe could not read the THP fault counters around its write loop (a kernel built without transparent huge pages has neither), so what the kernel did at the probe's faults shows only in the region's own huge-page share below.
:::end

The since-boot counters above and the free-block counts further down were read when the snapshot was collected, not around the probe's faults: they show how common fallbacks are on this machine and how scarce free 2 MiB blocks are now.

:::when derived.probe.written_huge_fraction is 1
Here the whole written region ended up in huge pages ({{derived:derived.probe.written_huge_fraction|percent}}): every first write into a 2 MiB stretch was satisfied with a whole huge page. The untouched and read-only regions have none, because nothing was ever written to them.
:::else
:::when derived.probe.written_huge_fraction
Here the written region ended up {{derived:derived.probe.written_huge_fraction|percent}} in huge pages: some of its 2 MiB stretches got a huge page at their first write, and the rest fell back to 4 KiB pages, one fault per page. A fault falls back when no huge page can be allocated for it without compaction that the defrag policy shown above does not allow, or when the compaction it allows does not free a block.
:::else
Here the written region got no huge pages ({{derived:derived.probe.written_huge_fraction|percent}}): either THP did not apply to it, or every one of its huge-page faults fell back to small pages.
:::end
:::unknown
The probe's huge page accounting is not available in this snapshot. Where it is, the written 8 MiB region shows all of its memory in huge pages when THP applies to it and free 2 MiB blocks exist, none when THP does not apply or every attempt fell back, or a share in between when some stretches fell back.
:::end

:::when derived.thp.mode is always
In `always` mode the probe's region is eligible without asking, so a stretch that got no huge page either had its allocation fall back, lay across a 2 MiB boundary (possible on kernels before 6.7), or was excluded some other way: since Linux 6.8 the per-size control `hugepages-2048kB/enabled` can be set to `never` while the global mode reads `always`, and a process can opt out with `prctl(PR_SET_THP_DISABLE)`, which the probe does not.
:::else
:::when derived.thp.mode is madvise
In `madvise` mode the probe would have had to mark the region with `MADV_HUGEPAGE` to get huge pages at fault time. It does not, so it gets what an ordinary program gets: small pages.
:::else
The mode here is {{obs:mem.thp.enabled}}, so no request from the probe would have given it huge pages at fault time.
:::end
:::unknown
Machine Atlas could not read the mode here, so it cannot tell from the setting whether THP is on for all anonymous memory, limited to marked regions, or off; the probe's outcome and fault counters above are the evidence.
:::end

### Where 2 MiB blocks come from

:::when kernel.page_size_bytes is 4096
With 4 KiB pages, a huge page is an order-9 block, and order 10 blocks can be split into two of them. These two columns of the buddy allocator's free lists are therefore the supply of huge pages that need no compaction:

:::table mem.buddyinfo columns=node,zone,order9,order10
:::else
With {{obs:kernel.page_size_bytes}} pages, a PMD huge page is {{obs:mem.thp.pmd_size_bytes}}, a higher order than on x86-64; the buddy allocator's free lists show how many large blocks are free at each order:

:::table mem.buddyinfo
:::end

Counts near zero do not mean memory is short, only that free memory is in small pieces; a fault in a region that may not stall then falls back, while one allowed to stall tries compaction first and falls back only if that fails.

## Predict

:::predict
The opt-in experiment maps two 32 MiB anonymous regions, aligned the way huge pages need. It leaves the first to the system default and marks the second `MADV_NOHUGEPAGE`, then writes to every page of both. How will their fault counts and their AnonHugePages in smaps compare here?

- The same: both get small pages and about 8 192 faults each.
- The default region takes about 16 faults and is all huge pages; the marked one takes about 8 192 and has none.
- The default region lands somewhere between, with some huge pages; the marked one still takes about 8 192.
:::reveal
:::when derived.thp.mode is always
With the mode `always`, the default region should take about 16 faults and show 32 MiB of AnonHugePages when free 2 MiB blocks exist, and land in between when memory is fragmented, because each of its 16 huge-page faults succeeds or falls back on its own. The marked region takes about 8 192 faults and shows none, whatever the free lists hold. If the two come out the same, the fault counters above should show fallbacks.
:::else
The mode here is {{obs:mem.thp.enabled}}, so neither region asks for huge pages and both should take about 8 192 faults with no AnonHugePages: marking one `MADV_NOHUGEPAGE` changes nothing when the default already gives none. In `madvise` mode a region marked `MADV_HUGEPAGE` instead would behave like the default region on an `always` machine, about 16 faults.
:::unknown
Machine Atlas could not read the mode here, so the outcome cannot be predicted from this machine. If THP is `always` and 2 MiB blocks are free, the default region takes about 16 faults and the marked one about 8 192; otherwise both take about 8 192.
:::end
:::end

## Try it

:::experiment fault-count

Besides the first touch, the experiment shows two more effects worth predicting: a second touch costs almost no faults in either region, and after `MADV_DONTNEED` both regions fault again from scratch; where the default region had huge pages, it can refault in 2 MiB steps, but only if free 2 MiB blocks are still available: each refault is a fresh huge-page allocation that can fall back, like the first touch. Three read-only commands show this machine's settings and how much of your shell's own memory is huge:

```sh
grep -H . /sys/kernel/mm/transparent_hugepage/enabled /sys/kernel/mm/transparent_hugepage/defrag
grep -E '^thp_(fault|collapse|split)' /proc/vmstat
grep AnonHugePages /proc/self/smaps_rollup
```

A small process such as a shell usually shows 0 kB: its heap is too small to contain an aligned 2 MiB range.

## Limits

:::misconception THP is always faster
It saves TLB misses and faults for large, densely used memory. For sparse memory it inflates resident size; with synchronous defrag it can stall a fault for milliseconds on compaction; and khugepaged and compaction cost CPU in the background. Whether it helps is a property of the workload, measured with the workload.
:::end

:::misconception AnonHugePages counts all huge pages
It counts PMD-sized transparent huge pages in anonymous memory. Shared memory, file-backed huge folios, explicit hugetlbfs pools and the smaller multi-size THP folios are counted elsewhere or not at all.
:::end

:::cannot
The counters say how many huge pages were allocated, collapsed, split or missed; they cannot say whether any of them made a program faster. That needs measurements of the program itself, such as TLB-miss counters from `perf`, which usually need privileges.

Whether one particular fault gets a huge page depends on alignment, on the VMA, and on the free lists at that instant, so the probe and the experiment record what happened once. The buddyinfo shown is a later snapshot, not the state the probe faulted against.
:::end

## Go deeper

[Physical memory](lesson:physical-memory) explains the buddy allocator and the fragmentation that decides whether a 2 MiB block is free. In Explore, the [page tables](component:page-tables) and [TLB](component:tlb) components show where a huge page saves work. Planned lessons cover hugetlbfs, page-table internals and the TLB.
