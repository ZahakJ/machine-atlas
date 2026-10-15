// Package server is the HTTP side of "atlas serve" (ARCHITECTURE.md §10): a JSON
// API over one active snapshot, and the browser interface embedded from
// web/.
//
// The server is reachable only from this computer. Every request passes the
// guards in guard.go: the Host header must name a loopback address with the
// bound port (a DNS-rebinding defence), state-changing requests must carry
// the per-process token and, when the browser sends one, a same-origin
// Origin; no CORS headers are ever sent. Every response carries the content
// security policy and the other security headers.
//
// Exactly one snapshot is active at a time. Activating one computes its
// derived facts, journey, component graph and roadmap once; requests then
// read that immutable activation under a mutex held only long enough to
// copy a pointer.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/experiment"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

// Mode says what the active snapshot describes.
type Mode string

const (
	// ModeLive: a collection of this machine made by this process.
	ModeLive Mode = "live"
	// ModeSnapshot: a file given on the command line or imported in the
	// browser. It may describe another machine, or this one in the past.
	ModeSnapshot Mode = "snapshot"
	// ModeFixture: an embedded demonstration snapshot, not a real machine.
	ModeFixture Mode = "fixture"
)

// Timeouts. Writes allow for the longest request, an experiment or a
// collection; the experiment package enforces each experiment's own bound.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 2 * time.Minute
	idleTimeout       = 60 * time.Second
	collectTimeout    = 60 * time.Second
)

// Options configure a Server.
type Options struct {
	// Port is the TCP port the server is bound to; the Host guard requires
	// it in every request.
	Port int
	// AllowRemote also accepts Host headers naming a non-loopback IP
	// literal, for "atlas serve --allow-remote". Host names other than
	// localhost are still refused: an IP literal cannot be rebound.
	AllowRemote bool

	// Mode, Snapshot and Fixture are the initial activation. Origin names
	// where a ModeSnapshot snapshot came from ("file" or "import").
	Mode     Mode
	Snapshot *snapshot.Snapshot
	Fixture  string
	Origin   string

	// Collect collects this machine. Nil when this build or platform cannot
	// collect; live mode and experiments are then unavailable.
	Collect func(ctx context.Context) (*snapshot.Snapshot, error)

	// Curriculum is the curriculum lessons are bound from; nil means
	// curriculum.Default().
	Curriculum *curriculum.Curriculum

	// Web holds index.html, css/ and js/; nil means WebFS().
	Web fs.FS
	// Fixtures holds snapshots/<name>.json (and optionally
	// roots/<name>/_meta.json for descriptions); nil means the embedded
	// fixtures.FS.
	Fixtures fs.FS

	// ListExperiments and RunExperiment default to the experiment
	// package; tests replace them.
	ListExperiments func() []experiment.Info
	RunExperiment   func(ctx context.Context, id string) (experiment.Result, error)
}

// Server serves the API and the interface. Create it with New.
type Server struct {
	opt     Options
	cur     *curriculum.Curriculum
	token   string
	usage   map[string]curriculum.Users
	handler http.Handler

	mu     sync.Mutex
	active *activation
	busy   string // "a collection", "an experiment" or "an activation" while one runs
}

// activation is one snapshot made active, with everything computed from it.
// It is never modified after activate returns, except for the lesson cache,
// which has its own lock.
type activation struct {
	mode    Mode
	fixture string
	origin  string

	snap       *snapshot.Snapshot
	encoded    []byte // canonical encoding, served by GET /api/snapshot
	facts      []derive.Fact
	journey    curriculum.Journey
	components curriculum.ComponentGraph
	roadmap    curriculum.RoadmapView
	counts     map[obs.Status]int

	lessonsMu sync.Mutex
	lessons   map[string][]byte // lesson id → bound lesson JSON
}

// New validates the options and makes the initial snapshot active.
func New(opt Options) (*Server, error) {
	if opt.Snapshot == nil {
		return nil, errors.New("server: no initial snapshot")
	}
	if opt.Port <= 0 || opt.Port > 65535 {
		return nil, fmt.Errorf("server: invalid port %d", opt.Port)
	}
	s := &Server{opt: opt, cur: opt.Curriculum}
	if s.cur == nil {
		var err error
		if s.cur, err = curriculum.Default(); err != nil {
			return nil, err
		}
	}
	if s.opt.Web == nil {
		web, err := WebFS()
		if err != nil {
			return nil, err
		}
		s.opt.Web = web
	}
	if s.opt.Fixtures == nil {
		s.opt.Fixtures = embeddedFixtures
	}
	if s.opt.ListExperiments == nil {
		s.opt.ListExperiments = experiment.List
	}
	if s.opt.RunExperiment == nil {
		s.opt.RunExperiment = experiment.Run
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	s.token = hex.EncodeToString(b[:])
	s.usage = s.cur.Usage()
	s.active = s.activation(opt.Mode, opt.Snapshot, opt.Fixture, opt.Origin)
	s.handler = s.guard(s.routes())
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// Token returns the per-process token state-changing requests must carry.
func (s *Server) Token() string { return s.token }

// HTTPServer returns an http.Server for s with the timeouts of §10.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Handler:           s,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    64 << 10,
	}
}

// activation computes everything the API serves about one snapshot.
func (s *Server) activation(mode Mode, snap *snapshot.Snapshot, fixture, origin string) *activation {
	a := &activation{mode: mode, fixture: fixture, origin: origin, snap: snap, lessons: map[string][]byte{}}
	a.encoded, _ = snapshot.Encode(snap) // validated snapshots always encode
	a.facts = derive.All(snap)
	a.journey = s.cur.Select(snap, a.facts)
	a.components = s.cur.Components(snap, a.facts)
	a.roadmap = s.cur.Roadmap(snap, a.facts)
	a.counts = map[obs.Status]int{}
	for _, st := range allStatuses {
		a.counts[st] = 0
	}
	for _, o := range snap.Observations {
		a.counts[o.Status]++
	}
	return a
}

var allStatuses = []obs.Status{
	obs.StatusOK, obs.StatusAbsent, obs.StatusPermissionDenied,
	obs.StatusUnsupported, obs.StatusFailed, obs.StatusNotCollected,
}

// current returns the active snapshot's activation.
func (s *Server) current() *activation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// errBusy is returned while a collection or an experiment runs.
type errBusy struct{ task string }

func (e errBusy) Error() string {
	return e.task + " is in progress; try again when it has finished"
}

// begin marks task as running, or reports the one already running. Only one
// collection or experiment runs at a time, and nothing is activated while
// one does: an experiment's measurements would be disturbed by a collection,
// and a collection finishing late must not replace a snapshot the user
// chose meanwhile.
func (s *Server) begin(task string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy != "" {
		return errBusy{s.busy}
	}
	s.busy = task
	return nil
}

func (s *Server) end() {
	s.mu.Lock()
	s.busy = ""
	s.mu.Unlock()
}

// activate computes an activation and makes it current, unless a
// collection or experiment is in progress.
func (s *Server) activate(mode Mode, snap *snapshot.Snapshot, fixture, origin string) (*activation, error) {
	if err := s.begin("an activation"); err != nil {
		return nil, err
	}
	defer s.end()
	return s.swap(s.activation(mode, snap, fixture, origin)), nil
}

func (s *Server) swap(a *activation) *activation {
	s.mu.Lock()
	s.active = a
	s.mu.Unlock()
	return a
}

// collectLive collects this machine and activates the result in live mode.
func (s *Server) collectLive(ctx context.Context) (*activation, error) {
	if s.opt.Collect == nil {
		return nil, errNoLive
	}
	if err := s.begin("a collection"); err != nil {
		return nil, err
	}
	defer s.end()
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()
	snap, err := s.opt.Collect(ctx)
	if err != nil {
		return nil, err
	}
	return s.swap(s.activation(ModeLive, snap, "", "collection")), nil
}

var errNoLive = errors.New("this build cannot collect from this machine (live collection needs Linux)")

// WebFS returns the interface's files. With ATLAS_WEB_DIR set to a
// directory (normally web/ of a checkout), files are read from disk on every
// request, so interface contributors edit and reload without rebuilding.
// The directory is opened as an os.Root: neither ".." nor a symbolic link
// can reach outside it. Without the variable the copy embedded at build
// time is served.
func WebFS() (fs.FS, error) {
	dir := os.Getenv("ATLAS_WEB_DIR")
	if dir == "" {
		return embeddedWeb, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("ATLAS_WEB_DIR: %w", err)
	}
	return root.FS(), nil
}
