package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// generic returns the rpi fixture as generic JSON, for tests to damage.
func generic(t *testing.T) map[string]any {
	t.Helper()
	data, err := snapshot.Encode(fixture(t, "rpi-arm64-noswap"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sealed sets the id of an edited generic snapshot to that of its new
// content, as atlas would when writing it; an edit without it is rejected
// as a changed file.
func sealed(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	s, err := snapshot.Decode(strings.NewReader(marshal(t, m)))
	if err != nil {
		t.Fatal(err)
	}
	m["id"] = snapshot.ComputeID(s)
	return m
}

// observation returns the generic observation with id.
func observation(t *testing.T, m map[string]any, id string) map[string]any {
	t.Helper()
	for _, o := range m["observations"].([]any) {
		if o := o.(map[string]any); o["id"] == id {
			return o
		}
	}
	t.Fatalf("no observation %s", id)
	return nil
}

func TestImportHostileSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		body   func(t *testing.T) string
		status int
		code   string
		detail string // a substring one of the details must contain
	}{
		{"wrong schema version", func(t *testing.T) string {
			m := generic(t)
			m["schema_version"] = 2
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "schema_version: version 2 is not supported"},
		{"wrong schema", func(t *testing.T) string {
			m := generic(t)
			m["schema"] = "something-else"
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "schema"},
		{"9 MiB body", func(t *testing.T) string {
			return `{"schema":"` + strings.Repeat("x", 9<<20) + `"}`
		}, http.StatusRequestEntityTooLarge, "body_too_large", ""},
		{"unknown top-level field", func(t *testing.T) string {
			m := generic(t)
			m["run"] = "rm -rf /"
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "unknown field"},
		{"unknown observation field", func(t *testing.T) string {
			m := generic(t)
			observation(t, m, "kernel.arch")["html"] = "<b>"
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "unknown field"},
		{"deep nesting", func(t *testing.T) string {
			m := generic(t)
			observation(t, m, "kernel.arch")["value"] = "NEST"
			return strings.Replace(marshal(t, m), `"NEST"`, strings.Repeat("[", 100000)+strings.Repeat("]", 100000), 1)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", ""},
		{"nested table cell", func(t *testing.T) string {
			m := generic(t)
			observation(t, m, "mem.zones")["value"] = []any{map[string]any{"node": map[string]any{"a": 1}}}
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "mem.zones"},
		{"duplicate ids", func(t *testing.T) string {
			m := generic(t)
			obs := m["observations"].([]any)
			m["observations"] = append(obs, observation(t, m, "kernel.arch"))
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "duplicate id"},
		{"table of 5000 rows", func(t *testing.T) string {
			m := generic(t)
			rows := make([]any, 5000)
			for i := range rows {
				rows[i] = map[string]any{"node": 0, "zone": "Normal", "free": i}
			}
			observation(t, m, "mem.zones")["value"] = rows
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "mem.zones"},
		{"wrong type for a catalogued id", func(t *testing.T) string {
			m := generic(t)
			observation(t, m, "mem.meminfo.MemTotal")["value"] = "a lot"
			observation(t, m, "kernel.arch")["value"] = 64
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "mem.meminfo.MemTotal"},
		{"value on a failed observation", func(t *testing.T) string {
			m := generic(t)
			o := observation(t, m, "kernel.arch")
			o["status"] = "failed"
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "must be null"},
		{"bad id", func(t *testing.T) string {
			m := generic(t)
			observation(t, m, "kernel.arch")["id"] = "../../etc/passwd"
			return marshal(t, m)
		}, http.StatusUnprocessableEntity, "invalid_snapshot", "does not match"},
		{"not JSON", func(t *testing.T) string { return "<script>alert(1)</script>" },
			http.StatusUnprocessableEntity, "invalid_snapshot", ""},
		{"trailing data", func(t *testing.T) string { return marshal(t, generic(t)) + "{}" },
			http.StatusUnprocessableEntity, "invalid_snapshot", "after the snapshot"},
		{"empty body", func(t *testing.T) string { return "" },
			http.StatusUnprocessableEntity, "invalid_snapshot", ""},
	}
	h := newHarness(t, ModeFixture)
	before := h.state().Snapshot.ID
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := wantError(t, h.post("/api/snapshot/import", tt.body(t)), tt.status, tt.code)
			if tt.detail != "" && !strings.Contains(strings.Join(e.Details, "\n"), tt.detail) {
				t.Errorf("details %q do not mention %q", e.Details, tt.detail)
			}
			if tt.code == "invalid_snapshot" && len(e.Details) == 0 {
				t.Error("no details")
			}
		})
	}
	if st := h.state(); st.Mode != ModeFixture || st.Snapshot.ID != before {
		t.Errorf("a rejected import changed the active snapshot: %+v", st)
	}
}

// Every validation problem is reported, not only the first.
func TestImportListsEveryProblem(t *testing.T) {
	h := newHarness(t, ModeFixture)
	m := generic(t)
	m["schema_version"] = 7
	observation(t, m, "mem.meminfo.MemTotal")["value"] = "a lot"
	observation(t, m, "kernel.arch")["status"] = "mysterious"
	e := wantError(t, h.post("/api/snapshot/import", marshal(t, m)), http.StatusUnprocessableEntity, "invalid_snapshot")
	if len(e.Details) < 3 {
		t.Errorf("details %q, want all three problems", e.Details)
	}
}

// A file changed after it was written keeps its old id, which no longer
// names its content; it is rejected rather than shown under that identity.
func TestImportRejectsIDNotMatchingContent(t *testing.T) {
	h := newHarness(t, ModeFixture)
	m := generic(t)
	observation(t, m, "mem.meminfo.MemTotal")["value"] = 1 << 30
	e := wantError(t, h.post("/api/snapshot/import", marshal(t, m)), http.StatusUnprocessableEntity, "invalid_snapshot")
	if len(e.Details) != 1 || !strings.Contains(e.Details[0], "does not match the content") {
		t.Errorf("details %q", e.Details)
	}
	m["id"] = "sha256:" + strings.Repeat("0", 64)
	wantError(t, h.post("/api/snapshot/import", marshal(t, m)), http.StatusUnprocessableEntity, "invalid_snapshot")
	decode(t, h.post("/api/snapshot/import", marshal(t, sealed(t, m))), http.StatusOK, nil)
}

func TestImportValidSwitchesMode(t *testing.T) {
	h := newHarness(t, ModeLive)
	want := fixture(t, "rpi-arm64-noswap")
	body, _ := snapshot.Encode(want)
	var st stateResponse
	decode(t, h.post("/api/snapshot/import", string(body)), http.StatusOK, &st)
	if st.Mode != ModeSnapshot || st.Origin != "import" || st.Snapshot.ID != want.ID ||
		st.Capabilities.Collect || st.Capabilities.Experiments || !st.Capabilities.Live {
		t.Errorf("after import: %+v", st)
	}
	wantError(t, h.post("/api/experiments/fault-count/run", `{"acknowledged":true}`), http.StatusConflict, "not_live")
}

// Markup in an imported snapshot is data: it is accepted (it breaks no
// rule) and comes back as the same plain JSON string, never as markup.
func TestImportScriptTagsStayStrings(t *testing.T) {
	const evil = `<script>alert("x")</script><img src=x onerror=alert(1)>`
	h := newHarness(t, ModeFixture)
	m := generic(t)
	observation(t, m, "os.release.name")["value"] = evil
	observation(t, m, "kernel.arch")["note"] = evil
	decode(t, h.post("/api/snapshot/import", marshal(t, sealed(t, m))), http.StatusOK, nil)

	rec := h.get("/api/snapshot")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("served as %q", ct)
	}
	var got snapshot.Snapshot
	decode(t, rec, http.StatusOK, &got)
	for _, o := range got.Observations {
		if o.ID == "os.release.name" && o.Value != evil {
			t.Errorf("value came back as %#v", o.Value)
		}
		if o.ID == "kernel.arch" && o.Note != evil {
			t.Errorf("note came back as %#v", o.Note)
		}
	}
	// Bound lessons and the component graph carry such strings as JSON
	// strings too; the browser renders them with textContent.
	if rec := h.get("/api/lessons/cpu-addressing"); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<script") {
		t.Errorf("lesson: %d, raw markup present", rec.Code)
	}
}
