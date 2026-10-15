package snapshot

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

var testTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func ob(id string, value any, kind obs.Kind, scope obs.Scope, unit string) obs.Observation {
	o := obs.OK(id, value, obs.File("/proc/test", ""))
	o.Kind, o.Scope, o.Unit = kind, scope, unit
	o.CollectedAt, o.Collector = obs.Timestamp(testTime), "test"
	return o
}

// sample returns a small valid snapshot: one int, one string, one table, one
// denied reading and one id from a future catalog.
func sample() *Snapshot {
	denied := obs.Denied("mem.slabinfo.cache_count", obs.File("/proc/slabinfo", ""), "open /proc/slabinfo: permission denied")
	denied.Kind, denied.Scope, denied.CollectedAt, denied.Collector = obs.KindLive, obs.ScopeKernel, obs.Timestamp(testTime), "test"
	observations := []obs.Observation{
		ob("mem.meminfo.MemTotal", int64(16<<30), obs.KindLive, obs.ScopeKernel, obs.UnitBytes),
		ob("mem.thp.enabled", "madvise", obs.KindStatic, obs.ScopeKernel, ""),
		ob("mem.psi.some.avg10", 0.25, obs.KindLive, obs.ScopeKernel, obs.UnitPercent),
		ob("net.interfaces", []map[string]any{{"name": "lo", "type": "loopback", "operstate": "unknown"}}, obs.KindLive, obs.ScopeNamespace, ""),
		ob("mem.future.counter", int64(7), obs.KindLive, obs.ScopeKernel, obs.UnitCount),
		denied,
	}
	rep := collect.Report{
		Mode: "fixture", Fixture: "desktop-x86_64", StartedAt: testTime, FinishedAt: testTime.Add(time.Second),
		Collectors: []collect.CollectorRun{{ID: "test", Status: collect.RunPartial, Duration: 3 * time.Millisecond, Observations: len(observations)}},
	}
	return Build(rep, observations)
}

func TestBuild(t *testing.T) {
	s := sample()
	if errs := Validate(s); len(errs) > 0 {
		t.Fatalf("sample does not validate: %v", errs)
	}
	if s.Observations[0].ID != "mem.future.counter" {
		t.Errorf("observations not sorted: first is %s", s.Observations[0].ID)
	}
	if s.CreatedAt != "2026-01-15T12:00:01Z" || s.Collection.Collectors[0].DurationMS != 3 || s.Redaction.Policy != PolicyNone {
		t.Errorf("Build mapped the report wrongly: %+v", s)
	}
}

func TestCanonicalRoundTrip(t *testing.T) {
	s := sample()
	first, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(first, []byte("}\n")) || !bytes.HasPrefix(first, []byte("{\n  \"schema\": ")) {
		t.Errorf("not the canonical layout:\n%s", first)
	}
	decoded, err := Load(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("re-encoding changed the bytes:\n%s\n---\n%s", first, second)
	}
	if ComputeID(decoded) != s.ID {
		t.Errorf("id changed across a round trip: %s vs %s", ComputeID(decoded), s.ID)
	}

	decoded.Collection.Collectors[0].DurationMS = 999
	if ComputeID(decoded) != s.ID {
		t.Error("durations must not affect the id")
	}
	decoded.Observations[1].Value = int64(1)
	if ComputeID(decoded) == s.ID {
		t.Error("a changed value must change the id")
	}
}

func TestEncodeKeepsPlaceholdersReadable(t *testing.T) {
	s := sample()
	s.Observations[1].Value = "<redacted:hostname>"
	data, _ := Encode(s)
	if !bytes.Contains(data, []byte("<redacted:hostname>")) {
		t.Error("placeholders are HTML-escaped")
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(s *Snapshot)
		want   string
	}{
		{"wrong schema", func(s *Snapshot) { s.Schema = "other/snapshot" }, "schema"},
		{"newer schema version", func(s *Snapshot) { s.SchemaVersion = 2 }, "version 2 is not supported"},
		{"malformed id", func(s *Snapshot) { s.ID = "md5:abc" }, "id:"},
		{"bad created_at", func(s *Snapshot) { s.CreatedAt = "yesterday" }, "created_at"},
		{"bad mode", func(s *Snapshot) { s.Collection.Mode = "remote" }, "collection.mode"},
		{"bad collector status", func(s *Snapshot) { s.Collection.Collectors[0].Status = "great" }, "collectors[0].status"},
		{"bad policy", func(s *Snapshot) { s.Redaction.Policy = "maximum" }, "redaction.policy"},
		{"applied contradicts policy", func(s *Snapshot) { s.Redaction.Applied = true }, "redaction.applied"},
		{"bad class", func(s *Snapshot) { s.Redaction.Classes = []string{"secret"} }, "redaction.classes[0]"},
		{"bad id pattern", func(s *Snapshot) { s.Observations[0].ID = "Mem.x" }, "does not match"},
		{"duplicate id", func(s *Snapshot) { s.Observations[1].ID = s.Observations[0].ID }, "duplicate id"},
		{"bad status", func(s *Snapshot) { s.Observations[0].Status = "weird" }, ".status"},
		{"bad kind", func(s *Snapshot) { s.Observations[0].Kind = "dynamic" }, ".kind"},
		{"bad scope", func(s *Snapshot) { s.Observations[0].Scope = "global" }, ".scope"},
		{"bad unit", func(s *Snapshot) { s.Observations[0].Unit = "kB" }, ".unit"},
		{"bad source type", func(s *Snapshot) { s.Observations[0].Source.Type = "magic" }, ".source.type"},
		{"bad collected_at", func(s *Snapshot) { s.Observations[0].CollectedAt = "now" }, ".collected_at"},
		{"value with non-ok status", func(s *Snapshot) { s.Observations[3].Value = int64(3) }, "must be null"},
		{"null value with ok status", func(s *Snapshot) { s.Observations[0].Value = nil }, "is null but status is ok"},
		{"catalog type mismatch", func(s *Snapshot) { s.Observations[1].Value = "16 GiB" }, "catalog declares int"},
		{"undeclared table column", func(s *Snapshot) {
			s.Observations[5].Value = []map[string]any{{"name": "lo", "address": "x"}}
		}, "does not declare"},
		{"long string", func(s *Snapshot) { s.Observations[0].Note = strings.Repeat("x", obs.MaxStringBytes+1) }, ".note"},
		{"long string value", func(s *Snapshot) { s.Observations[4].Value = strings.Repeat("x", obs.MaxStringBytes+1) }, "exceeds 4096"},
		{"too many rows", func(s *Snapshot) {
			rows := make([]map[string]any, obs.MaxTableRows+1)
			for i := range rows {
				rows[i] = map[string]any{"name": "lo"}
			}
			s.Observations[5].Value = rows
		}, "rows exceeds"},
		{"too many columns", func(s *Snapshot) {
			row := map[string]any{}
			for i := range obs.MaxTableColumns + 1 {
				row[strings.Repeat("c", i+1)] = int64(i)
			}
			s.Observations[0].Value = []map[string]any{row}
		}, "columns"},
		{"nested cell", func(s *Snapshot) {
			s.Observations[5].Value = []any{map[string]any{"name": map[string]any{"x": "y"}}}
		}, "unsupported value type"},
		{"edited after sealing", func(s *Snapshot) { s.Observations[1].Value = int64(1 << 20) }, "does not match the content"},
		{"made-up id", func(s *Snapshot) { s.ID = "sha256:" + strings.Repeat("0", 64) }, "does not match the content"},
		{"too many observations", func(s *Snapshot) {
			s.Observations = make([]obs.Observation, MaxObservations+1)
		}, "exceed the limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sample()
			tt.mutate(s)
			errs := Validate(s)
			if len(errs) == 0 {
				t.Fatalf("accepted")
			}
			if !strings.Contains(errors.Join(errs...).Error(), tt.want) {
				t.Errorf("errors %v do not mention %q", errs, tt.want)
			}
		})
	}
}

func TestDecodeRejects(t *testing.T) {
	good, _ := Encode(sample())
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"unknown top-level field", bytes.Replace(good, []byte(`"schema": `), []byte(`"extra": 1, "schema": `), 1), "unknown field"},
		{"unknown observation field", bytes.Replace(good, []byte(`"id": "mem.future.counter"`), []byte(`"id": "mem.future.counter", "html": "<b>"`), 1), "unknown field"},
		{"trailing data", append(bytes.Clone(good), []byte(`{}`)...), "after the snapshot"},
		{"not json", []byte("schema: 1"), "decode snapshot"},
		{"oversized", append(bytes.Clone(good), bytes.Repeat([]byte(" "), MaxBytes)...), "exceeds"},
	}
	for _, tt := range tests {
		_, err := Decode(bytes.NewReader(tt.data))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want mention of %q", tt.name, err, tt.want)
		}
	}
	if _, err := Load(bytes.NewReader(bytes.Replace(good, []byte(`"schema_version": 1`), []byte(`"schema_version": 3`), 1))); err == nil ||
		!strings.Contains(err.Error(), "version 3 is not supported") {
		t.Errorf("Load of a future schema: %v", err)
	}
}
