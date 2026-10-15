# References

Every primary source the lessons cite, grouped by topic and de-duplicated by URL, followed by the kernel-version-dependent behaviours the lessons call out. Both lists are generated from the front matter of `curriculum/lessons/*.md` (`references` and `version_notes`) by `go run ./scripts/docgen`; `atlas lesson ID --json` prints the same fields for one lesson. When a lesson adds or changes a reference, regenerate this file rather than editing it by hand.

A reference is filed under the topic of the first lesson on the journey that cites it, with that lesson's title for it; "Cited by" lists every lesson that does. Kinds are those the lint accepts: kernel documentation, man page, specification, kernel source, LWN, book, paper, other.

## Addresses and paging hardware

| Reference | Kind | Cited by |
|---|---|---|
| [Intel 64 and IA-32 Architectures Software Developer's Manual, Volume 3A, Chapter 4: Paging](https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html) | specification | `cpu-addressing`, `cpu-topology`, `cpu-frequency`, `page-faults` |
| [Linux kernel documentation: Memory management on x86-64](https://docs.kernel.org/arch/x86/x86_64/mm.html) | kernel documentation | `cpu-addressing`, `virtual-address-spaces` |
| [Linux kernel documentation: 5-level paging](https://docs.kernel.org/arch/x86/x86_64/5level-paging.html) | kernel documentation | `cpu-addressing`, `virtual-address-spaces` |
| [Linux kernel documentation: Memory layout on AArch64 Linux](https://docs.kernel.org/arch/arm64/memory.html) | kernel documentation | `cpu-addressing` |
| [Linux kernel documentation: Page tables](https://docs.kernel.org/mm/page_tables.html) | kernel documentation | `cpu-addressing`, `page-faults` |
| [Arm Architecture Reference Manual for A-profile architecture, chapter D8: The AArch64 Virtual Memory System Architecture](https://developer.arm.com/documentation/ddi0487/latest) | specification | `cpu-addressing`, `cpu-topology` |
| [The RISC-V Instruction Set Manual, Volume II: Privileged Architecture (Sv39, Sv48, Sv57)](https://github.com/riscv/riscv-isa-manual/releases) | specification | `cpu-addressing` |

## Address spaces and mappings

| Reference | Kind | Cited by |
|---|---|---|
| [proc_pid_maps(5): /proc/pid/maps](https://man7.org/linux/man-pages/man5/proc_pid_maps.5.html) | man page | `virtual-address-spaces`, `mappings` |
| [Linux kernel documentation: /proc/sys/kernel/, randomize_va_space](https://docs.kernel.org/admin-guide/sysctl/kernel.html) | kernel documentation | `virtual-address-spaces` |
| [Linux kernel documentation: /proc/sys/vm/, max_map_count and mmap_rnd_bits](https://docs.kernel.org/admin-guide/sysctl/vm.html) | kernel documentation | `virtual-address-spaces`, `physical-memory`, `writeback-and-durability`, `page-cache`, `reclaim-and-swap`, `memory-pressure-and-oom` |
| [Linux kernel documentation: Page Table Isolation (PTI)](https://docs.kernel.org/arch/x86/pti.html) | kernel documentation | `virtual-address-spaces` |
| [Linux kernel documentation: Process addresses](https://docs.kernel.org/mm/process_addrs.html) | kernel documentation | `virtual-address-spaces`, `mappings` |
| [mmap(2)](https://man7.org/linux/man-pages/man2/mmap.2.html) | man page | `virtual-address-spaces`, `mappings`, `page-faults` |
| [Go 1.26 release notes (heap base randomisation)](https://go.dev/doc/go1.26) | other | `virtual-address-spaces` |
| [proc_pid_smaps(5): /proc/pid/smaps](https://man7.org/linux/man-pages/man5/proc_pid_smaps.5.html) | man page | `mappings` |
| [Linux kernel documentation: The /proc filesystem (maps, smaps)](https://docs.kernel.org/filesystems/proc.html) | kernel documentation | `mappings`, `run-queues-and-load`, `physical-memory`, `memory-pressure-and-oom` |
| [Linux kernel documentation: Overcommit accounting](https://docs.kernel.org/mm/overcommit-accounting.html) | kernel documentation | `mappings`, `memory-pressure-and-oom` |
| [mallopt(3): M_MMAP_THRESHOLD](https://man7.org/linux/man-pages/man3/mallopt.3.html) | man page | `mappings` |
| [LWN: Introducing maple trees (2021)](https://lwn.net/Articles/845507/) | LWN | `mappings` |

## Page faults and huge pages

| Reference | Kind | Cited by |
|---|---|---|
| [getrusage(2): ru_minflt and ru_majflt](https://man7.org/linux/man-pages/man2/getrusage.2.html) | man page | `page-faults` |
| [madvise(2)](https://man7.org/linux/man-pages/man2/madvise.2.html) | man page | `page-faults`, `transparent-huge-pages` |
| [Linux kernel documentation: Examining process page tables (pagemap)](https://docs.kernel.org/admin-guide/mm/pagemap.html) | kernel documentation | `page-faults` |
| [Linux kernel documentation: Transparent Hugepage Support](https://docs.kernel.org/admin-guide/mm/transhuge.html) | kernel documentation | `page-faults`, `transparent-huge-pages` |
| [Linux kernel source: mm/memory.c (handle_mm_fault, do_anonymous_page, do_fault_around)](https://github.com/torvalds/linux/blob/master/mm/memory.c) | kernel source | `page-faults` |
| [Linux kernel documentation: Transparent Hugepage Support (design: splitting and refcounting)](https://docs.kernel.org/mm/transhuge.html) | kernel documentation | `transparent-huge-pages` |
| [Linux 6.8 documentation: Transparent Hugepage Support, introducing multi-size THP](https://docs.kernel.org/6.8/admin-guide/mm/transhuge.html) | kernel documentation | `transparent-huge-pages` |
| [Linux kernel documentation: Physical Memory](https://docs.kernel.org/mm/physical_memory.html) | kernel documentation | `transparent-huge-pages`, `physical-memory` |

## Physical memory

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Memory allocation guide (GFP flags and zones)](https://docs.kernel.org/core-api/memory-allocation.html) | kernel documentation | `physical-memory` |
| [Linux kernel documentation: Memory hot(un)plug](https://docs.kernel.org/admin-guide/mm/memory-hotplug.html) | kernel documentation | `physical-memory` |
| [Mel Gorman, Understanding the Linux Virtual Memory Manager (2004; describes Linux 2.4 and 2.6, historical)](https://www.kernel.org/doc/gorman/) | book | `physical-memory` |

## Memory accounting and the page cache

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: The /proc filesystem, /proc/meminfo](https://docs.kernel.org/filesystems/proc.html#meminfo) | kernel documentation | `anonymous-and-file-pages`, `page-cache` |
| [proc_meminfo(5)](https://man7.org/linux/man-pages/man5/proc_meminfo.5.html) | man page | `anonymous-and-file-pages` |
| [proc_pid_status(5): RssAnon, RssFile, RssShmem](https://man7.org/linux/man-pages/man5/proc_pid_status.5.html) | man page | `anonymous-and-file-pages` |
| [Linux kernel documentation: Concepts overview (page cache, anonymous memory)](https://docs.kernel.org/admin-guide/mm/concepts.html) | kernel documentation | `anonymous-and-file-pages` |
| [memfd_create(2)](https://man7.org/linux/man-pages/man2/memfd_create.2.html) | man page | `anonymous-and-file-pages` |
| [Kernel source: fs/proc/meminfo.c (how each meminfo line is computed)](https://github.com/torvalds/linux/blob/master/fs/proc/meminfo.c) | kernel source | `anonymous-and-file-pages` |
| [Linux kernel documentation: Concepts overview, Page cache](https://docs.kernel.org/admin-guide/mm/concepts.html#page-cache) | kernel documentation | `page-cache` |
| [Commit 34e431b0ae39: /proc/meminfo: provide estimated available memory](https://github.com/torvalds/linux/commit/34e431b0ae398fc54ea69ff85ec700722c9da773) | kernel source | `page-cache` |
| [Kernel source: si_mem_available() in mm/show_mem.c](https://github.com/torvalds/linux/blob/master/mm/show_mem.c) | kernel source | `page-cache` |
| [mincore(2)](https://man7.org/linux/man-pages/man2/mincore.2.html) | man page | `page-cache` |
| [cachestat(2)](https://man7.org/linux/man-pages/man2/cachestat.2.html) | man page | `page-cache` |

## Reclaim, swap, pressure and OOM

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Concepts overview, Reclaim](https://docs.kernel.org/admin-guide/mm/concepts.html#reclaim) | kernel documentation | `reclaim-and-swap` |
| [Linux kernel documentation: Multi-Gen LRU (admin guide)](https://docs.kernel.org/admin-guide/mm/multigen_lru.html) | kernel documentation | `reclaim-and-swap` |
| [Linux kernel documentation: Multi-Gen LRU (design)](https://docs.kernel.org/mm/multigen_lru.html) | kernel documentation | `reclaim-and-swap` |
| [Linux kernel documentation: zswap](https://docs.kernel.org/admin-guide/mm/zswap.html) | kernel documentation | `reclaim-and-swap` |
| [Linux kernel documentation: zram, compressed RAM-based block devices](https://docs.kernel.org/admin-guide/blockdev/zram.html) | kernel documentation | `reclaim-and-swap` |
| [swapon(2): swap priorities](https://man7.org/linux/man-pages/man2/swapon.2.html) | man page | `reclaim-and-swap` |
| [swapon(8)](https://man7.org/linux/man-pages/man8/swapon.8.html) | man page | `reclaim-and-swap` |
| [Kernel source: get_scan_count() in mm/vmscan.c (how swappiness balances anon and file)](https://github.com/torvalds/linux/blob/master/mm/vmscan.c) | kernel source | `reclaim-and-swap` |
| [LWN: Better active/inactive list balancing (2012)](https://lwn.net/Articles/495543/) | LWN | `reclaim-and-swap` |
| [Linux kernel documentation: Control Group v2, Memory](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory) | kernel documentation | `memory-pressure-and-oom` |
| [Linux kernel documentation: Concepts overview, OOM killer](https://docs.kernel.org/admin-guide/mm/concepts.html#oom-killer) | kernel documentation | `memory-pressure-and-oom` |
| [proc_pid_oom_score(5)](https://man7.org/linux/man-pages/man5/proc_pid_oom_score.5.html) | man page | `memory-pressure-and-oom` |
| [systemd-oomd.service(8)](https://man7.org/linux/man-pages/man8/systemd-oomd.service.8.html) | man page | `memory-pressure-and-oom` |
| [Kernel source: oom_badness() in mm/oom_kill.c](https://github.com/torvalds/linux/blob/master/mm/oom_kill.c) | kernel source | `memory-pressure-and-oom` |
| [LWN: Toward more predictable and reliable out-of-memory handling (2015)](https://lwn.net/Articles/668126/) | LWN | `memory-pressure-and-oom` |

## CPU topology

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: How CPU topology info is exported via sysfs](https://docs.kernel.org/admin-guide/cputopology.html) | kernel documentation | `cpu-topology` |
| [Linux kernel documentation: x86 Topology](https://docs.kernel.org/arch/x86/topology.html) | kernel documentation | `cpu-topology` |
| [Linux kernel documentation: Scheduler Domains](https://docs.kernel.org/scheduler/sched-domains.html) | kernel documentation | `cpu-topology` |
| [Linux kernel documentation: Capacity Aware Scheduling](https://docs.kernel.org/scheduler/sched-capacity.html) | kernel documentation | `cpu-topology` |
| [Linux kernel documentation: L1TF, L1 Terminal Fault (SMT control)](https://docs.kernel.org/admin-guide/hw-vuln/l1tf.html) | kernel documentation | `cpu-topology` |
| [Linux kernel documentation: CPU hotplug in the Kernel (possible, present and online)](https://docs.kernel.org/core-api/cpu_hotplug.html) | kernel documentation | `cpu-topology` |
| [lscpu(1)](https://man7.org/linux/man-pages/man1/lscpu.1.html) | man page | `cpu-topology` |

## CPU frequency scaling

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: CPU Performance Scaling (cpufreq)](https://docs.kernel.org/admin-guide/pm/cpufreq.html) | kernel documentation | `cpu-frequency` |
| [Linux kernel documentation: intel_pstate CPU Performance Scaling Driver](https://docs.kernel.org/admin-guide/pm/intel_pstate.html) | kernel documentation | `cpu-frequency` |
| [Linux kernel documentation: amd-pstate CPU Performance Scaling Driver](https://docs.kernel.org/admin-guide/pm/amd-pstate.html) | kernel documentation | `cpu-frequency` |
| [Linux kernel documentation: Schedutil](https://docs.kernel.org/scheduler/schedutil.html) | kernel documentation | `cpu-frequency` |
| [Linux kernel documentation: CPU Idle Time Management](https://docs.kernel.org/admin-guide/pm/cpuidle.html) | kernel documentation | `cpu-frequency` |
| [Linux kernel documentation: General description of the CPUFreq core and drivers](https://docs.kernel.org/cpu-freq/cpu-drivers.html) | kernel documentation | `cpu-frequency` |
| [cpupower-frequency-info(1)](https://man7.org/linux/man-pages/man1/cpupower-frequency-info.1.html) | man page | `cpu-frequency` |

## Run queues and load

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Scheduler Statistics (/proc/schedstat)](https://docs.kernel.org/scheduler/sched-stats.html) | kernel documentation | `run-queues-and-load` |
| [Kernel source: kernel/sched/loadavg.c (calc_global_load)](https://github.com/torvalds/linux/blob/master/kernel/sched/loadavg.c) | kernel source | `run-queues-and-load` |
| [proc_loadavg(5)](https://man7.org/linux/man-pages/man5/proc_loadavg.5.html) | man page | `run-queues-and-load` |
| [proc_stat(5)](https://man7.org/linux/man-pages/man5/proc_stat.5.html) | man page | `run-queues-and-load` |
| [vmstat(8)](https://man7.org/linux/man-pages/man8/vmstat.8.html) | man page | `run-queues-and-load` |
| [Brendan Gregg, Linux Load Averages: Solving the Mystery (2017)](https://www.brendangregg.com/blog/2017-08-08/linux-load-averages.html) | other | `run-queues-and-load` |

## Fair scheduling

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: EEVDF Scheduler](https://docs.kernel.org/scheduler/sched-eevdf.html) | kernel documentation | `fair-scheduling` |
| [Linux kernel documentation: CFS Scheduler](https://docs.kernel.org/scheduler/sched-design-CFS.html) | kernel documentation | `fair-scheduling` |
| [Linux kernel documentation: Extensible Scheduler Class (sched_ext)](https://docs.kernel.org/scheduler/sched-ext.html) | kernel documentation | `fair-scheduling` |
| [Linux kernel documentation: Real-Time group scheduling (sched_rt_runtime_us)](https://docs.kernel.org/scheduler/sched-rt-group.html) | kernel documentation | `fair-scheduling` |
| [sched(7): scheduling policies, nice values and autogroup](https://man7.org/linux/man-pages/man7/sched.7.html) | man page | `fair-scheduling` |
| [sched_setattr(2)](https://man7.org/linux/man-pages/man2/sched_setattr.2.html) | man page | `fair-scheduling` |
| [Kernel source: kernel/sched/fair.c (pick_eevdf, update_curr, place_entity)](https://github.com/torvalds/linux/blob/master/kernel/sched/fair.c) | kernel source | `fair-scheduling` |
| [Kernel source: kernel/sched/core.c (sched_prio_to_weight)](https://github.com/torvalds/linux/blob/master/kernel/sched/core.c) | kernel source | `fair-scheduling` |
| [LWN: An EEVDF CPU scheduler for Linux (2023)](https://lwn.net/Articles/925371/) | LWN | `fair-scheduling` |

## CPU limits and isolation

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Control Group v2 (cpu and cpuset controllers)](https://docs.kernel.org/admin-guide/cgroup-v2.html) | kernel documentation | `cpu-limits-and-isolation` |
| [Linux kernel documentation: CFS Bandwidth Control](https://docs.kernel.org/scheduler/sched-bwc.html) | kernel documentation | `cpu-limits-and-isolation` |
| [Linux kernel documentation: CPUSETS (cgroup v1)](https://docs.kernel.org/admin-guide/cgroup-v1/cpusets.html) | kernel documentation | `cpu-limits-and-isolation` |
| [Linux kernel documentation: NO_HZ, Reducing Scheduling-Clock Ticks](https://docs.kernel.org/timers/no_hz.html) | kernel documentation | `cpu-limits-and-isolation` |
| [Linux kernel documentation: The kernel's command-line parameters (isolcpus, nohz_full)](https://docs.kernel.org/admin-guide/kernel-parameters.html) | kernel documentation | `cpu-limits-and-isolation` |
| [sched_setaffinity(2)](https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html) | man page | `cpu-limits-and-isolation` |
| [taskset(1)](https://man7.org/linux/man-pages/man1/taskset.1.html) | man page | `cpu-limits-and-isolation` |
| [cpuset(7)](https://man7.org/linux/man-pages/man7/cpuset.7.html) | man page | `cpu-limits-and-isolation` |
| [systemd.resource-control(5): CPUWeight=, CPUQuota=, AllowedCPUs=](https://man7.org/linux/man-pages/man5/systemd.resource-control.5.html) | man page | `cpu-limits-and-isolation` |

## Block devices and request queues

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Multi-Queue Block IO Queueing Mechanism (blk-mq)](https://docs.kernel.org/block/blk-mq.html) | kernel documentation | `block-devices` |
| [Linux kernel documentation: Queue sysfs files](https://docs.kernel.org/block/queue-sysfs.html) | kernel documentation | `block-devices`, `writeback-and-durability` |
| [Linux kernel documentation: Block layer biovecs and bios](https://docs.kernel.org/block/biovecs.html) | kernel documentation | `block-devices` |
| [Linux kernel documentation: Device Mapper](https://docs.kernel.org/admin-guide/device-mapper/index.html) | kernel documentation | `block-devices` |
| [NVM Express Base Specification (queues and the submission/completion model)](https://nvmexpress.org/specifications/) | specification | `block-devices` |
| [Kernel source: block/blk-mq.c](https://github.com/torvalds/linux/blob/master/block/blk-mq.c) | kernel source | `block-devices` |
| [Kernel source: block/blk-merge.c (splitting and merging bios)](https://github.com/torvalds/linux/blob/master/block/blk-merge.c) | kernel source | `block-devices` |
| [LWN: The multiqueue block layer (2013)](https://lwn.net/Articles/552904/) | LWN | `block-devices` |
| [Bjørling, Axboe, Nellans, Bonnet: Linux Block IO: Introducing Multi-queue SSD Access on Multi-core Systems (SYSTOR 2013)](https://kernel.dk/blk-mq.pdf) | paper | `block-devices` |
| [lsblk(8)](https://man7.org/linux/man-pages/man8/lsblk.8.html) | man page | `block-devices` |

## I/O schedulers

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Switching Scheduler](https://docs.kernel.org/block/switching-sched.html) | kernel documentation | `io-schedulers` |
| [Linux kernel documentation: Deadline IO scheduler tunables](https://docs.kernel.org/block/deadline-iosched.html) | kernel documentation | `io-schedulers` |
| [Linux kernel documentation: BFQ (Budget Fair Queueing)](https://docs.kernel.org/block/bfq-iosched.html) | kernel documentation | `io-schedulers` |
| [Linux kernel documentation: Kyber I/O scheduler tunables](https://docs.kernel.org/block/kyber-iosched.html) | kernel documentation | `io-schedulers` |
| [Linux kernel documentation: Control Group v2, IO controller (io.weight, io.max, io.latency)](https://docs.kernel.org/admin-guide/cgroup-v2.html#io) | kernel documentation | `io-schedulers` |
| [Kernel source: block/elevator.c (elevator_get_default)](https://github.com/torvalds/linux/blob/master/block/elevator.c) | kernel source | `io-schedulers` |
| [Kernel source: block/mq-deadline.c](https://github.com/torvalds/linux/blob/master/block/mq-deadline.c) | kernel source | `io-schedulers` |
| [LWN: Two new block I/O schedulers for 4.12 (2017)](https://lwn.net/Articles/720675/) | LWN | `io-schedulers` |
| [LWN: A block layer introduction part 2: the request layer (2017)](https://lwn.net/Articles/738449/) | LWN | `io-schedulers` |

## I/O statistics and pressure

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Block layer statistics in /sys/block/&lt;dev&gt;/stat](https://docs.kernel.org/block/stat.html) | kernel documentation | `io-statistics` |
| [Linux kernel documentation: I/O statistics fields (/proc/diskstats)](https://docs.kernel.org/admin-guide/iostats.html) | kernel documentation | `io-statistics` |
| [Linux kernel documentation: PSI, Pressure Stall Information](https://docs.kernel.org/accounting/psi.html) | kernel documentation | `io-statistics`, `run-queues-and-load`, `reclaim-and-swap`, `memory-pressure-and-oom` |
| [iostat(1) (sysstat)](https://man7.org/linux/man-pages/man1/iostat.1.html) | man page | `io-statistics` |
| [proc_pid_io(5): per-process I/O counters](https://man7.org/linux/man-pages/man5/proc_pid_io.5.html) | man page | `io-statistics` |
| [Kernel source: block/genhd.c (diskstats_show) and block/blk-core.c (update_io_ticks)](https://github.com/torvalds/linux/blob/master/block/genhd.c) | kernel source | `io-statistics` |
| [Little, J. D. C.: A Proof for the Queuing Formula L = λW (Operations Research, 1961)](https://doi.org/10.1287/opre.9.3.383) | paper | `io-statistics` |
| [LWN: Tracking pressure-stall information (2018)](https://lwn.net/Articles/759781/) | LWN | `io-statistics` |

## Filesystems, the VFS and readahead

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Overview of the Linux Virtual File System](https://docs.kernel.org/filesystems/vfs.html) | kernel documentation | `filesystems-and-the-vfs`, `page-cache` |
| [Linux kernel documentation: Pathname lookup](https://docs.kernel.org/filesystems/path-lookup.html) | kernel documentation | `filesystems-and-the-vfs` |
| [Linux kernel documentation: ext4 data structures and algorithms (extents, delayed allocation)](https://docs.kernel.org/filesystems/ext4/index.html) | kernel documentation | `filesystems-and-the-vfs` |
| [Linux kernel documentation: Overlay Filesystem](https://docs.kernel.org/filesystems/overlayfs.html) | kernel documentation | `filesystems-and-the-vfs` |
| [Linux kernel documentation: Tmpfs](https://docs.kernel.org/filesystems/tmpfs.html) | kernel documentation | `filesystems-and-the-vfs`, `anonymous-and-file-pages` |
| [Kernel source: mm/readahead.c (the readahead window)](https://github.com/torvalds/linux/blob/master/mm/readahead.c) | kernel source | `filesystems-and-the-vfs` |
| [mount(8): filesystem-independent mount options](https://man7.org/linux/man-pages/man8/mount.8.html) | man page | `filesystems-and-the-vfs` |
| [readahead(2)](https://man7.org/linux/man-pages/man2/readahead.2.html) | man page | `filesystems-and-the-vfs` |
| [posix_fadvise(2)](https://man7.org/linux/man-pages/man2/posix_fadvise.2.html) | man page | `filesystems-and-the-vfs`, `page-cache` |
| [LWN: Improving readahead (2005, on-demand readahead's design)](https://lwn.net/Articles/155510/) | LWN | `filesystems-and-the-vfs` |
| [LWN: Introducing lazytime (2014)](https://lwn.net/Articles/621046/) | LWN | `filesystems-and-the-vfs` |

## Writeback and durability

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Explicit volatile write back cache control (REQ_PREFLUSH, REQ_FUA)](https://docs.kernel.org/block/writeback_cache_control.html) | kernel documentation | `writeback-and-durability` |
| [Linux kernel documentation: ext4 journal (data=ordered, writeback, journal)](https://docs.kernel.org/filesystems/ext4/journal.html) | kernel documentation | `writeback-and-durability` |
| [fsync(2)](https://man7.org/linux/man-pages/man2/fsync.2.html) | man page | `writeback-and-durability` |
| [open(2): O_SYNC, O_DSYNC and O_DIRECT](https://man7.org/linux/man-pages/man2/open.2.html) | man page | `writeback-and-durability` |
| [sync_file_range(2)](https://man7.org/linux/man-pages/man2/sync_file_range.2.html) | man page | `writeback-and-durability` |
| [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html) | man page | `writeback-and-durability` |
| [Kernel source: mm/page-writeback.c (balance_dirty_pages)](https://github.com/torvalds/linux/blob/master/mm/page-writeback.c) | kernel source | `writeback-and-durability` |
| [LWN: No-I/O dirty throttling (2011)](https://lwn.net/Articles/456904/) | LWN | `writeback-and-durability` |
| [LWN: Toward less-annoying background writeback (2016)](https://lwn.net/Articles/682582/) | LWN | `writeback-and-durability` |
| [LWN: PostgreSQL's fsync() surprise (2018)](https://lwn.net/Articles/752063/) | LWN | `writeback-and-durability` |
| [Pillai et al.: All File Systems Are Not Created Equal: On the Complexity of Crafting Crash-Consistent Applications (OSDI 2014)](https://www.usenix.org/conference/osdi14/technical-sessions/presentation/pillai) | paper | `writeback-and-durability` |

## Network interfaces and addresses

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: Operational States (operstate, carrier, dormant)](https://docs.kernel.org/networking/operstates.html) | kernel documentation | `network-interfaces` |
| [Linux kernel ABI documentation: /sys/class/net](https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net) | kernel documentation | `network-interfaces` |
| [Linux kernel documentation: IP sysctl (disable_ipv6, use_tempaddr)](https://docs.kernel.org/networking/ip-sysctl.html) | kernel documentation | `network-interfaces`, `packet-path`, `sockets-and-connections`, `tcp-throughput` |
| [netdevice(7)](https://man7.org/linux/man-pages/man7/netdevice.7.html) | man page | `network-interfaces` |
| [ip-address(8)](https://man7.org/linux/man-pages/man8/ip-address.8.html) | man page | `network-interfaces` |
| [ip-link(8)](https://man7.org/linux/man-pages/man8/ip-link.8.html) | man page | `network-interfaces` |
| [RFC 1918: Address Allocation for Private Internets](https://www.rfc-editor.org/rfc/rfc1918) | specification | `network-interfaces` |
| [RFC 4291: IP Version 6 Addressing Architecture](https://www.rfc-editor.org/rfc/rfc4291) | specification | `network-interfaces` |
| [RFC 4193: Unique Local IPv6 Unicast Addresses](https://www.rfc-editor.org/rfc/rfc4193) | specification | `network-interfaces` |
| [RFC 8981: Temporary Address Extensions for Stateless Address Autoconfiguration in IPv6](https://www.rfc-editor.org/rfc/rfc8981) | specification | `network-interfaces` |
| [RFC 6598: IANA-Reserved IPv4 Prefix for Shared Address Space (100.64.0.0/10)](https://www.rfc-editor.org/rfc/rfc6598) | specification | `network-interfaces` |
| [systemd: Predictable Network Interface Names](https://systemd.io/PREDICTABLE_INTERFACE_NAMES/) | other | `network-interfaces` |

## The path of a packet

| Reference | Kind | Cited by |
|---|---|---|
| [Linux kernel documentation: NAPI](https://docs.kernel.org/networking/napi.html) | kernel documentation | `packet-path` |
| [Linux kernel documentation: Scaling in the Linux Networking Stack (RSS, RPS, RFS, XPS)](https://docs.kernel.org/networking/scaling.html) | kernel documentation | `packet-path`, `packet-queues` |
| [Linux kernel documentation: Segmentation Offloads (TSO, GSO, GRO)](https://docs.kernel.org/networking/segmentation-offloads.html) | kernel documentation | `packet-path` |
| [Linux kernel documentation: /proc/sys/net/core (netdev_max_backlog, netdev_budget)](https://docs.kernel.org/admin-guide/sysctl/net.html) | kernel documentation | `packet-path`, `packet-queues` |
| [ip-route(8)](https://man7.org/linux/man-pages/man8/ip-route.8.html) | man page | `packet-path` |
| [ip(7)](https://man7.org/linux/man-pages/man7/ip.7.html) | man page | `packet-path` |
| [Kernel source: net/core/dev.c (__dev_queue_xmit, net_rx_action, enqueue_to_backlog)](https://github.com/torvalds/linux/blob/master/net/core/dev.c) | kernel source | `packet-path` |
| [Kernel source: net/ipv4/fib_trie.c (the IPv4 routing table)](https://github.com/torvalds/linux/blob/master/net/ipv4/fib_trie.c) | kernel source | `packet-path` |
| [LWN: Generic receive offload (2008)](https://lwn.net/Articles/358910/) | LWN | `packet-path` |
| [LWN: The IPv4 routing cache removal (2012)](https://lwn.net/Articles/507651/) | LWN | `packet-path` |

## Sockets and TCP connections

| Reference | Kind | Cited by |
|---|---|---|
| [RFC 9293: Transmission Control Protocol (TCP), section 3.3.2 State Machine Overview](https://www.rfc-editor.org/rfc/rfc9293) | specification | `sockets-and-connections`, `tcp-throughput` |
| [tcp(7)](https://man7.org/linux/man-pages/man7/tcp.7.html) | man page | `sockets-and-connections`, `tcp-throughput` |
| [socket(7)](https://man7.org/linux/man-pages/man7/socket.7.html) | man page | `sockets-and-connections`, `tcp-throughput` |
| [listen(2)](https://man7.org/linux/man-pages/man2/listen.2.html) | man page | `sockets-and-connections` |
| [ss(8)](https://man7.org/linux/man-pages/man8/ss.8.html) | man page | `sockets-and-connections` |
| [Linux kernel documentation: The /proc filesystem, /proc/net (tcp, sockstat, snmp)](https://docs.kernel.org/filesystems/proc.html#networking-info-in-proc-net) | kernel documentation | `sockets-and-connections` |
| [Kernel source: include/net/tcp_states.h](https://github.com/torvalds/linux/blob/master/include/net/tcp_states.h) | kernel source | `sockets-and-connections` |
| [Kernel source: net/ipv4/tcp_minisocks.c (TIME_WAIT)](https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_minisocks.c) | kernel source | `sockets-and-connections` |
| [LWN: SYN cookies ported to IPv6 (2013), and how cookies work](https://lwn.net/Articles/548075/) | LWN | `sockets-and-connections` |

## TCP throughput and congestion control

| Reference | Kind | Cited by |
|---|---|---|
| [RFC 5681: TCP Congestion Control](https://www.rfc-editor.org/rfc/rfc5681) | specification | `tcp-throughput` |
| [RFC 9438: CUBIC for Fast and Long-Distance Networks](https://www.rfc-editor.org/rfc/rfc9438) | specification | `tcp-throughput` |
| [RFC 7323: TCP Extensions for High Performance (window scaling)](https://www.rfc-editor.org/rfc/rfc7323) | specification | `tcp-throughput` |
| [RFC 6928: Increasing TCP's Initial Window](https://www.rfc-editor.org/rfc/rfc6928) | specification | `tcp-throughput` |
| [RFC 8985: The RACK-TLP Loss Detection Algorithm for TCP](https://www.rfc-editor.org/rfc/rfc8985) | specification | `tcp-throughput` |
| [Cardwell, Cheng, Gunn, Hassas Yeganeh, Jacobson: BBR: Congestion-Based Congestion Control, ACM Queue 14(5), 2016](https://queue.acm.org/detail.cfm?id=3022184) | paper | `tcp-throughput` |
| [Kernel source: net/ipv4/tcp_cubic.c and tcp_bbr.c](https://github.com/torvalds/linux/tree/master/net/ipv4) | kernel source | `tcp-throughput` |
| [LWN: BBR congestion control (2016)](https://lwn.net/Articles/701165/) | LWN | `tcp-throughput` |

## Packet queues

| Reference | Kind | Cited by |
|---|---|---|
| [tc(8)](https://man7.org/linux/man-pages/man8/tc.8.html) | man page | `packet-queues` |
| [tc-fq_codel(8)](https://man7.org/linux/man-pages/man8/tc-fq_codel.8.html) | man page | `packet-queues` |
| [tc-fq(8)](https://man7.org/linux/man-pages/man8/tc-fq.8.html) | man page | `packet-queues` |
| [tc-pfifo_fast(8)](https://man7.org/linux/man-pages/man8/tc-pfifo_fast.8.html) | man page | `packet-queues` |
| [RFC 8290: The Flow Queue CoDel Packet Scheduler and Active Queue Management Algorithm](https://www.rfc-editor.org/rfc/rfc8290) | specification | `packet-queues` |
| [RFC 8289: Controlled Delay Active Queue Management](https://www.rfc-editor.org/rfc/rfc8289) | specification | `packet-queues` |
| [Kernel source: net/sched/sch_fq_codel.c, sch_fq.c, sch_generic.c (pfifo_fast, mq)](https://github.com/torvalds/linux/tree/master/net/sched) | kernel source | `packet-queues` |
| [LWN: Network transmit queue limits (byte queue limits, 2011)](https://lwn.net/Articles/454390/) | LWN | `packet-queues` |
| [LWN: TCP small queues (2012)](https://lwn.net/Articles/507065/) | LWN | `packet-queues` |
| [Gettys, Nichols: Bufferbloat: Dark Buffers in the Internet, ACM Queue 9(11), 2011](https://queue.acm.org/detail.cfm?id=2071893) | paper | `packet-queues` |

## Version-dependent behaviour

What the lessons say changes with the kernel version, oldest first. "Applies" is copied from the lesson; where a lesson gives a range or a configuration instead of a single version, it is kept as written. The live-machine and fixture kernels Machine Atlas was exercised on are listed in the README.

| Since (as the lesson states it) | Behaviour | Lesson |
|---|---|---|
| Linux ≥ 2.6 | NAPI: drivers poll their receive rings from a softirq while packets keep arriving, instead of taking one interrupt per packet. | [`packet-path`](../curriculum/lessons/packet-path.md) |
| Linux ≥ 2.6.11 | /proc/stat reports steal time, the time a virtual CPU was ready to run but the hypervisor ran something else; guest time followed in 2.6.24 and guest_nice in 2.6.33. | [`run-queues-and-load`](../curriculum/lessons/run-queues-and-load.md) |
| Linux 2.6.23 | The Completely Fair Scheduler replaced the O(1) scheduler: per-task virtual runtime in a red-black tree, the task with the smallest one running next. | [`fair-scheduling`](../curriculum/lessons/fair-scheduling.md) |
| Linux ≥ 2.6.38 | Autogroup: tasks of each session (each setsid(), typically each terminal) form a group that shares CPU time with other sessions before its tasks share it among themselves. | [`fair-scheduling`](../curriculum/lessons/fair-scheduling.md) |
| Linux ≥ 2.6.30 | relatime became the default for mounts that do not choose an access-time policy; strictatime was added to ask for the old behaviour. | [`filesystems-and-the-vfs`](../curriculum/lessons/filesystems-and-the-vfs.md) |
| Linux ≥ 2.6.38 | Transparent huge pages for anonymous memory, with madvise(MADV_HUGEPAGE) and MADV_NOHUGEPAGE. | [`transparent-huge-pages`](../curriculum/lessons/transparent-huge-pages.md) |
| Linux ≥ 2.6.19 | CUBIC is the default congestion control; before, BIC. | [`tcp-throughput`](../curriculum/lessons/tcp-throughput.md) |
| Linux ≥ 3.0 | The initial congestion window is 10 segments (RFC 6928), so a new connection may send about 14 KiB before the first acknowledgement. | [`tcp-throughput`](../curriculum/lessons/tcp-throughput.md) |
| Linux ≥ 3.2 | CFS bandwidth control: cpu.cfs_quota_us and cpu.cfs_period_us cap a cgroup v1 group's CPU time per period. | [`cpu-limits-and-isolation`](../curriculum/lessons/cpu-limits-and-isolation.md) |
| Linux ≥ 3.2 | IO-less dirty throttling: balance_dirty_pages pauses a writer for a computed time instead of making it write pages back itself, and divides the dirty limit between devices by their measured writeback bandwidth. | [`writeback-and-durability`](../curriculum/lessons/writeback-and-durability.md) |
| Linux ≥ 3.3 | Byte queue limits (BQL) cap the bytes queued in a driver's transmit ring, for drivers that report completions to it. | [`packet-queues`](../curriculum/lessons/packet-queues.md) |
| Linux ≥ 3.5 | The fq_codel and codel qdiscs; fq (per-flow pacing) followed in 3.12, together with net.core.default_qdisc. | [`packet-queues`](../curriculum/lessons/packet-queues.md) |
| Linux ≥ 3.6 | The IPv4 routing cache was removed; each lookup walks the FIB trie, and sockets cache the result (the dst entry) instead. | [`packet-path`](../curriculum/lessons/packet-path.md) |
| Linux ≥ 3.6 | TCP Small Queues limit how much data one TCP socket may have in the qdisc and driver at once. | [`packet-queues`](../curriculum/lessons/packet-queues.md) |
| Linux ≥ 3.13 | blk-mq, the multi-queue block layer, was merged: per-CPU software queues mapped onto the device's hardware queues. NVMe used it from 3.19 and SCSI could from 3.17. | [`block-devices`](../curriculum/lessons/block-devices.md) |
| Linux ≥ 3.14 | MemAvailable was added to /proc/meminfo. Before it, tools estimated available memory as MemFree + Cached, which counts shmem as droppable and ignores reclaimable slab. | [`page-cache`](../curriculum/lessons/page-cache.md) |
| Linux ≥ 3.15 | Workingset detection: evicted file pages leave a shadow entry in the page cache, so a refault can be recognised and the page activated at once. Anonymous pages gained the same in 5.9, when workingset_refault split into _anon and _file counters. | [`reclaim-and-swap`](../curriculum/lessons/reclaim-and-swap.md) |
| Linux ≥ 3.17 | addr_assign_type in sysfs says where a MAC address came from: the hardware (0), randomly generated (1), copied from another device, as a bond's is (2), or set from user space (3). | [`network-interfaces`](../curriculum/lessons/network-interfaces.md) |
| Linux ≥ 3.18 | overlayfs was merged, the union filesystem container runtimes use for image layers. | [`filesystems-and-the-vfs`](../curriculum/lessons/filesystems-and-the-vfs.md) |
| Linux ≥ 4.0 | lazytime keeps access, change and modification times in memory and writes them only with other inode changes, on fsync, or after a day. | [`filesystems-and-the-vfs`](../curriculum/lessons/filesystems-and-the-vfs.md) |
| Linux ≥ 4.2 | Unprivileged readers of /proc/&lt;pid&gt;/pagemap see presence and swap bits but a zeroed frame number; 4.0 and 4.1 refused them entirely. CAP_SYS_ADMIN is needed for frame numbers. | [`page-faults`](../curriculum/lessons/page-faults.md) |
| Linux ≥ 4.4 | Connection requests are small request sockets in the listener's hash table (TCP_NEW_SYN_RECV) rather than entries in a per-listener list, so a SYN flood no longer contends on one lock; /proc/net/tcp still shows them as SYN_RECV. | [`sockets-and-connections`](../curriculum/lessons/sockets-and-connections.md) |
| Linux 4.4 to 5.2 | VmallocUsed always reads 0: the figure was dropped because computing it was slow, and restored in 5.3 from a cheap counter. On these kernels vmalloc memory falls into the unaccounted remainder. | [`anonymous-and-file-pages`](../curriculum/lessons/anonymous-and-file-pages.md) |
| Linux ≥ 4.5 | The number of random bits in the mmap base became tunable as vm.mmap_rnd_bits, with a build-time default of 28 on x86-64. | [`virtual-address-spaces`](../curriculum/lessons/virtual-address-spaces.md) |
| Linux ≥ 4.5 | /proc/&lt;pid&gt;/status splits VmRSS into RssAnon, RssFile and RssShmem. Older kernels give only the total. | [`anonymous-and-file-pages`](../curriculum/lessons/anonymous-and-file-pages.md) |
| Linux ≥ 4.6 | The gaps between the min, low and high watermarks scale with zone size through vm.watermark_scale_factor; before, low and high were fixed at 1.25 and 1.5 times min. | [`physical-memory`](../curriculum/lessons/physical-memory.md) |
| Linux ≥ 4.6 | The OOM reaper: a kernel thread that frees a victim's anonymous memory without waiting for the victim to exit, so a victim stuck in the kernel cannot hold the system hostage. | [`memory-pressure-and-oom`](../curriculum/lessons/memory-pressure-and-oom.md) |
| Linux ≥ 4.7 | The schedutil governor sets frequencies from the scheduler's own utilisation figures instead of sampling idle time as ondemand does. | [`cpu-frequency`](../curriculum/lessons/cpu-frequency.md) |
| Linux ≥ 4.8 | Huge pages for shmem and tmpfs, controlled by shmem_enabled and the tmpfs huge= mount option; counted in ShmemHugePages. | [`transparent-huge-pages`](../curriculum/lessons/transparent-huge-pages.md) |
| Linux ≥ 4.9 | BBR congestion control (tcp_bbr). Since 4.13 TCP paces itself when the qdisc does not, so BBR no longer requires the fq qdisc. | [`tcp-throughput`](../curriculum/lessons/tcp-throughput.md) |
| Linux ≥ 4.10 | Writeback throttling (wbt) limits how many background writes are in flight on a device when reads on it take longer than wbt_lat_usec. | [`writeback-and-durability`](../curriculum/lessons/writeback-and-durability.md) |
| Linux ≥ 4.11 | mq-deadline, the first scheduler for blk-mq; bfq and kyber followed in 4.12. | [`io-schedulers`](../curriculum/lessons/io-schedulers.md) |
| Linux ≥ 4.13 | Writeback errors are recorded per file with an errseq_t: every file descriptor open when an error occurs sees it once at its next fsync, instead of whichever caller happened to ask first. | [`writeback-and-durability`](../curriculum/lessons/writeback-and-durability.md) |
| Linux ≥ 4.13 | /proc/vmstat counts OOM kills (oom_kill), cgroup and system-wide alike, and cgroup v2 memory.events gained oom_kill. Before, the kernel log was the only record. | [`memory-pressure-and-oom`](../curriculum/lessons/memory-pressure-and-oom.md) |
| Linux ≥ 4.14 (x86-64) | 5-level paging became available (CONFIG_X86_5LEVEL). Since 4.17 one kernel image chooses 4 or 5 levels at boot from the CPU's LA57 support, the no5lvl boot option forces 4, and /proc/cpuinfo lists la57 only when the kernel enabled 5-level paging. | [`cpu-addressing`](../curriculum/lessons/cpu-addressing.md) |
| Linux ≥ 4.14 (x86-64, 5-level paging) | With 5-level paging, mmap still returns addresses below 2^47 unless a program passes a hint address above it, so ordinary processes keep the 4-level layout. | [`virtual-address-spaces`](../curriculum/lessons/virtual-address-spaces.md) |
| Linux ≥ 4.14 | /proc/&lt;pid&gt;/smaps_rollup gives the smaps totals for the whole process in one record, much cheaper to read than summing smaps. | [`mappings`](../curriculum/lessons/mappings.md) |
| Linux ≥ 4.14 | Swap readahead can follow the faulting process's virtual addresses instead of neighbouring swap slots (/sys/kernel/mm/swap/vma_ra_enabled); page-cluster still caps the window. | [`reclaim-and-swap`](../curriculum/lessons/reclaim-and-swap.md) |
| Linux ≥ 4.15 (x86-64) | Kernel page-table isolation (KPTI) gives each process a second, stripped-down page-table tree for user mode on CPUs affected by Meltdown; unaffected CPUs keep the single tree with the kernel mapped in. | [`virtual-address-spaces`](../curriculum/lessons/virtual-address-spaces.md) |
| Linux ≥ 4.15 | The cgroup v2 cpu controller: cpu.max (quota and period), cpu.weight and cpu.stat with throttling counts. | [`cpu-limits-and-isolation`](../curriculum/lessons/cpu-limits-and-isolation.md) |
| Linux ≥ 4.16 | meminfo has a Percpu line. Before it, per-CPU allocations were counted in no meminfo field at all. | [`anonymous-and-file-pages`](../curriculum/lessons/anonymous-and-file-pages.md) |
| Linux ≥ 4.18 | /sys/block/&lt;dev&gt;/queue/fua reports whether the driver accepts Force Unit Access writes. | [`block-devices`](../curriculum/lessons/block-devices.md) |
| Linux ≥ 4.18 | Block device statistics gained four discard fields, 15 in all. | [`io-statistics`](../curriculum/lessons/io-statistics.md) |
| Linux ≥ 4.19 | /sys/devices/system/cpu/smt/control and smt/active, added with the L1TF mitigations, report and switch simultaneous multithreading at run time. | [`cpu-topology`](../curriculum/lessons/cpu-topology.md) |
| Linux ≥ 4.19 | net.ipv4.tcp_tw_reuse defaults to 2: TIME_WAIT reuse for outgoing connections on loopback only. | [`sockets-and-connections`](../curriculum/lessons/sockets-and-connections.md) |
| Linux ≥ 4.19 | memory.oom.group lets a cgroup v2 group be killed as a whole, so a workload is not left half-alive. | [`memory-pressure-and-oom`](../curriculum/lessons/memory-pressure-and-oom.md) |
| Linux ≥ 4.20 | Pressure stall information (/proc/pressure/io, memory, cpu); kernels can be built without it (CONFIG_PSI) or boot with it disabled (psi=0). | [`io-statistics`](../curriculum/lessons/io-statistics.md) |
| Linux ≥ 4.20 | Pressure stall information: /proc/pressure/cpu reports the share of time some runnable task was waiting for a CPU. The system-wide full line was added in 5.13 and is always zero; it means something for cgroups. | [`run-queues-and-load`](../curriculum/lessons/run-queues-and-load.md) |
| Linux ≥ 4.20 | KReclaimable reports reclaimable slab together with other kernel memory the kernel can drop under pressure; SReclaimable remains the slab part. | [`anonymous-and-file-pages`](../curriculum/lessons/anonymous-and-file-pages.md) |
| Linux ≥ 4.20 | The page cache of each file is indexed by an XArray; before, by a radix tree with the same shape. Since 5.16 its entries are folios, which may span several pages, rather than single pages. | [`page-cache`](../curriculum/lessons/page-cache.md) |
| Linux ≥ 4.20 | Pressure stall information (/proc/pressure/memory). Kernels can be built without it (CONFIG_PSI) or boot with it disabled (psi=0). | [`reclaim-and-swap`](../curriculum/lessons/reclaim-and-swap.md) |
| Linux ≥ 4.20 | Pressure stall information, which user-space OOM daemons such as systemd-oomd use to act before the kernel does. | [`memory-pressure-and-oom`](../curriculum/lessons/memory-pressure-and-oom.md) |
| Linux ≥ 5.0 | The single-queue block layer and its schedulers (noop, deadline, cfq) were removed; every request-based driver uses blk-mq. | [`block-devices`](../curriculum/lessons/block-devices.md) |
| Linux ≥ 5.0 (arm64) | With 64 KiB pages a kernel can offer 52-bit user addresses, but mmap returns addresses above 2^48 only to programs that ask for them with a hint, so ordinary processes keep a 48-bit layout. | [`cpu-addressing`](../curriculum/lessons/cpu-addressing.md) |
| Linux ≥ 5.0 | The single-queue schedulers noop, deadline and cfq were removed with the single-queue block layer; none, mq-deadline, bfq and kyber remain. | [`io-schedulers`](../curriculum/lessons/io-schedulers.md) |
| Linux ≥ 5.0 | A device with one hardware queue gets mq-deadline by default and one with several gets none, unless a udev rule or an administrator selects another; the elevator= boot option is ignored since Linux 5.4. | [`io-schedulers`](../curriculum/lessons/io-schedulers.md) |
| Linux ≥ 5.2 | topology/die_id and die_cpus_list describe dies, for packages that hold more than one. | [`cpu-topology`](../curriculum/lessons/cpu-topology.md) |
| Linux ≥ 5.4 | net.core.somaxconn defaults to 4096; before, 128, which silently capped the backlog of busy servers. | [`sockets-and-connections`](../curriculum/lessons/sockets-and-connections.md) |
| Linux ≥ 5.5 | Block device statistics gained two flush fields, 17 in all. | [`io-statistics`](../curriculum/lessons/io-statistics.md) |
| Linux ≥ 5.7 (x86) | Frequency invariance: the scheduler scales a task's measured utilisation by the frequency it ran at, so a task that kept a slow CPU busy is not mistaken for a heavy one. | [`cpu-frequency`](../curriculum/lessons/cpu-frequency.md) |
| Linux ≥ 5.8 | vm.swappiness accepts 0 to 200; before, 0 to 100. Values above 100 make anonymous pages cheaper to reclaim than file pages, for fast swap such as zram. | [`reclaim-and-swap`](../curriculum/lessons/reclaim-and-swap.md) |
| Linux ≥ 5.10 | /proc/net/softnet_stat prints each CPU's number and backlog length; before, a line per online CPU with no number, so offline CPUs shift the rows. | [`packet-path`](../curriculum/lessons/packet-path.md) |
| Linux ≥ 5.12 | Threaded NAPI (/sys/class/net/&lt;if&gt;/threaded) can run a device's polling in a kernel thread the scheduler sees, instead of in softirq context. | [`packet-path`](../curriculum/lessons/packet-path.md) |
| Linux ≥ 5.13 | On hybrid Intel processors /sys/devices/cpu_core/cpus and /sys/devices/cpu_atom/cpus list the performance and efficiency cores, because the two kinds have different performance counters. | [`cpu-topology`](../curriculum/lessons/cpu-topology.md) |
| Linux ≥ 5.14 | madvise(MADV_POPULATE_READ / MADV_POPULATE_WRITE) prefaults a range on request, an alternative to MAP_POPULATE for memory that is already mapped. | [`page-faults`](../curriculum/lessons/page-faults.md) |
| Linux ≥ 5.14 | cpu.max.burst lets a group carry unused quota into the next periods, up to a limit, so short bursts are not throttled. | [`cpu-limits-and-isolation`](../curriculum/lessons/cpu-limits-and-isolation.md) |
| Linux ≥ 5.16 | topology/cluster_id and cluster_cpus_list describe clusters of cores that share an L2 cache or other resources below the package level. | [`cpu-topology`](../curriculum/lessons/cpu-topology.md) |
| Linux ≥ 5.16 | Folios: the kernel increasingly handles memory as folios, groups of one or more contiguous pages described together, instead of individual struct pages. | [`physical-memory`](../curriculum/lessons/physical-memory.md) |
| Linux ≥ 5.17 | Anonymous mappings can carry a name set with prctl(PR_SET_VMA_ANON_NAME), shown in maps as [anon:name]; Go 1.25 and later use it to label their heap, which is how the probe's maps show [anon: Go: heap] on recent kernels. | [`virtual-address-spaces`](../curriculum/lessons/virtual-address-spaces.md) |
| Linux ≥ 5.17 | Anonymous mappings may be named with prctl(PR_SET_VMA_ANON_NAME) and appear in maps as [anon:name]; a mapping with a different name will not merge with its neighbour. | [`mappings`](../curriculum/lessons/mappings.md) |
| Linux ≥ 5.18 | Readahead was rewritten around folios (page_cache_ra_order): a sequential read can allocate large folios for the readahead window on filesystems that support them. | [`filesystems-and-the-vfs`](../curriculum/lessons/filesystems-and-the-vfs.md) |
| Linux ≥ 5.19 | With zswap built in, meminfo adds Zswap (the compressed pool) and Zswapped (the pages it holds); before that the pool was counted nowhere in meminfo. | [`anonymous-and-file-pages`](../curriculum/lessons/anonymous-and-file-pages.md) |
| Linux ≥ 6.1 | A process's VMAs are indexed by a maple tree, a B-tree variant built for ranges. Before 6.1 they were kept in a red-black tree plus a sorted linked list, with a small per-thread cache of recent lookups. | [`mappings`](../curriculum/lessons/mappings.md) |
| Linux ≥ 6.1 | madvise(MADV_COLLAPSE) lets a program collapse a range into huge pages synchronously, whatever the sysfs mode. | [`transparent-huge-pages`](../curriculum/lessons/transparent-huge-pages.md) |
| Linux ≥ 6.1 | Multi-Gen LRU (CONFIG_LRU_GEN) can replace the two-list LRU; whether it is on is a build default (CONFIG_LRU_GEN_ENABLED) that /sys/kernel/mm/lru_gen/enabled can change at run time. | [`reclaim-and-swap`](../curriculum/lessons/reclaim-and-swap.md) |
| Linux ≥ 6.2 | Per-device writeback limits can be set in bytes (min_bytes, max_bytes) and with finer ratios, and strict_limit is exposed in sysfs. | [`writeback-and-durability`](../curriculum/lessons/writeback-and-durability.md) |
| Linux ≥ 6.3 | amd-pstate gained its active (EPP) mode, in which the processor chooses frequencies guided by an energy-performance preference, as intel_pstate does with HWP; later kernels made it the default on many Zen systems. | [`cpu-frequency`](../curriculum/lessons/cpu-frequency.md) |
| Linux ≥ 6.4 | Most anonymous page faults take a lock on the faulting VMA only, instead of the whole address space's mmap lock, so threads faulting in different regions no longer serialise. | [`page-faults`](../curriculum/lessons/page-faults.md) |
| Linux ≥ 6.4 | The largest buddy order constant, MAX_ORDER, was made inclusive, and Linux 6.8 renamed it MAX_PAGE_ORDER; with 4 KiB pages the largest order is still 10 (4 MiB) by default, so /proc/buddyinfo still has eleven columns. | [`physical-memory`](../curriculum/lessons/physical-memory.md) |
| Linux ≥ 6.5 | The cachestat() system call reports, for a range of a file, how many pages are cached, dirty, under writeback and recently evicted, without mapping the file as mincore() requires. | [`page-cache`](../curriculum/lessons/page-cache.md) |
| Linux ≥ 6.6 | EEVDF replaced CFS's pick: the eligible task with the earliest virtual deadline runs. The sched_latency and min_granularity tunables gave way to one base slice. | [`fair-scheduling`](../curriculum/lessons/fair-scheduling.md) |
| Linux ≥ 6.6 | The receive window is computed from the measured ratio of payload to buffer memory (scaling_ratio) instead of the fixed tcp_adv_win_scale, so more of tcp_rmem becomes usable window. | [`tcp-throughput`](../curriculum/lessons/tcp-throughput.md) |
| Linux ≥ 6.8 | Multi-size THP lets anonymous faults allocate blocks between one page and a PMD-sized huge page (16 KiB to 1 MiB with 4 KiB pages), when an administrator enables those sizes. | [`page-faults`](../curriculum/lessons/page-faults.md) |
| Linux ≥ 6.8 | Multi-size THP: per-size controls under /sys/kernel/mm/transparent_hugepage/hugepages-&lt;size&gt;kB/ allow anonymous folios between one page and PMD size. They are mapped with ordinary page-table entries and are not counted in AnonHugePages. | [`transparent-huge-pages`](../curriculum/lessons/transparent-huge-pages.md) |
| Linux ≥ 6.12 | sched_ext lets a BPF program implement the scheduling policy, and a task may request its own slice length through sched_setattr(). Real-time throttling is enforced by a deadline server that reserves time for fair tasks. | [`fair-scheduling`](../curriculum/lessons/fair-scheduling.md) |
| systemd ≥ 197 | udev names interfaces after where the device sits (enp3s0: PCI bus 3, slot 0; wlp4s0 for wireless) instead of the order drivers loaded (eth0, wlan0), so names survive reboots and hardware changes. Many embedded and container setups keep the old names. | [`network-interfaces`](../curriculum/lessons/network-interfaces.md) |
| iproute2 ≥ 4.13 | ip prints JSON with -json, which is how Machine Atlas reads IPv4 addresses; older ip and busybox's applet print only text. | [`network-interfaces`](../curriculum/lessons/network-interfaces.md) |
| Linux since 1993 | The load average began counting tasks in uninterruptible sleep as well as runnable ones, so that a system waiting for its disks would not look idle; every Linux since has kept that definition, unlike most other Unix systems. | [`run-queues-and-load`](../curriculum/lessons/run-queues-and-load.md) |
| Go ≥ 1.25 | The Go runtime sets GOMAXPROCS from a cgroup CPU quota as well as from the CPU affinity mask, so a Go program in a container no longer runs more threads than its quota can serve; most other runtimes still size themselves by CPU count. | [`cpu-limits-and-isolation`](../curriculum/lessons/cpu-limits-and-isolation.md) |
| systemd ≥ 217 | systemd sets net.core.default_qdisc to fq_codel through its sysctl defaults, so most distributions use it unless they override it. | [`packet-queues`](../curriculum/lessons/packet-queues.md) |
| cgroup v1 | Memory limits are set in memory.limit_in_bytes and OOM handling in memory.oom_control, in a separate memory hierarchy; there is no memory.high or memory.events. Machine Atlas reads only the v2 interface. | [`memory-pressure-and-oom`](../curriculum/lessons/memory-pressure-and-oom.md) |
