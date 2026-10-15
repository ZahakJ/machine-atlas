package catalog

import "github.com/ZahakJ/machine-atlas/internal/obs"

// CPU topology, frequency scaling and scheduler ids, collected by
// internal/collect/sched. The cpu.* ids here extend the ones in
// catalog_system.go (model, counts, cpu0's caches) with the per-CPU
// topology and cpufreq; sched.* is the scheduler's own state.

const (
	privSysfsCPU  = "Nothing: these sysfs files are world-readable; a hardened system may restrict /sys/devices/system/cpu to root, and root then reads the same values."
	privSelf      = "Nothing: a process may always read its own /proc/self files."
	cpuCgroupNote = "Applies to the cgroup atlas ran in; an ancestor cgroup can impose a tighter limit than this one shows."
)

// cpuList declares one /sys/devices/system/cpu list file. An empty string
// is a valid value: the kernel prints an empty line when the set is empty.
func cpuList(name, file, title, desc, caveat string) Entry {
	return Entry{
		ID: "cpu." + name, Title: title,
		Description: desc + " Read from /sys/devices/system/cpu/" + file + " in the kernel's CPU list format (\"0-3,8\"); an empty value means no CPU.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat: caveat, Privileged: privSysfsCPU,
	}
}

// schedSysctl declares one /proc/sys/kernel/sched_* tunable.
func schedSysctl(name, unit, title, desc, caveat string) Entry {
	return Entry{
		ID: "sched.sysctl.kernel." + name, Title: title, Description: desc,
		Unit: unit, Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat: caveat, Privileged: "Nothing to read; root could also change it, which Machine Atlas never does.",
	}
}

// schedPSI declares one /proc/pressure/cpu figure.
func schedPSI(line, field string) Entry {
	e := Entry{
		ID: "sched.psi." + line + "." + field, Kind: obs.KindLive, Scope: obs.ScopeKernel, Privileged: privSame,
		Caveat: "Pressure stall information measures time lost, not who caused it; it is absent when the kernel is older than 4.20, was built without CONFIG_PSI or was booted with psi=0.",
	}
	what := "at least one runnable task was waiting for a CPU"
	if line == "full" {
		what = "every non-idle task was waiting for a CPU at once (system-wide this stays zero; it is reported since Linux 5.13 for cgroups)"
	}
	if field == "total_us" {
		e.Title = "CPU pressure (" + line + ") total"
		e.Description = "Total time since boot during which " + what + "."
		e.Unit, e.Type = obs.UnitMicroseconds, obs.TypeInt
	} else {
		e.Title = "CPU pressure (" + line + ") " + field
		e.Description = "Share of wall time over the last " + field[3:] + " seconds during which " + what + "."
		e.Unit, e.Type = obs.UnitPercent, obs.TypeFloat
	}
	return e
}

// selfSched declares one of atlas's own scheduler figures.
func selfSched(name, typ, unit, title, desc, caveat string) Entry {
	return Entry{
		ID: "sched.self." + name, Title: title, Description: desc, Unit: unit, Type: typ,
		Kind: obs.KindLive, Scope: obs.ScopeProcess,
		Caveat:     caveat + " Describes the main thread of the atlas process at the moment of collection, not the other processes on this machine.",
		Privileged: privSelf,
	}
}

// cgroupCPU declares one figure of the cpu controller of atlas's cgroup.
func cgroupCPU(name, typ, unit, title, desc, caveat string) Entry {
	return Entry{
		ID: "sched.cgroup." + name, Title: title, Description: desc, Unit: unit, Type: typ,
		Kind: obs.KindStatic, Scope: obs.ScopeCgroup,
		Caveat: caveat, Privileged: privSame,
	}
}

// cgroupCPUStat declares one cpu.stat counter.
func cgroupCPUStat(name, unit, title, desc, caveat string) Entry {
	e := cgroupCPU("cpu."+name, obs.TypeInt, unit, title, desc, caveat+" Counts since the cgroup was created, which for a session or container may be much later than boot.")
	e.Kind = obs.KindLive
	return e
}

var schedEntries = []Entry{
	// ---------------------------------------------------------- topology
	{
		ID: "cpu.topology", Title: "CPU topology",
		Description: "One row per online logical CPU: its package (socket), die, cluster and core id, the hardware threads that share its core, on hybrid x86 parts whether it is a performance or an efficiency core, and where the kernel publishes it, its relative capacity (1024 for the fastest CPU).",
		Type:        obs.TypeTable, Columns: []string{"cpu", "package", "die", "cluster", "core_id", "thread_siblings", "core_type", "capacity"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "What firmware and the kernel report, which inside a virtual machine is whatever topology the hypervisor invents. Ids are labels, not positions: core ids repeat across packages and need not be contiguous. Cells a kernel does not export are left out.",
		Privileged: privSysfsCPU,
	},
	{
		ID: "cpu.cache_domains", Title: "Cache sharing",
		Description: "Every distinct cache of every online CPU, once: its level, type, size and the CPUs that share it, from /sys/devices/system/cpu/cpu*/cache. The number of rows of a level is how many copies of that cache the machine has.",
		Type:        obs.TypeTable, Columns: []string{"level", "type", "size_bytes", "shared_cpu_list", "cpus"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Virtual machines and some arm64 firmware publish incomplete or invented cache tables. Sharing is what the kernel was told, not a measurement of latency.",
		Privileged: privSysfsCPU,
	},
	{
		ID: "cpu.smt.control", Title: "SMT control",
		Description: "Whether simultaneous multithreading may be used: on, off, forceoff (disabled on the kernel command line), notsupported (the CPU has no SMT) or notimplemented (this architecture cannot switch it at run time).",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "The kernel's switch, not a statement about security: a CPU with SMT on may still be protected by core scheduling or other mitigations.",
		Privileged: privSysfsCPU,
	},
	{
		ID: "cpu.smt.active", Title: "SMT active",
		Description: "Whether any core currently runs more than one hardware thread (/sys/devices/system/cpu/smt/active).",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Inside a virtual machine this reflects the topology the hypervisor presents, not the host's.",
		Privileged: privSysfsCPU,
	},
	cpuList("present", "present", "Present CPUs", "The CPUs physically present (or, in a virtual machine, provided).",
		"Present is not online: a CPU can be present and switched off."),
	cpuList("possible", "possible", "Possible CPUs", "Every CPU number this boot could ever bring online, including hot-pluggable slots, for which the kernel reserves per-CPU memory.",
		"Often larger than the CPUs that exist, because firmware describes empty slots."),
	cpuList("offline", "offline", "Offline CPUs", "Possible CPUs that are not online now.",
		"Includes slots that will never hold a CPU."),
	cpuList("isolated", "isolated", "Isolated CPUs", "CPUs removed from the scheduler's load balancing at boot (isolcpus=): ordinary tasks are not placed on them unless their affinity asks.",
		"Only the boot-time isolcpus= set; CPUs isolated through cpuset partitions are listed in the root cgroup's cpuset.cpus.isolated instead."),
	cpuList("nohz_full", "nohz_full", "Tickless CPUs", "CPUs on which the periodic scheduler tick stops while a single task runs (nohz_full=), so that task is interrupted as little as possible.",
		"Absent when the kernel was built without CONFIG_NO_HZ_FULL; an empty value means the feature is built in but unused."),
	{
		ID: "cpu.hybrid.performance_cpus", Title: "Performance cores",
		Description: "On hybrid x86 processors (Intel Alder Lake and later), the logical CPUs on performance cores, from /sys/devices/cpu_core/cpus. Absent on processors whose cores are all of one kind.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Published by the perf subsystem because the two core types have different performance counters; arm big.LITTLE systems show their asymmetry in cpu_capacity instead.",
		Privileged: privSame,
	},
	{
		ID: "cpu.hybrid.efficiency_cpus", Title: "Efficiency cores",
		Description: "On hybrid x86 processors, the logical CPUs on efficiency cores, from /sys/devices/cpu_atom/cpus. Absent on processors whose cores are all of one kind.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Published by the perf subsystem; it says which cores are which, not how much slower an efficiency core is.",
		Privileged: privSame,
	},

	// --------------------------------------------------------- frequency
	{
		ID: "cpu.freq.policies", Title: "Frequency scaling policies",
		Description: "One row per cpufreq policy, the group of CPUs that must change frequency together: the CPUs it covers, the scaling driver and governor, the governors available, the current, minimum and maximum frequency allowed (kHz), the hardware limits (kHz), and the energy-performance preference where the driver has one.",
		Type:        obs.TypeTable, Columns: []string{"policy", "cpus", "driver", "governor", "governors", "cur_khz", "min_khz", "max_khz", "hw_min_khz", "hw_max_khz", "epp"},
		Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "cur_khz is the kernel's last request or an estimate, read once; hardware-managed drivers (intel_pstate, amd-pstate in active mode) change the real frequency many times a second. Absent in most virtual machines, whose hypervisor owns the frequency.",
		Privileged: "Root could read cpuinfo_cur_freq, the frequency some drivers read back from hardware; the rest is world-readable.",
	},
	{
		ID: "cpu.freq.boost", Title: "Frequency boost",
		Description: "Whether frequencies above the base clock (Turbo Boost, Precision Boost) are allowed, from /sys/devices/system/cpu/cpufreq/boost, which the acpi-cpufreq and amd-pstate drivers provide.",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "intel_pstate has no such file; it uses intel_pstate/no_turbo instead. Allowed is not achieved: boost depends on temperature, power and how many cores are busy.",
		Privileged: privSame,
	},
	{
		ID: "cpu.freq.intel_pstate.status", Title: "intel_pstate mode",
		Description: "The mode of the intel_pstate driver: active (the driver, usually with the CPU's own hardware P-states, chooses frequencies; only performance and powersave governors exist), passive (it acts as an ordinary driver, intel_cpufreq, under the generic governors) or off.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Absent when intel_pstate is not in use: on AMD and arm CPUs, in most virtual machines, and when the kernel prefers acpi-cpufreq.",
		Privileged: privSame,
	},
	{
		ID: "cpu.freq.intel_pstate.no_turbo", Title: "intel_pstate turbo disabled",
		Description: "True when intel_pstate is told not to use turbo frequencies (intel_pstate/no_turbo is 1).",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Firmware can also disable turbo, in which case the file reads 1 and cannot be changed.",
		Privileged: privSame,
	},
	{
		ID: "cpu.freq.amd_pstate.status", Title: "amd-pstate mode",
		Description: "The mode of the amd-pstate driver: active (the CPU chooses frequencies guided by an energy-performance preference), guided, passive, or disable.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Absent on kernels before 6.3, on non-AMD CPUs and when the driver is not loaded.",
		Privileged: privSame,
	},

	// ------------------------------------------------------ load and time
	{
		ID: "sched.loadavg.avg1", Title: "Load average, 1 minute",
		Description: "The exponentially damped average, over about one minute, of the number of tasks that are runnable or in uninterruptible sleep (state D), system-wide (/proc/loadavg).",
		Type:        obs.TypeFloat, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Not divided by the number of CPUs, and on Linux it counts tasks waiting for disk or locks in state D as well as tasks wanting a CPU. Inside a container it describes the whole host.",
		Privileged: privSame,
	},
	{
		ID: "sched.loadavg.avg5", Title: "Load average, 5 minutes",
		Description: "The same average as the 1-minute figure with a five-minute time constant.",
		Type:        obs.TypeFloat, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Not divided by the number of CPUs; includes tasks in uninterruptible sleep. Inside a container it describes the whole host.",
		Privileged: privSame,
	},
	{
		ID: "sched.loadavg.avg15", Title: "Load average, 15 minutes",
		Description: "The same average as the 1-minute figure with a fifteen-minute time constant.",
		Type:        obs.TypeFloat, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Not divided by the number of CPUs; includes tasks in uninterruptible sleep. Inside a container it describes the whole host.",
		Privileged: privSame,
	},
	{
		ID: "sched.loadavg.runnable", Title: "Runnable now",
		Description: "The fourth field of /proc/loadavg before the slash: tasks runnable at the instant of reading, including the one reading the file.",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     instantaneous + " Excludes tasks in uninterruptible sleep, which the averages include.",
		Privileged: privSame,
	},
	{
		ID: "sched.loadavg.entities", Title: "Scheduling entities",
		Description: "The fourth field of /proc/loadavg after the slash: every thread that exists on the machine, running or not.",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     instantaneous + " Counts threads, not processes.",
		Privileged: privSame,
	},
	{
		ID: "sched.stat.cpu_times", Title: "CPU time by state",
		Description: "From /proc/stat: for all CPUs together (row all) and for each CPU, the time spent since boot in user mode, user mode at positive nice, kernel mode, idle, idle with I/O outstanding, hard and soft interrupts, stolen by the hypervisor, and running guests, in USER_HZ ticks (1/100 s).",
		Type:        obs.TypeTable, Columns: []string{"cpu", "user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal", "guest", "guest_nice"},
		Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Cumulative since boot and sampled by the tick, so short bursts are attributed approximately; iowait is idle time and unreliable on multi-core machines; guest time is also counted in user. Inside a container it describes the host.",
		Privileged: privSame,
	},
	{
		ID: "sched.stat.ctxt", Title: "Context switches since boot",
		Description: "The number of times any CPU switched from one task to another since boot (ctxt in /proc/stat).",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     cumulative,
		Privileged: privSame,
	},
	{
		ID: "sched.stat.processes", Title: "Tasks created since boot",
		Description: "Forks since boot (processes in /proc/stat): every new process or thread.",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Counts threads as well as processes. " + cumulative,
		Privileged: privSame,
	},
	{
		ID: "sched.stat.procs_running", Title: "Tasks on run queues",
		Description: "Tasks on all CPUs' run queues at the instant of reading (procs_running in /proc/stat): running, or runnable and waiting for a CPU.",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     instantaneous + " Includes atlas itself, which was running while it read the file.",
		Privileged: privSame,
	},
	{
		ID: "sched.stat.procs_blocked", Title: "Tasks blocked on I/O",
		Description: "Tasks waiting for I/O to complete at the instant of reading (procs_blocked in /proc/stat).",
		Unit:        obs.UnitCount, Type: obs.TypeInt, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     instantaneous + " Not every task in uninterruptible sleep is counted: only those whose wait the kernel accounts as I/O.",
		Privileged: privSame,
	},
	{
		ID: "sched.schedstat.version", Title: "schedstat format version",
		Description: "The version line of /proc/schedstat, which says how to read its fields (15 from Linux 2.6.23, 16 and 17 in recent kernels).",
		Type:        obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Absent when the kernel was built without CONFIG_SCHEDSTATS or CONFIG_SCHED_INFO.",
		Privileged: privSame,
	},
	{
		ID: "sched.schedstat.cpus", Title: "Run and wait time per CPU",
		Description: "For each CPU, from /proc/schedstat: the time tasks spent running on it, the time tasks spent runnable on its run queue waiting for it, both in nanoseconds since boot, and the number of timeslices run (times a task was switched in).",
		Type:        obs.TypeTable, Columns: []string{"cpu", "run_ns", "wait_ns", "timeslices"},
		Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "Cumulative since boot. Wait time is queueing for this CPU only, not waiting for I/O or locks; idle time is not run time. Inside a container it describes the host.",
		Privileged: privSame,
	},
	schedPSI("some", "avg10"), schedPSI("some", "avg60"), schedPSI("some", "avg300"), schedPSI("some", "total_us"),
	schedPSI("full", "avg10"), schedPSI("full", "avg60"), schedPSI("full", "avg300"), schedPSI("full", "total_us"),

	// -------------------------------------------------- scheduler settings
	schedSysctl("sched_autogroup_enabled", "", "Autogroup",
		"1 when the fair scheduler groups the tasks of each session (each terminal, each login) and shares CPU time between the groups first, so one session's parallel build does not starve another's interactive shell.",
		"Has no effect on tasks placed in cgroups with their own cpu controller settings; absent when built without CONFIG_SCHED_AUTOGROUP."),
	schedSysctl("sched_rt_runtime_us", obs.UnitMicroseconds, "Real-time runtime per period",
		"How much of each sched_rt_period_us real-time tasks may use before ordinary tasks get the rest (default 950 000 of 1 000 000: 95%). -1 removes the limit.",
		"A safety valve against a runaway real-time task, not a reservation; since Linux 6.12 the fair-server deadline mechanism enforces it."),
	schedSysctl("sched_rt_period_us", obs.UnitMicroseconds, "Real-time period",
		"The period over which sched_rt_runtime_us is measured.",
		"Applies to SCHED_FIFO and SCHED_RR tasks only."),
	schedSysctl("sched_rr_timeslice_ms", "", "Round-robin timeslice (ms)",
		"The time a SCHED_RR task runs before another real-time task of the same priority gets its turn, in milliseconds.",
		"Only SCHED_RR uses it; ordinary tasks get slices from the fair scheduler."),
	schedSysctl("sched_util_clamp_min", "", "Utilization clamp, minimum allowed",
		"The highest minimum utilization (0 to 1024) a task may request with sched_setattr(), which frequency selection and placement treat as a floor.",
		"Absent when built without CONFIG_UCLAMP_TASK."),
	schedSysctl("sched_util_clamp_max", "", "Utilization clamp, maximum allowed",
		"The highest maximum utilization (0 to 1024) a task may have; 1024 leaves tasks unclamped.",
		"Absent when built without CONFIG_UCLAMP_TASK."),
	schedSysctl("sched_cfs_bandwidth_slice_us", obs.UnitMicroseconds, "CPU bandwidth slice",
		"How much of a cgroup's CPU quota a CPU takes from the shared pool at a time when the cgroup's tasks run there (default 5000 µs).",
		"Larger slices mean fewer trips to the shared pool but more quota stranded on CPUs that stop using it, which can throttle a cgroup before its quota is truly spent."),
	{
		ID: "sched.ext.state", Title: "sched_ext state",
		Description: "The state of the extensible scheduler class, which lets a BPF program replace the fair scheduler's policy: disabled, enabling, enabled or disabling (/sys/kernel/sched_ext/state).",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Absent before Linux 6.12 and when built without CONFIG_SCHED_CLASS_EXT. Enabled says a BPF scheduler is loaded, not which tasks it schedules.",
		Privileged: privSame,
	},

	// -------------------------------------------------------- atlas itself
	selfSched("voluntary_ctxt_switches", obs.TypeInt, obs.UnitCount, "Voluntary context switches (atlas)",
		"Times atlas's main thread gave up the CPU because it had to wait: for I/O, a lock, a timer or another thread (voluntary_ctxt_switches in /proc/self/status).",
		"Counts since the thread started."),
	selfSched("nonvoluntary_ctxt_switches", obs.TypeInt, obs.UnitCount, "Involuntary context switches (atlas)",
		"Times atlas's main thread was preempted while it could have kept running, because its slice ended or a more urgent task woke (nonvoluntary_ctxt_switches).",
		"Counts since the thread started."),
	selfSched("cpus_allowed_list", obs.TypeString, "", "CPUs atlas may run on",
		"The affinity mask of atlas's main thread (Cpus_allowed_list in /proc/self/status): the CPUs the scheduler may place it on, after sched_setaffinity(), cpusets and isolcpus.",
		"Affinity limits where a task may run, not how much; a container's CPU quota does not show here."),
	selfSched("policy", obs.TypeString, "", "Scheduling policy (atlas)",
		"The scheduling policy of atlas's main thread, from /proc/self/stat: SCHED_OTHER (the fair class), SCHED_BATCH, SCHED_IDLE, SCHED_FIFO, SCHED_RR, SCHED_DEADLINE or SCHED_EXT.",
		"Atlas sets none, so this is what it inherited from the process that started it."),
	selfSched("nice", obs.TypeInt, "", "Nice value (atlas)",
		"The nice value of atlas's main thread, -20 to 19, from /proc/self/stat; it selects the weight the fair scheduler gives the thread.",
		"Under autogroup, nice is relative to the other tasks of the same session only."),
	selfSched("run_us", obs.TypeInt, obs.UnitMicroseconds, "CPU time used (atlas)",
		"Time atlas's main thread has spent running on a CPU (first field of /proc/self/schedstat, converted from nanoseconds).",
		"The main thread only; Go programs do most work on other threads."),
	selfSched("wait_us", obs.TypeInt, obs.UnitMicroseconds, "Run-queue wait (atlas)",
		"Time atlas's main thread spent runnable but waiting for a CPU (second field of /proc/self/schedstat, converted from nanoseconds).",
		"Absent or zero on kernels built without CONFIG_SCHED_INFO."),
	selfSched("timeslices", obs.TypeInt, obs.UnitCount, "Timeslices run (atlas)",
		"How many times atlas's main thread was switched onto a CPU (third field of /proc/self/schedstat).",
		"Counts since the thread started."),
	selfSched("slice_us", obs.TypeInt, obs.UnitMicroseconds, "EEVDF slice (atlas)",
		"The slice the EEVDF scheduler gives atlas's main thread (se.slice in /proc/self/sched, converted from nanoseconds): the time it is meant to run before its virtual deadline.",
		"Printed only by EEVDF kernels (Linux 6.6 and later), and not by the earliest of them; /proc/<pid>/sched itself needs CONFIG_SCHED_DEBUG before 6.14."),
	selfSched("load_weight", obs.TypeInt, "", "Load weight (atlas)",
		"The weight of atlas's main thread as the fair scheduler stores it (se.load.weight in /proc/self/sched). On 64-bit kernels it carries 10 extra bits of resolution: nice 0 reads 1048576, that is 1024 × 1024.",
		"Needs /proc/<pid>/sched, which exists only with CONFIG_SCHED_DEBUG before Linux 6.14."),

	// ------------------------------------------------------- cpu cgroup
	cgroupCPU("cpu.max_quota_us", TypeLimit, obs.UnitMicroseconds, "cgroup CPU quota",
		"The CPU time atlas's cgroup may use per period (the first field of cpu.max on cgroup v2, or cpu.cfs_quota_us on v1), summed over all CPUs; \"max\" means no quota. A quota of twice the period lets the cgroup use two CPUs' worth.",
		cpuCgroupNote+" A quota limits time, not which CPUs: the cgroup's tasks may still run on every CPU at once and then be throttled."),
	cgroupCPU("cpu.max_period_us", obs.TypeInt, obs.UnitMicroseconds, "cgroup CPU period",
		"The accounting period of the quota (the second field of cpu.max, or cpu.cfs_period_us on v1), 100 000 µs by default.",
		cpuCgroupNote),
	cgroupCPU("cpu.weight", obs.TypeInt, "", "cgroup CPU weight",
		"The cgroup's share of CPU time relative to its siblings when they compete (cpu.weight, 1 to 10000, default 100).",
		"Only matters under contention and only between siblings; cgroup v2 only (v1 uses cpu.shares)."),
	cgroupCPU("cpu.shares_v1", obs.TypeInt, "", "cgroup CPU shares (v1)",
		"The cgroup v1 counterpart of cpu.weight (cpu.shares, default 1024).",
		"cgroup v1 only; on v2 it is unsupported."),
	cgroupCPUStat("usage_us", obs.UnitMicroseconds, "cgroup CPU usage",
		"CPU time used by all tasks in atlas's cgroup (usage_usec in cpu.stat, or cpuacct.usage on v1 converted from nanoseconds).",
		"Includes time in the kernel on the tasks' behalf."),
	cgroupCPUStat("user_us", obs.UnitMicroseconds, "cgroup user CPU time",
		"The part of the cgroup's CPU usage spent in user mode (user_usec in cpu.stat).",
		"cgroup v2 only."),
	cgroupCPUStat("system_us", obs.UnitMicroseconds, "cgroup system CPU time",
		"The part of the cgroup's CPU usage spent in the kernel (system_usec in cpu.stat).",
		"cgroup v2 only."),
	cgroupCPUStat("nr_periods", obs.UnitCount, "cgroup quota periods",
		"Periods in which the cgroup had runnable tasks while a quota was set (nr_periods in cpu.stat).",
		"Zero when no quota was ever set; present only when the cpu controller is enabled for the cgroup."),
	cgroupCPUStat("nr_throttled", obs.UnitCount, "cgroup throttled periods",
		"Periods in which the cgroup used up its quota and its tasks were stopped until the next period (nr_throttled in cpu.stat).",
		"Present only when the cpu controller is enabled for the cgroup."),
	cgroupCPUStat("throttled_us", obs.UnitMicroseconds, "cgroup throttled time",
		"Total time the cgroup's tasks were held back by throttling (throttled_usec in cpu.stat, or throttled_time on v1 converted from nanoseconds).",
		"Summed over CPUs, so it can exceed wall time."),
	cgroupCPU("cpuset.cpus_effective", obs.TypeString, "", "cgroup CPU set",
		"The CPUs the cpuset controller lets atlas's cgroup use (cpuset.cpus.effective on v2, cpuset.effective_cpus on v1), in CPU list format.",
		cpuCgroupNote+" Absent when the cpuset controller is not enabled for this cgroup."),
}

// schedDerivedEntries are the derived facts computed by
// internal/derive/sched.go.
var schedDerivedEntries = []Entry{
	derived("cpu.hybrid", obs.TypeBool, "", Inference, "Cores of different kinds",
		"Whether this processor mixes cores of different performance: the hybrid x86 core lists exist, or the kernel gives CPUs different capacities (arm big.LITTLE).",
		"An inference from what the kernel publishes; a virtual machine usually hides the host's asymmetry."),
	derived("cpu.isolated_count", obs.TypeInt, obs.UnitCount, Arithmetic, "Isolated CPUs",
		"The number of CPUs in cpu.isolated, removed from load balancing at boot.",
		"Counts boot-time isolation only, not cpuset partitions."),
	derived("cpu.freq.available", obs.TypeBool, "", Inference, "Frequency scaling visible",
		"Whether the kernel controls CPU frequency here: true when at least one cpufreq policy exists, false when /sys/devices/system/cpu/cpufreq is missing or empty, as in most virtual machines.",
		"False does not mean the frequency is fixed: a hypervisor or the firmware may still change it out of the guest's sight."),
	derived("cpu.freq.driver", obs.TypeString, "", Arithmetic, "Frequency scaling driver",
		"The scaling driver of the cpufreq policies, or mixed when policies differ.",
		"Names the kernel driver, which may delegate the real decisions to the hardware."),
	derived("cpu.freq.governor", obs.TypeString, "", Arithmetic, "Frequency governor",
		"The governor of the cpufreq policies, or mixed when policies differ.",
		"With intel_pstate or amd-pstate in active mode, performance and powersave are hints to the hardware rather than the generic governors of the same names."),
	derived("cpu.freq.boost_enabled", obs.TypeBool, "", Arithmetic, "Boost allowed",
		"Whether frequencies above the base clock are allowed: cpufreq/boost is 1, or intel_pstate/no_turbo is 0.",
		"Allowed is not achieved: boost depends on temperature, power and how many cores are busy."),
	derived("sched.load_per_cpu", obs.TypeFloat, "", Arithmetic, "Load per CPU",
		"The 1-minute load average divided by the number of online CPUs: around 1 means as many tasks wanted to run, or were stuck in uninterruptible sleep, as there are CPUs.",
		"Counts tasks in uninterruptible sleep, which need no CPU, and inside a container divides the host's load by the CPUs the container sees."),
	derived("sched.busy_since_boot_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Busy since boot",
		"The share of all CPU time since boot that was not idle: 1 − (idle + iowait) ÷ total, from the all row of /proc/stat.",
		"An average over the whole uptime, which hides every burst; it says nothing about now."),
	derived("sched.steal_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Stolen time since boot",
		"The share of all CPU time since boot that the hypervisor gave to something else while this guest wanted to run: steal ÷ total.",
		"Zero on bare metal, and on hypervisors that do not report steal time to the guest."),
	derived("sched.ctxt_per_second", obs.TypeFloat, "", Arithmetic, "Context switches per second",
		"The average rate of context switches since boot: ctxt ÷ uptime, for the whole machine.",
		"An average over the uptime; the rate now can be far higher or lower."),
	derived("sched.wait_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Share of runnable time spent waiting",
		"Of all the time tasks were runnable since boot, the share they spent waiting on a run queue rather than running: Σ wait ÷ (Σ run + Σ wait) over the CPUs in /proc/schedstat.",
		"Since boot and over all CPUs, so a busy minute disappears in a quiet month; it measures queueing for a CPU, not for I/O."),
	derived("sched.avg_timeslice_us", obs.TypeFloat, obs.UnitMicroseconds, Arithmetic, "Average timeslice",
		"How long a task ran, on average, each time it was switched onto a CPU: Σ run time ÷ Σ timeslices from /proc/schedstat.",
		"An average over every task, including the many that run for microseconds before sleeping again."),
	derived("sched.fair_scheduler", obs.TypeString, "", Inference, "Fair scheduler algorithm",
		"Which algorithm schedules ordinary tasks: eevdf (Linux 6.6 and later) or cfs (before), inferred from the se.slice field of /proc/self/sched where the kernel prints it, otherwise from the kernel release.",
		"Distribution kernels can backport; a release number is weaker evidence than the slice field."),
	derived("sched.ext_enabled", obs.TypeBool, "", Inference, "BPF scheduler loaded",
		"Whether a sched_ext BPF scheduler is running: sched.ext.state is enabled. A kernel without sched_ext has none.",
		"Does not say which tasks the BPF scheduler handles; in partial mode only tasks marked SCHED_EXT."),
	derived("sched.self_weight", obs.TypeInt, "", Arithmetic, "Weight of atlas's nice value",
		"The fair-scheduler weight for atlas's nice value, from the kernel's sched_prio_to_weight table: 1024 at nice 0, about 1.25 times less per step up.",
		"Applies to SCHED_OTHER and SCHED_BATCH; SCHED_IDLE tasks weigh 3, and real-time tasks are not scheduled by weight at all."),
	derived("sched.self_allowed_count", obs.TypeInt, obs.UnitCount, Arithmetic, "CPUs atlas may use",
		"The number of CPUs in atlas's affinity mask.",
		"Affinity only; a cgroup quota can still limit how much time those CPUs give."),
	derived("cgroup.cpu_limited", obs.TypeBool, "", Arithmetic, "CPU quota set",
		"Whether atlas's cgroup has a CPU quota (cpu.max or cpu.cfs_quota_us is a number rather than max). No cpu controller for the cgroup means no quota at this level.",
		"Ancestor cgroups are not read."),
	derived("cgroup.cpu_quota_cpus", obs.TypeFloat, "", Arithmetic, "CPU quota in CPUs",
		"The cgroup's CPU quota expressed as a number of CPUs: quota ÷ period.",
		"Unknown when there is no quota. The tasks may still run on more CPUs at once, briefly, before throttling."),
	derived("cgroup.cpu_throttled_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Share of periods throttled",
		"Of the quota periods in which the cgroup ran, the share in which it was throttled: nr_throttled ÷ nr_periods.",
		"Unknown before any period has elapsed under a quota; counts since the cgroup was created."),
}
