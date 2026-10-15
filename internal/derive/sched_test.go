package derive

import (
	"maps"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// schedMachine is a four-CPU machine with a cgroup quota: every input of
// the scheduler facts present.
func schedMachine() map[string]any {
	return map[string]any{
		"kernel.release":              "6.12.10-arch1-1",
		"kernel.uptime_seconds":       1000.0,
		"cpu.logical_count":           4,
		"cpu.hybrid.performance_cpus": missing(obs.StatusAbsent),
		"cpu.topology": []map[string]any{
			{"cpu": 0, "capacity": 1024}, {"cpu": 1, "capacity": 1024}, {"cpu": 2, "capacity": 1024}, {"cpu": 3, "capacity": 1024},
		},
		"cpu.isolated":                   "2-3",
		"cpu.freq.policies":              []map[string]any{{"policy": 0, "driver": "intel_pstate", "governor": "powersave"}, {"policy": 1, "driver": "intel_pstate", "governor": "performance"}},
		"cpu.freq.boost":                 missing(obs.StatusAbsent),
		"cpu.freq.intel_pstate.no_turbo": false,
		"sched.loadavg.avg1":             2.0,
		"sched.stat.cpu_times": []map[string]any{
			{"cpu": "all", "user": 300, "nice": 0, "system": 100, "idle": 550, "iowait": 50, "irq": 0, "softirq": 0, "steal": 0, "guest": 7, "guest_nice": 0},
		},
		"sched.stat.ctxt":                50000,
		"sched.schedstat.cpus":           []map[string]any{{"cpu": 0, "run_ns": 3000000, "wait_ns": 1000000, "timeslices": 1000}, {"cpu": 1, "run_ns": 1000000, "wait_ns": 0, "timeslices": 1000}},
		"sched.self.slice_us":            2800,
		"sched.self.load_weight":         1048576,
		"sched.ext.state":                "disabled",
		"sched.self.policy":              "SCHED_OTHER",
		"sched.self.nice":                5,
		"sched.self.cpus_allowed_list":   "0-1,3",
		"sched.cgroup.cpu.max_quota_us":  150000,
		"sched.cgroup.cpu.max_period_us": 100000,
		"sched.cgroup.cpu.nr_periods":    40,
		"sched.cgroup.cpu.nr_throttled":  10,
	}
}

func TestSchedFacts(t *testing.T) {
	facts := byID(All(snap(t, schedMachine())))
	want := map[string]any{
		"derived.cpu.hybrid":                     false,
		"derived.cpu.isolated_count":             int64(2),
		"derived.cpu.freq.available":             true,
		"derived.cpu.freq.driver":                "intel_pstate",
		"derived.cpu.freq.governor":              "mixed",
		"derived.cpu.freq.boost_enabled":         true,
		"derived.sched.load_per_cpu":             0.5,
		"derived.sched.busy_since_boot_fraction": 0.4,
		"derived.sched.steal_fraction":           0.0,
		"derived.sched.ctxt_per_second":          50.0,
		"derived.sched.wait_fraction":            0.2,
		"derived.sched.avg_timeslice_us":         2.0,
		"derived.sched.fair_scheduler":           "eevdf",
		"derived.sched.ext_enabled":              false,
		"derived.sched.self_weight":              int64(335),
		"derived.sched.self_allowed_count":       int64(3),
		"derived.cgroup.cpu_limited":             true,
		"derived.cgroup.cpu_quota_cpus":          1.5,
		"derived.cgroup.cpu_throttled_fraction":  0.25,
	}
	for id, v := range want {
		f := facts[id]
		if f.Status != StatusOK || f.Value != v {
			t.Errorf("%s = %#v (%s: %s), want %#v", id, f.Value, f.Status, f.Reason, v)
		}
	}
}

func TestWeightTable(t *testing.T) {
	if SchedPrioToWeight[20] != 1024 || SchedPrioToWeight[0] != 88761 || SchedPrioToWeight[39] != 15 {
		t.Error("sched_prio_to_weight endpoints are wrong")
	}
	for i := 1; i < 40; i++ {
		r := float64(SchedPrioToWeight[i-1]) / float64(SchedPrioToWeight[i])
		if r < 1.2 || r > 1.3 {
			t.Errorf("nice %d to %d: ratio %.3f, want about 1.25", i-21, i-20, r)
		}
	}
}

func TestSchedFactBranches(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]any
		want   map[string]any    // fact → value (ok)
		unk    map[string]string // fact → reason substring (unknown)
	}{
		{"hybrid x86", map[string]any{"cpu.hybrid.performance_cpus": "0-7"}, map[string]any{"derived.cpu.hybrid": true}, nil},
		{"big.LITTLE capacities", map[string]any{"cpu.topology": []map[string]any{{"cpu": 0, "capacity": 446}, {"cpu": 1, "capacity": 1024}}},
			map[string]any{"derived.cpu.hybrid": true}, nil},
		{"topology denied", map[string]any{"cpu.topology": missing(obs.StatusPermissionDenied)}, nil,
			map[string]string{"derived.cpu.hybrid": "cpu.topology is permission denied"}},
		{"no cpufreq (a VM)", map[string]any{"cpu.freq.policies": missing(obs.StatusAbsent), "cpu.freq.intel_pstate.no_turbo": missing(obs.StatusAbsent)},
			map[string]any{"derived.cpu.freq.available": false},
			map[string]string{"derived.cpu.freq.driver": "absent", "derived.cpu.freq.boost_enabled": "cpu.freq.boost is absent"}},
		{"cpufreq denied", map[string]any{"cpu.freq.policies": missing(obs.StatusPermissionDenied)}, nil,
			map[string]string{"derived.cpu.freq.available": "permission denied"}},
		{"boost file", map[string]any{"cpu.freq.boost": false}, map[string]any{"derived.cpu.freq.boost_enabled": false}, nil},
		{"CFS kernel", map[string]any{"sched.self.slice_us": missing(obs.StatusAbsent), "kernel.release": "4.19.113-300.el7.x86_64"},
			map[string]any{"derived.sched.fair_scheduler": "cfs"}, nil},
		{"release unreadable", map[string]any{"sched.self.slice_us": missing(obs.StatusAbsent), "kernel.release": missing(obs.StatusFailed)}, nil,
			map[string]string{"derived.sched.fair_scheduler": "kernel.release is failed"}},
		{"no sched file, new release", map[string]any{"sched.self.slice_us": missing(obs.StatusAbsent), "sched.self.load_weight": missing(obs.StatusAbsent), "kernel.release": "6.6.51+rpt-rpi-v8"},
			map[string]any{"derived.sched.fair_scheduler": "eevdf"}, nil},
		{"no sched file, old release", map[string]any{"sched.self.slice_us": missing(obs.StatusAbsent), "sched.self.load_weight": missing(obs.StatusAbsent), "kernel.release": "6.5.0"},
			map[string]any{"derived.sched.fair_scheduler": "cfs"}, nil},
		{"no sched_ext", map[string]any{"sched.ext.state": missing(obs.StatusAbsent)}, map[string]any{"derived.sched.ext_enabled": false}, nil},
		{"BPF scheduler", map[string]any{"sched.ext.state": "enabled"}, map[string]any{"derived.sched.ext_enabled": true}, nil},
		{"real-time", map[string]any{"sched.self.policy": "SCHED_FIFO"}, nil, map[string]string{"derived.sched.self_weight": "not scheduled by weight"}},
		{"idle policy", map[string]any{"sched.self.policy": "SCHED_IDLE"}, map[string]any{"derived.sched.self_weight": int64(3)}, nil},
		{"no quota", map[string]any{"sched.cgroup.cpu.max_quota_us": "max"}, map[string]any{"derived.cgroup.cpu_limited": false},
			map[string]string{"derived.cgroup.cpu_quota_cpus": "no CPU quota"}},
		{"cpu controller off", map[string]any{"sched.cgroup.cpu.max_quota_us": missing(obs.StatusAbsent), "sched.cgroup.cpu.nr_periods": missing(obs.StatusAbsent)},
			map[string]any{"derived.cgroup.cpu_limited": false},
			map[string]string{"derived.cgroup.cpu_quota_cpus": "absent", "derived.cgroup.cpu_throttled_fraction": "absent"}},
		{"no period yet", map[string]any{"sched.cgroup.cpu.nr_periods": 0, "sched.cgroup.cpu.nr_throttled": 0}, nil,
			map[string]string{"derived.cgroup.cpu_throttled_fraction": "no quota period"}},
		{"VM with steal", map[string]any{"sched.stat.cpu_times": []map[string]any{{"cpu": "all", "user": 400, "nice": 0, "system": 100, "idle": 400, "iowait": 0, "irq": 0, "softirq": 0, "steal": 100}}},
			map[string]any{"derived.sched.steal_fraction": 0.1, "derived.sched.busy_since_boot_fraction": 0.6}, nil},
		{"old /proc/stat", map[string]any{"sched.stat.cpu_times": []map[string]any{{"cpu": "all", "user": 400, "nice": 0, "system": 100, "idle": 500}}},
			map[string]any{"derived.sched.busy_since_boot_fraction": 0.5},
			map[string]string{"derived.sched.steal_fraction": "no steal column"}},
		{"schedstat absent", map[string]any{"sched.schedstat.cpus": missing(obs.StatusAbsent)}, nil,
			map[string]string{"derived.sched.wait_fraction": "absent", "derived.sched.avg_timeslice_us": "absent"}},
	}
	for _, tt := range tests {
		vals := schedMachine()
		maps.Copy(vals, tt.change)
		facts := byID(All(snap(t, vals)))
		for id, v := range tt.want {
			if f := facts[id]; f.Status != StatusOK || f.Value != v {
				t.Errorf("%s: %s = %#v (%s: %s), want %#v", tt.name, id, f.Value, f.Status, f.Reason, v)
			}
		}
		for id, sub := range tt.unk {
			if f := facts[id]; f.Status != StatusUnknown || !strings.Contains(f.Reason, sub) {
				t.Errorf("%s: %s = %+v, want unknown with %q", tt.name, id, f, sub)
			}
		}
	}
}
