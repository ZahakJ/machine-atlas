package system

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var initCollector = collector{
	id:   "system.init",
	owns: []string{"init.pid1_comm", "init.systemd_booted"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{
			stringFile(env, "init.pid1_comm", "/proc/1/comm"),
			// sd_booted(3) defines "booted with systemd" as this directory
			// existing; it is created by systemd as PID 1 and by nothing else.
			existsObs(env, "init.systemd_booted", "/run/systemd/system"),
		}
	},
}

// The DMI files read. Only these two: the same directory holds
// product_serial, product_uuid, board_serial and chassis_asset_tag, which
// identify one physical machine and are never opened.
var dmiFiles = []struct{ id, path string }{
	{"virt.dmi.sys_vendor", "/sys/class/dmi/id/sys_vendor"},
	{"virt.dmi.product_name", "/sys/class/dmi/id/product_name"},
}

var virtCollector = collector{
	id: "system.virt",
	owns: []string{
		"virt.dmi.sys_vendor", "virt.dmi.product_name",
		"virt.container.dockerenv", "virt.container.containerenv", "virt.container.systemd",
		"virt.pid1_cgroup_root", "virt.devicetree.model",
	},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		var out []obs.Observation
		for _, f := range dmiFiles {
			o := stringFile(env, f.id, f.path)
			if o.Status == obs.StatusAbsent {
				o.Note = "no DMI tables: usual on arm64 and RISC-V boards, which describe hardware with a device tree"
			}
			out = append(out, o)
		}
		out = append(out,
			existsObs(env, "virt.container.dockerenv", "/.dockerenv"),
			existsObs(env, "virt.container.containerenv", "/run/.containerenv"),
			systemdContainer(env),
			pid1CgroupRoot(env),
			deviceTreeModel(env),
		)
		return out
	},
}

// systemdContainer reads /run/systemd/container, which systemd writes at
// boot only when it runs as PID 1 inside a container, naming the runtime
// ("docker", "lxc", "systemd-nspawn", "podman"). It catches the case the
// other signals miss: a container whose PID 1 is a real init. The value is
// kept only if it is one short lowercase word.
func systemdContainer(env collect.Env) obs.Observation {
	const id, path = "virt.container.systemd", "/run/systemd/container"
	o := stringFile(env, id, path)
	if o.Status != obs.StatusOK {
		return o
	}
	s, _ := o.Value.(string)
	if len(s) > 32 || strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
		return obs.Failed(id, o.Source, "contents are not a container manager name")
	}
	return o
}

func pid1CgroupRoot(env collect.Env) obs.Observation {
	const id, path = "virt.pid1_cgroup_root", "/proc/1/cgroup"
	src := obs.File(path, "")
	src.Detail = "true when every hierarchy lists PID 1 at /"
	paths, err := readParsed(env, path, parseCgroupPaths)
	if err != nil {
		return obs.Errored(id, src, err)
	}
	root := true
	for _, p := range paths {
		root = root && p == "/"
	}
	o := obs.OK(id, root, src)
	for _, p := range paths {
		if p == "/init.scope" {
			// Worth saying: on a systemd host the answer is false although
			// no container is involved.
			o.Note = "PID 1 is in /init.scope: systemd moves itself out of the root cgroup, so false here is expected on a systemd host"
		}
	}
	return o
}

// parseCgroupPaths parses /proc/<pid>/cgroup ("hierarchy-id:controllers:path"
// per line; cgroup v2 is the single line "0::/path") and returns the paths.
// Paths are compared, never reported: inside a container they can contain
// the container id.
func parseCgroupPaths(data []byte) ([]string, error) {
	var paths []string
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], "/") {
			return nil, fmt.Errorf("cgroup line %d: expected id:controllers:/path, got %q", i+1, line)
		}
		paths = append(paths, parts[2])
	}
	if len(paths) == 0 || paths[0] == "" {
		return nil, fmt.Errorf("cgroup file is empty")
	}
	return paths, nil
}

func deviceTreeModel(env collect.Env) obs.Observation {
	const id, path = "virt.devicetree.model", "/proc/device-tree/model"
	src := obs.File(path, "")
	data, err := env.ReadFile(path)
	if err != nil {
		o := obs.Errored(id, src, err)
		if o.Status == obs.StatusAbsent {
			o.Note = "no device tree: the firmware described this machine with ACPI and DMI instead"
		}
		return o
	}
	// Device-tree strings are NUL-terminated.
	model := strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
	if model == "" || strings.ContainsRune(model, 0) {
		return obs.Failed(id, src, "device-tree model is empty or not a single string")
	}
	return obs.OK(id, model, src)
}
