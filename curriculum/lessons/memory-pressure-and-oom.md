---
{
  "id": "memory-pressure-and-oom",
  "title": "When reclaim is not enough",
  "subsystem": "memory",
  "kind": "core",
  "order": 9,
  "summary": "Overcommit, the path from a failing allocation to the OOM killer, how the victim is chosen, how memory cgroups move the same decision inside a group, and why user-space daemons now act before the kernel does.",
  "prerequisites": ["reclaim-and-swap"],
  "uses": [
    "derived.cgroup.memory_limited",
    "derived.commit.over_limit",
    "derived.env.kind",
    "derived.oom.any",
    "derived.oom.kills_since_boot",
    "derived.overcommit.mode_name",
    "derived.psi.available",
    "derived.swap.configured",
    "derived.swap.total_bytes",
    "kernel.uptime_seconds",
    "mem.cgroup.events.high",
    "mem.cgroup.events.max",
    "mem.cgroup.events.oom",
    "mem.cgroup.events.oom_kill",
    "mem.cgroup.memory.current",
    "mem.cgroup.memory.high",
    "mem.cgroup.memory.max",
    "mem.cgroup.memory.swap_max",
    "mem.cgroup.version",
    "mem.meminfo.CommitLimit",
    "mem.meminfo.Committed_AS",
    "mem.psi.full.avg300",
    "mem.sysctl.vm.overcommit_memory",
    "mem.sysctl.vm.overcommit_ratio",
    "mem.vmstat.oom_kill"
  ],
  "relevance": [
    {"when": "derived.oom.any", "is": true, "note": "The OOM killer has acted on this machine since boot."},
    {"when": "derived.cgroup.memory_limited", "is": true, "note": "Atlas runs under a cgroup memory limit, which has an OOM killer of its own."}
  ],
  "next": [],
  "deeper": ["planned:memory-cgroups", "planned:processes-and-threads", "planned:init-systems-and-services"],
  "version_notes": [
    {"applies": "Linux ≥ 4.6", "text": "The OOM reaper: a kernel thread that frees a victim's anonymous memory without waiting for the victim to exit, so a victim stuck in the kernel cannot hold the system hostage."},
    {"applies": "Linux ≥ 4.13", "text": "/proc/vmstat counts OOM kills (oom_kill), cgroup and system-wide alike, and cgroup v2 memory.events gained oom_kill. Before, the kernel log was the only record."},
    {"applies": "Linux ≥ 4.19", "text": "memory.oom.group lets a cgroup v2 group be killed as a whole, so a workload is not left half-alive."},
    {"applies": "Linux ≥ 4.20", "text": "Pressure stall information, which user-space OOM daemons such as systemd-oomd use to act before the kernel does."},
    {"applies": "cgroup v1", "text": "Memory limits are set in memory.limit_in_bytes and OOM handling in memory.oom_control, in a separate memory hierarchy; there is no memory.high or memory.events. Machine Atlas reads only the v2 interface."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Overcommit accounting", "url": "https://docs.kernel.org/mm/overcommit-accounting.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: /proc/sys/vm (overcommit_memory, overcommit_ratio, oom_kill_allocating_task, panic_on_oom)", "url": "https://docs.kernel.org/admin-guide/sysctl/vm.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Control Group v2, Memory", "url": "https://docs.kernel.org/admin-guide/cgroup-v2.html#memory", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: PSI, Pressure Stall Information", "url": "https://docs.kernel.org/accounting/psi.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: The /proc filesystem (oom_score, oom_score_adj)", "url": "https://docs.kernel.org/filesystems/proc.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Concepts overview, OOM killer", "url": "https://docs.kernel.org/admin-guide/mm/concepts.html#oom-killer", "kind": "kernel-doc"},
    {"title": "proc_pid_oom_score(5)", "url": "https://man7.org/linux/man-pages/man5/proc_pid_oom_score.5.html", "kind": "man-page"},
    {"title": "systemd-oomd.service(8)", "url": "https://man7.org/linux/man-pages/man8/systemd-oomd.service.8.html", "kind": "man-page"},
    {"title": "Kernel source: oom_badness() in mm/oom_kill.c", "url": "https://github.com/torvalds/linux/blob/master/mm/oom_kill.c", "kind": "source"},
    {"title": "LWN: Toward more predictable and reliable out-of-memory handling (2015)", "url": "https://lwn.net/Articles/668126/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.oom.any
Since it booted, {{obs:kernel.uptime_seconds}} ago, this kernel has killed {{derived:derived.oom.kills_since_boot}} processes because it could not find memory for an allocation. No program asked for that, and the processes killed were not necessarily the ones whose allocation failed. Who chose them, and why was killing the only option left?
:::else
Since it booted, {{obs:kernel.uptime_seconds}} ago, this kernel has not killed a single process for lack of memory. Yet it hands out address space far more freely than it has RAM, and on another day the same kernel could kill a process within seconds. What would have to go wrong, and who would choose the victim?
:::unknown
This kernel does not report how many processes it has killed for lack of memory ({{obs:mem.vmstat.oom_kill}}); before Linux 4.13 only the kernel log recorded it. Every Linux kernel can do it, though: it hands out address space far more freely than it has RAM. What would have to go wrong for it to kill a process, and who would choose the victim?
:::end

## Why it exists

Lessons 2 and 3 showed the kernel granting address space as a promise: `mmap` succeeds at once and RAM is supplied later, page by page, when touched. Most promises are never called in full, which is what lets a machine run far more programs than a strict count of RAM would allow. But nothing guarantees it. If enough processes touch enough of what they were promised at the same time, and reclaim, the subject of the last lesson, has nothing left to take, an allocation arrives that cannot be satisfied.

Many allocations cannot simply fail. A page fault has no error return: the faulting instruction cannot proceed without its page. Much of the kernel allocates in places where failure cannot be handled. So the kernel has two choices: refuse promises up front, or, when one is called in and cannot be kept, free memory by force. Linux offers the first as an option and relies on the second.

## How it works

### Overcommit: how much is promised

The kernel keeps a running total, `Committed_AS`, of memory it has promised that would need RAM or swap if fully touched: private writable mappings and shared memory, but not read-only or shared file mappings, which can always be reread. `vm.overcommit_memory` decides what that total is allowed to do:

- **0, heuristic** (the default). A request is refused only if it alone is larger than RAM plus swap. Everything else is granted; the total may exceed anything.
- **1, always.** Every request is granted. Useful for programs that map huge sparse arrays and touch little of them.
- **2, strict.** The total may not exceed `CommitLimit`, which is swap plus `overcommit_ratio` percent of RAM (50 by default; `overcommit_kbytes` can set an absolute amount). A request beyond it fails at once: `mmap` returns `ENOMEM` and `malloc` returns NULL, where the program can still handle it.

Mode 2 moves failure from touch time to request time, but it cannot make the kernel's own allocations safe, and programs that reserve much more than they use (language runtimes, browsers) may refuse to start under it. That is why it is rare outside specialised systems.

### The path to the OOM killer

An allocation that finds its zone below the low watermark wakes kswapd and retries against the min watermark. If that fails it does direct reclaim. If the request is for several contiguous pages it also tries compaction, moving pages to assemble a free block. If reclaim made progress it retries, and it keeps retrying while there is a realistic chance: the kernel gives up after many rounds without progress. Only then does it call `out_of_memory()`.

Not every allocation goes that far. Requests for more than eight contiguous pages, and requests marked as allowed to fail, return failure to their caller instead, and allocations made while holding filesystem locks retry rather than kill. The OOM killer is reserved for the case where an allocation of a few pages must succeed and nothing else is left.

### Choosing the victim

`out_of_memory()` scores every eligible process with `oom_badness()`: its resident pages, plus its pages in swap, plus its page tables, counted in pages. To that it adds `oom_score_adj` (from −1000 to +1000) times one thousandth of the memory available, so +500 counts as half of all memory more. A score of −1000 exempts a process entirely; services set it to protect themselves, and every process can raise its own. `/proc/<pid>/oom_score` shows the result, scaled to 0–2000.

The process with the highest score is sent `SIGKILL`, along with any other process sharing its address space. A process killed this way might be stuck in the kernel waiting for a lock the allocator also needs, so since Linux 4.6 the *OOM reaper* thread frees the victim's anonymous memory straight away, without waiting for the victim to finish dying. Two sysctls change the policy: `oom_kill_allocating_task` kills whichever task triggered the condition instead of the biggest, and `panic_on_oom` panics the machine, which clusters use to fail over quickly.

### Memory cgroups: the same decision, inside a group

A memory cgroup charges each page to the group whose process caused it: anonymous memory, page cache from its reads and writes, and kernel memory on its behalf. Under cgroup v2, `memory.high` is a throttle: above it, the group's tasks are slowed and pushed into reclaim of the group's own pages, but nothing is killed. `memory.max` is a wall: an allocation that would cross it reclaims within the group, and if that fails, the OOM killer runs with only the group's processes as candidates. `memory.events` counts each of these events.

So a container can be OOM-killed while the host has gigabytes free: its limit, not the machine's RAM, ran out. Its MemTotal and MemAvailable still show the host's figures, which is why programs that size themselves from meminfo misjudge memory inside containers. Under cgroup v1 the same limit is `memory.limit_in_bytes`, in a separate memory hierarchy.

### Killing earlier, from user space

The kernel's OOM killer is designed as a last resort, and the condition it waits for is "no progress", not "unusable". As [reclaim and swap](lesson:reclaim-and-swap) showed, a machine can thrash for minutes while reclaim still makes progress: it is unresponsive but, by that definition, not out of memory. User-space daemons act earlier. `systemd-oomd` watches pressure stall information and swap use per cgroup, and when a configured limit is exceeded for long enough it kills a whole cgroup, typically one application or session. `earlyoom` does something similar from MemAvailable and free swap. Both choose earlier and more coarsely, killing a unit rather than the largest process, and the kernel's killer remains behind them as a backstop. The systemd-oomd documentation recommends swap, because swapping slows the descent enough for the daemon to react; without it, a machine can go from fine to livelocked too quickly.

## On this machine

:::evidence OOM kills
- mem.vmstat.oom_kill
- derived.oom.kills_since_boot
- derived.oom.any
- kernel.uptime_seconds
:::end

:::evidence Overcommit
- mem.sysctl.vm.overcommit_memory
- derived.overcommit.mode_name
- mem.sysctl.vm.overcommit_ratio
- mem.meminfo.CommitLimit
- mem.meminfo.Committed_AS
- derived.commit.over_limit
:::end

:::when derived.overcommit.mode_name is heuristic
Overcommit here is in the default heuristic mode. The kernel has promised {{obs:mem.meminfo.Committed_AS}} of private memory and would refuse only a single request larger than RAM plus swap. `CommitLimit`, {{obs:mem.meminfo.CommitLimit}}, is computed but not enforced: it would matter only in mode 2.
:::else
:::when derived.overcommit.mode_name is strict
Overcommit here is strict: the kernel has promised {{obs:mem.meminfo.Committed_AS}} against a limit of {{obs:mem.meminfo.CommitLimit}}, and refuses any request that would take the total further. Programs on this machine see `malloc` fail rather than being killed later, unless the kernel itself runs short.
:::else
Overcommit here is set to always: every request for address space is granted. The {{obs:mem.meminfo.Committed_AS}} promised is bounded by nothing, and running short is handled entirely by reclaim and the OOM killer.
:::end
:::end

### Atlas's cgroup

:::evidence The cgroup Atlas ran in
- mem.cgroup.version
- mem.cgroup.memory.max
- mem.cgroup.memory.high
- mem.cgroup.memory.current
- mem.cgroup.memory.swap_max
- mem.cgroup.events.high
- mem.cgroup.events.max
- mem.cgroup.events.oom
- mem.cgroup.events.oom_kill
- derived.cgroup.memory_limited
- derived.env.kind
:::end

:::when derived.cgroup.memory_limited
Atlas ran in a cgroup with a limit of its own (`memory.max` {{obs:mem.cgroup.memory.max}}, `memory.high` {{obs:mem.cgroup.memory.high}}), currently charged {{obs:mem.cgroup.memory.current}}. For processes in it, that limit is the memory that can run out; the machine's MemAvailable is not. Since the cgroup was created, usage has hit `memory.high` {{obs:mem.cgroup.events.high}} times and `memory.max` {{obs:mem.cgroup.events.max}} times; reclaim failed and the OOM killer was invoked for a limit in the group {{obs:mem.cgroup.events.oom}} times, and {{obs:mem.cgroup.events.oom_kill}} of its processes were killed by an OOM killer of any kind.
:::else
Atlas ran in a cgroup with no memory limit of its own (`memory.max` {{obs:mem.cgroup.memory.max}}), so for it the limit is the machine's memory, unless an ancestor cgroup imposes one that Machine Atlas does not read.

:::when mem.cgroup.events.oom_kill
Even so, its `oom_kill` count is {{obs:mem.cgroup.events.oom_kill}}: processes in this group have been killed by an OOM killer since the group was created, while limits inside it were reached {{obs:mem.cgroup.events.oom}} times. With no limit of its own, those kills were made by the system-wide OOM killer or on behalf of an ancestor's limit, unless a nonzero `oom` count shows that a limit set on a group below this one was reached.
:::else
No process in it has been killed by an OOM killer of any kind since the group was created ({{obs:mem.cgroup.events.oom_kill}}); kills elsewhere on the machine appear only in the system-wide counter.
:::unknown
Its OOM counters could not be read ({{obs:mem.cgroup.events.oom_kill}}).
:::end
:::unknown
:::when mem.cgroup.version is v1
This system uses cgroup {{obs:mem.cgroup.version}}, whose memory limits live in a separate hierarchy (`memory.limit_in_bytes`) that Machine Atlas does not read. Whether Atlas ran under a limit, and how often it was reached, is not known from here.
:::else
Whether Atlas ran under a cgroup memory limit could not be determined: the cgroup files were unreadable ({{obs:mem.cgroup.memory.max}}).
:::end
:::end

The two OOM fields of `memory.events` answer different questions. `oom` counts the times this group, or a group below it, reached its own `memory.max`, reclaim failed and the OOM killer was invoked for that limit. `oom_kill` counts this group's processes killed by any OOM killer, the system-wide one and one acting for an ancestor's limit included. So `oom_kill` above zero with `oom` at zero means the group lost processes to a shortage outside it. Otherwise the two do not pair off exactly: one `oom` can kill several processes where `memory.oom.group` is set, or none, if a process exits on its own first.

:::when derived.env.kind is container
Machine Atlas appears to run inside a container. The meminfo figures it reads, MemTotal included, are the host's, so the cgroup above is the only accounting that describes this environment.
:::unknown
Whether Machine Atlas runs inside a container could not be determined. If it does, the meminfo figures are the host's, and only the cgroup above describes this environment.
:::end

:::when derived.psi.available
Over the last five minutes, all non-idle tasks were stalled on memory at once for {{obs:mem.psi.full.avg300}} of the time: the signal `systemd-oomd` acts on. Near zero means nothing is being starved now, whatever the kill counter says about the past.
:::else
Pressure stall information cannot be read here ({{obs:mem.psi.full.avg300}}), so a PSI-based daemon such as `systemd-oomd` would have nothing to act on.
:::end

:::when derived.cgroup.memory_limited
For processes in Atlas's cgroup, the road to an OOM kill runs inside the group, whatever the machine's free memory: above `memory.high` its tasks are throttled and made to reclaim the group's own pages, and at `memory.max` an allocation reclaims within the group and, if that fails, the OOM killer chooses among the group's processes. Whether anonymous memory can leave RAM on that road depends on two things: whether the machine has swap ({{derived:derived.swap.total_bytes}} here) and the group's own swap limit, `memory.swap.max` {{obs:mem.cgroup.memory.swap_max}}. If either is missing, reclaim can take only the group's file pages, and the group can thrash on them for a while before it is killed.
:::else
:::when derived.swap.configured
How this machine would approach an OOM kill depends on its {{derived:derived.swap.total_bytes}} of swap. As memory fills, reclaim writes cold anonymous pages out and keeps reading back the ones still in use: swap-ins climb, pressure rises, programs slow down, and the slide is gradual enough for a daemon or a person to notice. The kernel's killer acts only once swap is full or cannot be written fast enough to make progress.
:::else
With no swap ({{derived:derived.swap.total_bytes}}), this machine would approach an OOM kill differently. Anonymous memory cannot leave RAM, so as it grows the page cache, executable code included, is squeezed out and reread over and over; the machine can become unresponsive for minutes while the kernel still sees reclaim making progress. Only when no file pages are left to drop does the OOM killer act, which is one reason a PSI-based daemon is worth having on such a system.
:::end
:::unknown
Whether a cgroup limit applies to Atlas could not be determined here ({{derived:derived.cgroup.memory_limited}}). If one does, the OOM killer acts at that limit, among the group's processes. If not, the approach is machine-wide and depends on swap, {{derived:derived.swap.total_bytes}} here: with swap the slide is gradual, as cold anonymous pages are written out and those still in use read back; without it the page cache, program code included, is squeezed until the machine thrashes on rereading it, possibly for minutes before anything is killed.
:::end

## Predict

:::predict
Compare the two figures: the kernel has promised {{obs:mem.meminfo.Committed_AS}} of memory, and CommitLimit is {{obs:mem.meminfo.CommitLimit}}. Does that mean the next large allocation on this machine will fail?

- Yes if Committed_AS is above CommitLimit: the machine is over its limit.
- No, unless overcommit is in strict mode; in the default mode the comparison has no effect.
- No, because allocations never fail on Linux; processes are killed instead.
:::reveal
:::when derived.commit.over_limit
Here Committed_AS, {{obs:mem.meminfo.Committed_AS}}, is above CommitLimit, {{obs:mem.meminfo.CommitLimit}}.
:::else
Here Committed_AS, {{obs:mem.meminfo.Committed_AS}}, is below CommitLimit, {{obs:mem.meminfo.CommitLimit}}.
:::end

Either way, the comparison decides nothing unless `overcommit_memory` is 2, and here it is {{obs:mem.sysctl.vm.overcommit_memory}} ({{derived:derived.overcommit.mode_name}}). In the default mode CommitLimit is only reported; a desktop running a few browsers and language runtimes routinely has promised more than CommitLimit, sometimes more than RAM plus swap, because most reserved memory is never touched. In strict mode the first answer would be right: the request that takes the total past CommitLimit fails with `ENOMEM`.

The third answer is wrong too. Allocations do fail: in strict mode, for single requests larger than RAM plus swap even in mode 0, under an address-space limit such as `ulimit -v`, and inside the kernel for large contiguous requests. What does not fail is the touch of memory already granted: that is where the OOM killer comes in.
:::end

## Try it

These commands only read. Machine Atlas never induces memory pressure or an OOM kill, and you should not test one on a machine you care about: the victim is chosen by the kernel, not by you.

```sh
cat /proc/$$/oom_score /proc/$$/oom_score_adj
cat /sys/fs/cgroup$(cut -d: -f3 /proc/self/cgroup | head -n1)/memory.events
grep -E '^(CommitLimit|Committed_AS)' /proc/meminfo
journalctl -k -g 'Out of memory|oom-kill' --no-pager
```

The first prints the shell's score (small for a shell) and its adjustment (usually 0, or higher if a service manager set one). The second, on cgroup v2, prints your session's `memory.events`; `oom_kill` is how many of its processes an OOM killer of any kind has killed, and `oom` how often a limit within it was reached. The last searches the kernel log for past kills with the victim's name and memory; on many systems reading the kernel log needs root or membership in a log-reading group, and `dmesg` may be restricted (`kernel.dmesg_restrict`).

## Limits

:::misconception The OOM killer kills the process that asked for memory
By default it kills the process with the highest badness score, usually the largest by resident memory plus swap, adjusted by oom_score_adj. The allocating task is killed only if it is that process, or if `oom_kill_allocating_task` is set.
:::end

:::misconception malloc returns NULL when memory runs out
Under the default heuristic mode, `malloc` almost always succeeds, because it only reserves address space. The shortage shows up later, when the memory is touched, as a page fault the kernel cannot satisfy, and the answer is a kill, not an error code. NULL is the normal outcome only in strict mode, or under an address-space limit such as `ulimit -v`.
:::end

:::misconception OOM means swap ran out
Swap is only one thing that can run out. A cgroup limit, a kernel allocation that needs contiguous memory, or a machine without swap at all can each lead to an OOM kill while swap is empty or absent. Conversely a machine with swap left can thrash for a long time without the kernel declaring OOM.
:::end

:::cannot
The `oom_kill` counter says how many processes were killed since boot, not which ones, when, or why. That is in the kernel log, which is often readable only by root.

The counter includes kills made because a memory cgroup reached its limit as well as system-wide exhaustion, so by itself it cannot tell a container hitting its `memory.max` from a machine running out of RAM. A cgroup's `memory.events` narrows it down for that group: its `oom_kill` counts the group's processes killed by any OOM killer, system-wide included, while `oom` counts how often the group, or one below it, hit its own limit. Kills beyond that came from outside the group, but which kill was which is again only in the kernel log.

The cgroup figures describe only the cgroup Atlas ran in. Other services, sessions and containers have their own limits and counters, which an unprivileged process can often read but Machine Atlas does not collect. An ancestor cgroup's limit can be tighter than the one shown.

Overcommit figures describe promises, not use: neither Committed_AS nor any other counter can say how much of what was promised will be touched.
:::end

## Go deeper

This is the last lesson of the memory journey. In Explore, the [OOM killer](component:oom-killer) and [memory cgroups](component:memory-cgroup) components connect it to reclaim and to the rest of the system. The planned lesson on memory cgroups covers per-group reclaim, protection with `memory.low` and `memory.min`, and swap limits. On the roadmap, the processes card (processes, threads, signals) and the boot and init card (how services are supervised and given limits) continue from here.
