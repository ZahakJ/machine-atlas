//go:build linux

package experiment

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

func available() (bool, string) { return true, "" }

// IsTerminal reports whether f is a terminal, where a person can answer a
// confirmation prompt. A character device alone is not enough: /dev/null
// is one.
func IsTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}

// asHeadroom is how far above its size at start the child's address space
// may grow: the experiments map at most about 70 MiB and the Go runtime
// may add a heap arena, so this is ample, yet a runaway bug stops here and
// not in the machine's memory.
const asHeadroom = 512 * mib

// pendingRemove is the experiment's temporary file, if one exists, for the
// child's own deadline to delete before it exits.
var pendingRemove atomic.Pointer[string]

// ChildMain is the entry point of "atlas __experiment <id>"; it returns the
// exit code. It runs the one experiment named, prints its Result as one
// JSON line on stdout, and exits.
func ChildMain(args []string) int {
	var info Info
	ok := len(args) == 1
	if ok {
		info, ok = lookup(args[0])
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: atlas __experiment <id> (started by atlas experiment run; not for direct use)")
		return 2
	}
	// Exit by ourselves a second before the parent would kill us, so the
	// temporary file is not left behind.
	bound := time.Duration(info.Resources.TimeBoundSeconds) * time.Second
	time.AfterFunc(max(bound-time.Second, time.Second), func() {
		if p := pendingRemove.Load(); p != nil {
			os.Remove(*p)
		}
		fmt.Fprintln(os.Stderr, "atlas __experiment: time bound reached")
		os.Exit(3)
	})

	// The fault counters are per thread, so the experiment stays on one;
	// no garbage collection runs to fault pages of its own.
	runtime.LockOSThread()
	debug.SetGCPercent(-1)

	res, err := runChild(info)
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(res)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas __experiment %s: %v\n", info.ID, err)
		return 1
	}
	return 0
}

func runChild(info Info) (Result, error) {
	rec := &recorder{ms: make([]Measurement, 0, 64)}
	started := time.Now()
	var notes []string
	var err error
	switch info.ID {
	case idFaultCount:
		if err = limitSelf(0); err == nil {
			notes, err = faultCount(rec)
		}
	case idResidency:
		if err = limitSelf(2 * fileBytes); err == nil {
			notes, err = pageCacheResidency(rec)
		}
	}
	if err != nil {
		return Result{}, err
	}
	return Result{
		ID:           info.ID,
		StartedAt:    stamp(started),
		FinishedAt:   stamp(time.Now()),
		Measurements: rec.ms,
		Outcome:      classify(info.ID, rec.ms),
		Notes:        append(notes, ruleFor(info.ID)),
	}, nil
}

// limitSelf lowers the child's own limits so that a bug cannot make it a
// hazard: no core dumps, files no larger than fsize, and an address space
// a little above what it already has. Limits are only ever lowered.
func limitSelf(fsize uint64) error {
	vm, err := vmSize()
	if err != nil {
		return err
	}
	for _, l := range []struct {
		res int
		max uint64
	}{
		{unix.RLIMIT_CORE, 0},
		{unix.RLIMIT_FSIZE, fsize},
		{unix.RLIMIT_AS, vm + asHeadroom},
	} {
		var cur unix.Rlimit
		if err := unix.Getrlimit(l.res, &cur); err != nil {
			return fmt.Errorf("getrlimit: %w", err)
		}
		lim := unix.Rlimit{Cur: min(cur.Cur, l.max), Max: min(cur.Max, l.max)}
		if err := unix.Setrlimit(l.res, &lim); err != nil {
			return fmt.Errorf("setrlimit: %w", err)
		}
	}
	return nil
}

// vmSize reads the process's current virtual size in bytes.
func vmSize() (uint64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, noPath(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if v, ok := bytes.CutPrefix(sc.Bytes(), []byte("VmSize:")); ok {
			kb, err := strconv.ParseUint(string(bytes.TrimSpace(bytes.TrimSuffix(bytes.TrimSpace(v), []byte("kB")))), 10, 64)
			return kb << 10, err
		}
	}
	return 0, errors.New("no VmSize in /proc/self/status")
}

// recorder collects measurements, each stamped when it is added. Adding
// allocates, so it is never called between two readings of a counter.
type recorder struct{ ms []Measurement }

func (r *recorder) add(id, step, label string, value any, unit string) {
	r.ms = append(r.ms, Measurement{ID: id, Step: step, Label: label, Value: value, Unit: unit, At: stamp(time.Now())})
}

// stamp formats t as RFC 3339 UTC with all nine fractional digits, so
// timestamps line up in a table.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

// noPath drops the path from a file error: the child's messages reach the
// user interface, and the paths it uses contain the home directory.
func noPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}
