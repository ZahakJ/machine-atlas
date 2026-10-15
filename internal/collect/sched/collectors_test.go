package sched

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// machine builds a fixture root from machine paths and their contents, plus
// the paths that must fail with EACCES, and opens it.
func machine(t *testing.T, files map[string]string, deny ...string) *collect.FixtureEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test-root")
	meta, _ := json.Marshal(map[string]any{
		"description": "test", "now": "2026-01-15T12:00:00Z", "page_size": 4096,
		"uname": map[string]string{"machine": "x86_64", "release": "6.12.0"}, "deny": deny,
	})
	files = maps.Clone(files)
	files["/_meta.json"] = string(meta)
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, err := collect.OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.Close() })
	return env
}

// run collects with every scheduler collector and fails the test on any
// contract problem the runner found.
func run(t *testing.T, env collect.Env) map[string]obs.Observation {
	t.Helper()
	rep, out := collect.Run(context.Background(), env, Collectors())
	for _, r := range rep.Collectors {
		if r.Error != "" {
			t.Errorf("collector %s: %s", r.ID, r.Error)
		}
	}
	byID := map[string]obs.Observation{}
	for _, o := range out {
		byID[o.ID] = o
	}
	return byID
}

const cg2 = "/sys/fs/cgroup/test.slice/"

// modernMachine is a hybrid x86 machine on Linux 6.12 with every interface
// present: four CPUs, two performance threads on one core and two
// efficiency cores, and a cgroup v2 quota.
func modernMachine() map[string]string {
	f := map[string]string{
		"/sys/devices/system/cpu/online":                "0-3\n",
		"/sys/devices/system/cpu/present":               "0-3\n",
		"/sys/devices/system/cpu/possible":              "0-7\n",
		"/sys/devices/system/cpu/offline":               "4-7\n",
		"/sys/devices/system/cpu/isolated":              "\n",
		"/sys/devices/system/cpu/nohz_full":             "3\n",
		"/sys/devices/system/cpu/smt/control":           "on\n",
		"/sys/devices/system/cpu/smt/active":            "1\n",
		"/sys/devices/cpu_core/cpus":                    "0-1\n",
		"/sys/devices/cpu_atom/cpus":                    "2-3\n",
		"/sys/devices/system/cpu/cpufreq/boost":         "1\n",
		"/sys/devices/system/cpu/intel_pstate/status":   "active\n",
		"/sys/devices/system/cpu/intel_pstate/no_turbo": "0\n",
		"/sys/devices/system/cpu/amd_pstate/status":     "passive\n",
		"/proc/loadavg":                                 "1.50 1.20 0.90 2/512 4242\n",
		"/proc/stat":                                    "cpu  400 0 100 2000 10 0 5 0 0 0\ncpu0 100 0 25 500 3 0 1 0 0 0\ncpu1 100 0 25 500 3 0 1 0 0 0\ncpu2 100 0 25 500 2 0 2 0 0 0\ncpu3 100 0 25 500 2 0 1 0 0 0\nctxt 90000\nbtime 1\nprocesses 300\nprocs_running 2\nprocs_blocked 0\n",
		"/proc/schedstat":                               "version 16\ntimestamp 1\ncpu0 0 0 0 0 0 0 1000000 200000 50\ncpu1 0 0 0 0 0 0 1000000 100000 40\ncpu2 0 0 0 0 0 0 500000 50000 20\ncpu3 0 0 0 0 0 0 500000 50000 20\n",
		"/proc/pressure/cpu":                            "some avg10=1.00 avg60=0.50 avg300=0.25 total=123456\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"/sys/kernel/sched_ext/state":                   "disabled\n",
		"/proc/self/status":                             "Name:\tatlas\nCpus_allowed:\tf\nCpus_allowed_list:\t0-3\nvoluntary_ctxt_switches:\t12\nnonvoluntary_ctxt_switches:\t3\n",
		"/proc/self/stat":                               "4242 (atlas) S 1 4242 4242 0 -1 4194560 300 0 0 0 5 2 0 0 20 0 9 0 1000 700000000 3000 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0 0 0 0 0 0 0 0 0\n",
		"/proc/self/schedstat":                          "5000000 1000000 40\n",
		"/proc/self/sched":                              "atlas (4242, #threads: 9)\n---\nse.load.weight : 1048576\nse.slice : 2800000\n",
		"/proc/self/cgroup":                             "0::/test.slice\n",
		"/proc/self/mountinfo":                          "28 20 0:26 / /sys/fs/cgroup rw,nosuid - cgroup2 cgroup2 rw,nsdelegate\n",
		cg2 + "cpu.max":                                 "150000 100000\n",
		cg2 + "cpu.weight":                              "100\n",
		cg2 + "cpu.stat":                                "usage_usec 9000\nuser_usec 7000\nsystem_usec 2000\nnr_periods 40\nnr_throttled 10\nthrottled_usec 50000\n",
		cg2 + "cpuset.cpus.effective":                   "0-1\n",
	}
	for _, s := range schedSysctls {
		f["/proc/sys/kernel/"+s.name] = "1\n"
	}
	for c := range 4 {
		d := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/", c)
		core, sib := c, fmt.Sprint(c)
		if c < 2 {
			core, sib = 0, "0-1"
		}
		f[d+"topology/physical_package_id"] = "0\n"
		f[d+"topology/die_id"] = "0\n"
		f[d+"topology/cluster_id"] = fmt.Sprintf("%d\n", core/2)
		f[d+"topology/core_id"] = fmt.Sprintf("%d\n", core)
		f[d+"topology/thread_siblings_list"] = sib + "\n"
		f[d+"cpu_capacity"] = map[bool]string{true: "1024\n", false: "600\n"}[c < 2]
		f[d+"cache/index0/level"] = "1\n"
		f[d+"cache/index0/type"] = "Data\n"
		f[d+"cache/index0/size"] = "48K\n"
		f[d+"cache/index0/shared_cpu_list"] = sib + "\n"
		f[d+"cache/index1/level"] = "3\n"
		f[d+"cache/index1/type"] = "Unified\n"
		f[d+"cache/index1/size"] = "12288K\n"
		f[d+"cache/index1/shared_cpu_list"] = "0-3\n"
		p := fmt.Sprintf("/sys/devices/system/cpu/cpufreq/policy%d/", c)
		f[p+"affected_cpus"] = fmt.Sprintf("%d\n", c)
		f[p+"scaling_driver"] = "intel_pstate\n"
		f[p+"scaling_governor"] = "powersave\n"
		f[p+"scaling_available_governors"] = "performance powersave\n"
		f[p+"scaling_cur_freq"] = "1200000\n"
		f[p+"scaling_min_freq"] = "800000\n"
		f[p+"scaling_max_freq"] = "4700000\n"
		f[p+"cpuinfo_min_freq"] = "800000\n"
		f[p+"cpuinfo_max_freq"] = "4700000\n"
		f[p+"energy_performance_preference"] = "balance_performance\n"
	}
	return f
}

func TestModernMachine(t *testing.T) {
	got := run(t, machine(t, modernMachine()))
	for id, o := range got {
		if id == cgSharesID {
			if o.Status != obs.StatusUnsupported {
				t.Errorf("%s on cgroup v2: %s, want unsupported", id, o.Status)
			}
			continue
		}
		if o.Status != obs.StatusOK {
			t.Errorf("%s: %s (%s)", id, o.Status, o.Note)
		}
	}
	topo := got[topoID].Value.([]map[string]any)
	if len(topo) != 4 || topo[0]["core_type"] != "performance" || topo[3]["core_type"] != "efficiency" || topo[3]["capacity"] != int64(600) || topo[1]["thread_siblings"] != "0-1" {
		t.Errorf("topology = %v", topo)
	}
	caches := got[cachesID].Value.([]map[string]any)
	if len(caches) != 4 || caches[0]["shared_cpu_list"] != "0-1" || caches[3]["level"] != int64(3) || caches[3]["cpus"] != int64(4) {
		t.Errorf("cache domains = %v; want three L1 copies (one per core) and one shared L3", caches)
	}
	pol := got[policiesID].Value.([]map[string]any)
	if len(pol) != 4 || pol[2]["policy"] != int64(2) || pol[2]["cpus"] != "2" || pol[0]["epp"] != "balance_performance" || pol[0]["hw_max_khz"] != int64(4700000) {
		t.Errorf("policies = %v", pol)
	}
	for id, want := range map[string]any{
		"cpu.isolated": "", "cpu.nohz_full": "3", "sched.loadavg.avg1": 1.5, "sched.loadavg.runnable": int64(2),
		"sched.stat.ctxt": int64(90000), "sched.psi.some.avg10": 1.0, "sched.self.nice": int64(0),
		"sched.self.policy": "SCHED_OTHER", "sched.self.run_us": int64(5000), "sched.self.slice_us": int64(2800),
		"sched.self.load_weight": int64(1048576), "sched.self.cpus_allowed_list": "0-3",
		cgQuotaIDs[0]: int64(150000), cgQuotaIDs[1]: int64(100000), "sched.cgroup.cpu.nr_throttled": int64(10),
		cgCpusetID: "0-1", "sched.ext.state": "disabled",
	} {
		if o := got[id]; o.Value != want {
			t.Errorf("%s = %#v, want %#v", id, o.Value, want)
		}
	}
	if s := got[cgQuotaIDs[0]].Source.Path; s != "/sys/fs/cgroup/<cgroup>/cpu.max" {
		t.Errorf("cgroup source %q names the cgroup", s)
	}
}

// TestMalformed puts garbage in every file except the ones that locate the
// cgroup: every id must then be failed, with the parse error as the note.
func TestMalformed(t *testing.T) {
	files := modernMachine()
	for p := range files {
		if p != "/proc/self/cgroup" && p != "/proc/self/mountinfo" {
			files[p] = "garbage ) x\n"
		}
	}
	for id, o := range run(t, machine(t, files)) {
		if id == cgSharesID {
			continue
		}
		if o.Status != obs.StatusFailed || o.Note == "" {
			t.Errorf("%s: %s (%s), want failed with a note", id, o.Status, o.Note)
		}
	}
}

// TestNothingThere: an empty machine gives every id a non-ok status with a
// note, never a value.
func TestNothingThere(t *testing.T) {
	for id, o := range run(t, machine(t, map[string]string{})) {
		if o.Status == obs.StatusOK || o.Status == obs.StatusFailed || o.Note == "" {
			t.Errorf("%s: %s (%s), want absent or unsupported with a note", id, o.Status, o.Note)
		}
	}
}

func TestDenied(t *testing.T) {
	got := run(t, machine(t, modernMachine(), "/sys/devices/system/cpu", "/proc/pressure", "/proc/schedstat"))
	for _, id := range []string{topoID, cachesID, policiesID, "cpu.isolated", smtCtrlID, "sched.psi.some.avg10", "sched.psi.full.total_us", "sched.schedstat.cpus"} {
		if o := got[id]; o.Status != obs.StatusPermissionDenied {
			t.Errorf("%s: %s (%s), want permission_denied", id, o.Status, o.Note)
		}
	}
	if o := got["cpu.hybrid.performance_cpus"]; o.Status != obs.StatusOK {
		t.Errorf("the hybrid list is outside the denied directory: %+v", o)
	}
}

// An older machine: CFS kernel, no full PSI line, cgroup v1 with a quota,
// no cpufreq and no hybrid lists.
func TestOlderMachine(t *testing.T) {
	f := modernMachine()
	for p := range f {
		if strings.HasPrefix(p, "/sys/devices/system/cpu/cpufreq") || strings.HasPrefix(p, cg2) || strings.HasPrefix(p, "/sys/devices/cpu_") ||
			strings.Contains(p, "intel_pstate") || strings.Contains(p, "amd_pstate") || strings.Contains(p, "sched_ext") {
			delete(f, p)
		}
	}
	f["/proc/pressure/cpu"] = "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\n"
	f["/proc/self/sched"] = "atlas (4242, #threads: 9)\n---\nse.load.weight : 1024\n"
	f["/proc/self/cgroup"] = "3:cpu,cpuacct:/user.slice\n11:cpuset:/\n1:name=systemd:/user.slice\n"
	f["/proc/self/mountinfo"] = "32 20 0:25 / /sys/fs/cgroup/cpu,cpuacct rw - cgroup cgroup rw,cpu,cpuacct\n40 20 0:33 / /sys/fs/cgroup/cpuset rw - cgroup cgroup rw,cpuset\n"
	v1 := "/sys/fs/cgroup/cpu,cpuacct/user.slice/"
	f[v1+"cpu.cfs_quota_us"] = "-1\n"
	f[v1+"cpu.cfs_period_us"] = "100000\n"
	f[v1+"cpu.shares"] = "1024\n"
	f[v1+"cpuacct.usage"] = "5000000\n"
	f[v1+"cpu.stat"] = "nr_periods 0\nnr_throttled 0\nthrottled_time 0\n"
	f["/sys/fs/cgroup/cpuset/cpuset.effective_cpus"] = "0-3\n"
	got := run(t, machine(t, f))
	for id, want := range map[string]obs.Status{
		policiesID: obs.StatusAbsent, boostID: obs.StatusAbsent, pstateStatusID: obs.StatusAbsent,
		"cpu.hybrid.efficiency_cpus": obs.StatusAbsent, "sched.ext.state": obs.StatusAbsent,
		"sched.psi.full.avg10": obs.StatusAbsent, "sched.self.slice_us": obs.StatusAbsent,
		cgWeightID: obs.StatusUnsupported, "sched.cgroup.cpu.user_us": obs.StatusUnsupported,
	} {
		if o := got[id]; o.Status != want || o.Note == "" {
			t.Errorf("%s: %s (%s), want %s with a note", id, o.Status, o.Note, want)
		}
	}
	for id, want := range map[string]any{
		cgQuotaIDs[0]: "max", cgSharesID: int64(1024), "sched.cgroup.cpu.usage_us": int64(5000),
		"sched.cgroup.cpu.throttled_us": int64(0), cgCpusetID: "0-3", "sched.self.load_weight": int64(1024),
	} {
		if o := got[id]; o.Status != obs.StatusOK || o.Value != want {
			t.Errorf("%s = %#v (%s, %s), want %#v", id, o.Value, o.Status, o.Note, want)
		}
	}
	if s := got[cgQuotaIDs[0]].Source.Path; s != "/sys/fs/cgroup/cpu,cpuacct/<cgroup>/cpu.cfs_quota_us" {
		t.Errorf("v1 source %q", s)
	}
}

// The cpu controller not enabled for the cgroup: no cpu.max, and cpu.stat
// with only the usage lines.
func TestCPUControllerOff(t *testing.T) {
	f := modernMachine()
	delete(f, cg2+"cpu.max")
	delete(f, cg2+"cpu.weight")
	delete(f, cg2+"cpuset.cpus.effective")
	f[cg2+"cpu.stat"] = "usage_usec 9000\nuser_usec 7000\nsystem_usec 2000\n"
	got := run(t, machine(t, f))
	for _, id := range []string{cgQuotaIDs[0], cgQuotaIDs[1], cgWeightID, "sched.cgroup.cpu.nr_periods", cgCpusetID} {
		if o := got[id]; o.Status != obs.StatusAbsent || !strings.Contains(o.Note, "not enabled") || strings.Contains(o.Note, "test.slice") {
			t.Errorf("%s: %s (%s), want absent, saying the controller is off, without the cgroup path", id, o.Status, o.Note)
		}
	}
	if o := got["sched.cgroup.cpu.usage_us"]; o.Status != obs.StatusOK {
		t.Errorf("usage: %+v", o)
	}
}
