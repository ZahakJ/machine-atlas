---
{
  "id": "cpu-limits-and-isolation",
  "title": "Less than a whole CPU, or a CPU of one's own",
  "subsystem": "cpu",
  "kind": "core",
  "order": 5,
  "summary": "Three different controls over CPU time: sharing by cgroup weight, capping by quota and period with throttling, and placing by affinity, cpusets, isolated and tickless CPUs; and why a container's limit is invisible to the programs inside it.",
  "prerequisites": ["fair-scheduling"],
  "uses": [
    "cpu.isolated",
    "cpu.logical_count",
    "cpu.nohz_full",
    "derived.cgroup.cpu_limited",
    "derived.cgroup.cpu_quota_cpus",
    "derived.cgroup.cpu_throttled_fraction",
    "derived.cpu.isolated_count",
    "derived.env.kind",
    "derived.sched.self_allowed_count",
    "mem.cgroup.version",
    "sched.cgroup.cpu.max_period_us",
    "sched.cgroup.cpu.max_quota_us",
    "sched.cgroup.cpu.nr_periods",
    "sched.cgroup.cpu.nr_throttled",
    "sched.cgroup.cpu.shares_v1",
    "sched.cgroup.cpu.throttled_us",
    "sched.cgroup.cpu.usage_us",
    "sched.cgroup.cpu.weight",
    "sched.cgroup.cpuset.cpus_effective",
    "sched.self.cpus_allowed_list",
    "sched.sysctl.kernel.sched_cfs_bandwidth_slice_us"
  ],
  "relevance": [
    {"when": "derived.cgroup.cpu_limited", "is": true, "note": "Atlas runs under a CPU quota, so its cgroup can be throttled however idle the machine is."},
    {"when": "derived.cpu.isolated_count", "note": "Some CPUs here are isolated from the scheduler's load balancing."}
  ],
  "next": [],
  "deeper": ["planned:memory-cgroups", "planned:namespaces-and-isolation", "planned:real-time-scheduling"],
  "version_notes": [
    {"applies": "Linux ≥ 3.2", "text": "CFS bandwidth control: cpu.cfs_quota_us and cpu.cfs_period_us cap a cgroup v1 group's CPU time per period."},
    {"applies": "Linux ≥ 4.15", "text": "The cgroup v2 cpu controller: cpu.max (quota and period), cpu.weight and cpu.stat with throttling counts."},
    {"applies": "Linux ≥ 5.14", "text": "cpu.max.burst lets a group carry unused quota into the next periods, up to a limit, so short bursts are not throttled."},
    {"applies": "Go ≥ 1.25", "text": "The Go runtime sets GOMAXPROCS from a cgroup CPU quota as well as from the CPU affinity mask, so a Go program in a container no longer runs more threads than its quota can serve; most other runtimes still size themselves by CPU count."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Control Group v2 (cpu and cpuset controllers)", "url": "https://docs.kernel.org/admin-guide/cgroup-v2.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: CFS Bandwidth Control", "url": "https://docs.kernel.org/scheduler/sched-bwc.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: CPUSETS (cgroup v1)", "url": "https://docs.kernel.org/admin-guide/cgroup-v1/cpusets.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: NO_HZ, Reducing Scheduling-Clock Ticks", "url": "https://docs.kernel.org/timers/no_hz.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: The kernel's command-line parameters (isolcpus, nohz_full)", "url": "https://docs.kernel.org/admin-guide/kernel-parameters.html", "kind": "kernel-doc"},
    {"title": "sched_setaffinity(2)", "url": "https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html", "kind": "man-page"},
    {"title": "taskset(1)", "url": "https://man7.org/linux/man-pages/man1/taskset.1.html", "kind": "man-page"},
    {"title": "cpuset(7)", "url": "https://man7.org/linux/man-pages/man7/cpuset.7.html", "kind": "man-page"},
    {"title": "systemd.resource-control(5): CPUWeight=, CPUQuota=, AllowedCPUs=", "url": "https://man7.org/linux/man-pages/man5/systemd.resource-control.5.html", "kind": "man-page"}
  ]
}
---
## Question

:::when derived.cgroup.cpu_limited
Atlas runs in a cgroup that may use {{derived:derived.cgroup.cpu_quota_cpus}} CPUs' worth of time: a quota of {{obs:sched.cgroup.cpu.max_quota_us}} in every {{obs:sched.cgroup.cpu.max_period_us}}. Yet the machine reports {{obs:cpu.logical_count}} logical CPUs, and atlas may run on {{derived:derived.sched.self_allowed_count}} of them. How can a program run on several CPUs and still be held to a fraction of one, and what happens at the moment it uses up its share?
:::else
Atlas's cgroup has no CPU quota here, and atlas may run on {{derived:derived.sched.self_allowed_count}} of the {{obs:cpu.logical_count}} logical CPUs. Containers and services are often given less: half a CPU, or two particular CPUs. How can a program run on several CPUs and still be held to a fraction of one, and what happens at the moment it uses up its share?
:::unknown
Whether atlas's cgroup has a CPU quota could not be determined here ({{derived:derived.cgroup.cpu_limited}}). Containers and services are often given less than a machine: half a CPU, or two particular CPUs. How can a program run on several CPUs and still be held to a fraction of one, and what happens at the moment it uses up its share?
:::end

## Why it exists

Weights and nice values divide the CPU between tasks that compete for it, but they promise nothing to anyone else: a single task at nice 19 on an idle machine still gets a whole CPU. Machines shared between services, users or customers need more. A hosting provider must sell half a CPU and deliver no more than half even at three in the morning. A database must keep its latency-critical threads away from a backup job. A trading or audio system needs a CPU where nothing else ever runs, not even the kernel's own timer tick.

These are three different controls, and Linux keeps them separate: *sharing* (weights between groups), *capping* (a quota of time per period), and *placing* (which CPUs a task may use at all).

## How it works

### Sharing: weights between groups

The fair scheduler schedules groups as well as tasks. Every cgroup with the cpu controller enabled has a scheduling entity on each CPU, which competes with its siblings by its weight, `cpu.weight` (1 to 10 000, default 100; `cpu.shares`, default 1024, on cgroup v1). Inside the group, its tasks share what the group received, by their own weights. A weight only matters under contention: a group alone on an idle machine can use every CPU.

### Capping: quota and period

`cpu.max` holds a *quota* and a *period* in microseconds: `150000 100000` means the group's tasks together may run for 150 ms of CPU time in every 100 ms of wall time, one and a half CPUs' worth, whether or not anything else wants the CPU. `max` means no quota.

The quota is a pool, refilled at the start of every period. Each CPU running the group's tasks draws from it in slices of `sched_cfs_bandwidth_slice_us` (5 ms by default). When the pool is empty, the group is *throttled*: its tasks are taken off every run queue until the next period begins, however idle the machine is. `cpu.stat` counts the periods in which the group ran (`nr_periods`), those in which it ran out (`nr_throttled`), and the time its tasks spent held back (`throttled_usec`).

A quota limits time, not CPUs. A group with 1.5 CPUs' worth can run eight threads on eight CPUs at once; it simply exhausts the pool after under 19 ms of each 100 ms period and then waits for the rest. A multi-threaded program that does its work in bursts, such as a garbage collector or a request handled by a thread pool, is throttled in the middle of the burst, and its latency grows by the remainder of the period. `cpu.max.burst` (Linux 5.14 and later) lets a group bank unused quota for such bursts.

On cgroup v1 the same mechanism is configured by `cpu.cfs_quota_us` (−1 for none) and `cpu.cfs_period_us`.

### Placing: affinity and cpusets

A task's *affinity mask* lists the CPUs it may run on; `sched_setaffinity()` and `taskset` set it, and children inherit it. The *cpuset* controller does the same for a whole cgroup: `cpuset.cpus` requests CPUs, and `cpuset.cpus.effective` is what the group actually gets after its ancestors' restrictions. Affinity is respected strictly, so a task pinned to one busy CPU waits even while others are idle.

Placement and capping combine: `docker run --cpus 1.5 --cpuset-cpus 0-1` gives a container a quota of 150 000 per 100 000 µs and confines it to CPUs 0 and 1.

### Keeping CPUs for oneself

The boot parameter `isolcpus=` removes CPUs from the scheduler's load balancing: no ordinary task is moved there, so only tasks whose affinity names those CPUs run on them. `nohz_full=` goes further for the same CPUs: when exactly one task is runnable on such a CPU, the scheduler tick stops, and the kernel moves its periodic housekeeping, timers and deferred work to the other, *housekeeping* CPUs. The task then runs for seconds without an interrupt. Both are set at boot and published in `/sys/devices/system/cpu/isolated` and `nohz_full`; cpuset partitions can isolate CPUs at run time as well.

### What programs inside see

None of this changes what a program sees of the hardware. `/proc/cpuinfo` and `/sys/devices/system/cpu/online` list every CPU, and `nproc` counts the CPUs in the affinity mask, ignoring any quota. A runtime that starts one worker thread per CPU, a JVM, a thread pool, an older Go program, starts as many threads as the machine has CPUs and is then throttled for running them. Some runtimes now read the cgroup quota themselves; Go does since 1.25.

## On this machine

:::evidence The CPU controller of atlas's cgroup
- mem.cgroup.version
- sched.cgroup.cpu.max_quota_us
- sched.cgroup.cpu.max_period_us
- derived.cgroup.cpu_limited
- derived.cgroup.cpu_quota_cpus
- sched.cgroup.cpu.weight | Weight among sibling groups (cgroup v2)
- sched.cgroup.cpu.shares_v1 | Shares among sibling groups (cgroup v1)
- sched.cgroup.cpu.usage_us | CPU time used since the cgroup was created
- sched.cgroup.cpu.nr_periods
- sched.cgroup.cpu.nr_throttled
- sched.cgroup.cpu.throttled_us
- derived.cgroup.cpu_throttled_fraction
- sched.sysctl.kernel.sched_cfs_bandwidth_slice_us
:::end

:::when derived.cgroup.cpu_limited
:::when derived.cgroup.cpu_throttled_fraction
Atlas's cgroup may use {{derived:derived.cgroup.cpu_quota_cpus}} CPUs' worth of time per period, and it has hit that ceiling: in {{obs:sched.cgroup.cpu.nr_throttled}} of the {{obs:sched.cgroup.cpu.nr_periods}} periods in which it ran ({{derived:derived.cgroup.cpu_throttled_fraction|percent}}), its tasks were stopped until the next period, {{obs:sched.cgroup.cpu.throttled_us}} in all. Each of those was a pause its programs could not see coming, however idle the rest of the machine was.
:::else
Atlas's cgroup may use {{derived:derived.cgroup.cpu_quota_cpus}} CPUs' worth of time per period. It has not been throttled ({{obs:sched.cgroup.cpu.nr_throttled}} throttled periods), so far its tasks have always finished their work before the pool ran out.
:::unknown
Atlas's cgroup may use {{derived:derived.cgroup.cpu_quota_cpus}} CPUs' worth of time per period. How often it ran out could not be read ({{derived:derived.cgroup.cpu_throttled_fraction}}).
:::end
:::else
:::when sched.cgroup.cpu.max_quota_us
Atlas's cgroup has no CPU quota (`cpu.max` reads {{obs:sched.cgroup.cpu.max_quota_us}}), so nothing at this level stops its tasks using every CPU they are allowed on. A quota set on an ancestor cgroup would still apply, and Machine Atlas does not read the ancestors.
:::unknown
Atlas's cgroup has no `cpu.max` here ({{obs:sched.cgroup.cpu.max_quota_us}}): where the file is absent, the cpu controller is not enabled for the cgroup, so there is no quota and no weight at this level: atlas's tasks compete as members of the parent group. A quota set on an ancestor would still apply, and Machine Atlas does not read the ancestors.
:::end
:::unknown
Whether atlas's cgroup has a CPU quota could not be determined ({{derived:derived.cgroup.cpu_limited}}): the cgroup files could not be read, or no hierarchy carries the cpu controller.
:::end

:::when mem.cgroup.version is v1
This machine uses cgroup v1, so the quota above comes from `cpu.cfs_quota_us` and `cpu.cfs_period_us` and the group's weight is `cpu.shares` ({{obs:sched.cgroup.cpu.shares_v1}}, where 1024 is the default) rather than `cpu.weight`.
:::else
:::when sched.cgroup.cpu.weight
Under cgroup v2, the group's weight among its siblings is `cpu.weight`, here {{obs:sched.cgroup.cpu.weight}} (100 is the default).
:::unknown
Under cgroup v2 the group's weight is `cpu.weight`, which atlas's cgroup does not have here ({{obs:sched.cgroup.cpu.weight}}): without the cpu controller, its tasks compete inside the parent group.
:::end
:::end

Since the cgroup was created, its tasks have used {{obs:sched.cgroup.cpu.usage_us}} of CPU time.

### Where atlas may run

:::evidence Placement
- cpu.logical_count
- sched.self.cpus_allowed_list
- derived.sched.self_allowed_count
- sched.cgroup.cpuset.cpus_effective
- cpu.isolated | Isolated CPUs (blank when none)
- derived.cpu.isolated_count
- cpu.nohz_full | Tickless CPUs (blank when none)
:::end

:::when sched.cgroup.cpuset.cpus_effective
The cpuset controller confines atlas's cgroup to CPUs {{obs:sched.cgroup.cpuset.cpus_effective}}, and atlas's own affinity is {{obs:sched.self.cpus_allowed_list}}: {{derived:derived.sched.self_allowed_count}} CPUs. `nproc` here would print that number, whatever the quota allows.
:::unknown
Atlas may run on CPUs {{obs:sched.self.cpus_allowed_list}}, {{derived:derived.sched.self_allowed_count}} in all, and its cgroup has no CPU set of its own ({{obs:sched.cgroup.cpuset.cpus_effective}}). `nproc` here would print that number, whatever any quota allows.
:::end

:::when derived.cpu.isolated_count
{{derived:derived.cpu.isolated_count}} CPUs here are isolated at boot ({{obs:cpu.isolated}}): the scheduler balances no ordinary task onto them, which is why atlas's affinity leaves them out. CPUs {{obs:cpu.nohz_full}} also run without the scheduler tick whenever one task has them to itself.
:::else
No CPU is isolated here: `/sys/devices/system/cpu/isolated` is empty, so the scheduler may balance work onto every online CPU.
:::unknown
Whether any CPU is isolated could not be read here ({{obs:cpu.isolated}}).
:::end

:::when derived.env.kind is container
Atlas appears to run in a container. `/proc/cpuinfo`, `/sys/devices/system/cpu` and `/proc/stat` inside it describe the host, so the only place its CPU limit shows is the cgroup above.
:::else
No container was detected here. Inside one, `/proc/cpuinfo`, `/sys/devices/system/cpu` and `/proc/stat` would still describe the host, and the cgroup would be the only place its CPU limit shows.
:::unknown
Whether atlas runs in a container could not be determined here. Inside one, `/proc/cpuinfo`, `/sys/devices/system/cpu` and `/proc/stat` describe the host, and the cgroup is the only place its CPU limit shows.
:::end

## Predict

:::predict
A container has `cpu.max` set to `50000 100000` (half a CPU) on a machine with 8 idle CPUs. A program in it receives a request every 100 ms, handled by 8 threads that each need 10 ms of CPU. How long does each request take?

- About 160 ms: 80 ms of work at half a CPU.
- About 10 ms: there are 8 idle CPUs, so all threads run at once.
- Usually far longer than 10 ms: the threads use the whole quota in about 6 ms, then wait for the next period, and the backlog keeps growing.
:::reveal
The third. Eight threads on eight CPUs consume the 50 ms quota after about 6.25 ms of wall time, having done 50 of the 80 ms of work; the group is throttled until the period ends, and the rest runs at the start of the next period. Each request takes most of two periods instead of 10 ms. Worse, 80 ms of work arrives every period while only 50 ms can be done, so requests queue without limit: a quota caps throughput as well as adding latency.

The first answer treats the quota as a slower CPU; it is a budget that can be spent fast and then runs out. The second ignores the quota altogether, which is what the threads see from inside: eight CPUs in `/proc/cpuinfo`, eight in `nproc`.
:::end

## Try it

Read the CPU controls of the current shell's cgroup, read-only:

```sh
cg=/sys/fs/cgroup$(cut -d: -f3 /proc/self/cgroup | tail -n1)
cat "$cg/cpu.max" "$cg/cpu.weight" "$cg/cpu.stat" 2>&1
taskset -pc $$
nproc
cat /sys/devices/system/cpu/isolated
```

On cgroup v2 the first line finds the shell's own cgroup; missing files mean the cpu controller is not enabled for it. Read `cpu.stat` twice a few seconds apart under load to see `nr_throttled` grow. Inside a container with a quota, compare `nproc` with the quota: they disagree. To see throttling without a container, systemd can start a shell with a quota of its own, `systemd-run --user --pty -p CPUQuota=50% bash`, where the user session is allowed to delegate the cpu controller; a busy loop inside then shows 50 percent in `top`.

## Limits

:::misconception nproc shows how much CPU a container can use
`nproc` counts the CPUs in the affinity mask. A quota, the usual way containers are limited, does not change the mask, so a container limited to half a CPU still sees every CPU in `nproc`, `/proc/cpuinfo` and `/sys`.
:::end

:::misconception A quota of 1.5 CPUs means running on one and a half CPUs
It means 1.5 CPUs' worth of time per period, spent on any number of CPUs. Eight threads can run on eight CPUs at once and use it up in a fraction of the period, then wait.
:::end

:::misconception An isolated CPU is switched off
It runs whatever is placed on it explicitly; the scheduler just never moves ordinary work there. Left alone, it idles, and the machine has fewer CPUs for everything else.
:::end

:::cannot
These figures describe atlas's own cgroup only. Ancestor cgroups can impose tighter quotas and lower weights, sibling groups compete with it, and none of them are read here.

Throttling counts say how often the pool ran out, not when, for how long each time, or which requests it delayed: a throttled share that looks small since creation can hide bursts of long pauses.

Weights matter only under contention, which these figures cannot show: a cgroup with a low weight on an idle machine runs as fast as any other.
:::end

## Go deeper

This is the last lesson of the CPU and scheduler track. In Explore, the [CPU cgroup controllers](component:cpu-cgroup) and the [fair scheduler](component:fair-scheduler) show how groups and tasks fit together. The planned memory cgroups lesson covers the other half of a container's limits, namespaces and isolation what a container sees, and real-time scheduling the classes that run above every quota.
