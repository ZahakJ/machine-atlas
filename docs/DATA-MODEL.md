# Data model

How Machine Atlas represents what it reads from a machine: the observation,
the snapshot file that carries observations, the limits applied to it,
redaction, the derived facts computed from it, the catalog that declares every
id, and the rules for changing any of these (`internal/obs`,
`internal/snapshot`, `internal/derive`, `internal/catalog`).

## Observations

An observation is one value read from the machine, with where it came from,
when, whose view it describes, and whether the reading worked
(`internal/obs`):

| field | meaning |
|---|---|
| `id` | dotted, namespaced id matching `^[a-z][A-Za-z0-9_.-]*$` (`mem.meminfo.Active_anon`) |
| `value` | an integer, a float, a string, a boolean, a table (an array of flat objects whose cells are scalars), or `null` |
| `unit` | `bytes`, `pages`, `count`, `bits`, `percent`, `seconds`, `microseconds`, `fraction` (derived `*_fraction` facts, 0 to 1, shown as a percentage), or omitted for dimensionless and textual values |
| `kind` | `static` or `live` (below) |
| `scope` | `kernel`, `cgroup`, `process` or `namespace` (below) |
| `status` | one of six statuses (below) |
| `source` | `{type, path, field, detail}`: how to repeat the reading by hand |
| `collected_at` | RFC 3339, UTC, whole seconds |
| `collector` | the collector that produced it (`memory.meminfo`) |
| `note` | why the status is what it is, or a caveat about this reading |

Kernel field names keep their case. `(` becomes `_` and `)` is dropped, and any
other character outside `[A-Za-z0-9_.-]` becomes `_`, so `Active(anon)` is
`mem.meminfo.Active_anon`; the original name is kept in `source.field`. Values
the kernel reports in kB are converted to bytes at collection, and
`source.detail` says so ("converted from kB"). There is deliberately no
kilobyte unit.

### Statuses

A value is never fabricated or defaulted. When a collector cannot produce an
id it owns, it still emits the id with `value: null` and one of the non-`ok`
statuses, and a note that says why. Validation enforces that `value` is null
exactly when the status is not `ok`.

| status | rule |
|---|---|
| `ok` | the value was read and parsed. An empty table is `ok` when the interface answered with nothing in it (an empty `/proc/swaps` is an `ok`, empty `mem.swap.areas`). |
| `absent` | the interface answered that the thing is not there: `ENOENT`, `ENOTDIR`, or a command that is not installed. Where `ENOENT` cannot be told apart from "the kernel was built without this feature" (no `/proc/pressure/memory` on a 4.19 kernel or one built without `CONFIG_PSI`), the status stays `absent` and the note names both possibilities. |
| `permission_denied` | the kernel refused the read: `EACCES` or `EPERM`, or a command that failed for that reason (`lsblk` on a machine where `/sys/dev/block` is restricted). |
| `unsupported` | the id does not apply here **by rule**, and the note names the rule: an x86 CPU flag on arm64, a cgroup v2 file when the memory controller is on a v1 hierarchy, an `lsblk` older than util-linux 2.27 that rejects `--json`, an error that matches `errors.ErrUnsupported` (`ENOSYS`, `EOPNOTSUPP`), or the probe on a non-Linux build. |
| `failed` | collection was attempted and went wrong: a parse error, an unexpected format, output over the 1 MiB read limit, a timeout, a panic in the collector, or an observation that broke the collector contract (wrong type, undeclared id), which the runner downgrades rather than drops. |
| `not_collected` | the id was deliberately skipped: `--no-probe`, a fixture root without `_probe/`, or the 15 s collection budget running out before the collector started. An id a newer snapshot has and an older one lacks also reads as `not_collected` when lessons are bound. |

The mapping from errors is `obs.FromError`; collectors that know more (that
`address sizes` is simply not printed on arm64, for example) say so in the
note.

### Kinds and scopes

`kind` separates configuration from measurement, because a snapshot's age
matters for the second and hardly at all for the first:

- `static`: configuration, stable across a boot or longer (`kernel.release`,
  `mem.sysctl.vm.swappiness`).
- `live`: a measurement that varies from one reading to the next
  (`mem.meminfo.MemFree`, `mem.vmstat.pgfault`).

`scope` says whose view a value describes. The same file can mean different
things inside a container, so the scope travels with the value:

- `kernel`: system-wide, as seen by this kernel instance (`/proc/meminfo`;
  in a container that is the host's kernel).
- `cgroup`: the control group atlas ran in (`mem.cgroup.*`, `sched.cgroup.*`).
- `process`: the atlas process or its probe child (`probe.*`, `session.*`,
  `sched.self.*`).
- `namespace`: what is visible from atlas's namespaces (mounts, network
  interfaces, hostname, PID 1, `/etc/os-release`).

Kind and scope are properties of the id: the runner fills them from the
catalog when a collector leaves them empty.

### Sources

| `source.type` | `path` holds |
|---|---|
| `file` | the absolute machine path read (`/proc/meminfo`) |
| `command` | the argv joined by spaces (`lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE`); commands run without a shell, from `/usr/sbin:/usr/bin:/sbin:/bin` only, with an environment of just `LC_ALL=C` and `PATH` |
| `syscall` | the system call (`uname`, `getpagesize`) |
| `probe` | what the probe child measured (`/proc/<probe>/smaps`) |

`field` is the original field name inside the source; `detail` records a
conversion or how a value was picked out. An observation whose collector never
reported it (a timeout, an exhausted budget) may have an empty source; an `ok`
observation always has one.

## Snapshot schema v1

A snapshot is one JSON file holding a collection's observations and nothing
computed from them. An annotated example, shortened from the `restricted`
fixture (`fixtures/snapshots/restricted.json`); the comments are not part of
the format:

```jsonc
{
  "schema": "machine-atlas/snapshot",      // exact string
  "schema_version": 1,                     // anything else is rejected with a message naming the supported version
  "app_version": "0.2.0",                  // the atlas that wrote it
  "id": "sha256:f98c249e…",                // content id, see "Canonical encoding"
  "created_at": "2025-02-11T22:05:51Z",
  "collection": {
    "mode": "fixture",                     // "live" | "fixture"
    "fixture": "restricted",               // the root's name in fixture mode, "" when live
    "privileged": false,                   // true when collected as root
    "started_at": "2025-02-11T22:05:51Z",
    "finished_at": "2025-02-11T22:05:51Z",
    "collectors": [                        // the run report, in run order
      {"id": "system.cpu", "status": "partial", "duration_ms": 0, "observations": 14, "error": ""}
      // status: "ok" every observation ok | "partial" some not ok, or a contract problem
      //       | "failed" timed out, panicked or nothing but failures | "skipped" everything not_collected
    ]
  },
  "redaction": {"applied": true, "policy": "default", "classes": ["hostname"], "replaced": 1},
  "observations": [                        // sorted by id
    {
      "id": "mem.meminfo.MemTotal",
      "value": 8404336640,
      "unit": "bytes",
      "kind": "live",
      "scope": "kernel",
      "status": "ok",
      "source": {"type": "file", "path": "/proc/meminfo", "field": "MemTotal", "detail": "converted from kB"},
      "collected_at": "2025-02-11T22:05:51Z",
      "collector": "memory.meminfo"
    },
    {
      "id": "mem.zones",                   // a table observation that could not be read
      "value": null,
      "kind": "live",
      "scope": "kernel",
      "status": "permission_denied",
      "source": {"type": "file", "path": "/proc/zoneinfo"},
      "collected_at": "2025-02-11T22:05:51Z",
      "collector": "memory.zoneinfo",
      "note": "open /proc/zoneinfo: permission denied"
    },
    {
      "id": "os.hostname",                 // redacted: the status stays ok, the note says so
      "value": "<redacted:hostname>",
      "kind": "static",
      "scope": "namespace",
      "status": "ok",
      "source": {"type": "file", "path": "/proc/sys/kernel/hostname"},
      "collected_at": "2025-02-11T22:05:51Z",
      "collector": "system.hostname",
      "note": "redacted (hostname)"
    }
  ]
}
```

Table values are arrays of flat objects; cells are numbers, strings or
booleans, never nested values. Object keys inside a table row are encoded in
sorted order.

### Canonical encoding and the content id

`snapshot.Encode` writes the fields in the order above, observations sorted by
id, two-space indentation, a trailing newline, and no HTML escaping (so
placeholders such as `<redacted:hostname>` stay readable). `id` is `sha256:`
followed by the hex SHA-256 of that encoding with `id` empty and every
`duration_ms` zeroed. Re-encoding a decoded file, or collecting the same
fixture root with the same `--now`, therefore gives the same bytes and the
same id. Redaction re-seals the snapshot, so a redacted copy has its own id.

### Validation limits

`snapshot.Decode` and `snapshot.Validate` treat every file as untrusted. They
are applied to every imported file (`atlas serve --snapshot`, the import
dialog, `atlas lesson --snapshot`, `atlas snapshot …`) and, in tests, to
every fixture.

| rule | limit |
|---|---|
| file size | at most 8 MiB (`MaxBytes`); larger input is not read past the limit |
| observations | at most 20 000 (`MaxObservations`) |
| any string (values, cells, column names, notes, sources) | at most 4096 bytes |
| tables | at most 2 000 rows, at most 64 columns per row, flat cells only |
| unknown fields | rejected, at the top level and in every observation; trailing data after the object is rejected |
| ids | match the pattern and are unique |
| enums | `status`, `kind`, `scope`, `unit`, `source.type`, `collection.mode`, run `status`, `redaction.policy`, redaction classes |
| timestamps | parse as RFC 3339 |
| `value` | null exactly when status is not `ok`; finite numbers only; integers outside int64 rejected |
| catalog types | for an id the catalog declares, the value has the declared type, and a table uses only declared columns |
| redaction | `applied` agrees with `policy` (`none` ⇔ not applied) |

Ids the catalog does not know are accepted (a newer version may have
collected them) and the interface shows them as "not in this version's
catalog". Validation reports every problem at once, each naming where it is:
`observations[12] (mem.meminfo.MemTotal).value: is null but status is ok`.

A snapshot is data. Nothing in one is executed, interpolated into a command,
used as a file system path, or rendered as markup; the browser builds every
element with `createElement` and `textContent`.

## Redaction

The catalog gives every id a sensitivity class:

| class | meaning | ids today |
|---|---|---|
| `none` | harmless configuration and measurements | almost everything |
| `hostname` | the machine's name | `os.hostname` |
| `username` | a user name | none collected directly; found by pattern |
| `path` | a file system path that may contain user or machine names | `mem.cgroup.path`, `mounts.filesystems` (whose mount points are already reduced, and whose options are allow-listed, at collection) |
| `address` | MAC or IP addresses | the `mac` column of `net.interfaces`, `address` of `net.addresses.*`, `destination` and `gateway` of `net.routes.*`; elsewhere found by pattern |
| `serial` | serial numbers, UUIDs, machine ids | none collected; found by pattern |
| `freetext` | text written by firmware, vendors or the kernel build: not secret, but specific enough to narrow a machine down | `cpu.model_name`, `kernel.version_string`, `virt.dmi.sys_vendor`, `virt.dmi.product_name`, `virt.devicetree.model`, the `driver` column of `net.interfaces` |

A table's class applies to every string cell unless the catalog gives a
column its own (`column_sensitivity`): the network tables keep an address
next to its computed `class`, `scope` and flags, and only the address
column is of class `address`.

`snapshot.Redact(s, policy)` returns a new, re-sealed snapshot and the list of
changes; `snapshot.Preview(s, policy)` returns the same list without changing
anything. Each change is `{id, field, class, before, after}`, where `field`
says where the string was (`value`, `value[2].name`, `note`, `source.path`,
`collection.collectors[0].error`).

Policies:

- **`default`** (applied to every snapshot atlas writes unless told
  otherwise). Values and table cells of class `hostname`, `username`,
  `address` and `serial` are replaced wholesale with `<redacted:CLASS>`:
  every address, MAC, gateway and route destination, whatever its value,
  while the computed address class survives. Every other string in the
  snapshot (values, table cells, notes, sources, collector errors) is
  scrubbed of these patterns, in order:
  `/home/<x>` → `/home/<user>`, `/Users/<x>`, `/run/user/<n>`,
  `user-<n>.slice`, `@<n>.service`, `user@host` → `<user>@<host>`,
  UUIDs → `<uuid>`, runs of 32 or more hex digits (machine and boot ids) →
  `<id>`, MAC addresses → `<mac>`, MAC-derived interface names
  (`enx001122334455`) → `enx<mac>`, Docker bridge and veth names
  (`br-<12 hex>`, `veth<7 hex>`) → `br-<id>`, `veth<id>`, IPv4 and IPv6
  addresses → `<ipv4>`, `<ipv6>`. The machine's hostname (three characters or
  longer) is also replaced wherever it appears as a whole word inside other
  text, such as a self-built kernel's version string; values of class `none`
  are spared that search so a distribution id that happens to equal the
  hostname survives. A redacted observation keeps status `ok` and gains the
  note `redacted (CLASS, …)`. Redacting a redacted snapshot changes nothing:
  placeholders contain `<`, which no pattern accepts.
- **`strict`**: `default`, plus `freetext` values and columns replaced with
  `<redacted:freetext>`, and the identifying columns of tables replaced with
  numbered placeholders that keep rows distinguishable: per observation and
  column (`<name-1>`, `<name-2>`, …), or, for names several tables share,
  once per snapshot in a shared namespace (`shared_identifiers`). Interfaces
  use `if`: the `name` and `master` columns of `net.interfaces` and the
  `interface` column of the address, route and qdisc tables map one
  interface to the same `<if-N>`. Block devices use `dev`: the `name` of
  `block.devices`, `block.lsblk`, `block.queues`, `block.stats`,
  `block.bdi` and `mem.swap.areas` and the `device` and `disk` columns of
  `mounts.filesystems` map one device to the same `<dev-N>`. Derived facts
  that join these tables (the default route's interface, MTU and qdisc; the
  root filesystem's disk, scheduler and counters) still work on a strict
  snapshot.
- **`none`**: unchanged. `atlas collect --redact none` prints a warning.

The collectors themselves keep identifying data out before redaction runs:
mount points outside a fixed list of standard paths become `<path>`, swap
files are listed as `<file>` without their path, probe paths are reduced to
basenames, serial numbers and UUIDs are never read, socket tables are reduced
to counts by state (no address or port of any connection is kept), mount
options are reduced to an allow-list (never paths, subvolume names or user
ids), and interface and block device names never appear in a source path or
a note, where they could not be numbered.

Redaction knows only its classes and patterns. It cannot promise that a
snapshot is anonymous; the preview exists so a person can read what will be
shared before it is written.

## Derived facts

`derive.All(snapshot) []Fact` computes facts that no interface reports
directly but that follow from observations, by arithmetic (MemTotal minus
MemAvailable) or by inference (the page-table depth from the architecture, a
CPU flag and an address width). It is a pure function of the observations: no
clock, no environment, no map order. Facts are recomputed every time and never
stored in a snapshot, so a snapshot plus an application version determines
them.

```go
type Fact struct {
    ID     string   `json:"id"`               // "derived.…"
    Value  any      `json:"value"`
    Unit   string   `json:"unit,omitempty"`
    Status string   `json:"status"`           // "ok" | "unknown"
    Reason string   `json:"reason,omitempty"` // when unknown: which input was missing and its status
    Rule   string   `json:"rule"`             // the formula or inference rule, with this machine's specifics
    Inputs []string `json:"inputs"`           // observation ids read
    Basis  string   `json:"basis"`            // "arithmetic" | "inference"
}
```

A fact whose inputs are not all `ok` is `unknown` with the reason
(`mem.zones is permission denied`), never a guess. A few facts are unknown for
a reason of their own: `derived.swap.used_fraction` is undefined when no swap
is configured, and `derived.env.kind` is unknown when too few signals could be
read to claim there is no evidence of virtualization. An `inference` fact is
never presented as observed: the interface labels it DERIVED and shows the
rule.

The table below is generated from `derive.All` on the `desktop-x86_64`
fixture, so every id, rule and input list is exactly what this version
computes. Rules include machine-specific details where the rule text does (the
number of zones, which layout fields differed between probe runs).

| id | title | basis | unit | rule (as computed for desktop-x86_64) | inputs |
|---|---|---|---|---|---|
| `derived.cgroup.cpu_limited` | CPU quota set | arithmetic |  | the quota in cpu.max (or cpu.cfs_quota_us on cgroup v1) is a number rather than max; no cpu controller for the cgroup means no quota at this level | `sched.cgroup.cpu.max_quota_us` |
| `derived.cgroup.cpu_quota_cpus` | CPU quota in CPUs | arithmetic |  | quota ÷ period from cpu.max (cpu.cfs_quota_us ÷ cpu.cfs_period_us on cgroup v1) | `sched.cgroup.cpu.max_quota_us`, `sched.cgroup.cpu.max_period_us` |
| `derived.cgroup.cpu_throttled_fraction` | Share of periods throttled | arithmetic | fraction | nr_throttled ÷ nr_periods from cpu.stat | `sched.cgroup.cpu.nr_throttled`, `sched.cgroup.cpu.nr_periods` |
| `derived.cgroup.memory_limited` | Memory limited by cgroup | arithmetic |  | memory.max or memory.high of atlas's cgroup is a number rather than "max" | `mem.cgroup.memory.max`, `mem.cgroup.memory.high` |
| `derived.commit.over_limit` | Committed beyond limit | arithmetic |  | Committed_AS > CommitLimit | `mem.meminfo.Committed_AS`, `mem.meminfo.CommitLimit` |
| `derived.cpu.freq.available` | Frequency scaling visible | inference |  | true when /sys/devices/system/cpu/cpufreq has at least one policy; a missing or empty directory means the kernel does not control frequency here | `cpu.freq.policies` |
| `derived.cpu.freq.boost_enabled` | Boost allowed | arithmetic |  | cpufreq/boost is 1 (acpi-cpufreq, amd-pstate), or else intel_pstate/no_turbo is 0; here from intel_pstate/no_turbo | `cpu.freq.boost`, `cpu.freq.intel_pstate.no_turbo` |
| `derived.cpu.freq.driver` | Frequency scaling driver | arithmetic |  | the scaling_driver of every cpufreq policy, or mixed when they differ | `cpu.freq.policies` |
| `derived.cpu.freq.governor` | Frequency governor | arithmetic |  | the scaling_governor of every cpufreq policy, or mixed when they differ | `cpu.freq.policies` |
| `derived.cpu.hybrid` | Cores of different kinds | inference |  | true when /sys/devices/cpu_core/cpus lists performance cores (hybrid x86), or when cpu_capacity differs between CPUs (arm big.LITTLE and similar); false when neither; here no hybrid core list exists and the kernel publishes no CPU capacities, so the cores are taken to be alike | `cpu.hybrid.performance_cpus`, `cpu.topology` |
| `derived.cpu.isolated_count` | Isolated CPUs | arithmetic | count | number of CPUs in /sys/devices/system/cpu/isolated | `cpu.isolated` |
| `derived.cpu.phys_addressable_bytes` | Physically addressable bytes | arithmetic | bytes | 2^cpu.address_bits.physical | `cpu.address_bits.physical` |
| `derived.cpu.smt` | Simultaneous multithreading | arithmetic |  | cpu.threads_per_core > 1 | `cpu.threads_per_core` |
| `derived.dirty.background_threshold_estimate_bytes` | Background writeback threshold (estimate) | arithmetic | bytes | vm.dirty_background_bytes is 0, so vm.dirty_background_ratio applies: vm.dirty_background_ratio% × dirtyable memory, dirtyable memory = MemFree − the zones' high watermarks summed (at most MemFree) + Active(file) + Inactive(file), provided it stays below the dirty threshold (otherwise the kernel uses half of that). The dirty threshold it is compared with: vm.dirty_bytes is 0, so vm.dirty_ratio applies: vm.dirty_ratio% × dirtyable memory, dirtyable memory = MemFree − the zones' high watermarks summed (at most MemFree) + Active(file) + Inactive(file). An estimate of the global limit: the kernel also subtracts each zone's lowmem reserve, leaves out highmem (32-bit kernels, unless vm.highmem_is_dirtyable), gives real-time writers a quarter more, and applies per-cgroup and per-device limits below this one; none of that is counted here | `kernel.page_size_bytes`, `mem.sysctl.vm.dirty_bytes`, `mem.sysctl.vm.dirty_ratio`, `mem.meminfo.MemFree`, `mem.zones`, `mem.meminfo.Active_file`, `mem.meminfo.Inactive_file`, `mem.sysctl.vm.dirty_background_bytes`, `mem.sysctl.vm.dirty_background_ratio` |
| `derived.dirty.kernel_background_threshold_bytes` | Background writeback threshold (kernel) | arithmetic | bytes | nr_dirty_background_threshold (pages) × kernel.page_size_bytes | `mem.vmstat.nr_dirty_background_threshold`, `kernel.page_size_bytes` |
| `derived.dirty.kernel_threshold_bytes` | Dirty threshold (kernel) | arithmetic | bytes | nr_dirty_threshold (pages) × kernel.page_size_bytes | `mem.vmstat.nr_dirty_threshold`, `kernel.page_size_bytes` |
| `derived.dirty.threshold_estimate_bytes` | Dirty threshold (estimate) | arithmetic | bytes | vm.dirty_bytes is 0, so vm.dirty_ratio applies: vm.dirty_ratio% × dirtyable memory, dirtyable memory = MemFree − the zones' high watermarks summed (at most MemFree) + Active(file) + Inactive(file). An estimate of the global limit: the kernel also subtracts each zone's lowmem reserve, leaves out highmem (32-bit kernels, unless vm.highmem_is_dirtyable), gives real-time writers a quarter more, and applies per-cgroup and per-device limits below this one; none of that is counted here | `kernel.page_size_bytes`, `mem.sysctl.vm.dirty_bytes`, `mem.sysctl.vm.dirty_ratio`, `mem.meminfo.MemFree`, `mem.zones`, `mem.meminfo.Active_file`, `mem.meminfo.Inactive_file` |
| `derived.dirty.threshold_used_fraction` | Dirty data against the threshold | arithmetic | fraction | (nr_dirty + nr_writeback) / nr_dirty_threshold, all from /proc/vmstat | `mem.vmstat.nr_dirty`, `mem.vmstat.nr_writeback`, `mem.vmstat.nr_dirty_threshold` |
| `derived.env.arch_family` | Architecture family | inference |  | kernel.arch "x86_64" belongs to the x86 family (x86: x86_64, i?86; arm: aarch64, arm*; riscv: riscv*) | `kernel.arch` |
| `derived.env.kind` | Environment kind | inference |  | no-virtualization-evidence: no container marker file, systemd recorded no container manager, PID 1 is an init system ("systemd"), and neither the CPU flags, DMI nor the device tree name a hypervisor. This is not proof of bare metal: a container whose PID 1 is an init other than systemd shows none of these signals, and a hypervisor or runtime can hide every one of them (not available: virt.devicetree.model) | `virt.container.dockerenv`, `virt.container.containerenv`, `virt.container.systemd`, `init.pid1_comm`, `virt.pid1_cgroup_root`, `cpu.flags.hypervisor`, `virt.dmi.sys_vendor`, `virt.dmi.product_name`, `virt.devicetree.model` |
| `derived.mem.accounted_bytes` | Accounted memory | arithmetic | bytes | MemFree + Buffers + Cached + AnonPages + Slab + KernelStack + PageTables + Percpu + VmallocUsed | `mem.meminfo.MemFree`, `mem.meminfo.Buffers`, `mem.meminfo.Cached`, `mem.meminfo.AnonPages`, `mem.meminfo.Slab`, `mem.meminfo.KernelStack`, `mem.meminfo.PageTables`, `mem.meminfo.Percpu`, `mem.meminfo.VmallocUsed` |
| `derived.mem.available_fraction` | Available fraction | arithmetic | fraction | MemAvailable / MemTotal | `mem.meminfo.MemTotal`, `mem.meminfo.MemAvailable` |
| `derived.mem.file_cache_bytes` | File cache | arithmetic | bytes | Cached + Buffers − Shmem | `mem.meminfo.Cached`, `mem.meminfo.Buffers`, `mem.meminfo.Shmem` |
| `derived.mem.total_pages` | Usable RAM in pages | arithmetic | pages | MemTotal / kernel.page_size_bytes | `mem.meminfo.MemTotal`, `kernel.page_size_bytes` |
| `derived.mem.unaccounted_bytes` | Unaccounted memory | arithmetic | bytes | MemTotal − (MemFree + Buffers + Cached + AnonPages + Slab + KernelStack + PageTables + Percpu + VmallocUsed); what remains is memory meminfo attributes to none of these, typically the hugetlb pool (Hugetlb), swap-cache pages no process maps, the zswap pool (Zswap), driver allocations such as graphics and network buffers, and per-CPU page lists. Overlaps between fields can make it negative | `mem.meminfo.MemFree`, `mem.meminfo.Buffers`, `mem.meminfo.Cached`, `mem.meminfo.AnonPages`, `mem.meminfo.Slab`, `mem.meminfo.KernelStack`, `mem.meminfo.PageTables`, `mem.meminfo.Percpu`, `mem.meminfo.VmallocUsed`, `mem.meminfo.MemTotal` |
| `derived.mem.used_estimate_bytes` | Memory in use (estimate) | arithmetic | bytes | MemTotal − MemAvailable | `mem.meminfo.MemTotal`, `mem.meminfo.MemAvailable` |
| `derived.net.conntrack.fill_fraction` | Connection table fill | arithmetic | fraction | nf_conntrack_count / nf_conntrack_max | `net.conntrack.count`, `net.conntrack.max` |
| `derived.net.default_route` | Default route present | inference |  | a route for every destination (prefix length zero) exists, IPv4 or IPv6, in the routing tables atlas can read (here IPv4) | `net.routes.ipv4` |
| `derived.net.default_route_bdp_100ms_bytes` | Bandwidth-delay product at 100 ms | arithmetic | bytes | speed_mbps × 10⁶ / 8 × 0.1 s: the bytes in flight that fill the default route's link at a 100 ms round trip | `net.routes.ipv4`, `net.interfaces` |
| `derived.net.default_route_interface` | Default route interface | inference |  | the interface of the IPv4 default route with the lowest metric (the IPv6 one when IPv4 has none); policy routing rules, which atlas cannot read, can send some traffic elsewhere | `net.routes.ipv4` |
| `derived.net.default_route_mtu` | Default route MTU (bytes) | inference | count | the MTU in net.interfaces of the default route's interface | `net.routes.ipv4`, `net.interfaces` |
| `derived.net.default_route_qdisc` | Default route queueing discipline | inference |  | the queueing discipline that schedules the default route's interface in net.qdiscs: its root qdisc; under a multiqueue root (mq) the per-queue qdiscs; for a bond or bridge without a queue (noqueue), its members' qdiscs (here the root, fq_codel) | `net.routes.ipv4`, `net.qdiscs` |
| `derived.net.default_route_speed_mbps` | Default route link speed (Mbit/s) | inference | count | the link speed (Mbit/s) in net.interfaces of the default route's interface | `net.routes.ipv4`, `net.interfaces` |
| `derived.net.default_route_tx_queues` | Default route transmit queues | inference | count | the transmit queue count in net.interfaces of the default route's interface | `net.routes.ipv4`, `net.interfaces` |
| `derived.net.default_route_type` | Default route link type | inference |  | the type column of net.interfaces for the interface of the IPv4 default route with the lowest metric (the IPv6 one when IPv4 has none); policy routing rules, which atlas cannot read, can send some traffic elsewhere | `net.routes.ipv4`, `net.interfaces` |
| `derived.net.ephemeral_ports` | Ephemeral ports | arithmetic | count | ip_local_port_range high − low + 1 | `net.sysctl.ipv4.ip_local_port_range.low`, `net.sysctl.ipv4.ip_local_port_range.high` |
| `derived.net.global_address` | Has a global address | inference |  | an IPv4 or IPv6 address of class global exists on some interface | `net.addresses.ipv4`, `net.addresses.ipv6` |
| `derived.net.global_ipv6` | Has a global IPv6 address | inference |  | an IPv6 address of class global exists on some interface | `net.addresses.ipv6` |
| `derived.net.interface_count` | Network interfaces | arithmetic | count | rows of net.interfaces | `net.interfaces` |
| `derived.net.multiqueue` | Multiqueue interfaces present | arithmetic |  | some interface in net.interfaces has more than one transmit queue | `net.interfaces` |
| `derived.net.physical_interfaces` | Physical interfaces | arithmetic | count | rows of net.interfaces whose type is ethernet or wireless | `net.interfaces` |
| `derived.net.softnet.dropped` | Receive backlog drops | arithmetic | count | sum of the dropped column of net.softnet over 16 CPUs | `net.softnet` |
| `derived.net.softnet.time_squeeze` | Receive softirq squeezes | arithmetic | count | sum of the time_squeeze column of net.softnet over 16 CPUs | `net.softnet` |
| `derived.net.tcp.bbr_available` | BBR available | inference |  | bbr is listed in tcp_available_congestion_control (a module not yet loaded is not listed) | `net.sysctl.ipv4.tcp_available_congestion_control` |
| `derived.net.tcp.mem_pressure_bytes` | TCP memory pressure threshold | arithmetic | bytes | tcp_mem pressure (pages) × kernel.page_size_bytes | `net.sysctl.ipv4.tcp_mem.pressure`, `kernel.page_size_bytes` |
| `derived.net.tcp.mem_used_bytes` | TCP buffer memory in use | arithmetic | bytes | sockstat TCP mem (pages) × kernel.page_size_bytes | `net.sockstat.tcp_mem`, `kernel.page_size_bytes` |
| `derived.net.tcp.retrans_fraction` | TCP retransmission ratio | arithmetic | fraction | RetransSegs / OutSegs since boot | `net.snmp.Tcp.RetransSegs`, `net.snmp.Tcp.OutSegs` |
| `derived.net.tcp.rmem_covers_bdp` | Receive ceiling covers the link at 100 ms | arithmetic |  | tcp_rmem max ≥ the default route link's bandwidth-delay product at 100 ms (speed_mbps × 10⁶ / 8 × 0.1 s) | `net.sysctl.ipv4.tcp_rmem.max`, `net.routes.ipv4`, `net.interfaces` |
| `derived.net.tcp.rmem_max_mbps_at_100ms` | Receive rate the buffer ceiling allows at 100 ms (Mbit/s) | arithmetic | count | tcp_rmem max × 8 / 100 ms / 10⁶: the receive rate, in Mbit/s, of one connection whose window is the whole buffer ceiling, at a 100 ms round trip | `net.sysctl.ipv4.tcp_rmem.max` |
| `derived.numa.multi` | Multiple NUMA nodes | arithmetic |  | more than one NUMA node | `mem.numa.nodes` |
| `derived.numa.node_count` | NUMA node count | arithmetic | count | number of rows in mem.numa.nodes | `mem.numa.nodes` |
| `derived.oom.any` | Any OOM kill | arithmetic |  | mem.vmstat.oom_kill > 0 | `mem.vmstat.oom_kill` |
| `derived.oom.kills_since_boot` | OOM kills since boot | arithmetic | count | mem.vmstat.oom_kill | `mem.vmstat.oom_kill` |
| `derived.overcommit.mode_name` | Overcommit mode name | arithmetic |  | overcommit_memory 0 → heuristic, 1 → always, 2 → strict | `mem.sysctl.vm.overcommit_memory` |
| `derived.paging.index_bits` | Index bits per level | arithmetic | bits | log2(kernel.page_size_bytes / 8): a table fills one page with 8-byte entries | `kernel.page_size_bytes` |
| `derived.paging.leaf_table_span_bytes` | Span of one last-level table | arithmetic | bytes | (page size / 8) entries × page size | `kernel.page_size_bytes` |
| `derived.paging.levels` | Page table levels | inference | count | x86_64 without la57 in /proc/cpuinfo: 4-level paging with 48-bit virtual addresses (9+9+9+9 index bits above a 12-bit offset), whatever the CPU itself supports; since Linux 4.17 the kernel clears la57 from the CPU flags at boot unless it enabled 5-level paging (arch/x86/kernel/cpu/common.c, early_identify_cpu), so the flag's presence is the kernel's choice, not only the CPU's ability | `kernel.arch`, `cpu.flags.la57` |
| `derived.paging.offset_bits` | Page offset bits | arithmetic | bits | log2(kernel.page_size_bytes) | `kernel.page_size_bytes` |
| `derived.paging.profile` | Paging profile | inference |  | x86_64 without la57 in /proc/cpuinfo: 4-level paging with 48-bit virtual addresses (9+9+9+9 index bits above a 12-bit offset), whatever the CPU itself supports; since Linux 4.17 the kernel clears la57 from the CPU flags at boot unless it enabled 5-level paging (arch/x86/kernel/cpu/common.c, early_identify_cpu), so the flag's presence is the kernel's choice, not only the CPU's ability | `kernel.arch`, `cpu.flags.la57` |
| `derived.paging.upper_entry_span_bytes` | Span of an entry two levels up | arithmetic | bytes | (page size / 8)² entries × page size | `kernel.page_size_bytes` |
| `derived.paging.user_space_bytes` | User address space size | inference | bytes | 2^47 bytes: the user part of a 48-bit address space (x86_64 without la57 in /proc/cpuinfo: 4-level paging with 48-bit virtual addresses (9+9+9+9 index bits above a 12-bit offset), whatever the CPU itself supports; since Linux 4.17 the kernel clears la57 from the CPU flags at boot unless it enabled 5-level paging (arch/x86/kernel/cpu/common.c, early_identify_cpu), so the flag's presence is the kernel's choice, not only the CPU's ability) | `kernel.arch`, `cpu.flags.la57` |
| `derived.paging.va_bits` | Virtual address bits in use | inference | bits | x86_64 without la57 in /proc/cpuinfo: 4-level paging with 48-bit virtual addresses (9+9+9+9 index bits above a 12-bit offset), whatever the CPU itself supports; since Linux 4.17 the kernel clears la57 from the CPU flags at boot unless it enabled 5-level paging (arch/x86/kernel/cpu/common.c, early_identify_cpu), so the flag's presence is the kernel's choice, not only the CPU's ability | `kernel.arch`, `cpu.flags.la57` |
| `derived.probe.aslr_differs` | Layout differs between runs | arithmetic |  | any of stack, mmap_base, heap, exe differs between the probe's two runs (here: [stack mmap_base]) | `probe.layout.runs` |
| `derived.probe.read_region_rss_bytes` | Read-only region resident size | arithmetic | bytes | rss_bytes of the anon-read region (4 MiB only read from) | `probe.regions` |
| `derived.probe.untouched_rss_bytes` | Untouched region resident size | arithmetic | bytes | rss_bytes of the anon-untouched region (64 MiB mapped, never touched) | `probe.regions` |
| `derived.probe.written_huge_fraction` | Written region in huge pages | arithmetic | fraction | anon_huge_bytes / size_bytes of the anon-written region | `probe.regions` |
| `derived.probe.written_minflt_delta` | Faults taken by the write loop | arithmetic | count | minflt_after_touch − minflt_before_touch (getrusage around the write loop) | `probe.faults.minflt_after_touch`, `probe.faults.minflt_before_touch` |
| `derived.probe.written_pages_expected` | Pages in the written region | arithmetic | pages | size_bytes of anon-written / kernel.page_size_bytes: one fault per page if every page faulted separately | `probe.regions`, `kernel.page_size_bytes` |
| `derived.psi.available` | Pressure stall information available | arithmetic |  | /proc/pressure/memory could be read (mem.psi.some.avg10 is ok); absent means a kernel without CONFIG_PSI or booted with psi=0 | `mem.psi.some.avg10` |
| `derived.reclaim.allocstalls` | Direct reclaim entries since boot | arithmetic | count | allocstall_device + allocstall_dma + allocstall_dma32 + allocstall_movable + allocstall_normal: every allocstall_* counter /proc/vmstat has here; each counts entries into direct reclaim by allocations whose highest allowed zone is that zone; reclaim forced by a memory cgroup's limit is not counted | `mem.vmstat.allocstall_device`, `mem.vmstat.allocstall_dma`, `mem.vmstat.allocstall_dma32`, `mem.vmstat.allocstall_movable`, `mem.vmstat.allocstall_normal` |
| `derived.reclaim.lru_kind` | Reclaim LRU kind | inference |  | mglru if bit 0 of /sys/kernel/mm/lru_gen/enabled is set, otherwise classic; an absent file means an older kernel or one built without MGLRU, both classic | `mem.lru_gen.enabled` |
| `derived.sched.avg_timeslice_us` | Average timeslice | arithmetic | microseconds | Σ run_ns ÷ Σ timeslices over the 16 CPUs of /proc/schedstat, in microseconds | `sched.schedstat.cpus` |
| `derived.sched.busy_since_boot_fraction` | Busy since boot | arithmetic | fraction | 1 − (idle + iowait) ÷ (user + nice + system + idle + iowait + irq + softirq + steal), the all row of /proc/stat; guest time is already inside user and nice | `sched.stat.cpu_times` |
| `derived.sched.ctxt_per_second` | Context switches per second | arithmetic |  | ctxt from /proc/stat ÷ uptime | `sched.stat.ctxt`, `kernel.uptime_seconds` |
| `derived.sched.ext_enabled` | BPF scheduler loaded | inference |  | sched.ext.state is enabled; a kernel without /sys/kernel/sched_ext has no BPF scheduler | `sched.ext.state` |
| `derived.sched.fair_scheduler` | Fair scheduler algorithm | inference |  | eevdf when /proc/self/sched has an se.slice line (only EEVDF gives each task a slice); otherwise from the release: eevdf for Linux 6.6 and later, cfs before; here se.slice is present | `sched.self.slice_us` |
| `derived.sched.load_per_cpu` | Load per CPU | arithmetic |  | 1-minute load average ÷ online CPUs | `sched.loadavg.avg1`, `cpu.logical_count` |
| `derived.sched.self_allowed_count` | CPUs atlas may use | arithmetic | count | number of CPUs in Cpus_allowed_list of /proc/self/status | `sched.self.cpus_allowed_list` |
| `derived.sched.self_weight` | Weight of atlas's nice value | arithmetic |  | sched_prio_to_weight[nice + 20] for SCHED_OTHER and SCHED_BATCH, 3 for SCHED_IDLE; here SCHED_OTHER at nice 0 | `sched.self.policy`, `sched.self.nice` |
| `derived.sched.steal_fraction` | Stolen time since boot | arithmetic | fraction | steal ÷ (user + nice + system + idle + iowait + irq + softirq + steal), the all row of /proc/stat | `sched.stat.cpu_times` |
| `derived.sched.wait_fraction` | Share of runnable time spent waiting | arithmetic | fraction | Σ wait_ns ÷ (Σ run_ns + Σ wait_ns) over the 16 CPUs of /proc/schedstat | `sched.schedstat.cpus` |
| `derived.storage.root_atime` | Root access-time policy | inference |  | noatime or relatime when the options of / name it, otherwise strictatime (the kernel prints no word for it); here the options are "rw,relatime" | `mounts.filesystems` |
| `derived.storage.root_busy_queue_depth` | Root disk requests in flight while busy | arithmetic | count | weighted_ms / io_ms in block.stats of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.stats` |
| `derived.storage.root_disk` | Root disk | inference |  | the disk column of the mount at / in mounts.filesystems (sda, which holds / through sda2) | `mounts.filesystems` |
| `derived.storage.root_disk_kind` | Root disk kind | inference |  | the kernel's naming scheme for the root disk (sda, which holds / through sda2): nvme* NVMe, sd* SCSI (SATA, SAS, USB), vd* virtio, xvd* Xen, mmcblk* MMC, dm-* device-mapper, md* software RAID, zram*, loop*, nbd* | `mounts.filesystems` |
| `derived.storage.root_fstype` | Root filesystem type | arithmetic |  | the fstype of the mount at / in mounts.filesystems | `mounts.filesystems` |
| `derived.storage.root_hw_queues` | Root disk hardware queues | arithmetic | count | the hardware queue count in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_max_request_bytes` | Root disk largest request | arithmetic | bytes | the largest request size in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_nr_requests` | Root disk queue depth (nr_requests) | arithmetic | count | the nr_requests in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_options` | Root mount options | arithmetic |  | the allow-listed options of the mount at / in mounts.filesystems | `mounts.filesystems` |
| `derived.storage.root_read_ahead_bytes` | Root disk readahead window | arithmetic | bytes | the readahead window in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_read_bytes` | Read from the root disk since boot | arithmetic | bytes | sectors read × 512 in block.stats of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.stats` |
| `derived.storage.root_read_latency_seconds` | Average read time on the root disk | arithmetic | seconds | read_ms / reads in block.stats of the root disk (sda, which holds / through sda2), in seconds | `mounts.filesystems`, `block.stats` |
| `derived.storage.root_rotational` | Root disk rotates | arithmetic |  | the rotational flag in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_scheduler` | Root disk I/O scheduler | arithmetic |  | the selected I/O scheduler in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_schedulers` | Root disk schedulers available | arithmetic |  | the available I/O schedulers in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_util_fraction` | Root disk busy fraction since boot | arithmetic | fraction | io_ms in block.stats of the root disk (sda, which holds / through sda2) / (kernel.uptime_seconds × 1000) | `mounts.filesystems`, `block.stats`, `kernel.uptime_seconds` |
| `derived.storage.root_write_cache` | Root disk write cache | arithmetic |  | the write cache mode in block.queues of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.queues` |
| `derived.storage.root_write_latency_seconds` | Average write time on the root disk | arithmetic | seconds | write_ms / writes in block.stats of the root disk (sda, which holds / through sda2), in seconds | `mounts.filesystems`, `block.stats` |
| `derived.storage.root_written_bytes` | Written to the root disk since boot | arithmetic | bytes | sectors written × 512 in block.stats of the root disk (sda, which holds / through sda2) | `mounts.filesystems`, `block.stats` |
| `derived.swap.compressed` | Compressed swap in use | inference |  | active swap (SwapTotal > 0) with zswap enabled, or a zram device listed as an active swap area in /proc/swaps; here swap is active, uncompressed: zswap is off or not built (its parameter file is absent only when zswap is not built into the kernel), no zram block device exists (the zram module may simply not be loaded) | `mem.meminfo.SwapTotal`, `mem.zswap.enabled`, `mem.swap.areas`, `mem.zram.devices` |
| `derived.swap.configured` | Swap configured | arithmetic |  | SwapTotal > 0 | `mem.meminfo.SwapTotal` |
| `derived.swap.total_bytes` | Swap size | arithmetic | bytes | SwapTotal | `mem.meminfo.SwapTotal` |
| `derived.swap.used_fraction` | Swap used fraction | arithmetic | fraction | (SwapTotal − SwapFree) / SwapTotal | `mem.meminfo.SwapTotal`, `mem.meminfo.SwapFree` |
| `derived.thp.enabled` | THP enabled | arithmetic |  | THP mode is not never; an absent THP interface means a kernel built without THP, so no | `mem.thp.enabled` |
| `derived.thp.mode` | THP mode | arithmetic |  | the bracketed choice in /sys/kernel/mm/transparent_hugepage/enabled | `mem.thp.enabled` |
| `derived.zones.free_bytes` | Free memory by zones | arithmetic | bytes | sum of the free column over all 5 zones × page size: the free pages | `mem.zones`, `kernel.page_size_bytes` |
| `derived.zones.high_bytes` | Total high watermark | arithmetic | bytes | sum of the high column over all 5 zones × page size: the high watermarks | `mem.zones`, `kernel.page_size_bytes` |
| `derived.zones.low_bytes` | Total low watermark | arithmetic | bytes | sum of the low column over all 5 zones × page size: the low watermarks | `mem.zones`, `kernel.page_size_bytes` |
| `derived.zones.min_bytes` | Total min watermark | arithmetic | bytes | sum of the min column over all 5 zones × page size: the min watermarks | `mem.zones`, `kernel.page_size_bytes` |

`GET /api/derived` returns the same list for the active snapshot.

## The catalog

`internal/catalog` is the data dictionary: one entry per id, in one file per
namespace (`catalog_mem.go`, `catalog_system.go`, `catalog_sched.go`, `catalog_storage.go`, `catalog_net.go`,
`catalog_probe.go`, `catalog_derived.go`). Each entry has:

| field | meaning |
|---|---|
| `id` | the exact id, or a family ending in `.*` |
| `title`, `description` | what the inspector shows |
| `unit` | as for observations |
| `type` | `int`, `float`, `string`, `bool`, `table`, or `limit` (an integer in the entry's unit, bytes or microseconds, or the string `max` when a cgroup sets no limit: the kernel writes `max`, and inventing a number for "unlimited" would be fabrication) |
| `columns` | a table's columns; a name ending in `*` matches any column with that prefix (`mem.buddyinfo` has `order0`…`orderN`, and N depends on the kernel) |
| `kind`, `scope` | as for observations; empty for derived facts |
| `sensitivity` | the redaction class |
| `identifying_columns` | table columns the strict policy replaces |
| `shared_identifiers` | identifying columns numbered once per snapshot in a namespace shared with other tables (`if` for interface names, `dev` for block devices) |
| `column_sensitivity` | per-column classes overriding `sensitivity` |
| `basis` | `arithmetic` or `inference`, for derived facts only |
| `caveat` | what this number cannot tell you |
| `privileged` | what running as root would add |

Today it declares 341 observation ids, 110 derived facts and two families,
`mem.meminfo.*` (every `/proc/meminfo` line, in bytes) and `mem.vmstat.*`
(every `/proc/vmstat` counter, as counts). Specific entries exist for the
fields lessons use; those can carry a different unit (the reclaim scan and
steal counters and the refault counters are `pages`). Lookup prefers the
exact entry, then the longest matching family.

The catalog is enforced in three places: the collection runner drops or
downgrades any observation with an undeclared id or the wrong type and names
the problem in the run report (and the collector tests fail on it); snapshot
validation type-checks every declared id; and the curriculum lint rejects a
lesson that references an id the catalog does not declare. `GET /api/catalog`
serves the entries for the ids present in the active snapshot, the families,
and the ids the catalog does not know.

## Versioning

Three versions are recorded, and each has its own reason to change.

| version | where | bump when |
|---|---|---|
| `schema_version` (1) | `internal/snapshot.SchemaVersion`, every snapshot | the file format changes incompatibly: a field is added, removed, renamed or changes meaning, an enum gains a value, or a limit tightens. Validation rejects unknown fields, so even an addition is a new version. New observation ids are *not* a schema change: unknown ids are valid. |
| `curriculum_version` | `curriculum/roadmap.json` | lesson text, front matter, the roadmap or the component graph change in a way a reader would notice (new lessons, rewritten sections, changed relevance rules). Typo fixes may share a version. |
| `app_version` (0.2.0) | `internal/version.Version`, recorded in every snapshot | any release. Within 0.x a minor bump may change ids, derived rules and output; after 1.0, removing or renaming an id or changing a derived rule is a major change. |

What is deterministic, and across what:

- **Snapshot bytes and id.** The same fixture root, `--now` and app version
  produce a byte-identical snapshot (the golden test in `cmd/atlas` checks all
  six). A live collection is not reproducible, by nature.
- **Derived facts.** A pure function of the observations and the app version.
  An older snapshot read by a newer atlas gets the newer rules; a fact whose
  inputs the older snapshot lacks is unknown, with "not in this snapshot" as
  the reason.
- **Bound lessons.** The same snapshot, curriculum version and app version
  give byte-identical bound lessons (`atlas lesson --json`, `GET
  /api/lessons/<id>`): formatting happens in Go, nothing reads a clock, and no
  output depends on map order. Goldens in `cmd/atlas/testdata/lessons` guard
  two of them, and a determinism test binds every lesson twice on every embedded snapshot.
- **Selection and components.** `Select`, `Components`, `Roadmap` and `Usage`
  are pure functions of the curriculum and the snapshot.

Nothing is guaranteed across app versions except that a schema v1 snapshot
written by any 0.1.x atlas loads in any later atlas that reads schema v1.
