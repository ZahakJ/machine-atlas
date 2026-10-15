---
{
  "id": "run-queues-and-load",
  "title": "What a load average of 2 means",
  "subsystem": "cpu",
  "kind": "core",
  "order": 3,
  "summary": "Task states and per-CPU run queues, how the load average is computed and why Linux counts tasks waiting for disks, CPU time by state in /proc/stat, stolen time, run-queue wait in /proc/schedstat, and CPU pressure.",
  "prerequisites": ["cpu-topology"],
  "uses": [
    "cpu.logical_count",
    "derived.env.kind",
    "derived.sched.avg_timeslice_us",
    "derived.sched.busy_since_boot_fraction",
    "derived.sched.load_per_cpu",
    "derived.sched.steal_fraction",
    "derived.sched.wait_fraction",
    "kernel.uptime_seconds",
    "sched.loadavg.avg1",
    "sched.loadavg.avg15",
    "sched.loadavg.avg5",
    "sched.loadavg.entities",
    "sched.loadavg.runnable",
    "sched.psi.some.avg10",
    "sched.psi.some.avg300",
    "sched.psi.some.avg60",
    "sched.psi.some.total_us",
    "sched.schedstat.cpus",
    "sched.stat.cpu_times",
    "sched.stat.procs_blocked",
    "sched.stat.procs_running"
  ],
  "relevance": [
    {"when": "derived.env.kind", "is": "vm-guest", "note": "Atlas runs in a virtual machine, where a CPU can be taken away by the hypervisor: stolen time applies."},
    {"when": "derived.env.kind", "is": "container", "note": "Atlas runs in a container, whose load average and CPU times describe the whole host."}
  ],
  "next": ["fair-scheduling"],
  "deeper": ["planned:load-balancing-and-sched-domains", "planned:processes-and-threads"],
  "version_notes": [
    {"applies": "Linux since 1993", "text": "The load average began counting tasks in uninterruptible sleep as well as runnable ones, so that a system waiting for its disks would not look idle; every Linux since has kept that definition, unlike most other Unix systems."},
    {"applies": "Linux ≥ 2.6.11", "text": "/proc/stat reports steal time, the time a virtual CPU was ready to run but the hypervisor ran something else; guest time followed in 2.6.24 and guest_nice in 2.6.33."},
    {"applies": "Linux ≥ 4.20", "text": "Pressure stall information: /proc/pressure/cpu reports the share of time some runnable task was waiting for a CPU. The system-wide full line was added in 5.13 and is always zero; it means something for cgroups."}
  ],
  "references": [
    {"title": "Linux kernel documentation: The /proc filesystem (/proc/stat, /proc/loadavg)", "url": "https://docs.kernel.org/filesystems/proc.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Scheduler Statistics (/proc/schedstat)", "url": "https://docs.kernel.org/scheduler/sched-stats.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: PSI, Pressure Stall Information", "url": "https://docs.kernel.org/accounting/psi.html", "kind": "kernel-doc"},
    {"title": "Kernel source: kernel/sched/loadavg.c (calc_global_load)", "url": "https://github.com/torvalds/linux/blob/master/kernel/sched/loadavg.c", "kind": "source"},
    {"title": "proc_loadavg(5)", "url": "https://man7.org/linux/man-pages/man5/proc_loadavg.5.html", "kind": "man-page"},
    {"title": "proc_stat(5)", "url": "https://man7.org/linux/man-pages/man5/proc_stat.5.html", "kind": "man-page"},
    {"title": "vmstat(8)", "url": "https://man7.org/linux/man-pages/man8/vmstat.8.html", "kind": "man-page"},
    {"title": "Brendan Gregg, Linux Load Averages: Solving the Mystery (2017)", "url": "https://www.brendangregg.com/blog/2017-08-08/linux-load-averages.html", "kind": "other"}
  ]
}
---
## Question

:::when sched.loadavg.avg1
The load average here is {{obs:sched.loadavg.avg1}} over the last minute, {{obs:sched.loadavg.avg5}} over five and {{obs:sched.loadavg.avg15}} over fifteen, on a machine with {{obs:cpu.logical_count}} logical CPUs. Is that busy? Is it the number of CPUs in use, a percentage, a queue length? And why can a machine with idle CPUs report a load far above its CPU count?
:::else
The load average here is {{obs:sched.loadavg.avg1}}: over the last minute, essentially nothing on this machine wanted a CPU or waited for a disk. On a busy machine the same three numbers climb into the tens. What exactly do they count, and why can a machine with idle CPUs report a load far above its CPU count?
:::unknown
Machine Atlas could not read the load average here ({{obs:sched.loadavg.avg1}}). Every Linux machine publishes three such numbers, over one, five and fifteen minutes. What exactly do they count, and why can a machine with idle CPUs report a load far above its CPU count?
:::end

## Why it exists

A CPU runs one task at a time, and a machine nearly always has more tasks than CPUs. Most of them are asleep, waiting for a key, a packet, a timer or a disk, and need nothing. The rest are ready and must wait their turn. How many are waiting, and for how long, is the first question anyone asks about a slow machine, and the kernel keeps the answer at several levels of detail: one number every few seconds for the whole machine, CPU time by state since boot, and per CPU, the total time tasks spent waiting in line.

## How it works

### What a task is doing

Every thread is a *task* to the scheduler, and each task is in one state at a time, the letter `ps` and `top` show:

- **R**, runnable: running on a CPU, or ready and waiting for one;
- **S**, sleeping: waiting for an event, and a signal can wake it early;
- **D**, uninterruptible sleep: waiting for something that must not be interrupted, usually disk or network-filesystem I/O, sometimes a kernel lock;
- **T** stopped, **Z** exited and waiting to be reaped, **I** an idle kernel thread.

A task that blocks leaves the CPU at once (a *voluntary* context switch), and something else runs.

### One run queue per CPU

Each CPU has its own *run queue*: the runnable tasks that will run on it, one sub-queue per scheduling class (deadline, real-time, fair, idle). A CPU picks its next task from its own queue without consulting the others, which is what lets the scheduler scale to hundreds of CPUs. The price is balancing: a waking task is placed on a queue chosen by where it last ran, what is idle and what shares a cache with it, and periodically, or whenever a CPU goes idle, the scheduler pulls tasks from busier queues. When every queue is empty the CPU runs its idle task and drops into an idle state.

### The load average

Every five seconds the kernel adds up, over all CPUs, the tasks that are runnable plus those in uninterruptible sleep, and folds that count into three exponentially damped averages. Each update keeps a fixed share of the old value and adds the rest from the new count: for the one-minute figure the old value keeps e^(−5/60), about 92 percent. A sudden, lasting change therefore shows two-thirds of its size in the one-minute figure after a minute, and in the fifteen-minute figure only after fifteen.

Two things follow. The number is not divided by the CPU count: a load of 4 saturates a 4-CPU machine and leaves a 64-CPU machine nearly idle. And because tasks in D state are counted, a machine whose disks or NFS server are slow shows a high load while its CPUs sit idle. Linux chose this in 1993 so that a system waiting for its disks would not look idle; most other Unix systems count runnable tasks only.

### Where CPU time goes

The kernel also accounts, per CPU, what each tick of the clock was spent on, published in `/proc/stat` in units of USER_HZ (1/100 s):

| state | meaning |
|---|---|
| user, nice | running programs; nice counts those with a positive nice value |
| system | running kernel code on a program's behalf |
| irq, softirq | handling hardware interrupts and the deferred work they queue |
| idle | nothing to run |
| iowait | idle, while some task that last ran on this CPU waits for I/O |
| steal | in a virtual machine: ready to run, but the hypervisor ran something else |
| guest, guest_nice | running a virtual machine's CPU (already included in user and nice) |

iowait is idle time: the CPU could have run something else. On a multi-CPU machine the waiting task is not tied to any CPU, so the figure is an approximation the kernel documentation calls unreliable.

### Waiting in the queue

Counting tasks says nothing about how long each waited. With `CONFIG_SCHED_INFO`, the kernel records for every CPU the total time tasks ran on it and the total time they spent runnable on its queue without running, both in nanoseconds, plus how many timeslices it handed out: the last three numbers of each `cpuN` line in `/proc/schedstat`. The same three numbers exist per task in `/proc/<pid>/schedstat`.

### CPU pressure

Pressure stall information (Linux 4.20 and later) turns queueing into a share of time: `/proc/pressure/cpu` says for how much of the last 10, 60 and 300 seconds at least one runnable task was waiting for a CPU (*some*), with a running total in microseconds. Unlike the load average it ignores tasks waiting for disks, and unlike CPU utilisation it measures delay rather than busyness: a machine can be 100 percent busy with no one waiting, or half idle while tasks queue on one overloaded CPU.

## On this machine

:::evidence The load now
- sched.loadavg.avg1
- sched.loadavg.avg5
- sched.loadavg.avg15
- cpu.logical_count
- derived.sched.load_per_cpu
- sched.loadavg.runnable | Runnable at the instant of reading
- sched.loadavg.entities | Threads on the machine
- sched.stat.procs_running | On run queues now, atlas included
- sched.stat.procs_blocked | Blocked on I/O now
:::end

:::when derived.sched.load_per_cpu
Divided by the {{obs:cpu.logical_count}} logical CPUs, the one-minute load is {{derived:derived.sched.load_per_cpu}} per CPU. Below about 1, there was, on average, a CPU for everything that wanted one or was stuck in uninterruptible sleep; above it, something waited. At the instant Machine Atlas read the files, the run queues held {{obs:sched.stat.procs_running}} tasks, atlas itself among them; `procs_blocked`, the tasks waiting for I/O, was {{obs:sched.stat.procs_blocked}}; and the machine had {{obs:sched.loadavg.entities}} threads in all.
:::else
The load per CPU here is {{derived:derived.sched.load_per_cpu}}: the machine has been idle. At the instant Machine Atlas read the files, the run queues held {{obs:sched.stat.procs_running}} tasks, atlas itself among them; `procs_blocked`, the tasks waiting for I/O, was {{obs:sched.stat.procs_blocked}}; and the machine had {{obs:sched.loadavg.entities}} threads in all.
:::unknown
The load per CPU could not be computed here ({{derived:derived.sched.load_per_cpu}}).
:::end

:::when derived.env.kind is container
Atlas appears to run in a container. Nothing virtualises `/proc/loadavg` or `/proc/stat`, so these figures describe every task on the host, not the container's own, and the CPU count is the host's too. The container's own CPU use and limits are in its cgroup, the subject of a later lesson.
:::else
No container was detected, so these figures describe the tasks of the kernel atlas runs on. Inside a container they would describe the whole host, because nothing virtualises `/proc/loadavg` or `/proc/stat`.
:::unknown
Whether atlas runs in a container could not be determined here. If it does, these figures describe every task on the host rather than the container's own, because nothing virtualises `/proc/loadavg` or `/proc/stat`.
:::end

### CPU time since boot

:::table sched.stat.cpu_times columns=cpu,user,nice,system,idle,iowait,irq,softirq,steal limit=5

:::evidence Summed over all CPUs
- kernel.uptime_seconds
- derived.sched.busy_since_boot_fraction
- derived.sched.steal_fraction
:::end

Over {{obs:kernel.uptime_seconds}} of uptime, the CPUs were busy (not idle, not in iowait) {{derived:derived.sched.busy_since_boot_fraction|percent}} of the time. An average over the whole uptime hides every busy hour; for the present, take two readings a few seconds apart and subtract.

:::when derived.sched.steal_fraction
{{derived:derived.sched.steal_fraction|percent}} of the CPU time since boot was stolen: time a virtual CPU was ready to run while the hypervisor ran other guests or its own work. Stolen time lengthens every queue without showing as load inside the guest; a few percent is common, and tens of percent mean an oversubscribed host.
:::else
No time was stolen here since boot. On bare metal that is so by definition, since no hypervisor can take a CPU away; in a virtual machine it means the hypervisor reports no steal time to the guest, or never had to take a CPU away.
:::unknown
Stolen time could not be computed here ({{derived:derived.sched.steal_fraction}}). In a virtual machine it is the time a virtual CPU was ready to run while the host ran something else; on bare metal it is zero.
:::end

### Waiting for a CPU

:::table sched.schedstat.cpus columns=cpu,run_ns,wait_ns,timeslices limit=8

:::when derived.sched.wait_fraction
Of all the time tasks were runnable here since boot, they spent {{derived:derived.sched.wait_fraction|percent}} waiting on a run queue rather than running, and a timeslice lasted {{derived:derived.sched.avg_timeslice_us}} on average. The wait share is the queueing the load average only hints at: zero means every task found an idle CPU the moment it woke.
:::else
Tasks here have run without measurable queueing since boot: their wait time is {{derived:derived.sched.wait_fraction|percent}} of their runnable time.
:::unknown
Run-queue wait times could not be read here ({{obs:sched.schedstat.cpus}}). Without them, the load average and CPU pressure are the only measures of queueing.
:::end

### Pressure

:::when sched.psi.some.total_us
:::evidence CPU pressure
- sched.psi.some.avg10
- sched.psi.some.avg60
- sched.psi.some.avg300
- sched.psi.some.total_us
:::end

Over the last minute, at least one runnable task here was waiting for a CPU {{obs:sched.psi.some.avg60}} of the time, and {{obs:sched.psi.some.total_us}} of such waiting has accumulated since boot. Values below a few percent mean tasks rarely queue; sustained values in the tens mean work is regularly delayed.
:::else
No task here has waited for a CPU since boot ({{obs:sched.psi.some.total_us}} of CPU pressure): every runnable task found a CPU at once.
:::unknown
CPU pressure could not be read here ({{obs:sched.psi.some.total_us}}). If the file is absent, the kernel predates 4.20 or was built or booted without pressure stall information; if permission is denied, a sandbox or security policy hides `/proc/pressure`. The load average and the schedstat wait times are the measures of queueing left.
:::end

## Predict

:::predict
A machine with 4 CPUs shows a load average of 9, steady for an hour, while `top` shows the CPUs 80 percent idle. What explains it?

- The load average is measured in percent, so 9 is light.
- Most of the load is tasks in uninterruptible sleep, waiting for a disk or a network filesystem.
- top is wrong: a load of 9 on 4 CPUs means the CPUs are overloaded.
:::reveal
The second. Linux counts tasks in D state in the load average, and they use no CPU while they wait. A slow or failed disk, a hung NFS mount or contention on a kernel lock can leave dozens of tasks in D state, with a high load on idle CPUs. `ps -eo stat,comm | grep '^D'` lists them, and `procs_blocked` in `/proc/stat` counts those waiting for I/O.

The load average is a count of tasks, not a percentage, and top's idle figure is accurate: it is time the CPUs had nothing runnable. Had the load been runnable tasks, the CPUs would be busy and CPU pressure high.
:::end

## Try it

Read the queue and the time accounts, without changing anything:

```sh
cat /proc/loadavg
vmstat 1 5
cat /proc/pressure/cpu
```

`/proc/loadavg` ends with runnable/total threads and the most recent process id. In `vmstat`, `r` is tasks runnable (running or waiting), `b` tasks blocked on I/O, `cs` context switches per second, and `us sy id wa st` split CPU time into user, system, idle, iowait and stolen. On a quiet machine expect `r` at 1 or 2 and `id` in the nineties. While something CPU-bound runs on every CPU, `r` climbs to the CPU count and beyond, and the pressure averages rise as soon as `r` exceeds the CPUs.

## Limits

:::misconception The load average is CPU utilisation
It is an average count of tasks that are runnable or in uninterruptible sleep. It has no upper bound, is not divided by the CPU count, and includes tasks that use no CPU at all while they wait for disks.
:::end

:::misconception A load of 1 means a machine is fully loaded
On one CPU, yes. On {{obs:cpu.logical_count}} logical CPUs, full load starts near that many, and tasks begin to queue only above it.
:::end

:::misconception iowait is time the CPU spent doing I/O
CPUs do not wait for I/O; tasks do. iowait is idle time during which some task that last ran on that CPU was waiting for I/O. The CPU could have run other work, and on a busy machine the same wait shows as user time of the other tasks instead.
:::end

:::cannot
None of these figures says which tasks were waiting or for what: the load average and pressure are system-wide, and naming the tasks means reading every process's state, which this lesson's figures do not do.

The CPU times and schedstat totals are cumulative since boot, so one snapshot gives averages over the whole uptime; rates need two readings. The time accounts are sampled at the tick, so very short bursts are attributed approximately.

Inside a container, every figure here describes the host. Inside a virtual machine, they describe the guest's virtual CPUs, and only steal time hints at what the host is doing.
:::end

## Go deeper

Next, [fair scheduling](lesson:fair-scheduling) opens one run queue and follows how the scheduler chooses among the tasks in it. In Explore, the [run queues](component:run-queues) component connects these figures to the scheduler. Planned lessons cover how load balancing moves tasks between run queues, and processes and threads, the tasks themselves.
