package experiment

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// In tests the executable is the test binary, which has no "__experiment"
// verb, so TestMain plays the part of atlas's verb dispatch: one variable
// makes it an experiment child, another a child that never finishes.
const (
	childVar = "ATLAS_EXPERIMENT_TEST_CHILD"
	hangVar  = "ATLAS_EXPERIMENT_TEST_HANG"
)

func TestMain(m *testing.M) {
	if os.Getenv(hangVar) == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if os.Getenv(childVar) == "1" {
		os.Exit(ChildMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// testRunner starts the test binary as the child, with env as its entire
// environment.
func testRunner(env ...string) runner {
	r := defaultRunner()
	r.argv = func(id string) ([]string, error) { return []string{os.Args[0], id}, nil }
	r.env = env
	return r
}

func TestListContent(t *testing.T) {
	list := List()
	if len(list) != 2 || list[0].ID != idFaultCount || list[1].ID != idResidency {
		t.Fatalf("List order: %v", list)
	}
	for _, in := range list {
		if in.Title == "" || !strings.HasSuffix(in.Question, "?") {
			t.Errorf("%s: missing title or question", in.ID)
		}
		if len(in.Steps) < 3 || len(in.DoesNot) < 2 || len(in.Outcomes) < 3 {
			t.Errorf("%s: %d steps, %d does-not statements, %d outcomes", in.ID, len(in.Steps), len(in.DoesNot), len(in.Outcomes))
		}
		if in.Resources.MemoryBytes <= 0 || in.Resources.TimeBoundSeconds <= 0 {
			t.Errorf("%s: resources %+v", in.ID, in.Resources)
		}
		seen := map[string]bool{}
		for _, o := range in.Outcomes {
			sentences := strings.Count(o.Interpretation, ". ") + 1
			if o.ID == "" || o.Title == "" || seen[o.ID] || sentences < 2 || sentences > 4 {
				t.Errorf("%s/%s: empty, duplicate, or %d sentences", in.ID, o.ID, sentences)
			}
			seen[o.ID] = true
		}
		if !seen["unexpected"] {
			t.Errorf("%s: no unexpected outcome", in.ID)
		}
		if in.Available != (runtime.GOOS == "linux") || in.Available != (in.UnavailableReason == "") {
			t.Errorf("%s: available %v, reason %q on %s", in.ID, in.Available, in.UnavailableReason, runtime.GOOS)
		}
	}
	// List returns copies: a caller cannot change the catalog.
	list[0].Steps[0] = "changed"
	if List()[0].Steps[0] == "changed" {
		t.Error("List shares its slices with the catalog")
	}
}

func ms(kv ...any) []Measurement {
	var out []Measurement
	for i := 0; i < len(kv); i += 2 {
		out = append(out, Measurement{ID: kv[i].(string), Value: kv[i+1]})
	}
	return out
}

func TestClassifyFaultCount(t *testing.T) {
	const pages, size = 8192, 32 << 20
	tests := []struct {
		name           string
		aFaults, aHuge float64
		bFaults        float64
		want           string
	}{
		{"both per page", 8195, 0, 8197, "small-pages"},
		{"huge", 16, size, 8192, "huge-pages"},
		{"fragmented", 4100, 16 << 20, 8192, "partial-huge"},
		{"khugepaged after small faults", 8192, 2 << 20, 8192, "partial-huge"},
		{"few faults but huge not covering", 100, 8 << 20, 8192, "partial-huge"},
		{"B not per page", 8192, 0, 16, "unexpected"},
		{"mTHP: few faults, no AnonHugePages", 512, 0, 8192, "unexpected"},
	}
	for _, tt := range tests {
		got := classifyFaultCount(ms("region_pages", float64(pages), "region_bytes", int64(size),
			"a.first_touch.minflt", tt.aFaults, "a.first_touch.anon_huge_bytes", tt.aHuge, "b.first_touch.minflt", tt.bFaults))
		if got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
	if got := classifyFaultCount(ms("region_pages", 8192)); got != "unexpected" {
		t.Errorf("missing measurements: %s", got)
	}
}

func TestClassifyResidency(t *testing.T) {
	tests := []struct {
		fs           string
		advice, cold float64
		want         string
	}{
		{"ext2/ext3/ext4", 0, 100, "evicted-then-cached"},
		{"btrfs", 3, 98, "evicted-then-cached"},
		{"tmpfs", 100, 100, "not-evictable"},
		{"ramfs", 100, 100, "not-evictable"},
		{"xfs", 40, 100, "partially-evicted"},
		{"xfs", 100, 100, "unexpected"},
		{"fuse", 0, 10, "unexpected"},
		{"tmpfs", 0, 100, "evicted-then-cached"}, // a kernel that could drop tmpfs pages would be news, but the rule says what it saw
	}
	for _, tt := range tests {
		got := classifyResidency(ms("fs.type", tt.fs, "residency.after_advice", tt.advice, "residency.after_cold_read", tt.cold))
		if got != tt.want {
			t.Errorf("%s advice=%v cold=%v: %s, want %s", tt.fs, tt.advice, tt.cold, got, tt.want)
		}
	}
	if got := classifyResidency(ms("fs.type", "xfs")); got != "unexpected" {
		t.Errorf("missing measurements: %s", got)
	}
}

func TestDecodeResult(t *testing.T) {
	info, _ := lookup(idFaultCount)
	const at = `"2026-01-15T12:00:00.000000001Z"`
	good := `{"id":"fault-count","started_at":` + at + `,"finished_at":` + at + `,"measurements":[{"id":"x","step":"s","label":"l","value":1,"unit":"count","at":` + at + `}],"outcome":"small-pages","notes":["n"]}`
	if _, err := decodeResult([]byte(good+"\n"), info); err != nil {
		t.Fatalf("good result rejected: %v", err)
	}
	for name, bad := range map[string]string{
		"not json":        "fault",
		"trailing data":   good + good,
		"other id":        strings.Replace(good, `"id":"fault-count"`, `"id":"page-cache-residency"`, 1),
		"unknown field":   strings.Replace(good, `"notes"`, `"extra":1,"notes"`, 1),
		"unknown outcome": strings.Replace(good, "small-pages", "evicted-then-cached", 1),
		"bad time":        strings.Replace(good, `"at":`+at, `"at":"yesterday"`, 1),
		"object value":    strings.Replace(good, `"value":1`, `"value":{}`, 1),
		"no measurements": strings.Replace(good, `[{"id":"x","step":"s","label":"l","value":1,"unit":"count","at":`+at+`}]`, `[]`, 1),
	} {
		if _, err := decodeResult([]byte(bad), info); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRunRefusals(t *testing.T) {
	if _, err := Run(context.Background(), "rm -rf"); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown id: %v", err)
	}
	if runtime.GOOS != "linux" {
		if _, err := Run(context.Background(), idFaultCount); !errors.Is(err, ErrUnavailable) {
			t.Errorf("non-Linux run: %v", err)
		}
		return
	}
	running.Lock()
	_, err := Run(context.Background(), idFaultCount)
	running.Unlock()
	if !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent run: %v", err)
	}
}

// A child that never finishes is killed at the time bound.
func TestRunTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("experiments run only on Linux")
	}
	r := testRunner(hangVar + "=1")
	r.bound = func(Info) time.Duration { return 500 * time.Millisecond }
	start := time.Now()
	_, err := r.run(context.Background(), idFaultCount)
	if err == nil || !strings.Contains(err.Error(), "time bound") {
		t.Fatalf("err = %v, want a time bound error", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond+exitGrace+time.Second {
		t.Errorf("returned after %s", d)
	}
}

func TestCapped(t *testing.T) {
	c := &capped{max: 4}
	if n, err := c.Write([]byte("abc")); n != 3 || err != nil || c.over {
		t.Fatalf("n=%d err=%v over=%v", n, err, c.over)
	}
	if n, err := c.Write([]byte("defg")); n != 4 || err != nil || !c.over || c.buf.String() != "abcd" {
		t.Errorf("n=%d err=%v over=%v buf=%q", n, err, c.over, c.buf.String())
	}
}
