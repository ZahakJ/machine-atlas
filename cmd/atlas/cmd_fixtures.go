package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

func cmdFixtures(args []string) int {
	if len(args) != 1 || args[0] != "list" {
		fmt.Fprintln(os.Stderr, "Usage: atlas fixtures list")
		return 2
	}
	names, err := fs.Glob(fixtures.FS, "snapshots/*.json")
	if err != nil {
		return fail("fixtures", err)
	}
	if len(names) == 0 {
		fmt.Println("no demonstration snapshots are embedded in this build")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCAPTURED\tOBSERVATIONS\tSTATE")
	for _, name := range names {
		short := strings.TrimSuffix(path.Base(name), ".json")
		f, err := fixtures.FS.Open(name)
		if err != nil {
			return fail("fixtures", err)
		}
		s, err := snapshot.Load(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(tw, "%s\t-\t-\tinvalid: %v\n", short, err)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\tvalid\n", short, s.CreatedAt, len(s.Observations))
	}
	if err := tw.Flush(); err != nil {
		return fail("fixtures", err)
	}
	return 0
}
