package system

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const rootsDir = "../../../fixtures/roots"

// want is an expected observation: its status and, when value is not nil,
// its value.
type want struct {
	status obs.Status
	value  any
}

func ok(v any) want { return want{obs.StatusOK, v} }

var (
	absent      = want{status: obs.StatusAbsent}
	denied      = want{status: obs.StatusPermissionDenied}
	unsupported = want{status: obs.StatusUnsupported}
)

// fixtureWants lists, per root, the observations that make that root
// interesting. Every other observation only has to be not failed.
var fixtureWants = map[string]map[string]want{
	"desktop-x86_64": {
		"os.release.id":               ok("arch"),
		"os.release.version_id":       absent,
		"kernel.release":              ok("6.12.10-arch1-1"),
		"kernel.arch":                 ok("x86_64"),
		"kernel.page_size_bytes":      ok(int64(4096)),
		"kernel.boot_time":            ok("2025-01-17T15:26:57Z"),
		"cpu.model_name":              ok("Intel(R) Core(TM) i7-10700K CPU @ 3.80GHz"),
		"cpu.logical_count":           ok(int64(16)),
		"cpu.core_count":              ok(int64(8)),
		"cpu.socket_count":            ok(int64(1)),
		"cpu.threads_per_core":        ok(int64(2)),
		"cpu.address_bits.virtual":    ok(int64(48)),
		"cpu.flags.la57":              ok(false),
		"cpu.flags.pdpe1gb":           ok(true),
		"cpu.flags.hypervisor":        ok(false),
		"cpu.online":                  ok("0-15"),
		"init.pid1_comm":              ok("systemd"),
		"init.systemd_booted":         ok(true),
		"virt.pid1_cgroup_root":       ok(false),
		"virt.devicetree.model":       absent,
		"virt.container.dockerenv":    ok(false),
		"block.lsblk":                 {status: obs.StatusOK},
		"session.type":                ok("wayland"),
		"session.wayland_display_set": ok(true),
	},
	"rpi-arm64-noswap": {
		"kernel.arch":               ok("aarch64"),
		"cpu.model_name":            absent,
		"cpu.vendor":                ok("0x41"),
		"cpu.address_bits.virtual":  absent,
		"cpu.address_bits.physical": absent,
		"cpu.flags.la57":            unsupported,
		"cpu.flags.pse":             unsupported,
		"cpu.flags.pdpe1gb":         unsupported,
		"cpu.flags.hypervisor":      unsupported,
		"cpu.logical_count":         ok(int64(4)),
		"cpu.core_count":            ok(int64(4)),
		"cpu.threads_per_core":      ok(int64(1)),
		"virt.dmi.sys_vendor":       absent,
		"virt.dmi.product_name":     absent,
		"virt.devicetree.model":     ok("Raspberry Pi 4 Model B Rev 1.4"),
		"session.type":              ok("tty"),
		"session.desktop":           absent,
		"session.x11_display_set":   ok(false),
	},
	"alpine-container": {
		"os.release.id":               ok("alpine"),
		"os.release.id_like":          absent,
		"init.pid1_comm":              ok("sh"),
		"init.systemd_booted":         ok(false),
		"virt.container.dockerenv":    ok(true),
		"virt.container.containerenv": ok(false),
		"virt.pid1_cgroup_root":       ok(true),
		"virt.dmi.sys_vendor":         ok("Amazon EC2"),
		"cpu.flags.hypervisor":        ok(true),
		"cpu.core_count":              ok(int64(2)),
		"cpu.threads_per_core":        ok(int64(2)),
		"block.lsblk":                 absent,
		"session.type":                absent,
	},
	"numa-server-la57": {
		"cpu.logical_count":        ok(int64(64)),
		"cpu.core_count":           ok(int64(32)),
		"cpu.socket_count":         ok(int64(2)),
		"cpu.threads_per_core":     ok(int64(2)),
		"cpu.flags.la57":           ok(true),
		"cpu.address_bits.virtual": ok(int64(57)),
		"os.release.id_like":       ok("debian"),
		"virt.dmi.product_name":    ok("Super Server"),
	},
	"old-vm-cgroupv1": {
		"kernel.release":        ok("4.19.113-300.el7.x86_64"),
		"os.release.id":         ok("centos"),
		"os.release.id_like":    ok("rhel fedora"),
		"cpu.socket_count":      ok(int64(2)),
		"cpu.core_count":        ok(int64(2)),
		"cpu.threads_per_core":  ok(int64(1)),
		"cpu.flags.hypervisor":  ok(true),
		"virt.dmi.sys_vendor":   ok("QEMU"),
		"virt.pid1_cgroup_root": ok(true),
		"block.lsblk":           unsupported,
		"session.type":          absent,
	},
	"restricted": {
		"init.pid1_comm":        denied,
		"init.systemd_booted":   ok(true),
		"virt.pid1_cgroup_root": denied,
		"virt.dmi.sys_vendor":   denied,
		"virt.dmi.product_name": denied,
		"virt.devicetree.model": absent,
		"block.devices":         denied,
		"block.lsblk":           denied,
		"cpu.caches":            denied,
		"cpu.online":            denied,
		"cpu.logical_count":     ok(int64(4)),
		"cpu.core_count":        ok(int64(4)),
		"cpu.socket_count":      ok(int64(1)),
		"cpu.threads_per_core":  ok(int64(1)),
		"session.type":          absent,
	},
}

func fixtureRoots(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	for _, e := range entries {
		if e.IsDir() {
			roots = append(roots, e.Name())
		}
	}
	return roots
}

func runFixture(t *testing.T, dir string) (collect.Report, map[string]obs.Observation) {
	t.Helper()
	env, err := collect.OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	rep, all := collect.Run(context.Background(), env, Collectors())
	byID := map[string]obs.Observation{}
	for _, o := range all {
		byID[o.ID] = o
	}
	return rep, byID
}

// TestFixtureRoots runs every system collector over every fixture root and
// checks the statuses and values each root exists to demonstrate. No
// observation may be failed: a fixture is a machine whose interfaces all
// answered, even if the answer is "no" or "not allowed".
func TestFixtureRoots(t *testing.T) {
	roots := fixtureRoots(t)
	if len(roots) != len(fixtureWants) {
		t.Errorf("found roots %v, want the %d of ARCHITECTURE.md §12", roots, len(fixtureWants))
	}
	for _, name := range roots {
		t.Run(name, func(t *testing.T) {
			rep, byID := runFixture(t, filepath.Join(rootsDir, name))
			for _, c := range rep.Collectors {
				if c.Error != "" {
					t.Errorf("collector %s: %s", c.ID, c.Error)
				}
			}
			for id, o := range byID {
				if o.Status == obs.StatusFailed {
					t.Errorf("%s failed: %s", id, o.Note)
				}
				if o.Status != obs.StatusOK && o.Note == "" {
					t.Errorf("%s is %s without a note", id, o.Status)
				}
			}
			for id, w := range fixtureWants[name] {
				o, found := byID[id]
				switch {
				case !found:
					t.Errorf("%s not emitted", id)
				case o.Status != w.status:
					t.Errorf("%s: status %s (%s), want %s", id, o.Status, o.Note, w.status)
				case w.value != nil && !reflect.DeepEqual(o.Value, w.value):
					t.Errorf("%s = %#v, want %#v", id, o.Value, w.value)
				}
			}
		})
	}
}

// TestFixtureTables checks table observations whose shape the lessons rely
// on: skipped devices, hidden mount points, the cpuinfo fallback note.
func TestFixtureTables(t *testing.T) {
	_, desk := runFixture(t, filepath.Join(rootsDir, "desktop-x86_64"))
	devs := desk["block.devices"].Value.([]map[string]any)
	if len(devs) != 2 || devs[0]["name"] != "sda" || devs[1]["rotational"] != true {
		t.Errorf("desktop block.devices = %v; want sda and a rotational sdb, unused loops skipped", devs)
	}
	for _, row := range desk["mounts.filesystems"].Value.([]map[string]any) {
		mp := row["mount_point"].(string)
		if strings.Contains(mp, "user") || strings.Contains(mp, "docker") || strings.Contains(mp, "media") {
			t.Errorf("non-standard mount point %q kept verbatim", mp)
		}
	}
	caches := desk["cpu.caches"].Value.([]map[string]any)
	if len(caches) != 4 || caches[3]["size_bytes"] != int64(16<<20) || caches[0]["shared_cpu_list"] != "0,8" {
		t.Errorf("desktop caches = %v", caches)
	}

	_, ctr := runFixture(t, filepath.Join(rootsDir, "alpine-container"))
	var loops int
	for _, d := range ctr["block.devices"].Value.([]map[string]any) {
		if d["type"] == "loop" {
			loops++
		}
	}
	if loops != 2 {
		t.Errorf("container lists %d loop devices, want the 2 in use", loops)
	}

	// Mounts traced to their disks: a partition, a device-mapper volume
	// through its device number, a container's /dev/root bind mounts, and
	// none where /sys is root-only.
	for root, want := range map[string]map[string]string{
		"desktop-x86_64":   {"/": "sda", "/boot": "sda"},
		"rpi-arm64-noswap": {"/": "mmcblk0"},
		"old-vm-cgroupv1":  {"/": "dm-0", "/boot": "vda"},
		"alpine-container": {"/etc/hosts": "nvme0n1", "/": ""},
		"restricted":       {"/": ""},
	} {
		_, byID := runFixture(t, filepath.Join(rootsDir, root))
		for _, row := range byID["mounts.filesystems"].Value.([]map[string]any) {
			mp, _ := row["mount_point"].(string)
			if w, ok := want[mp]; ok {
				if got, _ := row["disk"].(string); got != w {
					t.Errorf("%s %s: disk %q, want %q", root, mp, got, w)
				}
			}
		}
	}

	_, rst := runFixture(t, filepath.Join(rootsDir, "restricted"))
	if n := rst["cpu.core_count"].Note; !strings.Contains(n, "/proc/cpuinfo") {
		t.Errorf("restricted cpu.core_count note %q does not mention the cpuinfo fallback", n)
	}
}

// writeRoot builds a minimal fixture root in a temporary directory.
func writeRoot(t *testing.T, meta string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["_meta.json"] = meta
	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if target, ok := strings.CutPrefix(content, "->"); ok {
			if err := os.Symlink(target, full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestMountDisks covers how a mount's device is traced to its disk: a
// partition through /sys/class/block, a whole device, a mapper name and
// /dev/root through the device number, and the cases that leave it out.
func TestMountDisks(t *testing.T) {
	const pci = "../../devices/pci0000:00/0000:00:1d.0/nvme/nvme0/"
	dir := writeRoot(t, minimalMeta, map[string]string{
		"/proc/self/mountinfo": "20 1 259:2 / / rw,noatime - ext4 /dev/nvme0n1p2 rw,commit=30\n" +
			"21 20 253:0 / /var rw - xfs /dev/mapper/vg-var rw,attr2\n" +
			"22 20 259:2 /x /etc/hosts rw - ext4 /dev/root rw\n" +
			"23 20 8:0 / /srv rw - ext4 /dev/sda rw\n" +
			"24 20 0:44 / /home rw - btrfs /dev/mapper/home rw,subvol=/@home\n" +
			"25 20 7:0 / /opt rw - squashfs /dev/loop0 ro\n",
		"/sys/class/block/nvme0n1p2": "->" + pci + "nvme0n1/nvme0n1p2",
		"/sys/class/block/sda":       "->../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda",
		"/sys/dev/block/253:0":       "->../../devices/virtual/block/dm-0",
		"/sys/dev/block/259:2":       "->" + pci + "nvme0n1/nvme0n1p2",
		"/sys/class/block/loop0":     "->../../devices/virtual/block/loop0",
		"/sys/block/nvme0n1/size":    "1000\n",
		"/sys/block/dm-0/size":       "1000\n",
		"/sys/block/sda/size":        "1000\n",
	})
	_, byID := runFixture(t, dir)
	rows := byID["mounts.filesystems"].Value.([]map[string]any)
	want := []any{"nvme0n1", "dm-0", "nvme0n1", "sda", nil, nil}
	for i, w := range want {
		if rows[i]["disk"] != w {
			t.Errorf("row %d (%v): disk %v, want %v", i, rows[i]["device"], rows[i]["disk"], w)
		}
	}
	if rows[0]["options"] != "rw,noatime,commit=30" || rows[4]["options"] != "rw" {
		t.Errorf("options %v, %v", rows[0]["options"], rows[4]["options"])
	}
}

const minimalMeta = `{"description": "test", "now": "2025-01-01T00:00:00Z",
	"uname": {"machine": "riscv64", "release": "6.6.0"}, "page_size": 4096,
	"commands": {"lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE": {"stdout": "_cmd/lsblk.txt"}}}`

// TestStatusPaths covers what the shipped roots do not: the os-release
// fallback, the RISC-V cpuinfo dialect, a mount table over the row cap,
// and malformed files that must produce failed observations.
func TestStatusPaths(t *testing.T) {
	var mounts strings.Builder
	for i := range maxMountRows + 5 {
		mounts.WriteString("1 1 0:1 / /run/x" + strings.Repeat("y", i%3) + " rw - tmpfs tmpfs rw\n")
	}
	dir := writeRoot(t, minimalMeta, map[string]string{
		"/usr/lib/os-release":            "ID=example\nNAME=Example\n",
		"/proc/cpuinfo":                  riscvCPUInfo,
		"/proc/stat":                     "cpu 1 2 3\n",
		"/proc/uptime":                   "12\n",
		"/proc/self/mountinfo":           mounts.String(),
		"/proc/device-tree/model":        "StarFive VisionFive 2 v1.3B\x00",
		"/_cmd/lsblk.txt":                "NAME TYPE\nsda disk\n",
		"/sys/devices/system/cpu/online": "0\n",
		"/sys/devices/system/cpu/cpu0/topology/physical_package_id":  "0\n",
		"/sys/devices/system/cpu/cpu0/topology/thread_siblings_list": "0\n",
		"/sys/devices/system/cpu/cpu0/cache/index0/size":             "32Q\n",
		"/sys/block/vda/size": "lots\n",
	})
	_, byID := runFixture(t, dir)
	checks := map[string]want{
		"os.release.id":               ok("example"),
		"os.release.version_id":       absent,
		"cpu.model_name":              ok("sifive,u74-mc"),
		"cpu.vendor":                  ok("0x489"),
		"cpu.address_bits.virtual":    absent,
		"cpu.flags.la57":              unsupported,
		"cpu.logical_count":           ok(int64(1)),
		"cpu.caches":                  {status: obs.StatusFailed},
		"virt.devicetree.model":       ok("StarFive VisionFive 2 v1.3B"),
		"kernel.boot_time":            {status: obs.StatusFailed},
		"kernel.uptime_seconds":       {status: obs.StatusFailed},
		"kernel.release":              absent,
		"block.lsblk":                 {status: obs.StatusFailed},
		"block.devices":               {status: obs.StatusFailed},
		"init.systemd_booted":         ok(false),
		"virt.dmi.sys_vendor":         absent,
		"session.wayland_display_set": ok(false),
	}
	for id, w := range checks {
		o := byID[id]
		if o.Status != w.status || (w.value != nil && !reflect.DeepEqual(o.Value, w.value)) {
			t.Errorf("%s = %s %#v (%s), want %s %#v", id, o.Status, o.Value, o.Note, w.status, w.value)
		}
	}
	if n := byID["cpu.address_bits.virtual"].Note; !strings.Contains(n, "sv39") {
		t.Errorf("riscv address bits note %q does not name the paging mode", n)
	}
	m := byID["mounts.filesystems"]
	if rows := m.Value.([]map[string]any); len(rows) != maxMountRows || !strings.Contains(m.Note, "205") {
		t.Errorf("mounts: %d rows, note %q", len(rows), m.Note)
	}
}

// TestLsblkOutcomes covers the three ways lsblk can answer.
func TestLsblkOutcomes(t *testing.T) {
	tests := []struct {
		command string
		want    obs.Status
	}{
		{`{"stdout": "_cmd/out.json"}`, obs.StatusOK},
		{`{"missing": true}`, obs.StatusAbsent},
		{`{"exit": 1, "stderr": "lsblk: unrecognized option '--json'"}`, obs.StatusUnsupported},
		{`{"exit": 32, "stderr": "lsblk: failed to access sysfs directory: /sys/dev/block: Permission denied"}`, obs.StatusPermissionDenied},
		{`{"exit": 1, "stderr": "lsblk: something else"}`, obs.StatusFailed},
	}
	for _, tt := range tests {
		meta := `{"description": "t", "now": "2025-01-01T00:00:00Z", "uname": {"machine": "x86_64"}, "page_size": 4096,
			"commands": {"lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE": ` + tt.command + `}}`
		dir := writeRoot(t, meta, map[string]string{"/_cmd/out.json": `{"blockdevices": []}`})
		env, err := collect.OpenFixture(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := lsblkCollector.Collect(context.Background(), env)
		env.Close()
		if len(got) != 1 || got[0].Status != tt.want {
			t.Errorf("%s: got %+v, want status %s", tt.command, got, tt.want)
		}
	}
}
