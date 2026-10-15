//go:build linux

package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// childLifetime bounds the child whatever the parent does: if nobody closes
// its stdin, it exits anyway.
const childLifetime = 5 * time.Second

const (
	mib = 1 << 20
	// regionAlign is the boundary of each region in the guarded fallback
	// layout, the alignment Linux 6.7 and later give a large anonymous
	// mmap by themselves.
	regionAlign = 2 * mib
	// fileMapMax is the most of the executable the file-private region maps.
	fileMapMax = 4 * mib
	// anonNamePrefix starts the name each anonymous region is given with
	// PR_SET_VMA_ANON_NAME; maps and smaps print "[anon: atlas probe: …]".
	anonNamePrefix = " atlas probe: "
)

// childRegion is a region the child has mapped.
type childRegion struct {
	name, backing string
	mem           []byte
	touched       int
}

// readSink keeps the read loops from being optimised away.
var readSink byte

// ChildMain is the entry point of "atlas __probe"; it returns the exit code.
// With no arguments it makes the five regions, prints the report as one
// JSON line, and waits until stdin is closed. With "layout" it prints only
// its address layout and exits: the minimal second execution.
func ChildMain(args []string) int {
	layoutOnly := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == layoutArg:
		layoutOnly = true
	default:
		fmt.Fprintln(os.Stderr, "usage: atlas __probe [layout] (started by atlas collect; not for direct use)")
		return 2
	}
	time.AfterFunc(childLifetime, func() { os.Exit(3) })
	var r *Report
	var err error
	if layoutOnly {
		var l LayoutRun
		l, err = selfLayout()
		r = &Report{LayoutRuns: []LayoutRun{l}}
	} else {
		r, err = probe()
	}
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(r)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "atlas __probe:", err)
		return 1
	}
	if !layoutOnly {
		// Stay alive, mappings intact, while the parent reads /proc/<pid>.
		io.Copy(io.Discard, os.Stdin)
	}
	return 0
}

// probe makes and touches the regions and measures them.
func probe() (*Report, error) {
	// One OS thread and no garbage collector: the fault counters read below
	// are per thread, and a collection cycle would fault pages of its own.
	runtime.LockOSThread()
	debug.SetGCPercent(-1)
	ps := unix.Getpagesize()

	regions, placement, err := mapRegions(ps)
	if err != nil {
		return nil, err
	}
	written, read, file, shmem := regions[1], regions[2], regions[3], regions[4]
	for i := 0; i < len(read.mem); i += ps {
		readSink += read.mem[i]
	}
	for i := 0; i < len(file.mem); i += ps {
		readSink += file.mem[i]
	}
	for i := 0; i < shmem.touched; i += ps {
		shmem.mem[i] = 1
	}

	// Pagemap entries for the largest region, allocated now so nothing is
	// allocated between the fault readings or while counting.
	pagemapBuf := make([]byte, len(regions[0].mem)/ps*8)

	// The write loop between two getrusage(RUSAGE_THREAD) readings. Nothing
	// in between allocates, and the thread is locked, so the delta is this
	// loop's faults. It is still not exact: the first execution of the loop
	// can fault in its own code page, the Go runtime may preempt the thread
	// with a signal whose handler touches memory, and with THP one fault
	// maps 2 MiB, so the delta can be far below the page count.
	//
	// The system-wide THP fault counters are read just outside that
	// window, so reading /proc/vmstat adds no faults to the delta.
	var before, after unix.Rusage
	thpBefore, thpOK := thpCounters()
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &before); err != nil {
		return nil, fmt.Errorf("getrusage: %w", err)
	}
	for i := 0; i < len(written.mem); i += ps {
		written.mem[i] = 1
	}
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &after); err != nil {
		return nil, fmt.Errorf("getrusage: %w", err)
	}
	thpAfter, thpAfterOK := thpCounters()

	r := &Report{
		Faults: &Faults{
			MinfltBefore: int64(before.Minflt), MinfltAfter: int64(after.Minflt),
			MajfltBefore: int64(before.Majflt), MajfltAfter: int64(after.Majflt),
		},
		Pagemap:   &Pagemap{},
		Placement: placement,
	}
	if thpOK && thpAfterOK {
		r.THPFaults = &THPFaults{
			FaultAllocDelta:    thpAfter[0] - thpBefore[0],
			FaultFallbackDelta: thpAfter[1] - thpBefore[1],
		}
	}
	pagemap, err := os.Open("/proc/self/pagemap")
	r.Pagemap.Readable = err == nil
	if err == nil {
		defer pagemap.Close()
	}
	for _, g := range regions {
		start := uint64(uintptr(unsafe.Pointer(&g.mem[0])))
		pages := len(g.mem) / ps
		reg := Region{
			Name: g.name, Start: hex(start), End: hex(start + uint64(len(g.mem))),
			SizeBytes: int64(len(g.mem)), TouchedBytes: int64(g.touched),
			Backing: g.backing, PagesTotal: int64(pages),
		}
		if r.Pagemap.Readable {
			buf := pagemapBuf[:pages*8]
			if _, err := pagemap.ReadAt(buf, int64(start/uint64(ps))*8); err != nil {
				return nil, fmt.Errorf("read pagemap for %s: %w", g.name, err)
			}
			present, swapped, pfn := countPages(buf)
			reg.PagesPresent, reg.PagesSwapped = &present, &swapped
			r.Pagemap.PFNVisible = r.Pagemap.PFNVisible || pfn
		}
		r.Regions = append(r.Regions, reg)
	}

	l, err := selfLayout()
	if err != nil {
		return nil, err
	}
	r.LayoutRuns = []LayoutRun{l}
	return r, nil
}

// thpCounters reads thp_fault_alloc and thp_fault_fallback from
// /proc/vmstat; ok is false when the file cannot be read or lacks either
// (a kernel built without THP).
func thpCounters() (counts [2]int64, ok bool) {
	data, err := os.ReadFile("/proc/vmstat")
	if err != nil {
		return counts, false
	}
	return parseTHPCounters(data)
}

// regionSpec is one region to map.
type regionSpec struct {
	name, backing string
	fd, size      int
	prot, flags   int
	touched       int
}

// mapRegions makes the five regions the way an ordinary program would: one
// mmap each, with no address hint, so the kernel chooses where they go and
// how they are aligned (Linux 6.7 and later place an anonymous mapping
// whose length is a multiple of 2 MiB on a 2 MiB boundary; older kernels
// do not, and then fewer of its 2 MiB ranges can hold a huge page). Each
// anonymous region is named with PR_SET_VMA_ANON_NAME (Linux 5.17 and
// later, CONFIG_ANON_VMA_NAME), which changes neither faults nor THP but
// keeps the kernel from merging it with an adjacent anonymous mapping and
// labels it in maps. A kernel that cannot name mappings gets the guarded
// layout instead. The second result says which layout was used.
func mapRegions(ps int) ([]childRegion, string, error) {
	exe, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, "", err
	}
	defer exe.Close()
	fi, err := exe.Stat()
	if err != nil {
		return nil, "", err
	}
	// Whole pages only: touching a page beyond the end of the file would
	// raise SIGBUS.
	fileLen := min(fileMapMax, int(fi.Size())/ps*ps)
	if fileLen == 0 {
		return nil, "", errors.New("executable is smaller than a page")
	}
	memfd, err := unix.MemfdCreate("atlas-probe", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, "", fmt.Errorf("memfd_create: %w", err)
	}
	defer unix.Close(memfd) // the mapping keeps the memory alive
	if err := unix.Ftruncate(memfd, 4*mib); err != nil {
		return nil, "", fmt.Errorf("ftruncate memfd: %w", err)
	}

	const (
		rw   = unix.PROT_READ | unix.PROT_WRITE
		anon = unix.MAP_PRIVATE | unix.MAP_ANONYMOUS
	)
	specs := []regionSpec{
		{"anon-untouched", "anon", -1, 64 * mib, rw, anon, 0},
		{"anon-written", "anon", -1, 8 * mib, rw, anon, 8 * mib},
		{"anon-read", "anon", -1, 4 * mib, rw, anon, 4 * mib},
		{"file-private", "file", int(exe.Fd()), fileLen, unix.PROT_READ, unix.MAP_PRIVATE, fileLen},
		{"shmem-shared", "shmem", memfd, 4 * mib, rw, unix.MAP_SHARED, 1 * mib},
	}
	out, named, err := mapOrdinary(specs)
	if err != nil || named {
		return out, PlacementMmap, err
	}
	out, err = mapGuarded(specs, ps)
	return out, PlacementGuarded, err
}

// mapOrdinary maps each region with a plain mmap and names the anonymous
// ones. If the kernel refuses the first name, it unmaps everything and
// reports named false.
func mapOrdinary(specs []regionSpec) (out []childRegion, named bool, err error) {
	for _, s := range specs {
		p, err := unix.MmapPtr(s.fd, 0, nil, uintptr(s.size), s.prot, s.flags)
		if err != nil {
			return nil, false, fmt.Errorf("mmap %s: %w", s.name, err)
		}
		out = append(out, childRegion{s.name, s.backing, unsafe.Slice((*byte)(p), s.size), s.touched})
		if s.backing != "anon" {
			continue
		}
		name, err := unix.BytePtrFromString(anonNamePrefix + s.name)
		if err != nil {
			return nil, false, err
		}
		if err := unix.Prctl(unix.PR_SET_VMA, unix.PR_SET_VMA_ANON_NAME, uintptr(p), uintptr(s.size), uintptr(unsafe.Pointer(name))); err != nil {
			for _, r := range out {
				unix.MunmapPtr(unsafe.Pointer(&r.mem[0]), uintptr(len(r.mem)))
			}
			return nil, false, nil
		}
	}
	return out, true, nil
}

// mapGuarded is the fallback for kernels that cannot name a mapping: the
// regions go inside one inaccessible reservation, each on a 2 MiB boundary
// with at least one PROT_NONE guard page on each side. The guards keep the
// kernel from merging a region with its neighbour into one VMA, which would
// mix their smaps figures. The alignment is the probe's choice here, not
// the kernel's.
func mapGuarded(specs []regionSpec, ps int) ([]childRegion, error) {
	total := regionAlign
	for _, s := range specs {
		total += alignUp(s.size+ps, regionAlign)
	}
	base, err := unix.MmapPtr(-1, 0, nil, uintptr(total), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS|unix.MAP_NORESERVE)
	if err != nil {
		return nil, fmt.Errorf("reserve address space: %w", err)
	}
	baseAddr := int(uintptr(base))
	off := alignUp(baseAddr+ps, regionAlign) - baseAddr
	var out []childRegion
	for _, s := range specs {
		p, err := unix.MmapPtr(s.fd, 0, unsafe.Add(base, off), uintptr(s.size), s.prot, s.flags|unix.MAP_FIXED)
		if err != nil {
			return nil, fmt.Errorf("mmap %s: %w", s.name, err)
		}
		out = append(out, childRegion{s.name, s.backing, unsafe.Slice((*byte)(p), s.size), s.touched})
		off = alignUp(baseAddr+off+s.size+ps, regionAlign) - baseAddr
	}
	return out, nil
}

func alignUp(n, align int) int { return (n + align - 1) / align * align }

// selfLayout reads the child's own maps for the addresses of a layout run.
func selfLayout() (LayoutRun, error) {
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return LayoutRun{}, err
	}
	vmas, err := parseMaps(data)
	if err != nil {
		return LayoutRun{}, err
	}
	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return LayoutRun{}, err
	}
	l, err := layout(vmas, strings.TrimSuffix(exe, " (deleted)"))
	if err != nil {
		return LayoutRun{}, fmt.Errorf("layout: %w", err)
	}
	l.Run = 1
	return l, nil
}
