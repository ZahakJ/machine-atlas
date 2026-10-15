// Package collect runs collectors against an environment and turns what they
// emit into checked, sorted observations with a per-collector report.
//
// Collectors never touch the machine directly: they go through Env, which is
// either the live machine (LiveEnv) or a fake machine tree on disk
// (FixtureEnv). That indirection is what lets every status path (absent,
// permission denied, unsupported, failed) be tested on any computer, and it is
// where the safety limits live: bounded reads, no shell, a fixed PATH and a
// cleared environment for commands, and an allow-list for environment
// variables.
package collect

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Limits on what one read or command may return. They protect the snapshot
// and the machine from an interface that unexpectedly produces huge output.
const (
	MaxFileBytes   = 1 << 20
	MaxOutputBytes = 1 << 20
	maxStderrBytes = 4 << 10
)

// CommandPath is the only PATH commands are looked up in, so a user's PATH
// cannot substitute a different binary for a system tool.
const CommandPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// AllowedEnv lists the only environment variables a collector may read. Atlas
// never collects process environments in general; these say what kind of
// session atlas runs in, and for the display variables only their presence is
// recorded.
var AllowedEnv = []string{"XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP", "WAYLAND_DISPLAY", "DISPLAY"}

func envAllowed(name string) bool { return slices.Contains(AllowedEnv, name) }

// ErrTooLarge reports a file or command output beyond the read limits. It
// maps to status failed: the value exists but atlas refused to read it all.
var ErrTooLarge = errors.New("output exceeds the read limit")

// ExitError reports a command that ran and exited non-zero. Both
// environments return it, so a collector handles "tool rejected the option"
// the same way against a fixture and the live machine.
type ExitError struct {
	Name   string
	Code   int
	Stderr string // capped
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s exited with status %d", e.Name, e.Code)
	}
	return fmt.Sprintf("%s exited with status %d: %s", e.Name, e.Code, e.Stderr)
}

// Uname is the part of uname(2) the collectors use.
type Uname struct {
	Machine string // "x86_64", "aarch64", "riscv64"
	Release string // "6.6.31-v8+"
}

// Env is everything a collector may read. Paths are absolute machine paths
// ("/proc/meminfo"); a FixtureEnv maps them under its root.
type Env interface {
	// ReadFile returns the file's contents, failing with ErrTooLarge beyond
	// MaxFileBytes rather than truncating silently.
	ReadFile(path string) ([]byte, error)
	// ReadDir returns the names in a directory, sorted.
	ReadDir(path string) ([]string, error)
	// Readlink returns a symbolic link's target as stored.
	Readlink(path string) (string, error)
	// Stat reports whether path exists. A missing path is (false, nil);
	// other failures, such as permission denied on a parent, are errors.
	Stat(path string) (exists bool, err error)
	// Run executes a command without a shell: name is looked up in
	// CommandPath only, the environment holds only LC_ALL=C and PATH, stdin
	// is empty and stdout is capped at MaxOutputBytes. A non-zero exit
	// returns the output so far and an *ExitError.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	// Getenv returns a variable from the atlas process environment, but only
	// for names in AllowedEnv; any other name reads as unset.
	Getenv(name string) (string, bool)
	// Uname returns the kernel's machine and release strings.
	Uname() (Uname, error)
	// PageSize returns the base page size in bytes.
	PageSize() (int, error)
	// Now is the collection time.
	Now() time.Time
	// Live is false when reading a fixture root.
	Live() bool
}
