---
{
  "id": "page-faults",
  "title": "Memory that appears when you touch it",
  "subsystem": "memory",
  "kind": "core",
  "order": 4,
  "summary": "How the CPU hands control to the kernel at the first touch of a page, what the kernel does in each case, and why fault counts are routinely lower than page counts.",
  "prerequisites": ["mappings"],
  "uses": [
    "derived.probe.read_region_rss_bytes",
    "derived.probe.written_huge_fraction",
    "derived.probe.written_minflt_delta",
    "derived.probe.written_pages_expected",
    "derived.thp.mode",
    "kernel.page_size_bytes",
    "kernel.uptime_seconds",
    "mem.thp.enabled",
    "mem.vmstat.pgfault",
    "mem.vmstat.pgmajfault",
    "mem.vmstat.thp_fault_alloc",
    "probe.faults.majflt_after_touch",
    "probe.faults.majflt_before_touch",
    "probe.faults.minflt_after_touch",
    "probe.faults.minflt_before_touch",
    "probe.pagemap.pfn_visible",
    "probe.regions",
    "probe.thp.fault_alloc_delta",
    "probe.thp.fault_fallback_delta"
  ],
  "relevance": [
    {"when": "derived.thp.mode", "is": "always", "note": "Transparent huge pages are on for all anonymous memory here, so one fault can map a whole huge page."}
  ],
  "next": ["physical-memory"],
  "deeper": ["transparent-huge-pages", "planned:copy-on-write-and-fork", "planned:userfaultfd"],
  "version_notes": [
    {"applies": "Linux ≥ 4.2", "text": "Unprivileged readers of /proc/<pid>/pagemap see presence and swap bits but a zeroed frame number; 4.0 and 4.1 refused them entirely. CAP_SYS_ADMIN is needed for frame numbers."},
    {"applies": "Linux ≥ 5.14", "text": "madvise(MADV_POPULATE_READ / MADV_POPULATE_WRITE) prefaults a range on request, an alternative to MAP_POPULATE for memory that is already mapped."},
    {"applies": "Linux ≥ 6.4", "text": "Most anonymous page faults take a lock on the faulting VMA only, instead of the whole address space's mmap lock, so threads faulting in different regions no longer serialise."},
    {"applies": "Linux ≥ 6.8", "text": "Multi-size THP lets anonymous faults allocate blocks between one page and a PMD-sized huge page (16 KiB to 1 MiB with 4 KiB pages), when an administrator enables those sizes."}
  ],
  "references": [
    {"title": "getrusage(2): ru_minflt and ru_majflt", "url": "https://man7.org/linux/man-pages/man2/getrusage.2.html", "kind": "man-page"},
    {"title": "madvise(2)", "url": "https://man7.org/linux/man-pages/man2/madvise.2.html", "kind": "man-page"},
    {"title": "mmap(2): MAP_POPULATE, MAP_ANONYMOUS", "url": "https://man7.org/linux/man-pages/man2/mmap.2.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: Examining process page tables (pagemap)", "url": "https://docs.kernel.org/admin-guide/mm/pagemap.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Transparent Hugepage Support", "url": "https://docs.kernel.org/admin-guide/mm/transhuge.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Page tables", "url": "https://docs.kernel.org/mm/page_tables.html", "kind": "kernel-doc"},
    {"title": "Linux kernel source: mm/memory.c (handle_mm_fault, do_anonymous_page, do_fault_around)", "url": "https://github.com/torvalds/linux/blob/master/mm/memory.c", "kind": "source"},
    {"title": "Intel 64 and IA-32 Architectures Software Developer's Manual, Volume 3A, §4.7: Page-fault exceptions", "url": "https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html", "kind": "spec"}
  ]
}
---
## Question

While writing one byte to each page of 8 MiB of fresh memory, the probe took {{derived:derived.probe.written_minflt_delta}} minor page faults. 8 MiB divided by the page size here is {{derived:derived.probe.written_pages_expected}}. Whether those two numbers match on this machine or differ by a factor of hundreds, the first question is the same: why does touching memory that `mmap` already granted "fault" at all, and what is the kernel doing each time it does?

## Why it exists

The previous lesson ended with a promise: a VMA that says what should be at each address, and no page-table entries behind it. The promise has to be kept at the last possible moment, the instant the program first uses a page, and not a moment before, or the savings disappear.

That needs the hardware's cooperation. The MMU translates addresses without the kernel, so something must stop it when it reaches a page with no valid translation, and hand the kernel enough information to fix the problem and let the instruction run again as if nothing had happened.

## How it works

### The exception

When the walk fails, because an entry is missing or forbids the access, the CPU raises a *page fault*: an exception, like a division by zero, that suspends the instruction and jumps to a kernel handler. It records the faulting address (in CR2 on x86-64, in FAR_EL1 on arm64) and the reason: whether the page was present at all, whether the access was a read, write or instruction fetch, and whether it came from user mode (an error code on x86-64, the syndrome register ESR_EL1 on arm64). When the handler returns, the CPU retries the instruction.

### Finding the promise

The handler looks up the VMA that contains the address. If there is none, or the VMA forbids the access (a write to a read-only mapping), the promise does not exist and the process gets SIGSEGV: that is a segmentation fault. One exception: an address just below the stack VMA, which is marked to grow downwards, extends the stack instead, up to the stack size limit and never closer than a guard gap to the next mapping.

### What the kernel does, case by case

If the VMA allows the access, the handler keeps the promise according to what the VMA and the page-table entry say:

- **First read of anonymous memory.** Nothing has been written, so every byte is zero. The kernel maps the *shared zero page*, a single frame of zeros used by every process, read-only. No memory is allocated.
- **First write to anonymous memory.** The kernel allocates a frame, fills it with zeros and installs a writable entry. The zeroing is not optional: the frame last held some other process's data, and handing it over unwiped would leak it.
- **File-backed memory.** The kernel looks for the file's page in the page cache. If it is there, mapping it is quick. If not, the kernel starts a read from storage and the process sleeps until it completes: that is a *major* fault. For read faults the kernel also maps neighbouring pages that are already cached, up to 64 KiB around the fault (*fault-around*, 16 pages with 4 KiB pages), because the program is likely to touch them next.
- **Write to a page shared copy-on-write.** A private file mapping, a page still shared between parent and child after `fork`, or a zero page that is now being written: the kernel copies the page into a new frame, maps the copy writable, and leaves the original alone.
- **A page that was swapped out.** The entry holds the page's location in swap instead of a frame number; the kernel reads it back, a major fault unless the page was still in the swap cache.
- **A transparent huge page.** If huge pages are allowed for the region and an aligned 2 MiB range (with 4 KiB pages) fits inside the VMA, the first fault in it can allocate and zero a whole huge page and map it with one entry one level up. The next 511 pages then need no fault at all.

### Minor and major

A *minor* fault is resolved without waiting for I/O: the data was already in memory or did not need to exist (zeros). A *major* fault had to wait for a read from storage. The distinction is about I/O, not about swap: most major faults on a desktop are the first run of a program reading its code from disk. Each thread counts both kinds, readable with `getrusage(2)`, and the kernel counts them for the whole system in `/proc/vmstat` as pgfault and pgmajfault.

### What a fault costs

A minor fault is an exception, a VMA lookup, perhaps an allocation and zeroing a page, and a return: roughly a microsecond, more when a huge page must be zeroed. A major fault costs a storage read, from tens of microseconds on fast flash to milliseconds on a disk. For most programs this is the right trade, but not for all: `MAP_POPULATE` on `mmap` and `madvise(MADV_POPULATE_WRITE)` fault a range in ahead of time, and `mlock` faults it in and keeps it resident, for programs that cannot afford a fault at an unpredictable moment.

## On this machine

:::evidence The probe's write loop
- probe.faults.minflt_before_touch
- probe.faults.minflt_after_touch
- derived.probe.written_minflt_delta | Minor faults during the write loop
- derived.probe.written_pages_expected | Pages in the 8 MiB region
- probe.faults.majflt_before_touch
- probe.faults.majflt_after_touch
- derived.probe.written_huge_fraction | Fraction of the region mapped with huge pages
- mem.thp.enabled
- derived.thp.mode
- probe.thp.fault_alloc_delta | Huge pages allocated at fault, system-wide, during the loop
- probe.thp.fault_fallback_delta | Huge-page faults that fell back, system-wide, during the loop
:::end

The probe makes each region the way any program would, with one plain `mmap` and no address hint, and names its anonymous regions so that maps shows the written one as `[anon: atlas probe: anon-written]` (names need Linux 5.17 or later and a kernel built with them; on a kernel without them the probe instead places its regions itself, on 2 MiB boundaries between guard pages, and the region table's source says which layout was used). Where the region lands is the kernel's choice. Since Linux 6.7, a kernel with transparent huge pages places a large anonymous mapping like this one on a 2 MiB boundary by itself, so with 4 KiB pages the 8 MiB are four aligned 2 MiB stretches, each a candidate for one huge page. An older kernel may place it across boundaries, leaving fewer whole stretches.

:::when derived.probe.written_huge_fraction is 1
The write loop took {{derived:derived.probe.written_minflt_delta}} faults for {{derived:derived.probe.written_pages_expected}} because the whole region ended up in transparent huge pages: the first write into each 2 MiB stretch faulted, the kernel allocated a whole huge page, and the remaining 511 writes in that stretch found a valid translation. A count a little above one per huge page is still this outcome: the loop's first pass can fault in its own code, and the Go runtime can interrupt the thread with a signal whose handler touches memory.
:::else
:::when derived.probe.written_huge_fraction
The write loop took {{derived:derived.probe.written_minflt_delta}} faults for {{derived:derived.probe.written_pages_expected}}, and {{derived:derived.probe.written_huge_fraction|percent}} of the region ended up in transparent huge pages: a mixed outcome. Some 2 MiB stretches got a huge page at their first write, one fault for 512 pages; the others fell back to 4 KiB pages and took one fault per page, 512 each. So the count is roughly one fault per huge stretch plus 512 per stretch that fell back, and most of it comes from the fallbacks. Each fallback is counted system-wide in thp_fault_fallback, which the probe read on either side of its loop (below), and buddyinfo shows how many free 2 MiB blocks there are now; the [transparent huge pages](lesson:transparent-huge-pages) lesson covers both.
:::else
The write loop took {{derived:derived.probe.written_minflt_delta}} faults for {{derived:derived.probe.written_pages_expected}}, and none of the region ended up in huge pages. Each page then needs its own fault, its own zeroed frame and its own entry, so expect a count close to the page count, plus a few from the loop's own code and the Go runtime. A count far below the page count without huge pages would mean the kernel mapped several small pages per fault, as multi-size THP (Linux 6.8 and later, when enabled) can; AnonHugePages does not count those.
:::end
:::unknown
The probe's fault counts or its region accounting are not available in this snapshot, so the comparison cannot be made here. On a typical machine it comes out one of three ways: about one fault per page, 2 048 with 4 KiB pages; a handful, about one per 2 MiB huge page, when transparent huge pages are on and free 2 MiB blocks are available; or in between, when some stretches get a huge page and the rest fall back.
:::end

:::when derived.thp.mode is always
The transparent huge page mode here is `always`: every aligned 2 MiB stretch of anonymous memory may get a huge page at its first fault without the program asking. Whether it does depends on a free 2 MiB block at that instant. A fault that finds none either stalls to compact memory, which may or may not free a block, or falls back to small pages at once, as the defrag setting decides; the [transparent huge pages](lesson:transparent-huge-pages) lesson shows the setting and the counters that record such fallbacks.
:::else
:::when derived.thp.mode is madvise
The transparent huge page mode here is `madvise`: only ranges marked with `madvise(MADV_HUGEPAGE)` get huge pages at fault time. The probe does not mark its region, so it is treated like the memory of any program that did not ask.
:::else
The transparent huge page mode here is {{obs:mem.thp.enabled}}, so faults in ordinary anonymous memory do not allocate huge pages: every page needs its own fault.
:::end
:::unknown
Machine Atlas could not read the transparent huge page mode here, so it cannot say whether this kernel offers huge pages to all anonymous memory, only to regions that ask for them, or to none; the region's own outcome above and the fault counters below are the evidence of what happened to the probe.
:::end

:::when probe.thp.fault_fallback_delta is 0
Just before and just after the write loop, the probe read the kernel's THP fault counters: thp_fault_alloc rose by {{obs:probe.thp.fault_alloc_delta}} and thp_fault_fallback by {{obs:probe.thp.fault_fallback_delta}}. No huge-page fault anywhere on the machine fell back to small pages in that instant, so if any of the region got small pages, the kernel never attempted a huge page there: the mode or the region's placement ruled it out, not a shortage of free 2 MiB blocks. The counters are system-wide, so a huge page another process faulted in at the same moment would be counted too.
:::else
Just before and just after the write loop, the probe read the kernel's THP fault counters: thp_fault_alloc rose by {{obs:probe.thp.fault_alloc_delta}} and thp_fault_fallback by {{obs:probe.thp.fault_fallback_delta}}. A fallback is a fault that tried for a huge page and took small pages instead, because no free 2 MiB block could be had or charged to the cgroup. The counters are system-wide, so they cannot say which process the fallbacks belonged to; set beside the region's huge-page fraction above, they show whether this kernel was refusing huge pages at the moment the probe asked for them.
:::unknown
The probe could not read the kernel's THP fault counters around its loop (a kernel built without transparent huge pages has neither), so whether any huge-page fault fell back in that instant is not known here.
:::end

The probe's major fault count read {{obs:probe.faults.majflt_before_touch}} before the loop and {{obs:probe.faults.majflt_after_touch}} after. On a normal run the two are equal: fresh anonymous memory is zeros, and producing zeros needs no I/O.

### The region that was only read

:::table probe.regions columns=name,pages_total,pages_present,rss_bytes,anon_huge_bytes

Look at `anon-read`. The probe read one byte from each of its pages and wrote nothing. The pagemap column `pages_present` shows whether each page has a valid translation now, and `rss_bytes` shows how much resident memory smaps charges to the region.

:::when derived.probe.read_region_rss_bytes is 0
Here the read region has translations for its pages but a resident size of {{derived:derived.probe.read_region_rss_bytes}}. Every read fault mapped the shared zero page, or, where transparent huge pages are allowed, the shared *huge* zero page, one entry covering 2 MiB. Those frames belong to no one, so smaps does not charge them to the process: 4 MiB was read, and not one byte of memory was allocated.
:::else
Here the read region shows {{derived:derived.probe.read_region_rss_bytes}} resident. The reads still mapped the shared zero page (or the huge zero page) rather than allocating, so this does not mean new memory was used; this kernel's smaps evidently counted those mappings as resident, which kernels have handled differently over time, or the huge zero page was disabled and the kernel allocated real huge pages to satisfy reads.
:::unknown
The read region's accounting is not available here. On current kernels its resident size is zero even though all its pages have translations: every read fault mapped the shared zero page (or the huge zero page), which smaps does not charge to the process.
:::end

### The whole system

:::evidence Faults since boot, all processes
- mem.vmstat.pgfault
- mem.vmstat.pgmajfault
- mem.vmstat.thp_fault_alloc
- kernel.uptime_seconds
:::end

Since it booted {{obs:kernel.uptime_seconds}} ago, this kernel has handled {{obs:mem.vmstat.pgfault}} page faults, of which {{obs:mem.vmstat.pgmajfault}} waited for I/O, and it allocated {{obs:mem.vmstat.thp_fault_alloc}} transparent huge pages directly in the fault handler. Every process start contributes: `exec` begins with an empty address space that faults its way in, page by page.

## See it

:::diagram fault-flow

The diagram follows one fault from the exception to the return, with a branch for each case above.

## Predict

:::predict
A program maps 32 MiB of new anonymous memory, aligned to 2 MiB, and writes one byte to each page. With {{obs:kernel.page_size_bytes}} pages and this machine's transparent huge page setting, how many minor faults will that take?

- About 8 192: one per 4 KiB page.
- About 16: one per 2 MiB huge page.
- Somewhere between, and it will vary from run to run.
:::reveal
:::when derived.thp.mode is always
In `always` mode the answer is about 16 when the kernel can find a free 2 MiB block for each huge page, because one fault maps 512 pages at once. When memory is fragmented, some faults fall back to small pages and the count lands between 16 and 8 192, different on each run; this kernel's defrag setting decides whether it stalls to compact memory first. A region marked `MADV_NOHUGEPAGE`, or memory in a process with THP disabled, takes about 8 192.
:::else
Here the mode is {{obs:mem.thp.enabled}}, so a region that did not ask for huge pages takes about 8 192 faults, one per 4 KiB page. In `madvise` mode, a region marked with `madvise(MADV_HUGEPAGE)` can drop to about 16, one per huge page, when contiguous memory is free; in `never` mode no request changes the answer. With fault-around, file mappings can also come in below one fault per page, but that applies to page-cache pages, not to fresh anonymous memory.
:::unknown
Machine Atlas could not read the transparent huge page mode here, so the prediction cannot be settled from this machine. With THP off or not requested, expect about 8 192 faults; with THP on and contiguous memory free, about 16; under fragmentation, anything between.
:::end

With pages larger than 4 KiB the arithmetic shifts: with 16 KiB pages, 32 MiB is 2 048 pages, and a single PMD-level huge page covers all 32 MiB.
:::end

## Try it

The opt-in experiment runs this prediction for real:

:::experiment fault-count

It maps two 32 MiB anonymous regions, one left to the system default and one marked `MADV_NOHUGEPAGE`, and counts minor faults across a first touch, a second touch, `MADV_DONTNEED` and a third touch. Expect the second touch to take almost no faults in either region: the translations exist now. `MADV_DONTNEED` throws the pages away, so the third touch faults again like the first, and finds zeros. The default region takes far fewer faults than the other only when transparent huge pages are on and contiguous memory is free; otherwise the two match.

For any running process, `ps` shows the counters `getrusage` reports:

```sh
ps -o pid,min_flt,maj_flt,cmd -p $$
```

Run it twice: the shell's minor fault count grows a little each time it starts the `ps` child, and its major count normally stays put.

## Limits

:::misconception A page fault is an error
Almost every page fault is the kernel keeping a promise on schedule: zero pages on first touch, page cache on first read, a copy on first write. Only a fault with no VMA behind it, or one the VMA forbids, is an error, and that one is called a segmentation fault.
:::end

:::misconception A major fault means swapping
A major fault means the process waited for I/O. On most machines the commonest cause is reading a program's code or a data file the first time; swap-ins are one cause among several. vmstat counts swap-ins separately, in pswpin.
:::end

:::misconception RSS rises when you allocate
`malloc` and `mmap` hand out address space. RSS rises when the program touches the memory, one page (or one huge page) per fault, and a region that is only read may never raise it at all.
:::end

:::cannot
Fault counters say how many faults happened, not which pages they were for or how long each took. Timing a fault needs tracing tools that require privileges.

The probe's pagemap readings show whether each page has a translation, but not where it points: since Linux 4.2, frame numbers read as zero without CAP_SYS_ADMIN. Whether the probe could see frame numbers here: {{obs:probe.pagemap.pfn_visible}}.

pgfault and pgmajfault in vmstat are totals since boot for every process on the machine, so a single reading cannot be attributed to anything; only the difference between two readings is a rate.

The exact count for a given touch depends on transparent huge page availability at that instant, on fault-around and on fragmentation, so the probe and the experiment report what happened once and state the expected range rather than an exact figure.
:::end

## Go deeper

Next, [physical memory](lesson:physical-memory) follows where the frames handed out by these faults come from. The branch lesson on [transparent huge pages](lesson:transparent-huge-pages) explains when one fault maps 2 MiB. In Explore, the [page fault handler](component:page-fault-handler) and [anonymous memory](component:anonymous-memory) components show what depends on this path. Planned lessons cover copy-on-write and fork, and userfaultfd, which lets a program handle faults in its own memory itself.
