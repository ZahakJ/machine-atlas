package system

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{"quoted and bare", "NAME=\"Arch Linux\"\nID=arch\n# comment\n\nID_LIKE='rhel fedora'\n",
			map[string]string{"NAME": "Arch Linux", "ID": "arch", "ID_LIKE": "rhel fedora"}, false},
		{"escapes in double quotes", `NAME="say \"hi\" \$x \\ \n"`, map[string]string{"NAME": `say "hi" $x \ \n`}, false},
		{"empty value", "VERSION_ID=\n", map[string]string{"VERSION_ID": ""}, false},
		{"crlf-free blank lines only", "\n\n", map[string]string{}, false},
		{"not an assignment", "this is not os-release\n", nil, true},
		{"unterminated quote", "NAME=\"Arch\n", nil, true},
		{"text after quote", "NAME=\"Arch\" Linux\n", nil, true},
		{"bad key", "1NAME=x\n", nil, true},
		{"bare value with space", "NAME=Arch Linux\n", nil, true},
		{"single quote inside single quotes", "NAME='a'b'\n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOSRelease([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBtimeAndUptime(t *testing.T) {
	btimes := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"cpu  1 2 3\nbtime 1737127217\nprocesses 9\n", 1737127217, false},
		{"cpu  1 2 3\n", 0, true},
		{"btime\n", 0, true},
		{"btime 12x\n", 0, true},
		{"btime -5\n", 0, true},
	}
	for _, tt := range btimes {
		got, err := parseBtime([]byte(tt.in))
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseBtime(%q) = %d, %v; want %d, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
	uptimes := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"274512.43 1998124.11\n", 274512.43, false},
		{"12.5\n", 0, true},
		{"abc 1\n", 0, true},
		{"-1 1\n", 0, true},
		{"NaN 1\n", 0, true},
	}
	for _, tt := range uptimes {
		got, err := parseUptime([]byte(tt.in))
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseUptime(%q) = %v, %v; want %v, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

const x86CPUInfo = `processor	: 0
vendor_id	: GenuineIntel
model name	: Example CPU
physical id	: 0
core id		: 0
flags		: fpu pse pdpe1gb la57
address sizes	: 46 bits physical, 57 bits virtual
power management:

processor	: 1
vendor_id	: GenuineIntel
model name	: Example CPU
physical id	: 0
core id		: 0
flags		: fpu pse pdpe1gb la57
address sizes	: 46 bits physical, 57 bits virtual
power management:

`

const arm64CPUInfo = `processor	: 0
BogoMIPS	: 108.00
Features	: fp asimd evtstrm crc32 cpuid
CPU implementer	: 0x41
CPU architecture: 8
CPU part	: 0xd08

processor	: 1
BogoMIPS	: 108.00
CPU implementer	: 0x41
CPU part	: 0xd08

Revision	: c03114
Model		: Raspberry Pi 4 Model B Rev 1.4
`

const riscvCPUInfo = `processor	: 0
hart		: 1
isa		: rv64imafdc_zicntr_zicsr
mmu		: sv39
uarch		: sifive,u74-mc
mvendorid	: 0x489

`

func TestParseCPUInfo(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		procs   int
		dialect archFamily
		extra   map[string]string
		wantErr bool
	}{
		{"x86", x86CPUInfo, 2, famX86, map[string]string{}, false},
		{"arm64 with board trailer", arm64CPUInfo, 2, famARM,
			map[string]string{"Revision": "c03114", "Model": "Raspberry Pi 4 Model B Rev 1.4"}, false},
		{"riscv", riscvCPUInfo, 1, famRISCV, map[string]string{}, false},
		{"no processor", "Hardware\t: BCM2835\n", 0, "", nil, true},
		{"line without colon", "processor\t: 0\ngarbage line\n", 0, "", nil, true},
		{"empty", "", 0, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCPUInfo([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got.procs) != tt.procs || got.dialect() != tt.dialect || !reflect.DeepEqual(got.extra, tt.extra) {
				t.Errorf("got %d procs, dialect %s, extra %v", len(got.procs), got.dialect(), got.extra)
			}
		})
	}
	info, _ := parseCPUInfo([]byte(x86CPUInfo))
	if v := info.procs[0]["power management"]; v != "" {
		t.Errorf("empty value parsed as %q", v)
	}
}

func TestFamilyOf(t *testing.T) {
	for in, want := range map[string]archFamily{
		"x86_64": famX86, "i686": famX86, "i386": famX86, "aarch64": famARM, "armv7l": famARM,
		"riscv64": famRISCV, "s390x": famOther, "ppc64le": famOther, "": famOther, "ia64": famOther,
	} {
		if got := familyOf(in); got != want {
			t.Errorf("familyOf(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestParseAddressSizes(t *testing.T) {
	tests := []struct {
		in         string
		phys, virt int
		wantErr    bool
	}{
		{"39 bits physical, 48 bits virtual", 39, 48, false},
		{"52 bits physical, 57 bits virtual", 52, 57, false},
		{"48 bits virtual", 0, 0, true},
		{"0 bits physical, 48 bits virtual", 0, 0, true},
		{"39 bits physical, 99 bits virtual", 0, 0, true},
		{"", 0, 0, true},
	}
	for _, tt := range tests {
		p, v, err := parseAddressSizes(tt.in)
		if (err != nil) != tt.wantErr || p != tt.phys || v != tt.virt {
			t.Errorf("parseAddressSizes(%q) = %d, %d, %v", tt.in, p, v, err)
		}
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name  string
		units []topoUnit
		want  topology
	}{
		{"one core", []topoUnit{{"0", "a"}}, topology{1, 1, 1, 1}},
		{"smt pairs", []topoUnit{{"0", "a"}, {"0", "b"}, {"0", "a"}, {"0", "b"}}, topology{4, 2, 1, 2}},
		// Core ids repeat across packages: the core key must include the package.
		{"two sockets", []topoUnit{{"0", "0/0"}, {"1", "1/0"}}, topology{2, 2, 2, 1}},
		// Hybrid CPU: P-cores with SMT, E-cores without; lscpu reports 2.
		{"hybrid", []topoUnit{{"0", "p0"}, {"0", "p0"}, {"0", "e0"}, {"0", "e1"}}, topology{4, 3, 1, 2}},
		{"empty", nil, topology{}},
	}
	for _, tt := range tests {
		if got := summarize(tt.units); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestCPUInfoTopology(t *testing.T) {
	info, err := parseCPUInfo([]byte(x86CPUInfo))
	if err != nil {
		t.Fatal(err)
	}
	units, err := cpuinfoTopology(info)
	if err != nil {
		t.Fatal(err)
	}
	if got := summarize(units); got != (topology{2, 1, 1, 2}) {
		t.Errorf("got %+v", got)
	}
	arm, _ := parseCPUInfo([]byte(arm64CPUInfo))
	if _, err := cpuinfoTopology(arm); err == nil {
		t.Error("arm64 cpuinfo has no topology fields, want an error")
	}
}

func TestParseCgroupPaths(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"0::/\n", []string{"/"}, false},
		{"0::/init.scope\n", []string{"/init.scope"}, false},
		{"2:memory:/\n1:name=systemd:/user.slice\n", []string{"/", "/user.slice"}, false},
		{"", nil, true},
		{"garbage\n", nil, true},
		{"0::relative\n", nil, true},
	}
	for _, tt := range tests {
		got, err := parseCgroupPaths([]byte(tt.in))
		if (err != nil) != tt.wantErr || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseCgroupPaths(%q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestParseSectorsAndBlockType(t *testing.T) {
	for in, want := range map[string]int64{"0\n": 0, "1953525168\n": 1953525168} {
		if got, err := parseSectors([]byte(in)); err != nil || got != want {
			t.Errorf("parseSectors(%q) = %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "-1", "12 34", "x", "99999999999999999999"} {
		if _, err := parseSectors([]byte(bad)); err == nil {
			t.Errorf("parseSectors(%q) succeeded", bad)
		}
	}
	for name, want := range map[string]string{
		"sda": "disk", "nvme0n1": "disk", "mmcblk0": "disk", "vda": "disk", "loop3": "loop", "ram0": "ram",
		"zram0": "zram", "dm-1": "dm", "md127": "raid", "sr0": "rom", "nbd0": "nbd",
	} {
		if got := blockType(name); got != want {
			t.Errorf("blockType(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestParseLsblk(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []map[string]any
		wantErr bool
	}{
		{"numeric sizes and children", `{"blockdevices": [{"name": "sda", "type": "disk", "size": 1000, "fstype": null,
			"children": [{"name": "sda1", "type": "part", "size": 600, "fstype": "ext4"}]}]}`,
			[]map[string]any{
				{"name": "sda", "type": "disk", "size_bytes": int64(1000)},
				{"name": "sda1", "type": "part", "size_bytes": int64(600), "fstype": "ext4"},
			}, false},
		{"string sizes (util-linux before 2.33)", `{"blockdevices": [{"name": "vda", "type": "disk", "size": "4096", "fstype": "xfs"}]}`,
			[]map[string]any{{"name": "vda", "type": "disk", "size_bytes": int64(4096), "fstype": "xfs"}}, false},
		{"uuid in a mapper name", `{"blockdevices": [{"name": "luks-3f2a9c1e-0b7d-4e8a-9c31-5d2e7f6a8b90", "type": "crypt", "size": 1}]}`,
			[]map[string]any{{"name": "luks-<uuid>", "type": "crypt", "size_bytes": int64(1)}}, false},
		{"empty list", `{"blockdevices": []}`, []map[string]any{}, false},
		{"human sizes", `{"blockdevices": [{"name": "sda", "type": "disk", "size": "1T"}]}`, nil, true},
		{"no array", `{"devices": []}`, nil, true},
		{"missing name", `{"blockdevices": [{"type": "disk", "size": 1}]}`, nil, true},
		{"not json", "NAME TYPE SIZE\nsda disk 1T\n", nil, true},
		{"too deep", `{"blockdevices": [` + strings.Repeat(`{"name":"a","type":"t","size":1,"children":[`, 10) +
			strings.Repeat(`]}`, 10) + `]}`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLsblk([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRejectsJSON(t *testing.T) {
	for in, want := range map[string]bool{
		"lsblk: unrecognized option '--json'\nTry 'lsblk --help'": true,
		"lsblk: invalid option -- 'J'":                            false,
		"lsblk: unknown option --json":                            true,
		"lsblk: failed to access sysfs directory: /sys/dev/block": false,
		"": false,
	} {
		if got := rejectsJSON(in); got != want {
			t.Errorf("rejectsJSON(%q) = %v", in, got)
		}
	}
}

func TestParseMountinfo(t *testing.T) {
	in := "20 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw,errors=remount-ro,data=ordered\n" +
		"21 20 0:22 / /proc rw,nosuid - proc proc rw\n" +
		"22 20 0:40 / /mnt/my\\040disk rw master:3 shared:7 - ext4 /dev/mapper/luks-3f2a9c1e-0b7d-4e8a-9c31-5d2e7f6a8b90 rw\n"
	got, err := parseMountinfo([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []mountEntry{
		{"/", "ext4", "/dev/sda2", "8:2", "rw,relatime", "rw,errors=remount-ro,data=ordered"},
		{"/proc", "proc", "proc", "0:22", "rw,nosuid", "rw"},
		{"/mnt/my disk", "ext4", "/dev/mapper/luks-3f2a9c1e-0b7d-4e8a-9c31-5d2e7f6a8b90", "0:40", "rw", "rw"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	rows := []map[string]any{mountRow(got[0]), mountRow(got[1]), mountRow(got[2])}
	wantRows := []map[string]any{
		{"mount_point": "/", "fstype": "ext4", "source_kind": "block-device", "device": "sda2", "options": "rw,relatime,errors=remount-ro,data=ordered"},
		{"mount_point": "/proc", "fstype": "proc", "source_kind": "pseudo", "options": "rw,nosuid"},
		{"mount_point": "<path>", "fstype": "ext4", "source_kind": "block-device", "device": "luks-<uuid>", "options": "rw"},
	}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("rows = %v", rows)
	}

	for _, bad := range []string{
		"",
		"20 1 8:2 / / rw shared:1 ext4 /dev/sda2 rw\n", // no separator
		"20 1 8:2 / / rw - ext4\n",                     // separator but too few fields after
		"20 1 8:2 / relative rw - ext4 /dev/sda2 rw\n", // mount point not absolute
		"20 1 8:2 / /a\\04 rw - ext4 /dev/sda2 rw\n",   // truncated escape
		"20 1 8:2 / /a\\9zz rw - ext4 /dev/sda2 rw\n",  // bad escape
		"20 1 / / rw - ext4 /dev/sda2 rw\n",            // too few fixed fields
	} {
		if _, err := parseMountinfo([]byte(bad)); err == nil {
			t.Errorf("parseMountinfo(%q) succeeded", bad)
		}
	}
}

// Only allow-listed options survive: never paths, overlay layers, user
// ids or subvolume names.
func TestKeptOptions(t *testing.T) {
	for _, tt := range []struct{ mount, super, want string }{
		{"rw,noatime", "rw,attr2,inode64,logbufs=8,noquota", "rw,noatime"},
		{"ro,relatime", "rw,compress=zstd:3,ssd,discard=async,space_cache=v2,subvolid=256,subvol=/@home", "ro,relatime,compress=zstd:3,ssd,discard=async"},
		{"rw,relatime", "rw,lowerdir=/var/lib/docker/overlay2/l/A:/x,upperdir=/var/lib/x/diff,workdir=/w", "rw,relatime"},
		{"rw,nosuid,nodev", "rw,size=1629532k,mode=700,uid=1000,gid=1000", "rw,nosuid,nodev"},
		{"rw", "rw,commit=60,data=journal,barrier=0,sync", "rw,commit=60,data=journal,barrier=0,sync"},
		{"rw", "rw,commit=x,data=bogus,compress=zstd:/etc", "rw"},
		{"", "", ""},
	} {
		if got := keptOptions(tt.mount, tt.super); got != tt.want {
			t.Errorf("keptOptions(%q, %q) = %q, want %q", tt.mount, tt.super, got, tt.want)
		}
	}
}

func TestSourceKind(t *testing.T) {
	tests := []struct{ fstype, source, want string }{
		{"ext4", "/dev/sda2", "block-device"},
		{"xfs", "/dev/mapper/vg-root", "block-device"},
		{"tmpfs", "tmpfs", "tmpfs"},
		{"devtmpfs", "udev", "tmpfs"},
		{"tmpfs", "/dev/null-looking", "tmpfs"},
		{"overlay", "overlay", "overlay"},
		{"nfs4", "server:/export", "network"},
		{"fuse.sshfs", "user@host:", "network"},
		{"fuse.portal", "portal", "fuse"},
		{"cgroup2", "cgroup2", "pseudo"},
		{"zfs", "rpool/ROOT", "other"},
	}
	for _, tt := range tests {
		if got := sourceKind(tt.fstype, tt.source); got != tt.want {
			t.Errorf("sourceKind(%q, %q) = %s, want %s", tt.fstype, tt.source, got, tt.want)
		}
	}
}

func TestStandardMount(t *testing.T) {
	for p, want := range map[string]bool{
		"/": true, "/boot/efi": true, "/sys/fs/cgroup/memory": true, "/sys/fs/cgroup/cpu,cpuacct": true,
		"/sys/fs/cgroup/user.slice": false, "/run/user/1000": false, "/home/someone": false,
		"/mnt/backup": false, "/var/lib/docker/overlay2/x/merged": false,
	} {
		if got := standardMount(p); got != want {
			t.Errorf("standardMount(%q) = %v", p, got)
		}
	}
}
