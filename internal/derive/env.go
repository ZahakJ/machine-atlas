package derive

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// knownInits are PID 1 command names of init systems. Inside a container
// PID 1 is usually the application or a shell instead.
var knownInits = []string{"systemd", "init", "openrc-init", "runit", "runit-init", "s6-svscan", "dinit", "shepherd", "sinit", "finit"}

// hypervisorMarks are substrings of DMI vendor or product names, or of a
// device-tree model, that hypervisors write into the tables they present.
var hypervisorMarks = []string{
	"QEMU", "KVM", "VMware", "VirtualBox", "innotek", "Xen", "Bochs", "BHYVE",
	"Parallels", "Virtual Machine", "Google Compute Engine", "OpenStack", "dummy-virt",
}

func envFacts(in inputs) []Fact {
	return []Fact{envKind(in), archFamily(in)}
}

// envKind infers where atlas runs. Positive evidence from any available
// signal is enough to say container or vm-guest; saying "no evidence"
// requires that the container checks and at least one VM signal could
// actually be read. Bare metal is never claimed: every signal can be hidden.
func envKind(in inputs) Fact {
	const id = "derived.env.kind"
	c := in.calc()
	ok := func(id string) (obs.Observation, bool) {
		o := c.look(id)
		return o, o.Status == obs.StatusOK
	}
	var container, vm, checked, unread []string
	note := func(id string, read bool) {
		if read {
			checked = append(checked, id)
		} else {
			unread = append(unread, id)
		}
	}

	for _, m := range []struct{ id, path string }{
		{"virt.container.dockerenv", "/.dockerenv"},
		{"virt.container.containerenv", "/run/.containerenv"},
	} {
		o, read := ok(m.id)
		note(m.id, read)
		if read && o.Value == true {
			container = append(container, m.path+" exists")
		}
	}
	if o, read := ok("virt.container.systemd"); read {
		container = append(container, fmt.Sprintf("systemd, as PID 1, recorded the container manager %q in /run/systemd/container", o.Value))
	} else if s := c.look("virt.container.systemd").Status; s != obs.StatusAbsent && s != obs.StatusUnsupported {
		note("virt.container.systemd", false)
	}
	// PID 1 of a host is always an init system. A PID 1 that is not one
	// (a shell, an application, tini) is a container's, whatever cgroup
	// it appears in: with its own cgroup namespace (the default for Docker
	// and containerd on cgroup v2) it sits at "/", like a host's non-systemd
	// init; without one it sits in a sub-cgroup.
	comm, commRead := ok("init.pid1_comm")
	note("init.pid1_comm", commRead)
	root, rootRead := ok("virt.pid1_cgroup_root")
	if commRead {
		if name, _ := comm.Value.(string); !slices.Contains(knownInits, name) {
			where := ""
			switch {
			case rootRead && root.Value == true:
				where = " and appears at the root of its cgroup hierarchy, as in a container with its own cgroup namespace"
			case rootRead:
				where = " and sits below the cgroup root, as in a container without a cgroup namespace"
			}
			container = append(container, fmt.Sprintf("PID 1 is %q, not an init system%s", name, where))
		}
	}

	vmRead := false
	if o, read := ok("cpu.flags.hypervisor"); read {
		vmRead = true
		if o.Value == true {
			vm = append(vm, "the CPU reports the hypervisor flag")
		}
	} else {
		note("cpu.flags.hypervisor", false)
	}
	for _, sid := range []string{"virt.dmi.sys_vendor", "virt.dmi.product_name", "virt.devicetree.model"} {
		o, read := ok(sid)
		if !read {
			note(sid, false)
			continue
		}
		vmRead = true
		if s, _ := o.Value.(string); markedByHypervisor(s) {
			vm = append(vm, fmt.Sprintf("%s is %q", sid, s))
		}
	}

	switch {
	case len(container) > 0:
		rule := "container, because " + strings.Join(container, " and ")
		if len(vm) > 0 {
			rule += "; it also runs in a VM, because " + strings.Join(vm, " and ")
		}
		return c.fact(id, rule, "container")
	case len(vm) > 0:
		return c.fact(id, "vm-guest, because "+strings.Join(vm, " and "), "vm-guest")
	}
	containerChecks := slices.Contains(checked, "virt.container.dockerenv") && slices.Contains(checked, "virt.container.containerenv") &&
		slices.Contains(checked, "init.pid1_comm")
	if !containerChecks || !vmRead {
		c.fail("no positive evidence, and too few signals could be read to say there is none (unavailable: %s)", strings.Join(unread, ", "))
		return c.fact(id, "container if a marker file exists, systemd recorded a container manager, or PID 1 is not an init system; vm-guest if the hypervisor flag, DMI or device tree name a hypervisor", nil)
	}
	rule := fmt.Sprintf("no-virtualization-evidence: no container marker file, systemd recorded no container manager, PID 1 is an init system (%q), and neither the CPU flags, DMI nor the device tree name a hypervisor. This is not proof of bare metal: a container whose PID 1 is an init other than systemd shows none of these signals, and a hypervisor or runtime can hide every one of them", comm.Value)
	if len(unread) > 0 {
		rule += fmt.Sprintf(" (not available: %s)", strings.Join(unread, ", "))
	}
	return c.fact(id, rule, "no-virtualization-evidence")
}

func markedByHypervisor(s string) bool {
	for _, m := range hypervisorMarks {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// archFamily groups machine names by page-table format family.
func archFamily(in inputs) Fact {
	c := in.calc()
	arch := c.str("kernel.arch")
	family := familyOf(arch)
	return c.fact("derived.env.arch_family",
		fmt.Sprintf("kernel.arch %q belongs to the %s family (x86: x86_64, i?86; arm: aarch64, arm*; riscv: riscv*)", arch, family), family)
}

func familyOf(arch string) string {
	switch {
	case arch == "x86_64" || arch == "amd64" || (len(arch) == 4 && arch[0] == 'i' && strings.HasSuffix(arch, "86")):
		return "x86"
	case arch == "aarch64" || arch == "arm64" || strings.HasPrefix(arch, "arm"):
		return "arm"
	case strings.HasPrefix(arch, "riscv"):
		return "riscv"
	}
	return "other"
}
