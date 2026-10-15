package probe

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

// Invented maps of a small Go program, in the kernel's format.
const sampleMaps = `00400000-004ab000 r-xp 00000000 103:02 1234567                            /opt/tools/bin/atlas
004ab000-0058e000 r--p 000ab000 103:02 1234567                            /opt/tools/bin/atlas
0058e000-00599000 rw-p 0018e000 103:02 1234567                            /opt/tools/bin/atlas
00599000-005cf000 rw-p 00000000 00:00 0
3a51c8400000-3a51c8c00000 rw-p 00000000 00:00 0                          [anon: Go: heap]
7f3a10000000-7f3a14000000 rw-p 00000000 00:00 0
7f3a14400000-7f3a14800000 rw-s 00000000 00:01 2048                       /memfd:atlas-probe (deleted)
7f3a2c000000-7f3a2c040000 rw-p 00000000 00:00 0                          [anon: Go: immortal metadata]
7f3a2c040000-7f3a2c044000 r--p 00000000 00:00 0                          [vvar]
7f3a2c044000-7f3a2c046000 r-xp 00000000 00:00 0                          [vdso]
7ffd5a3c1000-7ffd5a3e2000 rw-p 00000000 00:00 0                          [stack]
ffffffffff600000-ffffffffff601000 --xp 00000000 00:00 0                  [vsyscall]
`

func TestParseMaps(t *testing.T) {
	vmas, err := parseMaps([]byte(sampleMaps))
	if err != nil {
		t.Fatal(err)
	}
	if len(vmas) != 12 {
		t.Fatalf("%d vmas, want 12", len(vmas))
	}
	want := vma{Start: 0x7f3a14400000, End: 0x7f3a14800000, Perms: "rw-s", Offset: 0, Path: "/memfd:atlas-probe (deleted)"}
	if vmas[6] != want {
		t.Errorf("memfd line: got %+v, want %+v", vmas[6], want)
	}
	if vmas[3].Path != "" || vmas[4].Path != "[anon: Go: heap]" {
		t.Errorf("paths: %q %q", vmas[3].Path, vmas[4].Path)
	}
	for name, in := range map[string]string{
		"empty":       "",
		"no dash":     "00400000 r-xp 00000000 00:00 0\n",
		"bad hex":     "0040000g-004ab000 r-xp 00000000 00:00 0\n",
		"end<=start":  "004ab000-00400000 r-xp 00000000 00:00 0\n",
		"bad perms":   "00400000-004ab000 rwz 00000000 00:00 0\n",
		"bad offset":  "00400000-004ab000 r-xp zz 00:00 0\n",
		"bad device":  "00400000-004ab000 r-xp 00000000 0000 0\n",
		"bad inode":   "00400000-004ab000 r-xp 00000000 00:00 x\n",
		"truncated":   "00400000-004ab000 r-xp 0000",
		"overlapping": "00400000-004ab000 r-xp 00000000 00:00 0\n00400000-004ab000 r-xp 00000000 00:00 0\n",
	} {
		if _, err := parseMaps([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseSmaps(t *testing.T) {
	in := `7f3a10000000-7f3a10800000 rw-p 00000000 00:00 0
Size:               8192 kB
KernelPageSize:        4 kB
Rss:                8192 kB
Pss:                8192 kB
Anonymous:          8192 kB
AnonHugePages:      6144 kB
THPeligible:    1
ProtectionKey:         0
VmFlags: rd wr mr mw me ac sd
7f3a14400000-7f3a14800000 rw-s 00000000 00:01 2048                       /memfd:atlas-probe (deleted)
Rss:                1024 kB
Shared_Dirty:          0 kB
`
	got, err := parseSmaps([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Fields["Rss"] != 8<<20 || got[0].Fields["AnonHugePages"] != 6<<20 || got[1].Fields["Rss"] != 1<<20 {
		t.Errorf("got %+v", got)
	}
	if _, ok := got[0].Fields["THPeligible"]; ok {
		t.Error("THPeligible is not a size")
	}
	for name, in := range map[string]string{
		"empty":            "",
		"field first":      "Rss: 4 kB\n",
		"bad size":         "7f3a10000000-7f3a10800000 rw-p 00000000 00:00 0\nRss: x kB\n",
		"bad header":       "7f3a10000000-zz rw-p 00000000 00:00 0\nRss: 4 kB\n",
		"negative":         "7f3a10000000-7f3a10800000 rw-p 00000000 00:00 0\nRss: -4 kB\n",
		"truncated header": "7f3a10000000-7f3a1080",
	} {
		if _, err := parseSmaps([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestDisplayPathAndKind(t *testing.T) {
	tests := []struct{ in, path, kind string }{
		{"", "", "anon"},
		{"/home/someone/bin/atlas", "atlas", "file"},
		{"/memfd:atlas-probe (deleted)", "memfd:atlas-probe (deleted)", "shmem"},
		{"[stack]", "[stack]", "stack"},
		{"[heap]", "[heap]", "heap"},
		{"[vdso]", "[vdso]", "kernel"},
		{"[anon: Go: heap]", "[anon: Go: heap]", "anon"},
		{"/dev/shm/x", "x", "shmem"},
	}
	for _, tt := range tests {
		if got := displayPath(tt.in); got != tt.path {
			t.Errorf("displayPath(%q) = %q, want %q", tt.in, got, tt.path)
		}
		if got := vmaKind(tt.in); got != tt.kind {
			t.Errorf("vmaKind(%q) = %q, want %q", tt.in, got, tt.kind)
		}
	}
}

func TestLayout(t *testing.T) {
	vmas, err := parseMaps([]byte(sampleMaps))
	if err != nil {
		t.Fatal(err)
	}
	got, err := layout(vmas, "/opt/tools/bin/atlas")
	if err != nil {
		t.Fatal(err)
	}
	want := LayoutRun{Stack: "0x7ffd5a3c1000", MmapBase: "0x7f3a2c040000", Heap: "0x3a51c8400000", HeapKind: HeapGoArena, Exe: "0x400000"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// A C-style [heap] wins over the Go arena.
	withBrk := strings.Replace(sampleMaps, "00599000-005cf000 rw-p 00000000 00:00 0\n", "00599000-005cf000 rw-p 00000000 00:00 0                          [heap]\n", 1)
	vmas, _ = parseMaps([]byte(withBrk))
	if got, _ := layout(vmas, "/opt/tools/bin/atlas"); got.Heap != "0x599000" || got.HeapKind != HeapBrk {
		t.Errorf("heap with brk = %s (%s)", got.Heap, got.HeapKind)
	}
	if _, err := layout(vmas, "/elsewhere/atlas"); err == nil {
		t.Error("exe not mapped: want error")
	}
	noStack, _ := parseMaps([]byte(strings.Replace(sampleMaps, "[stack]", "", 1)))
	if _, err := layout(noStack, "/opt/tools/bin/atlas"); err == nil {
		t.Error("no stack: want error")
	}
}

func TestCountPages(t *testing.T) {
	entries := []uint64{
		0,                     // not present
		1<<63 | 0,             // present, frame hidden
		1<<63 | 0x12345,       // present with a frame number
		1<<62 | 0x7c0,         // swapped (type and offset in the low bits)
		1<<63 | 1<<61 | 1<<55, // present file page, soft-dirty, no frame
		1 << 56,               // exclusively mapped flag alone: not present
	}
	buf := make([]byte, 8*len(entries))
	for i, e := range entries {
		binary.NativeEndian.PutUint64(buf[8*i:], e)
	}
	present, swapped, pfn := countPages(buf)
	if present != 3 || swapped != 1 || !pfn {
		t.Errorf("got present %d swapped %d pfn %v", present, swapped, pfn)
	}
	if _, _, pfn := countPages(buf[8:16]); pfn {
		t.Error("zero frame number reported as visible")
	}
}

func TestDecodeReport(t *testing.T) {
	good := sampleReport()
	if _, err := decodeReport([]byte(good), true); err != nil {
		t.Fatalf("sample report rejected: %v", err)
	}
	layoutOnly := `{"layout_runs":[{"run":1,"stack":"0x7ffd0000","mmap_base":"0x7f00","exe":"0x400000"}]}`
	if _, err := decodeReport([]byte(layoutOnly), false); err != nil {
		t.Errorf("layout-only report rejected: %v", err)
	}
	if _, err := decodeReport([]byte(layoutOnly), true); err == nil {
		t.Error("layout-only report accepted as a full one")
	}
	for name, bad := range map[string]string{
		"not json":        "{",
		"unknown field":   strings.Replace(good, `"pagemap"`, `"pagemapx"`, 1),
		"bad placement":   strings.Replace(good, `"placement":"mmap"`, `"placement":"anywhere"`, 1),
		"trailing":        good + "{}",
		"size mismatch":   strings.Replace(good, `"size_bytes":67108864`, `"size_bytes":67108865`, 1),
		"present > total": strings.Replace(good, `"pages_present":2048`, `"pages_present":4096`, 1),
		"bad backing":     strings.Replace(good, `"backing":"shmem"`, `"backing":"anon"`, 1),
		"unknown region":  strings.Replace(good, `"anon-read"`, `"anon-other"`, 1),
		"bad address":     strings.Replace(good, `"stack":"0x7ffd5a3c1000"`, `"stack":"7ffd5a3c1000"`, 1),
		"no layout":       strings.Replace(good, `"layout_runs":[`, `"layout_runs":[],"x":[`, 1),
	} {
		if _, err := decodeReport([]byte(bad), true); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if got, want := hex(0x7f3a10000000), "0x7f3a10000000"; got != want {
		t.Errorf("hex = %s", got)
	}
	if _, err := parseHex("0x"); err == nil {
		t.Error(`parseHex("0x"): want error`)
	}
	if !reflect.DeepEqual(ids()[:2], []string{idRegions, idMapsSample}) || len(ids()) != 20 {
		t.Errorf("ids = %v", ids())
	}
}
