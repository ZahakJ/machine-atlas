// Package memory holds the memory collectors: /proc/meminfo, /proc/vmstat,
// zones, buddy allocator, swap, vm sysctls, transparent huge pages, MGLRU,
// zswap and zram, pressure stall information, NUMA nodes, huge page pools
// and the memory cgroup (ARCHITECTURE.md §4, "Memory").
//
// Each collector reads one interface. Parsing is kept apart from reading:
// every format has a pure function from bytes to a typed result, so parsers
// can be tested against old-kernel and malformed inputs, and fuzzed, without
// an environment. Collectors then map read errors and parse errors onto
// observation statuses; a value that cannot be read is never replaced by a
// default.
package memory
