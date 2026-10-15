package sched

import (
	"reflect"
	"testing"
)

func TestParseLoadavg(t *testing.T) {
	l, err := parseLoadavg([]byte("0.31 0.42 0.40 1/236 9132\n"))
	if err != nil || l.avg != [3]float64{0.31, 0.42, 0.40} || l.runnable != 1 || l.total != 236 {
		t.Errorf("parseLoadavg = %+v, %v", l, err)
	}
	for _, bad := range []string{"", "0.1 0.2 0.3 1/2", "x 0.2 0.3 1/2 5", "0.1 0.2 0.3 3/2 5", "0.1 0.2 0.3 1-2 5", "-1 0 0 1/2 5"} {
		if _, err := parseLoadavg([]byte(bad)); err == nil {
			t.Errorf("parseLoadavg(%q) accepted", bad)
		}
	}
}

func TestParseProcStat(t *testing.T) {
	in := "cpu  10 1 5 100 2 0 1 3 0 0\ncpu0 5 1 2 50 1 0 1 2 0 0\ncpu1 5 0 3 50 1 0 0 1 0 0\nintr 99 1 2\nctxt 1234\nbtime 1700000000\nprocesses 77\nprocs_running 3\nprocs_blocked 1\n"
	ps, err := parseProcStat([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.times) != 3 || ps.times[0]["cpu"] != "all" || ps.times[2]["cpu"] != "cpu1" || ps.times[0]["steal"] != int64(3) {
		t.Errorf("times = %v", ps.times)
	}
	want := map[string]int64{"ctxt": 1234, "processes": 77, "procs_running": 3, "procs_blocked": 1}
	if !reflect.DeepEqual(ps.scalar, want) {
		t.Errorf("scalars = %v", ps.scalar)
	}
	// An old kernel with seven columns: the later cells are left out.
	old, err := parseProcStat([]byte("cpu  1 2 3 4 5 6 7\n"))
	if err != nil || len(old.times[0]) != 8 {
		t.Errorf("seven columns: %v, %v", old.times, err)
	}
	for _, bad := range []string{"", "cpu0 1 2 3 4\n", "cpu 1 2 3\n", "cpu 1 2 3 x\n", "cpu 1 2 3 4\ncpu 1 2 3 4\n", "cpu 1 2 3 4 5 6 7 8 9 10 11\n", "cpu 1 2 3 4\nctxt -5\n"} {
		if _, err := parseProcStat([]byte(bad)); err == nil {
			t.Errorf("parseProcStat(%q) accepted", bad)
		}
	}
}

func TestParseSchedstat(t *testing.T) {
	in := "version 15\ntimestamp 4294\ncpu0 0 0 0 0 0 0 1000 200 7\ndomain0 00000003 1 2 3\ncpu1 0 0 0 0 0 0 2000 300 9\n"
	s, err := parseSchedstat([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"cpu": int64(0), "run_ns": int64(1000), "wait_ns": int64(200), "timeslices": int64(7)},
		{"cpu": int64(1), "run_ns": int64(2000), "wait_ns": int64(300), "timeslices": int64(9)},
	}
	if s.version != 15 || !reflect.DeepEqual(s.cpus, want) {
		t.Errorf("parseSchedstat = %+v", s)
	}
	for _, bad := range []string{
		"", "version 14\ncpu0 0 0 0 0 0 0 1 2 3\n", "version 18\ncpu0 0 0 0 0 0 0 1 2 3\n",
		"cpu0 0 0 0 0 0 0 1 2 3\n", "version 15\n", "version 15\ncpu0 1 2 3\n", "version 15\ncpu0 0 0 0 0 0 0 1 -2 3\n",
	} {
		if _, err := parseSchedstat([]byte(bad)); err == nil {
			t.Errorf("parseSchedstat(%q) accepted", bad)
		}
	}
}

func TestParsePSI(t *testing.T) {
	both, err := parsePSI([]byte("some avg10=0.31 avg60=0.42 avg300=0.37 total=3246\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"))
	if err != nil || both["some"].avg[1] != 0.42 || both["some"].total != 3246 || len(both) != 2 {
		t.Errorf("parsePSI = %v, %v", both, err)
	}
	someOnly, err := parsePSI([]byte("some avg10=1.00 avg60=2.00 avg300=3.00 total=9\n"))
	if _, full := someOnly["full"]; err != nil || full {
		t.Errorf("some-only file: %v, %v", someOnly, err)
	}
	for _, bad := range []string{"", "full avg10=0 avg60=0 avg300=0 total=0\n", "some avg10=101 avg60=0 avg300=0 total=0\n", "some avg10=0 avg60=0 avg300=0\n", "half avg10=0\n"} {
		if _, err := parsePSI([]byte(bad)); err == nil {
			t.Errorf("parsePSI(%q) accepted", bad)
		}
	}
}

func TestParseSelfStat(t *testing.T) {
	// The command name contains ") (" to check fields are counted from the
	// last parenthesis.
	in := "4242 (at) (las) S 1 4242 4242 0 -1 4194560 300 0 0 0 5 2 0 0 20 5 9 0 1000 700000000 3000 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 3 0 0 0 0 0 0 0 0 0 0 0\n"
	st, err := parseSelfStat([]byte(in))
	if err != nil || st.nice != 5 || st.policy != "SCHED_BATCH" {
		t.Errorf("parseSelfStat = %+v, %v", st, err)
	}
	for _, bad := range []string{"", "4242 atlas S 1", "4242 (atlas) S 1 2 3", "4242 (atlas) S 1 4242 4242 0 -1 4194560 300 0 0 0 5 2 0 0 20 30 9 0 1000 700000000 3000 1 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n",
		"4242 (atlas) S 1 4242 4242 0 -1 4194560 300 0 0 0 5 2 0 0 20 0 9 0 1000 700000000 3000 1 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 4 0 0 0\n"} {
		if _, err := parseSelfStat([]byte(bad)); err == nil {
			t.Errorf("parseSelfStat(%q) accepted", bad)
		}
	}
}

func TestParseSelfSched(t *testing.T) {
	in := "atlas (4242, #threads: 9)\n-------------------------------------------------------------------\nse.exec_start                                :     664496758.296118\nse.vruntime                                  :       8459385.077064\nse.load.weight                               :              1048576\neffective uclamp.min                         :                    0\npolicy                                       :                    0\nse.slice                                     :              2800000\ncurrent_node=0, numa_group_id=0\n"
	kv, err := parseSelfSched([]byte(in))
	if err != nil || kv["se.load.weight"] != "1048576" || kv["se.slice"] != "2800000" || kv["effective uclamp.min"] != "0" {
		t.Errorf("parseSelfSched = %v, %v", kv, err)
	}
	if _, bad := kv["atlas (4242, #threads"]; bad {
		t.Error("the header line was parsed as a field")
	}
	if _, err := parseSelfSched([]byte("se.slice : 1\n")); err == nil {
		t.Error("a file without se.load.weight was accepted")
	}
	for in, want := range map[string]int64{"2800000": 2800000, "8459385.077064": 8459385077064} {
		if n, err := schedNanos(in); err != nil || n != want {
			t.Errorf("schedNanos(%q) = %d, %v", in, n, err)
		}
	}
	for _, bad := range []string{"1.5", "-3", "x"} {
		if _, err := schedNanos(bad); err == nil {
			t.Errorf("schedNanos(%q) accepted", bad)
		}
	}
}

func TestParseCPUMax(t *testing.T) {
	if q, p, err := parseCPUMax("max 100000"); err != nil || q != "max" || p != 100000 {
		t.Errorf("max: %v %v %v", q, p, err)
	}
	if q, p, err := parseCPUMax("150000 100000"); err != nil || q != int64(150000) || p != 100000 {
		t.Errorf("quota: %v %v %v", q, p, err)
	}
	for _, bad := range []string{"", "max", "0 100000", "max 0", "150000 x", "1 2 3"} {
		if _, _, err := parseCPUMax(bad); err == nil {
			t.Errorf("parseCPUMax(%q) accepted", bad)
		}
	}
}

func TestFindHierarchy(t *testing.T) {
	mounts, err := parseMountinfo([]byte(
		"28 20 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup2 rw,nsdelegate\n" +
			"32 20 0:25 / /sys/fs/cgroup/cpu,cpuacct rw - cgroup cgroup rw,cpu,cpuacct\n" +
			"40 20 0:33 /lxc /sys/fs/cgroup/cpuset rw - cgroup cgroup rw,cpuset\n"))
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := parseProcCgroup([]byte("3:cpu,cpuacct:/user.slice\n11:cpuset:/lxc/box\n0::/user.slice/session-1.scope\n"))
	if h, ok := findHierarchy(v1, mounts, "cpu"); !ok || !h.v1 || h.dir != "/sys/fs/cgroup/cpu,cpuacct/user.slice" {
		t.Errorf("v1 cpu: %+v %v", h, ok)
	}
	if h, ok := findHierarchy(v1, mounts, "cpuset"); !ok || h.dir != "/sys/fs/cgroup/cpuset/box" || h.point != "/sys/fs/cgroup/cpuset" {
		t.Errorf("v1 cpuset below a mount root: %+v %v", h, ok)
	}
	v2, _ := parseProcCgroup([]byte("0::/user.slice/session-1.scope\n"))
	if h, ok := findHierarchy(v2, mounts, "cpu"); !ok || h.v1 || h.dir != "/sys/fs/cgroup/user.slice/session-1.scope" {
		t.Errorf("v2: %+v %v", h, ok)
	}
	hidden, _ := parseProcCgroup([]byte("11:cpuset:/elsewhere\n"))
	if h, ok := findHierarchy(hidden, mounts, "cpuset"); !ok || h.dir != "" || h.mountNote == "" {
		t.Errorf("cgroup outside the mount: %+v %v", h, ok)
	}
	if _, ok := findHierarchy(hidden, mounts, "cpu"); ok {
		t.Error("found a cpu hierarchy where there is none")
	}
}

func TestFormatCPUList(t *testing.T) {
	for want, in := range map[string][]int{"0-3": {0, 1, 2, 3}, "0,2,4-6": {0, 2, 4, 5, 6}, "": {}, "7": {7}} {
		if got := formatCPUList(in); got != want {
			t.Errorf("formatCPUList(%v) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeCPUWords("0 1 2 3"); got != "0-3" {
		t.Errorf("normalizeCPUWords = %q", got)
	}
}
