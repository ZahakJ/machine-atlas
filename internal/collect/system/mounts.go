package system

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	mountinfoPath = "/proc/self/mountinfo"
	// maxMountRows bounds the table: a host running many containers can
	// have thousands of mounts, and the lessons need the standard ones.
	maxMountRows = 200
	// hiddenMount replaces every mount point not in standardMounts.
	hiddenMount = "<path>"
)

// standardMounts are mount points kept verbatim. They are defined by the
// filesystem hierarchy, systemd or the kernel and say nothing about who
// owns the machine; any other mount point (a user's /mnt/backup-of-alice,
// /run/user/1000/doc) is replaced by hiddenMount at collection.
var standardMounts = []string{
	"/", "/boot", "/boot/efi", "/boot/firmware", "/efi", "/home", "/nix", "/nix/store",
	"/opt", "/root", "/srv", "/tmp", "/usr", "/var", "/var/cache", "/var/lib",
	"/var/log", "/var/tmp", "/mnt", "/media",
	"/dev", "/dev/hugepages", "/dev/mqueue", "/dev/pts", "/dev/shm",
	"/proc", "/proc/sys", "/proc/sys/fs/binfmt_misc", "/proc/sysrq-trigger", "/proc/bus",
	"/proc/fs", "/proc/irq", "/proc/kcore", "/proc/keys", "/proc/timer_list", "/proc/scsi",
	"/proc/acpi", "/proc/interrupts",
	"/run", "/run/lock", "/run/credentials", "/run/systemd",
	"/sys", "/sys/firmware", "/sys/firmware/efi/efivars", "/sys/fs/bpf", "/sys/fs/cgroup",
	"/sys/fs/fuse/connections", "/sys/fs/pstore", "/sys/fs/selinux", "/sys/kernel/config",
	"/sys/kernel/debug", "/sys/kernel/security", "/sys/kernel/tracing", "/sys/devices/virtual/powercap",
	"/etc/hostname", "/etc/hosts", "/etc/resolv.conf",
}

// cgroupV1Controllers are the per-controller mount points of a cgroup v1
// hierarchy, under /sys/fs/cgroup.
var cgroupV1Controllers = []string{
	"blkio", "cpu", "cpu,cpuacct", "cpuacct", "cpuset", "devices", "freezer", "hugetlb",
	"memory", "misc", "net_cls", "net_cls,net_prio", "net_prio", "perf_event", "pids",
	"rdma", "systemd", "unified",
}

func standardMount(p string) bool {
	if slices.Contains(standardMounts, p) {
		return true
	}
	c, ok := strings.CutPrefix(p, "/sys/fs/cgroup/")
	return ok && slices.Contains(cgroupV1Controllers, c)
}

// Filesystem types grouped by where their data lives.
var (
	memoryFS  = []string{"tmpfs", "ramfs", "devtmpfs"}
	overlayFS = []string{"overlay", "aufs"}
	networkFS = []string{"nfs", "nfs4", "cifs", "smb3", "ceph", "glusterfs", "9p", "virtiofs", "afs", "fuse.sshfs", "fuse.rclone"}
	pseudoFS  = []string{
		"proc", "sysfs", "cgroup", "cgroup2", "devpts", "mqueue", "debugfs", "tracefs",
		"securityfs", "pstore", "bpf", "configfs", "fusectl", "hugetlbfs", "autofs",
		"binfmt_misc", "efivarfs", "nsfs", "rpc_pipefs", "selinuxfs", "nfsd",
	}
)

// sourceKind says where a mount's data comes from. The filesystem type
// decides first (a tmpfs is memory-backed whatever its source string);
// only then does a /dev/ source mean a block device.
func sourceKind(fstype, source string) string {
	switch {
	case slices.Contains(memoryFS, fstype):
		return "tmpfs"
	case slices.Contains(overlayFS, fstype):
		return "overlay"
	case slices.Contains(networkFS, fstype):
		return "network"
	case slices.Contains(pseudoFS, fstype):
		return "pseudo"
	case strings.HasPrefix(source, "/dev/"):
		return "block-device"
	case strings.HasPrefix(fstype, "fuse"):
		return "fuse"
	}
	return "other"
}

// mountEntry is the part of a mountinfo line atlas keeps. options are the
// per-mount options, superOptions the filesystem's; both are filtered
// through keptOptions before they reach a row.
type mountEntry struct{ mountPoint, fstype, source, devno, options, superOptions string }

// parseMountinfo parses /proc/<pid>/mountinfo (proc(5)): six fixed fields,
// optional fields up to a lone "-", then fstype, source and super options.
// Paths escape space, tab, newline and backslash as octal (\040).
func parseMountinfo(data []byte) ([]mountEntry, error) {
	var out []mountEntry
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		f := strings.Fields(line)
		sep := slices.Index(f, "-")
		if sep < 6 || len(f) < sep+3 {
			return nil, fmt.Errorf("mountinfo line %d: malformed %q", i+1, line)
		}
		mp, err := unescapeOctal(f[4])
		if err != nil || !strings.HasPrefix(mp, "/") {
			return nil, fmt.Errorf("mountinfo line %d: bad mount point %q", i+1, f[4])
		}
		src, err := unescapeOctal(f[sep+2])
		if err != nil {
			return nil, fmt.Errorf("mountinfo line %d: bad source %q", i+1, f[sep+2])
		}
		e := mountEntry{mountPoint: mp, fstype: f[sep+1], source: src, devno: f[2], options: f[5]}
		if len(f) > sep+3 {
			e.superOptions = f[sep+3]
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("mountinfo is empty")
	}
	return out, nil
}

// unescapeOctal decodes the kernel's \ooo escapes in mountinfo paths.
func unescapeOctal(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) {
			return "", fmt.Errorf("truncated escape in %q", s)
		}
		n, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			return "", fmt.Errorf("bad escape in %q", s)
		}
		b.WriteByte(byte(n))
		i += 3
	}
	return b.String(), nil
}

// mountRow turns an entry into a table row. Mount points outside the
// standard list become "<path>"; block devices are named by their basename
// with any embedded UUID removed, so "/dev/mapper/luks-<uuid>" cannot
// identify the machine. Only allow-listed options are kept.
func mountRow(m mountEntry) map[string]any {
	mp := m.mountPoint
	if !standardMount(mp) {
		mp = hiddenMount
	}
	kind := sourceKind(m.fstype, m.source)
	row := map[string]any{"mount_point": mp, "fstype": m.fstype, "source_kind": kind}
	if kind == "block-device" {
		row["device"] = scrubUUID(path.Base(m.source))
	}
	if opts := keptOptions(m.options, m.superOptions); opts != "" {
		row["options"] = opts
	}
	return row
}

// plainOptions are mount options kept verbatim: they say how a filesystem
// behaves and nothing about who owns the machine.
var plainOptions = []string{
	"ro", "rw", "noatime", "relatime", "strictatime", "nodiratime", "lazytime", "sync", "dirsync",
	"discard", "nodiscard", "ssd", "nossd", "ssd_spread", "barrier", "nobarrier", "nodev", "nosuid", "noexec",
	"autodefrag", "journal_checksum", "journal_async_commit", "dax",
}

// valuedOptions are options kept with their value when the value matches.
var valuedOptions = regexp.MustCompile(`^(commit=[0-9]{1,6}|data=(ordered|journal|writeback)|` +
	`(compress|compress-force)=(zlib|lzo|zstd|lz4)(:[0-9]{1,2})?|discard=(async|sync)|barrier=[01]|` +
	`errors=(remount-ro|continue|panic))$`)

// keptOptions joins the allow-listed per-mount and filesystem options,
// per-mount first, without repeats. ro and rw are taken from the per-mount
// options when they have them: that is the access this mount gives.
func keptOptions(perMount, super string) string {
	var out []string
	perMountAccess := false
	for i, list := range []string{perMount, super} {
		for _, o := range strings.Split(list, ",") {
			if !slices.Contains(plainOptions, o) && !valuedOptions.MatchString(o) {
				continue
			}
			if o == "ro" || o == "rw" {
				if i == 1 && perMountAccess {
					continue
				}
				perMountAccess = perMountAccess || i == 0
			}
			if !slices.Contains(out, o) {
				out = append(out, o)
			}
		}
	}
	return strings.Join(out, ",")
}

// diskOf finds the whole disk in /sys/block that holds a block-device
// mount: /sys/class/block/<name> links to the device's sysfs directory, a
// partition's directory sits inside its disk's, and a whole device's sits
// in a "block" directory. When the source's name is not a kernel name
// (/dev/mapper/<volume>, /dev/root), the mount's device number is tried in
// /sys/dev/block. Any failure leaves the disk unknown.
func diskOf(env collect.Env, m mountEntry) (string, bool) {
	var target string
	var err error
	if base := path.Base(m.source); base != "." && base != "/" && !strings.Contains(base, "..") {
		target, err = env.Readlink("/sys/class/block/" + base)
	}
	if target == "" || err != nil {
		if !devno.MatchString(m.devno) {
			return "", false
		}
		if target, err = env.Readlink("/sys/dev/block/" + m.devno); err != nil {
			return "", false
		}
	}
	parts := strings.Split(strings.TrimRight(target, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	disk := parts[len(parts)-1]
	if parent := parts[len(parts)-2]; parent != "block" {
		disk = parent
	}
	if ok, err := env.Stat(sysBlock + "/" + disk); err != nil || !ok {
		return "", false
	}
	return disk, true
}

var devno = regexp.MustCompile(`^[0-9]{1,4}:[0-9]{1,7}$`)

var mountsCollector = collector{
	id:   "system.mounts",
	owns: []string{"mounts.filesystems"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		const id = "mounts.filesystems"
		src := obs.File(mountinfoPath, "")
		src.Detail = "mount point, filesystem type, source and allow-listed options; non-standard mount points replaced by " + hiddenMount + "; disks from /sys/class/block and /sys/dev/block"
		entries, err := readParsed(env, mountinfoPath, parseMountinfo)
		if err != nil {
			return []obs.Observation{obs.Errored(id, src, err)}
		}
		var note string
		if len(entries) > maxMountRows {
			note = fmt.Sprintf("showing the first %d of %d mounts", maxMountRows, len(entries))
			entries = entries[:maxMountRows]
		}
		rows := make([]map[string]any, len(entries))
		for i, m := range entries {
			rows[i] = mountRow(m)
			if rows[i]["source_kind"] == "block-device" {
				if disk, ok := diskOf(env, m); ok {
					rows[i]["disk"] = disk
				}
			}
		}
		o := obs.OK(id, rows, src)
		o.Note = note
		return []obs.Observation{o}
	},
}
