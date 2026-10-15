package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/collect/all"
	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/experiment"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

const (
	testPort = 7373
	testHost = "127.0.0.1:7373"
	rootsDir = "../../fixtures/roots"
)

// harness is a server over the desktop fixture whose "live machine" is
// another fixture root collected unredacted, so live-mode paths (collect,
// experiments, redaction of real-looking data) run without touching this
// computer.
type harness struct {
	*Server
	t        *testing.T
	collects atomic.Int32
	runs     atomic.Int32
}

var testWeb = fstest.MapFS{
	"index.html":     {Data: []byte("<!doctype html><title>t</title>")},
	"js/app.js":      {Data: []byte("export {};")},
	"css/base.css":   {Data: []byte("body{}")},
	"js/.secret.js":  {Data: []byte("hidden")},
	"js/blob.bin":    {Data: []byte("binary")},
	"js/sub/deep.js": {Data: []byte("export const x = 1;")},
}

func newHarness(t *testing.T, mode Mode) *harness {
	t.Helper()
	h := &harness{t: t}
	opt := Options{
		Port: testPort,
		Web:  testWeb,
		Collect: func(ctx context.Context) (*snapshot.Snapshot, error) {
			h.collects.Add(1)
			return collectRoot(t, "numa-server-la57"), nil
		},
		ListExperiments: func() []experiment.Info {
			return []experiment.Info{
				{ID: "fault-count", Title: "Fault count", Available: true},
				{ID: "elsewhere", Title: "Not here", UnavailableReason: "needs something this machine lacks"},
			}
		},
		RunExperiment: func(ctx context.Context, id string) (experiment.Result, error) {
			h.runs.Add(1)
			return experiment.Result{ID: id, Outcome: "as-expected", Measurements: []experiment.Measurement{}, Notes: []string{}}, nil
		},
	}
	switch mode {
	case ModeLive:
		opt.Mode, opt.Snapshot = ModeLive, collectRoot(t, "numa-server-la57")
	case ModeFixture:
		opt.Mode, opt.Fixture, opt.Snapshot = ModeFixture, "desktop-x86_64", fixture(t, "desktop-x86_64")
	default:
		opt.Mode, opt.Origin, opt.Snapshot = ModeSnapshot, "file", fixture(t, "desktop-x86_64")
	}
	s, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	h.Server = s
	return h
}

// collectRoot collects a fixture root without redaction, as live
// collection would.
func collectRoot(t *testing.T, name string) *snapshot.Snapshot {
	t.Helper()
	env, err := collect.OpenFixture(filepath.Join(rootsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	rep, o := collect.Run(context.Background(), env, all.All(all.Options{}))
	return snapshot.Build(rep, o)
}

func fixture(t *testing.T, name string) *snapshot.Snapshot {
	t.Helper()
	f, err := embeddedFixtures.Open("snapshots/" + name + ".json")
	if err != nil {
		t.Fatalf("%v; run \"make fixtures\"", err)
	}
	defer f.Close()
	s, err := snapshot.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// req describes one request; zero fields take safe defaults (the right
// host, the token on state-changing methods).
type req struct {
	method, path string
	body         []byte
	host         string
	token        *string
	origin       string
}

func (h *harness) do(r req) *httptest.ResponseRecorder {
	h.t.Helper()
	if r.method == "" {
		r.method = http.MethodGet
	}
	hr := httptest.NewRequest(r.method, r.path, bytes.NewReader(r.body))
	hr.Host = testHost
	if r.host != "" {
		hr.Host = r.host
	}
	if r.token != nil {
		hr.Header.Set(tokenHeader, *r.token)
	} else if r.method != http.MethodGet {
		hr.Header.Set(tokenHeader, h.Token())
	}
	if r.origin != "" {
		hr.Header.Set("Origin", r.origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, hr)
	return rec
}

func (h *harness) get(path string) *httptest.ResponseRecorder { return h.do(req{path: path}) }

func (h *harness) post(path string, body string) *httptest.ResponseRecorder {
	return h.do(req{method: http.MethodPost, path: path, body: []byte(body)})
}

func (h *harness) state() stateResponse {
	h.t.Helper()
	var st stateResponse
	decode(h.t, h.get("/api/state"), http.StatusOK, &st)
	return st
}

// decode checks the status and the JSON content type and decodes the body.
func decode(t *testing.T, rec *httptest.ResponseRecorder, status int, v any) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q", ct)
	}
	if v != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
	}
}

// wantError checks a JSON error response.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) apiError {
	t.Helper()
	var e struct{ Error apiError }
	decode(t, rec, status, &e)
	if e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("error %+v, want code %q", e.Error, code)
	}
	return e.Error
}

func TestReadEndpoints(t *testing.T) {
	h := newHarness(t, ModeFixture)
	snap := fixture(t, "desktop-x86_64")

	st := h.state()
	if st.Mode != ModeFixture || st.Fixture != "desktop-x86_64" || st.Token != h.Token() || len(st.Token) != 64 ||
		st.Snapshot.ID != snap.ID || st.Snapshot.CreatedAt != snap.CreatedAt || !st.Snapshot.Redaction.Applied ||
		st.Snapshot.Observations != len(snap.Observations) || st.SchemaVersion != 1 || st.AppVersion == "" {
		t.Errorf("state %+v", st)
	}
	if st.Capabilities != (capabilities{Live: true}) {
		t.Errorf("capabilities %+v: a fixture can switch to live but not re-collect or run experiments", st.Capabilities)
	}
	if st.Environment.ID != "derived.env.kind" || st.Environment.Basis != "inference" {
		t.Errorf("environment %+v", st.Environment)
	}
	total := 0
	for _, n := range st.Snapshot.StatusCounts {
		total += n
	}
	if total != len(snap.Observations) || len(st.Snapshot.StatusCounts) != 6 || st.Snapshot.StatusCounts["ok"] == 0 {
		t.Errorf("status counts %v", st.Snapshot.StatusCounts)
	}

	rec := h.get("/api/snapshot")
	want, _ := snapshot.Encode(snap)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("snapshot: status %d, body differs from the canonical encoding", rec.Code)
	}

	var facts []struct{ ID, Status string }
	decode(t, h.get("/api/derived"), http.StatusOK, &facts)
	if len(facts) < 30 {
		t.Errorf("%d derived facts", len(facts))
	}

	var cat catalogResponse
	decode(t, h.get("/api/catalog"), http.StatusOK, &cat)
	if len(cat.Entries) == 0 || len(cat.Families) == 0 {
		t.Errorf("catalog: %d entries, %d families", len(cat.Entries), len(cat.Families))
	}
	for _, e := range cat.Entries {
		if e.Family() {
			t.Errorf("family %s among entries", e.ID)
		}
	}

	var journey curriculum.Journey
	decode(t, h.get("/api/journey"), http.StatusOK, &journey)
	if len(journey.Graph.Nodes) == 0 {
		t.Error("journey graph is empty")
	}

	var lesson struct {
		ID       string
		Sections []json.RawMessage
	}
	decode(t, h.get("/api/lessons/cpu-addressing"), http.StatusOK, &lesson)
	if lesson.ID != "cpu-addressing" || len(lesson.Sections) == 0 {
		t.Errorf("lesson %s with %d sections", lesson.ID, len(lesson.Sections))
	}
	if again := h.get("/api/lessons/cpu-addressing"); again.Body.String() != h.get("/api/lessons/cpu-addressing").Body.String() {
		t.Error("cached lesson differs")
	}
	wantError(t, h.get("/api/lessons/no-such-lesson"), http.StatusNotFound, "not_found")

	var graph curriculum.ComponentGraph
	decode(t, h.get("/api/components"), http.StatusOK, &graph)
	if len(graph.Nodes) == 0 {
		t.Error("no components")
	}
	var roadmap curriculum.RoadmapView
	decode(t, h.get("/api/roadmap"), http.StatusOK, &roadmap)
	if len(roadmap.Subsystems) != 9 {
		t.Errorf("%d roadmap subsystems", len(roadmap.Subsystems))
	}
	var usage map[string]curriculum.Users
	decode(t, h.get("/api/usage"), http.StatusOK, &usage)
	if len(usage["kernel.arch"].Lessons)+len(usage["kernel.arch"].Components) == 0 {
		t.Errorf("usage of kernel.arch: %+v", usage["kernel.arch"])
	}
	var exps []experiment.Info
	decode(t, h.get("/api/experiments"), http.StatusOK, &exps)
	if len(exps) != 2 || exps[0].ID != "fault-count" {
		t.Errorf("experiments %+v", exps)
	}

	var fixtures []fixtureInfo
	decode(t, h.get("/api/fixtures"), http.StatusOK, &fixtures)
	found := false
	for _, f := range fixtures {
		if f.Name == "rpi-arm64-noswap" && f.Description != "" && f.Observations > 0 && f.CreatedAt != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("fixtures %+v", fixtures)
	}

	wantError(t, h.get("/api/nothing-here"), http.StatusNotFound, "not_found")
}

func TestEveryAPIResponseIsUncachedAndHasNoCORS(t *testing.T) {
	h := newHarness(t, ModeFixture)
	for _, p := range []string{"/api/state", "/api/snapshot", "/api/derived", "/api/lessons/nope", "/api/nothing"} {
		rec := h.get(p)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q", p, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != csp {
			t.Errorf("%s: CSP %q", p, got)
		}
		for k := range rec.Header() {
			if strings.HasPrefix(k, "Access-Control-") {
				t.Errorf("%s: CORS header %s", p, k)
			}
		}
	}
}

func TestActivateFixtureAndReturnLive(t *testing.T) {
	h := newHarness(t, ModeLive)
	var st stateResponse
	decode(t, h.post("/api/fixtures/rpi-arm64-noswap/activate", ""), http.StatusOK, &st)
	if st.Mode != ModeFixture || st.Fixture != "rpi-arm64-noswap" || st.Capabilities.Collect {
		t.Errorf("after activate: %+v", st)
	}
	wantError(t, h.post("/api/fixtures/nope/activate", ""), http.StatusNotFound, "not_found")
	wantError(t, h.post("/api/fixtures/..%2f..%2fetc/activate", ""), http.StatusNotFound, "not_found")
	wantError(t, h.post("/api/collect", ""), http.StatusConflict, "not_live")

	decode(t, h.post("/api/snapshot/live", ""), http.StatusOK, &st)
	if st.Mode != ModeLive || !st.Capabilities.Collect || !st.Capabilities.Experiments || st.Snapshot.Redaction.Applied {
		t.Errorf("after live: %+v", st)
	}
	before := h.collects.Load()
	decode(t, h.post("/api/collect", "{}"), http.StatusOK, &st)
	if h.collects.Load() != before+1 || st.Mode != ModeLive {
		t.Errorf("collect did not collect: %+v", st)
	}
}

func TestLiveUnavailable(t *testing.T) {
	h := newHarness(t, ModeFixture)
	h.opt.Collect = nil
	wantError(t, h.post("/api/snapshot/live", ""), http.StatusConflict, "unavailable")
	if st := h.state(); st.Capabilities != (capabilities{}) {
		t.Errorf("capabilities %+v without a collector", st.Capabilities)
	}
}

func TestExperiments(t *testing.T) {
	ack := `{"acknowledged": true}`

	h := newHarness(t, ModeSnapshot)
	wantError(t, h.post("/api/experiments/fault-count/run", ack), http.StatusConflict, "not_live")
	h = newHarness(t, ModeFixture)
	wantError(t, h.post("/api/experiments/fault-count/run", ack), http.StatusConflict, "not_live")

	h = newHarness(t, ModeLive)
	wantError(t, h.post("/api/experiments/fault-count/run", ""), http.StatusBadRequest, "not_acknowledged")
	wantError(t, h.post("/api/experiments/fault-count/run", `{"acknowledged": false}`), http.StatusBadRequest, "not_acknowledged")
	wantError(t, h.post("/api/experiments/fault-count/run", `{"acknowledged": true, "argv": ["rm"]}`), http.StatusBadRequest, "bad_request")
	wantError(t, h.post("/api/experiments/rm%20-rf/run", ack), http.StatusNotFound, "not_found")
	wantError(t, h.post("/api/experiments/elsewhere/run", ack), http.StatusConflict, "unavailable")

	if err := h.begin("a collection"); err != nil {
		t.Fatal(err)
	}
	wantError(t, h.post("/api/experiments/fault-count/run", ack), http.StatusConflict, "busy")
	wantError(t, h.post("/api/collect", ""), http.StatusConflict, "busy")
	wantError(t, h.post("/api/fixtures/restricted/activate", ""), http.StatusConflict, "busy")
	h.end()
	if h.runs.Load() != 0 {
		t.Fatalf("%d experiments ran while refused", h.runs.Load())
	}

	var res experiment.Result
	decode(t, h.post("/api/experiments/fault-count/run", ack), http.StatusOK, &res)
	if res.ID != "fault-count" || h.runs.Load() != 1 {
		t.Errorf("result %+v after %d runs", res, h.runs.Load())
	}
}

func TestPreviewAndExportAgreeWithRedact(t *testing.T) {
	h := newHarness(t, ModeLive)
	active := h.current().snap
	for _, policy := range []string{snapshot.PolicyDefault, snapshot.PolicyStrict, snapshot.PolicyNone} {
		wantChanges, err := snapshot.Preview(active, policy)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Policy  string
			Changes []snapshot.Change
		}
		decode(t, h.post("/api/snapshot/preview", `{"policy":"`+policy+`"}`), http.StatusOK, &got)
		if len(got.Changes) != len(wantChanges) {
			t.Fatalf("%s: preview has %d changes, Redact %d", policy, len(got.Changes), len(wantChanges))
		}
		for i := range got.Changes {
			if got.Changes[i] != wantChanges[i] {
				t.Errorf("%s: change %d: %+v != %+v", policy, i, got.Changes[i], wantChanges[i])
			}
		}
		if policy == snapshot.PolicyDefault && len(got.Changes) == 0 {
			t.Error("default preview of an unredacted collection changes nothing")
		}

		red, _, _ := snapshot.Redact(active, policy)
		want, _ := snapshot.Encode(red)
		rec := h.get("/api/snapshot/export?policy=" + policy)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Errorf("%s: export differs from snapshot.Redact (status %d)", policy, rec.Code)
		}
		wantName := `attachment; filename="machine-atlas-` + strings.TrimPrefix(red.ID, "sha256:")[:12] + `.json"`
		if got := rec.Header().Get("Content-Disposition"); got != wantName {
			t.Errorf("%s: Content-Disposition %q, want %q", policy, got, wantName)
		}
	}
	// The default policy applies when none is named.
	red, _, _ := snapshot.Redact(active, snapshot.PolicyDefault)
	want, _ := snapshot.Encode(red)
	if rec := h.get("/api/snapshot/export"); !bytes.Equal(rec.Body.Bytes(), want) {
		t.Error("export without a policy is not default-redacted")
	}
	if !strings.Contains(string(want), "<redacted:hostname>") {
		t.Error("the test snapshot has no hostname to redact")
	}
	wantError(t, h.get("/api/snapshot/export?policy=lenient"), http.StatusBadRequest, "bad_policy")
	wantError(t, h.post("/api/snapshot/preview", `{"policy":"lenient"}`), http.StatusBadRequest, "bad_policy")
}

func TestStaticFiles(t *testing.T) {
	h := newHarness(t, ModeFixture)
	for path, ctype := range map[string]string{
		"/":               "text/html; charset=utf-8",
		"/index.html":     "text/html; charset=utf-8",
		"/js/app.js":      "text/javascript; charset=utf-8",
		"/js/sub/deep.js": "text/javascript; charset=utf-8",
		"/css/base.css":   "text/css; charset=utf-8",
	} {
		rec := h.get(path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ctype {
			t.Errorf("%s: status %d, type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Content-Security-Policy") != csp || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: security headers missing: %v", path, rec.Header())
		}
	}
	// Directories are never listed; hidden and unknown-type files are not
	// served; nothing outside the file system is reachable.
	for _, path := range []string{"/js", "/js/", "/js/sub/", "/js/.secret.js", "/js/blob.bin", "/nope.js",
		"/js/../../server.go", "/%2e%2e/%2e%2e/go.mod", "/..%2f..%2fgo.mod", "/js/..%5c..%5cgo.mod"} {
		rec := h.get(path)
		if rec.Code == http.StatusOK {
			t.Errorf("%s: served %q", path, rec.Body)
		}
		if rec.Header().Get("Content-Security-Policy") != csp {
			t.Errorf("%s: no CSP on a refusal", path)
		}
	}
	rec := h.do(req{method: http.MethodPost, path: "/index.html"})
	wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestEmbeddedWeb(t *testing.T) {
	t.Setenv("ATLAS_WEB_DIR", "")
	web, err := WebFS()
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, ModeFixture)
	h.opt.Web = web
	rec := h.get("/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("embedded index.html: %d", rec.Code)
	}
}

func TestWebDirStaysInside(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.js"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>dev</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.js"), filepath.Join(dir, "link.js")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATLAS_WEB_DIR", dir)
	web, err := WebFS()
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, ModeFixture)
	h.opt.Web = web
	if rec := h.get("/"); rec.Body.String() != "<p>dev</p>" {
		t.Errorf("ATLAS_WEB_DIR index: %q", rec.Body)
	}
	if rec := h.get("/link.js"); rec.Code == http.StatusOK {
		t.Errorf("a symbolic link out of ATLAS_WEB_DIR was followed: %q", rec.Body)
	}
}

func TestHeadAndMethods(t *testing.T) {
	h := newHarness(t, ModeFixture)
	rec := h.do(req{method: http.MethodHead, path: "/api/state"})
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD /api/state: %d", rec.Code)
	}
	for _, r := range []req{
		{method: http.MethodDelete, path: "/api/state"},
		{method: http.MethodPut, path: "/api/snapshot"},
		{method: http.MethodGet, path: "/api/collect"},
		{method: http.MethodGet, path: "/api/snapshot/import"},
		{method: http.MethodPost, path: "/api/snapshot/export"},
		{method: http.MethodGet, path: "/api/experiments/fault-count/run"},
	} {
		rec := h.do(r)
		wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		if rec.Header().Get("Allow") == "" {
			t.Errorf("%s %s: no Allow header", r.method, r.path)
		}
	}
}

func TestResponseBodiesAreComplete(t *testing.T) {
	// A regression guard for handlers that write raw bytes: each response
	// must be one complete JSON document.
	h := newHarness(t, ModeFixture)
	for _, p := range []string{"/api/snapshot", "/api/lessons/cpu-addressing"} {
		dec := json.NewDecoder(h.get(p).Body)
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		if _, err := dec.Token(); err != io.EOF {
			t.Errorf("%s: trailing data", p)
		}
	}
}
