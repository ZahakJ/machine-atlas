package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

func cmdLesson(args []string) int {
	fs := newFlags("lesson", "atlas lesson ID [--snapshot FILE | --fixture NAME] [--json]")
	file := fs.String("snapshot", "", "bind to the snapshot `FILE` instead of this machine")
	fixture := fs.String("fixture", "", "bind to the embedded demonstration snapshot `NAME` (see atlas fixtures list)")
	asJSON := fs.Bool("json", false, "print the bound lesson as JSON, the shape the web interface receives")
	pos, code := parseArgs(fs, args)
	if code >= 0 {
		return code
	}
	if len(pos) != 1 {
		return usageError(fs, "want exactly one lesson ID")
	}
	if *file != "" && *fixture != "" {
		return usageError(fs, "--snapshot and --fixture are alternatives")
	}
	cur, err := curriculum.Default()
	if err != nil {
		return fail("lesson", err)
	}
	lesson, ok := cur.Lesson(pos[0])
	if !ok {
		return fail("lesson", fmt.Errorf("no lesson %q", pos[0]))
	}
	s, err := chooseSnapshot(*file, *fixture)
	if err != nil {
		return fail("lesson", err)
	}
	bound := curriculum.Bind(lesson, s, derive.All(s))
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(bound)
	} else {
		_, err = fmt.Print(curriculum.RenderText(bound))
	}
	if err != nil {
		return fail("lesson", err)
	}
	return 0
}

// chooseSnapshot returns the snapshot a command works on: a validated file,
// an embedded fixture, or (with neither) a fresh collection from this
// machine, including the probe.
func chooseSnapshot(file, fixture string) (*snapshot.Snapshot, error) {
	switch {
	case file != "":
		return loadFile(file)
	case fixture != "":
		return loadFixture(fixture)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return collectSnapshot(ctx, "", time.Time{}, false)
}

// loadFixture loads and validates fixtures/snapshots/NAME.json from the
// binary. A name is a plain file name: it cannot select another directory.
func loadFixture(name string) (*snapshot.Snapshot, error) {
	p := "snapshots/" + name + ".json"
	if strings.ContainsAny(name, "/\\") || !fs.ValidPath(p) {
		return nil, fmt.Errorf("%q is not a fixture name (see atlas fixtures list)", name)
	}
	f, err := fixtures.FS.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no fixture %q (see atlas fixtures list)", name)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return snapshot.Load(f)
}
