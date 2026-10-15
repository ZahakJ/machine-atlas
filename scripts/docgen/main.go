// Command docgen regenerates the parts of the documentation that are copied
// from the code: docs/REFERENCES.md (from the lessons' front matter) and the
// derived-facts table in docs/DATA-MODEL.md (from derive.All on the
// desktop-x86_64 fixture). Run it from the repository root:
//
//	go run ./scripts/docgen
//
// and review the diff. It writes nothing else.
package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/derive"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

func main() {
	cur, err := curriculum.Default()
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile("docs/REFERENCES.md", references(cur), 0o644); err != nil {
		fail(err)
	}
	if err := factTable("docs/DATA-MODEL.md"); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "docgen:", err)
	os.Exit(1)
}

// topics files each lesson's references under a heading.
var topics = []struct {
	title   string
	lessons []string
}{
	{"Addresses and paging hardware", []string{"cpu-addressing"}},
	{"Address spaces and mappings", []string{"virtual-address-spaces", "mappings"}},
	{"Page faults and huge pages", []string{"page-faults", "transparent-huge-pages"}},
	{"Physical memory", []string{"physical-memory"}},
	{"Memory accounting and the page cache", []string{"anonymous-and-file-pages", "page-cache"}},
	{"Reclaim, swap, pressure and OOM", []string{"reclaim-and-swap", "memory-pressure-and-oom"}},
	{"CPU topology", []string{"cpu-topology"}},
	{"CPU frequency scaling", []string{"cpu-frequency"}},
	{"Run queues and load", []string{"run-queues-and-load"}},
	{"Fair scheduling", []string{"fair-scheduling"}},
	{"CPU limits and isolation", []string{"cpu-limits-and-isolation"}},
	{"Block devices and request queues", []string{"block-devices"}},
	{"I/O schedulers", []string{"io-schedulers"}},
	{"I/O statistics and pressure", []string{"io-statistics"}},
	{"Filesystems, the VFS and readahead", []string{"filesystems-and-the-vfs"}},
	{"Writeback and durability", []string{"writeback-and-durability"}},
	{"Network interfaces and addresses", []string{"network-interfaces"}},
	{"The path of a packet", []string{"packet-path"}},
	{"Sockets and TCP connections", []string{"sockets-and-connections"}},
	{"TCP throughput and congestion control", []string{"tcp-throughput"}},
	{"Packet queues", []string{"packet-queues"}},
}

var kindNames = map[string]string{
	"kernel-doc": "kernel documentation", "man-page": "man page", "spec": "specification",
	"source": "kernel source", "lwn": "LWN", "book": "book", "paper": "paper", "other": "other",
}

// readingOrder is the core lessons by order, each branch lesson placed
// right after the last of its prerequisites.
func readingOrder(cur *curriculum.Curriculum) []*curriculum.Lesson {
	var order, branches []*curriculum.Lesson
	for _, l := range cur.Lessons {
		if l.Kind == "core" {
			order = append(order, l)
		} else {
			branches = append(branches, l)
		}
	}
	slices.SortStableFunc(order, func(a, b *curriculum.Lesson) int { return a.Order - b.Order })
	slices.SortStableFunc(branches, func(a, b *curriculum.Lesson) int { return a.Order - b.Order })
	for _, b := range branches {
		at := 0
		for i, l := range order {
			if slices.Contains(b.Prerequisites, l.ID) {
				at = i + 1
			}
		}
		order = slices.Insert(order, at, b)
	}
	return order
}

func references(cur *curriculum.Curriculum) []byte {
	order := readingOrder(cur)
	topicOf := map[string]string{}
	for _, t := range topics {
		for _, id := range t.lessons {
			topicOf[id] = t.title
		}
	}
	type ref struct {
		curriculum.Reference
		topic   string
		citedBy []string
	}
	var refs []*ref
	byURL := map[string]*ref{}
	for _, l := range order {
		if topicOf[l.ID] == "" {
			fail(fmt.Errorf("lesson %s has no topic in scripts/docgen", l.ID))
		}
		for _, r := range l.References {
			if got, ok := byURL[r.URL]; ok {
				if !slices.Contains(got.citedBy, l.ID) {
					got.citedBy = append(got.citedBy, l.ID)
				}
				continue
			}
			n := &ref{r, topicOf[l.ID], []string{l.ID}}
			byURL[r.URL] = n
			refs = append(refs, n)
		}
	}

	var b bytes.Buffer
	b.WriteString(`# References

Every primary source the lessons cite, grouped by topic and de-duplicated by URL, followed by the kernel-version-dependent behaviours the lessons call out. Both lists are generated from the front matter of ` + "`curriculum/lessons/*.md`" + ` (` + "`references`" + ` and ` + "`version_notes`" + `) by ` + "`go run ./scripts/docgen`" + `; ` + "`atlas lesson ID --json`" + ` prints the same fields for one lesson. When a lesson adds or changes a reference, regenerate this file rather than editing it by hand.

A reference is filed under the topic of the first lesson on the journey that cites it, with that lesson's title for it; "Cited by" lists every lesson that does. Kinds are those the lint accepts: kernel documentation, man page, specification, kernel source, LWN, book, paper, other.
`)
	for _, t := range topics {
		fmt.Fprintf(&b, "\n## %s\n\n| Reference | Kind | Cited by |\n|---|---|---|\n", t.title)
		for _, r := range refs {
			if r.topic != t.title {
				continue
			}
			ids := make([]string, len(r.citedBy))
			for i, id := range r.citedBy {
				ids[i] = "`" + id + "`"
			}
			fmt.Fprintf(&b, "| [%s](%s) | %s | %s |\n", cell(r.Title), r.URL, kindNames[r.Kind], strings.Join(ids, ", "))
		}
	}

	type note struct {
		curriculum.VersionNote
		lesson     string
		major, min int
		versioned  bool
	}
	var notes []note
	for _, l := range order {
		for _, v := range l.VersionNotes {
			n := note{VersionNote: v, lesson: l.ID}
			if m := versionRE.FindStringSubmatch(v.Applies); m != nil {
				n.major, _ = strconv.Atoi(m[1])
				n.min, _ = strconv.Atoi(m[2])
				n.versioned = true
			}
			notes = append(notes, n)
		}
	}
	// Stable: equal versions keep reading order, then the lesson's order.
	slices.SortStableFunc(notes, func(a, b note) int {
		switch {
		case a.versioned != b.versioned:
			if a.versioned {
				return -1
			}
			return 1
		case a.major != b.major:
			return a.major - b.major
		}
		return a.min - b.min
	})
	b.WriteString(`
## Version-dependent behaviour

What the lessons say changes with the kernel version, oldest first. "Applies" is copied from the lesson; where a lesson gives a range or a configuration instead of a single version, it is kept as written. The live-machine and fixture kernels Machine Atlas was exercised on are listed in the README.

| Since (as the lesson states it) | Behaviour | Lesson |
|---|---|---|
`)
	for _, n := range notes {
		fmt.Fprintf(&b, "| %s | %s | [`%s`](../curriculum/lessons/%s.md) |\n", cell(n.Applies), cell(n.Text), n.lesson, n.lesson)
	}
	return b.Bytes()
}

// versionRE finds the first "Linux X.Y" in a version note's applies field.
var versionRE = regexp.MustCompile(`Linux (?:≥ )?(\d+)\.(\d+)`)

// cell escapes text for a Markdown table cell: pipes, and angle brackets
// that would read as a tag ("/proc/<pid>/maps"); a lone ">" stays.
func cell(s string) string {
	s = tagRE.ReplaceAllString(s, "&lt;$1&gt;")
	return strings.NewReplacer("|", `\|`, "<", "&lt;").Replace(s)
}

var tagRE = regexp.MustCompile(`<([^<>]*)>`)

const factFixture = "desktop-x86_64"

// factTable replaces the table that starts "| id | title | basis" in the
// file with one generated from derive.All on the desktop-x86_64 fixture.
func factTable(path string) error {
	f, err := fixtures.FS.Open("snapshots/" + factFixture + ".json")
	if err != nil {
		return err
	}
	s, err := snapshot.Load(f)
	f.Close()
	if err != nil {
		return err
	}
	var t bytes.Buffer
	fmt.Fprintf(&t, "| id | title | basis | unit | rule (as computed for %s) | inputs |\n|---|---|---|---|---|---|\n", factFixture)
	for _, fact := range derive.All(s) {
		e, ok := catalog.Lookup(fact.ID)
		if !ok {
			return fmt.Errorf("%s is not in the catalog", fact.ID)
		}
		inputs := make([]string, len(fact.Inputs))
		for i, id := range fact.Inputs {
			inputs[i] = "`" + id + "`"
		}
		fmt.Fprintf(&t, "| `%s` | %s | %s | %s | %s | %s |\n", fact.ID, cell(e.Title), fact.Basis, fact.Unit, cell(fact.Rule), strings.Join(inputs, ", "))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	start := bytes.Index(data, []byte("| id | title | basis |"))
	if start < 0 {
		return fmt.Errorf("%s: no derived-facts table", path)
	}
	end := bytes.Index(data[start:], []byte("\n\n"))
	if end < 0 {
		return fmt.Errorf("%s: derived-facts table does not end", path)
	}
	out := slices.Concat(data[:start], t.Bytes(), data[start+end+1:])
	return os.WriteFile(path, out, 0o644)
}
