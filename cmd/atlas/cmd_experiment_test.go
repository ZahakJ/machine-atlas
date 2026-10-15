package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/experiment"
)

// fakeCLI returns an experimentCLI whose runner records whether it was
// called and returns a canned result, so nothing is run.
func fakeCLI(input string, tty bool, called *bool) (experimentCLI, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return experimentCLI{
		in: strings.NewReader(input), out: out, errOut: errOut, tty: tty,
		run: func(id string) (experiment.Result, error) {
			*called = true
			return experiment.Result{
				ID: id, StartedAt: "2026-01-15T12:00:00.000000000Z", FinishedAt: "2026-01-15T12:00:00.100000000Z",
				Measurements: []experiment.Measurement{{ID: "region_bytes", Step: "setup", Label: "Size of each region", Value: float64(32 << 20), Unit: "bytes", At: "2026-01-15T12:00:00.000000001Z"}},
				Outcome:      "small-pages", Notes: []string{"a note"},
			}, nil
		},
	}, out, errOut
}

func TestExperimentConfirmation(t *testing.T) {
	if !experiment.List()[0].Available {
		t.Skip("experiments are unavailable on this platform")
	}
	tests := []struct {
		name    string
		args    []string
		input   string
		tty     bool
		wantRun bool
		code    int
	}{
		{"not a terminal, no --yes", []string{"run", "fault-count"}, "y\n", false, false, 1},
		{"answered no", []string{"run", "fault-count"}, "n\n", true, false, 1},
		{"no answer", []string{"run", "fault-count"}, "", true, false, 1},
		{"answered yes", []string{"run", "fault-count"}, "yes\n", true, true, 0},
		{"--yes without a terminal", []string{"run", "fault-count", "--yes"}, "", false, true, 0},
		{"unknown id", []string{"run", "nope", "--yes"}, "", false, false, 2},
		{"no id", []string{"run", "--yes"}, "", false, false, 2},
	}
	for _, tt := range tests {
		called := false
		c, out, _ := fakeCLI(tt.input, tt.tty, &called)
		if code := c.main(tt.args); code != tt.code || called != tt.wantRun {
			t.Errorf("%s: exit %d, ran %v; want %d, %v", tt.name, code, called, tt.code, tt.wantRun)
		}
		if tt.wantRun {
			for _, want := range []string{"What it does not do:", "Outcome: Both regions faulted about once per page", "Size of each region", "32 MiB (33554432 B)", "Other possible outcomes:", "huge-pages"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s: output lacks %q", tt.name, want)
				}
			}
		}
	}
}

func TestExperimentJSON(t *testing.T) {
	if !experiment.List()[0].Available {
		t.Skip("experiments are unavailable on this platform")
	}
	called := false
	c, out, errOut := fakeCLI("", false, &called)
	if code := c.main([]string{"run", "--json", "fault-count", "--yes"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var r experiment.Result
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Outcome != "small-pages" {
		t.Errorf("stdout is not the result: %v %q", err, out)
	}
	if !strings.Contains(errOut.String(), "What it does:") {
		t.Error("the description should go to stderr with --json")
	}
}

func TestExperimentList(t *testing.T) {
	called := false
	c, out, _ := fakeCLI("", false, &called)
	if code := c.main([]string{"list"}); code != 0 || !strings.Contains(out.String(), "page-cache-residency") || called {
		t.Errorf("exit %d, output %q", code, out)
	}
	if code := c.main(nil); code != 2 {
		t.Errorf("no subcommand: exit %d", code)
	}
}
