package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// FixtureEnv reads a fake machine tree: machine paths map directly under the
// root (/proc/meminfo → <root>/proc/meminfo), and <root>/_meta.json supplies
// what is not a file: the clock, uname, page size, environment, canned
// command output and paths that must fail with EACCES. Collectors cannot tell
// it from the live machine except through Live().
//
// The root is opened with os.Root, so symbolic links inside a fixture
// resolve within it and can never reach the real /proc or /sys. Errors carry
// the machine path, never the location of the fixture on disk.
type FixtureEnv struct {
	// Clock overrides the time in _meta.json when set.
	Clock func() time.Time

	name string
	root *os.Root
	meta fixtureMeta
	now  time.Time
}

// fixtureMeta is the schema of _meta.json (ARCHITECTURE.md §12).
type fixtureMeta struct {
	Description string `json:"description"`
	Now         string `json:"now"`
	Uname       struct {
		Machine string `json:"machine"`
		Release string `json:"release"`
	} `json:"uname"`
	PageSize int                       `json:"page_size"`
	Deny     []string                  `json:"deny"`
	Env      map[string]string         `json:"env"`
	Commands map[string]fixtureCommand `json:"commands"`
}

// fixtureCommand is the canned result of one argv: stdout read from a file
// under the root, or a missing tool, or a non-zero exit with stderr.
type fixtureCommand struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Exit    int    `json:"exit"`
	Missing bool   `json:"missing"`
}

// OpenFixture opens the fixture root at dir and reads its _meta.json, which
// must exist and name a valid "now". Close the result when done.
func OpenFixture(dir string) (*FixtureEnv, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	f := &FixtureEnv{name: filepath.Base(filepath.Clean(dir)), root: root}
	if err := f.loadMeta(); err != nil {
		root.Close()
		return nil, fmt.Errorf("fixture %s: %w", f.name, err)
	}
	return f, nil
}

func (f *FixtureEnv) loadMeta() error {
	data, err := f.root.ReadFile("_meta.json")
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f.meta); err != nil {
		return fmt.Errorf("_meta.json: %w", err)
	}
	if f.now, err = time.Parse(time.RFC3339, f.meta.Now); err != nil {
		return fmt.Errorf("_meta.json: now: %w", err)
	}
	for i, d := range f.meta.Deny {
		if !path.IsAbs(d) {
			return fmt.Errorf("_meta.json: deny entry %q is not an absolute path", d)
		}
		f.meta.Deny[i] = path.Clean(d)
	}
	return nil
}

// Close releases the root directory.
func (f *FixtureEnv) Close() error { return f.root.Close() }

// Name is the fixture's directory name, which is what a snapshot records.
func (f *FixtureEnv) Name() string { return f.name }

// Description is the one-line description from _meta.json.
func (f *FixtureEnv) Description() string { return f.meta.Description }

// rel maps an absolute machine path to a path relative to the root.
func rel(p string) (string, error) {
	if !path.IsAbs(p) {
		return "", &fs.PathError{Op: "open", Path: p, Err: errors.New("machine paths must be absolute")}
	}
	r := strings.TrimPrefix(path.Clean(p), "/")
	if r == "" {
		r = "."
	}
	return r, nil
}

// denied reports whether p is a deny entry or lies beneath one.
func (f *FixtureEnv) denied(p string) bool {
	p = path.Clean(p)
	return slices.ContainsFunc(f.meta.Deny, func(d string) bool {
		return p == d || strings.HasPrefix(p, d+"/")
	})
}

// machineErr re-labels an error from os.Root with the machine path, so the
// fixture's location on disk never appears in a note or a snapshot.
func machineErr(op, p string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return &fs.PathError{Op: op, Path: p, Err: err}
}

// ReadFile implements Env.
func (f *FixtureEnv) ReadFile(p string) ([]byte, error) {
	if f.denied(p) {
		return nil, machineErr("open", p, syscall.EACCES)
	}
	r, err := rel(p)
	if err != nil {
		return nil, err
	}
	file, err := f.root.Open(r)
	if err != nil {
		return nil, machineErr("open", p, err)
	}
	defer file.Close()
	return readCapped(file, p)
}

// ReadDir implements Env.
func (f *FixtureEnv) ReadDir(p string) ([]string, error) {
	if f.denied(p) {
		return nil, machineErr("open", p, syscall.EACCES)
	}
	r, err := rel(p)
	if err != nil {
		return nil, err
	}
	dir, err := f.root.Open(r)
	if err != nil {
		return nil, machineErr("open", p, err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, machineErr("readdirent", p, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	slices.Sort(names)
	return names, nil
}

// Readlink implements Env.
func (f *FixtureEnv) Readlink(p string) (string, error) {
	if f.denied(p) {
		return "", machineErr("readlink", p, syscall.EACCES)
	}
	r, err := rel(p)
	if err != nil {
		return "", err
	}
	target, err := f.root.Readlink(r)
	if err != nil {
		return "", machineErr("readlink", p, err)
	}
	return target, nil
}

// Stat implements Env. A path named in deny exists but cannot be read, as a
// root-only file does; a path beneath a denied directory cannot even be
// checked, so it fails with EACCES like a directory without search
// permission.
func (f *FixtureEnv) Stat(p string) (bool, error) {
	r, err := rel(p)
	if err != nil {
		return false, err
	}
	if slices.Contains(f.meta.Deny, path.Clean(p)) {
		return true, nil
	}
	if f.denied(p) {
		return false, machineErr("stat", p, syscall.EACCES)
	}
	_, err = f.root.Stat(r)
	if err != nil {
		err = machineErr("stat", p, err)
	}
	return existence(err)
}

// Run implements Env. The argv joined by spaces is looked up in the
// "commands" map of _meta.json; an argv that is not listed behaves like a
// tool that is not installed.
func (f *FixtureEnv) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	key := strings.Join(append([]string{name}, args...), " ")
	c, ok := f.meta.Commands[key]
	if !ok || c.Missing {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	var out []byte
	if c.Stdout != "" {
		data, err := f.root.ReadFile(c.Stdout)
		if err != nil {
			return nil, fmt.Errorf("fixture output for %q: %w", key, machineErr("open", "/"+c.Stdout, err))
		}
		if len(data) > MaxOutputBytes {
			return nil, fmt.Errorf("%s: %w", name, ErrTooLarge)
		}
		out = data
	}
	if c.Exit != 0 {
		return out, &ExitError{Name: name, Code: c.Exit, Stderr: c.Stderr}
	}
	return out, nil
}

// Getenv implements Env with the "env" map of _meta.json, subject to the
// same allow-list as the live environment.
func (f *FixtureEnv) Getenv(name string) (string, bool) {
	if !envAllowed(name) {
		return "", false
	}
	v, ok := f.meta.Env[name]
	return v, ok
}

// Uname implements Env from the "uname" object of _meta.json.
func (f *FixtureEnv) Uname() (Uname, error) {
	if f.meta.Uname.Machine == "" {
		return Uname{}, errors.New("fixture _meta.json has no uname.machine")
	}
	return Uname{Machine: f.meta.Uname.Machine, Release: f.meta.Uname.Release}, nil
}

// PageSize implements Env from "page_size" in _meta.json.
func (f *FixtureEnv) PageSize() (int, error) {
	if f.meta.PageSize <= 0 {
		return 0, errors.New("fixture _meta.json has no page_size")
	}
	return f.meta.PageSize, nil
}

// Now implements Env: the fixture's fixed time, so goldens are reproducible.
func (f *FixtureEnv) Now() time.Time {
	if f.Clock != nil {
		return f.Clock()
	}
	return f.now
}

// Live implements Env.
func (f *FixtureEnv) Live() bool { return false }
