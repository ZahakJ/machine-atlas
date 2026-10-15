---
{
  "id": "fair-scheduling",
  "title": "Who runs next, and for how long",
  "subsystem": "cpu",
  "kind": "core",
  "order": 4,
  "summary": "Scheduling classes and policies, weights from nice values, virtual runtime, CFS's smallest-vruntime rule and EEVDF's lag, eligibility and virtual deadlines, preemption and context switches, autogroup, real-time throttling and sched_ext.",
  "prerequisites": ["run-queues-and-load"],
  "uses": [
    "derived.sched.ctxt_per_second",
    "derived.sched.ext_enabled",
    "derived.sched.fair_scheduler",
    "derived.sched.self_weight",
    "kernel.release",
    "sched.ext.state",
    "sched.self.load_weight",
    "sched.self.nice",
    "sched.self.nonvoluntary_ctxt_switches",
    "sched.self.policy",
    "sched.self.run_us",
    "sched.self.slice_us",
    "sched.self.timeslices",
    "sched.self.voluntary_ctxt_switches",
    "sched.self.wait_us",
    "sched.stat.ctxt",
    "sched.sysctl.kernel.sched_autogroup_enabled",
    "sched.sysctl.kernel.sched_rr_timeslice_ms",
    "sched.sysctl.kernel.sched_rt_period_us",
    "sched.sysctl.kernel.sched_rt_runtime_us"
  ],
  "relevance": [
    {"when": "derived.sched.fair_scheduler", "is": "cfs", "note": "This kernel predates EEVDF and schedules ordinary tasks with CFS, the smallest-vruntime rule."},
    {"when": "derived.sched.ext_enabled", "is": true, "note": "A sched_ext BPF scheduler is running here, so the fair scheduler's policy may not be the one in charge."}
  ],
  "next": ["cpu-limits-and-isolation"],
  "deeper": ["planned:real-time-scheduling", "planned:sched-ext", "planned:load-balancing-and-sched-domains"],
  "version_notes": [
    {"applies": "Linux 2.6.23", "text": "The Completely Fair Scheduler replaced the O(1) scheduler: per-task virtual runtime in a red-black tree, the task with the smallest one running next."},
    {"applies": "Linux ≥ 2.6.38", "text": "Autogroup: tasks of each session (each setsid(), typically each terminal) form a group that shares CPU time with other sessions before its tasks share it among themselves."},
    {"applies": "Linux ≥ 6.6", "text": "EEVDF replaced CFS's pick: the eligible task with the earliest virtual deadline runs. The sched_latency and min_granularity tunables gave way to one base slice."},
    {"applies": "Linux ≥ 6.12", "text": "sched_ext lets a BPF program implement the scheduling policy, and a task may request its own slice length through sched_setattr(). Real-time throttling is enforced by a deadline server that reserves time for fair tasks."}
  ],
  "references": [
    {"title": "Linux kernel documentation: EEVDF Scheduler", "url": "https://docs.kernel.org/scheduler/sched-eevdf.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: CFS Scheduler", "url": "https://docs.kernel.org/scheduler/sched-design-CFS.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Extensible Scheduler Class (sched_ext)", "url": "https://docs.kernel.org/scheduler/sched-ext.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Real-Time group scheduling (sched_rt_runtime_us)", "url": "https://docs.kernel.org/scheduler/sched-rt-group.html", "kind": "kernel-doc"},
    {"title": "sched(7): scheduling policies, nice values and autogroup", "url": "https://man7.org/linux/man-pages/man7/sched.7.html", "kind": "man-page"},
    {"title": "sched_setattr(2)", "url": "https://man7.org/linux/man-pages/man2/sched_setattr.2.html", "kind": "man-page"},
    {"title": "Kernel source: kernel/sched/fair.c (pick_eevdf, update_curr, place_entity)", "url": "https://github.com/torvalds/linux/blob/master/kernel/sched/fair.c", "kind": "source"},
    {"title": "Kernel source: kernel/sched/core.c (sched_prio_to_weight)", "url": "https://github.com/torvalds/linux/blob/master/kernel/sched/core.c", "kind": "source"},
    {"title": "LWN: An EEVDF CPU scheduler for Linux (2023)", "url": "https://lwn.net/Articles/925371/", "kind": "lwn"}
  ]
}
---
## Question

:::when sched.stat.ctxt
Since it booted, this machine has switched its CPUs from one task to another {{obs:sched.stat.ctxt}} times, about {{derived:derived.sched.ctxt_per_second}} times a second on average. Each switch was a decision: of all the tasks ready to run on that CPU, this one, now, for this long. Atlas itself was switched out {{obs:sched.self.voluntary_ctxt_switches}} times because it chose to wait and {{obs:sched.self.nonvoluntary_ctxt_switches}} times because the scheduler took the CPU away. What rule makes those decisions, and what does "fair" mean when tasks have different priorities?
:::unknown
A busy Linux machine switches its CPUs from one task to another thousands of times a second; how many times this one has could not be read here ({{obs:sched.stat.ctxt}}). Each switch is a decision: of all the tasks ready to run on that CPU, this one, now, for this long. What rule makes those decisions, and what does "fair" mean when tasks have different priorities?
:::end

## Why it exists

Run queues hold the tasks waiting for a CPU; something must choose among them, many times a second, on every CPU. The choice has competing goals. A compiler wants long uninterrupted runs, because every switch costs cache contents. An editor wants to run within milliseconds of a key press, though it needs the CPU only briefly. A user who lowers a background job's priority expects it to yield, not to stop. And whatever happens, no runnable task may wait forever.

Linux's answer for ordinary tasks is proportional sharing: each runnable task gets a share of the CPU in proportion to a weight, and the scheduler keeps the actual shares close to those proportions at every moment, not just on average. The other goals, latency and starvation, are met by how it orders the turns.

## How it works

### Classes and policies

Every task has a *scheduling policy*, and each policy belongs to a *class*. Classes are strictly ordered: a CPU runs a task of a lower class only when no higher class has one ready.

| class | policies | rule |
|---|---|---|
| deadline | `SCHED_DEADLINE` | earliest deadline first, with admission control |
| real-time | `SCHED_FIFO`, `SCHED_RR` | fixed priorities 1 to 99; FIFO runs until it blocks, RR rotates within a priority every `sched_rr_timeslice_ms` |
| fair | `SCHED_OTHER`, `SCHED_BATCH`, `SCHED_IDLE` | weighted fair sharing, this lesson |
| ext | `SCHED_EXT` | a BPF program's policy (Linux 6.12 and later) |
| idle | (the idle task) | runs when nothing else can |

A real-time task that never blocks would starve everything below it, so by default real-time tasks together may use only `sched_rt_runtime_us` out of every `sched_rt_period_us` (950 000 of 1 000 000 µs, 95 percent), leaving the rest to everyone else.

### Weights

Each fair task has a nice value from −20 to 19, and the kernel turns it into a *weight* with a fixed table, `sched_prio_to_weight`: nice 0 is 1024, and each step changes the weight by a factor of about 1.25. The factor is chosen so that one nice step moves about 10 percent of a CPU between two competing tasks: at nice 0 and nice 1 they weigh 1024 and 820, and share the CPU 55.5 to 44.5. Five steps apart, 1024 against 335, the split is about 75 to 25. A task's share is its weight divided by the sum of the weights of all runnable tasks; `SCHED_IDLE` tasks weigh 3.

### Virtual runtime

To enforce the shares, the scheduler gives every task a *virtual runtime* (vruntime), a clock that advances only while the task runs, and runs slower the heavier the task: running for *t* adds *t* × 1024 ÷ weight. A nice-0 task's vruntime advances in real time; a task four times as heavy advances at a quarter of the speed. If every task's vruntime advances at the same rate, every task receives CPU in proportion to its weight, so the scheduler's job reduces to keeping the vruntimes level.

### CFS: run the one furthest behind

From Linux 2.6.23 to 6.5 the Completely Fair Scheduler kept each run queue's tasks in a red-black tree ordered by vruntime and ran the leftmost, the task furthest behind. It let that task run for its weighted share of a *scheduling period*, about 6 ms multiplied by a factor that grows with the number of CPUs, then put it back in the tree. New tasks started a little behind the slowest existing one, and a task waking from sleep was given a bounded amount of credit, at most half a period, so that interactive tasks ran promptly without saving up the time they slept.

CFS was fair in the long run, but its latency was hard to reason about: how soon a task ran depended on heuristics for sleepers and wake-ups that piled up over fifteen years.

### EEVDF: eligible, then earliest deadline

Since Linux 6.6 the fair class uses *Earliest Eligible Virtual Deadline First*, from a 1995 paper by Stoica and Abdel-Wahab. It adds two ideas to vruntime.

*Lag* measures fairness exactly. Let V be the weighted average of the vruntimes of the runnable tasks: where every task would be if the CPU had been shared perfectly. A task's lag is its weight times (V − its vruntime): positive if it has received less than its share, negative if more. The lags of all runnable tasks sum to zero. A task is *eligible* when its lag is not negative, that is when its vruntime is at or below V.

*Virtual deadlines* order the eligible tasks. Each task asks for a *slice*, by default the base slice: about three-quarters of a millisecond, multiplied by 1 + log2 of the number of CPUs, counting at most 8. Its deadline is its vruntime plus the slice in virtual time, slice × 1024 ÷ weight. The scheduler runs the eligible task with the earliest deadline, and lets it run until it has used its slice. A task with a shorter slice gets an earlier deadline and so runs sooner, but more often for less time: latency can be traded against throughput without changing anyone's share. Since Linux 6.12 a task can request its own slice through `sched_setattr()`.

Tasks join with zero lag: a new task neither owes nor is owed anything. A task that sleeps leaves with its lag and comes back with it, so sleeping earns no credit and running ahead is not forgiven by taking a nap.

### Preemption and context switches

The scheduler reconsiders at every *tick* (the periodic timer, typically every 1 to 4 ms), whenever a task wakes up, and whenever the running task blocks. If the running task has used its slice, or a woken task should run first, the running task is *preempted*: an involuntary, or *nonvoluntary*, context switch. If the running task blocks on I/O, a lock or a timer, it gives the CPU up itself: a *voluntary* switch.

A context switch saves one task's registers and loads another's, switches the page-table root if the new task belongs to another process (tagged TLB entries, PCID on x86, avoid flushing them), and resumes. That takes a few microseconds, but the indirect cost is larger: the new task finds the caches full of the old task's data.

### Groups

Weights also apply to groups. With *autogroup*, each session, typically each terminal, is a group: two sessions share the CPU equally first, however many tasks each runs, so a parallel build in one terminal does not slow another's shell much. Autogroup affects only tasks outside cgroups with their own CPU settings; on systemd machines cgroup weights, the subject of the next lesson, do the same job per service and per user.

### sched_ext

Since Linux 6.12 a BPF program can take over scheduling policy through the ext class: either for every task the fair class would schedule, or only for tasks that ask for `SCHED_EXT`. The kernel keeps the run queues and enforces safety: if the program misbehaves or stalls a task, the kernel unloads it and the fair scheduler takes over again.

## On this machine

:::evidence The scheduler here
- kernel.release
- derived.sched.fair_scheduler
- sched.ext.state
- derived.sched.ext_enabled
- sched.sysctl.kernel.sched_autogroup_enabled
- sched.sysctl.kernel.sched_rt_runtime_us
- sched.sysctl.kernel.sched_rt_period_us
- sched.sysctl.kernel.sched_rr_timeslice_ms
:::end

:::when derived.sched.fair_scheduler is eevdf
Ordinary tasks here are scheduled by EEVDF: of the eligible tasks, the one with the earliest virtual deadline runs. Machine Atlas infers this (the rule is in the inspector) from the slice field of atlas's own scheduler entry where the kernel prints it, otherwise from the release, {{obs:kernel.release}}.
:::else
This kernel ({{obs:kernel.release}}) predates EEVDF, so ordinary tasks here are scheduled by CFS: the task with the smallest vruntime runs, for its weighted share of a scheduling period. Everything about weights and vruntime above applies; lag, eligibility and deadlines do not.
:::unknown
Which fair-scheduling algorithm this kernel uses could not be determined ({{derived:derived.sched.fair_scheduler}}).
:::end

:::when derived.sched.ext_enabled
A sched_ext BPF scheduler is loaded here ({{obs:sched.ext.state}}), so the policy that places ordinary tasks may be that program's rather than the fair class's.
:::else
No BPF scheduler is running here. The sched_ext state is {{obs:sched.ext.state}}; where it is absent, the kernel predates 6.12 or was built without the class.
:::unknown
Whether a BPF scheduler is loaded could not be read here ({{obs:sched.ext.state}}).
:::end

:::when sched.sysctl.kernel.sched_autogroup_enabled
Autogroup is on here, so tasks outside cgroups with their own CPU settings share the CPU session by session. Real-time tasks may use {{obs:sched.sysctl.kernel.sched_rt_runtime_us}} out of every {{obs:sched.sysctl.kernel.sched_rt_period_us}}.
:::else
Autogroup is off here ({{obs:sched.sysctl.kernel.sched_autogroup_enabled}}), so tasks outside cgroups with CPU settings compete one by one, not session by session. Real-time tasks may use {{obs:sched.sysctl.kernel.sched_rt_runtime_us}} out of every {{obs:sched.sysctl.kernel.sched_rt_period_us}}.
:::end

### Atlas as a task

:::evidence Atlas's own scheduling
- sched.self.policy
- sched.self.nice
- derived.sched.self_weight
- sched.self.load_weight | Weight as stored (10 extra bits on 64-bit kernels)
- sched.self.slice_us | Slice (EEVDF kernels only)
- sched.self.run_us
- sched.self.wait_us
- sched.self.timeslices
- sched.self.voluntary_ctxt_switches
- sched.self.nonvoluntary_ctxt_switches
:::end

:::when derived.sched.self_weight
Atlas's main thread runs under {{obs:sched.self.policy}} at nice {{obs:sched.self.nice}}, which the weight table turns into a weight of {{derived:derived.sched.self_weight}}. Competing with one nice-0 task, which weighs 1024, on the same CPU, it would receive its weight divided by the sum of the two. The kernel stores the weight as {{obs:sched.self.load_weight}}: on 64-bit kernels weights carry ten extra bits of precision, 1024 times the table's value.
:::else
Atlas's main thread runs under {{obs:sched.self.policy}}, a policy whose tasks are not scheduled by weight, so the weight table does not apply to it ({{derived:derived.sched.self_weight}}).
:::unknown
Atlas's scheduling policy or nice value could not be read here ({{obs:sched.self.policy}}).
:::end

:::when sched.self.slice_us
The kernel gives atlas a slice of {{obs:sched.self.slice_us}}: the base slice scaled by 1 + log2 of the number of CPUs, counting at most 8. That is the length of virtual time between its vruntime and its deadline, in real time at nice 0.
:::unknown
This kernel does not report a per-task slice ({{obs:sched.self.slice_us}}): CFS kernels have none, and early EEVDF kernels do not print it.
:::end

So far atlas's main thread has run for {{obs:sched.self.run_us}} in {{obs:sched.self.timeslices}} timeslices and waited {{obs:sched.self.wait_us}} on run queues. Its {{obs:sched.self.voluntary_ctxt_switches}} voluntary switches are the times it blocked, mostly reading files and waiting for its other threads; its {{obs:sched.self.nonvoluntary_ctxt_switches}} involuntary ones are the times the scheduler took the CPU away while it could have continued.

## See it

:::sim fair-share

The simulator shares one CPU among a few always-runnable tasks under EEVDF or CFS, starting from this machine's algorithm, slice and atlas's nice value where they can be read. Its vruntime, lag and shares follow the kernel's rules; its tasks, workloads and clock are invented.

## Predict

:::predict
Two CPU-bound tasks share one CPU, one at nice 0 and one at nice 5. What share of the CPU does each get?

- 50 and 50: nice only matters when tasks wait for I/O.
- About 55 and 45: each nice step costs a few percent.
- About 75 and 25: each nice step is a factor of 1.25 in weight.
:::reveal
About 75 and 25. Nice 0 weighs 1024 and nice 5 weighs 335, about 1.25 to the fifth power less, so the first task gets 1024 ÷ 1359 of the CPU and the second the rest. The 10 percent rule applies per step, but the steps multiply rather than add.

The first answer is how some other systems use priorities, only for ordering waits. The second is the result for one step (nice 0 against nice 1 splits 55.5 to 44.5), not five. In the simulator, choose the scenario "nice 0 against nice 5" and run it: the shares converge on 75.3 and 24.7 percent.

:::when derived.sched.self_weight
Atlas itself weighs {{derived:derived.sched.self_weight}} here.
:::end
:::end

## Try it

Look at the scheduling settings of the current shell, read-only:

```sh
chrt -p $$
ps -o pid,cls,ni,pri,comm -p $$
grep ctxt_switches /proc/$$/status
```

To see weights at work, run two busy loops pinned to the same CPU, at nice 0 and nice 5, and compare their shares after ten seconds:

```sh
taskset -c 0 sh -c 'while :; do :; done' & A=$!
taskset -c 0 nice -n 5 sh -c 'while :; do :; done' & B=$!
sleep 10; ps -o pid,ni,pcpu,comm -p $A,$B; kill $A $B
```

Expect about 75 and 25 percent in `pcpu`, which is averaged over the processes' lifetimes, so it converges as they run. Both loops share CPU 0, so everything else on the machine is undisturbed, and the last command stops them.

## Limits

:::misconception A higher nice value means a higher priority
It means a lower one: the name says how "nice" the task is to others. Nice 19 is the least demanding and weighs 15; nice −20 is the most and weighs 88761, and only root may lower a nice value.
:::end

:::misconception A nice 19 task gets no CPU while others run
It gets a small share, not none: against one nice-0 task, 15 ÷ 1039, about 1.4 percent. Only `SCHED_IDLE`, weighing 3, comes close to running only when the CPU would otherwise idle, and even it is not starved.
:::end

:::misconception The task with the smallest vruntime always runs next
That was CFS. Under EEVDF, the eligible task with the earliest virtual deadline runs: a task far behind in vruntime but with a long slice may wait while a task with a short slice and an earlier deadline goes first.
:::end

:::cannot
Machine Atlas reads only its own scheduling state. The vruntime, lag, deadline and waits of other tasks are in their `/proc/<pid>/sched` files, which it does not read, and the kernel publishes nothing about the order of a run queue.

The figures for atlas describe its main thread at the moment of collection; Go runs most of its work on other threads, each scheduled separately.

Shares are relative to whatever else is runnable on the same CPU and in the same group. Cgroup weights and autogroup apply above the task weights shown here, and neither the groups' weights nor their contents are visible from these figures.
:::end

## Go deeper

Next, [CPU limits and isolation](lesson:cpu-limits-and-isolation) applies weights to groups of processes and adds the controls that cap and confine them. In Explore, the [fair scheduler](component:fair-scheduler) component shows what it depends on. Planned lessons cover real-time and deadline scheduling, BPF schedulers with sched_ext, and load balancing between run queues.
