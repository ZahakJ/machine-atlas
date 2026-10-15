// Package obs defines the observation: one value read from the machine,
// together with where it came from, when, at what scope, and whether the
// reading succeeded. Every other layer of Machine Atlas consumes these.
//
// The central rule is that a value is never fabricated: when an interface
// cannot answer, the observation still exists, with a null value and a
// status that says why (absent, permission denied, unsupported, failed or not
// collected), so lessons can explain the gap instead of hiding it.
package obs

import (
	"regexp"
	"strings"
	"time"
)

// Status says whether an observation holds a value and, if not, why not.
type Status string

const (
	// StatusOK means the value was read and parsed.
	StatusOK Status = "ok"
	// StatusAbsent means the interface answered that the thing does not
	// exist here (ENOENT, an empty /proc/swaps, a missing tool).
	StatusAbsent Status = "absent"
	// StatusPermissionDenied means the kernel refused the read (EACCES/EPERM).
	StatusPermissionDenied Status = "permission_denied"
	// StatusUnsupported means the id does not apply here by rule: an x86-only
	// flag on arm64, a cgroup v2 file on a v1 hierarchy, a non-Linux build.
	StatusUnsupported Status = "unsupported"
	// StatusFailed means collection was attempted and went wrong: a parse
	// error, a timeout, an unexpected format.
	StatusFailed Status = "failed"
	// StatusNotCollected means the id was deliberately skipped (a flag, a
	// disabled collector, an exhausted time budget).
	StatusNotCollected Status = "not_collected"
)

// Valid reports whether s is one of the six defined statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusOK, StatusAbsent, StatusPermissionDenied, StatusUnsupported, StatusFailed, StatusNotCollected:
		return true
	}
	return false
}

// Kind separates configuration from measurement, because a snapshot's age
// matters for the second and hardly at all for the first.
type Kind string

const (
	// KindStatic is configuration, stable across a boot or longer.
	KindStatic Kind = "static"
	// KindLive is a measurement that varies from one reading to the next.
	KindLive Kind = "live"
)

// Valid reports whether k is a defined kind.
func (k Kind) Valid() bool { return k == KindStatic || k == KindLive }

// Scope says whose view a value describes. The same file can mean different
// things inside a container, so the scope travels with the value.
type Scope string

const (
	// ScopeKernel is system-wide as seen by this kernel instance.
	ScopeKernel Scope = "kernel"
	// ScopeCgroup is the control group atlas ran in.
	ScopeCgroup Scope = "cgroup"
	// ScopeProcess is the atlas process or its probe child.
	ScopeProcess Scope = "process"
	// ScopeNamespace is what is visible from atlas's namespaces (mounts,
	// network interfaces, hostname, PID 1).
	ScopeNamespace Scope = "namespace"
)

// Valid reports whether s is a defined scope.
func (s Scope) Valid() bool {
	switch s {
	case ScopeKernel, ScopeCgroup, ScopeProcess, ScopeNamespace:
		return true
	}
	return false
}

// Units an observation may carry. Kernel values reported in kB are converted
// to bytes at collection, so there is deliberately no kilobyte unit.
const (
	UnitBytes        = "bytes"
	UnitPages        = "pages"
	UnitCount        = "count"
	UnitBits         = "bits"
	UnitPercent      = "percent"
	UnitSeconds      = "seconds"
	UnitMicroseconds = "microseconds"
	// UnitFraction is a ratio between 0 and 1, stored as such and shown as
	// a percentage. Derived "…_fraction" facts carry it.
	UnitFraction = "fraction"
)

// ValidUnit reports whether u is a defined unit or empty (dimensionless or
// textual values).
func ValidUnit(u string) bool {
	switch u {
	case "", UnitBytes, UnitPages, UnitCount, UnitBits, UnitPercent, UnitSeconds, UnitMicroseconds, UnitFraction:
		return true
	}
	return false
}

// Source types: where a value was read from.
const (
	SourceFile    = "file"
	SourceCommand = "command"
	SourceSyscall = "syscall"
	SourceProbe   = "probe"
)

// Source is the provenance of an observation, shown in the inspector so a
// reader can repeat the reading by hand.
type Source struct {
	Type   string `json:"type"`             // "file" | "command" | "syscall" | "probe"
	Path   string `json:"path,omitempty"`   // "/proc/meminfo"; for commands the argv joined by spaces
	Field  string `json:"field,omitempty"`  // original field name in the source, e.g. "Active(anon)"
	Detail string `json:"detail,omitempty"` // e.g. "converted from kB"
}

// File returns a file source; field is the original name of the field read,
// or empty when the whole file is the value.
func File(path, field string) Source { return Source{Type: SourceFile, Path: path, Field: field} }

// Command returns a command source for the given argv.
func Command(argv ...string) Source {
	return Source{Type: SourceCommand, Path: strings.Join(argv, " ")}
}

// Syscall returns a syscall source, e.g. Syscall("uname").
func Syscall(name string) Source { return Source{Type: SourceSyscall, Path: name} }

// Probe returns a probe source; what names the probe measurement or file.
func Probe(what string) Source { return Source{Type: SourceProbe, Path: what} }

// Observation is one value with its provenance. Value is int64, float64,
// string, bool, a table ([]map[string]any of flat cells) or nil; nil exactly
// when Status is not ok.
type Observation struct {
	ID          string `json:"id"`
	Value       any    `json:"value"`
	Unit        string `json:"unit,omitempty"`
	Kind        Kind   `json:"kind"`
	Scope       Scope  `json:"scope"`
	Status      Status `json:"status"`
	Source      Source `json:"source"`
	CollectedAt string `json:"collected_at"` // RFC 3339 UTC, second precision
	Collector   string `json:"collector"`
	Note        string `json:"note,omitempty"`
}

// OK returns a successful observation. Kind, scope, unit, collector and time
// are filled in by the collection runner from the catalog and environment
// unless the collector sets them.
func OK(id string, value any, src Source) Observation {
	return Observation{ID: id, Value: value, Status: StatusOK, Source: src}
}

func missing(id string, status Status, src Source, note string) Observation {
	return Observation{ID: id, Status: status, Source: src, Note: note}
}

// Absent returns an observation for something the interface says is not
// there.
func Absent(id string, src Source, note string) Observation {
	return missing(id, StatusAbsent, src, note)
}

// Denied returns an observation for a read the kernel refused.
func Denied(id string, src Source, note string) Observation {
	return missing(id, StatusPermissionDenied, src, note)
}

// Unsupported returns an observation for an id that does not apply here by
// rule; note should name the rule.
func Unsupported(id string, src Source, note string) Observation {
	return missing(id, StatusUnsupported, src, note)
}

// Failed returns an observation for an attempt that went wrong.
func Failed(id string, src Source, note string) Observation {
	return missing(id, StatusFailed, src, note)
}

// NotCollected returns an observation for an id that was deliberately skipped.
func NotCollected(id string, src Source, note string) Observation {
	return missing(id, StatusNotCollected, src, note)
}

// Errored returns the non-ok observation that err implies, with the error
// text as the note. It is the usual way to turn a failed read into an
// observation: Errored(id, src, err) after env.ReadFile fails.
func Errored(id string, src Source, err error) Observation {
	return missing(id, FromError(err), src, err.Error())
}

// Timestamp formats t as the snapshot timestamps are written: RFC 3339, UTC,
// whole seconds.
func Timestamp(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

var idPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_.-]*$`)

// ValidID reports whether id matches the observation id pattern.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// FieldID turns a kernel field name into an id component: "(" becomes "_",
// ")" is dropped, so "Active(anon)" becomes "Active_anon". Any other
// character outside [A-Za-z0-9_.-] becomes "_". Kernel case is kept so the
// id can be matched against documentation by eye.
func FieldID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == ')':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
