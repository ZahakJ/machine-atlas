package collect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const testMeta = `{
  "description": "test machine",
  "now": "2026-01-15T12:00:00Z",
  "uname": {"machine": "aarch64", "release": "6.6.31-v8+"},
  "page_size": 16384,
  "deny": ["/proc/slabinfo", "/sys/kernel/secret"],
  "env": {"XDG_SESSION_TYPE": "tty", "HOME": "/home/should-not-leak"},
  "commands": {
    "lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE": {"stdout": "_cmd/lsblk.json"},
    "lsblk --version": {"missing": true},
    "lsblk --bytes": {"exit": 1, "stderr": "lsblk: unrecognized option '--json'"}
  }
}`

// writeTree creates files (path → content) under dir; content "->target"
// creates a symbolic link instead.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		var err error
		if target, ok := strings.CutPrefix(content, "->"); ok {
			err = os.Symlink(target, full)
		} else {
			err = os.WriteFile(full, []byte(content), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func openTestFixture(t *testing.T) (*FixtureEnv, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pi-test")
	writeTree(t, dir, map[string]string{
		"_meta.json":              testMeta,
		"_cmd/lsblk.json":         `{"blockdevices": []}`,
		"proc/meminfo":            "MemTotal: 1024 kB\n",
		"proc/slabinfo":           "slabinfo - version: 2.1\n",
		"sys/kernel/secret/value": "1\n",
		"sys/block/mmcblk0":       "->../devices/platform/mmc/block/mmcblk0",
		"sys/devices/platform/mmc/block/mmcblk0/size": "1000\n",
		"etc/escape": "->/etc/hostname",
	})
	f, err := OpenFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, dir
}

func TestFixtureFiles(t *testing.T) {
	f, dir := openTestFixture(t)
	if f.Live() || f.Name() != "pi-test" || f.Description() != "test machine" {
		t.Fatalf("Live/Name/Description = %v %q %q", f.Live(), f.Name(), f.Description())
	}
	if data, err := f.ReadFile("/proc/meminfo"); err != nil || string(data) != "MemTotal: 1024 kB\n" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if data, err := f.ReadFile("/sys/block/mmcblk0/size"); err != nil || string(data) != "1000\n" {
		t.Fatalf("ReadFile through a relative symlink = %q, %v", data, err)
	}
	if target, err := f.Readlink("/sys/block/mmcblk0"); err != nil || target != "../devices/platform/mmc/block/mmcblk0" {
		t.Fatalf("Readlink = %q, %v", target, err)
	}
	if names, err := f.ReadDir("/proc"); err != nil || !reflect.DeepEqual(names, []string{"meminfo", "slabinfo"}) {
		t.Fatalf("ReadDir = %v, %v", names, err)
	}

	tests := []struct {
		name string
		err  error
		want obs.Status
	}{
		{"missing file", second(f.ReadFile("/proc/pressure/memory")), obs.StatusAbsent},
		{"missing dir", errOf(f.ReadDir("/sys/kernel/mm/lru_gen")), obs.StatusAbsent},
		{"denied file", second(f.ReadFile("/proc/slabinfo")), obs.StatusPermissionDenied},
		{"denied subtree", second(f.ReadFile("/sys/kernel/secret/value")), obs.StatusPermissionDenied},
		{"denied dir", errOf(f.ReadDir("/sys/kernel/secret")), obs.StatusPermissionDenied},
		{"symlink escaping the root", second(f.ReadFile("/etc/escape")), obs.StatusFailed},
	}
	for _, tt := range tests {
		if got := obs.FromError(tt.err); got != tt.want {
			t.Errorf("%s: status %q (err %v), want %q", tt.name, got, tt.err, tt.want)
		}
		if tt.err != nil && strings.Contains(tt.err.Error(), dir) {
			t.Errorf("%s: error %q reveals the fixture location", tt.name, tt.err)
		}
	}
	if _, err := f.ReadFile("proc/meminfo"); err == nil {
		t.Error("relative machine path accepted")
	}
}

func second(_ []byte, err error) error  { return err }
func errOf(_ []string, err error) error { return err }

func TestFixtureStat(t *testing.T) {
	f, _ := openTestFixture(t)
	tests := []struct {
		path   string
		exists bool
		status obs.Status
	}{
		{"/proc/meminfo", true, obs.StatusOK},
		{"/proc/nothing", false, obs.StatusOK},
		{"/proc/meminfo/x", false, obs.StatusOK}, // ENOTDIR is "no"
		{"/proc/slabinfo", true, obs.StatusOK},   // denied files exist, like root-only files
		{"/sys/kernel/secret/value", false, obs.StatusPermissionDenied},
	}
	for _, tt := range tests {
		exists, err := f.Stat(tt.path)
		if exists != tt.exists || obs.FromError(err) != tt.status {
			t.Errorf("Stat(%s) = %v, %v; want %v, %s", tt.path, exists, err, tt.exists, tt.status)
		}
	}
}

func TestFixtureCommands(t *testing.T) {
	f, _ := openTestFixture(t)
	ctx := context.Background()

	out, err := f.Run(ctx, "lsblk", "--json", "--bytes", "--output", "NAME,TYPE,SIZE,FSTYPE")
	if err != nil || string(out) != `{"blockdevices": []}` {
		t.Fatalf("canned stdout = %q, %v", out, err)
	}
	if _, err := f.Run(ctx, "lsblk", "--version"); obs.FromError(err) != obs.StatusAbsent {
		t.Errorf("missing tool: %v", err)
	}
	if _, err := f.Run(ctx, "free", "-b"); obs.FromError(err) != obs.StatusAbsent {
		t.Errorf("unlisted command: %v", err)
	}
	_, err = f.Run(ctx, "lsblk", "--bytes")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 1 || !strings.Contains(exit.Stderr, "unrecognized option") {
		t.Errorf("non-zero exit: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Run(cancelled, "lsblk", "--json", "--bytes", "--output", "NAME,TYPE,SIZE,FSTYPE"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: %v", err)
	}
}

func TestFixtureMeta(t *testing.T) {
	f, _ := openTestFixture(t)
	if v, ok := f.Getenv("XDG_SESSION_TYPE"); !ok || v != "tty" {
		t.Errorf("Getenv(XDG_SESSION_TYPE) = %q, %v", v, ok)
	}
	if v, ok := f.Getenv("HOME"); ok || v != "" {
		t.Errorf("Getenv(HOME) = %q, %v; names off the allow-list must read as unset", v, ok)
	}
	if _, ok := f.Getenv("DISPLAY"); ok {
		t.Error("DISPLAY should be unset")
	}
	if u, err := f.Uname(); err != nil || u != (Uname{Machine: "aarch64", Release: "6.6.31-v8+"}) {
		t.Errorf("Uname = %+v, %v", u, err)
	}
	if ps, err := f.PageSize(); err != nil || ps != 16384 {
		t.Errorf("PageSize = %d, %v", ps, err)
	}
	want := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if !f.Now().Equal(want) {
		t.Errorf("Now = %v", f.Now())
	}
	other := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	f.Clock = func() time.Time { return other }
	if !f.Now().Equal(other) {
		t.Errorf("Clock override ignored: %v", f.Now())
	}
}

func TestOpenFixtureRejectsBadMeta(t *testing.T) {
	for name, meta := range map[string]string{
		"missing now":   `{"description": "x"}`,
		"unknown field": `{"now": "2026-01-15T12:00:00Z", "arch": "x86_64"}`,
		"relative deny": `{"now": "2026-01-15T12:00:00Z", "deny": ["proc/slabinfo"]}`,
		"not json":      `now: today`,
	} {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"_meta.json": meta})
		if f, err := OpenFixture(dir); err == nil {
			f.Close()
			t.Errorf("%s: OpenFixture accepted %s", name, meta)
		}
	}
	if _, err := OpenFixture(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("OpenFixture accepted a missing directory")
	}
}
