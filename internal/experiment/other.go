//go:build !linux

package experiment

import (
	"fmt"
	"os"
	"runtime"
)

func available() (bool, string) {
	return false, "the experiments use Linux interfaces (madvise, RUSAGE_THREAD, /proc/self/smaps, mincore, posix_fadvise); this build is for " + runtime.GOOS
}

// IsTerminal reports whether f is a terminal. Outside Linux nothing can
// be run, so nothing needs confirming.
func IsTerminal(*os.File) bool { return false }

// ChildMain is the entry point of "atlas __experiment <id>"; outside Linux
// there is nothing to run.
func ChildMain([]string) int {
	fmt.Fprintln(os.Stderr, "atlas __experiment: experiments run only on Linux")
	return 2
}
