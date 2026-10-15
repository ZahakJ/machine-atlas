---
{
  "id": "cpu-topology",
  "title": "How many CPUs does this machine have, really?",
  "subsystem": "cpu",
  "kind": "core",
  "order": 1,
  "summary": "Packages, cores and hardware threads, the caches each one shares, hybrid cores, and how Linux numbers logical CPUs and groups them for the scheduler.",
  "prerequisites": ["cpu-addressing"],
  "uses": [
    "cpu.cache_domains",
    "cpu.core_count",
    "cpu.hybrid.efficiency_cpus",
    "cpu.hybrid.performance_cpus",
    "cpu.logical_count",
    "cpu.model_name",
    "cpu.offline",
    "cpu.online",
    "cpu.possible",
    "cpu.present",
    "cpu.smt.active",
    "cpu.smt.control",
    "cpu.socket_count",
    "cpu.threads_per_core",
    "cpu.topology",
    "derived.cpu.hybrid",
    "derived.cpu.smt",
    "derived.env.kind"
  ],
  "relevance": [
    {"when": "derived.cpu.hybrid", "is": true, "note": "The cores here are not all alike, so where the scheduler places a task changes how fast it runs."},
    {"when": "derived.cpu.smt", "is": true, "note": "Cores here run two hardware threads, so pairs of logical CPUs share execution units and caches."}
  ],
  "next": ["cpu-frequency"],
  "deeper": ["planned:cpu-caches", "planned:numa-placement", "planned:load-balancing-and-sched-domains"],
  "version_notes": [
    {"applies": "Linux ≥ 4.19", "text": "/sys/devices/system/cpu/smt/control and smt/active, added with the L1TF mitigations, report and switch simultaneous multithreading at run time."},
    {"applies": "Linux ≥ 5.2", "text": "topology/die_id and die_cpus_list describe dies, for packages that hold more than one."},
    {"applies": "Linux ≥ 5.13", "text": "On hybrid Intel processors /sys/devices/cpu_core/cpus and /sys/devices/cpu_atom/cpus list the performance and efficiency cores, because the two kinds have different performance counters."},
    {"applies": "Linux ≥ 5.16", "text": "topology/cluster_id and cluster_cpus_list describe clusters of cores that share an L2 cache or other resources below the package level."}
  ],
  "references": [
    {"title": "Linux kernel documentation: How CPU topology info is exported via sysfs", "url": "https://docs.kernel.org/admin-guide/cputopology.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: x86 Topology", "url": "https://docs.kernel.org/arch/x86/topology.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Scheduler Domains", "url": "https://docs.kernel.org/scheduler/sched-domains.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Capacity Aware Scheduling", "url": "https://docs.kernel.org/scheduler/sched-capacity.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: L1TF, L1 Terminal Fault (SMT control)", "url": "https://docs.kernel.org/admin-guide/hw-vuln/l1tf.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: CPU hotplug in the Kernel (possible, present and online)", "url": "https://docs.kernel.org/core-api/cpu_hotplug.html", "kind": "kernel-doc"},
    {"title": "lscpu(1)", "url": "https://man7.org/linux/man-pages/man1/lscpu.1.html", "kind": "man-page"},
    {"title": "Intel 64 and IA-32 Architectures Software Developer's Manual, Volume 3A, Chapter 10: Multiple-Processor Management (topology enumeration with CPUID)", "url": "https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html", "kind": "spec"},
    {"title": "Arm Architecture Reference Manual for A-profile architecture: MPIDR_EL1, the multiprocessor affinity register", "url": "https://developer.arm.com/documentation/ddi0487/latest", "kind": "spec"}
  ]
}
---
## Question

:::when cpu.logical_count
This machine reports {{obs:cpu.logical_count}} logical CPUs: that is the number `nproc` prints and the number of rows in a CPU usage view. Yet it has {{obs:cpu.core_count}} physical cores; the most hardware threads any one core runs is {{obs:cpu.threads_per_core}}; and the number of packages, the chips in sockets, is {{obs:cpu.socket_count}}. Which of these numbers is "how many CPUs" this machine has, and why does Linux count the smallest unit rather than the chips you could hold in your hand?

:::when cpu.model_name
The processor calls itself {{obs:cpu.model_name}}.
:::unknown
The processor gives no model name here ({{obs:cpu.model_name}}): arm64 kernels identify a core only by implementer and part numbers.
:::end
:::unknown
Machine Atlas could not count the logical CPUs here ({{obs:cpu.logical_count}}). A machine usually reports one figure that `nproc` prints, and a processor built from fewer cores and fewer packages than that. Which of these numbers is "how many CPUs" a machine has, and why does Linux count the smallest unit rather than the chips you could hold in your hand?
:::end

## Why it exists

For thirty years a faster computer meant a faster clock. Around 2005 that stopped: each smaller transistor still switched faster, but no longer at lower power, and a chip that runs faster than about 4 GHz turns too much of its power into heat to cool. What kept growing was the number of transistors. Designers spent them on more complete processors on one chip, called *cores*, each able to run its own program.

A single core is also idle more often than it looks. When an instruction waits a hundred nanoseconds for memory, the execution units behind it have nothing to do. *Simultaneous multithreading* (SMT; Intel calls it Hyper-Threading) gives one core two sets of registers, so it can run two instruction streams and fill one stream's stalls with the other's work. To the operating system each set looks like a separate processor.

So a modern machine is a nest: packages hold cores, cores hold hardware threads, and caches are shared at every level in between. Linux schedules onto the innermost unit, the hardware thread, and calls each one a *logical CPU*. But where a task runs matters: two tasks on sibling threads compete for one core, a task moved to another core leaves its cached data behind, and on some processors one core is simply faster than another. The scheduler needs the nest, and the kernel publishes it.

## How it works

### Packages, dies, cores and threads

A *package* is what sits in a socket. It may contain several *dies* (separate pieces of silicon, such as the chiplets of an AMD Ryzen or EPYC), each die several *cores*, and each core one or more *hardware threads*. Firmware describes this nest to the kernel at boot: x86 processors report it through the CPUID instruction (leaves 0xB and 0x1F) and the ACPI tables, arm64 systems through the MPIDR register, ACPI's PPTT table or the device tree.

The kernel numbers the logical CPUs in the order firmware lists them, and that order rarely matches the nest. Intel and AMD desktops usually list the first thread of every core before any second thread, so CPU 0 and CPU *n* + 0 are siblings on an *n*-core part, not CPU 0 and CPU 1. Each CPU's directory in `/sys/devices/system/cpu/cpuN/topology` gives its `physical_package_id`, `die_id`, `core_id` and `thread_siblings_list`. Only the sibling list identifies a core reliably: core ids are labels that can repeat in every package and skip numbers.

### What two hardware threads share

Sibling threads have separate registers and instruction pointers, and share almost everything else: the decoders, the execution units, the L1 and L2 caches and the core's TLB. When one thread stalls on memory the other uses the idle units, which typically adds 10 to 30 percent of throughput to the core, far from doubling it. When both are busy with the same kind of work, each runs noticeably slower than it would alone.

Sharing also leaks. Several speculative-execution attacks, such as L1TF and MDS, let one thread infer data the sibling thread touched in shared buffers. That is why the kernel can switch SMT off at run time: `/sys/devices/system/cpu/smt/control` reads `on`, `off`, `forceoff` (disabled on the command line), `notsupported` (no SMT) or `notimplemented` (this architecture cannot switch it).

### Caches, and who shares them

Each core has its own small, fast L1 caches, one for data and one for instructions, a few tens of kilobytes answering in about a nanosecond. Behind them is a larger L2, private to a core on most designs and shared by a cluster of four cores on others (Intel's efficiency cores, many arm designs). The last level, L3, is megabytes in size and shared by all the cores of a package or of one chiplet. Caches move memory in lines of 64 bytes.

For every CPU, `/sys/devices/system/cpu/cpuN/cache/indexK` lists each cache it uses with its level, type, size and `shared_cpu_list`. Two CPUs that list the same L3 can hand each other data through it; two that share only main memory pay a trip to RAM, and on a multi-socket machine possibly across the link between sockets.

### Hybrid processors

Some processors mix cores of different designs: large *performance* cores and small, power-efficient *efficiency* cores (Intel since Alder Lake; arm's big.LITTLE and DynamIQ). The scheduler must then place demanding tasks on fast cores and background work on slow ones. The kernel describes the difference as a *capacity* per CPU, 1024 for the fastest, published in `cpu_capacity` on arm64 and RISC-V, and on hybrid x86 it lists the two kinds in `/sys/devices/cpu_core/cpus` and `/sys/devices/cpu_atom/cpus`.

### Possible, present and online

Linux distinguishes CPUs that could ever exist in this boot (`possible`, for which per-CPU memory is reserved), CPUs that are there (`present`), and CPUs that are running (`online`). A CPU can be taken offline by writing 0 to its `online` file, and virtual machines can hot-plug CPUs, so these lists can differ.

### What the scheduler builds from it

At boot, and whenever CPUs come or go, the scheduler builds *scheduling domains*: nested groups of CPUs that share something. Typically the smallest domain is the SMT siblings of one core, the next the cores sharing a cache, then the package, then NUMA nodes. Load balancing works from the inside out: it moves work between siblings readily, between packages reluctantly, because every move leaves cached data behind. The same nest is what `lscpu` and `taskset` describe.

## On this machine

:::evidence The counts
- cpu.model_name
- cpu.logical_count | Logical CPUs: hardware threads Linux schedules on
- cpu.core_count | Physical cores
- cpu.socket_count | Packages (sockets)
- cpu.threads_per_core | Most hardware threads on one core
- derived.cpu.smt
- cpu.smt.control
- cpu.smt.active
- derived.cpu.hybrid
:::end

:::diagram cpu-topology

:::when derived.cpu.smt
Cores here run {{obs:cpu.threads_per_core}} hardware threads, so the {{obs:cpu.logical_count}} logical CPUs come from only {{obs:cpu.core_count}} cores. Two tasks placed on siblings of one core share its execution units; placed on different cores, each has a core to itself. The `thread_siblings` column of the table below names each CPU's siblings.
:::else
Each core here runs a single hardware thread, so every logical CPU is a whole core with its own execution units and private caches.
:::unknown
Whether cores here run more than one hardware thread could not be determined ({{obs:cpu.threads_per_core}}). Where they do, two logical CPUs that are siblings share one core's execution units and caches.
:::end

:::when cpu.smt.control is on
SMT control reads {{obs:cpu.smt.control}}: the kernel may run sibling threads, and an administrator could switch them off at run time.
:::else
SMT control reads {{obs:cpu.smt.control}}: `notsupported` means the processor, or the virtual machine's description of it, has no SMT; `notimplemented` that this architecture cannot switch it at run time; `off` or `forceoff` that it was switched off.
:::unknown
SMT control could not be read here ({{obs:cpu.smt.control}}): the kernel predates it (4.19), or `/sys/devices/system/cpu` is restricted.
:::end

:::table cpu.topology columns=cpu,package,die,core_id,thread_siblings,core_type,capacity limit=16

:::when derived.cpu.hybrid
:::when cpu.hybrid.performance_cpus
The cores are not all alike here: logical CPUs {{obs:cpu.hybrid.performance_cpus}} are on performance cores and {{obs:cpu.hybrid.efficiency_cpus}} on efficiency cores, marked in the `core_type` column. The scheduler tends to place demanding tasks on the performance cores and lets the efficiency cores carry background work, and a task that moves between the two kinds changes speed.
:::else
The cores are not all alike here: the kernel gives the CPUs different capacities, shown in the `capacity` column (1024 is the fastest). The scheduler tends to place demanding tasks on high-capacity CPUs and lets the others carry background work, and a task that moves between them changes speed.
:::end
:::else
The cores here are all of one kind: no hybrid core lists exist, and where the kernel publishes a capacity per CPU, every CPU has the same one. All else equal, a task runs equally fast on any of them.
:::unknown
Whether the cores here are all alike could not be determined, because the per-CPU topology could not be read ({{obs:cpu.topology}}).
:::end

### The caches

:::table cpu.cache_domains columns=level,type,size_bytes,shared_cpu_list,cpus limit=32

Each row is one physical cache. A level with as many rows as cores is private to each core; a level with one row per package is shared by the whole package.

### Numbering and hot-plug

:::evidence Which CPUs exist
- cpu.possible
- cpu.present
- cpu.online
- cpu.offline | Offline CPUs (blank when none)
:::end

:::when derived.env.kind is vm-guest
Atlas found evidence of a virtual machine. Every figure above is the topology the hypervisor chose to present: the "cores" are threads of the host scheduled like any other, they may move between physical cores, and two of them may share one physical core without either knowing.
:::else
The figures above are what the kernel was told at boot. On bare metal they describe the silicon; inside a virtual machine, which Machine Atlas found no evidence of here, they would describe whatever the hypervisor chose to present.
:::unknown
Whether this system is a virtual machine could not be determined here. If it is, every figure above is the topology the hypervisor chose to present, not the physical cores underneath.
:::end

## Predict

:::predict
Two programs each keep one hardware thread busy with arithmetic and little memory traffic. Will they finish sooner on two sibling threads of one core, or on two different cores?

- Sooner on siblings: they share a cache, so they help each other.
- Sooner on different cores: siblings compete for one core's execution units.
- No difference: a logical CPU is a CPU.
:::reveal
On different cores. Sibling threads share the execution units, so two arithmetic-heavy programs on one core each get roughly the share of the core the other leaves, typically 55 to 65 percent of their speed alone; on two cores each runs at full speed. The scheduler knows this, which is why it fills idle cores before idle siblings.

The first answer is right for a different workload: two threads exchanging data through memory can benefit from sharing a core's L1 and L2, because a cache line written by one is already where the other reads it. The third is what the logical CPU count invites you to believe.

:::when derived.cpu.smt
This machine has SMT, so the experiment below can show the difference.
:::else
This machine has no SMT, so every pair of logical CPUs here is a pair of cores; on a machine with SMT the difference shows clearly.
:::end
:::end

## Try it

Read the topology the same way the kernel publishes it, without changing anything:

```sh
lscpu -e=CPU,SOCKET,CORE,CACHE,ONLINE
grep . /sys/devices/system/cpu/cpu0/topology/*_list
grep . /sys/devices/system/cpu/cpu0/cache/index*/shared_cpu_list
```

The first prints one line per logical CPU with its package, core and the caches it uses (L1d:L1i:L2:L3 by number). The second shows which CPUs share cpu0's core, die and package; the third which share each of its caches.

On a machine with SMT you can measure the prediction. Pick two siblings from the table above and two CPUs on different cores, and time the same busy loop pinned each way:

```sh
time (taskset -c 0 sh -c 'i=0; while [ $i -lt 3000000 ]; do i=$((i+1)); done' & taskset -c SIBLING sh -c 'i=0; while [ $i -lt 3000000 ]; do i=$((i+1)); done' & wait)
```

Replace SIBLING with cpu0's sibling, then with a CPU on another core. Expect the pair on one core to take noticeably longer; on a busy machine the difference is noisier.

## Limits

:::misconception nproc is the number of cores
`nproc` counts the logical CPUs this process may run on: hardware threads, after affinity and cpusets have removed any. On a machine with SMT it is twice the number of cores; in a container with a CPU set it can be fewer than the machine has; and it ignores CPU quotas entirely.
:::end

:::misconception SMT doubles the processor
Two hardware threads share one core's execution units and caches. They add throughput only where one thread leaves units idle, typically 10 to 30 percent, and two busy siblings each run slower than one thread alone.
:::end

:::misconception Neighbouring CPU numbers are neighbours on the chip
Logical CPU numbers follow the order firmware lists the threads, which on most x86 machines puts all first threads before all second threads. Siblings, dies and packages must be read from the topology files, never inferred from the numbers.
:::end

:::cannot
Nothing here measures distance or speed. The topology says which CPUs share a core or a cache, not how long a cache line takes to travel between them, how hot each core runs, or how a hybrid core's speed compares beyond the capacity number the kernel was given.

Inside a virtual machine the topology is a description chosen by the hypervisor, and nothing inside the guest can see the physical cores its virtual CPUs run on or which other guests share them.

The physical arrangement on the die, which cores sit next to which cache slice, is not published at all.
:::end

## Go deeper

Next, [frequency scaling](lesson:cpu-frequency) follows how fast each of these cores is allowed to run, and why that changes from moment to moment. In Explore, the [CPU topology](component:cpu-topology) and [CPU caches](component:cpu-caches) components show what depends on this nest. Planned lessons cover CPU caches and cache lines in depth, NUMA placement, and how the scheduler's domains balance load between cores.
