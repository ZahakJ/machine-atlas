package all

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const rootsDir = "../../../fixtures/roots"

// Every collector's declared ownership must refer to the catalog, and
// collector ids must be unique, or the run report becomes ambiguous.
func TestRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range All(Options{}) {
		if seen[c.ID()] {
			t.Errorf("duplicate collector id %s", c.ID())
		}
		seen[c.ID()] = true
		for _, p := range c.Owns() {
			if prefix, family := strings.CutSuffix(p, "*"); family {
				if !strings.HasSuffix(prefix, ".") {
					t.Errorf("%s: family pattern %q must end in \".*\"", c.ID(), p)
				}
				continue
			}
			if _, ok := catalog.Lookup(p); !ok {
				t.Errorf("%s owns %s, which the catalog does not declare", c.ID(), p)
			}
		}
	}
}

// Running every collector against every fixture root must produce no
// contract problems: no undeclared or unowned ids, no wrong value types, no
// missing owned ids. Denied and absent readings are fine; they are what the
// fixtures exist to exercise.
func TestCollectorsHonourContractOnFixtures(t *testing.T) {
	roots, err := os.ReadDir(rootsDir)
	if os.IsNotExist(err) {
		t.Skip("no fixture roots yet")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roots {
		if !r.IsDir() {
			continue
		}
		t.Run(r.Name(), func(t *testing.T) {
			env, err := collect.OpenFixture(filepath.Join(rootsDir, r.Name()))
			if err != nil {
				t.Fatal(err)
			}
			defer env.Close()
			rep, _ := collect.Run(context.Background(), env, All(Options{}))
			for _, run := range rep.Collectors {
				if run.Error != "" {
					t.Errorf("%s: %s", run.ID, run.Error)
				}
			}
		})
	}
}

func TestNoProbe(t *testing.T) {
	cs := All(Options{NoProbe: true})
	last := cs[len(cs)-1]
	if last.ID() != "probe" {
		t.Fatalf("probe should run last, got %s", last.ID())
	}
	for _, o := range last.Collect(context.Background(), nil) {
		if o.Status != obs.StatusNotCollected {
			t.Errorf("%s: status %s with --no-probe", o.ID, o.Status)
		}
	}
}
