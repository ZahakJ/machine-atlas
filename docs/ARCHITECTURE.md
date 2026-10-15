# Architecture

How `atlas` is built, from collection to the browser. Section numbers are
cited from code comments (`ARCHITECTURE.md §7`); keep them stable.

## 1. What it is

An interactive textbook of the Linux computer it runs on. One Go binary,
`atlas`, collects observations from the machine, normalises them into a
versioned snapshot, binds authored lessons to that snapshot, and serves a local
web UI with three connected views: **Learn**, **Explore**, **Inspect**.
It needs no root and no network.

## 2. Stack

- **Go, standard library + `golang.org/x/sys/unix`.** Raw syscalls for the
  probe and experiments (`mmap`, `mincore`, `memfd_create`, `posix_fadvise`);
  one static binary.
- **Web UI: plain ES modules and CSS, no framework, no build step, no npm
  dependencies.** Served from the binary with `go:embed`. Pure logic modules
  are tested with `node --test`.
- **Lessons: Markdown files with a JSON front matter**, parsed and bound by Go
  into a typed block tree; the browser renders that tree with DOM APIs only
  (never `innerHTML` with data), so collected strings cannot inject markup.

Go version: the `go` line of `go.mod` (1.25); CI builds with exactly that.
Targets: `linux/amd64` and `linux/arm64` fully; `linux/riscv64` best effort.
Non-Linux builds (`darwin`, …) compile and serve snapshots and fixtures
only (collection, probe and experiments live behind `//go:build linux` with
stubs that report `unsupported`).

## 3. Layers and repository layout

Four layers, each depending only on the ones above it in this list:

```
collection   → observations            internal/obs, internal/collect/...
modeling     → snapshot, derived facts internal/snapshot, internal/derive, internal/catalog
curriculum   → lessons, graph, binding internal/curriculum   (+ curriculum/ content)
presentation → HTTP API + web UI       internal/server, web/
```

```
cmd/atlas/                 CLI entry point (thin; flag parsing and wiring only); testdata/lessons/ bound-lesson goldens
internal/obs/              Observation, Status, Kind, Scope, Source types
internal/catalog/          data dictionary: every known observation/derived id
internal/collect/          Collector interface, Env (file/command/clock access), runner, limits
internal/collect/memory/   memory collectors
internal/collect/system/   cpu, os, kernel, init, virt, block, mounts, session collectors
internal/collect/sched/    CPU topology and caches, cpufreq, load and CPU time, schedstat, CPU pressure, scheduler settings, atlas's own scheduling, cpu cgroup
internal/collect/storage/  request queues and schedulers, I/O statistics, BDI writeback settings, I/O pressure, filesystem types
internal/collect/network/  interfaces, addresses, routes, sockets, counters, softnet, qdiscs, conntrack, net sysctls
internal/collect/probe/    the controlled child process (linux only) + fixture reader
internal/collect/all/      the explicit list of registered collectors
internal/snapshot/         schema v1, canonical encoding, validation, redaction, preview
internal/derive/           derived facts (pure functions of a snapshot)
internal/curriculum/       lesson parser, graph, selection, binding, components, roadmap, lint
internal/experiment/       opt-in experiments (linux only) run in a child process
internal/server/           HTTP API, embedded UI, request guards
internal/version/          the app version constant
web/                       index.html, css/, js/ (app, views, render/, sim/, diagrams/, lib/), tests in web/test/,
                           dev/ (design-system, diagram and simulation pages; not embedded)
curriculum/                lessons/*.md, roadmap.json, components.json (embedded)
fixtures/roots/<name>/     fake machine trees (proc/, sys/, etc/, _meta.json, _cmd/, _probe/, README.md)
fixtures/snapshots/        golden snapshots generated from the roots, plus live-sample.json (embedded)
docs/                      ARCHITECTURE.md, DATA-MODEL.md, CONTRIBUTING.md, REFERENCES.md, media/
scripts/                   shot.sh + shot.py (screenshots, one browser at a time), fixtures-gen/ (writes fixtures/roots),
                           docgen/ (go run ./scripts/docgen: docs/REFERENCES.md and the DATA-MODEL.md fact table)
.github/workflows/ci.yml   CI: checks, tests, cross builds, a real collection on the runner
```

Go module path: `github.com/ZahakJ/machine-atlas`. Version constant in
`internal/version` (`0.2.0`). Embedding: `curriculum/embed.go`,
`web/embed.go`, `fixtures/embed.go` each export an `embed.FS` named `FS`
(`fixtures.FS` holds `snapshots/*.json` and `roots/*/_meta.json` only).

## 4. Observations

```go
package obs

type Status string
const (
    StatusOK               Status = "ok"
    StatusAbsent           Status = "absent"            // the interface answered: this thing does not exist here (ENOENT, empty /proc/swaps)
    StatusPermissionDenied Status = "permission_denied" // EACCES/EPERM
    StatusUnsupported      Status = "unsupported"       // not applicable on this arch/kernel/tool version, by rule
    StatusFailed           Status = "failed"            // tried; parse error, timeout, unexpected format
    StatusNotCollected     Status = "not_collected"     // deliberately skipped (flag, collector disabled, snapshot mode)
)

type Kind string   // "static" (configuration; stable across a boot or longer) | "live" (measurement; varies)
type Scope string  // "kernel" (system-wide as seen by this kernel instance) | "cgroup" (the cgroup atlas ran in)
                   // | "process" (the atlas process or its probe child) | "namespace" (visible from our namespaces)

type Source struct {
    Type   string `json:"type"`             // "file" | "command" | "syscall" | "probe"
    Path   string `json:"path,omitempty"`   // "/proc/meminfo"; for commands the argv joined by spaces
    Field  string `json:"field,omitempty"`  // original field name in the source, e.g. "Active(anon)"
    Detail string `json:"detail,omitempty"`
}

type Observation struct {
    ID          string  `json:"id"`
    Value       any     `json:"value"`                 // number (int64/float64), string, bool, []map[string]any (a table), or null
    Unit        string  `json:"unit,omitempty"`        // "bytes" | "pages" | "count" | "bits" | "percent" | "seconds" | "microseconds" | "fraction" (derived *_fraction facts: 0–1, shown as a percentage) | ""
    Kind        Kind    `json:"kind"`
    Scope       Scope   `json:"scope"`
    Status      Status  `json:"status"`
    Source      Source  `json:"source"`
    CollectedAt string  `json:"collected_at"`          // RFC 3339 UTC, second precision
    Collector   string  `json:"collector"`
    Note        string  `json:"note,omitempty"`        // why the status is what it is, or a caveat specific to this reading
}
```

Rules:

- IDs match `^[a-z][A-Za-z0-9_.-]*$`, dotted, namespaced (`mem.`, `cpu.`,
  `kernel.`, `os.`, `init.`, `virt.`, `block.`, `mounts.`, `fs.`, `net.`, `session.`,
  `sched.`, `probe.`). Kernel field names keep their case; `(`→`_`, `)` dropped
  (`Active(anon)` → `mem.meminfo.Active_anon`), any other character outside
  `[A-Za-z0-9_.-]` becomes `_`; the original goes in `source.field`.
- Values in kB are converted to **bytes** at collection; the raw unit is noted
  in `source.detail`. Tables are arrays of flat objects with string/number/bool
  cells.
- A collector that cannot produce an id it owns must still emit it with a
  non-`ok` status and `value: null`. Mapping: ENOENT → `absent`;
  EACCES/EPERM → `permission_denied`; rule-based inapplicability (x86-only flag
  on arm64, cgroup v1 hierarchy where v2 files are expected, PSI on a kernel
  built without it when the kernel is known to lack it) → `unsupported`;
  anything else → `failed` (`obs.FromError`; ENOTDIR and a missing executable
  are `absent`, `errors.ErrUnsupported` is `unsupported`). When ENOENT cannot
  be told apart from "kernel built without the feature", use `absent` and say
  so in `note`.
- A value is never fabricated or defaulted. No "0" for unknown.
- Every id is declared in `internal/catalog` (`catalog.Entry`: title,
  description, unit, value type `int|float|string|bool|table|limit`, table
  `columns` (a trailing `*` matches a prefix), kind, scope, sensitivity class,
  `identifying_columns` for strict redaction, `basis` for derived facts,
  caveat = what this number cannot tell you, privileged = what root would
  add). Type `limit` is an integer in the entry's unit (bytes, or
  microseconds for the CPU quota) or the string `"max"` (cgroup limits). Wildcard entries exist for the open-ended families
  `mem.meminfo.*` (bytes) and `mem.vmstat.*` (count), with specific entries
  for the fields lessons use; `catalog.Lookup` prefers the exact entry, then
  the longest family. The runner drops or downgrades to `failed` any
  undeclared, unowned or mistyped observation and names the problem in the
  run report, and the collector tests fail on it.

### Collectors

```go
package collect

type Env interface {
    ReadFile(path string) ([]byte, error)        // bounded to MaxFileBytes (1 MiB); absolute machine paths
    ReadDir(path string) ([]string, error)
    Readlink(path string) (string, error)
    Stat(path string) (exists bool, err error)
    Run(ctx context.Context, name string, args ...string) ([]byte, error) // no shell; fixed argv; LC_ALL=C; cleared env; output capped at 1 MiB
    Getenv(name string) (string, bool)           // only collect.AllowedEnv names
    Uname() (Uname, error)                       // {Machine, Release}
    PageSize() (int, error)
    Now() time.Time
    Live() bool                                  // false when reading a fixture root
}

type Collector interface {
    ID() string                 // "memory.meminfo"
    Owns() []string             // exact ids (always emitted) or "prefix.*" families
    Collect(ctx context.Context, env Env) []obs.Observation
}

type TimeoutCollector interface { Collector; Timeout() time.Duration } // the probe: 5 s
```

`Run` looks the command up in `/usr/sbin:/usr/bin:/sbin:/bin` only, with an
environment of `LC_ALL=C` and that `PATH`; a non-zero exit is a
`*collect.ExitError` with capped stderr. `ReadDir` returns sorted names.
`collect.Skip(c, reason)` wraps a disabled collector so its exact ids are
reported `not_collected`.

- `LiveEnv` reads the real machine. `FixtureEnv` (`collect.OpenFixture`) maps
  paths under `fixtures/roots/<name>/`; `_meta.json` there holds
  `description`, `now`, `uname` (`machine`, `release`), `page_size`, `deny`
  (paths that return EACCES), `env`, and `commands` (§12).
- The runner runs collectors sequentially (system, CPU and scheduler, memory,
  storage, network, then the probe)
  with a 2 s timeout each (probe: 5 s) and a 15 s total budget (collectors not
  started in time report `not_collected`), recovers panics and timeouts into
  `failed` observations, fills kind and scope from the catalog, sorts
  observations by id, and records per-collector status (`ok` | `partial` |
  `failed` | `skipped`), duration and contract problems.
- Collection is read-only. The single exception, disabled with `--no-probe`, is the probe child (below), which maps about
  84 MiB, of which about 13 MiB becomes resident, for under a second, and
  writes no files.
- Never collected: process environments (except the allow-listed
  `XDG_SESSION_TYPE`, `XDG_CURRENT_DESKTOP`, `WAYLAND_DISPLAY` *presence*,
  `DISPLAY` *presence* of the atlas process itself), command lines, file
  contents outside the listed interfaces, serial numbers, UUIDs, usernames,
  other processes' memory maps, and anything per connection: the socket
  tables are read only to count sockets by state. The machine's own MAC and
  IP addresses, routes and gateways are collected, and every redaction policy
  replaces them (§5).

### Observation ids

Memory (`internal/collect/memory`):

| ids | source | notes |
|---|---|---|
| `mem.meminfo.<Field>` | `/proc/meminfo` | every field; bytes (counts for `HugePages_*`); live |
| `mem.vmstat.<key>` | `/proc/vmstat` | every key (≤ 400, more is `failed`); count, except the declared page counters (`pgscan_*`, `pgsteal_*`, `pswpin`, `pswpout`, `zswpin`, `zswpout`, `zswpwb`, `workingset_refault_*`, `nr_dirty`, `nr_writeback`, `nr_dirtied`, `nr_written`, `nr_dirty_threshold`, `nr_dirty_background_threshold`): pages; live |
| `mem.zones` (table: node, zone, free, min, low, high, spanned, present, managed — pages) | `/proc/zoneinfo` | live |
| `mem.buddyinfo` (table: node, zone, order0…orderN) | `/proc/buddyinfo` | live |
| `mem.swap.areas` (table: name, type, size_bytes, used_bytes, priority) | `/proc/swaps` | empty table with status `ok` when the file exists but lists nothing; `name` is a device name, or `<file>` for swap files (the path is not kept) |
| `mem.sysctl.vm.<name>` for swappiness, overcommit_memory, overcommit_ratio, min_free_kbytes, watermark_scale_factor, dirty_ratio, dirty_background_ratio, dirty_bytes, dirty_background_bytes (bytes; non-zero replaces the ratio), dirty_expire_centisecs, dirty_writeback_centisecs, max_map_count, page-cluster (id `page_cluster`), vfs_cache_pressure, nr_hugepages | `/proc/sys/vm/*` | static |
| `mem.sysctl.kernel.randomize_va_space` | `/proc/sys/kernel/randomize_va_space` | static |
| `mem.thp.enabled`, `mem.thp.defrag`, `mem.thp.shmem_enabled` (the bracketed choice), `mem.thp.pmd_size_bytes` | `/sys/kernel/mm/transparent_hugepage/*` | static |
| `mem.lru_gen.enabled` (string as printed, e.g. `0x0007`) | `/sys/kernel/mm/lru_gen/enabled` | static |
| `mem.zswap.enabled` (bool) | `/sys/module/zswap/parameters/enabled` | static |
| `mem.zram.devices` (table: name, disksize_bytes) | `/sys/block/zram*/disksize` | static |
| `mem.psi.some.avg10|avg60|avg300`, `mem.psi.some.total_us`, same for `full` | `/proc/pressure/memory` | live |
| `mem.numa.nodes` (table: node, mem_total_bytes, mem_free_bytes, cpulist) | `/sys/devices/system/node/node*/` | |
| `mem.hugepages.sizes` (table: size_bytes, nr, free) | `/sys/kernel/mm/hugepages/` | |
| `mem.memory_block_size_bytes` | `/sys/devices/system/memory/block_size_bytes` (hex) | |
| `mem.slabinfo.cache_count` | `/proc/slabinfo` | normally `permission_denied` unprivileged |
| `mem.cgroup.version` (`v2`/`v1`/`hybrid`), `mem.cgroup.path` (sensitivity: path), `mem.cgroup.memory.max|high|swap_max` (type `limit`: bytes, or the string `"max"`, which is stored without a unit), `mem.cgroup.memory.current|swap_current` (bytes), `mem.cgroup.events.oom|oom_kill|max|high` | `/proc/self/cgroup`, `/sys/fs/cgroup/<path>/memory.*` | scope `cgroup`; on v1 the v2 ids are `unsupported` |

System (`internal/collect/system`). Scope is `kernel` unless noted; `os.*`,
`init.*`, `virt.container.*` and `virt.pid1_cgroup_root` are `namespace`:

| ids | source |
|---|---|
| `os.release.id`, `.id_like`, `.name`, `.version_id` | `/etc/os-release`, fallback `/usr/lib/os-release` |
| `os.hostname` (sensitivity: hostname) | `/proc/sys/kernel/hostname` |
| `kernel.release`, `kernel.version_string`, `kernel.arch` (uname machine), `kernel.page_size_bytes` (syscall), `kernel.boot_time` (btime, RFC 3339), `kernel.uptime_seconds` (live) | `/proc/sys/kernel/osrelease`, `/proc/version`, `uname(2)`, `getpagesize`, `/proc/stat`, `/proc/uptime` |
| `cpu.model_name`, `cpu.vendor`, `cpu.logical_count`, `cpu.core_count`, `cpu.socket_count`, `cpu.threads_per_core`, `cpu.address_bits.virtual`, `cpu.address_bits.physical`, `cpu.flags.la57`, `cpu.flags.pse`, `cpu.flags.pdpe1gb`, `cpu.flags.hypervisor`, `cpu.caches` (table: level, type, size_bytes, line_size_bytes, ways, shared_cpu_list), `cpu.online` (cpulist) | `/proc/cpuinfo`, `/sys/devices/system/cpu/` — x86 flags are `unsupported` on other architectures; `address sizes` is absent on arm64 (then `absent` with a note) |
| `init.pid1_comm`, `init.systemd_booted` (bool: `/run/systemd/system` exists) | `/proc/1/comm`, stat |
| `virt.dmi.sys_vendor`, `virt.dmi.product_name` (sensitivity: freetext; serials are never read), `virt.devicetree.model` (freetext), `virt.container.dockerenv` (bool), `virt.container.containerenv` (bool), `virt.container.systemd` (string: the container manager systemd recorded, kept only if it is one short lowercase word; `absent` on a host or without systemd as PID 1), `virt.pid1_cgroup_root` (bool) | `/sys/class/dmi/id/*`, `/.dockerenv`, `/run/.containerenv`, `/run/systemd/container`, `/proc/1/cgroup` or mountinfo, `/proc/device-tree/model` |
| `block.devices` (table: name, size_bytes, rotational, removable, type) | `/sys/block/*` (loop and ram devices of size 0 skipped); `name` is in the shared `dev` namespace under strict redaction, like every column naming a block device |
| `block.lsblk` (table: name, type, size_bytes, fstype) | command `lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE` — `absent` when the tool is missing, `unsupported` when it rejects `--json`, `permission_denied` when it cannot read sysfs; the `/sys/block` collector is the primary source |
| `mounts.filesystems` (table: mount_point, fstype, source_kind, device, disk, options; sensitivity: path) | `/proc/self/mountinfo` — mount points outside a fixed list of standard paths are replaced by `<path>` at collection; `device` is the basename of a block-device source (UUIDs scrubbed); `disk` the whole device in `/sys/block` holding it, found through `/sys/class/block/<device>` or the mount's device number in `/sys/dev/block`; `options` the per-mount and filesystem options from an allow-list (ro/rw, atime policy, sync, discard, commit=, data=, compress=, barrier, ssd, nodev, nosuid, noexec; never paths, subvolumes or ids); scope `namespace` |
| `session.type`, `session.desktop`, `session.wayland_display_set`, `session.x11_display_set` | allow-listed env of the atlas process; scope `process` |

Storage (`internal/collect/storage`), scope `kernel`. Every table lists the
devices `block.devices` lists, names them in the shared `dev` namespace under
strict redaction, and never names a device in a source path or note;
attributes a device or kernel lacks are left out of its row:

| ids | source |
|---|---|
| `block.queues` (table: name, scheduler, schedulers, rotational, logical_block_bytes, physical_block_bytes, nr_requests, max_request_bytes, max_hw_request_bytes, hw_queues, write_cache, fua, discard_max_bytes, read_ahead_bytes, wbt_latency_us) | `/sys/block/*/queue/` (KiB values converted to bytes); `hw_queues` counts `*/mq/` (none on bio-based devices) |
| `block.stats` (table: name, reads, reads_merged, read_bytes, read_ms, writes, …, in_flight, io_ms, weighted_ms, discards… (4.18), flushes, flush_ms (5.5)) | `/sys/block/*/stat` (sectors converted to bytes) |
| `block.bdi` (table: name, min_ratio, max_ratio, max_bytes, strict_limit, stable_pages_required) | `/sys/block/*/bdi/` (max_bytes and strict_limit since 6.2) |
| `block.psi.some|full.avg10|avg60|avg300|total_us` | `/proc/pressure/io` |
| `fs.types` (table: name, nodev) | `/proc/filesystems` |

The writeback sysctls `mem.sysctl.vm.dirty_expire_centisecs` and
`dirty_writeback_centisecs` and the vmstat counters `nr_dirty`,
`nr_writeback`, `nr_dirtied`, `nr_written`, `nr_dirty_threshold`,
`nr_dirty_background_threshold` (pages) belong to the memory collectors.

CPU and scheduler (`internal/collect/sched`). Scope `kernel` unless noted;
every list file may be empty (no CPU):

| ids | source | notes |
|---|---|---|
| `cpu.topology` (table: cpu, package, die, cluster, core_id, thread_siblings, core_type, capacity) | `/sys/devices/system/cpu/cpuN/topology/*`, `cpuN/cpu_capacity`, `/sys/devices/cpu_core|cpu_atom/cpus` | online CPUs; optional cells left out when the kernel has no such file |
| `cpu.cache_domains` (table: level, type, size_bytes, shared_cpu_list, cpus) | `cpu*/cache/index*` | one row per distinct cache of the online CPUs |
| `cpu.smt.control` (string), `cpu.smt.active` (bool) | `/sys/devices/system/cpu/smt/` | |
| `cpu.present`, `cpu.possible`, `cpu.offline`, `cpu.isolated`, `cpu.nohz_full` (CPU lists) | `/sys/devices/system/cpu/*` | `nohz_full` absent without CONFIG_NO_HZ_FULL |
| `cpu.hybrid.performance_cpus`, `cpu.hybrid.efficiency_cpus` | `/sys/devices/cpu_core/cpus`, `cpu_atom/cpus` | absent unless hybrid x86 |
| `cpu.freq.policies` (table: policy, cpus, driver, governor, governors, cur_khz, min_khz, max_khz, hw_min_khz, hw_max_khz, epp), `cpu.freq.boost`, `cpu.freq.intel_pstate.status`, `.no_turbo`, `cpu.freq.amd_pstate.status` | `/sys/devices/system/cpu/cpufreq/policy*`, `intel_pstate/`, `amd_pstate/` | live; absent in most VMs |
| `sched.loadavg.avg1|avg5|avg15` (float), `.runnable`, `.entities` | `/proc/loadavg` | live |
| `sched.stat.cpu_times` (table: cpu (`all`, `cpuN`), user … guest_nice in USER_HZ ticks), `sched.stat.ctxt`, `.processes`, `.procs_running`, `.procs_blocked` | `/proc/stat` | live |
| `sched.schedstat.version`, `sched.schedstat.cpus` (table: cpu, run_ns, wait_ns, timeslices) | `/proc/schedstat` (versions 15 to 17) | live |
| `sched.psi.some|full.avg10|avg60|avg300|total_us` | `/proc/pressure/cpu` | `full` absent before 5.13 |
| `sched.sysctl.kernel.<name>` for sched_autogroup_enabled, sched_rt_runtime_us, sched_rt_period_us, sched_rr_timeslice_ms, sched_util_clamp_min, sched_util_clamp_max, sched_cfs_bandwidth_slice_us; `sched.ext.state` | `/proc/sys/kernel/*`, `/sys/kernel/sched_ext/state` | static |
| `sched.self.voluntary_ctxt_switches`, `.nonvoluntary_ctxt_switches`, `.cpus_allowed_list`, `.policy`, `.nice`, `.run_us`, `.wait_us`, `.timeslices`, `.slice_us`, `.load_weight` | `/proc/self/status`, `stat`, `schedstat`, `sched` | scope `process`: atlas's main thread only; nothing of other processes is read |
| `sched.cgroup.cpu.max_quota_us` (type `limit` in µs), `.max_period_us`, `.weight`, `.shares_v1`, `.usage_us`, `.user_us`, `.system_us`, `.nr_periods`, `.nr_throttled`, `.throttled_us`, `sched.cgroup.cpuset.cpus_effective` | the cgroup of `/proc/self/cgroup`: v2 `cpu.max`, `cpu.weight`, `cpu.stat`, `cpuset.cpus.effective`; v1 `cpu.cfs_quota_us` (−1 → `max`), `cpu.cfs_period_us`, `cpu.shares`, `cpuacct.usage`, `cpu.stat`, `cpuset.effective_cpus` | scope `cgroup`; sources name `<cgroup>` instead of the path |

Network (`internal/collect/network`), scope `namespace` unless noted; every
table naming an interface uses the shared `if` namespace under strict
redaction:

| ids | source |
|---|---|
| `net.interfaces` (table: name, type, operstate, carrier, mtu, speed_mbps, duplex, tx_queue_len, rx/tx_queues, bql_limit_bytes, driver, master, mac (address), mac_scope, mac_origin, rx/tx bytes, packets, errors, dropped) | `/sys/class/net/*`: attribute files, `queues/`, `statistics/`, `device/driver` and `master` links; attributes a device lacks are left out of its row; failures never name the interface |
| `net.addresses.ipv4` (table: interface, address, prefix_len, scope, class, dynamic) | command `ip -json -4 address show` — `absent` without iproute2, `unsupported` for busybox or iproute2 before JSON, `permission_denied` when netlink is refused |
| `net.addresses.ipv6` (+ temporary) | `/proc/net/if_inet6` |
| `net.routes.ipv4`, `net.routes.ipv6` (table: interface, destination, prefix_len, destination_class, gateway, via_gateway, metric, default, type) | `/proc/net/route` (main table only), `/proc/net/ipv6_route` (cached routes skipped); at most 2000 rows |
| `net.sockstat.*`, `net.sockstat6.*` | `/proc/net/sockstat`, `sockstat6` |
| `net.sockets.tcp_states` (table: state, ipv4, ipv6), `net.sockets.udp_ipv4`, `udp_ipv6`, `unix` | `/proc/net/{tcp,tcp6,udp,udp6,unix}`: counts only, nothing else kept |
| `net.snmp.<Group>.<Name>`, `net.netstat.TcpExt.<Name>` (declared counters only) | `/proc/net/snmp`, `/proc/net/netstat` |
| `net.softnet` (table per CPU: processed, dropped, time_squeeze, received_rps, flow_limit_count, backlog_len); scope `kernel` | `/proc/net/softnet_stat` (hex; no CPU number before 5.10) |
| `net.qdiscs` (table: interface, kind, handle, parent, root, bytes, packets, drops, overlimits, requeues, backlog_bytes, qlen) | command `tc -json -statistics qdisc show`, statuses as for `ip` |
| `net.conntrack.count`, `.max` | `/proc/sys/net/netfilter/nf_conntrack_{count,max}` — `absent` when the module is not loaded |
| `net.sysctl.core.*`, `net.sysctl.ipv4.*` (`tcp_rmem`, `tcp_wmem`, `tcp_mem` and `ip_local_port_range` split into one id per value), `net.sysctl.ipv6.conf.all.disable_ipv6` | `/proc/sys/net/*`; machine-wide ones are absent in a container's own namespace |

Probe (`internal/collect/probe`), scope `process`, source type `probe`:

The collector re-executes the atlas binary as `atlas __probe`. The child makes
these mappings, reports their addresses as JSON on stdout, then blocks on stdin
until the parent has read its `/proc/<pid>/{maps,smaps,status,stat}`; it also
reads its own `/proc/self/pagemap` for each region. Each region is one plain
`mmap` with no address hint, so the kernel chooses address and alignment
(Linux 6.7 and later put a large anonymous mapping on a 2 MiB boundary
themselves); each anonymous region is named with
`prctl(PR_SET_VMA_ANON_NAME)`, so maps prints `[anon: atlas probe: <region>]`
and the kernel never merges it with a neighbour. A kernel that refuses the
name (before 5.17, or built without `CONFIG_ANON_VMA_NAME`) gets the guarded
fallback: everything is unmapped and remade inside one `PROT_NONE`
reservation, each region on a 2 MiB boundary with guard pages between. The
report's `placement` (`mmap` | `guarded`) records which, and
`probe.regions` says so in `source.detail`.

| region | what the child does |
|---|---|
| `anon-untouched` | `mmap` 64 MiB private anonymous, never touched |
| `anon-written` | `mmap` 8 MiB private anonymous, writes one byte per page |
| `anon-read` | `mmap` 4 MiB private anonymous, reads one byte per page (zero-page behaviour) |
| `file-private` | `mmap` up to 4 MiB of its own executable, read-only private, reads every page |
| `shmem-shared` | `memfd_create` 4 MiB, `MAP_SHARED`, writes the first 1 MiB |

Ids: `probe.regions` (table: name, start, end (hex strings), size_bytes, perms,
backing (`anon`/`file`/`shmem`), touched_bytes, rss_bytes, pss_bytes,
anonymous_bytes, private_clean_bytes, private_dirty_bytes, shared_clean_bytes,
shared_dirty_bytes, swap_bytes, anon_huge_bytes, pages_total, pages_present,
pages_swapped, path — pseudo-names and basenames only), `probe.maps.sample`
(table, first 64 VMAs: start, end, perms, offset, kind, path), `probe.maps.vma_count`,
`probe.maps.lowest_start`, `probe.maps.highest_end`,
`probe.status.VmSize|VmRSS|RssAnon|RssFile|RssShmem|VmSwap|VmPTE` (bytes),
`probe.faults.minflt_before_touch`, `probe.faults.minflt_after_touch`,
`probe.faults.majflt_before_touch`, `probe.faults.majflt_after_touch` (the
child touches `anon-written` between the two readings of its own
`getrusage`), `probe.thp.fault_alloc_delta`, `probe.thp.fault_fallback_delta`
(growth of the system-wide `/proc/vmstat` `thp_fault_alloc` and
`thp_fault_fallback` read just before and after the write loop, outside the
getrusage window, so other processes' faults in that instant are included;
`absent` without THP counters), `probe.pagemap.pfn_visible` (bool),
`probe.layout.runs` (table: run, stack (start of the `[stack]` VMA), mmap_base,
heap, heap_kind (`brk` for the `[heap]` VMA, preferred when present;
otherwise `go-arena` for the VMA named `[anon: Go: heap]`; both cells absent
when there is neither), exe — hex addresses from two separate executions, for
ASLR; run 2 is a minimal second execution, `atlas __probe layout`). The
child exits by itself after 5 s whatever the parent does. With `--no-probe`,
or on a non-Linux build, every probe id is `not_collected`/`unsupported`.
`FixtureEnv` reads the same data from `_probe/` in the root.

## 5. Snapshot (schema v1)

```json
{
  "schema": "machine-atlas/snapshot",
  "schema_version": 1,
  "app_version": "0.2.0",
  "id": "sha256:…",
  "created_at": "2026-01-15T12:00:00Z",
  "collection": {
    "mode": "live | fixture",
    "fixture": "",
    "privileged": false,
    "started_at": "…", "finished_at": "…",
    "collectors": [{"id": "memory.meminfo", "status": "ok|partial|failed|skipped", "duration_ms": 3, "observations": 54, "error": ""}]
  },
  "redaction": {"applied": true, "policy": "default", "classes": ["hostname", "path"], "replaced": 3},
  "observations": [ … sorted by id … ]
}
```

- Derived facts are **not** stored; they are recomputed from observations, so a
  snapshot plus an app version fully determines them.
- Canonical encoding: struct field order as above, observations sorted by id,
  two-space indent, trailing newline. `id` is the SHA-256 of the canonical
  encoding with `id` empty and `duration_ms` zeroed.
- Validation (`snapshot.Validate`) is applied to every imported file
  (`atlas snapshot validate`, `--snapshot`, `POST /api/snapshot/import`) and
  every fixture: exact `schema`, `schema_version == 1` (others rejected with a clear
  message), size ≤ 8 MiB, ≤ 20 000 observations, strings ≤ 4096 bytes, tables ≤
  2 000 rows × ≤ 64 columns, flat cells only, ids match the pattern and are
  unique, enums valid, timestamps parse, `value` is null exactly when status is
  not `ok`, numbers finite and within int64 when integral, `redaction.applied`
  agrees with `policy`, `id` equals the id recomputed from the content (a file
  edited after it was written is rejected), and for ids in the catalog the value type matches the
  catalog type and table columns are declared. All problems are reported at
  once (`snapshot.Load` = `Decode` + `Validate`).
  Unknown ids are kept and shown as "not in this version's catalog". Unknown
  top-level or observation fields are rejected.
- Imported snapshots are data. Nothing in one is ever executed, interpolated
  into a command, used as a filesystem path, or rendered as markup.

### Redaction

The catalog assigns each id a sensitivity class: `none`, `hostname`,
`username`, `path`, `address`, `serial`, `freetext` (CPU model, kernel version
string, DMI vendor/product, device-tree model). `snapshot.Redact(s, policy)
(*Snapshot, []Change, error)` returns a new, re-sealed snapshot and the list
of changes:

- `default` policy: replace values of class hostname/username/address/serial
  with `<redacted:class>`; scrub every other string (values, cells, notes,
  sources, collector errors) of `/home/<x>`, `/Users/<x>`, `/run/user/<n>`,
  `user-<n>.slice`, `@<n>.service`, `user@host`, UUIDs, runs of ≥ 32 hex
  digits, MAC addresses, `enx<mac>`, `br-<id>`/`veth<id>` interface names, and
  IPv4/IPv6 addresses, and of the hostname wherever it occurs as a whole word
  (not inside class-`none` values); the redacted observation keeps status
  `ok` and gets a note `redacted (class, …)`. Redaction is idempotent.
- A table column can carry its own class (`column_sensitivity`): address,
  MAC, gateway and destination cells are replaced wholesale by every policy
  while the computed class, scope and flags beside them survive.
- `strict`: additionally replaces freetext values and freetext columns
  (driver names) with `<redacted:freetext>`, and the catalog's identifying
  table columns (block device, swap area and mount device names) with
  numbered placeholders. Names that several tables share are numbered once
  per snapshot in a namespace (`shared_identifiers`): interfaces as
  `<if-2>` in every network table, block devices as `<dev-3>` in the block,
  swap and mount tables, so joins (the default route's interface, the root
  filesystem's disk) still work.
- `none`: unchanged (the CLI and UI warn).
- `snapshot.Preview(s, policy)` returns the same `[]Change`
  (`{id, field, class, before, after}`) without changing anything; the CLI and
  the export dialog show it before writing.
- Every snapshot atlas writes (`atlas collect`, to a file or stdout; export)
  defaults to `default`. Committed fixtures and images are always
  redacted.

## 6. Derived facts

`derive.All(s *snapshot.Snapshot) []Fact`, pure and deterministic, sorted by
id. `docs/DATA-MODEL.md` lists every fact with its rule, generated from it
by `go run ./scripts/docgen`.

```go
type Fact struct {
    ID     string   `json:"id"`      // "derived.…"
    Value  any      `json:"value"`
    Unit   string   `json:"unit,omitempty"`
    Status string   `json:"status"`  // "ok" | "unknown"
    Reason string   `json:"reason,omitempty"` // when unknown: which input was missing and its status
    Rule   string   `json:"rule"`    // human-readable formula or inference rule
    Inputs []string `json:"inputs"`  // observation ids
    Basis  string   `json:"basis"`   // "arithmetic" | "inference" — inference is never shown as observed
}
```

Facts (every one is declared in the catalog):

`derived.env.kind` (`container` | `vm-guest` | `no-virtualization-evidence` |
`unknown`; inference, with the evidence in `rule`: container marker files,
`virt.container.systemd`, PID 1's name and cgroup, the hypervisor flag, DMI
and the device tree), `derived.env.arch_family`,
`derived.paging.levels` (on x86-64, 5 when `/proc/cpuinfo` lists la57, which
since Linux 4.17 means the running kernel enabled 5-level paging; the CPU's
`address sizes` is its capability and is not used; on arm64 and RISC-V from
the page size and the probe's stack address), `derived.paging.va_bits`, `derived.paging.index_bits`,
`derived.paging.user_space_bytes`, `derived.paging.profile` (one of the
simulator profiles in §9), `derived.mem.total_pages`,
`derived.mem.used_estimate_bytes` (MemTotal − MemAvailable),
`derived.mem.available_fraction`, `derived.mem.file_cache_bytes` (Cached +
Buffers − Shmem), `derived.mem.accounted_bytes`, `derived.mem.unaccounted_bytes`
(MemTotal minus the listed consumers; the rule names them),
`derived.cpu.phys_addressable_bytes`, `derived.paging.offset_bits`,
`derived.paging.leaf_table_span_bytes`, `derived.paging.upper_entry_span_bytes`,
`derived.swap.configured`, `derived.swap.total_bytes`,
`derived.swap.used_fraction`, `derived.swap.compressed` (inference: compressed
swap in use, i.e. SwapTotal > 0 with zswap enabled, or a zram device listed as
an active swap area in `/proc/swaps`; a zram device that is not swap does not
count),
`derived.thp.mode`, `derived.thp.enabled` (bool: mode ≠ never),
`derived.reclaim.lru_kind` (`mglru` | `classic` | unknown),
`derived.zones.min_bytes|low_bytes|high_bytes` (sums over zones),
`derived.zones.free_bytes`, `derived.numa.node_count`, `derived.numa.multi`
(bool), `derived.cpu.smt` (bool), `derived.oom.kills_since_boot`,
`derived.oom.any` (bool), `derived.cgroup.memory_limited` (bool),
`derived.overcommit.mode_name`, `derived.commit.over_limit` (bool),
`derived.probe.untouched_rss_bytes`, `derived.probe.written_pages_expected`,
`derived.probe.written_minflt_delta`, `derived.probe.written_huge_fraction`,
`derived.probe.read_region_rss_bytes`, `derived.probe.aslr_differs` (bool),
`derived.psi.available` (bool), `derived.dirty.threshold_estimate_bytes` and
`derived.dirty.background_threshold_estimate_bytes` (vm.dirty_bytes /
dirty_background_bytes when non-zero, else the ratio of MemFree +
Active(file) + Inactive(file) − Σ zone high watermarks; an estimate),
`derived.reclaim.allocstalls` (sum of the `mem.vmstat.allocstall_*` counters
present; `absent` zones are skipped, any other unreadable one makes it
unknown; cgroup-limit reclaim is not counted). CPU and scheduler
(`internal/derive/sched.go`): `derived.cpu.hybrid` (inference),
`derived.cpu.isolated_count`, `derived.cpu.freq.available` (inference: a
missing cpufreq directory is no), `.driver`, `.governor` (or `mixed`),
`.boost_enabled`, `derived.sched.load_per_cpu`,
`derived.sched.busy_since_boot_fraction`, `derived.sched.steal_fraction`,
`derived.sched.ctxt_per_second`, `derived.sched.wait_fraction`,
`derived.sched.avg_timeslice_us`, `derived.sched.fair_scheduler` (inference:
`eevdf` when `/proc/self/sched` has `se.slice`, otherwise from the release),
`derived.sched.ext_enabled`, `derived.sched.self_weight`
(`sched_prio_to_weight`), `derived.sched.self_allowed_count`,
`derived.cgroup.cpu_limited`, `derived.cgroup.cpu_quota_cpus`,
`derived.cgroup.cpu_throttled_fraction`. Storage
(`internal/derive/storage.go`), all about the disk that holds / (the `disk`
of the mount at /; unknown when / is overlay, tmpfs or remote, or cannot be
traced): `derived.storage.root_fstype`, `.root_options`, `.root_atime`
(inference: noatime, relatime or strictatime), `.root_disk`,
`.root_disk_kind` (inference from the name: nvme, scsi, virtio, xen, mmc,
dm, md, zram, loop, nbd, other), from `block.queues` `.root_scheduler`,
`.root_schedulers`, `.root_rotational`, `.root_hw_queues`,
`.root_nr_requests`, `.root_read_ahead_bytes`, `.root_max_request_bytes`,
`.root_write_cache`, and from `block.stats` `.root_read_bytes`,
`.root_written_bytes`, `.root_read_latency_seconds`,
`.root_write_latency_seconds`, `.root_util_fraction` (io_ms / uptime),
`.root_busy_queue_depth` (weighted_ms / io_ms); `derived.dirty.kernel_threshold_bytes`,
`.kernel_background_threshold_bytes` (the thresholds `/proc/vmstat`
publishes, in bytes) and `.threshold_used_fraction` ((nr_dirty +
nr_writeback) / nr_dirty_threshold). Every `*_fraction` fact has unit
`fraction`.

A fact whose inputs are not all `ok` is `unknown` with the reason — never a
guess. A fact may also be unknown for a reason of its own (no swap makes
`derived.swap.used_fraction` undefined). An input missing from the snapshot
reads as `not_collected`. Unit and basis come from the catalog entry.

## 7. Curriculum

### Files

- `curriculum/roadmap.json` — `curriculum_version`, the nine subsystems of the
  roadmap (`id`, `title`, `layer` hardware|kernel|userspace, `status`
  implemented|overview|planned, `summary`, `memory_link` = one sentence tying it
  to the memory journey, `observe` = ids to show on the overview card,
  `track` = optional `{title, order, lede}` for a subsystem with its own
  sequence of core lessons,
  `planned_concepts` = `[{id, title, summary, prerequisites, relevance}]`).
  Every core lesson belongs to the track of its `subsystem`.
- `curriculum/components.json` — the Explore graph (below).
- `curriculum/lessons/<id>.md` — one lesson each.

### Lesson file

````markdown
---
{
  "id": "page-faults",
  "title": "Page faults: memory that appears when you touch it",
  "subsystem": "memory",
  "kind": "core",                       // "core" (on the journey) | "branch" (selected by relevance)
  "order": 4,
  "summary": "One sentence.",
  "prerequisites": ["mappings"],
  "uses": ["probe.regions", "mem.vmstat.pgfault", "derived.thp.mode"],
  "relevance": [
    {"when": "derived.thp.enabled", "is": true, "note": "THP is on here, so one fault can map 2 MiB."}
  ],
  "next": ["physical-memory"],
  "deeper": ["transparent-huge-pages", "planned:userfaultfd"],
  "version_notes": [{"applies": "Linux ≥ 5.14", "text": "…"}],
  "references": [{"title": "…", "url": "https://docs.kernel.org/…", "kind": "kernel-doc"}]
}
---
## Question
…
````

`relevance[].is` is any JSON scalar; without it the rule matches a truthy
value. Reference kinds: `kernel-doc`, `man-page`, `spec`, `source`, `lwn`,
`book`, `paper`, `other`. Lesson ids match `^[a-z][a-z0-9-]*$` and equal the
file name.

Sections, in this order, by exact heading: `Question`, `Why it exists`, `How it
works`, `On this machine`, `See it` (optional), `Predict`, `Try it`, `Limits`,
`Go deeper`. References come from the front matter.

Markdown subset: paragraphs, `###`/`####` headings, `-` and `1.` lists (one
nested level), fenced code blocks, pipe tables, `>` quotes, inline `code`,
`*em*`, `**strong**`, `[text](target)` where target is `https://…`,
`lesson:<id>`, `component:<id>`, or `obs:<id>`. Raw HTML is not allowed and is
rendered as text.

Inline directives: `{{obs:ID}}`, `{{derived:ID}}`, optionally `|fmt` with fmt ∈
`auto` (by unit), `bytes`, `count`, `pages`, `hex`, `percent`, `raw`. They
become **chips**: a formatted value that opens the inspector for that id. A chip
whose target is not `ok` reads e.g. "not available (permission denied)".

Block directives (a line starting with `:::`):

| directive | meaning |
|---|---|
| `:::when FACT` / `:::when FACT is VALUE` … `:::else` … `:::unknown` … `:::end` | three-way branch on an observation or derived id. `else` and `unknown` are optional; a missing `unknown` branch is generated ("Machine Atlas could not determine … (status, source)"). All branches are kept in the bound output with the chosen one marked, so the UI can show what the text would say under the other condition. A directive may contain one nested directive (e.g. a `when` inside a `when` or a `predict` reveal), no deeper. |
| `:::evidence Title` + `- ID` or `- ID \| caption` lines + `:::end` | an evidence table: value, label, source, time, status per row |
| `:::table OBS_ID columns=a,b,c limit=N` | render a table observation (cells formatted in Go) |
| `:::diagram NAME` | a client diagram (§9) |
| `:::sim NAME` | a client simulation (§9) |
| `:::experiment ID` | an experiment card (§8) |
| `:::predict` question … `- option` lines … `:::reveal` … `:::end` | prediction with a hidden reveal; the reveal may use chips and `when` |
| `:::misconception Title` … `:::end` | callout |
| `:::cannot` … `:::end` | "what these measurements cannot establish" callout; every core lesson has at least one |

### Labels

Every block in the bound output carries one label, computed, never typed by
the author (`internal/curriculum/bind.go`). Only chips whose target has
status `ok` count. A paragraph, heading or pipe table takes the weaker of its
chips' labels (`derived` is weaker than `observed`: a sentence that uses an
inference is an inference), and is `general` with none. An `evidence` block is
`observed` if any row with a value is, `derived` if only derived rows have
one, else `general` (rows carry their own labels). A `branch` decided by a
fact with a value carries that fact's label, and in its chosen arm each
paragraph, heading, table, list item and nested decided branch takes the
weaker of its own label and the fact's; callouts, evidence and data tables
keep their own. A branch whose fact is unknown is `general`, and its unknown
arm, written or generated, is labelled by its own chips only (a generated one
is `general`). A `datatable` with a value and the data-bound diagrams
(`address-space-map`, `meminfo-breakdown`, `zone-watermarks`,
`probe-regions`, `cpu-topology`, `packet-path`, `io-stack`) are `observed`; `sim` blocks are `simulated`; `experiment`
cards are `general` (a bound lesson holds no results; the UI labels a result
it runs live OBSERVED); everything else is `general`. The UI shows the four labels as text
badges (OBSERVED, DERIVED, GENERAL, SIMULATED), never by colour alone.

### Bound lesson (API shape)

```
Lesson  {id,title,summary,subsystem,kind,order,prerequisites,next,deeper,version_notes,references,
         sections:[{id,title,blocks:[Block]}], uses:[{id,kind,status}], coverage:{ok,total}}
Block   every block has t and label:
          {t:"p",label,inl} | {t:"h",label,level,inl} | {t:"ul"|"ol",label,items:[[Block]]}
        | {t:"code",label,lang,text} | {t:"table",label,head:[[Inline]],rows:[[[Inline]]]} | {t:"quote",label,blocks}
        | {t:"evidence",label,title,rows:[{ref,kind,caption,display,unit,status,label,source,collected_at}]}
        | {t:"datatable",label,ref,status,display?,columns,rows:[[string]],total}   // display: status text when not ok; total: rows before limit
        | {t:"diagram",label,name} | {t:"sim",label,name} | {t:"experiment",label,id}
        | {t:"predict",label,question:[Block],options:[[Inline]],reveal:[Block]}
        | {t:"callout",label,kind:"misconception"|"cannot",title,blocks}
        | {t:"branch",label,fact,expect,chosen:"then"|"else"|"unknown",then:[Block],else:[Block],unknown:[Block],generated_unknown}
Inline  {t:"text",s} | {t:"code",s} | {t:"em"|"strong",inl} | {t:"link",kind:"url"|"lesson"|"component"|"obs",href,inl}
        | {t:"chip",kind:"obs"|"derived",ref,display,status,label}
```

Arrays are always present (`[]`, never `null`). `expect` is `null` for a
truthy test. `curriculum.Bind(l *Lesson, s *snapshot.Snapshot, facts
[]derive.Fact) *BoundLesson` produces it; `curriculum.RenderText` renders it
for the terminal (`atlas lesson`).

Value formatting happens in Go (`curriculum.Format`), so the same snapshot,
curriculum version and app version give byte-identical bound lessons. Bytes
are shown in IEC units with three significant digits; the inspector always
shows the exact value.

### Selection

The curriculum is loaded once: `curriculum.Default() (*Curriculum, error)`
parses the embedded files (`curriculum.Embedded()`); `Load(fsys)` any other.
Its methods are pure functions of the curriculum and their arguments:
`Lesson(id)`, `Select(s, facts) Journey`, `Components(s, facts)
ComponentGraph`, `Roadmap(s, facts) RoadmapView`, `Usage() map[string]Users`.

`Select` returns `{core, tracks, branches:{relevant,available}, planned:{relevant,
available}, graph:{nodes,edges}}`: the ordered core lessons (topological order
of the prerequisite graph, ties by track order, then `order`, then id), `tracks`
as `[{id, title, lede, lessons}]` in track order, each with matched
relevance notes and evidence coverage; branch lessons and planned concepts
split into `relevant` and `available` by their relevance rules (a rule whose
fact is unknown counts as not matched and says so in `reason`). Foundational lessons are never hidden when a feature is absent; their
text branches instead.

### Lint

`atlas curriculum lint` (also the Go test `TestEmbeddedCurriculum`, which is
strict) fails on: unknown, misordered or missing section, bad front matter, a
prerequisite cycle or dangling id, `uses` not equal to the set of ids actually
referenced, an id not in the catalog (or `:::table` on a non-table), unknown
diagram/sim/experiment name (`curriculum.Diagrams`, `Sims`, `Experiments`), a
link to a missing lesson/component, a core lesson without `:::cannot`,
without `:::predict`, or without references, and an invalid `roadmap.json` or
`components.json`. Each problem names file, line and rule. With
`--allow-missing-lessons`, references to lessons not written yet are
warnings; CI lints strictly.

### Components (Explore)

`curriculum/components.json`: `[{id, title, layer, subsystem, status, what,
why, depends_on:[id], used_by is computed, observe:[ids], varies (what would
change under different conditions), lessons:[ids], instances: {from: table obs
id, key: columns, title: template with {column}, within?: {component, column,
relation}}}]`. `Components(s, facts)` returns `{nodes, edges}`: nodes with
`used_by` computed, `observe` values formatted, and instances discovered from
table observations (zones, NUMA nodes, swap areas, block devices and their
request queues and writeback settings, mounts, CPU caches,
probe regions); edges `{from, to, basis, relation}` with `basis:
"architecture"` (authored `depends_on`, general) or `"observed"` (an instance
`within` another, present in this snapshot). The UI never presents an authored relation as an observed one.

## 8. Experiments

Separate package, separate CLI verb, never run by collection or page load.
Each experiment declares: id, title, question, what it does step by step,
resource use (memory, disk, time bound), what it does not do, and the
alternative outcomes with their interpretations. It runs in a child process
(`atlas __experiment <id>`) with a hard timeout (the child exits a second
early and deletes its file; the parent kills it at the bound), and returns
timestamped measurements plus the id of the outcome that a fixed, pure rule
(`classify.go`, recorded in the result's notes) matched. The declarations are
`experiment.Info {id, title, question, steps, resources:{memory_bytes,
disk_bytes, time_bound_seconds}, does_not, outcomes:[{id, title,
interpretation}], available, unavailable_reason}`; a result is `{id,
started_at, finished_at, measurements:[{id, step, label, value, unit, at}],
outcome, notes}`. Non-Linux builds list both as unavailable.

- `fault-count` — maps two 32 MiB anonymous regions on 2 MiB boundaries (one
  default, one `MADV_NOHUGEPAGE`), counts minor faults
  (`getrusage(RUSAGE_THREAD)`) across first touch, second touch,
  `MADV_DONTNEED`, and touch again; reads the regions' `smaps`. Outcomes
  `small-pages`, `huge-pages`, `partial-huge`, `unexpected`. At most 64 MiB,
  5 s, no files.
- `page-cache-residency` — writes a 16 MiB file under
  `$XDG_CACHE_HOME/machine-atlas/` (or `~/.cache/machine-atlas/`, mode 0700;
  refuses when that filesystem has under 256 MiB free), `fsync`, `mincore`
  residency, `posix_fadvise(DONTNEED)`, residency again, timed cold read,
  residency, timed warm read, delete. Reports the filesystem type of the
  directory; on tmpfs the eviction step is expected to do nothing and the
  interpretation says so. Outcomes `evicted-then-cached`, `not-evictable`,
  `partially-evicted`, `unexpected`. At most 18 MiB memory, 16 MiB disk, 20 s.

CLI: `atlas experiment list`, `atlas experiment run <id>` (prints the resource
statement and asks for confirmation; `--yes` skips the prompt; `--json` prints
the result). API: `POST /api/experiments/<id>/run` with `{"acknowledged":
true}` (400 `not_acknowledged` otherwise); refused with 409 (`not_live`,
`busy`, `unavailable`) when the active snapshot is not the live machine, when
something else is running, or on a platform that cannot run it. One
experiment runs at a time. Results are not stored in snapshots.

## 9. Web UI

Routes (hash router, `web/js/router.js`): `#/` overview, `#/learn`,
`#/learn/<lesson>`, `#/explore`, `#/explore/<component>`, `#/inspect`,
`#/inspect/<obs-or-derived-id>`; any route may carry a query for view state
(`#/inspect?status=failed`).

- **Overview** — what this machine is (derived environment, labelled as
  inference), the nine roadmap subsystems with their status and a few real
  observations each, and each lesson track with its progress.
- **Learn** — journey rail (one group per track, core lessons in order, prerequisite state, branch
  lessons marked relevant/available with the reason, planned concepts marked
  "not written yet"), the `memory-layers` diagram and a prerequisite graph on
  the journey page, the lesson, and a side inspector that opens when a chip
  or evidence row is clicked. Progress is kept in `localStorage`. A branch
  reads as ordinary prose: the chosen text, with a marker (⑂) that opens why
  it was shown, the deciding facts and the other readings ("Elsewhere this
  would read"); a branch whose chosen text is only another branch collapses
  into the same marker, listing every condition.
- **Explore** — components by layer, as cards; no lines are drawn. For the
  selected (or hovered) component the related cards are marked in place ("↓
  relied on", "↑ relies on it") and the rest dim; authored architecture
  relations and relations observed between instances in this snapshot are
  outlined and tagged differently, never merged. A detail panel answers:
  what is this, why does it exist, what depends on it, where can I see it on my
  machine, what would change under different conditions, what should I
  understand next.
- **Inspect** — every observation and derived fact: search, filter by status /
  kind / collector, exact value, source, time, scope, catalog description and
  caveat, which lessons and components use it; collector run report; snapshot
  metadata; export (with redaction preview) and import.
- A persistent banner states the mode: live collection (with collection time),
  snapshot (with age, e.g. "captured 3 days ago — not this machine's current
  state"), or fixture ("demonstration data, not your machine").
- Simulations (`web/js/sim/<name>.model.js` + `<name>.view.js`, registered in
  `web/js/render/lesson.js`), each a pure state module plus a view, each
  showing its assumptions and a SIMULATED badge:
  - `translation-walker` — splits a virtual address into page-table indices for
    a paging profile (`x86_64-4level`, `x86_64-5level`, `arm64-4k-48`,
    `arm64-4k-39`, `arm64-16k-47`, `arm64-16k-48`, `arm64-64k-48`,
    `riscv-sv39`, `riscv-sv48`; `derived.paging.profile` names one of them),
    default profile and example addresses from the snapshot; the index
    arithmetic is exact (DERIVED), the table contents and frame numbers are
    invented (SIMULATED); canonical-address check; huge-page toggle; TLB reach.
  - `reclaim` — a small machine with free/file-clean/file-dirty/anon frames,
    active/inactive lists, min/low/high watermarks, optional swap, swappiness;
    actions and scripted scenarios; an event log explaining each decision
    (kswapd wake, direct reclaim, writeback, swap-out, refault, OOM would be
    invoked). Seeded from the machine's swap presence and swappiness;
    deterministic.
  - `fair-share` — a few always-runnable tasks sharing one CPU under EEVDF
    or CFS: weights from `sched_prio_to_weight`, vruntime, V, lag,
    eligibility and virtual deadlines (or CFS periods), sleep, wake, renice,
    exit; run queue table, virtual-time axis, slice timeline, decision log,
    scenarios. Seeded from `derived.sched.fair_scheduler`,
    `sched.self.slice_us` and `sched.self.nice`; deterministic.
- Diagrams (`web/js/diagrams/`, registered in `index.js`, each exporting
  `title` and `mount`): data-bound `address-space-map`,
  `meminfo-breakdown`, `zone-watermarks`, `probe-regions`, `cpu-topology`
  (packages, cores, threads and cache sharing from `cpu.topology` and
  `cpu.cache_domains`), `packet-path`
  (the stack's layers, general, annotated with this machine's values), `io-stack`
  (the layers from a system call to the device, annotated with the root disk's
  filesystem, scheduler, queues and write cache); authored
  `fault-flow`, `lru-lists`, `memory-layers`. Data-bound diagrams cite the ids
  they draw.
- Rendering uses `createElement`/`textContent` only. CSP:
  `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'`.
- Light and dark themes, keyboard navigable, usable at 1280 px and at 390 px.

## 10. HTTP API

`atlas serve` binds `127.0.0.1` (default port 7373, `--addr` to change; a
non-loopback address is refused unless `--allow-remote`, which also lets the
Host guard accept IP literals). Guards (`internal/server/guard.go`): `Host`
must be `localhost` or a loopback IP literal with the bound port (421
`bad_host`; DNS-rebinding defence); state-changing requests need
`X-Atlas-Token` equal to the per-process token from `/api/state` (403
`bad_token`) and a same-origin `Origin` when present (403 `bad_origin`); no
CORS headers; bodies capped (import 8 MiB, others 64 KiB; JSON bodies reject
unknown fields); read-header 5 s, read 30 s, write 2 min; every response
carries the CSP, `nosniff`, `X-Frame-Options: DENY`, `no-referrer`, and API
responses `Cache-Control: no-store`. `ATLAS_WEB_DIR` serves the UI from disk
instead of the embedded copy (development).

| method and path | returns |
|---|---|
| `GET /api/state` | `app_version`, `curriculum_version`, `schema_version`, `mode` (`live`/`snapshot`/`fixture`), `fixture`, `origin` (`collection`/`file`/`import`/`fixture`), `snapshot` meta (id, created_at, collection, redaction, observation and status counts), `environment` (the `derived.env.kind` fact), `token`, `capabilities` (`live`, `collect`, `experiments`), `busy` |
| `GET /api/snapshot` | the active snapshot |
| `GET /api/derived` | derived facts |
| `GET /api/catalog` | `{entries, families, unknown}`: catalog entries for ids present, the wildcard families, ids not in this version's catalog |
| `GET /api/journey` | selection result, prerequisite graph, planned concepts |
| `GET /api/lessons/<id>` | bound lesson |
| `GET /api/components` | component graph |
| `GET /api/roadmap` | roadmap with observation values for the overview |
| `GET /api/usage` | reverse index: id → lessons and components using it |
| `GET /api/experiments` | experiment descriptions and availability |
| `POST /api/experiments/<id>/run` | run result |
| `POST /api/collect` | re-collect (live mode only) |
| `POST /api/snapshot/import` | validate (body: the file itself) and activate an uploaded snapshot; 422 `invalid_snapshot` with every problem in `details` |
| `POST /api/snapshot/preview` | `{"policy": …}` → `{policy, changes}` |
| `GET /api/snapshot/export?policy=default` | the redacted snapshot as `machine-atlas-<12 hex of id>.json` |
| `POST /api/snapshot/live` | return to the live machine (when available) |
| `GET /api/fixtures`, `POST /api/fixtures/<name>/activate` | embedded demonstration snapshots (`{name, description, created_at, observations}`) |

Errors are `{"error": {"code": "…", "message": "…", "details": ["…"]}}`
(`details` optional) with a proper status.

## 11. CLI

```
atlas serve [--addr 127.0.0.1:7373] [--snapshot FILE | --fixture NAME] [--no-probe] [--open] [--allow-remote]
atlas collect [-o FILE] [--no-probe] [--root DIR [--now RFC3339]] [--redact default|strict|none]
atlas snapshot validate FILE
atlas snapshot preview FILE [--policy default|strict]
atlas snapshot redact FILE -o OUT [--policy default|strict]
atlas snapshot show FILE [--status STATUS] [--prefix ID]
atlas lesson ID [--snapshot FILE | --fixture NAME] [--json]     # bound lesson as text
atlas curriculum lint [--allow-missing-lessons]
atlas experiment list | run ID [--yes] [--json]
atlas fixtures list
atlas version
```

`atlas collect` applies `default` redaction unless told otherwise, whether it
writes to a file or to stdout, and prints every replacement to stderr.
`--now` requires `--root` (a live collection always records the time it ran).
`atlas serve` on a non-Linux build, with no snapshot named, shows the
`desktop-x86_64` fixture. Internal verbs: `atlas __probe [layout]`, `atlas
__experiment <id>` (child processes, not for direct use).

## 12. Fixtures

Fake roots modelled on real interface formats, generated by
`scripts/fixtures-gen` (Python, `python3 -I gen.py <empty dir>`) from the
descriptions in `machines.py` (network files from `gen_net.py`, with
documentation and private address ranges and locally administered MACs
only), each with a `README.md` saying what it models
and which behaviours it exercises:

| root | models |
|---|---|
| `desktop-x86_64` | 16-thread desktop, systemd, swap partition, THP `always`, MGLRU, cgroup v2, some OOM kills, intel_pstate active, EEVDF with `se.slice`, sched_ext disabled; a SATA SSD under mq-deadline holding / and a 4Kn-physical spinning disk under bfq; Ethernet with fq_codel, Wi-Fi off, Docker bridge and veth, a tunnel, global IPv6 |
| `rpi-arm64-noswap` | Raspberry Pi 4, arm64, 4 KiB pages, 39-bit VA, no swap, no DMI, device tree, THP `madvise`, one cpufreq-dt `schedutil` policy, no cpu controller for the session; an SD card without a volatile write cache; on Wi-Fi only, pfifo_fast, no conntrack |
| `alpine-container` | Alpine in Docker on an EC2 host (kernel 6.8): PID 1 is `sh`, no systemd, cgroup `memory.max` 512 MiB, restricted `/proc` and `/sys`, host `/proc/meminfo`, no `lsblk`, `cpu.max` 1.5 CPUs with throttling and a two-CPU cpuset, no cpufreq; overlay root over the host's EBS NVMe volume, whose queues it sees; its own network namespace (eth0 and lo, IPv6 disabled), busybox `ip`, no `tc` |
| `numa-server-la57` | two NUMA nodes, 5-level paging, 1 GiB huge pages, zswap, swap file, THP `never`, `performance` governor, isolated tickless CPUs; two 64-queue NVMe drives without volatile cache for the database; bonded 25 Gbit/s multiqueue NICs with mq and fq, BBR, large buffers, listen overflows, a busy conntrack table |
| `old-vm-cgroupv1` | KVM guest (CentOS 7), kernel 4.19, no PSI, no MGLRU, cgroup v1, a nearly empty DMA zone, `lsblk` without `--json`, CFS, steal time, no cpufreq, a v1 CPU quota; LVM root on bio-based dm-0 over a single-queue virtio disk, 15-field disk statistics; virtio NIC without speed, pfifo_fast, `ip` and `tc` without JSON |
| `restricted` | hardened Debian 12 (kernel 6.1): sysfs restricted to root, `/proc` with `hidepid`, many `deny` entries: permission failures everywhere, including the socket and routing tables and netlink; `/proc/schedstat` denied, atlas at nice 10 under `SCHED_BATCH`; no mount traceable to its disk |

Root layout. Machine paths map directly (`/proc/meminfo` →
`<root>/proc/meminfo`). Three reserved entries:

```
_meta.json   {"description": "…", "now": "2026-01-15T12:00:00Z",
              "uname": {"machine": "aarch64", "release": "6.6.31-v8+"}, "page_size": 4096,
              "deny": ["/proc/slabinfo"],                    // these paths return EACCES
              "env": {"XDG_SESSION_TYPE": "tty"},
              "commands": {"lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE":
                             {"stdout": "_cmd/lsblk.json"}   // or {"missing": true} or {"exit": 1, "stderr": "lsblk: unrecognized option '--json'"}
                          }}
_cmd/        canned command outputs
_probe/      report.json, maps, smaps, status — what a probe child on that machine would have produced
```

`_probe/report.json` is exactly what `atlas __probe` prints on stdout:

```json
{"regions": [{"name": "anon-untouched", "start": "0x7f1a2c000000", "end": "0x7f1a30000000",
              "size_bytes": 67108864, "touched_bytes": 0, "backing": "anon",
              "pages_total": 16384, "pages_present": 0, "pages_swapped": 0}],
 "faults": {"minflt_before_touch": 301, "minflt_after_touch": 2349,
            "majflt_before_touch": 0, "majflt_after_touch": 0},
 "thp_faults": {"fault_alloc_delta": 4, "fault_fallback_delta": 0},
 "pagemap": {"readable": true, "pfn_visible": false},
 "placement": "mmap",
 "layout_runs": [{"run": 1, "stack": "0x7ffd…000", "mmap_base": "0x7f…", "heap": "0x…", "heap_kind": "go-arena", "exe": "0x…"},
                 {"run": 2, "stack": "0x7ffc…000", "mmap_base": "0x7e…", "heap": "0x…", "heap_kind": "go-arena", "exe": "0x…"}]}
```

`maps`, `smaps` and `status` are the raw procfs text of that child (pathnames
already reduced to basenames). They are written by `scripts/fixtures-gen` to
match the report: on kernels 5.17 and later the anonymous regions are named
`[anon: atlas probe: <region>]` with `placement` `mmap`, the Go heap is
`[anon: Go: heap]` and `heap` is that arena's start; `old-vm-cgroupv1`
(4.19) uses the `guarded` placement, unnamed regions between guard VMAs, and
has no heap in its layout runs; `stack` is the page-aligned start of the
`[stack]` VMA. A root without `_probe/` yields `not_collected` probe
observations.

Golden snapshots in `fixtures/snapshots/<root>.json` are produced by `make
fixtures` (`atlas collect --root …` with default redaction and the root's
`now`); `TestGoldenSnapshots` regenerates and compares them byte for byte.
One further snapshot, `fixtures/snapshots/live-sample.json`, is a
strict-redacted capture of a real machine;
`TestCommittedSnapshotsAreRedacted` checks every committed snapshot is
already redacted. Bound-lesson goldens (`cmd/atlas/testdata/lessons/`:
`cpu-addressing` on `desktop-x86_64` and `rpi-arm64-noswap`, `cpu-topology`
on `desktop-x86_64`; rewrite with
`go test ./cmd/atlas -run TestBoundLessonGoldens -update`) and a test binding
every lesson twice on every snapshot guard determinism. Adding a root means
adding its expectations to `fixtureWants` in the `fixtures_test.go` of
`internal/collect/system`, `sched`, `storage` and `network`.

## 13. Tests

Covered: parsers against representative fixtures and malformed input; every status
path (absent, permission denied, unsupported, failed, not collected) through
`FixtureEnv`; no undeclared ids; snapshot validation (accept goldens, reject a
table of bad cases); redaction (no identifying token survives, preview equals
applied changes); derived facts including unknown propagation; lesson parser
and lint; deterministic selection and byte-identical binding; chip display
values equal to the formatted observation; server guards (host, token, body
limits) and import of hostile snapshots; simulation reducers and diagram
models (`node --test`); `go vet`; cross-build for `linux/arm64` and
`darwin/arm64`.

CI (`.github/workflows/ci.yml`) runs: `gofmt -l .` empty,
`go vet ./...`, `go test ./...` (use `-p 2` on small machines; this includes
live probe and experiment tests on Linux unless `-short`), `node --test
'web/test/*.test.mjs'`, `atlas curriculum lint` (strict), `make cross`
(linux/arm64, linux/riscv64, darwin/arm64), then a real `atlas collect`,
`atlas snapshot validate` and `atlas lesson … --snapshot` on the runner.
