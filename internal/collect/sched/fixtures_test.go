package sched

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const rootsDir = "../../../fixtures/roots"

type want struct {
	status obs.Status
	value  any
}

func ok(v any) want { return want{obs.StatusOK, v} }

var (
	absent      = want{status: obs.StatusAbsent}
	denied      = want{status: obs.StatusPermissionDenied}
	unsupported = want{status: obs.StatusUnsupported}
)

// fixtureWants lists, per root, the scheduler observations that make that
// root interesting (the system collectors have their own list in
// internal/collect/system). Every other observation only has to be not
// failed.
var fixtureWants = map[string]map[string]want{
	"desktop-x86_64": {
		"cpu.smt.control":                    ok("on"),
		"cpu.freq.intel_pstate.status":       ok("active"),
		"cpu.freq.boost":                     absent,
		"cpu.hybrid.performance_cpus":        absent,
		"cpu.isolated":                       ok(""),
		"sched.ext.state":                    ok("disabled"),
		"sched.schedstat.version":            ok(int64(16)),
		"sched.self.slice_us":                ok(int64(3000)),
		"sched.cgroup.cpu.max_quota_us":      ok("max"),
		"sched.cgroup.cpu.weight":            ok(int64(100)),
		"sched.cgroup.cpuset.cpus_effective": absent,
	},
	"rpi-arm64-noswap": {
		"cpu.smt.control":                          ok("notimplemented"),
		"cpu.nohz_full":                            absent,
		"cpu.freq.intel_pstate.status":             absent,
		"sched.ext.state":                          absent,
		"sched.self.slice_us":                      absent,
		"sched.cgroup.cpu.max_quota_us":            absent,
		"sched.cgroup.cpu.nr_periods":              absent,
		"sched.cgroup.cpu.usage_us":                {status: obs.StatusOK},
		"sched.sysctl.kernel.sched_util_clamp_min": absent,
	},
	"alpine-container": {
		"cpu.freq.policies":                  absent,
		"cpu.freq.intel_pstate.status":       absent,
		"sched.cgroup.cpu.max_quota_us":      ok(int64(150000)),
		"sched.cgroup.cpu.max_period_us":     ok(int64(100000)),
		"sched.cgroup.cpu.nr_throttled":      ok(int64(40213)),
		"sched.cgroup.cpuset.cpus_effective": ok("0-1"),
		"sched.self.cpus_allowed_list":       ok("0-1"),
	},
	"numa-server-la57": {
		"cpu.isolated":                 ok("8-15,40-47"),
		"cpu.nohz_full":                ok("8-15,40-47"),
		"sched.self.cpus_allowed_list": ok("0-7,16-39,48-63"),
		"sched.loadavg.runnable":       ok(int64(23)),
	},
	"old-vm-cgroupv1": {
		"cpu.freq.policies":                           absent,
		"cpu.smt.control":                             ok("notsupported"),
		"sched.psi.some.avg10":                        absent,
		"sched.ext.state":                             absent,
		"sched.schedstat.version":                     ok(int64(15)),
		"sched.self.slice_us":                         absent,
		"sched.cgroup.cpu.max_quota_us":               ok(int64(150000)),
		"sched.cgroup.cpu.shares_v1":                  ok(int64(1024)),
		"sched.cgroup.cpu.weight":                     unsupported,
		"sched.cgroup.cpu.user_us":                    unsupported,
		"sched.cgroup.cpuset.cpus_effective":          ok("0-1"),
		"sched.sysctl.kernel.sched_autogroup_enabled": ok(int64(0)),
	},
	"restricted": {
		"cpu.topology":            denied,
		"cpu.cache_domains":       denied,
		"cpu.freq.policies":       denied,
		"cpu.isolated":            denied,
		"sched.psi.some.avg10":    denied,
		"sched.schedstat.cpus":    denied,
		"sched.loadavg.avg1":      ok(0.52),
		"sched.self.nice":         ok(int64(10)),
		"sched.self.policy":       ok("SCHED_BATCH"),
		"sched.cgroup.cpu.weight": ok(int64(20)),
		"sched.self.load_weight":  ok(int64(112640)),
	},
}

// TestFixtureRoots runs the scheduler collectors over every fixture root:
// no observation may be failed or lack a note, and each root shows what it
// exists to demonstrate.
func TestFixtureRoots(t *testing.T) {
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		t.Fatal(err)
	}
	var roots int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		roots++
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			env, err := collect.OpenFixture(filepath.Join(rootsDir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer env.Close()
			byID := run(t, env)
			for id, o := range byID {
				if o.Status == obs.StatusFailed {
					t.Errorf("%s failed: %s", id, o.Note)
				}
				if o.Status != obs.StatusOK && o.Note == "" {
					t.Errorf("%s is %s without a note", id, o.Status)
				}
			}
			for id, w := range fixtureWants[name] {
				o := byID[id]
				switch {
				case o.Status != w.status:
					t.Errorf("%s: status %s (%s), want %s", id, o.Status, o.Note, w.status)
				case w.value != nil && !reflect.DeepEqual(o.Value, w.value):
					t.Errorf("%s = %#v, want %#v", id, o.Value, w.value)
				}
			}
		})
	}
	if roots != len(fixtureWants) {
		t.Errorf("found %d roots, fixtureWants describes %d", roots, len(fixtureWants))
	}
}

// The cache table of the numa server: one L1d, L1i and L2 per core, one L3
// per socket.
func TestFixtureCacheDomains(t *testing.T) {
	env, err := collect.OpenFixture(filepath.Join(rootsDir, "numa-server-la57"))
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	rows := run(t, env)[cachesID].Value.([]map[string]any)
	count := map[int64]int{}
	for _, r := range rows {
		count[r["level"].(int64)]++
	}
	if count[1] != 64 || count[2] != 32 || count[3] != 2 {
		t.Errorf("cache copies per level = %v, want 64 L1 (data and instruction per core), 32 L2, 2 L3", count)
	}
}
