package curriculum

import (
	"slices"
	"strings"
	"testing"

	"github.com/ZahakJ/machine-atlas/internal/derive"
)

func TestComponents(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	s := snap(t, numaNoSwap())
	g := c.Components(s, derive.All(s))
	node := func(id string) ComponentNode {
		i := slices.IndexFunc(g.Nodes, func(n ComponentNode) bool { return n.ID == id })
		if i < 0 {
			t.Fatalf("no component %q", id)
		}
		return g.Nodes[i]
	}

	numa := node("numa-nodes")
	if len(numa.Instances) != 2 || numa.Instances[1].ID != "numa-nodes/1" || numa.Instances[1].Title != "Node 1: 64 GiB, CPUs 32-63" {
		t.Errorf("NUMA instances %+v", numa.Instances)
	}
	zones := node("zones")
	if len(zones.Instances) != 2 || zones.Instances[0].ID != "zones/0/Normal" || zones.Instances[0].Cells["free"] != "2 000" {
		t.Errorf("zone instances %+v", zones.Instances)
	}
	if !slices.Contains(g.Edges, ComponentEdge{From: "zones/1/Normal", To: "numa-nodes/1", Basis: BasisObserved, Relation: "lies within"}) {
		t.Error("missing observed edge from a zone to its node")
	}
	if !slices.Contains(g.Edges, ComponentEdge{From: "zones", To: "numa-nodes", Basis: BasisArchitecture, Relation: "depends on"}) {
		t.Error("missing authored edge zones → numa-nodes")
	}
	for _, e := range g.Edges {
		if e.Basis == BasisObserved && !strings.Contains(e.From, "/") {
			t.Errorf("observed edge %+v must connect instances found in the snapshot", e)
		}
	}
	if swap := node("swap-areas"); len(swap.Instances) != 0 || swap.InstanceSource == nil || swap.InstanceSource.Status != "ok" {
		t.Errorf("swap areas on a swapless machine %+v", swap)
	}
	if !slices.Contains(node("numa-nodes").UsedBy, "zones") || !slices.Contains(node("buddy-allocator").DependsOn, "zones") {
		t.Error("used_by is not the reverse of depends_on")
	}
	mmu := node("cpu-mmu")
	if mmu.Observe[0].ID != "kernel.arch" || mmu.Observe[0].Display != "x86_64" || mmu.Observe[0].Label != LabelObserved {
		t.Errorf("observe values %+v", mmu.Observe)
	}

	// With swap on a block device the swap area lies on that device.
	s = snap(t, x86Desktop())
	g = c.Components(s, derive.All(s))
	if !slices.Contains(g.Edges, ComponentEdge{From: "swap-areas/nvme0n1p3", To: "block-devices/nvme0n1p3", Basis: BasisObserved, Relation: "stored on"}) {
		t.Error("missing observed edge from the swap area to its block device")
	}
}

func TestComponentsWithUnreadableTable(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	vals := x86Desktop()
	vals["mem.zones"] = missing("permission_denied")
	s := snap(t, vals)
	g := c.Components(s, derive.All(s))
	i := slices.IndexFunc(g.Nodes, func(n ComponentNode) bool { return n.ID == "zones" })
	if n := g.Nodes[i]; len(n.Instances) != 0 || n.InstanceSource.Display != "not available (permission denied)" {
		t.Errorf("zones %+v", n)
	}
}

func TestRoadmap(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	vals := x86Desktop()
	vals["mem.meminfo.MemAvailable"] = missing("failed")
	s := snap(t, vals)
	r := c.Roadmap(s, derive.All(s))
	if len(r.Subsystems) != 9 || r.CurriculumVersion == "" {
		t.Fatalf("roadmap %+v", r)
	}
	mem := r.Subsystems[slices.IndexFunc(r.Subsystems, func(s SubsystemView) bool { return s.ID == "memory" })]
	if mem.Status != "implemented" || !slices.Contains(mem.Lessons, "cpu-addressing") {
		t.Errorf("memory card %+v", mem)
	}
	byID := map[string]Value{}
	for _, v := range mem.Observe {
		byID[v.ID] = v
	}
	if v := byID["mem.meminfo.MemTotal"]; v.Display != "16 GiB" || v.Status != "ok" || v.Title != "Usable RAM" {
		t.Errorf("MemTotal %+v", v)
	}
	if v := byID["mem.meminfo.MemAvailable"]; v.Display != "not available (collection failed)" || v.Status != "failed" {
		t.Errorf("MemAvailable %+v", v)
	}
	var mglru Option
	for _, p := range mem.PlannedConcepts {
		if p.ID == "mglru-internals" {
			mglru = p
		}
	}
	if len(mglru.Notes) != 1 || !mglru.Notes[0].Matched || mglru.Notes[0].Value != "mglru" {
		t.Errorf("mglru-internals relevance %+v", mglru)
	}
}

func TestUsage(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	u := c.Usage()
	if got := u["kernel.arch"]; !slices.Contains(got.Lessons, "cpu-addressing") || !slices.Contains(got.Components, "cpu-mmu") {
		t.Errorf("kernel.arch used by %+v", got)
	}
	if got := u["mem.zones"]; !slices.Contains(got.Lessons, "physical-memory") || !slices.Equal(got.Components, []string{"zones"}) {
		t.Errorf("mem.zones used by %+v", got)
	}
	if got := u["derived.paging.levels"]; !slices.Contains(got.Lessons, "cpu-addressing") {
		t.Errorf("derived.paging.levels used by %+v", got)
	}
}
