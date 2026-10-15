package memory

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readTestdata(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseMeminfo(t *testing.T) {
	fields, err := parseMeminfo(readTestdata(t, "meminfo-6.8.txt"))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]meminfoField{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for key, want := range map[string]meminfoField{
		"MemTotal":        {"MemTotal", 16318412 * 1024, true},
		"Active(anon)":    {"Active(anon)", 2345678 * 1024, true},
		"HugePages_Total": {"HugePages_Total", 0, false},
		"VmallocTotal":    {"VmallocTotal", 34359738367 * 1024, true},
	} {
		if byKey[key] != want {
			t.Errorf("%s = %+v, want %+v", key, byKey[key], want)
		}
	}

	// Kernels before 3.14 have no MemAvailable: the parser must not invent it.
	old, err := parseMeminfo(readTestdata(t, "meminfo-3.10.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range old {
		if f.Key == "MemAvailable" {
			t.Error("MemAvailable present in a 3.10 meminfo")
		}
	}
}

func TestParseMeminfoMalformed(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "",
		"no colon":       "MemTotal 1024 kB\n",
		"other unit":     "MemTotal: 1024 MB\n",
		"not a number":   "MemTotal: lots kB\n",
		"negative":       "MemTotal: -4 kB\n",
		"duplicate":      "MemTotal: 1 kB\nMemTotal: 2 kB\n",
		"overflow":       "MemTotal: 99999999999999999 kB\n",
		"truncated line": "MemTotal: 1024 kB\nMemFr",
	} {
		if _, err := parseMeminfo([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseVmstat(t *testing.T) {
	got, err := parseVmstat([]byte("nr_free_pages 12345\npgfault 98765432\nnr_drifted 18446744073709551615\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []vmstatCounter{{"nr_free_pages", 12345}, {"pgfault", 98765432}, {"nr_drifted", 1<<64 - 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	var tooMany strings.Builder
	for i := range maxVmstatKeys + 1 {
		fmt.Fprintf(&tooMany, "key%d 1\n", i)
	}
	for name, in := range map[string]string{
		"empty":     "",
		"one field": "pgfault\n",
		"negative":  "pgfault -1\n",
		"three":     "pgfault 1 2\n",
		"duplicate": "pgfault 1\npgfault 2\n",
		"too many":  tooMany.String(),
	} {
		if _, err := parseVmstat([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseZoneinfo(t *testing.T) {
	tests := []struct {
		file  string
		zones int
		check zone
	}{
		// A two-node machine on a current kernel: boost, promo and cma lines,
		// per-CPU "high:" lines that must not be taken for the watermark.
		{"zoneinfo-numa.txt", 6, zone{Node: 1, Name: "Normal", Free: 6234567, Min: 11290, Low: 19446, High: 27602, Spanned: 8388608, Present: 8388608, Managed: 8256012}},
		// Linux 4.19: no boost, promo or cma; unpopulated zones still listed.
		{"zoneinfo-4.19.txt", 4, zone{Node: 0, Name: "DMA32", Free: 312345, Min: 8458, Low: 10572, High: 12686, Spanned: 520160, Present: 520160, Managed: 494912}},
	}
	for _, tt := range tests {
		zones, err := parseZoneinfo(readTestdata(t, tt.file))
		if err != nil {
			t.Fatalf("%s: %v", tt.file, err)
		}
		if len(zones) != tt.zones {
			t.Errorf("%s: %d zones, want %d", tt.file, len(zones), tt.zones)
		}
		found := false
		for _, z := range zones {
			if z.Node == tt.check.Node && z.Name == tt.check.Name {
				found = true
				if z != tt.check {
					t.Errorf("%s: got %+v, want %+v", tt.file, z, tt.check)
				}
			}
		}
		if !found {
			t.Errorf("%s: zone %d/%s missing", tt.file, tt.check.Node, tt.check.Name)
		}
	}
}

func TestParseZoneinfoMalformed(t *testing.T) {
	full := string(readTestdata(t, "zoneinfo-4.19.txt"))
	for name, in := range map[string]string{
		"empty":            "",
		"before header":    "  pages free 3\nNode 0, zone DMA\n",
		"truncated":        full[:strings.Index(full, "spanned  520160")],
		"missing managed":  strings.Replace(full, "managed  494912", "", 1),
		"bad number":       strings.Replace(full, "min      8458", "min      lots", 1),
		"negative":         strings.Replace(full, "low      10572", "low      -1", 1),
		"bad node":         "Node x, zone DMA\n  pages free 1\n",
		"header no fields": "Node 0, zone DMA\n",
	} {
		if _, err := parseZoneinfo([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseBuddyinfo(t *testing.T) {
	in := "Node 0, zone      DMA      1      1      1      0      2      1      1      0      1      1      3 \n" +
		"Node 1, zone   Normal   4021   2210    911    402    120     37     10      2      1      0      0 \n"
	got, err := parseBuddyinfo([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Node != 1 || got[1].Name != "Normal" || len(got[1].Orders) != 11 || got[1].Orders[0] != 4021 {
		t.Errorf("got %+v", got)
	}
	for name, in := range map[string]string{
		"empty":          "",
		"no counts":      "Node 0, zone DMA\n",
		"ragged":         "Node 0, zone DMA 1 2 3\nNode 0, zone DMA32 1 2\n",
		"not a count":    "Node 0, zone DMA 1 x 3\n",
		"missing comma":  "Node 0 zone DMA 1 2 3\n",
		"garbage":        "hello\n",
		"negative order": "Node 0, zone DMA 1 -2 3\n",
	} {
		if _, err := parseBuddyinfo([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseSwaps(t *testing.T) {
	header := "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"
	tests := []struct {
		name string
		in   string
		want []swapArea
	}{
		{"header only", header, []swapArea{}},
		{"partition", header + "/dev/nvme0n1p3                          partition\t8388604\t\t1024\t\t-2\n",
			[]swapArea{{"nvme0n1p3", "partition", 8388604 * 1024, 1024 * 1024, -2}}},
		{"file path not kept", header + "/var/lib/swap/swapfile\\040one          file\t\t2097148\t\t0\t\t-3\n",
			[]swapArea{{"<file>", "file", 2097148 * 1024, 0, -3}}},
		{"zram", header + "/dev/zram0                              partition\t4194300\t\t512\t\t100\n",
			[]swapArea{{"zram0", "partition", 4194300 * 1024, 512 * 1024, 100}}},
	}
	for _, tt := range tests {
		got, err := parseSwaps([]byte(tt.in))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
	for name, in := range map[string]string{
		"empty":       "",
		"no header":   "/dev/sda2 partition 100 0 -2\n",
		"short line":  header + "/dev/sda2 partition 100 0\n",
		"bad type":    header + "/dev/sda2 tape 100 0 -2\n",
		"bad size":    header + "/dev/sda2 partition big 0 -2\n",
		"bad prio":    header + "/dev/sda2 partition 100 0 high\n",
		"neg size":    header + "/dev/sda2 partition -100 0 -2\n",
		"truncated":   header + "/dev/sda2 partit",
		"overflowing": header + "/dev/sda2 partition 9223372036854775807 0 -2\n",
	} {
		if _, err := parseSwaps([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParsePSI(t *testing.T) {
	in := "some avg10=1.25 avg60=0.50 avg300=0.10 total=123456\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=789\n"
	got, err := parsePSI([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got["some"] != (psiLine{1.25, 0.5, 0.1, 123456}) || got["full"] != (psiLine{0, 0, 0, 789}) {
		t.Errorf("got %+v", got)
	}
	for name, in := range map[string]string{
		"empty":     "",
		"no full":   "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"no total":  "some avg10=0.00 avg60=0.00 avg300=0.00\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"NaN":       "some avg10=NaN avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"over 100":  "some avg10=100.5 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"unknown":   "most avg10=0.00\n",
		"no equals": "some avg10 0.00\n",
		"duplicate": in + "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"neg total": "some avg10=0.00 avg60=0.00 avg300=0.00 total=-1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"truncated": in[:30],
	} {
		if _, err := parsePSI([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseProcCgroupAndClassify(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want cgroupView
	}{
		{"v2", "0::/user.slice/user-4242.slice/session-7.scope\n",
			cgroupView{"v2", "/user.slice/user-4242.slice/session-7.scope", true}},
		{"v1", "12:pids:/system.slice/demo.service\n11:memory:/system.slice/demo.service\n4:cpu,cpuacct:/system.slice/demo.service\n1:name=systemd:/system.slice/demo.service\n",
			cgroupView{"v1", "/system.slice/demo.service", false}},
		{"hybrid, memory on v1", "11:memory:/user.slice\n1:name=systemd:/user.slice/session-2.scope\n0::/user.slice/session-2.scope\n",
			cgroupView{"hybrid", "/user.slice", false}},
		{"hybrid, memory on v2", "3:cpu:/x\n0::/y\n",
			cgroupView{"hybrid", "/y", true}},
		{"v1 without memory", "3:cpu:/x\n", cgroupView{"v1", "", false}},
		{"path with colon", "0::/odd:name\n", cgroupView{"v2", "/odd:name", true}},
	}
	for _, tt := range tests {
		entries, err := parseProcCgroup([]byte(tt.in))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got := classify(entries); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
	for name, in := range map[string]string{
		"empty":            "",
		"two fields":       "0:/x\n",
		"relative path":    "0::x\n",
		"bad id":           "a::/x\n",
		"v2 with ctrls":    "0:memory:/x\n",
		"v1 without ctrls": "3::/x\n",
	} {
		if _, err := parseProcCgroup([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestCgroupDir(t *testing.T) {
	mounts, err := parseMountinfo([]byte(
		"25 1 0:22 / /sys/fs/cgroup/unified rw,nosuid shared:7 - cgroup2 cgroup2 rw\n" +
			"26 1 0:23 / /sys/fs/cgroup/memory rw shared:8 - cgroup cgroup rw,memory\n" +
			"30 1 0:24 /docker/abc /sys/fs/cgroup/my\\040dir rw - cgroup2 cgroup2 rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	if mounts[2].Point != "/sys/fs/cgroup/my dir" {
		t.Errorf("octal escape not undone: %q", mounts[2].Point)
	}
	point, rel, ok := cgroupDir(mounts, "/user.slice")
	if !ok || point != "/sys/fs/cgroup/unified" || rel != "/user.slice" {
		t.Errorf("got %q %q %v", point, rel, ok)
	}
	// Only a mount whose root contains the cgroup can show it.
	if _, _, ok := cgroupDir(mounts[2:], "/elsewhere"); ok {
		t.Error("cgroup outside the mount root resolved")
	}
	if _, _, ok := cgroupDir(mounts[2:], "/docker/abcdef"); ok {
		t.Error("sibling with a common prefix resolved")
	}
	if point, rel, ok := cgroupDir(mounts[2:], "/docker/abc"); !ok || rel != "" || point != "/sys/fs/cgroup/my dir" {
		t.Errorf("mount root itself: %q %q %v", point, rel, ok)
	}
	if _, err := parseMountinfo([]byte("25 1 0:22 / /x rw\n")); err == nil {
		t.Error("mountinfo line without separator: want error")
	}
}

func TestParseLimitAndEvents(t *testing.T) {
	for in, want := range map[string]any{"max\n": "max", "1073741824\n": int64(1 << 30), "0": int64(0)} {
		got, err := parseLimit([]byte(in))
		if err != nil || got != want {
			t.Errorf("parseLimit(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "unlimited", "-1", "max max"} {
		if _, err := parseLimit([]byte(bad)); err == nil {
			t.Errorf("parseLimit(%q): want error", bad)
		}
	}
	ev, err := parseFlatKeyed([]byte("low 0\nhigh 12\nmax 3\noom 1\noom_kill 1\noom_group_kill 0\n"))
	if err != nil || ev["high"] != 12 || ev["oom_kill"] != 1 {
		t.Errorf("parseFlatKeyed = %v, %v", ev, err)
	}
	if _, err := parseFlatKeyed([]byte("oom one\n")); err == nil {
		t.Error("parseFlatKeyed: want error")
	}
}

func TestParseNodeMeminfo(t *testing.T) {
	in := "Node 1 MemTotal:       16384000 kB\nNode 1 MemFree:         8192000 kB\nNode 1 MemUsed:         8192000 kB\n"
	total, free, err := parseNodeMeminfo([]byte(in), 1)
	if err != nil || total != 16384000*1024 || free != 8192000*1024 {
		t.Errorf("got %d %d %v", total, free, err)
	}
	for name, in := range map[string]string{
		"wrong node": "Node 0 MemTotal: 1 kB\nNode 0 MemFree: 1 kB\n",
		"no free":    "Node 1 MemTotal: 1 kB\n",
		"no unit":    "Node 1 MemTotal: 1\nNode 1 MemFree: 1 kB\n",
	} {
		if _, _, err := parseNodeMeminfo([]byte(in), 1); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestSmallParsers(t *testing.T) {
	if n, ok := hugepageSize("hugepages-1048576kB"); !ok || n != 1<<30 {
		t.Errorf("hugepageSize = %d, %v", n, ok)
	}
	for _, bad := range []string{"hugepages-", "hugepages-2048", "hugepages-xkB", "other"} {
		if _, ok := hugepageSize(bad); ok {
			t.Errorf("hugepageSize(%q) accepted", bad)
		}
	}
	if got := numberedEntries([]string{"node10", "node2", "node", "node01", "possible", "node0"}, "node"); !reflect.DeepEqual(got, []int{0, 2, 10}) {
		t.Errorf("numberedEntries = %v", got)
	}
	for name, want := range map[string]bool{"zram0": true, "zram12": true, "zram": false, "zramx": false, "sda": false} {
		if isZram(name) != want {
			t.Errorf("isZram(%q) != %v", name, want)
		}
	}
	n, err := countSlabCaches([]byte("slabinfo - version: 2.1\n# name <active_objs> ...\next4_inode 10 10 1 1 1\ndentry 5 5 1 1 1\n"))
	if err != nil || n != 2 {
		t.Errorf("countSlabCaches = %d, %v", n, err)
	}
	if _, err := countSlabCaches([]byte("ext4_inode 1\n")); err == nil {
		t.Error("countSlabCaches without header: want error")
	}
}
