package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/experiment"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
	"github.com/ZahakJ/machine-atlas/internal/version"
)

// routes maps every path of ARCHITECTURE.md §10. Patterns carry no method, so a
// wrong method reaches the handler and gets a JSON 405 rather than the
// mux's plain-text one; unknown /api/ paths get a JSON 404.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	get := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if allowMethods(w, r, http.MethodGet, http.MethodHead) {
				h(w, r)
			}
		})
	}
	post := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if allowMethods(w, r, http.MethodPost) {
				h(w, r)
			}
		})
	}
	get("/api/state", s.handleState)
	get("/api/snapshot", s.handleSnapshot)
	get("/api/derived", s.handleDerived)
	get("/api/catalog", s.handleCatalog)
	get("/api/journey", s.handleJourney)
	get("/api/lessons/{id}", s.handleLesson)
	get("/api/components", s.handleComponents)
	get("/api/roadmap", s.handleRoadmap)
	get("/api/usage", s.handleUsage)
	get("/api/experiments", s.handleExperiments)
	post("/api/experiments/{id}/run", s.handleRunExperiment)
	post("/api/collect", s.handleCollect)
	post("/api/snapshot/import", s.handleImport)
	post("/api/snapshot/preview", s.handlePreview)
	get("/api/snapshot/export", s.handleExport)
	post("/api/snapshot/live", s.handleLive)
	get("/api/fixtures", s.handleFixtures)
	post("/api/fixtures/{name}/activate", s.handleActivateFixture)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
	})
	mux.HandleFunc("/", s.static)
	return mux
}

// --- responses ---------------------------------------------------------

// apiError is the documented error shape, {"error": {"code", "message"}}.
// Details lists every problem when there are several, as for a rejected
// import.
type apiError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, details ...string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message, Details: details}})
}

// writeJSON encodes v. HTML-significant characters are escaped (<),
// which changes nothing for a JSON reader but keeps markup out of the bytes
// in case anything ever treats them as HTML.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"error":{"code":"internal","message":"cannot encode the response"}}`)
	}
	writeRaw(w, status, append(data, '\n'))
}

func writeRaw(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}

// allowMethods answers 405 unless r uses one of methods.
func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
		fmt.Sprintf("%s is not allowed here; use %s", r.Method, strings.Join(methods, " or ")))
	return false
}

// readBody decodes an optional JSON object body into v. An empty body
// leaves v as it is; unknown fields are refused. It writes the error
// response itself and reports whether to continue.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, extra := dec.Token(); extra != io.EOF {
			err = errors.New("unexpected data after the JSON object")
		}
	}
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil || errors.Is(err, io.EOF):
		return true
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
			fmt.Sprintf("the request body exceeds %d bytes", tooLarge.Limit))
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "malformed request body: "+err.Error())
	}
	return false
}

// --- state -------------------------------------------------------------

type stateResponse struct {
	AppVersion        string       `json:"app_version"`
	CurriculumVersion string       `json:"curriculum_version"`
	SchemaVersion     int          `json:"schema_version"`
	Mode              Mode         `json:"mode"`
	Fixture           string       `json:"fixture"` // the fixture name in fixture mode
	Origin            string       `json:"origin"`  // collection | file | import | fixture
	Snapshot          snapshotMeta `json:"snapshot"`
	Environment       derive.Fact  `json:"environment"` // derived.env.kind, an inference
	Token             string       `json:"token"`
	Capabilities      capabilities `json:"capabilities"`
	Busy              string       `json:"busy"` // what is in progress, or ""
}

type snapshotMeta struct {
	ID           string             `json:"id"`
	CreatedAt    string             `json:"created_at"`
	AppVersion   string             `json:"app_version"`
	Collection   collectionMeta     `json:"collection"`
	Redaction    snapshot.Redaction `json:"redaction"`
	Observations int                `json:"observations"`
	StatusCounts map[obs.Status]int `json:"status_counts"`
}

type collectionMeta struct {
	Mode       string `json:"mode"`
	Fixture    string `json:"fixture"`
	Privileged bool   `json:"privileged"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// capabilities say which actions make sense now. Live: this build can
// collect this machine (POST /api/snapshot/live). Collect: re-collection is
// possible now (live mode). Experiments: experiments can run now (live
// mode and at least one available on this platform).
type capabilities struct {
	Live        bool `json:"live"`
	Collect     bool `json:"collect"`
	Experiments bool `json:"experiments"`
}

func (s *Server) state() stateResponse {
	s.mu.Lock()
	a, busy := s.active, s.busy
	s.mu.Unlock()
	live := s.opt.Collect != nil
	snap := a.snap
	return stateResponse{
		AppVersion:        version.Version,
		CurriculumVersion: s.cur.RoadmapData.CurriculumVersion,
		SchemaVersion:     snapshot.SchemaVersion,
		Mode:              a.mode,
		Fixture:           a.fixture,
		Origin:            a.origin,
		Snapshot: snapshotMeta{
			ID: snap.ID, CreatedAt: snap.CreatedAt, AppVersion: snap.AppVersion,
			Collection: collectionMeta{
				Mode: snap.Collection.Mode, Fixture: snap.Collection.Fixture, Privileged: snap.Collection.Privileged,
				StartedAt: snap.Collection.StartedAt, FinishedAt: snap.Collection.FinishedAt,
			},
			Redaction:    snap.Redaction,
			Observations: len(snap.Observations),
			StatusCounts: a.counts,
		},
		Environment: a.fact("derived.env.kind"),
		Token:       s.token,
		Capabilities: capabilities{
			Live:        live,
			Collect:     live && a.mode == ModeLive,
			Experiments: live && a.mode == ModeLive && s.anyExperimentAvailable(),
		},
		Busy: busy,
	}
}

// fact returns the derived fact id, or an unknown fact saying it is
// missing (an older app version cannot compute it).
func (a *activation) fact(id string) derive.Fact {
	for _, f := range a.facts {
		if f.ID == id {
			return f
		}
	}
	return derive.Fact{ID: id, Status: derive.StatusUnknown, Reason: "not computed by this version", Inputs: []string{}}
}

func (s *Server) anyExperimentAvailable() bool {
	return slices.ContainsFunc(s.opt.ListExperiments(), func(i experiment.Info) bool { return i.Available })
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

// --- read-only views of the active snapshot ----------------------------

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	writeRaw(w, http.StatusOK, s.current().encoded)
}

func (s *Server) handleDerived(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, nonNil(s.current().facts))
}

// catalogResponse holds the entries for the ids in the active snapshot
// (observations and derived facts), every wildcard family, and the ids
// this version's catalog does not know.
type catalogResponse struct {
	Entries  []catalog.Entry `json:"entries"`
	Families []catalog.Entry `json:"families"`
	Unknown  []string        `json:"unknown"`
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	a := s.current()
	ids := make([]string, 0, len(a.snap.Observations)+len(a.facts))
	for _, o := range a.snap.Observations {
		ids = append(ids, o.ID)
	}
	for _, f := range a.facts {
		ids = append(ids, f.ID)
	}
	resp := catalogResponse{Entries: []catalog.Entry{}, Families: nonNil(catalog.Families()), Unknown: []string{}}
	seen := map[string]bool{}
	for _, id := range ids {
		e, ok := catalog.Lookup(id)
		switch {
		case !ok:
			resp.Unknown = append(resp.Unknown, id)
		case !e.Family() && !seen[e.ID]:
			seen[e.ID] = true
			resp.Entries = append(resp.Entries, e)
		}
	}
	slices.SortFunc(resp.Entries, func(a, b catalog.Entry) int { return strings.Compare(a.ID, b.ID) })
	slices.Sort(resp.Unknown)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleJourney(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.current().journey)
}

func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.current().components)
}

func (s *Server) handleRoadmap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.current().roadmap)
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.usage)
}

// handleLesson binds a lesson to the active snapshot on first request and
// serves the cached bytes afterwards.
func (s *Server) handleLesson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lesson, ok := s.cur.Lesson(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no lesson %q", id))
		return
	}
	a := s.current()
	a.lessonsMu.Lock()
	data, ok := a.lessons[id]
	if !ok {
		var err error
		if data, err = json.Marshal(curriculum.Bind(lesson, a.snap, a.facts)); err == nil {
			data = append(data, '\n')
			a.lessons[id] = data
		}
	}
	a.lessonsMu.Unlock()
	if data == nil {
		writeError(w, http.StatusInternalServerError, "internal", "cannot encode the lesson")
		return
	}
	writeRaw(w, http.StatusOK, data)
}

// --- experiments -------------------------------------------------------

func (s *Server) handleExperiments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, nonNil(s.opt.ListExperiments()))
}

// handleRunExperiment runs one experiment, refusing unless the request
// acknowledges the resource statement, the active snapshot is this
// machine's live collection, and nothing else is running.
func (s *Server) handleRunExperiment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	list := s.opt.ListExperiments()
	i := slices.IndexFunc(list, func(i experiment.Info) bool { return i.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no experiment %q", id))
		return
	}
	info := list[i]
	var body struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if !body.Acknowledged {
		writeError(w, http.StatusBadRequest, "not_acknowledged",
			`an experiment runs only with {"acknowledged": true}, after its resource use has been shown to the user`)
		return
	}
	if !info.Available {
		writeError(w, http.StatusConflict, "unavailable", info.UnavailableReason)
		return
	}
	if err := s.begin("an experiment"); err != nil {
		writeError(w, http.StatusConflict, "busy", err.Error())
		return
	}
	defer s.end()
	// Checked after begin: nothing can be activated while an experiment
	// runs, so the mode cannot change underneath it.
	if s.current().mode != ModeLive || s.opt.Collect == nil {
		writeError(w, http.StatusConflict, "not_live",
			"experiments measure this machine now, so they run only in live mode; return to the live machine first")
		return
	}
	res, err := s.opt.RunExperiment(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "experiment_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- changing the active snapshot --------------------------------------

func (s *Server) handleCollect(w http.ResponseWriter, r *http.Request) {
	if !readBody(w, r, &struct{}{}) {
		return
	}
	if s.current().mode != ModeLive {
		writeError(w, http.StatusConflict, "not_live",
			"re-collection replaces the live snapshot; the active snapshot is not live (POST /api/snapshot/live returns to this machine)")
		return
	}
	s.respondCollect(w, r)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !readBody(w, r, &struct{}{}) {
		return
	}
	s.respondCollect(w, r)
}

func (s *Server) respondCollect(w http.ResponseWriter, r *http.Request) {
	_, err := s.collectLive(r.Context())
	var busy errBusy
	switch {
	case errors.Is(err, errNoLive):
		writeError(w, http.StatusConflict, "unavailable", err.Error())
	case errors.As(err, &busy):
		writeError(w, http.StatusConflict, "busy", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "collect_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, s.state())
	}
}

// handleImport validates an uploaded snapshot and makes it active. The body
// is the snapshot file itself. Every validation problem is returned, so the
// user can see all of what is wrong with a file at once.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	snap, err := snapshot.Decode(r.Body)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge) || errors.Is(err, snapshot.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
			fmt.Sprintf("a snapshot file is at most %d bytes", snapshot.MaxBytes))
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, "invalid_snapshot", "the file is not a Machine Atlas snapshot", err.Error())
		return
	}
	if errs := snapshot.Validate(snap); len(errs) > 0 {
		details := make([]string, len(errs))
		for i, e := range errs {
			details[i] = e.Error()
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_snapshot",
			fmt.Sprintf("the snapshot has %d problems", len(errs)), details...)
		return
	}
	s.respondActivate(w, ModeSnapshot, snap, "", "import")
}

func (s *Server) respondActivate(w http.ResponseWriter, mode Mode, snap *snapshot.Snapshot, fixture, origin string) {
	if _, err := s.activate(mode, snap, fixture, origin); err != nil {
		writeError(w, http.StatusConflict, "busy", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.state())
}

// --- redaction and export ----------------------------------------------

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	body := struct {
		Policy string `json:"policy"`
	}{Policy: snapshot.PolicyDefault}
	if !readBody(w, r, &body) {
		return
	}
	changes, err := snapshot.Preview(s.current().snap, body.Policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_policy", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Policy  string            `json:"policy"`
		Changes []snapshot.Change `json:"changes"`
	}{body.Policy, nonNil(changes)})
}

// handleExport sends the active snapshot, redacted with the requested
// policy (default "default"), as a file download. The file name is built
// from the redacted snapshot's content id only, never from anything a
// snapshot or request could choose.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	policy := r.URL.Query().Get("policy")
	if policy == "" {
		policy = snapshot.PolicyDefault
	}
	red, _, err := snapshot.Redact(s.current().snap, policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_policy", err.Error())
		return
	}
	data, err := snapshot.Encode(red)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "cannot encode the snapshot")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportName(red.ID)+`"`)
	writeRaw(w, http.StatusOK, data)
}

var hexID = regexp.MustCompile(`^sha256:([0-9a-f]{12})`)

// exportName is machine-atlas-<first 12 hex digits of the id>.json.
func exportName(id string) string {
	m := hexID.FindStringSubmatch(id)
	if m == nil {
		return "machine-atlas-snapshot.json"
	}
	return "machine-atlas-" + m[1] + ".json"
}

// --- fixtures ----------------------------------------------------------

// fixtureInfo describes one embedded demonstration snapshot.
type fixtureInfo struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
	Observations int    `json:"observations"`
}

var fixtureName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (s *Server) handleFixtures(w http.ResponseWriter, r *http.Request) {
	names, err := fs.Glob(s.opt.Fixtures, "snapshots/*.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := []fixtureInfo{}
	for _, file := range names {
		name := strings.TrimSuffix(path.Base(file), ".json")
		snap, err := s.loadFixture(name)
		if err != nil {
			continue // TestEmbeddedSnapshotsValidate keeps this from happening
		}
		desc := s.fixtureDescription(name)
		if desc == "" && snap.Collection.Mode == "live" {
			desc = "a redacted capture of a real machine"
		}
		out = append(out, fixtureInfo{
			Name: name, Description: desc,
			CreatedAt: snap.CreatedAt, Observations: len(snap.Observations),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleActivateFixture(w http.ResponseWriter, r *http.Request) {
	if !readBody(w, r, &struct{}{}) {
		return
	}
	name := r.PathValue("name")
	snap, err := s.loadFixture(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.respondActivate(w, ModeFixture, snap, name, "fixture")
}

// loadFixture loads and validates snapshots/<name>.json. The name is
// checked against a strict pattern before it is used in a path.
func (s *Server) loadFixture(name string) (*snapshot.Snapshot, error) {
	if !fixtureName.MatchString(name) {
		return nil, fmt.Errorf("%q is not a fixture name", name)
	}
	f, err := s.opt.Fixtures.Open("snapshots/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("no fixture %q", name)
	}
	defer f.Close()
	return snapshot.Load(f)
}

// fixtureDescription reads the description from the fixture root's
// _meta.json when it is embedded; the live sample has no root.
func (s *Server) fixtureDescription(name string) string {
	data, err := fs.ReadFile(s.opt.Fixtures, "roots/"+name+"/_meta.json")
	if err != nil {
		return ""
	}
	var meta struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.Description
}

// nonNil makes a nil slice encode as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
