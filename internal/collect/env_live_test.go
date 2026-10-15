//go:build linux

package collect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

func TestLiveReadFileLimit(t *testing.T) {
	dir := t.TempDir()
	small, big := filepath.Join(dir, "small"), filepath.Join(dir, "big")
	writeTree(t, dir, map[string]string{"small": "ok\n", "big": strings.Repeat("x", MaxFileBytes+1)})
	var env LiveEnv
	if data, err := env.ReadFile(small); err != nil || string(data) != "ok\n" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if _, err := env.ReadFile(big); !errors.Is(err, ErrTooLarge) || obs.FromError(err) != obs.StatusFailed {
		t.Errorf("oversized file: %v", err)
	}
	if exists, err := env.Stat(filepath.Join(small, "child")); exists || err != nil {
		t.Errorf("Stat below a file = %v, %v", exists, err)
	}
	if _, err := os.Stat("/proc/self/status"); err == nil {
		if data, err := env.ReadFile("/proc/self/status"); err != nil || len(data) == 0 {
			t.Errorf("procfs read (size 0 in stat) = %d bytes, %v", len(data), err)
		}
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := lookPath(name); err != nil {
		t.Skipf("%s not found in %s", name, CommandPath)
	}
}

func TestLiveRunEnvironment(t *testing.T) {
	requireTool(t, "env")
	t.Setenv("ATLAS_TEST_SECRET", "leak")
	out, err := LiveEnv{}.Run(context.Background(), "env")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(out))
	if len(got) != 2 || got[0] != "LC_ALL=C" || got[1] != "PATH="+CommandPath {
		t.Errorf("child environment = %q; want only LC_ALL and PATH", got)
	}
}

func TestLiveRunFailures(t *testing.T) {
	var env LiveEnv
	if _, err := env.Run(context.Background(), "atlas-no-such-tool"); obs.FromError(err) != obs.StatusAbsent {
		t.Errorf("missing tool: %v", err)
	}
	if _, err := env.Run(context.Background(), "/bin/true"); err == nil {
		t.Error("a command path with a slash was accepted")
	}

	requireTool(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := env.Run(ctx, "sleep", "5")
	if !errors.Is(err, context.DeadlineExceeded) || obs.FromError(err) != obs.StatusFailed {
		t.Errorf("timeout: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("Run outlived its deadline by %v", time.Since(start))
	}

	requireTool(t, "ls")
	_, err = env.Run(context.Background(), "ls", "/atlas-no-such-path")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code == 0 || exit.Stderr == "" {
		t.Errorf("non-zero exit: %#v", err)
	}

	requireTool(t, "head")
	if _, err := env.Run(context.Background(), "head", "-c", "1100000", "/dev/zero"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized output: %v", err)
	}
}

func TestLiveGetenvAllowList(t *testing.T) {
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("HOME", "/home/someone")
	var env LiveEnv
	if v, ok := env.Getenv("XDG_SESSION_TYPE"); !ok || v != "wayland" {
		t.Errorf("allowed name = %q, %v", v, ok)
	}
	if _, ok := env.Getenv("HOME"); ok {
		t.Error("HOME is not on the allow-list but was readable")
	}
}

func TestLiveUnamePageSize(t *testing.T) {
	var env LiveEnv
	if u, err := env.Uname(); err != nil || u.Machine == "" || u.Release == "" {
		t.Errorf("Uname = %+v, %v", u, err)
	}
	if ps, err := env.PageSize(); err != nil || ps < 4096 || ps&(ps-1) != 0 {
		t.Errorf("PageSize = %d, %v", ps, err)
	}
}
