package catalog

import "github.com/ZahakJ/machine-atlas/internal/obs"

// x86Flag declares one /proc/cpuinfo flag, reported as present or not.
func x86Flag(flag, title, desc, caveat string) Entry {
	return Entry{
		ID: "cpu.flags." + flag, Title: title, Description: desc,
		Type: obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     caveat + " An x86 flag: unsupported on other architectures.",
		Privileged: privSame,
	}
}

// osRelease declares one os-release field.
func osRelease(field, title, desc string) Entry {
	return Entry{
		ID: "os.release." + field, Title: title, Description: desc,
		Type: obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "Describes the userspace in this mount namespace: inside a container it is the container image's distribution, not the host's.",
		Privileged: privSame,
	}
}

// cpuCount declares one CPU topology count.
func cpuCount(name, title, desc, caveat string) Entry {
	return Entry{
		ID: "cpu." + name, Title: title, Description: desc, Unit: obs.UnitCount,
		Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Caveat: caveat, Privileged: privSame,
	}
}

// sessionEnv declares one allow-listed session environment fact.
func sessionEnv(name, typ, title, desc string) Entry {
	return Entry{
		ID: "session." + name, Title: title, Description: desc, Type: typ,
		Kind: obs.KindStatic, Scope: obs.ScopeProcess,
		Caveat:     "Read from atlas's own environment, which reflects how atlas was started (a terminal, a service, ssh), not necessarily the desktop the user sees.",
		Privileged: "Nothing: atlas reads only its own environment, and only these names.",
	}
}

var systemEntries = []Entry{
	osRelease("id", "Distribution id", "The machine-readable distribution name from os-release, e.g. arch, debian, alpine."),
	osRelease("id_like", "Related distributions", "Distributions this one derives from or resembles (ID_LIKE), which hints at its packaging conventions."),
	osRelease("name", "Distribution name", "The human-readable distribution name from os-release."),
	osRelease("version_id", "Distribution version", "The distribution release (VERSION_ID); rolling releases usually omit it."),
	{
		ID: "os.hostname", Title: "Hostname",
		Description: "The name of this machine as the kernel knows it, from the UTS namespace atlas runs in.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeNamespace, Sensitivity: Hostname,
		Caveat:     "Inside a container it is the container's name, not the host's. Exports replace it.",
		Privileged: privSame,
	},
	{
		ID: "kernel.release", Title: "Kernel release",
		Description: "The running kernel's version string as uname -r prints it. Many behaviours in these lessons depend on it.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Distribution kernels backport features, so the version number alone does not prove a feature is absent; inside a container this is the host's kernel.",
		Privileged: privSame,
	},
	{
		ID: "kernel.version_string", Title: "Kernel build string",
		Description: "The full /proc/version line: release, the compiler that built the kernel, and the build number and date.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Sensitivity: Freetext,
		Caveat:     "Describes how the kernel was built, not how it is configured.",
		Privileged: privSame,
	},
	{
		ID: "kernel.arch", Title: "Machine architecture",
		Description: "The hardware architecture the kernel runs on, as uname reports it (x86_64, aarch64, riscv64). It decides the page table format and which CPU flags exist.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "A 32-bit userspace on a 64-bit kernel, or emulation, can make this differ from what programs are compiled for.",
		Privileged: privSame,
	},
	{
		ID: "kernel.page_size_bytes", Title: "Base page size",
		Description: "The unit of virtual memory: the size of the smallest range one page-table entry maps, and so the granularity of every mapping, fault and protection change. Its log2 is the number of address offset bits.",
		Unit:        obs.UnitBytes, Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "The base size only; huge pages map larger ranges with single entries higher in the tree.",
		Privileged: privSame,
	},
	{
		ID: "kernel.boot_time", Title: "Boot time",
		Description: "When the kernel booted (btime in /proc/stat), the zero point of every since-boot counter in these lessons.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Derived from the wall clock, so it shifts if the clock was adjusted after boot; in a container it is the host's boot.",
		Privileged: privSame,
	},
	{
		ID: "kernel.uptime_seconds", Title: "Uptime",
		Description: "Seconds since boot, from /proc/uptime: divide any cumulative counter by it for a long-run average rate.",
		Unit:        obs.UnitSeconds, Type: obs.TypeFloat, Kind: obs.KindLive, Scope: obs.ScopeKernel,
		Caveat:     "A long-run average hides bursts; time spent suspended may or may not be counted depending on the clock.",
		Privileged: privSame,
	},
	{
		ID: "cpu.model_name", Title: "CPU model",
		Description: "The processor model string reported by the CPU or firmware.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Sensitivity: Freetext,
		Caveat:     "Absent on many arm64 systems, whose cpuinfo lists implementer and part numbers instead; in a VM the hypervisor chooses what to report.",
		Privileged: privSame,
	},
	{
		ID: "cpu.vendor", Title: "CPU vendor",
		Description: "The CPU vendor id (GenuineIntel, AuthenticAMD) or, on arm64, the implementer code.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "In a VM the hypervisor can present a different vendor.",
		Privileged: privSame,
	},
	cpuCount("logical_count", "Logical CPUs",
		"CPUs the kernel can schedule on: every hardware thread of every core.",
		"Counts CPUs the kernel knows; a container's cpuset or CPU quota can allow it fewer."),
	cpuCount("core_count", "Physical cores",
		"Distinct cores across all sockets. With simultaneous multithreading each core runs more than one logical CPU.",
		"In a VM the topology is whatever the hypervisor describes."),
	cpuCount("socket_count", "CPU sockets",
		"Physical processor packages. Multi-socket machines usually have one NUMA node per socket.",
		"In a VM the topology is whatever the hypervisor describes."),
	cpuCount("threads_per_core", "Threads per core",
		"Logical CPUs per physical core: 2 with SMT (Hyper-Threading) enabled, 1 without.",
		"SMT can be disabled at boot or at runtime; this is the current state."),
	{
		ID: "cpu.address_bits.virtual", Title: "Virtual address bits",
		Description: "How many bits of a virtual address the CPU's MMU translates (48 for 4-level paging, 57 with 5-level). The remaining upper bits of a 64-bit pointer must copy the top translated bit.",
		Unit:        obs.UnitBits, Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "What the CPU supports, not what the kernel uses: a CPU with 57 bits may run a 4-level kernel. Absent on arm64, whose cpuinfo has no address sizes line.",
		Privileged: privSame,
	},
	{
		ID: "cpu.address_bits.physical", Title: "Physical address bits",
		Description: "How many bits a physical address can have, which bounds the size of a page frame number in a page-table entry.",
		Unit:        obs.UnitBits, Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Bounds addressable physical memory; it says nothing about how much RAM is fitted. Absent on arm64.",
		Privileged: privSame,
	},
	x86Flag("la57", "5-level paging in use",
		"Listed when the kernel runs 5-level page tables with 57-bit virtual addresses (LA57). Since Linux 4.17 the kernel clears the flag at boot unless it enabled 5-level paging, so its absence means 4 levels whether or not the CPU could do 5.",
		"Reflects the running kernel, not only the CPU: a kernel built without 5-level support, or booted with no5lvl, hides the flag on an LA57-capable CPU. Before Linux 4.17 the flag showed CPU support alone."),
	x86Flag("pse", "Large page support",
		"Page Size Extension: page directory entries can map large pages directly, the basis of 2 MiB huge pages.",
		"Universal on x86-64; listed for completeness."),
	x86Flag("pdpe1gb", "1 GiB page support",
		"The CPU can map 1 GiB pages with a single entry at the third level of the page table.",
		"Whether 1 GiB pages can be allocated also depends on boot parameters and fragmentation."),
	x86Flag("hypervisor", "Running under a hypervisor",
		"The CPU reports that it is virtualised: a hypervisor set this CPUID bit for the guest.",
		"A hypervisor can hide it, so its absence does not prove bare metal."),
	{
		ID: "cpu.caches", Title: "CPU caches",
		Description: "Each cache level the CPU reports: data, instruction or unified, its size, line size, associativity, and which CPUs share it.",
		Type:        obs.TypeTable, Columns: []string{"level", "type", "size_bytes", "line_size_bytes", "ways", "shared_cpu_list"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Listed for cpu0; often absent or incomplete in VMs and on some arm64 firmware.",
		Privileged: privSame,
	},
	{
		ID: "cpu.online", Title: "Online CPUs",
		Description: "The CPUs currently online, as a CPU list such as 0-15.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Online in the kernel, not necessarily allowed to atlas: a cpuset can restrict which of them a container may use.",
		Privileged: privSame,
	},
	{
		ID: "init.pid1_comm", Title: "PID 1",
		Description: "The command name of process 1 in atlas's PID namespace: the init system on a full machine (systemd, openrc-init, runit), often a shell or application inside a container.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "Only the name; a renamed binary or wrapper reads differently from what is actually running.",
		Privileged: privSame,
	},
	{
		ID: "init.systemd_booted", Title: "Booted with systemd",
		Description: "Whether /run/systemd/system exists, the check systemd itself documents for \"was this system booted with systemd\".",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "Having systemd installed is not the same as running it; this checks the running state.",
		Privileged: privSame,
	},
	{
		ID: "virt.dmi.sys_vendor", Title: "System vendor (DMI)",
		Description: "The system manufacturer from the firmware's DMI tables, which hypervisors also fill in (QEMU, VMware, Microsoft Corporation).",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Sensitivity: Freetext,
		Caveat:     "Absent on machines without DMI, such as most arm64 boards; firmware can contain placeholder text.",
		Privileged: "Root could read serial numbers and UUIDs from DMI; Machine Atlas never reads them.",
	},
	{
		ID: "virt.dmi.product_name", Title: "Product name (DMI)",
		Description: "The product name from the firmware's DMI tables; in a VM, the hypervisor's machine type.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Sensitivity: Freetext,
		Caveat:     "Absent on machines without DMI; firmware can contain placeholder text.",
		Privileged: "Root could read serial numbers and UUIDs from DMI; Machine Atlas never reads them.",
	},
	{
		ID: "virt.container.dockerenv", Title: "Docker marker file",
		Description: "Whether /.dockerenv exists, a file Docker creates in its containers.",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "A convention, not a guarantee: other runtimes do not create it and anyone can.",
		Privileged: privSame,
	},
	{
		ID: "virt.container.containerenv", Title: "Podman marker file",
		Description: "Whether /run/.containerenv exists, a file Podman and some other runtimes create in their containers.",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "A convention, not a guarantee.",
		Privileged: privSame,
	},
	{
		ID: "virt.container.systemd", Title: "Container named by systemd",
		Description: "The contents of /run/systemd/container, which systemd writes when it boots as PID 1 inside a container: the container manager's name, such as docker, lxc, podman or systemd-nspawn. Absent on a host, and in containers whose PID 1 is not systemd.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "Only systemd writes it, so its absence says nothing; the other container signals cover containers without systemd.",
		Privileged: privSame,
	},
	{
		ID: "virt.pid1_cgroup_root", Title: "PID 1 in the root cgroup",
		Description: "Whether process 1 sits at the root of the cgroup hierarchy it can see. On a host, init does; in a container with its own cgroup namespace it also appears at the root, so this is evidence to combine with the others, not proof.",
		Type:        obs.TypeBool, Kind: obs.KindStatic, Scope: obs.ScopeNamespace,
		Caveat:     "Cgroup namespaces make a container's cgroup look like the root, so on its own this cannot tell a container from a host.",
		Privileged: privSame,
	},
	{
		ID: "virt.devicetree.model", Title: "Device tree model",
		Description: "The board model from the device tree, the hardware description arm64 and RISC-V boards hand the kernel in place of DMI tables.",
		Type:        obs.TypeString, Kind: obs.KindStatic, Scope: obs.ScopeKernel, Sensitivity: Freetext,
		Caveat:     "Absent on machines that boot with ACPI and DMI, such as most x86 PCs.",
		Privileged: privSame,
	},
	{
		ID: "block.devices", Title: "Block devices",
		Description: "Block devices from /sys/block: disks, partitions' parents, zram and device-mapper volumes, with size, whether the kernel thinks they rotate, and whether they are removable.",
		Type:        obs.TypeTable, Columns: []string{"name", "size_bytes", "rotational", "removable", "type"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		IdentifyingColumns: []string{"name"}, SharedIdentifiers: map[string]string{"name": devColumn},
		Caveat:     "The rotational flag is what the driver reports; virtual disks often claim to rotate. Unused loop and ram devices are skipped.",
		Privileged: "Root could read SMART data and serial numbers; Machine Atlas never reads them.",
	},
	{
		ID: "block.lsblk", Title: "Block devices (lsblk)",
		Description: "The same devices as reported by the lsblk tool, with filesystem types. Kept to show how a tool's output differs from the kernel interface and between tool versions.",
		Type:        obs.TypeTable, Columns: []string{"name", "type", "size_bytes", "fstype"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		IdentifyingColumns: []string{"name"}, SharedIdentifiers: map[string]string{"name": devColumn},
		Caveat:     "Absent when lsblk is not installed and unsupported when it is too old for --json; block.devices is the primary source.",
		Privileged: "Root lets lsblk read filesystem signatures it otherwise gets from the udev database.",
	},
	{
		ID: "mounts.filesystems", Title: "Mounted filesystems",
		Description: "Filesystems mounted in atlas's mount namespace, with their type, the kind of source (block-device, tmpfs, overlay, network, fuse, pseudo, other), for block devices the device name and the whole disk in /sys/block it belongs to, and the mount options from an allow-list (ro or rw, the atime policy, sync, dirsync, lazytime, discard, commit=, data=, compress=, barrier settings, ssd, nodev, nosuid, noexec).",
		Type:        obs.TypeTable, Columns: []string{"mount_point", "fstype", "source_kind", "device", "disk", "options"},
		Kind: obs.KindStatic, Scope: obs.ScopeNamespace, Sensitivity: Path,
		IdentifyingColumns: []string{"device", "disk"}, SharedIdentifiers: map[string]string{"device": devColumn, "disk": devColumn},
		Caveat:     "Mount points outside a fixed list of standard paths are replaced by <path> at collection, and only allow-listed options are kept (never paths, subvolume names, user ids or overlay layers). disk is found by following /sys/class/block/<device>, or the mount's device number in /sys/dev/block; it is missing when neither leads to a disk (a btrfs subvolume's anonymous device number with a mapper source, or a restricted /sys). Other mount namespaces can see a different tree.",
		Privileged: privSame,
	},
	sessionEnv("type", obs.TypeString, "Session type", "The session type (wayland, x11, tty) from XDG_SESSION_TYPE."),
	sessionEnv("desktop", obs.TypeString, "Desktop", "The desktop environment or compositor from XDG_CURRENT_DESKTOP."),
	sessionEnv("wayland_display_set", obs.TypeBool, "Wayland display available", "Whether WAYLAND_DISPLAY is set, so a Wayland compositor is reachable. Only its presence is recorded."),
	sessionEnv("x11_display_set", obs.TypeBool, "X11 display available", "Whether DISPLAY is set, so an X server (or Xwayland) is reachable. Only its presence is recorded."),
}
