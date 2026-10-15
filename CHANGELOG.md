# Changelog

While the version is 0.x, a minor release may change observation ids, derived
rules and output. Schema and curriculum versions: `docs/DATA-MODEL.md`,
"Versioning".

## 0.2.0

Snapshot schema 1, curriculum 0.2.0.

- Lesson tracks: the journey groups core lessons by subsystem.
- CPU and scheduler track (five lessons), simulation `fair-share`, diagram
  `cpu-topology`; collectors for topology, cpufreq, load, `/proc/stat`,
  schedstat, CPU pressure, atlas's own scheduling and the cpu cgroup.
- Storage track (five lessons), diagram `io-stack`; block queues, I/O
  statistics, BDI, I/O pressure, filesystem types, mount options.
- Network track (five lessons), diagram `packet-path`; interfaces,
  addresses, routes, socket counts, SNMP counters, softnet, qdiscs,
  conntrack, net sysctls.
- Redaction: per-column sensitivity; strict numbers interfaces (`<if-N>`)
  and block devices (`<dev-N>`) consistently across tables.

## 0.1.0

Snapshot schema 1, curriculum 0.1.0.

- `atlas serve`: Learn, Explore and Inspect over a live collection, a snapshot
  file or a fixture.
- Unprivileged collection from `/proc`, `/sys`, `uname(2)` and `lsblk`, every
  id with a status.
- The probe child: mappings, page faults, zero-page reads, shared memory,
  address randomization, THP fault counters.
- Snapshot schema v1: canonical encoding, content id, strict validation,
  `default`/`strict`/`none` redaction with preview.
- Derived facts with rule, inputs and basis.
- The memory journey: nine core lessons and a THP branch; `atlas lesson`.
- Roadmap of nine subsystems; Explore map of 23 components.
- Simulations `translation-walker` and `reclaim`; seven diagrams.
- Experiments `fault-count` and `page-cache-residency`.
- Six fixture machines and a strict-redacted live sample.
- HTTP API with Host, token, Origin and body-size guards and a strict CSP.
