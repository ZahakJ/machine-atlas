package collect

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Time limits for a collection run.
const (
	DefaultTimeout = 2 * time.Second
	TotalBudget    = 15 * time.Second
)

// Collector run statuses in the report.
const (
	RunOK      = "ok"      // every observation ok, no contract problems
	RunPartial = "partial" // some observations not ok, or contract problems
	RunFailed  = "failed"  // timed out, panicked, or nothing but failures
	RunSkipped = "skipped" // every observation not_collected
)

// Report describes one collection run.
type Report struct {
	Mode       string // "live" | "fixture"
	Fixture    string // fixture name in fixture mode
	Privileged bool   // collected as root
	StartedAt  time.Time
	FinishedAt time.Time
	Collectors []CollectorRun
}

// CollectorRun is the report line for one collector.
type CollectorRun struct {
	ID           string
	Status       string
	Duration     time.Duration // zero in fixture mode, where timings describe this computer, not the fixture
	Observations int
	// Error lists what went wrong with the collector itself (timeout, panic)
	// and every way its output broke the contract (undeclared or unowned
	// ids, wrong value types, missing owned ids). Empty for a correct
	// collector, however many of its readings were denied or absent.
	Error string
}

// Runner runs collectors. The zero value uses TotalBudget.
type Runner struct {
	// Budget bounds the whole run; collectors not started within it report
	// their ids as not_collected.
	Budget time.Duration
}

// Run runs collectors with the default budget.
func Run(ctx context.Context, env Env, collectors []Collector) (Report, []obs.Observation) {
	return Runner{}.Run(ctx, env, collectors)
}

// Run executes the collectors one after another, each under its own timeout,
// and returns the report and every observation sorted by id. Collectors run
// sequentially: the total is small, and sequential runs keep the probe's
// measurements free of interference from other collectors.
//
// Every observation is checked against the catalog and the collector's Owns
// list. An observation that breaks the contract is dropped or downgraded to
// failed, and the problem is named in the collector's report line, so a bug
// in one collector can neither corrupt the snapshot nor go unnoticed.
func (r Runner) Run(ctx context.Context, env Env, collectors []Collector) (Report, []obs.Observation) {
	budget := r.Budget
	if budget <= 0 {
		budget = TotalBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	rep := Report{Mode: "live", StartedAt: env.Now()}
	if !env.Live() {
		rep.Mode = "fixture"
		if f, ok := env.(*FixtureEnv); ok {
			rep.Fixture = f.Name()
		}
	}
	rep.Privileged = env.Live() && os.Geteuid() == 0

	var all []obs.Observation
	seen := map[string]string{} // id → collector that emitted it
	for _, c := range collectors {
		run, out := runOne(ctx, env, c)
		kept := out[:0]
		var dups []string
		for _, o := range out {
			if prev, dup := seen[o.ID]; dup {
				dups = append(dups, fmt.Sprintf("id %s already emitted by %s", o.ID, prev))
				continue
			}
			seen[o.ID] = c.ID()
			kept = append(kept, o)
		}
		if len(dups) > 0 {
			run.Error = joinProblems(run.Error, dups)
			if run.Status == RunOK {
				run.Status = RunPartial
			}
		}
		run.Observations = len(kept)
		all = append(all, kept...)
		rep.Collectors = append(rep.Collectors, run)
	}
	slices.SortFunc(all, func(a, b obs.Observation) int { return strings.Compare(a.ID, b.ID) })
	rep.FinishedAt = env.Now()
	return rep, all
}

type outcome struct {
	obs   []obs.Observation
	panic any
}

// failure is a collector-level failure: what happened, and the status its
// owned ids get because of it.
type failure struct {
	status obs.Status
	note   string
}

// runOne runs a single collector under its timeout and checks its output.
func runOne(ctx context.Context, env Env, c Collector) (CollectorRun, []obs.Observation) {
	run := CollectorRun{ID: c.ID()}
	if ctx.Err() != nil {
		return finish(run, c, env, nil, &failure{obs.StatusNotCollected, "collection time budget exhausted"})
	}
	timeout := DefaultTimeout
	if tc, ok := c.(TimeoutCollector); ok {
		timeout = tc.Timeout()
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	done := make(chan outcome, 1) // buffered: an abandoned collector must not block forever
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- outcome{panic: p}
			}
		}()
		done <- outcome{obs: c.Collect(cctx, env)}
	}()

	var res outcome
	var fail *failure
	select {
	case res = <-done:
		if res.panic != nil {
			fail = &failure{obs.StatusFailed, fmt.Sprintf("collector panicked: %v", res.panic)}
		}
	case <-cctx.Done():
		note := fmt.Sprintf("collector timed out after %s", timeout)
		if ctx.Err() != nil {
			note = "collection time budget exhausted while the collector ran"
		}
		fail = &failure{obs.StatusFailed, note}
	}
	if env.Live() {
		run.Duration = time.Since(start)
	}
	return finish(run, c, env, res.obs, fail)
}

// finish checks a collector's output, accounts for every exact id it owns,
// and computes its report status. With a failure, the owned ids it did not
// emit take the failure's status and note; otherwise a missing owned id is a
// contract problem and is reported as failed.
func finish(run CollectorRun, c Collector, env Env, out []obs.Observation, fail *failure) (CollectorRun, []obs.Observation) {
	patterns := c.Owns()
	now := obs.Timestamp(env.Now())
	emitted := map[string]bool{}
	var kept []obs.Observation
	var problems []string
	for _, o := range out {
		o, keep, problem := check(o, patterns, emitted)
		if problem != "" {
			problems = append(problems, problem)
		}
		if !keep {
			continue
		}
		emitted[o.ID] = true
		o.Collector = c.ID()
		if o.CollectedAt == "" {
			o.CollectedAt = now
		}
		kept = append(kept, o)
	}

	missing := failure{obs.StatusFailed, "collector did not emit this id"}
	if fail != nil {
		missing = *fail
	}
	for _, id := range exactIDs(patterns) {
		if emitted[id] {
			continue
		}
		if fail == nil {
			problems = append(problems, "did not emit owned id "+id)
		}
		o := obs.Observation{ID: id, Status: missing.status, Note: missing.note, Collector: c.ID(), CollectedAt: now}
		fillFromCatalog(&o)
		kept = append(kept, o)
	}
	fatal := fail != nil && fail.status == obs.StatusFailed
	if fatal {
		problems = append([]string{fail.note}, problems...)
	}

	run.Status = runStatus(kept, fatal, len(problems) > 0)
	run.Error = joinProblems("", problems)
	return run, kept
}

// check validates one emitted observation against the catalog and the
// collector's ownership patterns. It returns the observation, possibly
// downgraded to failed, whether to keep it, and the problem found, if any.
func check(o obs.Observation, patterns []string, emitted map[string]bool) (obs.Observation, bool, string) {
	switch {
	case !obs.ValidID(o.ID):
		return o, false, fmt.Sprintf("invalid id %q", o.ID)
	case emitted[o.ID]:
		return o, false, "emitted " + o.ID + " twice"
	case !owns(patterns, o.ID):
		return o, false, "emitted " + o.ID + ", which this collector does not own"
	}
	entry, ok := catalog.Lookup(o.ID)
	if !ok {
		return o, false, "emitted undeclared id " + o.ID
	}
	fillFromCatalog(&o)
	o.Note = clip(o.Note)

	if !o.Status.Valid() {
		return downgrade(o, fmt.Sprintf("invalid status %q", o.Status))
	}
	if o.Status != obs.StatusOK {
		if o.Value != nil {
			o.Value = nil
			return o, true, o.ID + ": non-null value with status " + string(o.Status)
		}
		return o, true, ""
	}
	v, err := obs.Normalize(o.Value)
	if err == nil && v == nil {
		err = fmt.Errorf("null value with status ok")
	}
	if err == nil {
		err = entry.Check(v)
	}
	if err == nil {
		err = obs.CheckLimits(v)
	}
	if err != nil {
		return downgrade(o, err.Error())
	}
	o.Value = v
	if o.Unit == "" && obs.TypeOf(v) != obs.TypeString {
		o.Unit = entry.Unit
	}
	return o, true, ""
}

// downgrade turns a malformed observation into a failed one, keeping the id
// so the gap is visible, and reports why.
func downgrade(o obs.Observation, why string) (obs.Observation, bool, string) {
	o.Status, o.Value, o.Note = obs.StatusFailed, nil, "rejected by the collection runner: "+why
	return o, true, o.ID + ": " + why
}

// fillFromCatalog supplies kind and scope from the catalog when the
// collector left them empty: they are properties of the id, so collectors
// need not repeat them.
func fillFromCatalog(o *obs.Observation) {
	e, ok := catalog.Lookup(o.ID)
	if !ok {
		return
	}
	if o.Kind == "" {
		o.Kind = e.Kind
	}
	if o.Scope == "" {
		o.Scope = e.Scope
	}
}

func runStatus(out []obs.Observation, fatal, problems bool) string {
	counts := map[obs.Status]int{}
	for _, o := range out {
		counts[o.Status]++
	}
	n := len(out)
	switch {
	case fatal || (n > 0 && counts[obs.StatusFailed] == n):
		return RunFailed
	case n > 0 && counts[obs.StatusNotCollected] == n:
		return RunSkipped
	case problems || counts[obs.StatusOK] < n:
		return RunPartial
	default:
		return RunOK
	}
}

// joinProblems appends problems to an existing error text, clipped so the
// report line stays within the snapshot's string limit.
func joinProblems(existing string, problems []string) string {
	if existing != "" {
		problems = append([]string{existing}, problems...)
	}
	return clip(strings.Join(problems, "; "))
}

// clip shortens s to the snapshot's string limit, marking the cut.
func clip(s string) string {
	if len(s) <= obs.MaxStringBytes {
		return s
	}
	return strings.ToValidUTF8(s[:obs.MaxStringBytes-len("…")], "") + "…"
}
