package main

import (
	"fmt"
	"os"

	"github.com/ZahakJ/machine-atlas/internal/curriculum"
)

func cmdCurriculum(args []string) int {
	if len(args) == 0 || args[0] != "lint" {
		fmt.Fprintln(os.Stderr, "Usage: atlas curriculum lint [--allow-missing-lessons]")
		return 2
	}
	fs := newFlags("curriculum lint", "atlas curriculum lint [--allow-missing-lessons]")
	allowMissing := fs.Bool("allow-missing-lessons", false,
		"report references to lessons not written yet as warnings")
	pos, code := parseArgs(fs, args[1:])
	if code >= 0 {
		return code
	}
	if len(pos) > 0 {
		return usageError(fs, "unexpected argument %q", pos[0])
	}
	problems := curriculum.Lint(curriculum.Embedded(), curriculum.LintOptions{AllowMissingLessons: *allowMissing})
	errs := 0
	for _, p := range problems {
		fmt.Println(p)
		if !p.Warning {
			errs++
		}
	}
	fmt.Printf("%d errors, %d warnings\n", errs, len(problems)-errs)
	if curriculum.Failed(problems) {
		return 1
	}
	return 0
}
