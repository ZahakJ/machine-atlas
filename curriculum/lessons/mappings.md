---
{
  "id": "mappings",
  "title": "An address space is a list of promises",
  "subsystem": "memory",
  "kind": "core",
  "order": 3,
  "summary": "What mmap actually creates: the VMA, a record of what should be at each address, and how /proc/<pid>/maps and smaps let you read those records and the memory behind them.",
  "prerequisites": ["virtual-address-spaces"],
  "uses": [
    "derived.overcommit.mode_name",
    "derived.probe.untouched_rss_bytes",
    "mem.sysctl.vm.max_map_count",
    "mem.sysctl.vm.overcommit_memory",
    "probe.maps.sample",
    "probe.maps.vma_count",
    "probe.regions"
  ],
  "relevance": [],
  "next": ["page-faults"],
  "deeper": ["planned:copy-on-write-and-fork"],
  "version_notes": [
    {"applies": "Linux ≥ 6.1", "text": "A process's VMAs are indexed by a maple tree, a B-tree variant built for ranges. Before 6.1 they were kept in a red-black tree plus a sorted linked list, with a small per-thread cache of recent lookups."},
    {"applies": "Linux ≥ 4.14", "text": "/proc/<pid>/smaps_rollup gives the smaps totals for the whole process in one record, much cheaper to read than summing smaps."},
    {"applies": "Linux ≥ 5.17", "text": "Anonymous mappings may be named with prctl(PR_SET_VMA_ANON_NAME) and appear in maps as [anon:name]; a mapping with a different name will not merge with its neighbour."}
  ],
  "references": [
    {"title": "mmap(2)", "url": "https://man7.org/linux/man-pages/man2/mmap.2.html", "kind": "man-page"},
    {"title": "proc_pid_maps(5): /proc/pid/maps", "url": "https://man7.org/linux/man-pages/man5/proc_pid_maps.5.html", "kind": "man-page"},
    {"title": "proc_pid_smaps(5): /proc/pid/smaps", "url": "https://man7.org/linux/man-pages/man5/proc_pid_smaps.5.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: The /proc filesystem (maps, smaps)", "url": "https://docs.kernel.org/filesystems/proc.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Process addresses (VMAs, the maple tree, locking)", "url": "https://docs.kernel.org/mm/process_addrs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Overcommit accounting", "url": "https://docs.kernel.org/mm/overcommit-accounting.html", "kind": "kernel-doc"},
    {"title": "mallopt(3): M_MMAP_THRESHOLD", "url": "https://man7.org/linux/man-pages/man3/mallopt.3.html", "kind": "man-page"},
    {"title": "LWN: Introducing maple trees (2021)", "url": "https://lwn.net/Articles/845507/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.probe.untouched_rss_bytes is 0
Machine Atlas's probe asked the kernel for 64 MiB of fresh memory, and the kernel said yes at once. The probe never touched it, and the kernel's own accounting says how much of that region occupies RAM: {{derived:derived.probe.untouched_rss_bytes}}. Nothing. Yet the probe could have written to any byte of it, and the write would have worked. What did `mmap` actually give it?
:::else
Machine Atlas's probe asked the kernel for 64 MiB of fresh memory and never touched it, yet the kernel's accounting says {{derived:derived.probe.untouched_rss_bytes}} of that region occupies RAM. On a normal Linux system this figure is zero; something here filled the region in ahead of time. To see why zero is the normal answer and what could change it, start with what `mmap` actually gives a process.
:::unknown
Machine Atlas's probe normally asks the kernel for 64 MiB of fresh memory, never touches it, and reads back how much of it occupies RAM; that measurement is not available in this snapshot. On a normal Linux system the answer is nothing at all, although the process could write to any byte of the region at any moment. What did `mmap` actually give it?
:::end

## Why it exists

Allocating and filling physical memory at the moment it is requested would waste time and RAM: time to zero or read pages that may never be used, and frames held for reservations that are mostly empty, like the Go runtime's from the previous lesson. Programs ask for memory in large, round amounts and use it sparsely and late.

So the kernel separates the promise from its fulfilment. At request time it records, compactly, *what should be at each address*: zeros, or a given range of a given file, readable or writable, private or shared. It supplies the actual pages later, one at a time, when the program first touches them. This lesson is about the record; the next is about the fulfilment.

## How it works

### The VMA

The record is a *virtual memory area* (VMA, `struct vm_area_struct` in the kernel): a page-aligned range of addresses in which every page is treated the same way. It holds the start and end, the permissions (read, write, execute), flags (shared or private, whether it grows downwards like a stack, whether it is locked in RAM) and the backing: a file and an offset into it, or nothing, which makes it *anonymous*. A successful `mmap` creates a VMA (or extends one) and returns; it allocates no frame and writes no page-table entry.

### Private or shared, file or anonymous

Two choices describe every mapping, and together they decide what a write does:

| | private (`MAP_PRIVATE`) | shared (`MAP_SHARED`) |
|---|---|---|
| file-backed | reads see the file; the first write to a page makes a private copy (copy-on-write) that the file never sees: how executables and libraries are mapped | reads and writes go to the file's pages in the page cache, seen by every process mapping it, written back to the file later |
| anonymous | zero-filled memory private to the process: heaps, stacks, most allocations | zero-filled memory shared with child processes and anyone else holding it, such as a `memfd`; backed by the kernel's internal shmem filesystem |

The probe makes one region of each interesting kind: three private anonymous regions (untouched, written, read), a private read-only mapping of its own executable, and a shared mapping of a `memfd`, a file that exists only in memory.

### Reading /proc/<pid>/maps

Each line of `/proc/<pid>/maps` is one VMA. An invented example:

```text
7f3c89400000-7f3c89800000 r--p 00000000 08:02 1835021    /usr/lib/libexample.so
```

The address range (end exclusive); the permissions, whose fourth letter is `p` for private or `s` for shared; the offset into the file; the device and inode of the file (`00:00 0` for anonymous memory); and a path. Where there is no file the kernel prints a pseudo-name or nothing: `[heap]` for the `brk` heap, `[stack]`, `[vdso]`, `[vvar]`, and since Linux 5.17 `[anon:name]` for an anonymous region the program has named. A shared anonymous mapping shows a file it does not really have, such as `/dev/zero (deleted)` or `memfd:name (deleted)`: shared memory needs an object to share, and the kernel makes one on its internal filesystem.

### Merging and splitting

The kernel keeps as few VMAs as it can. A new mapping that directly follows an existing one with the same permissions, flags and backing (for files, the next offset) is merged into it, so a program that calls `mmap` many times may still have one VMA. The reverse happens when part of a VMA changes: `mprotect` on a page in the middle of a region splits it into three, before, changed and after; `munmap` of a middle part leaves two. Every VMA counts against vm.max_map_count, which is why programs that change protection page by page can hit that limit long before they run out of memory. The probe names each of its anonymous regions with `prctl(PR_SET_VMA_ANON_NAME)` for exactly this reason: the kernel never merges mappings whose names differ, so maps lists them apart, as `[anon: atlas probe: anon-written]` and so on, and their figures do not mix. (On a kernel without anonymous mapping names, before 5.17 or built without them, the probe falls back to separating its regions with inaccessible guard pages.)

Every page fault starts by finding the VMA that contains the faulting address, so the index must be fast. Since Linux 6.1 the VMAs of a process are kept in a *maple tree*, a B-tree designed for non-overlapping ranges that can be read without taking the address-space lock; before that, a red-black tree and a linked list.

### brk, mmap and malloc

A process has two system calls for getting memory. `brk` moves the end of the heap region up or down: cheap, but it can only shrink from the top, so a long-lived allocation near the top pins everything below it. `mmap` creates an independent region anywhere. glibc's `malloc` uses both: small requests are carved out of the `brk` heap (and per-thread arenas made with `mmap`), and requests above a threshold, 128 KiB at first and adjusted upward as large blocks are freed, get their own `mmap` so that `free` can return them to the kernel at once. `malloc` itself is a library; the kernel only ever sees `brk` and `mmap`.

### smaps: the accounting per VMA

`/proc/<pid>/smaps` repeats each maps line followed by the kernel's accounting for that VMA. `Size` is the range. `Rss` (resident set size) is how much of it is in RAM now. `Pss` (proportional set size) divides each resident page by the number of processes mapping it, so that summing Pss over all processes counts each page once. Each resident page is also classified as `Shared` or `Private` (mapped by other processes, or by this one alone) and `Clean` or `Dirty` (identical to its backing, or modified since); `Anonymous` counts pages with no file behind them, including private copies made from file pages; `Swap` counts pages currently written out to swap. `smaps_rollup` gives the same fields summed over the whole process.

### What the yes from mmap means

`mmap` answers with address space, not memory, but the kernel does keep a running total of how much private writable memory it has promised, because those pages would need RAM or swap if they were all touched. vm.overcommit_memory sets how strictly that total is policed: 0 refuses only requests that obviously cannot be met, 1 never refuses, and 2 refuses anything that would take the total past a fixed limit. Mappings made with `MAP_NORESERVE`, read-only mappings and shared file mappings are not counted at all, except in mode 2, which ignores `MAP_NORESERVE`. The lesson on [memory pressure and the OOM killer](lesson:memory-pressure-and-oom) follows what happens when too many promises are called in at once.

## On this machine

:::table probe.regions columns=name,size_bytes,perms,backing,touched_bytes,rss_bytes,pss_bytes,anonymous_bytes,path

Each region differs from the others in one respect. `anon-untouched` was mapped and never used: its resident size is {{derived:derived.probe.untouched_rss_bytes}}. `anon-written` had one byte written to every page, so all of it became resident and anonymous. `file-private` is the probe's own executable, mapped privately and read; it is resident but not anonymous, because its pages are the page cache's copies of the file, and its Pss is lower than its Rss whenever other processes map the same pages (the parent atlas process runs the same executable). `shmem-shared` is a 4 MiB `memfd` of which the probe wrote the first megabyte: only the written part is resident, and although the region is listed with a file name it counts as neither file cache nor ordinary anonymous memory, a puzzle the [anonymous and file pages](lesson:anonymous-and-file-pages) lesson resolves. `anon-read` is left for the next lesson.

The last column is what maps prints after each range. The anonymous regions carry the names the probe gave them, which is how they are told apart (an empty cell means this kernel could not name them, and the probe kept them apart with guard pages instead); the executable shows its file name, and the `memfd` its `memfd:` name, marked `(deleted)` because it was never linked into any directory.

:::evidence Mappings and promises
- probe.maps.vma_count
- mem.sysctl.vm.max_map_count
- derived.probe.untouched_rss_bytes
- mem.sysctl.vm.overcommit_memory
- derived.overcommit.mode_name
:::end

The probe's whole address space is {{obs:probe.maps.vma_count}} VMAs, against a limit of {{obs:mem.sysctl.vm.max_map_count}} per process. Its first mappings, as the kernel lists them:

:::table probe.maps.sample columns=start,end,perms,offset,kind,path limit=24

The `kind` column is Machine Atlas's reading of each line (file, anonymous, shmem, stack, kernel-provided), and paths are reduced to file names.

:::when derived.overcommit.mode_name is heuristic
The overcommit policy here is the default heuristic (mode {{obs:mem.sysctl.vm.overcommit_memory}}): the probe's 76 MiB of private writable anonymous regions were added to the system's committed total, but nothing was checked beyond whether such a request could ever be satisfied.
:::else
The overcommit policy here is not the default (mode {{obs:mem.sysctl.vm.overcommit_memory}}, {{derived:derived.overcommit.mode_name}}). In mode 1 every request succeeds whatever the total; in mode 2 the probe's private writable regions had to fit under the commit limit, and a large enough request would have failed at `mmap` time instead of at first touch.
:::end

## See it

:::diagram probe-regions

Each bar is one region at its mapped size, with the resident part filled in. The gap between the two is promise not yet called in.

## Predict

:::predict
Before looking at the table above: of the probe's five regions, which would you expect to have resident memory, and how much of each?

- All five, at their full mapped size: mmap allocated them.
- Only the parts that were touched: all of anon-written, all of file-private, the first megabyte of shmem-shared, none of anon-untouched.
- Only anon-written, because it is the only region that was written.
:::reveal
The second: memory becomes resident when it is touched, not when it is mapped, and reading counts as touching for a file mapping, because the data has to be brought into a frame to be read. So the untouched region has {{derived:derived.probe.untouched_rss_bytes}} resident, while the read-only file mapping is fully resident without a single write. The odd one out is `anon-read`, which was read in full but holds anonymous memory that has never been written: there is nothing to read yet. Check its row in the table, then see the [page faults](lesson:page-faults) lesson for why its answer is what it is.
:::end

## Try it

This Python script maps 64 MiB of private anonymous memory, prints the VMA that contains it, writes 16 MiB, and prints it again:

```python
import ctypes, mmap
m = mmap.mmap(-1, 64 << 20, flags=mmap.MAP_PRIVATE | mmap.MAP_ANONYMOUS)
addr = ctypes.addressof(ctypes.c_char.from_buffer(m))
def show(when):
    inside = False
    for line in open("/proc/self/smaps"):
        f = line.split()
        if not f[0].endswith(":"):
            lo, hi = (int(x, 16) for x in f[0].split("-"))
            inside = lo <= addr < hi
            if inside: print(when, line.strip())
        elif inside and f[0] in ("Size:", "Rss:", "AnonHugePages:"):
            print("   ", line.strip())
show("mapped:")
m[: 16 << 20] = bytes(16 << 20)
show("written:")
```

Expect `Rss` near zero after the mapping and about 16 MiB higher after the write. `Size` may come out larger than 64 MiB, and `Rss` slightly above zero at first: the new region sat next to an anonymous mapping of Python's own with the same permissions, and the kernel merged the two into one VMA. `AnonHugePages` may show some of the written memory as huge pages, the subject of the [transparent huge pages](lesson:transparent-huge-pages) lesson. For a whole process at once, `cat /proc/self/smaps_rollup`.

## Limits

:::misconception mmap allocates memory
It creates a VMA: a record that says what the range should contain. No frame is allocated and no page-table entry written until the program touches a page. That is why a 64 MiB mapping returns immediately, and why a program can map far more than the machine has.
:::end

:::misconception Shared library memory is counted once per process
`Rss` counts every resident page a process maps, including the libc and executable pages that hundreds of other processes map too, so it charges shared pages to every one of them. `Pss` divides each page by the number of processes sharing it, which is why Pss sums across processes and Rss does not.
:::end

:::cannot
maps lists what is promised, not what is present: a VMA's range says nothing about how many of its pages are resident. smaps gives the totals per VMA, but not which pages; that needs pagemap, the next lesson's tool.

Rss is not additive across processes: summing it double-counts every shared page, and even Pss is a fair share rather than a cost, because a shared page would not be freed by ending one of the processes that share it.

A successful `mmap` does not guarantee that touching the memory later will succeed. Under the default overcommit policy the kernel promises more than it has; if the promises are called in together, a later touch can find no frame, and the outcome is reclaim, swap or the OOM killer, not an error from `mmap`.
:::end

## Go deeper

Next, [page faults](lesson:page-faults) shows how and when the kernel keeps these promises. In Explore, [VMAs](component:vmas) and the [probe process](component:probe-process) connect the regions above to the rest of the memory system. The planned lesson on copy-on-write and fork follows what happens to private mappings when a process is copied.
