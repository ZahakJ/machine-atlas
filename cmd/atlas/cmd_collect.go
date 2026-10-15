package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/collect/all"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

func cmdCollect(args []string) int {
	fs := newFlags("collect", "atlas collect [-o FILE] [--no-probe] [--root DIR] [--now RFC3339] [--redact default|strict|none]")
	out := fs.String("o", "", "write the snapshot to `FILE` instead of standard output")
	noProbe := fs.Bool("no-probe", false, "do not start the probe child process (its ids become not_collected)")
	root := fs.String("root", "", "collect from the fixture root `DIR` instead of this machine")
	now := fs.String("now", "", "with --root: record this collection `time` (RFC 3339) instead of the fixture's")
	policy := fs.String("redact", snapshot.PolicyDefault, "redaction `policy`: default, strict or none")
	pos, code := parseArgs(fs, args)
	if code >= 0 {
		return code
	}
	if len(pos) > 0 {
		return usageError(fs, "unexpected argument %q", pos[0])
	}
	var at time.Time
	if *now != "" {
		// A live collection records when each value was really read; a
		// chosen time exists only to make fixture output reproducible.
		if *root == "" {
			return usageError(fs, "--now needs --root: a live collection always records the time it actually ran")
		}
		var err error
		if at, err = time.Parse(time.RFC3339, *now); err != nil {
			return usageError(fs, "--now: %v", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s, err := collectSnapshot(ctx, *root, at, *noProbe)
	if err != nil {
		return fail("collect", err)
	}
	red, changes, err := snapshot.Redact(s, *policy)
	if err != nil {
		return usageError(fs, "%v", err)
	}
	printRedactionSummary(os.Stderr, *policy, changes)
	data, err := snapshot.Encode(red)
	if err != nil {
		return fail("collect", err)
	}
	if *out == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = writeFile(*out, data)
	}
	if err != nil {
		return fail("collect", err)
	}
	return 0
}

// collectSnapshot runs every collector against the live machine, or against
// the fixture root when root is set, and returns the unredacted snapshot. A
// non-zero at overrides a fixture's collection time; a live collection
// always uses the real clock. The golden fixture test calls it too, so
// goldens are produced exactly the way "atlas collect" does.
func collectSnapshot(ctx context.Context, root string, at time.Time, noProbe bool) (*snapshot.Snapshot, error) {
	var env collect.Env = collect.LiveEnv{}
	if root != "" {
		f, err := collect.OpenFixture(root)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if !at.IsZero() {
			f.Clock = func() time.Time { return at }
		}
		env = f
	} else if !at.IsZero() {
		return nil, errors.New("a collection time can be set only for a fixture root")
	}
	rep, observations := collect.Run(ctx, env, all.All(all.Options{NoProbe: noProbe}))
	return snapshot.Build(rep, observations), nil
}

// printRedactionSummary tells the user what redaction replaced before the
// file is shared. Values are quoted so control characters in collected
// strings cannot act on the terminal.
func printRedactionSummary(w io.Writer, policy string, changes []snapshot.Change) {
	if policy == snapshot.PolicyNone {
		fmt.Fprintln(w, "warning: the snapshot is not redacted; it may contain this machine's hostname, user names, paths and addresses")
		return
	}
	if len(changes) == 0 {
		fmt.Fprintf(w, "redaction (%s): nothing to replace\n", policy)
		return
	}
	count := map[string]int{}
	for _, c := range changes {
		count[c.Class]++
	}
	var parts []string
	for _, class := range slices.Sorted(maps.Keys(count)) {
		parts = append(parts, fmt.Sprintf("%s %d", class, count[class]))
	}
	fmt.Fprintf(w, "redaction (%s): %d replacements (%s)\n", policy, len(changes), strings.Join(parts, ", "))
	printChanges(w, changes)
}

// printChanges lists changes one per line: where, class, before → after.
func printChanges(w io.Writer, changes []snapshot.Change) {
	for _, c := range changes {
		where := c.ID
		if where == "" {
			where = "(snapshot)"
		}
		fmt.Fprintf(w, "  %s %s [%s]: %q → %q\n", where, c.Field, c.Class, c.Before, c.After)
	}
}

// writeFile writes data to path through a temporary file and a rename, so
// an interrupted run never leaves a truncated snapshot. CreateTemp makes the
// file readable by its owner only, which suits a description of a machine.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".atlas-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
