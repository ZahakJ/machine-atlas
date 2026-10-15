---
{
  "id": "cpu-addressing",
  "title": "How many bits is an address, really?",
  "subsystem": "memory",
  "kind": "core",
  "order": 1,
  "summary": "Virtual and physical addresses, pages, and the tree of page tables the MMU walks to translate every memory access.",
  "prerequisites": [],
  "uses": [
    "cpu.address_bits.physical",
    "cpu.address_bits.virtual",
    "cpu.flags.la57",
    "cpu.flags.pdpe1gb",
    "cpu.model_name",
    "derived.cpu.phys_addressable_bytes",
    "derived.env.arch_family",
    "derived.env.kind",
    "derived.paging.index_bits",
    "derived.paging.leaf_table_span_bytes",
    "derived.paging.levels",
    "derived.paging.offset_bits",
    "derived.paging.profile",
    "derived.paging.upper_entry_span_bytes",
    "derived.paging.user_space_bytes",
    "derived.paging.va_bits",
    "kernel.arch",
    "kernel.page_size_bytes",
    "mem.meminfo.MemTotal"
  ],
  "relevance": [
    {"when": "derived.paging.levels", "is": 5, "note": "This machine appears to run 5-level paging, so every translation tree here has an extra level."}
  ],
  "next": ["virtual-address-spaces"],
  "deeper": ["planned:page-table-internals", "planned:tlb-and-pcid"],
  "version_notes": [
    {"applies": "Linux ≥ 4.14 (x86-64)", "text": "5-level paging became available (CONFIG_X86_5LEVEL). Since 4.17 one kernel image chooses 4 or 5 levels at boot from the CPU's LA57 support, the no5lvl boot option forces 4, and /proc/cpuinfo lists la57 only when the kernel enabled 5-level paging."},
    {"applies": "Linux ≥ 5.0 (arm64)", "text": "With 64 KiB pages a kernel can offer 52-bit user addresses, but mmap returns addresses above 2^48 only to programs that ask for them with a hint, so ordinary processes keep a 48-bit layout."}
  ],
  "references": [
    {"title": "Intel 64 and IA-32 Architectures Software Developer's Manual, Volume 3A, Chapter 4: Paging", "url": "https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html", "kind": "spec"},
    {"title": "Linux kernel documentation: Memory management on x86-64", "url": "https://docs.kernel.org/arch/x86/x86_64/mm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: 5-level paging", "url": "https://docs.kernel.org/arch/x86/x86_64/5level-paging.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Memory layout on AArch64 Linux", "url": "https://docs.kernel.org/arch/arm64/memory.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Page tables", "url": "https://docs.kernel.org/mm/page_tables.html", "kind": "kernel-doc"},
    {"title": "Arm Architecture Reference Manual for A-profile architecture, chapter D8: The AArch64 Virtual Memory System Architecture", "url": "https://developer.arm.com/documentation/ddi0487/latest", "kind": "spec"},
    {"title": "The RISC-V Instruction Set Manual, Volume II: Privileged Architecture (Sv39, Sv48, Sv57)", "url": "https://github.com/riscv/riscv-isa-manual/releases", "kind": "spec"}
  ]
}
---
## Question

:::when derived.env.arch_family is x86
A pointer on this {{obs:kernel.arch}} machine is 64 bits wide, yet its processor ({{obs:cpu.model_name}}) reports that it translates only {{obs:cpu.address_bits.virtual}} of a virtual address, and that a physical address has {{obs:cpu.address_bits.physical}}. Where did the other bits of the pointer go, and why are there two different widths at all?
:::else
A pointer on this {{obs:kernel.arch}} machine is 64 bits wide, but the processor does not say how many of those bits it translates: unlike x86 CPUs, it publishes no address widths to unprivileged programs. From the highest address the kernel gave Machine Atlas's probe process, the kernel here appears to translate addresses {{derived:derived.paging.va_bits}} wide. Where did the other bits of the pointer go, and what decides how many are used?
:::end

The two widths are not one quantity measured twice. They describe two different kinds of address, and the gap between them, and between either of them and 64, is the subject of this lesson.

## Why it exists

A program needs addresses that stay valid wherever its data happens to sit in RAM, that no other program can reach, and that outnumber the bytes of RAM, so it can put code at the bottom of its range, the stack near the top, and leave the space between empty. The hardware needs the opposite: names for actual bytes on memory chips and device registers, densely packed.

Translation separates the two. Every address a program uses is a *virtual address*, and the CPU converts it into a *physical address* on every instruction fetch, load and store, billions of times a second. That rules out asking the kernel each time. Translation is done by hardware in each core, the memory management unit (MMU), from tables the kernel prepares in advance: the kernel decides the mapping, the MMU applies it.

## How it works

### Pages and offsets

Translating each byte separately would need one table entry per byte. Instead, memory is translated in *pages*: fixed-size, aligned blocks of addresses. A page here is {{obs:kernel.page_size_bytes}}. Within a page, the low bits of an address pass through translation unchanged; there are log2(page size) of them, {{derived:derived.paging.offset_bits}} here. Only the bits above them, the virtual page number, are translated, into a physical *frame* number: the page-sized slot of RAM that holds the data.

### A tree of tables

One flat table indexed by virtual page number would be enormous: 48-bit addresses with 4 KiB pages need 2^36 entries of 8 bytes, 512 GiB for every process. So the table is a tree. Each table in it occupies exactly one page and holds page size ÷ 8 entries, so each level of the tree is indexed by a fixed slice of the address, {{derived:derived.paging.index_bits}} here. The top slice indexes the root table, whose entry gives the physical address of a table one level down, indexed by the next slice, and so on until the last level, whose entry holds the frame number and the permissions. A part of the address space that is not mapped needs no tables below its empty entry, which is what makes the tree affordable for the sparse way programs use addresses.

The split is a property of the architecture, the page size and the address width the kernel was configured for. The configurations Linux offers:

| format | page size | index bits per level | levels | translated bits |
|---|---|---|---|---|
| x86-64, 4-level | 4 KiB | 9 | 4 | 9+9+9+9+12 = 48 |
| x86-64, 5-level (LA57) | 4 KiB | 9 | 5 | 57 |
| arm64, 4 KiB granule | 4 KiB | 9 | 3 / 4 / 5 | 39 / 48 / 52 (LPA2) |
| arm64, 16 KiB granule | 16 KiB | 11 | 2 / 3 / 4 / 4 | 36 / 47 / 48 / 52 (LPA2) |
| arm64, 64 KiB granule | 64 KiB | 13 | 2 / 3 / 3 | 42 / 48 / 52 (LVA) |
| RISC-V Sv39 / Sv48 / Sv57 | 4 KiB | 9 | 3 / 4 / 5 | 39 / 48 / 57 |

### Canonical addresses

The bits above the translated width are not free for other uses. On x86-64 and RISC-V they must all be copies of the top translated bit, which makes the address *canonical*. With 48 bits, user space runs from `0x0000_0000_0000_0000` to `0x0000_7fff_ffff_ffff` and the kernel's half starts at `0xffff_8000_0000_0000`; everything in between is a hole that can never be mapped. Touching it is refused before any table is read: on x86-64 with a general protection fault rather than a page fault, on RISC-V with a page-fault exception. arm64 states the rule differently: bit 55 of the address selects the tree, the user root register TTBR0_EL1 when it is 0 and the kernel's TTBR1_EL1 when it is 1, so each half has its own tree, and the bits between the translated width and bit 55 must all equal bit 55 or the access takes a translation fault. Linux also turns on *top-byte ignore* for user space, so the top 8 bits of a user pointer are not part of the address at all and may carry a tag.

### The root register and the walker

The tree's root is a physical address held in a register: CR3 on x86-64, TTBR0_EL1 and TTBR1_EL1 on arm64, satp on RISC-V. Switching to another process loads a different root, and with it a different address space. On an access, the MMU's walker reads one entry per level, through the ordinary data caches, without the kernel. If an entry is missing or forbids the access (a write to a read-only page, a user-mode access to a kernel page), the CPU raises a page fault and the kernel takes over, the subject of a later lesson.

### The TLB

A walk costs up to one memory read per level, and each can miss the caches. To avoid paying that on every access, the MMU keeps completed translations in the *translation lookaside buffer*, the TLB. A hit translates with no memory reads at all, and because programs touch the same pages over and over, a TLB of a few thousand entries satisfies almost every access. Its *reach* is the number of entries times the page size: 2 048 entries of 4 KiB pages cover only 8 MiB, so a program working across gigabytes misses often.

### Huge pages

A leaf entry may sit higher in the tree. An entry one level above the last maps, as a single page, the whole range a last-level table would have mapped: {{derived:derived.paging.leaf_table_span_bytes}} with this page size. An entry two levels above the last maps {{derived:derived.paging.upper_entry_span_bytes}}. One TLB entry then covers that whole range and the walk is one or two levels shorter. Whether the largest size may be used depends on the CPU; on x86-64 it is advertised by the pdpe1gb flag.

### Physical address bits

The physical address width bounds the frame number a page-table entry can hold, and so the physical address space the CPU can name: RAM and device registers (memory-mapped I/O) together, sized for the largest machine the design supports. It says nothing about how much RAM is fitted.

## On this machine

:::evidence The address format of this machine
- kernel.arch
- cpu.model_name
- cpu.address_bits.virtual | Virtual address bits the CPU reports
- cpu.address_bits.physical | Physical address bits the CPU reports
- kernel.page_size_bytes
- cpu.flags.la57
- cpu.flags.pdpe1gb
- derived.paging.offset_bits
- derived.paging.index_bits
- derived.paging.levels
- derived.paging.va_bits
- derived.paging.profile
- derived.paging.user_space_bytes
:::end

Machine Atlas infers that this kernel walks {{derived:derived.paging.levels}} levels of page table to translate addresses {{derived:derived.paging.va_bits}} wide. That figure is derived, not read: no unprivileged interface reports the level count as such, so the inspector shows the rule and the observations behind it.

:::when derived.env.arch_family is x86
:::when cpu.flags.la57
This kernel lists the la57 flag. Since Linux 4.17, /proc/cpuinfo shows la57 only when the kernel actually enabled 5-level paging: a kernel running four levels, because it was built without 5-level support or booted with `no5lvl`, clears the flag. The virtual address width the CPU reports, {{obs:cpu.address_bits.virtual}}, is its own capability, unaffected by how the kernel booted; Machine Atlas infers five levels from the flag alone, because the flag records the kernel's choice and the width only the CPU's ability, and the rule in the inspector says so.
:::else
This kernel does not list the la57 flag, so it runs 4-level paging: 9 + 9 + 9 + 9 index bits above the 12-bit offset, 48 bits in all. That alone does not say whether the CPU could walk five levels, because since Linux 4.17 the kernel hides la57 whenever it is not using 5-level paging. The CPU's own capability is its reported {{obs:cpu.address_bits.virtual}} of virtual address: 48 means four levels is all it has, 57 would mean it supports a fifth level this kernel does not use.
:::end
:::else
On {{obs:kernel.arch}}, /proc/cpuinfo has no address sizes line, so the CPU's widths read as {{obs:cpu.address_bits.virtual}}. The width above comes instead from the page size and the highest address the probe process saw: the kernel places a process's stack just below the top of user space, so the narrowest width Linux can be configured with for this page size that still lies above the stack is the one in use. A wider width would also fit the address, but it would put the top of user space, and the stack, far higher.
:::end

User programs on this machine can address {{derived:derived.paging.user_space_bytes}}: the user part of the translated range. It is a ceiling on address space, not on memory, and it is far larger than anything they will touch.

:::when cpu.address_bits.physical
The CPU can name {{derived:derived.cpu.phys_addressable_bytes}} of physical address space, while the kernel manages {{obs:mem.meminfo.MemTotal}} of RAM. The difference is the headroom of the address format, not memory that is missing.
:::unknown
This CPU does not report its physical address width to unprivileged programs, so the contrast with the {{obs:mem.meminfo.MemTotal}} of RAM the kernel manages cannot be drawn from here. On arm64 the width is in the ID_AA64MMFR0_EL1 register, which user space cannot read directly.
:::end

## See it

:::sim translation-walker

The simulator splits an address into per-level indices for a paging profile, starting with the one inferred here, {{derived:derived.paging.profile}}. The index arithmetic is exact; the table contents and frame numbers it shows are invented.

## Predict

:::predict
With pages of {{obs:kernel.page_size_bytes}} and 8-byte entries, how much virtual address space does one complete last-level page table map? And how much does one entry in the level above it map?

- One table maps a single page; one entry above maps a whole table's worth.
- One table maps entries × page size; one entry above maps exactly the same range.
- One table maps entries × page size; one entry above maps entries times that again.
:::reveal
A last-level table holds page size ÷ 8 entries of one page each, so it maps {{derived:derived.paging.leaf_table_span_bytes}}. An entry one level up points at exactly one such table, so it covers the same {{derived:derived.paging.leaf_table_span_bytes}}, which is why a leaf entry at that level makes a huge page of exactly that size. It is a whole table at that level, not one entry, that covers {{derived:derived.paging.upper_entry_span_bytes}}, the next huge page size up.

:::when kernel.page_size_bytes is 4096
With 4 KiB pages these are the 2 MiB and 1 GiB huge pages familiar from x86-64.
:::else
Pages larger than 4 KiB make both spans grow quickly: 16 KiB pages give 32 MiB and 64 GiB, 64 KiB pages give 512 MiB and 4 TiB. Sizes that large are hard to use, so arm64 also offers a contiguous bit that lets a run of adjacent entries share one TLB entry.
:::end
:::end

## Try it

Three read-only commands show the inputs to everything above:

```sh
grep -m1 'address sizes' /proc/cpuinfo
getconf PAGE_SIZE
python3 -c 'import ctypes; b = ctypes.create_string_buffer(8); print(hex(ctypes.addressof(b)))'
```

The first prints the CPU's two widths on x86 and nothing on arm64 or RISC-V. The second should print {{obs:kernel.page_size_bytes|raw}}. The third prints the address of a small buffer in Python's heap; expect it below {{derived:derived.paging.user_space_bytes|hex}}, the top of user space. Decompose it in the simulator, then run the command again: the address changes, for reasons the next lesson explains.

## Limits

:::misconception 64-bit means 2^64 bytes are addressable
A 64-bit register can hold a 64-bit pointer, but only {{derived:derived.paging.va_bits}} of it are translated here; the rest must follow the canonical rule. No current processor translates all 64 bits, because the page tables, the TLB and the walker would all grow for address space nobody needs.
:::end

:::misconception Physical address bits tell you how much RAM is fitted
They say how large a physical address can be, a limit of the processor design. Installed RAM is a fraction of it and is reported elsewhere; MemTotal is what the kernel manages after firmware and the kernel itself have taken their share.
:::end

:::misconception The kernel walks the page tables on every access
The kernel writes the page tables and the MMU reads them. Most accesses do not walk at all, because the TLB already holds the translation. The kernel is involved only when a walk fails and the CPU raises a page fault.
:::end

:::cannot
An unprivileged program cannot read the page tables, the root register or the TLB. Everything above about this machine's translation is inferred from what is visible: the architecture, the page size, CPU flags and, where the CPU publishes nothing, the addresses the probe process was given.

On x86-64 the la57 flag in /proc/cpuinfo reflects what the kernel enabled and the address sizes line what the CPU can do; neither is read from the page tables themselves, which is why the level count is a derived fact with its rule shown rather than an observation. Elsewhere it rests on where the kernel put the stack.

:::when derived.env.kind is vm-guest
Machine Atlas found evidence that it runs in a virtual machine. There, the "physical" addresses the guest kernel manages are themselves translated again, by a second stage of page tables the hypervisor controls, and nothing inside the guest can see that stage or what it costs.
:::else
If this system runs inside a virtual machine that hides itself, the "physical" addresses the kernel manages are themselves translated again by a second stage of page tables the hypervisor controls, and nothing inside the guest can see that stage.
:::end
:::end

## Go deeper

Next, [virtual address spaces](lesson:virtual-address-spaces) follows one page-table tree per process and what the kernel puts in it. In Explore, the [MMU](component:cpu-mmu), the [TLB](component:tlb) and the [page tables](component:page-tables) show how these pieces depend on each other. Two planned lessons go further: page-table internals (the bits of an entry and how Linux's generic levels map onto each architecture) and the TLB with PCID (what invalidating translations on other CPUs costs).
