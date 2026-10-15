package snapshot

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Invented identifying values the seeded snapshot spreads around.
var identifying = []string{
	"atlas-test-box", "dana", "user-1000", "/run/user/1000", "@1000.service",
	"192.168.31.24", "10.0.0.7", "fe80::1c2b:3aff:fe4d:5e6f", "2001:db8:85a3:0:0:8a2e:370:7334",
	"0a:1b:2c:3d:4e:5f", "0a1b2c3d4e5f", "123e4567-e89b-12d3-a456-426614174000",
	"4f1c8a2b9d3e4f5a6b7c8d9e0f1a2b3c",
}

func seeded() *Snapshot {
	k, n, p := obs.KindStatic, obs.ScopeNamespace, obs.ScopeProcess
	note := ob("mem.future.addresses", "seen 192.168.31.24 and fe80::1c2b:3aff:fe4d:5e6f via 0a:1b:2c:3d:4e:5f", obs.KindLive, n, "")
	note.Note = "also 2001:db8:85a3:0:0:8a2e:370:7334 and 10.0.0.7; config in /Users/dana/atlas"
	cg := ob("mem.cgroup.path", "/user.slice/user-1000.slice/user@1000.service/app.slice/term.scope", k, obs.ScopeCgroup, "")
	cg.Source.Path = "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/memory.max"
	observations := []obs.Observation{
		ob("os.hostname", "atlas-test-box", k, n, ""),
		ob("os.release.id", "debian", k, n, ""),
		ob("kernel.version_string", "Linux version 6.9.1-custom (dana@atlas-test-box) (gcc 14.1.0) #1 SMP PREEMPT_DYNAMIC", k, obs.ScopeKernel, ""),
		ob("cpu.model_name", "Example CPU 9000 @ 3.60GHz", k, obs.ScopeKernel, ""),
		ob("virt.dmi.product_name", "Board 123e4567-e89b-12d3-a456-426614174000", k, obs.ScopeKernel, ""),
		ob("session.type", "wayland", k, p, ""),
		cg,
		note,
		ob("mem.future.ids", "machine 4f1c8a2b9d3e4f5a6b7c8d9e0f1a2b3c booted on atlas-test-box.lan", obs.KindLive, obs.ScopeKernel, ""),
		ob("mounts.filesystems", []map[string]any{
			{"mount_point": "/", "fstype": "ext4", "source_kind": "block"},
			{"mount_point": "/home/dana", "fstype": "btrfs", "source_kind": "block"},
			{"mount_point": "/run/user/1000", "fstype": "tmpfs", "source_kind": "virtual"},
		}, k, n, ""),
		ob("net.interfaces", []map[string]any{
			{"name": "enx0a1b2c3d4e5f", "type": "ethernet", "operstate": "up"},
			{"name": "wlp3s0", "type": "wireless", "operstate": "up"},
			{"name": "lo", "type": "loopback", "operstate": "unknown"},
		}, obs.KindLive, n, ""),
	}
	rep := collect.Report{
		Mode: "live", StartedAt: testTime, FinishedAt: testTime,
		Collectors: []collect.CollectorRun{{ID: "test", Status: collect.RunOK, Observations: len(observations),
			Error: "could not read /home/dana/.config"}},
	}
	return Build(rep, observations)
}

func encoded(t *testing.T, s *Snapshot) string {
	t.Helper()
	data, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func value(t *testing.T, s *Snapshot, id string) any {
	t.Helper()
	for _, o := range s.Observations {
		if o.ID == id {
			return o.Value
		}
	}
	t.Fatalf("no %s", id)
	return nil
}

func TestRedactDefaultRemovesIdentifyingTokens(t *testing.T) {
	s := seeded()
	before := encoded(t, s)
	for _, tok := range identifying {
		if !strings.Contains(before, tok) {
			t.Fatalf("seed does not contain %q; the test would prove nothing", tok)
		}
	}
	red, changes, err := Redact(s, PolicyDefault)
	if err != nil {
		t.Fatal(err)
	}
	after := encoded(t, red)
	for _, tok := range identifying {
		if strings.Contains(after, tok) {
			t.Errorf("%q survived default redaction", tok)
		}
	}
	if encoded(t, s) != before {
		t.Error("Redact modified its input")
	}
	if errs := Validate(red); len(errs) > 0 {
		t.Errorf("redacted snapshot is invalid: %v", errs)
	}
	if red.ID == s.ID || red.ID != ComputeID(red) {
		t.Error("redacted snapshot not resealed")
	}
	if !red.Redaction.Applied || red.Redaction.Policy != PolicyDefault || red.Redaction.Replaced != len(changes) ||
		!reflect.DeepEqual(red.Redaction.Classes, []string{"address", "hostname", "serial", "username"}) {
		t.Errorf("redaction record = %+v", red.Redaction)
	}

	// What default redaction must leave alone.
	keep := map[string]any{
		"os.release.id": "debian", "session.type": "wayland", "cpu.model_name": "Example CPU 9000 @ 3.60GHz",
	}
	for id, want := range keep {
		if got := value(t, red, id); got != want {
			t.Errorf("%s = %v, want it unchanged", id, got)
		}
	}
	if v := value(t, red, "os.hostname"); v != "<redacted:hostname>" {
		t.Errorf("hostname = %v", v)
	}
	if v := value(t, red, "mem.cgroup.path"); v != "/user.slice/user-<uid>.slice/user@<uid>.service/app.slice/term.scope" {
		t.Errorf("cgroup path = %v", v)
	}
	for _, o := range red.Observations {
		if o.ID == "os.hostname" && (o.Status != obs.StatusOK || o.Note != "redacted (hostname)") {
			t.Errorf("redacted observation must stay ok with a note: %+v", o)
		}
	}

	// Redacting twice changes nothing more.
	again, more, _ := Redact(red, PolicyDefault)
	if len(more) != 0 || encoded(t, again) != after {
		t.Errorf("second redaction changed %d things: %v", len(more), more)
	}
}

func TestRedactStrict(t *testing.T) {
	red, _, err := Redact(seeded(), PolicyStrict)
	if err != nil {
		t.Fatal(err)
	}
	after := encoded(t, red)
	for _, tok := range append(identifying, "Example CPU", "wlp3s0", "Board") {
		if strings.Contains(after, tok) {
			t.Errorf("%q survived strict redaction", tok)
		}
	}
	rows := value(t, red, "net.interfaces").([]map[string]any)
	names := []any{rows[0]["name"], rows[1]["name"], rows[2]["name"]}
	if !reflect.DeepEqual(names, []any{"<if-1>", "<if-2>", "<if-3>"}) || rows[1]["type"] != "wireless" {
		t.Errorf("interfaces = %v", rows)
	}
	if red.Redaction.Policy != PolicyStrict {
		t.Errorf("policy = %q", red.Redaction.Policy)
	}
	if errs := Validate(red); len(errs) > 0 {
		t.Errorf("strict snapshot is invalid: %v", errs)
	}
}

func TestRedactNoneAndUnknownPolicy(t *testing.T) {
	s := seeded()
	red, changes, err := Redact(s, PolicyNone)
	if err != nil || len(changes) != 0 || encoded(t, red) != encoded(t, s) {
		t.Errorf("none policy changed something: %v %v", changes, err)
	}
	if _, _, err := Redact(s, "paranoid"); err == nil {
		t.Error("unknown policy accepted")
	}
}

// fields flattens the strings of a snapshot that redaction may change,
// keyed the way Change identifies them.
func fields(s *Snapshot) map[string]string {
	out := map[string]string{"/collection.fixture": s.Collection.Fixture}
	for i, c := range s.Collection.Collectors {
		out[fmt.Sprintf("/collection.collectors[%d].error", i)] = c.Error
	}
	for _, o := range s.Observations {
		k := o.ID + "/"
		out[k+"source.path"], out[k+"source.field"], out[k+"source.detail"] = o.Source.Path, o.Source.Field, o.Source.Detail
		// The "redacted (…)" mark is an annotation, not a replacement.
		note, _, _ := strings.Cut(o.Note, "redacted (")
		out[k+"note"] = strings.TrimSuffix(note, "; ")
		switch v := o.Value.(type) {
		case string:
			out[k+"value"] = v
		case []map[string]any:
			for i, row := range v {
				for col, c := range row {
					if cs, ok := c.(string); ok {
						out[fmt.Sprintf("%svalue[%d].%s", k, i, col)] = cs
					}
				}
			}
		}
	}
	return out
}

func TestPreviewEqualsAppliedChanges(t *testing.T) {
	for _, policy := range []string{PolicyDefault, PolicyStrict} {
		s := seeded()
		preview, err := Preview(s, policy)
		if err != nil {
			t.Fatal(err)
		}
		red, changes, _ := Redact(s, policy)
		if !reflect.DeepEqual(preview, changes) {
			t.Fatalf("%s: preview differs from the applied changes", policy)
		}

		orig, now := fields(s), fields(red)
		last := map[string]Change{}
		for _, c := range preview {
			k := c.ID + "/" + c.Field
			if prev, chained := last[k]; chained && prev.After != c.Before {
				t.Errorf("%s: %s: change chain broken: %q then %q", policy, k, prev.After, c.Before)
			} else if !chained && orig[k] != c.Before {
				t.Errorf("%s: %s: preview says before = %q, snapshot had %q", policy, k, c.Before, orig[k])
			}
			last[k] = c
		}
		for k, v := range now {
			c, changed := last[k]
			switch {
			case changed && c.After != v:
				t.Errorf("%s: %s: preview says %q, redacted snapshot has %q", policy, k, c.After, v)
			case !changed && orig[k] != v:
				t.Errorf("%s: %s changed from %q to %q without a preview entry", policy, k, orig[k], v)
			}
		}
	}
}

func TestReplaceWord(t *testing.T) {
	tests := []struct{ in, want string }{
		{"box", "<h>"},
		{"box.lan", "<h>.lan"},
		{"mailbox", "mailbox"},
		{"box-2", "box-2"},
		{"BOX and box", "<h> and <h>"},
		{"(dana@box)", "(dana@<h>)"},
	}
	for _, tt := range tests {
		if got := replaceWord(tt.in, "box", "<h>"); got != tt.want {
			t.Errorf("replaceWord(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPatternsSpareOrdinaryText(t *testing.T) {
	r := &redactor{}
	for _, s := range []string{
		"6.6.31-v8+", "Linux version 6.1.0-18-amd64", "12:00:00", "std::vector", "0x7ffd12345678",
		"always [madvise] never", "/sys/fs/cgroup/system.slice/atlas.service", "Thu Jan 15 12:00:00 UTC 2026",
	} {
		if got := r.patterns("x", "value", s); got != s {
			t.Errorf("%q was altered to %q", s, got)
		}
	}
}
