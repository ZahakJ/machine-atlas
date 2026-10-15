package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LiveEnv reads the machine atlas runs on. Its clock is always the real
// one: each observation's collected_at is when it was read, so a live
// collection cannot be backdated (atlas collect --now needs --root).
type LiveEnv struct{}

// ReadFile implements Env.
func (LiveEnv) ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readCapped(f, path)
}

// readCapped reads at most MaxFileBytes. /proc and /sys files report size 0,
// so the limit must be enforced while reading, not from a stat.
func readCapped(r io.Reader, path string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: path, Err: err}
	}
	if len(data) > MaxFileBytes {
		return nil, &fs.PathError{Op: "read", Path: path, Err: ErrTooLarge}
	}
	return data, nil
}

// ReadDir implements Env.
func (LiveEnv) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

// Readlink implements Env.
func (LiveEnv) Readlink(path string) (string, error) { return os.Readlink(path) }

// Stat implements Env.
func (LiveEnv) Stat(path string) (bool, error) {
	_, err := os.Stat(path)
	return existence(err)
}

// existence turns a stat error into Env.Stat's answer: not-found kinds are a
// plain "no", anything else is a real error.
func existence(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return false, nil
	default:
		return false, err
	}
}

// Run implements Env.
func (LiveEnv) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := lookPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"LC_ALL=C", "PATH=" + CommandPath}
	cmd.Dir = "/"
	stdout := &cappedBuffer{max: MaxOutputBytes}
	stderr := &cappedBuffer{max: maxStderrBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// A child that leaves its output pipes open must not hold atlas past the
	// deadline.
	cmd.WaitDelay = 500 * time.Millisecond

	err = cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("%s: %w", name, ctx.Err())
	case stdout.over:
		return nil, fmt.Errorf("%s: %w", name, ErrTooLarge)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.buf.Bytes(), &ExitError{Name: name, Code: exitErr.ExitCode(), Stderr: strings.TrimSpace(stderr.buf.String())}
	}
	if err != nil {
		return nil, err
	}
	return stdout.buf.Bytes(), nil
}

// lookPath finds name in CommandPath only. Names containing a slash are
// refused, so every command a collector runs is a system tool found the same
// way on every machine.
func lookPath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '/') {
		return "", &exec.Error{Name: name, Err: errors.New("command names must be bare")}
	}
	for _, dir := range filepath.SplitList(CommandPath) {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// cappedBuffer keeps the first max bytes written and remembers whether more
// arrived. It accepts everything so the child is never killed by a broken
// pipe halfway through a write. The buffer is a named field, not embedded:
// an embedded bytes.Buffer would promote ReadFrom, and io.Copy would use it
// to bypass the cap.
type cappedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.over = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// Getenv implements Env.
func (LiveEnv) Getenv(name string) (string, bool) {
	if !envAllowed(name) {
		return "", false
	}
	return os.LookupEnv(name)
}

// Now implements Env.
func (LiveEnv) Now() time.Time { return time.Now() }

// Live implements Env.
func (LiveEnv) Live() bool { return true }
