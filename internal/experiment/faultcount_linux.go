//go:build linux

package experiment

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	regionBytes = 32 * mib
	// hugeAlign places each region on a 2 MiB boundary, so region A can be
	// backed by PMD-sized transparent huge pages where the kernel allows.
	hugeAlign = 2 * mib
)

var faultNotes = []string{
	"Faults are counted for the experiment's own thread (getrusage with RUSAGE_THREAD). The thread is locked, garbage collection is off, and nothing is allocated between two readings of the counters, so the Go runtime's other threads are not counted.",
	"The counts are still not exact: the Go runtime may interrupt the thread with a preemption signal whose handler touches memory, and any fault the thread takes on its own stack or code between the readings is counted too.",
	"With transparent huge pages one fault can map 2 MiB, so a fault count is not a page count; khugepaged may also collapse small pages into huge ones between readings.",
	"Rss and AnonHugePages are read from /proc/self/smaps for each region's mapping; the kernel reports them in kB, converted here to bytes.",
}

// faultCount runs the fault-count experiment.
func faultCount(rec *recorder) ([]string, error) {
	ps := unix.Getpagesize()
	rec.add("page_size_bytes", "setup", "Base page size", int64(ps), "bytes")
	rec.add("thp.enabled", "setup", "Transparent huge page mode (/sys/kernel/mm/transparent_hugepage/enabled)", thpMode(), "")
	rec.add("region_bytes", "setup", "Size of each region", int64(regionBytes), "bytes")
	rec.add("region_pages", "setup", "Base pages in each region", int64(regionBytes/ps), "count")

	a, b, release, err := mapRegions(ps)
	if err != nil {
		return nil, err
	}
	defer release()
	advice := "applied"
	if err := unix.Madvise(b, unix.MADV_NOHUGEPAGE); err != nil {
		advice = "refused: " + err.Error()
	}
	rec.add("b.nohugepage_advice", "setup", "Region B: madvise(MADV_NOHUGEPAGE)", advice, "")

	// Run the touch loop and the counter reading once on memory that is
	// already present, so their own code and stack pages are not faulted
	// in during the first measurement.
	warm := make([]byte, 2*ps)
	touchPages(warm, ps)
	countFaults(warm, ps)

	for _, r := range []struct {
		name, title string
		mem         []byte
	}{{"a", "Region A (default policy)", a}, {"b", "Region B (MADV_NOHUGEPAGE)", b}} {
		if err := measureRegion(rec, r.name, r.title, r.mem, ps); err != nil {
			return nil, err
		}
	}
	return faultNotes, nil
}

// measureRegion records the fault and smaps figures for one region.
func measureRegion(rec *recorder, name, title string, mem []byte, ps int) error {
	faults := func(step, what string) error {
		minor, major, err := countFaults(mem, ps)
		if err != nil {
			return err
		}
		rec.add(name+"."+strings.ReplaceAll(step, "-", "_")+".minflt", name+"."+step, title+": minor faults during "+what, minor, "count")
		rec.add(name+"."+strings.ReplaceAll(step, "-", "_")+".majflt", name+"."+step, title+": major faults during "+what, major, "count")
		return nil
	}
	smaps := func(step, when string) error {
		rss, huge, err := regionSmaps(mem)
		if err != nil {
			return err
		}
		rec.add(name+"."+strings.ReplaceAll(step, "-", "_")+".rss_bytes", name+"."+step, title+": Rss "+when, rss, "bytes")
		rec.add(name+"."+strings.ReplaceAll(step, "-", "_")+".anon_huge_bytes", name+"."+step, title+": AnonHugePages "+when, huge, "bytes")
		return nil
	}
	if err := faults("first-touch", "the first write to every page"); err != nil {
		return err
	}
	if err := smaps("first-touch", "after the first write"); err != nil {
		return err
	}
	if err := faults("second-touch", "the second write to every page"); err != nil {
		return err
	}
	if err := unix.Madvise(mem, unix.MADV_DONTNEED); err != nil {
		return fmt.Errorf("madvise(MADV_DONTNEED): %w", err)
	}
	if err := smaps("dontneed", "after MADV_DONTNEED"); err != nil {
		return err
	}
	return faults("touch-again", "the write after MADV_DONTNEED")
}

// countFaults writes one byte to every page of mem between two readings of
// the thread's fault counters and returns the differences. Nothing between
// the readings allocates.
func countFaults(mem []byte, ps int) (minor, major int64, err error) {
	var before, after unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &before); err != nil {
		return 0, 0, fmt.Errorf("getrusage: %w", err)
	}
	touchPages(mem, ps)
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &after); err != nil {
		return 0, 0, fmt.Errorf("getrusage: %w", err)
	}
	return int64(after.Minflt - before.Minflt), int64(after.Majflt - before.Majflt), nil
}

// touchPages writes one byte to every page of mem.
func touchPages(mem []byte, ps int) {
	for i := 0; i < len(mem); i += ps {
		mem[i] = 1
	}
}

// mapRegions maps regions A and B inside one inaccessible reservation, each
// on a 2 MiB boundary with a PROT_NONE guard on both sides. The guards keep
// the kernel from merging a region with a neighbouring anonymous mapping
// (the Go heap is one), which would mix their smaps figures.
func mapRegions(ps int) (a, b []byte, release func(), err error) {
	slot := alignUp(regionBytes+ps, hugeAlign)
	total := hugeAlign + 2*slot
	const anon = unix.MAP_PRIVATE | unix.MAP_ANONYMOUS
	base, err := unix.MmapPtr(-1, 0, nil, uintptr(total), unix.PROT_NONE, anon|unix.MAP_NORESERVE)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reserve address space: %w", err)
	}
	release = func() { unix.MunmapPtr(base, uintptr(total)) }
	off := alignUp(int(uintptr(base))+ps, hugeAlign) - int(uintptr(base))
	var regions [2][]byte
	for i := range regions {
		p, err := unix.MmapPtr(-1, 0, unsafe.Add(base, off), regionBytes, unix.PROT_READ|unix.PROT_WRITE, anon|unix.MAP_FIXED)
		if err != nil {
			release()
			return nil, nil, nil, fmt.Errorf("mmap region: %w", err)
		}
		regions[i] = unsafe.Slice((*byte)(p), regionBytes)
		off += slot
	}
	return regions[0], regions[1], release, nil
}

func alignUp(n, align int) int { return (n + align - 1) / align * align }

// thpMode returns the bracketed choice in the THP "enabled" file, or a word
// saying why there is none.
func thpMode() string {
	data, err := os.ReadFile("/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil {
		return "unavailable"
	}
	s := string(data)
	if i := strings.IndexByte(s, '['); i >= 0 {
		if j := strings.IndexByte(s[i:], ']'); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return "unreadable"
}

// regionSmaps returns the Rss and AnonHugePages, in bytes, of the mapping
// that starts at mem.
func regionSmaps(mem []byte) (rss, huge int64, err error) {
	data, err := os.ReadFile("/proc/self/smaps")
	if err != nil {
		return 0, 0, noPath(err)
	}
	return parseSmapsVMA(data, uint64(uintptr(unsafe.Pointer(&mem[0]))))
}

// parseSmapsVMA finds the VMA starting at start in smaps text and returns
// its Rss and AnonHugePages in bytes.
func parseSmapsVMA(data []byte, start uint64) (rss, huge int64, err error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	in, found, haveRSS, haveHuge := false, false, false, false
	for sc.Scan() {
		line := sc.Text()
		key, rest, isField := strings.Cut(line, ":")
		if !isField || strings.Contains(key, "-") {
			// A VMA header: "start-end perms offset dev inode [path]";
			// the dev field contains the colon.
			lo, _, _ := strings.Cut(line, "-")
			addr, perr := strconv.ParseUint(lo, 16, 64)
			in = perr == nil && addr == start
			found = found || in
			continue
		}
		if !in {
			continue
		}
		var dst *int64
		switch key {
		case "Rss":
			dst, haveRSS = &rss, true
		case "AnonHugePages":
			dst, haveHuge = &huge, true
		default:
			continue
		}
		kb, perr := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "kB")), 10, 64)
		if perr != nil {
			return 0, 0, fmt.Errorf("smaps %s: %w", key, perr)
		}
		*dst = kb << 10
	}
	switch {
	case !found:
		return 0, 0, fmt.Errorf("no mapping at %#x in smaps", start)
	case !haveRSS || !haveHuge:
		return 0, 0, fmt.Errorf("mapping at %#x lacks Rss or AnonHugePages in smaps", start)
	}
	return rss, huge, sc.Err()
}
