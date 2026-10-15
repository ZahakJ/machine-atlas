package memory

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors for how physical memory is laid out: NUMA nodes, huge page
// pools of each size, and the memory hotplug block size.

const nodeDir = "/sys/devices/system/node/"

// numberedEntries returns the entries of a sysfs directory named prefix
// followed by a decimal number ("node0", "node1"), with their numbers, in
// numeric order so node10 sorts after node9.
func numberedEntries(names []string, prefix string) []int {
	var out []int
	for _, name := range names {
		s, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && strconv.Itoa(n) == s {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// parseNodeMeminfo returns MemTotal and MemFree in bytes from a per-node
// meminfo file, whose lines read "Node 0 MemTotal:  32038980 kB".
func parseNodeMeminfo(data []byte, node int) (total, free int64, err error) {
	fields, err := collect.ParseFields(data)
	if err != nil {
		return 0, 0, err
	}
	prefix := "Node " + strconv.Itoa(node) + " "
	found := map[string]int64{}
	for _, f := range fields {
		key, ok := strings.CutPrefix(f.Key, prefix)
		if !ok {
			return 0, 0, fmt.Errorf("line %q does not start with %q", clipText(f.Key), prefix)
		}
		if key != "MemTotal" && key != "MemFree" {
			continue
		}
		v, kb, err := collect.ParseKB(f.Value)
		if err == nil && (!kb || v < 0) {
			err = fmt.Errorf("expected a size in kB, got %q", clipText(f.Value))
		}
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", key, err)
		}
		found[key] = v
	}
	total, okT := found["MemTotal"]
	free, okF := found["MemFree"]
	if !okT || !okF {
		return 0, 0, errors.New("no MemTotal or MemFree line")
	}
	return total, free, nil
}

func numaCollector() collect.Collector {
	const id = "mem.numa.nodes"
	return collector{
		id:   "memory.numa",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(nodeDir+"node*/", "meminfo, cpulist")
			src.Detail = "sizes converted from kB"
			names, err := env.ReadDir(nodeDir)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no NUMA node directory: the kernel was built without NUMA support (CONFIG_NUMA), so all memory is one node")}
			}
			nodes := numberedEntries(names, "node")
			if len(nodes) == 0 {
				return []obs.Observation{malformed(id, src, errors.New("no node directories"))}
			}
			rows := make([]map[string]any, 0, len(nodes))
			for _, n := range nodes {
				dir := nodeDir + "node" + strconv.Itoa(n) + "/"
				mem, err := env.ReadFile(dir + "meminfo")
				if err != nil {
					return []obs.Observation{unavailable(id, src, err, "")}
				}
				total, free, err := parseNodeMeminfo(mem, n)
				if err != nil {
					return []obs.Observation{malformed(id, src, fmt.Errorf("node%d/meminfo: %w", n, err))}
				}
				cpus, err := env.ReadFile(dir + "cpulist")
				if err != nil {
					return []obs.Observation{unavailable(id, src, err, "")}
				}
				cpulist := strings.TrimSpace(string(cpus))
				if _, err := collect.ParseCPUList(cpulist); err != nil {
					return []obs.Observation{malformed(id, src, fmt.Errorf("node%d/cpulist: %w", n, err))}
				}
				rows = append(rows, map[string]any{
					"node": int64(n), "mem_total_bytes": total, "mem_free_bytes": free, "cpulist": cpulist,
				})
			}
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}

const hugepagesDir = "/sys/kernel/mm/hugepages/"

// hugepageSize parses a pool directory name such as "hugepages-2048kB".
func hugepageSize(name string) (int64, bool) {
	s, ok := strings.CutPrefix(name, "hugepages-")
	if !ok || !strings.HasSuffix(s, "kB") {
		return 0, false
	}
	n, err := collect.ParseSize(strings.TrimSuffix(s, "B"))
	return n, err == nil && n > 0
}

func hugepagesCollector() collect.Collector {
	const id = "mem.hugepages.sizes"
	return collector{
		id:   "memory.hugepages",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(hugepagesDir+"hugepages-*/", "nr_hugepages, free_hugepages")
			names, err := env.ReadDir(hugepagesDir)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no huge page pools: the kernel was built without hugetlbfs (CONFIG_HUGETLBFS)")}
			}
			rows := []map[string]any{}
			for _, name := range names {
				size, ok := hugepageSize(name)
				if !ok {
					continue
				}
				row := map[string]any{"size_bytes": size}
				for _, c := range []struct{ col, file string }{{"nr", "nr_hugepages"}, {"free", "free_hugepages"}} {
					p := hugepagesDir + name + "/" + c.file
					data, err := env.ReadFile(p)
					if err != nil {
						return []obs.Observation{unavailable(id, src, err, "")}
					}
					v, err := parseInt(data)
					if err == nil && v < 0 {
						err = fmt.Errorf("negative count %d", v)
					}
					if err != nil {
						return []obs.Observation{malformed(id, src, fmt.Errorf("%s: %w", p, err))}
					}
					row[c.col] = v
				}
				rows = append(rows, row)
			}
			slices.SortFunc(rows, func(a, b map[string]any) int {
				return cmp.Compare(a["size_bytes"].(int64), b["size_bytes"].(int64))
			})
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}

func blockSizeCollector() collect.Collector {
	const id, p = "mem.memory_block_size_bytes", "/sys/devices/system/memory/block_size_bytes"
	return collector{
		id:   "memory.block_size",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(p, "")
			src.Detail = "hexadecimal without a 0x prefix"
			data, err := env.ReadFile(p)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no memory block directory: the kernel was built without memory hotplug support (CONFIG_MEMORY_HOTPLUG)")}
			}
			s := strings.TrimSpace(string(data))
			v, err := strconv.ParseInt(s, 16, 64)
			if err != nil || v <= 0 {
				return []obs.Observation{malformed(id, src, fmt.Errorf("expected a positive hex size, got %q", clipText(s)))}
			}
			return []obs.Observation{obs.OK(id, v, src)}
		},
	}
}

func slabinfoCollector() collect.Collector {
	const id, p = "mem.slabinfo.cache_count", "/proc/slabinfo"
	return collector{
		id:   "memory.slabinfo",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(p, "")
			data, err := env.ReadFile(p)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no /proc/slabinfo: the kernel was built without slab statistics (CONFIG_SLUB_DEBUG)")}
			}
			n, err := countSlabCaches(data)
			if err != nil {
				return []obs.Observation{malformed(id, src, err)}
			}
			return []obs.Observation{obs.OK(id, n, src)}
		},
	}
}

// countSlabCaches counts the cache lines of /proc/slabinfo, after the
// "slabinfo - version: 2.1" line and the "# name …" column header.
func countSlabCaches(data []byte) (int64, error) {
	lines := strings.Split(string(data), "\n")
	if !strings.HasPrefix(lines[0], "slabinfo - version:") {
		return 0, fmt.Errorf("expected a \"slabinfo - version:\" header, got %q", clipText(lines[0]))
	}
	var n int64
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
	}
	return n, nil
}
