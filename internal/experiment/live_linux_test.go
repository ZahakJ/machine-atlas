//go:build linux

package experiment

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// runLive runs experiment id for real and checks what every result must
// satisfy, returning its values by measurement id.
func runLive(t *testing.T, id string, env ...string) measured {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the experiment")
	}
	info, _ := lookup(id)
	start := time.Now()
	res, err := testRunner(append(env, childVar+"=1")...).run(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Duration(info.Resources.TimeBoundSeconds)*time.Second {
		t.Errorf("took %s, over the bound", d)
	}
	t.Logf("%s: outcome %s", id, res.Outcome)
	return values(res.Measurements)
}

// Only what Linux guarantees is asserted: THP decides region A's counts,
// so they are not checked.
func TestLiveFaultCount(t *testing.T) {
	v := runLive(t, idFaultCount)
	pages, _ := v.num("region_pages")
	if ps, _ := v.num("page_size_bytes"); ps*pages != regionBytes {
		t.Errorf("page size %v × %v pages != region size", ps, pages)
	}
	first, _ := v.num("b.first_touch.minflt")
	second, _ := v.num("b.second_touch.minflt")
	if first < pages-16 {
		t.Errorf("region B first touch: %v faults for %v pages", first, pages)
	}
	if second > first/10 {
		t.Errorf("region B second touch: %v faults, first %v", second, first)
	}
	for _, r := range []string{"a", "b"} {
		if rss, ok := v.num(r + ".dontneed.rss_bytes"); !ok || rss != 0 {
			t.Errorf("region %s Rss after MADV_DONTNEED: %v", r, rss)
		}
	}
}

func TestLivePageCacheResidency(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "machine-atlas")
	// A file left by a crashed run. 2^30 is above the kernel's largest
	// pid_max (2^22), so no live process has that PID.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, filePrefix+strconv.Itoa(1<<30)+fileSuffix)
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := runLive(t, idResidency, "XDG_CACHE_HOME="+cache)
	if v["file.removed"] != true {
		t.Errorf("file.removed = %v", v["file.removed"])
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 0 {
		t.Errorf("files left behind: %v", left)
	}
	if r, ok := v.num("residency.after_cold_read"); !ok || r < 0 || r > 100 {
		t.Errorf("residency after the cold read: %v", r)
	}
}

func TestChildUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"sh"}, {idFaultCount, "extra"}} {
		if code := ChildMain(args); code != 2 {
			t.Errorf("ChildMain(%q) = %d, want 2", args, code)
		}
	}
}

func TestParseSmapsVMA(t *testing.T) {
	smaps := `7f0000000000-7f0002000000 rw-p 00000000 00:00 0
Size:              32768 kB
Rss:               32768 kB
AnonHugePages:     30720 kB
VmFlags: rd wr mr mw me ac
7f0002000000-7f0004000000 rw-p 00000000 00:00 0
Size:              32768 kB
Rss:                   0 kB
AnonHugePages:         0 kB
`
	rss, huge, err := parseSmapsVMA([]byte(smaps), 0x7f0000000000)
	if err != nil || rss != 32<<20 || huge != 30<<20 {
		t.Errorf("first: rss %d huge %d err %v", rss, huge, err)
	}
	if rss, huge, err := parseSmapsVMA([]byte(smaps), 0x7f0002000000); err != nil || rss != 0 || huge != 0 {
		t.Errorf("second: rss %d huge %d err %v", rss, huge, err)
	}
	if _, _, err := parseSmapsVMA([]byte(smaps), 0x1000); err == nil {
		t.Error("missing mapping accepted")
	}
	if _, _, err := parseSmapsVMA([]byte("7f0000000000-7f0002000000 rw-p 0 00:00 0\nRss: x kB\nAnonHugePages: 0 kB\n"), 0x7f0000000000); err == nil {
		t.Error("malformed Rss accepted")
	}
}

func TestFSName(t *testing.T) {
	if fsName(0xef53) != "ext2/ext3/ext4" || fsName(0x01021994) != "tmpfs" || fsName(1) != "unknown (magic 0x1)" {
		t.Error("fsName")
	}
}
