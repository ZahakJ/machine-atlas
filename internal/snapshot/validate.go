package snapshot

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/version"
)

var (
	idPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	sourceTypes    = []string{obs.SourceFile, obs.SourceCommand, obs.SourceSyscall, obs.SourceProbe}
	runStatuses    = []string{"ok", "partial", "failed", "skipped"}
	collectionMode = []string{"live", "fixture"}
)

// Validate checks every rule of schema v1 and returns one error per
// violation, each naming where it is ("observations[12] (mem.meminfo.MemTotal):
// value is null but status is ok"). It is applied to every imported file and
// every fixture. Ids missing from the catalog are allowed (a newer version
// may have collected them); ids in the catalog must carry the declared type.
func Validate(s *Snapshot) []error {
	v := &validator{}
	v.header(s)
	v.collection(&s.Collection)
	v.redaction(&s.Redaction)
	if len(s.Observations) > MaxObservations {
		v.add("observations", "%d observations exceed the limit of %d", len(s.Observations), MaxObservations)
	} else {
		seen := make(map[string]bool, len(s.Observations))
		for i := range s.Observations {
			v.observation(i, &s.Observations[i], seen)
		}
	}
	if len(v.errs) == 0 {
		// Only a structurally valid snapshot is worth encoding to measure.
		if data, err := Encode(s); err != nil {
			v.add("snapshot", "cannot be encoded: %v", err)
		} else if len(data) > MaxBytes {
			v.add("snapshot", "encoded size %d bytes exceeds %d", len(data), MaxBytes)
		} else if want := ComputeID(s); s.ID != want {
			// The id names the content (§5): a file edited after it was
			// sealed would otherwise share an identity with the original.
			v.add("id", "%s does not match the content, whose id is %s: the file was changed after it was written", s.ID, want)
		}
	}
	return v.errs
}

type validator struct{ errs []error }

func (v *validator) add(where, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, args...)))
}

func (v *validator) str(where, s string) {
	if len(s) > obs.MaxStringBytes {
		v.add(where, "string of %d bytes exceeds %d", len(s), obs.MaxStringBytes)
	}
}

func (v *validator) timestamp(where, s string) {
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		v.add(where, "%q is not an RFC 3339 timestamp", s)
	}
}

func (v *validator) oneOf(where, s string, allowed []string) {
	if !slices.Contains(allowed, s) {
		v.add(where, "%q is not one of %q", s, allowed)
	}
}

func (v *validator) header(s *Snapshot) {
	if s.Schema != Schema {
		v.add("schema", "%q is not %q", s.Schema, Schema)
	}
	if s.SchemaVersion != SchemaVersion {
		v.add("schema_version", "version %d is not supported: Machine Atlas %s reads schema version %d", s.SchemaVersion, version.Version, SchemaVersion)
	}
	if s.AppVersion == "" {
		v.add("app_version", "missing")
	}
	v.str("app_version", s.AppVersion)
	if !idPattern.MatchString(s.ID) {
		v.add("id", "%q is not sha256: followed by 64 lowercase hex digits", s.ID)
	}
	v.timestamp("created_at", s.CreatedAt)
}

func (v *validator) collection(c *Collection) {
	v.oneOf("collection.mode", c.Mode, collectionMode)
	v.str("collection.fixture", c.Fixture)
	v.timestamp("collection.started_at", c.StartedAt)
	v.timestamp("collection.finished_at", c.FinishedAt)
	for i, r := range c.Collectors {
		where := fmt.Sprintf("collection.collectors[%d]", i)
		if r.ID == "" {
			v.add(where+".id", "missing")
		}
		v.str(where+".id", r.ID)
		v.oneOf(where+".status", r.Status, runStatuses)
		if r.DurationMS < 0 || r.Observations < 0 {
			v.add(where, "negative duration or count")
		}
		v.str(where+".error", r.Error)
	}
}

func (v *validator) redaction(r *Redaction) {
	v.oneOf("redaction.policy", r.Policy, policies)
	if r.Applied != (r.Policy != PolicyNone) {
		v.add("redaction.applied", "%v contradicts policy %q", r.Applied, r.Policy)
	}
	for i, c := range r.Classes {
		if !catalog.Sensitivity(c).Valid() {
			v.add(fmt.Sprintf("redaction.classes[%d]", i), "%q is not a sensitivity class", c)
		}
	}
	if r.Replaced < 0 {
		v.add("redaction.replaced", "negative")
	}
}

func (v *validator) observation(i int, o *obs.Observation, seen map[string]bool) {
	where := fmt.Sprintf("observations[%d]", i)
	if !obs.ValidID(o.ID) {
		v.add(where+".id", "%q does not match %s", o.ID, `^[a-z][A-Za-z0-9_.-]*$`)
		return
	}
	where = fmt.Sprintf("observations[%d] (%s)", i, o.ID)
	v.str(where+".id", o.ID)
	if seen[o.ID] {
		v.add(where, "duplicate id")
	}
	seen[o.ID] = true

	if !o.Status.Valid() {
		v.add(where+".status", "%q is not a valid status", o.Status)
	}
	if !o.Kind.Valid() {
		v.add(where+".kind", "%q is not static or live", o.Kind)
	}
	if !o.Scope.Valid() {
		v.add(where+".scope", "%q is not a valid scope", o.Scope)
	}
	if !obs.ValidUnit(o.Unit) {
		v.add(where+".unit", "%q is not a valid unit", o.Unit)
	}
	// Ids the runner had to account for (a missing or timed-out reading) can
	// have no source; a value always does.
	if o.Source.Type != "" || o.Status == obs.StatusOK {
		v.oneOf(where+".source.type", o.Source.Type, sourceTypes)
	}
	v.str(where+".source.path", o.Source.Path)
	v.str(where+".source.field", o.Source.Field)
	v.str(where+".source.detail", o.Source.Detail)
	v.str(where+".collector", o.Collector)
	v.str(where+".note", o.Note)
	v.timestamp(where+".collected_at", o.CollectedAt)
	v.value(where, o)
}

func (v *validator) value(where string, o *obs.Observation) {
	if o.Status != obs.StatusOK {
		if o.Value != nil {
			v.add(where+".value", "must be null when status is %s", o.Status)
		}
		return
	}
	if o.Value == nil {
		v.add(where+".value", "is null but status is ok")
		return
	}
	val, err := obs.Normalize(o.Value)
	if err != nil {
		v.add(where+".value", "%v (values are numbers, strings, booleans, or tables of flat objects)", err)
		return
	}
	if err := obs.CheckLimits(val); err != nil {
		v.add(where+".value", "%v", err)
	}
	if e, ok := catalog.Lookup(o.ID); ok {
		if err := e.Check(val); err != nil {
			v.add(where+".value", "%v", err)
		}
	}
}
