package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

func TestParseArgsInterleaved(t *testing.T) {
	flags := flag.NewFlagSet("t", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	out := flags.String("o", "", "")
	policy := flags.String("policy", "default", "")
	pos, code := parseArgs(flags, []string{"in.json", "-o", "out.json", "--policy", "strict", "extra"})
	if code != -1 || *out != "out.json" || *policy != "strict" || !reflect.DeepEqual(pos, []string{"in.json", "extra"}) {
		t.Errorf("pos=%v code=%d o=%q policy=%q", pos, code, *out, *policy)
	}
	if _, code := parseArgs(flags, []string{"-h"}); code != 0 {
		t.Errorf("-h exit code %d, want 0", code)
	}
	if _, code := parseArgs(flags, []string{"--bogus"}); code != 2 {
		t.Errorf("unknown flag exit code %d, want 2", code)
	}
}

func TestDispatch(t *testing.T) {
	stderr := os.Stderr
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() { os.Stderr = stderr }()
	if code := run(nil); code != 2 {
		t.Errorf("no arguments: exit %d", code)
	}
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Errorf("unknown verb: exit %d", code)
	}
	seen := map[string]bool{}
	for _, v := range verbs {
		if seen[v.name] || v.run == nil || (!v.hidden && v.summary == "") {
			t.Errorf("verb %q is duplicated or incomplete", v.name)
		}
		seen[v.name] = true
	}
}

const (
	rootsDir   = "../../fixtures/roots"
	goldensDir = "../../fixtures/snapshots"
)

// Golden snapshots must be exactly what "atlas collect --root" produces now
// (with default redaction); "make fixtures" regenerates them.
func TestGoldenSnapshots(t *testing.T) {
	roots, err := os.ReadDir(rootsDir)
	if os.IsNotExist(err) {
		t.Skip("no fixture roots yet")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roots {
		if !r.IsDir() {
			continue
		}
		t.Run(r.Name(), func(t *testing.T) {
			s, err := collectSnapshot(context.Background(), filepath.Join(rootsDir, r.Name()), time.Time{}, false)
			if err != nil {
				t.Fatal(err)
			}
			red, _, err := snapshot.Redact(s, snapshot.PolicyDefault)
			if err != nil {
				t.Fatal(err)
			}
			if errs := snapshot.Validate(red); len(errs) > 0 {
				t.Fatalf("fixture produces an invalid snapshot: %v", errs)
			}
			got, err := snapshot.Encode(red)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(goldensDir, r.Name()+".json"))
			if err != nil {
				t.Fatalf("%v; run \"make fixtures\"", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden is out of date; run \"make fixtures\" and review the diff")
			}
		})
	}
}

// Every embedded snapshot must pass validation, as imported files must.
func TestEmbeddedSnapshotsValidate(t *testing.T) {
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		f, err := fixtures.FS.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := snapshot.Load(f); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		f.Close()
	}
}

// Committed snapshots, live-sample.json above all, must already be
// redacted: applying the default policy again finds nothing to replace.
// This does not prove a file is free of identifying data: redaction knows
// only its patterns and catalog classes.
func TestCommittedSnapshotsAreRedacted(t *testing.T) {
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		f, err := fixtures.FS.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		s, err := snapshot.Load(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !s.Redaction.Applied || s.Redaction.Policy == snapshot.PolicyNone {
			t.Errorf("%s: redaction %+v", name, s.Redaction)
		}
		changes, err := snapshot.Preview(s, snapshot.PolicyDefault)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("%s: %s %s still holds %s data: %q", name, c.ID, c.Field, c.Class, c.Before)
		}
	}
}
