package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/version"
)

// The binary built for the CLI tests, shared by every test and removed by
// TestMain.
var (
	binaryOnce sync.Once
	binaryDir  string
	binaryPath string
	binaryErr  error
)

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if binaryDir != "" {
		os.RemoveAll(binaryDir)
	}
	os.Exit(code)
}

// atlasBinary builds the atlas binary once per test run into a temporary
// directory and returns its path.
func atlasBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary")
	}
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "atlas-cli-test-")
		if err != nil {
			binaryErr = err
			return
		}
		binaryDir = dir
		binaryPath = filepath.Join(dir, "atlas")
		out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput()
		if err != nil {
			binaryErr = &buildError{err, out}
		}
	})
	if binaryErr != nil {
		t.Fatal(binaryErr)
	}
	return binaryPath
}

type buildError struct {
	err error
	out []byte
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + string(e.out) }

// runAtlas runs the binary and returns stdout, stderr and the exit code.
func runAtlas(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(atlasBinary(t), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

func TestCLI(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rpi.json")

	_, stderr, code := runAtlas(t, "collect", "--root", filepath.Join(rootsDir, "rpi-arm64-noswap"), "-o", out)
	if code != 0 || !strings.Contains(stderr, "redaction (default)") {
		t.Fatalf("collect: exit %d: %s", code, stderr)
	}
	golden, err := os.ReadFile(filepath.Join(goldensDir, "rpi-arm64-noswap.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, golden) {
		t.Error("atlas collect --root differs from the golden snapshot")
	}

	// A live collection cannot be backdated: --now is for fixture roots.
	if _, stderr, code := runAtlas(t, "collect", "--no-probe", "--now", "2001-01-01T00:00:00Z"); code != 2 || !strings.Contains(stderr, "--now needs --root") {
		t.Errorf("collect --now without --root: exit %d: %s", code, stderr)
	}

	stdout, stderr, code := runAtlas(t, "snapshot", "validate", out)
	if code != 0 || !strings.Contains(stdout, "valid schema 1 snapshot") {
		t.Errorf("snapshot validate: exit %d: %s%s", code, stdout, stderr)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, bytes.Replace(golden, []byte(`"schema_version": 1`), []byte(`"schema_version": 9`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runAtlas(t, "snapshot", "validate", bad); code != 1 || !strings.Contains(stderr, "version 9 is not supported") {
		t.Errorf("snapshot validate of schema 9: exit %d: %s", code, stderr)
	}

	stdout, stderr, code = runAtlas(t, "lesson", "cpu-addressing", "--fixture", "rpi-arm64-noswap")
	if code != 0 || !strings.Contains(stdout, "[OBSERVED]") {
		t.Errorf("lesson: exit %d: %.300s%s", code, stdout, stderr)
	}
	if _, _, code := runAtlas(t, "lesson", "cpu-addressing", "--fixture", "../etc/passwd"); code != 1 {
		t.Errorf("lesson with a path as fixture: exit %d", code)
	}

	stdout, stderr, code = runAtlas(t, "curriculum", "lint", "--allow-missing-lessons")
	if code != 0 || !strings.Contains(stdout, "0 errors") {
		t.Errorf("curriculum lint: exit %d: %s%s", code, stdout, stderr)
	}

	stdout, _, code = runAtlas(t, "fixtures", "list")
	if code != 0 || !strings.Contains(stdout, "desktop-x86_64") || strings.Contains(stdout, "invalid") {
		t.Errorf("fixtures list: exit %d: %s", code, stdout)
	}

	stdout, _, code = runAtlas(t, "version")
	if code != 0 || !strings.HasPrefix(stdout, "atlas "+version.Version+"\n") {
		t.Errorf("version: exit %d: %s", code, stdout)
	}

	if _, stderr, code := runAtlas(t, "serve", "--addr", "0.0.0.0:0"); code != 2 || !strings.Contains(stderr, "not a loopback address") {
		t.Errorf("serve on a public address: exit %d: %s", code, stderr)
	}
}
