package curriculum

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Chip formats an author may request with {{obs:ID|fmt}}.
var formats = []string{"auto", "bytes", "count", "pages", "hex", "percent", "raw"}

const (
	// groupSep groups digits: U+202F NARROW NO-BREAK SPACE, as the web UI
	// (web/js/lib/format.js GROUP) writes it, so a number never wraps
	// across two lines: 8 192.
	groupSep = "\u202f"
	minus    = "−" // a typographic minus for negative quantities
)

// Format renders a value for lesson text. It is deterministic and
// locale-free, so the same value always reads the same:
//
//   - bytes: IEC units with at most three significant digits, trailing zeros
//     dropped (4 KiB, 1.5 GiB, 118 MiB), matching web/js/lib/format.js
//   - count: integers grouped by narrow no-break spaces (8 192)
//   - pages: a count followed by "pages"
//   - hex: 0x-prefixed lower-case hexadecimal
//   - percent: a value whose unit is percent as written, any other number
//     as a fraction of one (0.25 → 25%)
//   - auto with unit "fraction": a percentage too, so a stored 0.0492
//     reads 4.92% in chips, evidence rows and everywhere else
//   - raw: the value as stored
//   - auto (or ""): chosen by unit; seconds and microseconds read as a
//     duration (in µs or ms below a second), bits as "48 bits", booleans as
//     yes/no, tables as a row count
//
// The inspector always shows the exact value beside the formatted one.
func Format(v any, unit, format string) string {
	if t, ok := v.([]map[string]any); ok {
		return plural(int64(len(t)), "row")
	}
	switch format {
	case "bytes":
		if n, ok := obs.Float(v); ok {
			return formatBytes(n)
		}
	case "count":
		if n, ok := obs.Float(v); ok {
			return formatCount(n)
		}
	case "pages":
		if n, ok := obs.Int(v); ok {
			return plural(n, "page")
		}
	case "hex":
		if n, ok := obs.Int(v); ok && n >= 0 {
			return "0x" + strconv.FormatInt(n, 16)
		}
		if s, ok := v.(string); ok {
			return s
		}
	case "percent":
		if n, ok := obs.Float(v); ok {
			if unit != obs.UnitPercent {
				n *= 100
			}
			return significant(n) + "%"
		}
	case "raw":
		return rawString(v)
	}
	return formatAuto(v, unit)
}

func formatAuto(v any, unit string) string {
	switch x := v.(type) {
	case nil:
		return "—"
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case string:
		return x
	}
	n, ok := obs.Float(v)
	if !ok {
		return rawString(v)
	}
	switch unit {
	case obs.UnitBytes:
		return formatBytes(n)
	case obs.UnitPages:
		if i, ok := obs.Int(v); ok {
			return plural(i, "page")
		}
	case obs.UnitBits:
		return formatCount(n) + " bits"
	case obs.UnitPercent:
		return significant(n) + "%"
	case obs.UnitFraction:
		return significant(n*100) + "%"
	case obs.UnitSeconds:
		return formatDuration(n)
	case obs.UnitMicroseconds:
		return formatDuration(n / 1e6)
	case obs.UnitCount:
		return formatCount(n)
	}
	if _, isInt := v.(int64); isInt {
		return formatCount(n)
	}
	return significant(n)
}

// rawString is the value as stored: strings unchanged, numbers in plain
// decimal, booleans as true/false. It is also how values compare against
// the expected value of a :::when branch or a relevance rule.
func rawString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []map[string]any:
		return plural(int64(len(x)), "row")
	}
	return fmt.Sprint(v)
}

var iec = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

func formatBytes(n float64) string {
	sign := ""
	if n < 0 {
		sign, n = minus, -n
	}
	unit := 0
	for n >= 1024 && unit < len(iec)-1 {
		n /= 1024
		unit++
	}
	if unit == 0 {
		return sign + strconv.FormatFloat(math.Round(n), 'f', 0, 64) + " B"
	}
	text := significant(n)
	// Rounding can carry into the next unit: 1023.9 KiB reads 1 MiB.
	if v, _ := strconv.ParseFloat(strings.ReplaceAll(text, groupSep, ""), 64); v >= 1024 && unit < len(iec)-1 {
		unit++
		text = significant(v / 1024)
	}
	return sign + text + " " + iec[unit]
}

// significant writes n with at most three significant digits and no
// trailing zeros; values of 100 and more are rounded to whole numbers and
// grouped like counts.
func significant(n float64) string {
	if n == 0 {
		return "0"
	}
	a := math.Abs(n)
	if a >= 100 {
		return formatCount(math.Round(n))
	}
	decimals := 2 - int(math.Floor(math.Log10(a)))
	s := strconv.FormatFloat(n, 'f', decimals, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func formatCount(n float64) string {
	if n != math.Trunc(n) || math.Abs(n) >= 1<<63 {
		return significant(n)
	}
	i := int64(n)
	sign := ""
	if i < 0 {
		sign, i = minus, -i
	}
	digits := strconv.FormatInt(i, 10)
	var b strings.Builder
	for k, d := range digits {
		if k > 0 && (len(digits)-k)%3 == 0 {
			b.WriteString(groupSep)
		}
		b.WriteRune(d)
	}
	return sign + b.String()
}

func plural(n int64, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return formatCount(float64(n)) + " " + word + "s"
}

// formatDuration reads seconds as the two largest units: "3 d 4 h",
// "12 min 5 s", "42 s", "0.25 s".
func formatDuration(s float64) string {
	if s < 0 {
		return minus + formatDuration(-s)
	}
	switch {
	case s > 0 && s < 1e-3:
		return significant(s*1e6) + " µs"
	case s > 0 && s < 1:
		return significant(s*1e3) + " ms"
	case s < 60:
		return significant(s) + " s"
	}
	total := int64(math.Round(s))
	units := []struct {
		name string
		size int64
	}{{"d", 86400}, {"h", 3600}, {"min", 60}, {"s", 1}}
	var parts []string
	for _, u := range units {
		if total >= u.size || len(parts) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", total/u.size, u.name))
			total %= u.size
		}
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// StatusText is the wording shown in place of a value whose observation is
// not ok: "not available (permission denied)". Derived facts that are
// unknown read "not determined".
func StatusText(status string) string {
	switch status {
	case "unknown":
		return "not determined"
	case string(obs.StatusAbsent):
		return "not available (absent)"
	case string(obs.StatusPermissionDenied):
		return "not available (permission denied)"
	case string(obs.StatusUnsupported):
		return "not available (unsupported here)"
	case string(obs.StatusFailed):
		return "not available (collection failed)"
	case string(obs.StatusNotCollected):
		return "not available (not collected)"
	}
	return "not available (" + strings.ReplaceAll(status, "_", " ") + ")"
}

// SourceText describes where an observation came from in one line, the way
// the evidence tables and the generated unknown branches cite it.
func SourceText(src obs.Source) string {
	var s string
	switch src.Type {
	case obs.SourceFile:
		s = src.Path
	case obs.SourceCommand:
		s = "command " + src.Path
	case obs.SourceSyscall:
		s = src.Path + "(2)"
	case obs.SourceProbe:
		s = "probe " + src.Path
	default:
		return "no source recorded"
	}
	if src.Field != "" {
		s += " (" + src.Field + ")"
	}
	return s
}
