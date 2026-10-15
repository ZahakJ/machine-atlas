package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/derive"
)

// A container that leaves no marker file (containerd, Kubernetes, LXC) and
// runs in its own cgroup namespace shows PID 1 at the cgroup root, like a
// host. The alpine-container root without /.dockerenv and without the
// hypervisor flag is such a container; PID 1, a shell, still gives it away.
func TestContainerWithoutMarkerFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unmarked-container")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(rootsDir, "alpine-container"))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".dockerenv")); err != nil {
		t.Fatal(err)
	}
	cpuinfo := filepath.Join(dir, "proc", "cpuinfo")
	data, err := os.ReadFile(cpuinfo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cpuinfo, []byte(strings.ReplaceAll(string(data), " hypervisor ", " ")), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := collectSnapshot(context.Background(), dir, time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range derive.All(s) {
		if f.ID != "derived.env.kind" {
			continue
		}
		if f.Value != "container" || !strings.Contains(f.Rule, `PID 1 is "sh"`) {
			t.Errorf("derived.env.kind = %v (%s; %s), want container because of PID 1", f.Value, f.Rule, f.Reason)
		}
		return
	}
	t.Fatal("no derived.env.kind")
}
