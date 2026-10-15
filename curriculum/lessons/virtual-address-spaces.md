---
{
  "id": "virtual-address-spaces",
  "title": "Every process gets the whole machine",
  "subsystem": "memory",
  "kind": "core",
  "order": 2,
  "summary": "One page-table tree per process: what the kernel puts in an address space, where it puts it, why the layout moves on every run, and why virtual size costs almost nothing.",
  "prerequisites": ["cpu-addressing"],
  "uses": [
    "derived.env.arch_family",
    "derived.paging.levels",
    "derived.paging.user_space_bytes",
    "derived.probe.aslr_differs",
    "mem.sysctl.kernel.randomize_va_space",
    "mem.sysctl.vm.max_map_count",
    "probe.layout.runs",
    "probe.maps.highest_end",
    "probe.maps.lowest_start",
    "probe.maps.vma_count",
    "probe.status.VmRSS",
    "probe.status.VmSize"
  ],
  "relevance": [],
  "next": ["mappings"],
  "deeper": ["planned:copy-on-write-and-fork", "planned:tlb-and-pcid"],
  "version_notes": [
    {"applies": "Linux ≥ 4.15 (x86-64)", "text": "Kernel page-table isolation (KPTI) gives each process a second, stripped-down page-table tree for user mode on CPUs affected by Meltdown; unaffected CPUs keep the single tree with the kernel mapped in."},
    {"applies": "Linux ≥ 4.5", "text": "The number of random bits in the mmap base became tunable as vm.mmap_rnd_bits, with a build-time default of 28 on x86-64."},
    {"applies": "Linux ≥ 5.17", "text": "Anonymous mappings can carry a name set with prctl(PR_SET_VMA_ANON_NAME), shown in maps as [anon:name]; Go 1.25 and later use it to label their heap, which is how the probe's maps show [anon: Go: heap] on recent kernels."},
    {"applies": "Linux ≥ 4.14 (x86-64, 5-level paging)", "text": "With 5-level paging, mmap still returns addresses below 2^47 unless a program passes a hint address above it, so ordinary processes keep the 4-level layout."}
  ],
  "references": [
    {"title": "proc_pid_maps(5): /proc/pid/maps", "url": "https://man7.org/linux/man-pages/man5/proc_pid_maps.5.html", "kind": "man-page"},
    {"title": "Linux kernel documentation: /proc/sys/kernel/, randomize_va_space", "url": "https://docs.kernel.org/admin-guide/sysctl/kernel.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/vm/, max_map_count and mmap_rnd_bits", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Memory management on x86-64 (virtual memory map)", "url": "https://docs.kernel.org/arch/x86/x86_64/mm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: 5-level paging", "url": "https://docs.kernel.org/arch/x86/x86_64/5level-paging.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Page Table Isolation (PTI)", "url": "https://docs.kernel.org/arch/x86/pti.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Process addresses", "url": "https://docs.kernel.org/mm/process_addrs.html", "kind": "kernel-doc"},
    {"title": "mmap(2)", "url": "https://man7.org/linux/man-pages/man2/mmap.2.html", "kind": "man-page"},
    {"title": "Go 1.26 release notes (heap base randomisation)", "url": "https://go.dev/doc/go1.26", "kind": "other"}
  ]
}
---
## Question

:::when derived.probe.aslr_differs
Machine Atlas started its probe program twice, a moment apart, from the same file on the same machine. The table below shows where four landmarks of its memory ended up each time, and they did not all land in the same places. Meanwhile any two programs running here can each keep different data at the very same address without either noticing the other. If an address does not name a place in memory, what does it name, and who decides?
:::else
Machine Atlas started its probe program twice and recorded where four landmarks of its memory ended up. On this machine they landed at the same addresses both times, which is not what a modern Linux system normally does. Meanwhile any two programs running here can each keep different data at the very same address without either noticing the other. If an address does not name a place in memory, what does it name, and who decides where things go?
:::unknown
Machine Atlas normally starts a small probe program twice and compares where its memory landmarks end up; that comparison is not available in this snapshot. The puzzle stands without it: any two programs running here can each keep different data at the very same address without either noticing the other. If an address does not name a place in memory, what does it name, and who decides where things go?
:::end

:::table probe.layout.runs columns=run,exe,heap,heap_kind,mmap_base,stack

## Why it exists

The previous lesson showed that every address a program uses is translated by the MMU through a tree of page tables. Nothing requires every program to use the same tree. Giving each process its own buys three things at once.

Isolation: a process can only reach the frames its own tree points to, so a stray pointer in one program cannot overwrite another's data or the kernel's. Simplicity: every program can be linked and loaded as if it were alone on the machine, with its code, heap and stack at conventional addresses, without coordinating with anything else that runs. And independence from RAM: the range a process may use is set by the address format, not by how much memory is fitted, so a program can reserve far more address space than it will ever touch and pay only for what it uses.

That set of addresses, together with the tree that translates them, is the process's *address space*.

## How it works

### One tree per process

The kernel describes each address space with an `mm_struct`: the root of its page-table tree, the list of mapped regions (the next lesson), and counters. Every process has one; the threads of a process share it, which is what makes them threads rather than separate processes. When the scheduler switches the CPU from one process to another, it loads the new process's root table into the root register (CR3 on x86-64, TTBR0_EL1 on arm64, satp on RISC-V), and from the next instruction on every address means something different. Switching between threads of one process skips that step. Translations cached in the TLB would be wrong after a switch, so CPUs tag them with an address-space number (PCID on x86-64, ASID on arm64) instead of flushing everything.

### The kernel's half

The translated range is split in two. The lower half belongs to user space; the upper half is the kernel's, and the same kernel mappings appear in every address space. A system call or an interrupt therefore runs kernel code without changing the root register: the kernel's code and data are already mapped, and page-table entries marked supervisor-only keep user mode from touching them.

:::when derived.env.arch_family is x86
On x86-64 one tree covers both halves: the kernel's top-level entries are copied into every process's root table. The Meltdown flaw in many Intel CPUs let user code read supervisor-only memory speculatively, so since Linux 4.15 affected machines run with kernel page-table isolation (KPTI): user mode runs on a second tree that maps only the kernel's entry and exit code, and every kernel entry switches root registers, at a measurable cost in system-call-heavy work. CPUs that are not affected keep the single tree.
:::else
On arm64 the halves have separate trees: TTBR0_EL1 points at the current process's user tree and TTBR1_EL1 at the kernel's, which is the same for every process, so a context switch only changes TTBR0. The few arm64 cores affected by Meltdown get an equivalent of x86's page-table isolation, which unmaps the kernel while user code runs.
:::end

User space here spans {{derived:derived.paging.user_space_bytes}}. The kernel half is never listed in a process's maps, because the process cannot use it.

### The conventional layout

Inside the user half, the kernel and the program loader place things by convention, from the bottom up:

- The first pages are left unmapped (vm.mmap_min_addr; distributions commonly set 64 KiB), so a null pointer dereference faults instead of reading something.
- The executable's code, read-only data and writable data, followed by the *heap*: the region the classic `brk` system call grows upwards. C allocators use it for small allocations.
- A large gap, then the *mmap area*, which grows downwards from below the stack: shared libraries, large allocations, file mappings and thread stacks all land here, each new mapping below the last.
- The main thread's *stack* near the top, growing down, and next to it the `[vdso]` (a small shared library the kernel maps into every process so calls like `clock_gettime` need no system call) and `[vvar]`, the data it reads.

The probe is a Go program, which changes one detail: the Go runtime does not use `brk`. It reserves its own heap with `mmap`, at an address it chooses, and on recent kernels and Go versions labels it `[anon: Go: heap]` in maps. A `[heap]` (`brk`) mapping appears only if C code in the process called `malloc`, as it can when Machine Atlas was built with cgo. The "heap" column above shows that `[heap]` mapping when the probe has one, and otherwise the Go runtime's labelled arena, and the "heap_kind" column says which it is: `brk` for the `[heap]` mapping, `go-arena` for the Go arena. Both cells are empty when the probe had neither, as on a kernel too old to show the Go label.

### Address space layout randomisation

If every process put its stack and libraries at the same addresses, an attacker who found a memory-corruption bug would know exactly where to jump. Address space layout randomisation (ASLR) moves the landmarks by a random offset each time a program starts. It is controlled by kernel.randomize_va_space: 0 turns it off; 1 randomises the stack, the mmap base and the vDSO (and so every shared library, and the code of position-independent executables); 2, the default, also randomises the start of the `brk` heap. The amount of randomness in the mmap base is vm.mmap_rnd_bits, which root may tune within limits fixed when the kernel was built: 28 bits of page number by default on x86-64, so the base can fall anywhere in a range of 2^28 pages, 1 TiB. An executable that is not position-independent was linked for one address and is always loaded there, whatever the setting.

### Virtual size is a promise, not a cost

Mapping address space costs a few hundred bytes of bookkeeping and nothing more until the memory is touched; frames are attached later, page by page (the page-faults lesson). So a process's *virtual size*, `VmSize` in `/proc/<pid>/status`, the sum of everything mapped, can be many times its *resident set size*, `VmRSS`, the part currently in RAM. Runtimes exploit this: Go reserves address space for its heap and its own metadata up front and touches it only as it grows.

### Limits on an address space

Three ceilings apply. The format: user space here ends at {{derived:derived.paging.user_space_bytes}}. The number of mappings: vm.max_map_count, here {{obs:mem.sysctl.vm.max_map_count}}; beyond it `mmap` fails with ENOMEM however small the request. And resource limits: `RLIMIT_AS` caps total virtual size and `RLIMIT_DATA` the private writable part, both unlimited by default and set with `ulimit -v` or `setrlimit(2)`.

:::when derived.paging.levels is 5
This machine appears to run 5-level paging, so the format allows a far larger user space. The kernel still hands out addresses below 2^47 unless a program asks for higher ones with a hint address to `mmap`, because some programs keep tags in the top bits of their pointers and would break.
:::else
Few programs come near this ceiling; the mapping count and the resource limits are the ones that bite.
:::unknown
Machine Atlas could not infer how many page-table levels this kernel uses, so the size of user space above is not determined either. It is set by the virtual address width the kernel was configured for.
:::end

### fork and exec

`fork` gives the child a copy of the parent's address space without copying its memory: both trees point at the same frames, marked read-only, and a page is copied only when one side writes to it (copy-on-write, a planned lesson). `exec` throws the address space away and builds a new one from the program file, which is why it is where ASLR picks fresh offsets.

## On this machine

:::evidence The probe's address space
- probe.maps.lowest_start | Lowest mapped address (usually the executable)
- probe.maps.highest_end | Highest user mapping end (stack or vDSO)
- probe.maps.vma_count
- probe.status.VmSize
- probe.status.VmRSS
- mem.sysctl.kernel.randomize_va_space
- derived.probe.aslr_differs
- mem.sysctl.vm.max_map_count
- derived.paging.user_space_bytes
:::end

The probe's mappings run from {{obs:probe.maps.lowest_start}} to {{obs:probe.maps.highest_end}}, in {{obs:probe.maps.vma_count}} separate regions. Everything outside those regions is unmapped, and an access there would end the program with a segmentation fault (except just below the stack, which the kernel extends on demand).

The probe maps about 84 MiB of regions of its own for the lessons that follow, and touches only part of them. Its virtual size is {{obs:probe.status.VmSize}}, of which {{obs:probe.status.VmRSS}} is resident; the rest is address space the Go runtime and the probe reserved and never touched. Reserving it cost no frames and, beyond the tables that cover the touched parts, no page-table pages.

:::when mem.sysctl.kernel.randomize_va_space is 2
ASLR is in its default, full mode here (randomize_va_space is {{obs:mem.sysctl.kernel.randomize_va_space}}): stack, mmap base, vDSO and `brk` heap all move between runs.
:::else
ASLR is not in its default mode here (randomize_va_space is {{obs:mem.sysctl.kernel.randomize_va_space}}). With 1 the stack, mmap base and vDSO still move but a `brk` heap does not; with 0 nothing moves. Distributions ship 2; a lower value is usually a deliberate choice for debugging or for an old program that assumes a fixed layout.
:::end

## See it

:::diagram address-space-map

The map draws the probe's real mappings on a scale of the whole user range, which is why almost all of it is empty.

## Predict

:::predict
Look at the probe's two runs. Of the four landmarks (executable, heap, mmap base and stack), which would you expect to differ between runs on a default Linux system, and in roughly which bits of the address?

- All four, in every bit.
- Stack and mmap base, in the middle bits; the executable stays put; the heap depends on the runtime.
- None: a program loaded from the same file lands at the same addresses.
:::reveal
:::when derived.probe.aslr_differs
They differed here. Compare the columns: the stack and the mmap base move, while the low 12 bits of the mmap base (the offset within a page) and the top bits (the region of the address space) stay fixed; the randomness is in between, about 28 bits of page number on x86-64. (The kernel also offsets the initial stack pointer within its page, but the column shows where the stack mapping starts, which is always page-aligned, so that part is not visible here.) The executable does not move, because Go builds ordinary executables that are not position-independent unless asked to (`-buildmode=pie`), linked to load at one fixed address. Whether the heap moves depends on its kind, in the heap_kind column. A `[heap]` (`brk`) mapping starts at a random offset after the executable's data when randomize_va_space is 2. The Go runtime's own arena is placed by the runtime, not the kernel: since Go 1.26 it picks a random heap base itself, older versions used a fixed hint address, and randomize_va_space has no say either way.
:::else
They did not differ here, which means randomisation is off: randomize_va_space is {{obs:mem.sysctl.kernel.randomize_va_space}}, or the program was started with randomisation disabled for it (`setarch -R`, which debuggers use so addresses are reproducible). On a default system the stack and the mmap base would differ in the middle bits, about 28 bits of page number on x86-64, while the executable, which is not position-independent, would stay put regardless.
:::unknown
The probe's runs are not available here. On a default system the stack and the mmap base differ in the middle bits, about 28 bits of page number on x86-64; the page offset and the topmost bits stay fixed; a non-position-independent executable such as the probe never moves; and the heap moves if it is a `brk` heap (randomize_va_space 2) or a Go runtime arena from Go 1.26 or later, which randomises its own base.
:::end

With randomize_va_space set to 0, all four would be identical between runs.
:::end

## Try it

Two read-only commands on your own shell process:

```sh
cat /proc/self/maps; cat /proc/self/maps
grep -E '^Vm(Size|RSS)' /proc/self/status
```

Each `cat` in the first line is a new process, so the two listings should show the same kinds of line (the `cat` binary, its libraries, `[heap]`, `[stack]`, `[vvar]`, `[vdso]`) at different addresses, except for the executable itself if it is not position-independent. Most distributions now build their programs as position-independent executables, so even `cat`'s code moves. The second command shows the same promise-versus-cost gap as the probe: a `VmSize` of several megabytes and a `VmRSS` well below it.

## Limits

:::misconception VmSize is memory used
VmSize counts every mapped address, touched or not, including reservations that will never be touched and shared libraries whose pages are shared with every other process. Here the probe's {{obs:probe.status.VmSize}} of virtual size has {{obs:probe.status.VmRSS}} resident. Even VmRSS overstates the probe's own cost, because pages shared with other processes count in full; the mappings lesson shows the fairer figure.
:::end

:::misconception A process's addresses say where its data is in RAM
They say where it is in that process's address space. The same address in two processes is translated through two different trees to unrelated frames, and one frame can appear at different addresses in two processes. The physical location is chosen by the kernel when a page is first touched and may change later (migration, compaction, swap) without the address changing.
:::end

:::cannot
That two runs differed shows that randomisation happened, not how much: two samples cannot measure entropy. The bit count comes from the kernel's configuration (vm.mmap_rnd_bits, which only root can read), not from this comparison.

Machine Atlas reads only its own probe's maps. Another user's `/proc/<pid>/maps` requires ptrace-level access to that process, so an unprivileged program cannot survey the address spaces of the rest of the system.

The kernel half of the address space is not listed anywhere a process can read. Its layout is documented per architecture, and randomised too (KASLR), but no unprivileged interface shows where the kernel is.
:::end

## Go deeper

The next lesson, [mappings](lesson:mappings), opens the list of regions inside an address space and shows what each one promises. In Explore, the [process address space](component:process-address-space) and the [page tables](component:page-tables) components show what depends on this. Two planned lessons go further: copy-on-write and fork (how a child shares its parent's pages until one of them writes), and the TLB with PCID (what switching address spaces costs).
