---
{
  "id": "cpu-frequency",
  "title": "Why the clock speed keeps changing",
  "subsystem": "cpu",
  "kind": "core",
  "order": 2,
  "summary": "Frequency scaling: why running slower saves so much power, cpufreq policies, drivers and governors, hardware-managed P-states with an energy-performance preference, turbo, and why a virtual machine has none of it.",
  "prerequisites": ["cpu-topology"],
  "uses": [
    "cpu.freq.amd_pstate.status",
    "cpu.freq.boost",
    "cpu.freq.intel_pstate.no_turbo",
    "cpu.freq.intel_pstate.status",
    "cpu.freq.policies",
    "derived.cpu.freq.available",
    "derived.cpu.freq.boost_enabled",
    "derived.cpu.freq.driver",
    "derived.cpu.freq.governor",
    "derived.env.kind"
  ],
  "relevance": [
    {"when": "derived.cpu.freq.available", "is": false, "note": "The kernel controls no CPU frequency here, so the lesson's no-cpufreq branch applies."}
  ],
  "next": ["run-queues-and-load"],
  "deeper": ["planned:cpu-idle-states"],
  "version_notes": [
    {"applies": "Linux ≥ 4.7", "text": "The schedutil governor sets frequencies from the scheduler's own utilisation figures instead of sampling idle time as ondemand does."},
    {"applies": "Linux ≥ 5.7 (x86)", "text": "Frequency invariance: the scheduler scales a task's measured utilisation by the frequency it ran at, so a task that kept a slow CPU busy is not mistaken for a heavy one."},
    {"applies": "Linux ≥ 6.3", "text": "amd-pstate gained its active (EPP) mode, in which the processor chooses frequencies guided by an energy-performance preference, as intel_pstate does with HWP; later kernels made it the default on many Zen systems."}
  ],
  "references": [
    {"title": "Linux kernel documentation: CPU Performance Scaling (cpufreq)", "url": "https://docs.kernel.org/admin-guide/pm/cpufreq.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: intel_pstate CPU Performance Scaling Driver", "url": "https://docs.kernel.org/admin-guide/pm/intel_pstate.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: amd-pstate CPU Performance Scaling Driver", "url": "https://docs.kernel.org/admin-guide/pm/amd-pstate.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Schedutil", "url": "https://docs.kernel.org/scheduler/schedutil.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: CPU Idle Time Management", "url": "https://docs.kernel.org/admin-guide/pm/cpuidle.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: General description of the CPUFreq core and drivers", "url": "https://docs.kernel.org/cpu-freq/cpu-drivers.html", "kind": "kernel-doc"},
    {"title": "Intel 64 and IA-32 Architectures Software Developer's Manual, Volume 3B, Power and Thermal Management (Hardware-Controlled Performance States)", "url": "https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html", "kind": "spec"},
    {"title": "cpupower-frequency-info(1)", "url": "https://man7.org/linux/man-pages/man1/cpupower-frequency-info.1.html", "kind": "man-page"}
  ]
}
---
## Question

:::when derived.cpu.freq.available
The policy table further down shows the frequency each group of CPUs here was running at when Machine Atlas looked, somewhere between its hardware limits, and a second look would show different numbers. The clock is set by the {{derived:derived.cpu.freq.driver}} driver under the {{derived:derived.cpu.freq.governor}} governor. Who decides the speed, how often, and why not simply run flat out all the time?
:::else
This system shows no CPU frequency control at all: the kernel has no cpufreq policy here ({{obs:cpu.freq.policies}}). The processor underneath still changes speed; something other than this kernel decides. What does frequency scaling do on a machine that has it, and who decides here instead?
:::unknown
Machine Atlas could not read the CPU frequency settings here ({{obs:cpu.freq.policies}}). On most machines the processors change speed many times a second. Who decides the speed, how often, and why not simply run flat out all the time?
:::end

## Why it exists

The power a processor draws while it switches grows with its frequency times the square of its voltage, and a higher frequency needs a higher voltage. So power rises roughly with the cube of frequency: running 30 percent slower can save more than half the power. Most of the time a desktop or server has little to do, and running fast while waiting for the next keystroke or packet only turns electricity into heat.

The opposite also holds. A burst of work finishes sooner at a high frequency, after which the core can drop into a deep idle state, so racing to idle is often the cheaper choice for a short task. And processors can briefly exceed their rated frequency, as long as the package stays within its power and temperature limits. Somebody has to keep choosing, thousands of times a second, between these. In Linux that is the cpufreq subsystem, often in partnership with the processor itself.

## How it works

### Performance states and policies

A processor offers a range of *performance states* (P-states): frequency and voltage pairs it can switch between in tens of microseconds. CPUs that share a clock or a voltage rail must change together; the kernel groups them in a *policy*, one directory per group in `/sys/devices/system/cpu/cpufreq/policyN`. On most x86 machines each logical CPU is its own policy; on many arm boards all the cores of a cluster share one.

Each policy has hardware limits (`cpuinfo_min_freq`, `cpuinfo_max_freq`), software limits within them (`scaling_min_freq`, `scaling_max_freq`), and a current value, all in kHz.

### Drivers

A *scaling driver* knows how to talk to the hardware:

- `acpi-cpufreq` writes P-state requests that the ACPI firmware tables describe; it is the generic x86 driver.
- `intel_pstate` drives Intel processors directly. In *active* mode with hardware P-states (HWP) it hands the processor a range and an *energy-performance preference* (EPP) and lets the processor pick the frequency itself, many times faster than software could. In *passive* mode it behaves like an ordinary driver under the generic governors.
- `amd-pstate` does the same for AMD processors through the CPPC interface, with active, guided and passive modes.
- `cpufreq-dt` and others drive arm and RISC-V systems from operating points listed in the device tree.

### Governors

A *governor* decides what frequency to ask for:

- `performance` asks for the maximum allowed, `powersave` for the minimum; with intel_pstate or amd-pstate in active mode the two words mean something different: they become hints that set the EPP, and the processor still varies the frequency.
- `schedutil` asks for a frequency proportional to how busy the scheduler says the CPU is: the utilisation it tracks for every task, plus a boost for tasks that just waited for I/O. Because it hears from the scheduler at every update, it reacts within a scheduler tick.
- `ondemand` and `conservative` sample idle time periodically and step up or down; `userspace` lets a program choose.

### Turbo, boost and limits

Above the base frequency lie turbo or boost frequencies. They are allowed, not promised: the processor grants them only while few enough cores are active and the package is within its power and thermal limits, so a single busy core may run far faster than all cores together can. `intel_pstate/no_turbo` or `cpufreq/boost` switches the range off.

### Idle states are separate

When a CPU has nothing to run it does not merely run slowly: the cpuidle subsystem puts it into an idle state (a C-state), from clock-gated to powered off, chosen by how long the idle period is expected to last. Frequency describes a running CPU; idle states describe a waiting one, and a deeper one costs more time to wake.

### In a virtual machine

A guest kernel usually has no cpufreq at all: the hypervisor owns the physical processor's frequency, and the guest's virtual CPUs run at whatever speed the host gives the threads behind them. The `cpu MHz` line of `/proc/cpuinfo` inside a guest is the frequency the hypervisor reported at boot, not a measurement.

## On this machine

:::evidence Frequency scaling here
- derived.cpu.freq.available
- derived.cpu.freq.driver
- derived.cpu.freq.governor
- derived.cpu.freq.boost_enabled
- cpu.freq.boost | Global boost switch (acpi-cpufreq, amd-pstate)
- cpu.freq.intel_pstate.status
- cpu.freq.intel_pstate.no_turbo
- cpu.freq.amd_pstate.status
:::end

:::when derived.cpu.freq.available
:::table cpu.freq.policies columns=policy,cpus,driver,governor,cur_khz,min_khz,max_khz,hw_min_khz,hw_max_khz,epp limit=8
:::else
There is no policy table to show: the kernel here controls no CPU frequency.
:::unknown
The frequency policies could not be read here ({{obs:cpu.freq.policies}}).
:::end

:::when derived.cpu.freq.driver is intel_pstate
:::when cpu.freq.intel_pstate.status is active
This machine uses intel_pstate in active mode: the {{derived:derived.cpu.freq.governor}} governor here is not the generic governor of that name but a hint, and with hardware P-states the processor itself picks each frequency inside the limits, steered by the energy-performance preference in the `epp` column. The `cur_khz` column is the driver's last reading, already out of date.
:::else
This machine uses intel_pstate in {{obs:cpu.freq.intel_pstate.status}} mode, acting as an ordinary scaling driver under the {{derived:derived.cpu.freq.governor}} governor, which chooses the frequencies itself.
:::end
:::else
:::when derived.cpu.freq.driver is amd-pstate-epp
This machine uses amd-pstate in its EPP mode (its status reads {{obs:cpu.freq.amd_pstate.status}}): the {{derived:derived.cpu.freq.governor}} governor is a hint, and the processor chooses each frequency inside the limits, steered by the energy-performance preference in the `epp` column.
:::else
The scaling driver here is {{derived:derived.cpu.freq.driver}} under the {{derived:derived.cpu.freq.governor}} governor: the kernel itself chooses each frequency and asks the hardware for it. With schedutil it follows the scheduler's measure of how busy each CPU is, updated at every scheduler event.
:::end
:::unknown
No scaling driver can be named here. Either the kernel controls no CPU frequency, as in most virtual machines, or its settings could not be read; the evidence above says which.
:::end

:::when derived.cpu.freq.boost_enabled
Frequencies above the base clock are allowed here. Whether a core reaches them at any moment depends on how many cores are busy, the temperature and the power limit, none of which this table shows.
:::else
Frequencies above the base clock are switched off here, so the `hw_max_khz` column is the ceiling in practice.
:::unknown
Whether frequencies above the base clock are allowed could not be determined here: there is no boost switch Machine Atlas can read, because the kernel controls no frequency, its driver has none, or the files are hidden.
:::end

:::when derived.env.kind is vm-guest
Machine Atlas found evidence of a virtual machine. Its virtual CPUs run at whatever speed the host's processor runs the threads behind them, and the host's frequency decisions, made for all its guests together, are invisible here.
:::else
:::when derived.env.kind is container
Atlas appears to run in a container. Frequency scaling is a property of the host kernel and its hardware; the container sees the host's settings, if the host exposes them at all, and cannot change them.
:::else
No virtualisation was detected, so the policies above, where present, belong to this kernel and its own processor.
:::end
:::unknown
Whether atlas runs in a virtual machine or a container could not be determined here. In either, the frequency of the physical processor is decided by the host, out of sight of this system.
:::end

## Predict

:::predict
On a machine whose cpufreq governor is `performance`, a single-threaded program runs flat out for a minute. Will its core stay at the hardware maximum the whole time?

- Yes: performance means the maximum frequency.
- No: the maximum is a turbo frequency that power and temperature limits may not sustain.
- No: the governor lowers the frequency when the program is not waiting for I/O.
:::reveal
Usually not, for the second reason. The hardware maximum is typically a turbo frequency, and the processor grants it only while the package stays within its power and thermal budget; after some seconds of sustained load many desktop and laptop processors settle lower, and a server whose other cores are also busy may never reach it. With intel_pstate or amd-pstate in active mode, the processor makes this choice itself, whatever the governor's name.

The first answer describes what the governor asks for, not what the hardware grants. The third has the logic of schedutil backwards: a program that never waits keeps the CPU fully utilised, which is when every governor raises the frequency.

:::when derived.cpu.freq.available
Here the hardware maximum is in the `hw_max_khz` column above, and the governor is {{derived:derived.cpu.freq.governor}}.
:::else
On this machine the question cannot be watched from inside: no cpufreq policy exists.
:::unknown
Here the frequency policies could not be read, so the question cannot be watched from this account.
:::end
:::end

## Try it

Watch the current frequency of every CPU, read-only:

```sh
grep . /sys/devices/system/cpu/cpufreq/policy*/scaling_cur_freq
grep MHz /proc/cpuinfo
```

Run either twice, a second apart, while the machine is idle, then while something busy runs (a compilation, a video). On a hardware-managed driver expect wide jumps between readings; under schedutil the busy CPUs move to high frequencies within milliseconds. Inside a virtual machine the first command fails, because the directory does not exist, and the second prints the same value every time.

`cpupower frequency-info` (in the kernel's tools, packaged as `cpupower` or `linux-tools`) summarises a policy's driver, governor and limits. `turbostat`, which needs root, reads the actual average frequency from the processor's own counters; nothing unprivileged can.

## Limits

:::misconception The powersave governor makes the machine slow
With intel_pstate or amd-pstate in active mode, powersave is a preference, not a fixed low frequency: the processor still raises the frequency under load, only less eagerly. Under the generic governors (acpi-cpufreq, cpufreq-dt) powersave does pin the minimum, which is why the same word behaves so differently on different machines.
:::end

:::misconception Lower frequency always saves energy
For a fixed amount of work, running slowly keeps the whole core, its caches and the rest of the package powered for longer. Finishing fast and dropping into a deep idle state often costs less energy in total, which is why governors raise the frequency quickly when work arrives.
:::end

:::misconception The cpu MHz line shows the processor's speed
It is a single reading, already old when it is printed, and on many systems an estimate. Inside a virtual machine it is the figure the hypervisor gave at boot.
:::end

:::cannot
The frequencies above are the kernel's requests and its last readings, taken once. The real frequency changes every few milliseconds, and the average over any interval needs the processor's own cycle counters (APERF and MPERF on x86), which only root can read.

Nothing here shows why a core is not at its maximum: power limits, temperature, the number of active cores and the firmware's own policy are all hidden from unprivileged programs, as are the idle states the cores spend their quiet time in.

In a virtual machine or container, the host's frequency decisions are invisible.
:::end

## Go deeper

Next, [run queues and load](lesson:run-queues-and-load) looks at the work these CPUs are given: who is waiting for them and how long. In Explore, the [frequency scaling](component:cpufreq) component shows what the governor depends on. A planned lesson covers CPU idle states, the other half of power management.
