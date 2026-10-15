package collect

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

type fakeCollector struct {
	id      string
	owns    []string
	timeout time.Duration
	collect func(ctx context.Context) []obs.Observation
}

func (f fakeCollector) ID() string     { return f.id }
func (f fakeCollector) Owns() []string { return f.owns }
func (f fakeCollector) Collect(ctx context.Context, _ Env) []obs.Observation {
	return f.collect(ctx)
}

// slowCollector is a fakeCollector with its own timeout.
type slowCollector struct{ fakeCollector }

func (s slowCollector) Timeout() time.Duration { return s.timeout }

func emits(o ...obs.Observation) func(context.Context) []obs.Observation {
	return func(context.Context) []obs.Observation { return o }
}

var meminfoSrc = obs.File("/proc/meminfo", "")

func find(t *testing.T, all []obs.Observation, id string) obs.Observation {
	t.Helper()
	i := slices.IndexFunc(all, func(o obs.Observation) bool { return o.ID == id })
	if i < 0 {
		t.Fatalf("no observation %s in %v", id, all)
	}
	return all[i]
}

func TestRunFillsAndSorts(t *testing.T) {
	env, _ := openTestFixture(t)
	c := fakeCollector{id: "memory.meminfo", owns: []string{"mem.meminfo.*", "mem.slabinfo.cache_count"}, collect: emits(
		obs.OK("mem.meminfo.MemTotal", 1048576, meminfoSrc),
		obs.OK("mem.meminfo.DirectMap4k", uint64(4096), meminfoSrc),
		obs.Denied("mem.slabinfo.cache_count", obs.File("/proc/slabinfo", ""), "open /proc/slabinfo: permission denied"),
	)}
	rep, all := Run(context.Background(), env, []Collector{c})

	if rep.Mode != "fixture" || rep.Fixture != "pi-test" || rep.Privileged {
		t.Errorf("report header = %+v", rep)
	}
	if !rep.StartedAt.Equal(env.Now()) || !rep.FinishedAt.Equal(env.Now()) {
		t.Errorf("fixture runs take their times from the fixture clock: %v %v", rep.StartedAt, rep.FinishedAt)
	}
	ids := []string{all[0].ID, all[1].ID, all[2].ID}
	if !slices.IsSorted(ids) || len(all) != 3 {
		t.Fatalf("observations not sorted or wrong count: %v", ids)
	}
	mt := find(t, all, "mem.meminfo.MemTotal")
	if mt.Value != int64(1048576) || mt.Unit != obs.UnitBytes || mt.Kind != obs.KindStatic ||
		mt.Scope != obs.ScopeKernel || mt.Collector != "memory.meminfo" || mt.CollectedAt != "2026-01-15T12:00:00Z" {
		t.Errorf("MemTotal not normalised and filled from the catalog: %+v", mt)
	}
	run := rep.Collectors[0]
	if run.Status != RunPartial || run.Error != "" || run.Observations != 3 || run.Duration != 0 {
		t.Errorf("run = %+v; want partial (one denied), no contract error, zero duration in fixture mode", run)
	}
}

func TestRunContractViolations(t *testing.T) {
	env, _ := openTestFixture(t)
	tests := []struct {
		name      string
		c         Collector
		problem   string
		check     func(t *testing.T, all []obs.Observation)
		runStatus string
	}{
		{
			name:    "undeclared id",
			c:       fakeCollector{id: "x", owns: []string{"mem.*"}, collect: emits(obs.OK("mem.nonsense", int64(1), meminfoSrc))},
			problem: "undeclared id mem.nonsense",
			check: func(t *testing.T, all []obs.Observation) {
				if len(all) != 0 {
					t.Errorf("undeclared observation kept: %v", all)
				}
			},
			runStatus: RunPartial,
		},
		{
			name:    "unowned id",
			c:       fakeCollector{id: "x", owns: []string{"mem.meminfo.*"}, collect: emits(obs.OK("kernel.release", "6.6.31", obs.File("/proc/sys/kernel/osrelease", "")))},
			problem: "does not own",
			check: func(t *testing.T, all []obs.Observation) {
				if len(all) != 0 {
					t.Errorf("unowned observation kept: %v", all)
				}
			},
			runStatus: RunPartial,
		},
		{
			name:    "owned exact id not emitted",
			c:       fakeCollector{id: "x", owns: []string{"mem.thp.enabled", "mem.thp.defrag"}, collect: emits(obs.OK("mem.thp.enabled", "always", obs.File("/sys/kernel/mm/transparent_hugepage/enabled", "")))},
			problem: "did not emit owned id mem.thp.defrag",
			check: func(t *testing.T, all []obs.Observation) {
				o := find(t, all, "mem.thp.defrag")
				if o.Status != obs.StatusFailed || o.Value != nil || o.Note == "" || o.Kind != obs.KindStatic {
					t.Errorf("missing id not filled as failed: %+v", o)
				}
			},
			runStatus: RunPartial,
		},
		{
			name:    "wrong value type",
			c:       fakeCollector{id: "x", owns: []string{"mem.meminfo.*"}, collect: emits(obs.OK("mem.meminfo.MemTotal", "16 GB", meminfoSrc))},
			problem: "catalog declares int",
			check: func(t *testing.T, all []obs.Observation) {
				if o := find(t, all, "mem.meminfo.MemTotal"); o.Status != obs.StatusFailed || o.Value != nil {
					t.Errorf("mistyped value not downgraded: %+v", o)
				}
			},
			runStatus: RunFailed,
		},
		{
			name:    "value with a non-ok status",
			c:       fakeCollector{id: "x", owns: []string{"mem.meminfo.*"}, collect: emits(obs.Observation{ID: "mem.meminfo.MemFree", Value: int64(0), Status: obs.StatusAbsent})},
			problem: "non-null value",
			check: func(t *testing.T, all []obs.Observation) {
				if o := find(t, all, "mem.meminfo.MemFree"); o.Value != nil {
					t.Errorf("value kept on an absent observation: %+v", o)
				}
			},
			runStatus: RunPartial,
		},
		{
			name: "panic",
			c: fakeCollector{id: "x", owns: []string{"mem.zswap.enabled"}, collect: func(context.Context) []obs.Observation {
				var m map[string]int
				m["boom"]++
				return nil
			}},
			problem: "collector panicked",
			check: func(t *testing.T, all []obs.Observation) {
				if o := find(t, all, "mem.zswap.enabled"); o.Status != obs.StatusFailed || !strings.Contains(o.Note, "panicked") {
					t.Errorf("owned id after panic: %+v", o)
				}
			},
			runStatus: RunFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, all := Run(context.Background(), env, []Collector{tt.c})
			run := rep.Collectors[0]
			if !strings.Contains(run.Error, tt.problem) {
				t.Errorf("report error %q does not mention %q", run.Error, tt.problem)
			}
			if run.Status != tt.runStatus {
				t.Errorf("status %q, want %q", run.Status, tt.runStatus)
			}
			tt.check(t, all)
		})
	}
}

func TestRunTimeoutAndBudget(t *testing.T) {
	env, _ := openTestFixture(t)
	release := make(chan struct{})
	defer close(release)
	hang := func(context.Context) []obs.Observation { <-release; return nil }

	stuck := slowCollector{fakeCollector{id: "stuck", owns: []string{"mem.zswap.enabled"}, timeout: 50 * time.Millisecond, collect: hang}}
	start := time.Now()
	rep, all := Run(context.Background(), env, []Collector{stuck})
	if time.Since(start) > time.Second {
		t.Errorf("runner waited %v for a 50ms collector", time.Since(start))
	}
	if run := rep.Collectors[0]; run.Status != RunFailed || !strings.Contains(run.Error, "timed out after 50ms") {
		t.Errorf("timeout run = %+v", run)
	}
	if o := find(t, all, "mem.zswap.enabled"); o.Status != obs.StatusFailed || !strings.Contains(o.Note, "timed out") {
		t.Errorf("owned id after timeout: %+v", o)
	}

	// The budget runs out during the first collector; the second never starts.
	long := slowCollector{fakeCollector{id: "long", owns: []string{"mem.zswap.enabled"}, timeout: time.Second, collect: hang}}
	next := fakeCollector{id: "next", owns: []string{"mem.thp.enabled"}, collect: emits(obs.OK("mem.thp.enabled", "never", obs.File("/sys/kernel/mm/transparent_hugepage/enabled", "")))}
	rep, all = Runner{Budget: 50 * time.Millisecond}.Run(context.Background(), env, []Collector{long, next})
	if run := rep.Collectors[0]; run.Status != RunFailed || !strings.Contains(run.Error, "budget") {
		t.Errorf("collector cut off by the budget: %+v", run)
	}
	if run := rep.Collectors[1]; run.Status != RunSkipped || run.Error != "" {
		t.Errorf("collector after the budget: %+v", run)
	}
	if o := find(t, all, "mem.thp.enabled"); o.Status != obs.StatusNotCollected || o.Value != nil {
		t.Errorf("id of a collector that never started: %+v", o)
	}
}

func TestRunDuplicateAcrossCollectors(t *testing.T) {
	env, _ := openTestFixture(t)
	a := fakeCollector{id: "a", owns: []string{"mem.meminfo.*"}, collect: emits(obs.OK("mem.meminfo.MemTotal", int64(1), meminfoSrc))}
	b := fakeCollector{id: "b", owns: []string{"mem.meminfo.*"}, collect: emits(obs.OK("mem.meminfo.MemTotal", int64(2), meminfoSrc))}
	rep, all := Run(context.Background(), env, []Collector{a, b})
	if len(all) != 1 || all[0].Value != int64(1) || all[0].Collector != "a" {
		t.Errorf("first emission should win: %+v", all)
	}
	if run := rep.Collectors[1]; !strings.Contains(run.Error, "already emitted by a") || run.Observations != 0 {
		t.Errorf("second collector's report: %+v", run)
	}
}

func TestSkip(t *testing.T) {
	env, _ := openTestFixture(t)
	probe := fakeCollector{id: "probe", owns: []string{"probe.regions", "probe.maps.vma_count"}, collect: func(context.Context) []obs.Observation {
		t.Error("a skipped collector must not run")
		return nil
	}}
	rep, all := Run(context.Background(), env, []Collector{Skip(probe, "disabled with --no-probe")})
	if run := rep.Collectors[0]; run.Status != RunSkipped || run.Error != "" || run.ID != "probe" {
		t.Errorf("skipped run = %+v", run)
	}
	for _, o := range all {
		if o.Status != obs.StatusNotCollected || o.Note != "disabled with --no-probe" || o.Scope != obs.ScopeProcess {
			t.Errorf("skipped observation = %+v", o)
		}
	}
	if len(all) != 2 {
		t.Errorf("want both owned ids, got %d", len(all))
	}
}
