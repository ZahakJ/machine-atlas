---
{
  "id": "anonymous-and-file-pages",
  "title": "Two kinds of page, told apart by what backs them",
  "subsystem": "memory",
  "kind": "core",
  "order": 6,
  "summary": "Why the kernel can drop some pages instantly and must work hard to free others: file-backed, anonymous and shared memory pages, and how /proc/meminfo and a process's status file account for each.",
  "prerequisites": ["physical-memory"],
  "uses": [
    "derived.mem.accounted_bytes",
    "derived.mem.file_cache_bytes",
    "derived.mem.unaccounted_bytes",
    "derived.swap.configured",
    "derived.swap.total_bytes",
    "mem.meminfo.Active_anon",
    "mem.meminfo.Active_file",
    "mem.meminfo.AnonPages",
    "mem.meminfo.Buffers",
    "mem.meminfo.Cached",
    "mem.meminfo.Inactive_anon",
    "mem.meminfo.Inactive_file",
    "mem.meminfo.KernelStack",
    "mem.meminfo.Mapped",
    "mem.meminfo.MemFree",
    "mem.meminfo.MemTotal",
    "mem.meminfo.Mlocked",
    "mem.meminfo.PageTables",
    "mem.meminfo.Percpu",
    "mem.meminfo.SReclaimable",
    "mem.meminfo.SUnreclaim",
    "mem.meminfo.Shmem",
    "mem.meminfo.Slab",
    "mem.meminfo.SwapCached",
    "mem.meminfo.Unevictable",
    "mem.meminfo.VmallocUsed",
    "probe.regions",
    "probe.status.RssAnon",
    "probe.status.RssFile",
    "probe.status.RssShmem"
  ],
  "relevance": [
    {"when": "derived.swap.configured", "is": false, "note": "There is no swap here, so every anonymous and shared memory page on this machine stays in RAM until it is freed."}
  ],
  "next": ["page-cache"],
  "deeper": ["planned:ksm", "planned:slab-allocator", "planned:copy-on-write-and-fork"],
  "version_notes": [
    {"applies": "Linux ≥ 4.5", "text": "/proc/<pid>/status splits VmRSS into RssAnon, RssFile and RssShmem. Older kernels give only the total."},
    {"applies": "Linux 4.4 to 5.2", "text": "VmallocUsed always reads 0: the figure was dropped because computing it was slow, and restored in 5.3 from a cheap counter. On these kernels vmalloc memory falls into the unaccounted remainder."},
    {"applies": "Linux ≥ 4.16", "text": "meminfo has a Percpu line. Before it, per-CPU allocations were counted in no meminfo field at all."},
    {"applies": "Linux ≥ 4.20", "text": "KReclaimable reports reclaimable slab together with other kernel memory the kernel can drop under pressure; SReclaimable remains the slab part."},
    {"applies": "Linux ≥ 5.19", "text": "With zswap built in, meminfo adds Zswap (the compressed pool) and Zswapped (the pages it holds); before that the pool was counted nowhere in meminfo."}
  ],
  "references": [
    {"title": "Linux kernel documentation: The /proc filesystem, /proc/meminfo", "url": "https://docs.kernel.org/filesystems/proc.html#meminfo", "kind": "kernel-doc"},
    {"title": "proc_meminfo(5)", "url": "https://man7.org/linux/man-pages/man5/proc_meminfo.5.html", "kind": "man-page"},
    {"title": "proc_pid_status(5): RssAnon, RssFile, RssShmem", "url": "https://man7.org/linux/man-pages/man5/proc_pid_status.5.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: Tmpfs", "url": "https://docs.kernel.org/filesystems/tmpfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Concepts overview (page cache, anonymous memory)", "url": "https://docs.kernel.org/admin-guide/mm/concepts.html", "kind": "kernel-doc"},
    {"title": "memfd_create(2)", "url": "https://man7.org/linux/man-pages/man2/memfd_create.2.html", "kind": "man-page"},
    {"title": "Kernel source: fs/proc/meminfo.c (how each meminfo line is computed)", "url": "https://github.com/torvalds/linux/blob/master/fs/proc/meminfo.c", "kind": "source"}
  ]
}
---
## Question

This machine's kernel manages {{obs:mem.meminfo.MemTotal}} of RAM. Some of the frames in it could be handed to a new program within microseconds: the kernel would simply forget what they held. Others can only be freed by writing their contents somewhere first, and some cannot be freed at all while their owner lives. Nothing in a frame says which kind it is; the bytes look the same. So what decides it, and how much of this machine's memory is of each kind?

## Why it exists

Previous lessons followed frames being handed out. Sooner or later the kernel needs some back, and to take a frame away from a page without breaking the program that uses it, the kernel must be able to recreate the contents when the program touches that page again. Freeing a frame is therefore never just a question of whether the page is in use; it is a question of where else the contents exist.

That question has only a few answers, and they divide memory into kinds whose reclaim costs differ by orders of magnitude. Every later lesson, on the page cache, on reclaim and on running out of memory, depends on this division, so it is worth getting exactly right.

## How it works

### File-backed pages

A *file-backed* page holds a copy of part of a file. Every file read and write goes through such pages, and a mapping of a file, an executable's code or a library, uses the very same pages. While the page is *clean*, identical to what is on storage, the kernel can drop it at any time: unmap it from any page tables that point to it, free the frame, and read the file again if anyone asks. A *dirty* page, one written since it was read, must first be written back to the file. Either way the file is the backing store, and the cost of reclaim is at most one write now and one read later.

### Anonymous pages

An *anonymous* page has no file behind it: heap, stack, and the zero-filled memory a program gets from `mmap` without a file. Its only copy is the one in RAM. To free its frame the kernel must write it to *swap*, an area of disk (or compressed RAM) set aside to hold such pages, and fault it back in when touched. Without swap there is nowhere to put it, and an anonymous page stays in RAM until its owner frees it or exits.

A private file mapping that has been written belongs here too. The first write to a private page of a file triggers copy-on-write: the process gets its own copy, and that copy is anonymous, because writing it back to the file would change the file for everyone else.

### Shared memory: a file with no disk

*Shmem* is the third kind, and the one that confuses the accounting. tmpfs files (`/dev/shm`, often `/tmp`), `memfd_create` files, shared anonymous mappings and System V shared memory are all implemented as files of an internal in-memory filesystem. They have inodes, they live in the page cache, and in `/proc/<pid>/maps` they appear with a path. But the page cache is their only copy: there is no disk file to reread them from. So they behave like anonymous memory: they can be reclaimed only to swap, and the kernel keeps them on the anonymous LRU lists, the lists reclaim uses to rank swap-backed pages.

### Kernel memory

The kernel allocates for itself too: slab caches of small objects (inodes, directory entries, network buffers), a stack per thread, the page tables of every process, per-CPU variables, and large virtually contiguous buffers from vmalloc. None of this is on the LRU lists. Part of the slab, mainly the inode and dentry caches, can be shrunk on request; the rest stays until the code that allocated it frees it.

### How meminfo slices it

`/proc/meminfo` is a set of independent counters, not a partition. The fields that matter here:

- `Cached` is the page cache: file pages and shmem pages, minus pages in the swap cache and minus `Buffers`.
- `Buffers` is the page cache of block devices themselves, mostly filesystem metadata.
- `Shmem` is all shmem, and every byte of it is also inside `Cached`.
- `AnonPages` counts anonymous pages mapped into some process's page tables. Shmem is not in it.
- `Mapped` counts page cache pages (including shmem) currently mapped by at least one process, once each however many map them.
- `Active(anon)`, `Inactive(anon)`, `Active(file)` and `Inactive(file)` are the LRU lists. The anon lists hold anonymous pages and shmem; the file lists hold the rest of the page cache.
- `Unevictable` and `Mlocked` are pages reclaim may not touch: `mlock`ed memory, ramfs, locked shared memory segments.
- `Slab` (split into `SReclaimable` and `SUnreclaim`), `KernelStack`, `PageTables`, `Percpu` and `VmallocUsed` are the kernel's own.

Because these are counters of different properties, they overlap. A shmem page is in `Cached`, `Shmem`, an anon LRU list and, if mapped, `Mapped`. Other memory is in none of them: pages a driver took directly from the page allocator, free pages parked on per-CPU lists, the hugetlb pool. So no sum of fields equals `MemTotal`, and no exact one can be written.

### The per-process view

`/proc/<pid>/status` splits a process's resident memory the same way: `RssAnon` (its anonymous pages), `RssFile` (resident file pages it maps) and `RssShmem` (resident shmem it maps). File and shmem pages shared with other processes are counted in full in each of them, which is why these figures cannot be summed across processes.

## On this machine

:::evidence What meminfo reports
- mem.meminfo.MemTotal
- mem.meminfo.MemFree
- mem.meminfo.Cached | Page cache, including shmem
- mem.meminfo.Buffers
- mem.meminfo.Shmem | Shared memory and tmpfs (inside Cached)
- mem.meminfo.AnonPages
- mem.meminfo.Mapped
- mem.meminfo.SwapCached | Swapped pages also still in RAM
- mem.meminfo.Active_anon
- mem.meminfo.Inactive_anon
- mem.meminfo.Active_file
- mem.meminfo.Inactive_file
- mem.meminfo.Unevictable
- mem.meminfo.Mlocked
- mem.meminfo.Slab
- mem.meminfo.SReclaimable
- mem.meminfo.SUnreclaim
- mem.meminfo.KernelStack
- mem.meminfo.PageTables
- mem.meminfo.Percpu
- mem.meminfo.VmallocUsed
:::end

Taking shmem out of the page cache leaves {{derived:derived.mem.file_cache_bytes}} of memory that holds copies of files (`Cached + Buffers − Shmem`): the kind the kernel can drop once it is clean. Compare it with the two file LRU lists, {{obs:mem.meminfo.Active_file}} and {{obs:mem.meminfo.Inactive_file}}. They should come close to the same total, because they are the same pages counted another way; they differ by file pages that are unevictable (locked, or on ramfs) and by the moment-to-moment drift of counters read one after another.

On the other side, the anonymous lists, {{obs:mem.meminfo.Active_anon}} and {{obs:mem.meminfo.Inactive_anon}}, hold the {{obs:mem.meminfo.AnonPages}} of mapped anonymous memory, the {{obs:mem.meminfo.Shmem}} of shared memory and, where swap is in use, pages that already have a copy in swap but are mapped by no process at the moment (they are part of SwapCached, {{obs:mem.meminfo.SwapCached}} here). Their total is the memory that needs swap to be reclaimed.

:::when derived.swap.configured
This machine has {{derived:derived.swap.total_bytes}} of swap, so these pages can leave RAM, at the price of a write now and a major fault when they are next touched.
:::else
This machine has no swap ({{derived:derived.swap.total_bytes}}). Every page on the anonymous lists, shmem included, stays in RAM until whatever owns it frees it: the kernel has nowhere to put it. When memory runs short here, only file pages and reclaimable kernel caches can give way, a situation the [reclaim and swap](lesson:reclaim-and-swap) lesson follows.
:::end

### Adding it up

Machine Atlas adds the main consumers, `MemFree + Buffers + Cached + AnonPages + Slab + KernelStack + PageTables + Percpu + VmallocUsed`, and gets {{derived:derived.mem.accounted_bytes}}. `Cached` already contains shmem and `AnonPages` does not, so shared memory is counted once. Subtracting from MemTotal leaves {{derived:derived.mem.unaccounted_bytes}} that meminfo attributes to none of these fields.

A positive remainder is memory none of those fields covers: pages drivers took straight from the page allocator (graphics buffers are often the largest, network drivers' receive pools another), the hugetlb pool, free pages sitting on per-CPU lists, which are not counted in MemFree, swap cache pages no process maps, and the compressed zswap pool where zswap is in use (since Linux 5.19 meminfo reports it on a line of its own, Zswap, that this sum does not include). A negative remainder means some memory was counted twice: with stacks allocated from vmalloc (the default on x86-64 and arm64) every kernel stack is in both `KernelStack` and `VmallocUsed`, and fields read microseconds apart can each include a page that moved between them. Either way the figure measures the limits of meminfo, not a leak; the inspector shows the exact rule.

### The probe, one region of each kind

:::table probe.regions columns=name,backing,size_bytes,rss_bytes,anonymous_bytes,shared_clean_bytes,shared_dirty_bytes,private_clean_bytes,private_dirty_bytes

The probe made one mapping of each kind. `anon-written` is resident, all of it anonymous and private dirty: its only copy is in RAM. `file-private`, the probe's own executable, is resident with no anonymous bytes, because it was only read: its pages are the page cache's copy of the file, and the parent atlas process maps the same pages, which is what the shared columns count. (If the executable was written moments before, as by `go build`, those page cache pages may still be dirty.) `shmem-shared` is the `memfd`: it is listed with a file name, yet the megabyte the probe wrote shows as private dirty and not anonymous, and it is in neither of the probe's other two figures:

:::evidence The probe process's own split
- probe.status.RssAnon
- probe.status.RssFile
- probe.status.RssShmem
:::end

`RssShmem` holds the memfd's megabyte, and since the probe also mapped the memfd, that megabyte is counted in the system's `Shmem`, `Cached` and `Mapped`. If `RssShmem` is larger than one megabyte, the likely reason is that the atlas executable itself sits on tmpfs (as it does under `go run`), which makes `file-private` shmem too.

## See it

:::diagram meminfo-breakdown

The diagram draws this machine's meminfo fields to scale and marks which overlap. Its unaccounted band is the same remainder as above.

## Predict

:::predict
The probe wrote 1 MiB into a `memfd` and mapped it. Where does that megabyte appear in the system's accounting, and can the kernel drop it under pressure if there is no swap?

- In AnonPages, and it can be dropped like other unused memory.
- In Cached, so it can be dropped like other cached file data.
- In Cached and Shmem but not AnonPages, and without swap it cannot be dropped at all.
:::reveal
It is in `Cached` and `Shmem` (and `Mapped`, while the probe maps it), not in `AnonPages`; the probe's own `RssShmem` is {{obs:probe.status.RssShmem}}. A memfd is a file of the kernel's internal tmpfs, so its pages sit in the page cache. But the page cache is the only copy, so the kernel cannot drop them. Only swap could free those frames:

:::when derived.swap.configured
here, with {{derived:derived.swap.total_bytes}} of swap, it could write them out and read them back on the next touch.
:::else
here, with no swap ({{derived:derived.swap.total_bytes}}), those frames stay occupied until the memfd is closed and unmapped by everyone, however long it goes unused.
:::end

The first answer mistakes the backing: there is no anonymous mapping, only a shared file. The second is the common mistake that makes `Cached` look more reclaimable than it is.
:::end

## Try it

Read the same counters yourself, then compare one tmpfs mount with the shmem total:

```sh
grep -E '^(AnonPages|Mapped|Shmem|Cached|Buffers|Active|Inactive)' /proc/meminfo
df -h /dev/shm
grep -E '^Rss' /proc/self/status
```

`df` reports what the files in `/dev/shm` occupy, usually a small part of `Shmem`, which also counts every other tmpfs mount, memfds such as Wayland and graphics buffers, and shared anonymous mappings. The last command prints the split for `grep` itself: some anonymous memory, a few megabytes of file pages from its executable and libraries, and normally no shmem.

## Limits

:::misconception Cached is file cache that can always be given back
Cached includes shmem, here {{obs:mem.meminfo.Shmem}}, which has no file to reread from and can only go to swap. Even the true file cache has to be written back first where it is dirty, and pages in active use are taken last. Subtracting shmem, as `derived.mem.file_cache_bytes` does, gives a better estimate of what is droppable; the next lesson shows how MemAvailable refines it further.
:::end

:::misconception Summing RSS over all processes gives memory used
RSS counts every shared page in full in every process that maps it: one libc, one executable's code, one shared memory segment, counted once per process. The sum exceeds what is used whenever anything is shared. Pss divides shared pages among their users, and even its sum leaves out the page cache nobody maps and all kernel memory.
:::end

:::cannot
meminfo fields are separate counters read one after another without a global lock, and several count the same pages from different angles, so no arithmetic on them is exact. Some kernel allocations appear in no field at all; the unaccounted remainder above names the usual contributors but cannot say which of them it is on this machine.

Which pages a frame holds, and whether it is anonymous, file, shmem, dirty or locked, is recorded per page in `/proc/kpageflags`, readable only by root; unprivileged programs see totals. The probe is the exception only because its own history is known.

RssAnon, RssFile and RssShmem are maintained per process with batched updates, so they can lag the truth slightly; smaps and smaps_rollup are exact but slower to read.
:::end

## Go deeper

Next, the [page cache](lesson:page-cache) looks at the file-backed side: why it fills memory and why that is the plan. After it, [reclaim and swap](lesson:reclaim-and-swap) shows how the kernel uses the distinction drawn here to pick victims. In Explore, [anonymous memory](component:anonymous-memory), the [page cache](component:page-cache) and [shared memory](component:shmem) show what depends on each kind. Planned lessons cover the slab allocator (the kernel's own small objects), copy-on-write and fork, and kernel same-page merging, which turns identical anonymous pages into one shared copy.
