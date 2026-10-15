package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Verb is the hidden atlas verb that runs ChildMain.
const Verb = "__experiment"

const (
	// maxResultBytes caps what the parent reads from a child's stdout. A
	// result is a few kilobytes; anything near this is a bug.
	maxResultBytes = 256 << 10
	maxStderrBytes = 4 << 10
	// maxMeasurements and maxStringBytes bound a result the parent accepts.
	maxMeasurements = 200
	maxStringBytes  = 4096
	// exitGrace is how long the parent waits for the child's pipes after
	// killing it.
	exitGrace = time.Second
)

// running serialises experiments: two at once would measure each other.
var running sync.Mutex

// runner starts children. Tests replace argv and env, because there the
// executable is the test binary, which becomes a child through TestMain
// and an environment variable instead of a verb; and bound, to test the
// timeout with a child that hangs.
type runner struct {
	argv  func(id string) ([]string, error)
	env   []string
	bound func(Info) time.Duration
}

func defaultRunner() runner {
	return runner{
		argv: func(id string) ([]string, error) {
			exe, err := os.Executable()
			return []string{exe, Verb, id}, err
		},
		env:   childEnv(),
		bound: func(in Info) time.Duration { return time.Duration(in.Resources.TimeBoundSeconds) * time.Second },
	}
}

// childEnv is the child's entire environment: only what it needs to find
// the cache directory.
func childEnv() []string {
	var env []string
	for _, name := range []string{"HOME", "XDG_CACHE_HOME"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

func (r runner) run(ctx context.Context, id string) (Result, error) {
	info, ok := lookup(id)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	if !info.Available {
		return Result{}, fmt.Errorf("%w: %s", ErrUnavailable, info.UnavailableReason)
	}
	if !running.TryLock() {
		return Result{}, ErrBusy
	}
	defer running.Unlock()

	argv, err := r.argv(id)
	if err != nil {
		return Result{}, fmt.Errorf("cannot find the atlas executable: %w", err)
	}
	bound := r.bound(info)
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	// CommandContext kills the child (SIGKILL to its PID) when ctx ends.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = r.env
	cmd.Dir = "/"
	cmd.WaitDelay = exitGrace
	stdout := &capped{max: maxResultBytes}
	stderr := &capped{max: maxStderrBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	err = cmd.Run()
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Result{}, fmt.Errorf("experiment %s exceeded its time bound of %s and was killed", id, bound)
	case err != nil && ctx.Err() != nil:
		return Result{}, fmt.Errorf("experiment %s was stopped: %w", id, ctx.Err())
	case err != nil:
		msg := fmt.Sprintf("experiment %s failed: %v", id, err)
		if first, _, _ := strings.Cut(strings.TrimSpace(stderr.buf.String()), "\n"); first != "" {
			msg += ": " + first
		}
		return Result{}, errors.New(msg)
	case stdout.over:
		return Result{}, fmt.Errorf("experiment %s printed more than %d bytes", id, maxResultBytes)
	}
	return decodeResult(stdout.buf.Bytes(), info)
}

// decodeResult parses and checks a child's output: exactly one JSON Result
// for this experiment, with known fields, a known outcome, and well-formed
// measurements.
func decodeResult(data []byte, info Info) (Result, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r Result
	if err := dec.Decode(&r); err != nil {
		return Result{}, fmt.Errorf("experiment %s: invalid result: %w", info.ID, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Result{}, fmt.Errorf("experiment %s: invalid result: data after the result", info.ID)
	}
	if err := checkResult(r, info); err != nil {
		return Result{}, fmt.Errorf("experiment %s: invalid result: %w", info.ID, err)
	}
	return r, nil
}

func checkResult(r Result, info Info) error {
	if r.ID != info.ID {
		return fmt.Errorf("id %q", r.ID)
	}
	if !outcomeIDs(info)[r.Outcome] {
		return fmt.Errorf("unknown outcome %q", r.Outcome)
	}
	start, err1 := time.Parse(time.RFC3339Nano, r.StartedAt)
	end, err2 := time.Parse(time.RFC3339Nano, r.FinishedAt)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if end.Before(start) {
		return errors.New("finished before it started")
	}
	if len(r.Measurements) == 0 || len(r.Measurements) > maxMeasurements {
		return fmt.Errorf("%d measurements", len(r.Measurements))
	}
	seen := map[string]bool{}
	for _, m := range r.Measurements {
		if m.ID == "" || m.Step == "" || m.Label == "" || seen[m.ID] {
			return fmt.Errorf("measurement %q: empty or duplicate id, step or label", m.ID)
		}
		seen[m.ID] = true
		if _, err := time.Parse(time.RFC3339Nano, m.At); err != nil {
			return fmt.Errorf("measurement %s: %w", m.ID, err)
		}
		switch v := m.Value.(type) {
		case float64, bool:
		case string:
			if len(v) > maxStringBytes {
				return fmt.Errorf("measurement %s: string too long", m.ID)
			}
		default:
			return fmt.Errorf("measurement %s: value of type %T", m.ID, m.Value)
		}
	}
	for _, n := range r.Notes {
		if len(n) > maxStringBytes {
			return errors.New("note too long")
		}
	}
	return nil
}

// capped keeps the first max bytes written and discards the rest without
// failing, so a noisy child is never stopped by a broken pipe; over says
// whether anything was discarded.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.max - c.buf.Len(); n > room {
		c.over = true
		p = p[:max(room, 0)]
	}
	c.buf.Write(p)
	return n, nil
}
