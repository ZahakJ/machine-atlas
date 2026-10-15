package probe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
)

// vma is one line of /proc/<pid>/maps: "start-end perms offset dev inode
// [path]".
type vma struct {
	Start, End uint64
	Perms      string
	Offset     uint64
	Path       string // as printed: absolute path, "[stack]", "[anon: …]", or empty
}

// parseMapsLine parses one maps line. The path is everything after the
// inode, so names with spaces (" (deleted)", "[anon: Go: heap]") survive.
func parseMapsLine(line string) (vma, error) {
	var v vma
	rest := line
	var f [5]string
	for i := range f {
		rest = strings.TrimLeft(rest, " \t")
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		f[i], rest = rest[:end], rest[end:]
	}
	v.Path = strings.TrimLeft(rest, " \t")
	lo, hi, ok := strings.Cut(f[0], "-")
	if !ok {
		return v, fmt.Errorf("address range %q", clipText(f[0]))
	}
	var err error
	if v.Start, err = strconv.ParseUint(lo, 16, 64); err != nil {
		return v, fmt.Errorf("start address %q", clipText(lo))
	}
	if v.End, err = strconv.ParseUint(hi, 16, 64); err != nil || v.End <= v.Start {
		return v, fmt.Errorf("end address %q", clipText(hi))
	}
	if len(f[1]) != 4 || strings.Trim(f[1], "rwxps-") != "" {
		return v, fmt.Errorf("permissions %q", clipText(f[1]))
	}
	v.Perms = f[1]
	if v.Offset, err = strconv.ParseUint(f[2], 16, 64); err != nil {
		return v, fmt.Errorf("offset %q", clipText(f[2]))
	}
	if _, _, ok := strings.Cut(f[3], ":"); !ok {
		return v, fmt.Errorf("device %q", clipText(f[3]))
	}
	if _, err := strconv.ParseUint(f[4], 10, 64); err != nil {
		return v, fmt.Errorf("inode %q", clipText(f[4]))
	}
	return v, nil
}

// parseMaps parses /proc/<pid>/maps. VMAs must ascend and not overlap, as
// the kernel prints them.
func parseMaps(data []byte) ([]vma, error) {
	var out []vma
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		v, err := parseMapsLine(line)
		if err != nil {
			return nil, fmt.Errorf("maps line %d: bad %w", i+1, err)
		}
		if n := len(out); n > 0 && v.Start < out[n-1].End {
			return nil, fmt.Errorf("maps line %d: overlaps or precedes the previous mapping", i+1)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New("maps: no mappings")
	}
	return out, nil
}

// smapsVMA is one VMA of /proc/<pid>/smaps with its size fields in bytes,
// keyed by the kernel's names ("Rss", "Private_Dirty", "AnonHugePages").
type smapsVMA struct {
	vma
	Fields map[string]int64
}

// parseSmaps parses /proc/<pid>/smaps: a maps line, then "Key: N kB" lines
// for that VMA. Lines without a kB value (THPeligible, ProtectionKey,
// VmFlags) carry no sizes and are skipped.
func parseSmaps(data []byte) ([]smapsVMA, error) {
	var out []smapsVMA
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, val, isField := strings.Cut(line, ":")
		// A maps line also contains a colon (the device), but its first
		// word is an address range, which has no colon before the dash.
		if isField && !strings.ContainsAny(key, " \t-") {
			if len(out) == 0 {
				return nil, fmt.Errorf("smaps line %d: field before the first mapping", i+1)
			}
			if !strings.HasSuffix(strings.TrimSpace(val), " kB") {
				continue
			}
			n, _, err := collect.ParseKB(val)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("smaps line %d: %s: bad size %q", i+1, key, clipText(val))
			}
			out[len(out)-1].Fields[key] = n
			continue
		}
		v, err := parseMapsLine(line)
		if err != nil {
			return nil, fmt.Errorf("smaps line %d: bad %w", i+1, err)
		}
		if n := len(out); n > 0 && v.Start < out[n-1].End {
			return nil, fmt.Errorf("smaps line %d: overlaps or precedes the previous mapping", i+1)
		}
		out = append(out, smapsVMA{vma: v, Fields: map[string]int64{}})
	}
	if len(out) == 0 {
		return nil, errors.New("smaps: no mappings")
	}
	return out, nil
}

// displayPath reduces a mapping's path to what may leave the machine: the
// pseudo-names the kernel prints in brackets ("[stack]", "[anon: Go: heap]")
// and anything without a slash are kept, file paths become their basename
// ("/memfd:atlas-probe (deleted)" → "memfd:atlas-probe (deleted)").
func displayPath(p string) string {
	if p == "" || strings.HasPrefix(p, "[") || !strings.Contains(p, "/") {
		return p
	}
	return path.Base(p)
}

// vmaKind classifies a mapping for the maps sample.
func vmaKind(p string) string {
	switch {
	case p == "" || strings.HasPrefix(p, "[anon"):
		return "anon"
	case p == "[stack]":
		return "stack"
	case p == "[heap]":
		return "heap"
	case strings.HasPrefix(p, "["): // [vdso], [vvar], [vsyscall]: provided by the kernel
		return "kernel"
	case strings.HasPrefix(p, "/memfd:"), strings.HasPrefix(p, "/dev/shm/"), strings.HasPrefix(p, "/SYSV"):
		return "shmem"
	default:
		return "file"
	}
}

// layout finds the addresses a layout run reports in a process's own maps.
// exe is the executable's path as maps prints it. The mmap base is not
// printed anywhere; the end of the highest mapping below the stack, other
// than the kernel's vDSO and vvar pages, approximates it, because top-down
// mmap allocation starts there.
func layout(vmas []vma, exe string) (LayoutRun, error) {
	var l LayoutRun
	var mmapBase uint64
	var stack *vma
	for i := range vmas {
		if vmas[i].Path == "[stack]" {
			stack = &vmas[i]
		}
	}
	if stack == nil {
		return l, errors.New("no [stack] mapping")
	}
	for _, v := range vmas {
		p := strings.TrimSuffix(v.Path, " (deleted)")
		switch {
		case l.Exe == "" && p == exe:
			l.Exe = hex(v.Start)
		case v.Path == "[heap]":
			// A Go program allocates its heap with mmap and has a [heap]
			// (brk) mapping only if C code called malloc; recent Go
			// runtimes name their heap arenas instead.
			l.Heap, l.HeapKind = hex(v.Start), HeapBrk
		case l.Heap == "" && v.Path == "[anon: Go: heap]":
			l.Heap, l.HeapKind = hex(v.Start), HeapGoArena
		}
		if v.End <= stack.Start && p != exe && !strings.HasPrefix(v.Path, "[v") && v.Path != "[heap]" {
			mmapBase = max(mmapBase, v.End)
		}
	}
	if l.Exe == "" {
		return l, errors.New("no mapping of the executable")
	}
	if mmapBase == 0 {
		return l, errors.New("no mapping below the stack")
	}
	l.Stack, l.MmapBase = hex(stack.Start), hex(mmapBase)
	return l, nil
}

// Pagemap entry bits (Documentation/admin-guide/mm/pagemap.rst).
const (
	pmPresent = 1 << 63
	pmSwapped = 1 << 62
	pmPFNMask = 1<<55 - 1
)

// countPages counts present and swapped pages in raw pagemap entries (one
// native-endian uint64 per page), and
// whether any present page showed a frame number. Since Linux 4.2 the frame
// number reads as zero without CAP_SYS_ADMIN.
func countPages(entries []byte) (present, swapped int64, pfnVisible bool) {
	for i := 0; i+8 <= len(entries); i += 8 {
		e := binary.NativeEndian.Uint64(entries[i:])
		switch {
		case e&pmPresent != 0:
			present++
			pfnVisible = pfnVisible || e&pmPFNMask != 0
		case e&pmSwapped != 0:
			swapped++
		}
	}
	return present, swapped, pfnVisible
}

// clipText shortens s for an error message.
func clipText(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + "…"
}

// parseTHPCounters finds thp_fault_alloc and thp_fault_fallback in the
// text of /proc/vmstat; ok is false unless both are there.
func parseTHPCounters(vmstat []byte) (counts [2]int64, ok bool) {
	var found [2]bool
	for line := range strings.SplitSeq(string(vmstat), "\n") {
		key, val, _ := strings.Cut(line, " ")
		i := slices.Index([]string{"thp_fault_alloc", "thp_fault_fallback"}, key)
		if i < 0 {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return counts, false
		}
		counts[i], found[i] = n, true
	}
	return counts, found[0] && found[1]
}
