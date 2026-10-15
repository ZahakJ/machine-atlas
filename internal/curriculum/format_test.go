package curriculum

import (
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		v      any
		unit   string
		format string
		want   string
	}{
		{int64(4096), obs.UnitBytes, "", "4 KiB"},
		{int64(1536), obs.UnitBytes, "auto", "1.5 KiB"},
		{int64(123456789), obs.UnitBytes, "", "118 MiB"},
		{int64(1023), obs.UnitBytes, "", "1023 B"},
		{int64(1048575), obs.UnitBytes, "", "1 MiB"}, // 1023.999 KiB rounds into the next unit
		{int64(17179869184), obs.UnitBytes, "", "16 GiB"},
		{int64(32856072192), obs.UnitBytes, "", "30.6 GiB"},
		{int64(-3 << 20), obs.UnitBytes, "", "−3 MiB"},
		{int64(140737488355328), obs.UnitBytes, "", "128 TiB"},
		{int64(8192), obs.UnitCount, "", "8 192"},
		{int64(1234567), "", "", "1 234 567"},
		{int64(999), obs.UnitCount, "", "999"},
		{int64(4096), obs.UnitBytes, "count", "4 096"},
		{int64(2048), obs.UnitPages, "", "2 048 pages"},
		{int64(1), "", "pages", "1 page"},
		{int64(48), obs.UnitBits, "", "48 bits"},
		{int64(140737488355328), obs.UnitBytes, "hex", "0x800000000000"},
		{"0x7ffd5e8c4000", "", "hex", "0x7ffd5e8c4000"},
		{0.625, "", "percent", "62.5%"},
		{0.12, obs.UnitPercent, "", "0.12%"},
		{12.0, obs.UnitPercent, "percent", "12%"},
		{0.0, "", "percent", "0%"},
		{0.0492, obs.UnitFraction, "", "4.92%"},
		{1.0, obs.UnitFraction, "auto", "100%"},
		{0.25, obs.UnitFraction, "percent", "25%"},
		{int64(4096), obs.UnitBytes, "raw", "4096"},
		{true, "", "", "yes"},
		{false, "", "raw", "false"},
		{"always", "", "", "always"},
		{"max", obs.UnitBytes, "", "max"},
		{42.5, obs.UnitSeconds, "", "42.5 s"},
		{93784.0, obs.UnitSeconds, "", "1 d 2 h"},
		{725.0, obs.UnitSeconds, "", "12 min 5 s"},
		{int64(250000), obs.UnitMicroseconds, "", "250 ms"},
		{int64(2800), obs.UnitMicroseconds, "", "2.8 ms"},
		{int64(401), obs.UnitMicroseconds, "", "401 µs"},
		{int64(0), obs.UnitMicroseconds, "", "0 s"},
		{0.8732, "", "", "0.873"},
		{[]map[string]any{{"a": int64(1)}, {"a": int64(2)}}, "", "", "2 rows"},
		{[]map[string]any{{"a": int64(1)}}, "", "bytes", "1 row"},
	}
	for _, tt := range tests {
		if got := Format(tt.v, tt.unit, tt.format); got != tt.want {
			t.Errorf("Format(%v, %q, %q) = %q, want %q", tt.v, tt.unit, tt.format, got, tt.want)
		}
	}
}

// TestGroupSeparator pins the digit-group separator to U+202F NARROW
// NO-BREAK SPACE, the one web/js/lib/format.js uses: a plain or thin space
// would let a number wrap across two lines.
func TestGroupSeparator(t *testing.T) {
	if got := Format(int64(1234567), obs.UnitCount, ""); got != "1\u202f234\u202f567" {
		t.Errorf("Format(1234567) = %+q, want groups separated by U+202F", got)
	}
}

func TestStatusText(t *testing.T) {
	tests := map[string]string{
		"permission_denied": "not available (permission denied)",
		"absent":            "not available (absent)",
		"unsupported":       "not available (unsupported here)",
		"failed":            "not available (collection failed)",
		"not_collected":     "not available (not collected)",
		"unknown":           "not determined",
	}
	for status, want := range tests {
		if got := StatusText(status); got != want {
			t.Errorf("StatusText(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestSourceText(t *testing.T) {
	tests := []struct {
		src  obs.Source
		want string
	}{
		{obs.File("/proc/meminfo", "MemTotal"), "/proc/meminfo (MemTotal)"},
		{obs.Command("lsblk", "--json"), "command lsblk --json"},
		{obs.Syscall("uname"), "uname(2)"},
		{obs.Probe("smaps"), "probe smaps"},
		{obs.Source{}, "no source recorded"},
	}
	for _, tt := range tests {
		if got := SourceText(tt.src); got != tt.want {
			t.Errorf("SourceText(%+v) = %q, want %q", tt.src, got, tt.want)
		}
	}
}
