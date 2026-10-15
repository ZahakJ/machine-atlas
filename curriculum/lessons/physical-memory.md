---
{
  "id": "physical-memory",
  "title": "What the kernel has to give out",
  "subsystem": "memory",
  "kind": "core",
  "order": 5,
  "summary": "Frames, nodes and zones, the buddy allocator's free lists and the watermarks that define running low: how the kernel organises the RAM that is left after boot.",
  "prerequisites": ["page-faults"],
  "uses": [
    "derived.env.kind",
    "derived.mem.total_pages",
    "derived.numa.multi",
    "derived.numa.node_count",
    "derived.zones.free_bytes",
    "derived.zones.high_bytes",
    "derived.zones.low_bytes",
    "derived.zones.min_bytes",
    "kernel.page_size_bytes",
    "mem.buddyinfo",
    "mem.hugepages.sizes",
    "mem.memory_block_size_bytes",
    "mem.meminfo.MemFree",
    "mem.meminfo.MemTotal",
    "mem.numa.nodes",
    "mem.sysctl.vm.min_free_kbytes",
    "mem.sysctl.vm.watermark_scale_factor",
    "mem.zones",
    "virt.dmi.product_name",
    "virt.dmi.sys_vendor"
  ],
  "relevance": [
    {"when": "derived.numa.multi", "note": "This machine has more than one NUMA node, so each node has its own zones, free lists and watermarks."}
  ],
  "next": ["anonymous-and-file-pages"],
  "deeper": ["planned:numa-placement", "planned:slab-allocator", "planned:hugetlbfs", "planned:memory-hotplug"],
  "version_notes": [
    {"applies": "Linux ≥ 4.6", "text": "The gaps between the min, low and high watermarks scale with zone size through vm.watermark_scale_factor; before, low and high were fixed at 1.25 and 1.5 times min."},
    {"applies": "Linux ≥ 5.16", "text": "Folios: the kernel increasingly handles memory as folios, groups of one or more contiguous pages described together, instead of individual struct pages."},
    {"applies": "Linux ≥ 6.4", "text": "The largest buddy order constant, MAX_ORDER, was made inclusive, and Linux 6.8 renamed it MAX_PAGE_ORDER; with 4 KiB pages the largest order is still 10 (4 MiB) by default, so /proc/buddyinfo still has eleven columns."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Physical Memory (nodes, zones, pages)", "url": "https://docs.kernel.org/mm/physical_memory.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Memory allocation guide (GFP flags and zones)", "url": "https://docs.kernel.org/core-api/memory-allocation.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/vm/, min_free_kbytes and watermark_scale_factor", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: The /proc filesystem (buddyinfo, zoneinfo, meminfo)", "url": "https://docs.kernel.org/filesystems/proc.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Memory hot(un)plug", "url": "https://docs.kernel.org/admin-guide/mm/memory-hotplug.html", "kind": "kernel-doc"},
    {"title": "Mel Gorman, Understanding the Linux Virtual Memory Manager (2004; describes Linux 2.4 and 2.6, historical)", "url": "https://www.kernel.org/doc/gorman/", "kind": "book"}
  ]
}
---
## Question

The kernel reports {{obs:mem.meminfo.MemTotal}} of memory here. That is not a round number, and it is less than the RAM fitted to the machine, which is sold in round sizes. Where is the rest? And the frames the kernel does have: how does it find a free one in an instant, and why does it file them under nodes and zones instead of keeping one list?

## Why it exists

Every lesson so far has ended with the kernel handing out a frame: at a page fault, for a page-table page, for a cached file page. Frames are the one resource all the others spend, and the allocator that hands them out runs millions of times a second, so it must find a free frame in constant time without scanning anything.

It must also meet requests that are harder than "any page". Some need runs of physically contiguous frames: a huge page, a buffer for a device that does not translate addresses. Some devices can only reach low physical addresses, so their buffers must come from there. On machines with several memory controllers, memory close to the running CPU is faster than memory attached to another socket. And the allocator must notice when free memory is running low early enough to make more before anyone has to wait.

## How it works

### Frames and their descriptors

Physical memory is divided into frames of the base page size, {{obs:kernel.page_size_bytes}} here.

For each frame the kernel keeps a descriptor, `struct page`, of 64 bytes on typical 64-bit configurations: what the frame is used for, its reference count, which LRU list it is on, and where it is mapped. That is about 1.6% of RAM spent describing RAM. Since Linux 5.16 the kernel increasingly works with *folios*, groups of contiguous pages handled as one unit, so that a huge page is one object rather than 512.

### What is gone before MemTotal

Before Linux starts, firmware hands it a map of physical memory: which ranges are usable RAM and which are reserved (the e820 table on PCs, the UEFI memory map, or the device tree on boards). Firmware keeps ranges for itself, ACPI tables and, on machines with integrated graphics, a carve-out for the GPU. The kernel then removes its own image, the `struct page` array, early allocations and any crash-kernel reservation. What remains is handed to the page allocator; those frames are the *managed* pages, and `MemTotal` is their sum. That is why it is never the size printed on the memory modules.

### Nodes and zones

On a machine with several sockets or memory controllers, each group of memory is a NUMA *node* (non-uniform memory access) with its own allocator state, so that allocations can prefer memory local to the CPU that asked. A machine with one memory controller has one node.

Each node is divided into *zones* by physical address, because some devices cannot reach all of memory. On x86-64, `DMA` is the first 16 MiB, for ancient ISA devices; `DMA32` is everything below 4 GiB, for devices that can only issue 32-bit addresses; `Normal` is the rest. On arm64 the DMA zones are sized by what the platform's devices can reach. `Movable` holds only pages whose contents the kernel can relocate, so that whole blocks of memory can later be emptied and removed; `Device` describes memory on devices such as persistent memory or GPUs, which the page allocator does not hand out. An allocation names the highest zone it can use and falls back to lower ones, never the reverse, and each zone keeps a reserve so that allocations that could have used a higher zone cannot exhaust the low zone that devices depend on.

Each zone reports three sizes in pages. *Spanned* is the range from its first to its last frame, holes included. *Present* is the RAM actually inside that range; on PCs, `DMA32` usually spans far more than is present, because the addresses just below 4 GiB are where PCI devices' registers are mapped, and the RAM that would have been there is remapped above 4 GiB. *Managed* is what the page allocator received after the boot reservations.

### The buddy allocator

Free frames are kept on lists by *order*: the order-*n* list holds free blocks of 2^*n* contiguous, aligned frames, from single pages up to order 10 (4 MiB with 4 KiB pages). To allocate an order-*n* block, the allocator takes one from that list; if it is empty, it takes a larger block and splits it in halves, keeping one and putting the other, its *buddy*, on the list below, repeatedly until the size is right. When a block is freed, the allocator checks whether its buddy, whose address differs in exactly one bit, is free too; if so they merge into a block of the next order, and so on upwards. Allocation and freeing are a few list operations whatever the amount of memory.

`/proc/buddyinfo` prints, for each zone, the number of free blocks at each order. Many high-order blocks mean free memory is in large contiguous pieces. Free memory scattered as single pages between used ones is *external fragmentation*: plenty of memory free, but no 2 MiB run for a huge page. To limit it, the allocator groups pages by mobility, so movable user pages and immovable kernel objects do not interleave, and *compaction* migrates movable pages to rebuild large free blocks, in the background (kcompactd) or synchronously when a large allocation needs it.

### Per-CPU lists

Taking the zone's lock for every single-page allocation would make all CPUs contend. Each CPU therefore keeps a small list of free pages per zone, the per-CPU pageset, refilled from and drained to the buddy lists in batches. Pages on these lists are not counted as free: to the zone they are already allocated.

### Watermarks

Each zone has three thresholds in free pages, which are the allocator's definition of "running low". `min` comes from vm.min_free_kbytes, divided among the zones in proportion to their size. `low` and `high` lie above it, separated by the larger of a quarter of `min` and vm.watermark_scale_factor ten-thousandths of the zone. When an allocation finds a zone below `low`, the node's `kswapd` thread wakes to reclaim memory in the background until the zone is back above `high`. An allocation that would take the zone below `min` must reclaim memory itself first and waits: direct reclaim. Allocations that cannot sleep, such as those in interrupt handlers, may dip below `min`; that reserve exists for them. The [reclaim and swap](lesson:reclaim-and-swap) lesson follows what reclaim does.

### Memory blocks and hotplug

The kernel also divides memory into *memory blocks* of a fixed, architecture-dependent size, the unit in which memory can be added to or removed from a running system (memory hotplug), as virtual machines and some servers do. Blocks destined for removal are placed in `ZONE_MOVABLE`, so that no immovable kernel allocation can pin them.

## On this machine

:::evidence What the kernel manages
- mem.meminfo.MemTotal
- derived.mem.total_pages | MemTotal in frames
- mem.meminfo.MemFree
- derived.zones.free_bytes | Free memory summed over zones
- mem.sysctl.vm.min_free_kbytes
- mem.sysctl.vm.watermark_scale_factor
- derived.zones.min_bytes
- derived.zones.low_bytes
- derived.zones.high_bytes
- derived.numa.node_count
- mem.memory_block_size_bytes
:::end

`MemTotal` here is {{derived:derived.mem.total_pages}} of {{obs:kernel.page_size_bytes}}.

:::when mem.zones
MemTotal is, by definition, the sum of the zones' managed pages, not a measurement of the modules, so the `managed` column below should add up to the number of frames above.

:::table mem.zones columns=node,zone,spanned,present,managed,free,min,low,high

Zones that appear with nothing present exist in this kernel's configuration but have no memory on this machine. Across all zones, the min watermarks add up to {{derived:derived.zones.min_bytes}}, which is vm.min_free_kbytes ({{obs:mem.sysctl.vm.min_free_kbytes}}) shared out; kswapd wakes as zones fall below their low marks (together {{derived:derived.zones.low_bytes}}) and sleeps once they are above their high marks (together {{derived:derived.zones.high_bytes}}).
:::unknown
The per-zone figures in `/proc/zoneinfo` could not be read here ({{obs:mem.zones}}), so the zones, their managed pages and their watermarks cannot be shown. The input to the watermarks is still visible: vm.min_free_kbytes is {{obs:mem.sysctl.vm.min_free_kbytes}}, which the kernel shares out among the zones as their min marks, with low and high above them as described.
:::end

:::table mem.buddyinfo

:::when derived.numa.multi
This machine has {{derived:derived.numa.node_count}} NUMA nodes, so every zone above appears once per node, each with its own free lists and watermarks. Each node's memory is attached to a subset of the CPUs, and an allocation is served from the local node first, falling back to a remote one only when the local zones are low:

:::table mem.numa.nodes
:::else
This machine has a single NUMA node, so all its memory is equally close to every CPU and each zone appears once. On a multi-socket server each zone would be listed once per node, with separate free lists and watermarks, and the kernel would prefer memory local to the CPU that asked.
:::unknown
Machine Atlas could not read the NUMA node directory here. A kernel built without NUMA support has none and treats all memory as a single node, which is usual on small boards; the zones above then describe all of memory.
:::end

:::when mem.memory_block_size_bytes
Memory is divided into hotplug blocks of {{obs:mem.memory_block_size_bytes}}.
:::else
:::unknown
Machine Atlas could not read the memory block size here. A kernel built without memory hotplug support has no memory blocks to report; on other systems the file may simply be unreadable. Nothing else in this lesson depends on it.
:::end

Huge page sizes this machine supports for explicit, reserved pools (hugetlbfs), with how many pages each pool holds. Pool pages are taken from the frames above when the pool is sized: they still count in MemTotal, but they leave MemFree and nothing but programs that ask for them can use them.

:::table mem.hugepages.sizes

## See it

:::diagram zone-watermarks

The diagram draws each zone's free pages against its min, low and high marks, to scale.

## Predict

:::predict
Two questions about the tables above. First: if you add the zones' `free` pages and multiply by the page size, will you get exactly `MemFree`? Second: a zone with many free order-10 blocks in buddyinfo: does that mean it has plenty of memory, or something narrower?

- Exactly MemFree; and many order-10 blocks mean plenty of memory.
- Close to MemFree but not always identical; and many order-10 blocks mean little fragmentation, not necessarily plenty of memory.
- Far from MemFree, because MemFree includes reclaimable cache; and order-10 counts are unrelated to free memory.
:::reveal
The second. Both figures come from the same per-zone free-page counters.

:::when derived.zones.free_bytes
Here the zones add up to {{derived:derived.zones.free_bytes}} and MemFree is {{obs:mem.meminfo.MemFree}}.
:::unknown
Here MemFree is {{obs:mem.meminfo.MemFree}}, but the zones' free pages cannot be read ({{derived:derived.zones.free_bytes}}), so the comparison cannot be made on this machine.
:::end

The two can differ slightly because the two files are read at different moments and each CPU batches its counter updates before folding them into the totals. Neither includes pages sitting on per-CPU lists, and neither includes cache: MemFree is memory holding nothing at all.

Each order-*n* block is 2^*n* free pages, so buddyinfo also adds up to the free memory; the columns say how that free memory is shaped. Many order-10 blocks mean the free memory is in large contiguous pieces, which is what huge pages and device buffers need. A zone can show many high-order blocks and still be short of memory overall, if most of it is in use; and a zone with lots of free memory can have no high-order blocks at all, if it is fragmented.
:::end

## Try it

Two read-only commands show the raw sources:

```sh
cat /proc/buddyinfo
grep -E 'Node|pages free|min|low|high|spanned|present|managed|protection' /proc/zoneinfo
```

Run the first a few times: the low-order columns change constantly as pages are allocated and freed, while the high orders change slowly. In the second, each zone's block lists its free pages and watermarks; the `protection` line that follows the watermarks is the per-zone reserve against fallback from higher zones.

## Limits

:::misconception MemTotal is the installed RAM
It is the memory the page allocator manages, after firmware reservations, the kernel image, the `struct page` array and other boot-time reservations. The installed capacity is recorded in the firmware's DMI tables, which only root can read in full (`dmidecode`).
:::end

:::misconception Free memory is the memory available to programs
MemFree counts frames holding nothing at all, and Linux keeps it small on purpose, because an unused frame does no work. Memory available to new programs includes cache that can be dropped; the [page cache](lesson:page-cache) lesson shows the difference.
:::end

:::cannot
Installed module sizes and slots come from DMI tables, which unprivileged programs cannot read in full; nothing here measures the RAM fitted.

`/proc/iomem` shows the physical memory map firmware handed to the kernel, but without privileges every address in it reads as zero, so where the reserved ranges lie cannot be seen.

:::when derived.env.kind is vm-guest
Machine Atlas found evidence that it runs in a virtual machine (system vendor {{obs:virt.dmi.sys_vendor}}, product {{obs:virt.dmi.product_name}}). Here "physical" memory is what the hypervisor presents to the guest: it may be backed by host memory only partly, shared with other guests, or shrunk by a balloon driver, and nothing in the guest can see how.
:::else
If this system is a container, the zones, buddy lists and MemTotal above are the host kernel's, describing all of the host's memory, not what the container may use; that is set by its cgroup limit. If it is a virtual machine that hides itself, "physical" memory is whatever the hypervisor presents.
:::unknown
Machine Atlas could not tell whether it runs in a virtual machine or a container. In either case "physical" memory is not what it seems: in a VM it is what the hypervisor presents; in a container the figures here are the host's, and the container's share is set by its cgroup.
:::end

Zone, buddy and meminfo figures are separate snapshots of counters that change thousands of times a second, read at slightly different moments, so small mismatches between them are expected and mean nothing.
:::end

## Go deeper

Next, [anonymous and file pages](lesson:anonymous-and-file-pages) looks at what the allocated frames hold. In Explore, the [physical frames](component:physical-frames), [NUMA nodes](component:numa-nodes), [zones](component:zones) and [buddy allocator](component:buddy-allocator) components show how these pieces connect. Planned lessons cover NUMA placement, the slab allocator that carves frames into small kernel objects, hugetlbfs pools, and memory hotplug.
