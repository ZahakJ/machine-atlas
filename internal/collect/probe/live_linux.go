//go:build linux

package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Time limits for the children, inside the collector's own timeout. The
// child also exits by itself after childLifetime, so a parent that dies
// cannot leave it behind for long.
const (
	exitTimeout   = 1 * time.Second // from closing stdin to the child's exit
	layoutTimeout = 2 * time.Second // the whole minimal second execution
)

// live runs the probe: the full child, whose /proc files are read while it
// waits on stdin, then a minimal second execution for the second layout run.
func (c probeCollector) live(ctx context.Context, env collect.Env) []obs.Observation {
	argv, err := c.command()
	if err != nil {
		return allFailed("cannot find the atlas executable to start the probe: " + err.Error())
	}
	in, err := c.runFull(ctx, env, argv)
	if err != nil {
		return allFailed(err.Error())
	}
	if in.report == nil {
		return observe(in) // nothing to add a second run to
	}
	second, err := c.runLayout(ctx, argv)
	if err != nil {
		in.layoutNote = "only one run: the second execution failed: " + err.Error()
	} else {
		second.Run = len(in.report.LayoutRuns) + 1
		in.report.LayoutRuns = append(in.report.LayoutRuns, second)
	}
	return observe(in)
}

// allFailed reports every probe id as failed with note, for a child that
// could not be started or did not report.
func allFailed(note string) []obs.Observation {
	out := make([]obs.Observation, 0, len(ids()))
	for _, id := range ids() {
		out = append(out, obs.Failed(id, obs.Probe("atlas "+Verb), note))
	}
	return out
}

// runFull starts the full child, reads its report from stdout, reads its
// maps, smaps and status while it blocks on stdin, then closes stdin and
// waits for it, killing it if it does not exit in time.
func (c probeCollector) runFull(ctx context.Context, env collect.Env, argv []string) (inputs, error) {
	cmd := c.child(ctx, argv)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return inputs{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return inputs{}, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = &limitedWriter{w: stderr, n: 4 << 10}
	if err := cmd.Start(); err != nil {
		return inputs{}, fmt.Errorf("start the probe child: %w", err)
	}

	var in inputs
	line, readErr := readLine(stdout, maxReportBytes)
	if readErr == nil {
		in.report, in.reportErr = decodeReport(line, true)
		pid := strconv.Itoa(cmd.Process.Pid)
		read := func(name string) part {
			b, err := env.ReadFile("/proc/" + pid + "/" + name)
			return part{b, err}
		}
		in.maps, in.smaps, in.status = read("maps"), read("smaps"), read("status")
	}

	stdin.Close() // the child's signal to exit
	waitErr := waitOrKill(cmd, exitTimeout)
	if readErr != nil {
		return inputs{}, childError("the probe child did not report", readErr, waitErr, stderr)
	}
	return in, nil
}

// runLayout runs the minimal second execution and returns its layout.
func (c probeCollector) runLayout(ctx context.Context, argv []string) (LayoutRun, error) {
	ctx, cancel := context.WithTimeout(ctx, layoutTimeout)
	defer cancel()
	cmd := c.child(ctx, append(argv, layoutArg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdout, n: maxReportBytes + 1}
	cmd.Stderr = &limitedWriter{w: stderr, n: 4 << 10}
	if err := cmd.Run(); err != nil {
		return LayoutRun{}, childError("layout run", err, nil, stderr)
	}
	r, err := decodeReport(stdout.Bytes(), false)
	if err != nil {
		return LayoutRun{}, err
	}
	if len(r.LayoutRuns) != 1 {
		return LayoutRun{}, fmt.Errorf("layout run printed %d runs, want 1", len(r.LayoutRuns))
	}
	return r.LayoutRuns[0], nil
}

// child prepares a child command: no inherited environment, a neutral
// working directory, and killed with the context.
func (c probeCollector) child(ctx context.Context, argv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append([]string{}, c.env...)
	cmd.Dir = "/"
	cmd.WaitDelay = exitTimeout
	return cmd
}

// readLine reads one newline-terminated line of at most max bytes. The
// child prints its report as one JSON line and then waits, so reading to
// EOF would deadlock.
func readLine(r io.Reader, max int) ([]byte, error) {
	var line []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		line = append(line, buf[:n]...)
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			return line[:i+1], nil
		}
		if len(line) > max {
			return nil, fmt.Errorf("report exceeds %d bytes", max)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
}

// waitOrKill waits for cmd to exit, killing it after timeout.
func waitOrKill(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		cmd.Process.Kill()
		return fmt.Errorf("killed after not exiting within %s: %w", timeout, <-done)
	}
}

// childError describes a child that failed, with the first line of its
// standard error, which the child keeps free of paths.
func childError(what string, err, waitErr error, stderr *bytes.Buffer) error {
	msg := what + ": " + err.Error()
	if waitErr != nil {
		msg += " (" + waitErr.Error() + ")"
	}
	if first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); first != "" {
		msg += ": " + first
	}
	return errors.New(msg)
}

// limitedWriter keeps the first n bytes and discards the rest without
// failing, so a noisy child is never killed by a broken pipe.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
