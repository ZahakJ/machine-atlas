package collect

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parsers for formats that several kernel interfaces share. Each rejects
// input it does not understand instead of guessing, so a format change shows
// up as a failed observation rather than a wrong number.

// Field is one "Key: value" line.
type Field struct {
	Key   string
	Value string // trimmed text after the first colon
}

// ParseFields parses "Key: value" lines, the format of /proc/meminfo,
// /proc/<pid>/status and the per-node meminfo files. Blank lines are
// skipped; a non-blank line without a colon is an error naming the line.
func ParseFields(data []byte) ([]Field, error) {
	var out []Field
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("line %d: expected \"key: value\", got %q", i+1, line)
		}
		out = append(out, Field{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
	}
	return out, nil
}

// ParseKB parses a meminfo-style value: "16318412 kB" yields the size in
// bytes and kb=true; a bare "42" (HugePages_Total) yields 42 and kb=false.
// The kernel's "kB" means KiB.
func ParseKB(s string) (value int64, kb bool, err error) {
	num, unit, _ := strings.Cut(strings.TrimSpace(s), " ")
	unit = strings.TrimSpace(unit)
	if unit != "" && unit != "kB" {
		return 0, false, fmt.Errorf("unexpected unit %q in %q", unit, s)
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("value %q: %w", s, err)
	}
	if unit == "" {
		return n, false, nil
	}
	if n > math.MaxInt64/1024 || n < math.MinInt64/1024 {
		return 0, false, fmt.Errorf("value %q overflows bytes", s)
	}
	return n * 1024, true, nil
}

// MaxCPU bounds CPU numbers in a CPU list, so a corrupt "0-4294967295"
// cannot allocate gigabytes. Linux's own NR_CPUS limit is 8192.
const MaxCPU = 1 << 16

// ParseCPUList parses the kernel's CPU list format ("0-3,8,10-11", as in
// /sys/devices/system/cpu/online) into ascending CPU numbers. An empty list
// is valid (no CPUs) and yields an empty slice.
func ParseCPUList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []int{}, nil
	}
	var out []int
	for part := range strings.SplitSeq(s, ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := cpuNumber(lo)
		if err != nil {
			return nil, fmt.Errorf("cpu list %q: %w", s, err)
		}
		b := a
		if isRange {
			if b, err = cpuNumber(hi); err != nil {
				return nil, fmt.Errorf("cpu list %q: %w", s, err)
			}
		}
		if b < a || (len(out) > 0 && a <= out[len(out)-1]) {
			return nil, fmt.Errorf("cpu list %q: ranges must ascend", s)
		}
		for c := a; c <= b; c++ {
			out = append(out, c)
		}
	}
	return out, nil
}

func cpuNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n >= MaxCPU {
		return 0, fmt.Errorf("bad cpu number %q", s)
	}
	return n, nil
}

// ParseChoice returns the selected word of a sysfs choice file such as
// "always [madvise] never". Exactly one word must be bracketed.
func ParseChoice(s string) (string, error) {
	var chosen []string
	for w := range strings.FieldsSeq(s) {
		if len(w) > 2 && w[0] == '[' && w[len(w)-1] == ']' {
			chosen = append(chosen, w[1:len(w)-1])
		}
	}
	if len(chosen) != 1 {
		return "", fmt.Errorf("choice %q: want exactly one [selected] word, found %d", strings.TrimSpace(s), len(chosen))
	}
	return chosen[0], nil
}

// ParseSize parses a size with an optional binary suffix, as sysfs cache and
// hugepage files write them: "32K", "1M", "2048", "512 KB". K, M, G and T are
// powers of 1024; a trailing "B" or "iB" is accepted.
func ParseSize(s string) (int64, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimSuffix(strings.TrimSuffix(t, "B"), "i")
	t = strings.TrimSpace(t)
	if t == "" {
		return 0, errors.New("empty size")
	}
	shift := 0
	switch t[len(t)-1] {
	case 'K', 'k':
		shift = 10
	case 'M':
		shift = 20
	case 'G':
		shift = 30
	case 'T':
		shift = 40
	}
	if shift > 0 {
		t = strings.TrimSpace(t[:len(t)-1])
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("size %q: not a non-negative integer", s)
	}
	if n > math.MaxInt64>>shift {
		return 0, fmt.Errorf("size %q overflows", s)
	}
	return n << shift, nil
}
