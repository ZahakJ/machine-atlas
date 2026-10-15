# Contributing

One Go module with one dependency (`golang.org/x/sys`), a UI of plain ES
modules with no build step, lessons in Markdown. Internals: [ARCHITECTURE.md](ARCHITECTURE.md).
Observations, snapshots and derived facts: [DATA-MODEL.md](DATA-MODEL.md).

## Development loop

You need Go (the version in `go.mod` or newer) and, for the interface tests
only, Node 21 or newer. Nothing is installed with npm, and nothing is
downloaded at build time.

```sh
go build ./...                      # everything compiles
make test                           # go test -p 2 ./... and the Node tests
node --test 'web/test/*.test.mjs'   # the interface tests alone (quote the glob)
make vet fmt-check                  # go vet; gofmt check
make lint-curriculum                # atlas curriculum lint, strict
make cross                          # linux/arm64, linux/riscv64, darwin/arm64 builds
make serve-dev                      # serve the interface from web/ on disk
```

`make serve-dev` runs `atlas serve` with `ATLAS_WEB_DIR=web`, so the server
reads `index.html`, `css/` and `js/` from disk instead of the copy embedded in
the binary: edit a file and reload the page. Any `atlas serve` accepts the
variable (`ATLAS_WEB_DIR=web go run ./cmd/atlas serve --fixture
rpi-arm64-noswap`). Override the address with `ADDR=127.0.0.1:PORT`.

`web/dev/` holds pages that are never embedded: `kit.html` shows every
component of the design system, `diagrams.html` every diagram against the
fixture data in `web/dev/data/` (`?fixture=<name>&only=<diagram>`),
`sims.html` every simulation. Serve the `web/` directory with any static file
server bound to `127.0.0.1` and open `dev/<page>.html`.

Use `go test -p 2` (the Makefile does) on machines with little memory.

### Screenshots

`scripts/shot.sh URL OUT.png [--width W] [--height H] [--dark]
[--full-page] [--click SEL] [--wait-for SEL]` takes one screenshot with
headless Chromium (`scripts/shot.py --help` lists every option); concurrent
calls wait on `flock /tmp/machine-atlas-browser.lock`. Committed images
(`docs/media/`) show fixtures or redacted snapshots only.

## Add a collector

A worked example: kernel samepage merging (KSM) merges identical pages of
different processes, and whether it runs is in
`/sys/kernel/mm/ksm/run` (`0` stopped, `1` running, `2` unmerging). We add
`mem.ksm.run`.

**1. Declare the id in the catalog.** Every id is declared before anything
emits it; the runner rejects undeclared ids. In
`internal/catalog/catalog_mem.go`, add to `memEntries`:

```go
{
    ID: "mem.ksm.run", Title: "KSM state",
    Description: "Whether kernel samepage merging is running: 0 stopped, 1 running, 2 stopped and unmerging every shared page.",
    Type: obs.TypeInt, Kind: obs.KindStatic, Scope: obs.ScopeKernel,
    Caveat:     "Running does not mean any page is merged: only memory a program marked with madvise(MADV_MERGEABLE) or prctl(PR_SET_MEMORY_MERGE) is scanned.",
    Privileged: privSame,
},
```

Every field matters: `Caveat` is what the number cannot tell you,
`Privileged` what root would add, and `Sensitivity` (default `none`) decides
redaction; a table can give single columns their own class
(`ColumnSensitivity`, so an address cell is replaced while its class
survives) and number names shared across tables once per snapshot under
strict (`SharedIdentifiers`, as every network table does for interfaces and every
block table for devices). `TestEntriesAreComplete` fails on a missing title, description,
caveat or privileged text.

**2. Write the pure parser.** Parsing is a function of bytes, tested without
any environment. Start a new file `internal/collect/memory/ksm.go`; its
imports already cover the collector of step 3, so it compiles once that is
added:

```go
package memory

import (
    "fmt"

    "github.com/ZahakJ/machine-atlas/internal/collect"
    "github.com/ZahakJ/machine-atlas/internal/obs"
)

// parseKSMRun reads /sys/kernel/mm/ksm/run, one of 0, 1 or 2.
func parseKSMRun(data []byte) (int64, error) {
    n, err := parseInt(data)
    if err != nil {
        return 0, err
    }
    if n < 0 || n > 2 {
        return 0, fmt.Errorf("expected 0, 1 or 2, got %d", n)
    }
    return n, nil
}
```

**3. Write the collector** in the same file. It reads only through `env`,
owns exactly the ids it emits, and turns every failure into a status with a
note: `unavailable` maps the error (ENOENT → `absent`, EACCES →
`permission_denied`) and adds a note for the absent case, `malformed` marks
a parse error as `failed`.

```go
func ksmCollector() collect.Collector {
    const id, p = "mem.ksm.run", "/sys/kernel/mm/ksm/run"
    return collector{
        id:   "memory.ksm",
        owns: []string{id},
        read: func(env collect.Env) []obs.Observation {
            src := obs.File(p, "")
            data, err := env.ReadFile(p)
            if err != nil {
                return []obs.Observation{unavailable(id, src, err, "no KSM directory: the kernel was built without CONFIG_KSM")}
            }
            n, err := parseKSMRun(data)
            if err != nil {
                return []obs.Observation{malformed(id, src, err)}
            }
            return []obs.Observation{obs.OK(id, n, src)}
        },
    }
}
```

Never return a default for something you could not read, and never read a
path outside the interfaces the catalog describes.

**4. Register it** by adding `ksmCollector()` to the list in
`memory.Collectors()` (`internal/collect/memory/collectors.go`). The full
registry is `internal/collect/all`: system collectors first, then CPU and
scheduler (`internal/collect/sched`), memory, storage, network, and the probe last.
`TestRegistry` checks that every owned id is declared.

**5. Test the parser and every status path.** In
`internal/collect/memory/parse_test.go`, test the parser on its own:

```go
func TestParseKSMRun(t *testing.T) {
    for in, want := range map[string]int64{"0\n": 0, "1\n": 1, "2": 2} {
        if n, err := parseKSMRun([]byte(in)); err != nil || n != want {
            t.Errorf("parseKSMRun(%q) = %d, %v; want %d", in, n, err, want)
        }
    }
    for _, bad := range []string{"7", "-1", "x", ""} {
        if _, err := parseKSMRun([]byte(bad)); err == nil {
            t.Errorf("parseKSMRun(%q) accepted", bad)
        }
    }
}
```

In `collectors_test.go`, the `machine` helper builds a fixture root from a
map of paths, plus paths that must fail with EACCES:

```go
func TestKSM(t *testing.T) {
    on := run(t, machine(t, map[string]string{"/sys/kernel/mm/ksm/run": "1\n"}))
    if o := on["mem.ksm.run"]; o.Status != obs.StatusOK || o.Value != int64(1) {
        t.Errorf("running: %+v", o)
    }
    if o := run(t, machine(t, map[string]string{}))["mem.ksm.run"]; o.Status != obs.StatusAbsent || o.Note == "" {
        t.Errorf("missing: %+v", o)
    }
    denied := run(t, machine(t, map[string]string{"/sys/kernel/mm/ksm/run": "1\n"}, "/sys/kernel/mm/ksm/run"))
    if o := denied["mem.ksm.run"]; o.Status != obs.StatusPermissionDenied {
        t.Errorf("denied: %+v", o)
    }
}
```

`run` fails the test on any contract problem the runner reports (an undeclared
id, a wrong type, an owned id not emitted).

Then add the file to `modernMachine()` in the same file
(`"/sys/kernel/mm/ksm/run": "1\n",`). It is the machine on which every
memory id must be `ok` (`TestModernMachine`) and on which garbage in every
file must give `failed` with the parse error (`TestMalformed`); both fail
with `mem.ksm.run: absent` until the path is there.

**6. Add the file to the fixture machines.** The six roots in
`fixtures/roots/` are generated by `scripts/fixtures-gen` from the
descriptions in `machines.py`. In `machine.py`'s `generate`, write the file
for machines that have KSM (`r.w('/sys/kernel/mm/ksm/run', '0\n')`), and give
a machine description a key to vary it. (CPU and scheduler files are written
by `gen_sched.py`, with per-machine choices in its `KNOBS`; storage files by
`gen_storage.py`, with its `STORAGE` table.) Regenerate into
an empty directory and replace the roots:

```sh
python3 -I scripts/fixtures-gen/gen.py /tmp/roots && rm -r fixtures/roots && mv /tmp/roots fixtures/roots
```

Review `git diff fixtures/roots` and keep only the change you meant. Where a
root makes the new id interesting (absent on the old VM, denied on
`restricted`), say so in `fixtureWants` in the collector package's
`fixtures_test.go` (`system`, `sched`, `storage`, `network`); every other
observation only has to be not `failed`. Scheduler, storage and network
files come from `gen_sched.py`, `gen_storage.py` and `gen_net.py`, each with its own random generator and
per-machine table so the other files do not shift; fixtures may hold only
documentation and private addresses and locally administered MACs, which
`TestFixturesHoldNothingIdentifying` checks.

**7. Regenerate the golden snapshots** with `make fixtures`, which runs
`atlas collect --root` on every root with default redaction and the time in
the root's `_meta.json`. `TestGoldenSnapshots` compares them byte for byte.
Review the diff: it should add one observation per fixture and change each
snapshot's id. If a bound lesson changed, `go test ./cmd/atlas -run
TestBoundLessonGoldens -update` rewrites those goldens; read that diff too.

## Add a lesson

Lessons live in `curriculum/lessons/<id>.md`, and their place in the journey
comes from their front matter.

The reader is a systems engineer who wants mechanisms. Write bottom-up: the
problem the hardware or kernel faced, the mechanism that solves it, then what
user space can see of it. Teach causes, not trivia: "Cached is 9 GiB" is
trivia; "reading a file left its pages in memory because dropping them would
only cost a re-read later" is a cause. Every number about this machine is a
chip, never typed by hand, and the `else` arm of a branch gets the same care:
a reader without swap still learns swap. Where accounting overlaps or omits,
show the discrepancy. Predictions and experiments give each alternative
outcome with its reason. Cite primary sources: docs.kernel.org, man7.org,
the Intel SDM / AMD APM / Arm ARM, kernel source, LWN.

**Front matter** is JSON between `---` lines:

```json
{
  "id": "ksm",
  "title": "Identical pages, stored once",
  "subsystem": "memory",
  "kind": "branch",
  "order": 2,
  "summary": "How kernel samepage merging finds identical pages, and what it costs.",
  "prerequisites": ["anonymous-and-file-pages"],
  "uses": ["mem.ksm.run", "derived.mem.total_pages"],
  "relevance": [{"when": "mem.ksm.run", "is": 1, "note": "KSM is running on this machine."}],
  "next": [],
  "deeper": [],
  "version_notes": [{"applies": "Linux ≥ 6.4", "text": "prctl(PR_SET_MEMORY_MERGE) opts a whole process in."}],
  "references": [{"title": "Linux kernel documentation: Kernel Samepage Merging", "url": "https://docs.kernel.org/admin-guide/mm/ksm.html", "kind": "kernel-doc"}]
}
```

`kind` is `core` (on the journey, ordered by prerequisites then `order`) or
`branch` (offered when a relevance rule matches). `uses` must equal the set of
ids the text references, no more and no less. Reference kinds are
`kernel-doc`, `man-page`, `spec`, `source`, `lwn`, `book`, `paper`, `other`.
`docs/REFERENCES.md` is generated from every lesson's `references` and
`version_notes`: after changing either, run `go run ./scripts/docgen` (which
also rewrites the fact table in `docs/DATA-MODEL.md`) and review the diff. A
new lesson needs a topic in `scripts/docgen/main.go`. A `deeper` entry is a lesson id or `planned:<concept>` from `roadmap.json`.

**Sections**, in this order, by exact heading: `## Question`, `## Why it
exists`, `## How it works`, `## On this machine`, `## See it` (optional),
`## Predict`, `## Try it`, `## Limits`, `## Go deeper`.

**Directives** bind the text to the machine:

```markdown
KSM here is {{obs:mem.ksm.run}}; this machine has {{derived:mem.total_pages|count}} pages.

:::when mem.ksm.run is 1
KSM is scanning pages that programs have marked mergeable.
:::else
KSM is not running, so no page is merged, whatever programs ask for.
:::unknown
Atlas could not read the KSM setting here.
:::end

:::evidence What KSM reports
- mem.ksm.run | the run switch
:::end

:::predict
If two processes each load the same 100 MiB file with read(), will KSM merge their buffers?
- Yes, they are identical
- Only if the programs marked the memory mergeable
:::reveal
Only memory marked with madvise(MADV_MERGEABLE) is scanned…
:::end

:::cannot
Whether any pages are merged right now: that is in /sys/kernel/mm/ksm/pages_sharing, which this version does not collect.
:::end
```

Chips (`{{obs:ID}}`, `{{derived:ID}}`, with an optional `|fmt` of `auto`,
`bytes`, `count`, `pages`, `hex`, `percent`, `raw`) become values the reader
can click to inspect. A chip whose value is unavailable reads "not available
(permission denied)" and so on, so write sentences that still make sense then.
`:::when` branches have three arms; leave out `:::unknown` and a sentence
naming the status and source is generated. Other blocks: `:::table ID
columns=a,b limit=N`, `:::diagram NAME`, `:::sim NAME`, `:::experiment ID`,
`:::misconception Title`. One directive may nest inside another, no deeper.
Links are `[text](https://…)`, `lesson:<id>`, `component:<id>` or `obs:<id>`.
Raw HTML is shown as text.

Labels are computed, never written: a paragraph with an observation chip is
OBSERVED, one with only derived chips DERIVED, a simulation SIMULATED,
everything else GENERAL. Do not claim in GENERAL text what only a chip can
show.

**Check it:**

```sh
go run ./cmd/atlas curriculum lint
```

The lint fails on a missing or misordered section, bad front matter, a
prerequisite cycle or dangling id, `uses` that differs from the text, an id
missing from the catalog, an unknown diagram, simulation or experiment, a
link to a missing lesson or component, and a core lesson without `:::cannot`,
`:::predict` or references. `--allow-missing-lessons` turns references to
lessons not written yet into warnings; CI and
`TestEmbeddedCurriculum` lint strictly.

**Read it bound to several machines.** A lesson that reads well on your
desktop can be wrong on a Raspberry Pi or in a container. Read it on at least
the fixtures that differ for your topic:

```sh
go run ./cmd/atlas lesson ksm --fixture desktop-x86_64
go run ./cmd/atlas lesson ksm --fixture restricted
go run ./cmd/atlas lesson ksm --fixture old-vm-cgroupv1
go run ./cmd/atlas lesson ksm                          # this machine
```

The text output shows the branch chosen for that machine and a label on every
paragraph, list item and evidence row; `--json` prints the bound tree the
interface receives, including the branches not chosen. Then read it in the browser
with `make serve-dev`.

## Add a derived fact

1. Declare it in `internal/catalog/catalog_derived.go` with
   `derived(id, type, unit, basis, title, description, caveat)`. The basis
   is `Arithmetic` for a formula over observations, `Inference` for anything
   that reasons beyond them (an architecture implying a page-table format).
2. Compute it in the right group of `internal/derive` with a `calc`: read
   each input with `c.int`, `c.str`, `c.bool` or `c.table`, which record the
   input id and, when the input is not `ok`, the reason. Then
   `c.fact(id, rule, value)` returns an `unknown` fact with those reasons, or
   the value. Check `c.failed()` before dividing, and call `c.fail(...)` for
   reasons of your own (a zero denominator, a value that means something
   else).
3. Write the rule as a reader would want to check it, with this machine's
   specifics where they help ("sum of the min column over all 5 zones").
4. Test the value on a fixture and the unknown path with an input denied or
   absent (`TestUnknownPropagation` shows the pattern).
5. Regenerate the fact table in `docs/DATA-MODEL.md` with
   `go run ./scripts/docgen` (it computes `derive.All` on the
   `desktop-x86_64` fixture; it also rewrites `docs/REFERENCES.md`).

Facts are never stored. If a fact needs data no observation has, add the
observation first.

## Add a diagram or simulation

Diagrams live in `web/js/diagrams/`, one module each exporting `title` and
`mount(container, ctx)`. A data-bound diagram keeps its arithmetic in a pure
`<name>.model.js` that `node --test` covers in `web/test/`, cites the ids it
draws, and says what it does when an input is unavailable. Register the
module in `web/js/diagrams/index.js` and add its name to `Diagrams` in
`internal/curriculum/parse.go` (and to `dataDiagrams` if it draws
observations, which makes its block OBSERVED). Check it against the fixtures in
`web/dev/diagrams.html`.

Simulations live in `web/js/sim/` as a pure, deterministic
`<name>.model.js` (state and reducer, fully tested) and a
`<name>.view.js`. Register the view in `web/js/render/lesson.js` and the name
in `Sims` in `parse.go`. A simulation states its assumptions on screen and
carries the SIMULATED badge; anything it takes from the machine is labelled
as such, and anything it invents (page-table contents, frame numbers) is
labelled invented.

Render with `createElement` and `textContent` only (the helpers in
`web/js/lib/dom.js`); the content security policy forbids inline script and
style, and collected strings must never become markup.

## Add an experiment

Experiments are the only part of Machine Atlas that changes the machine:

- Declared in `internal/experiment/catalog.go` with a question, every step in
  order, the resources used (memory, disk, a hard time bound), what it does
  **not** do, and every outcome with its interpretation, including
  `unexpected`.
- Never run by collection or page load. The CLI shows the resource statement
  and asks for confirmation (`--yes` skips it); the API requires
  `{"acknowledged": true}` and the token, and refuses unless the active
  snapshot is the live machine.
- Runs in a child process (`atlas __experiment <id>`) that exits by itself
  before its time bound and is killed by the parent at it. Linux only; other
  builds list the experiment as unavailable with the reason.
- Unprivileged, no system settings changed, no other process touched. Files
  only under `$XDG_CACHE_HOME/machine-atlas` (mode 0700), deleted before
  exit, also on failure; refuse when the disk is short.
- The outcome is chosen by a fixed, pure rule over the measurements
  (`classify.go`), recorded in the result's notes, and every outcome is
  reachable from synthetic measurements in tests.
- Results are never stored in snapshots.

Add the id to `Experiments` in `internal/curriculum/parse.go` so lessons can
embed it.

## Add a fixture machine

A fixture is a fake machine tree that exercises behaviours no single real
machine shows. Add a description to `MACHINES` in
`scripts/fixtures-gen/machines.py` (a `readme` saying what it models and
which behaviours it exercises, `now`, `uname`, the memory figures, the files
that should be missing or denied), regenerate the roots as in step 6 above,
add the root's expectations to `fixtureWants`, and run `make fixtures`. The
root's `_meta.json` holds `description`, `now`, `uname` (`machine`,
`release`), `page_size`, `deny` (paths that fail with EACCES), `env`
(allow-listed variables) and `commands` (argv → `{"stdout": file}`,
`{"missing": true}` or `{"exit": N, "stderr": text}`); `_probe/` holds what a
probe child on that machine would report. Every value is invented.

## Rules

**Privacy.** Nothing identifying about a real machine or person enters the
repository: no hostnames, user names, home paths, addresses, serial numbers
or UUIDs, in code, tests, fixtures, screenshots or commit messages. Fixtures
are invented. A real capture (`fixtures/snapshots/live-sample.json`) is
redacted and then read through by hand, because redaction knows only its
patterns. New collectors read only the interfaces the catalog lists and
never environments, command lines or other processes' memory.

**Labels.** Every claim the interface shows is OBSERVED, DERIVED, GENERAL or
SIMULATED, and the label is computed from what the claim rests on. Never let
an inference read as an observation, a simulation as a measurement, or
general kernel behaviour as a fact about this machine.

**Limits.** When Linux does not account for something exactly,
say what the discrepancy is instead of smoothing it over; when a value cannot
be read, show the status and why. Every core lesson says what its
measurements cannot establish, and every catalog entry says what its number
cannot tell you.

**Code.** No new dependencies. If the code and `docs/ARCHITECTURE.md` disagree, fix one of them in the
same change.
