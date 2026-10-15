package memory

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors for kernel features that are configured through sysfs and may
// simply not be built in: transparent huge pages, Multi-Gen LRU, zswap, zram
// and pressure stall information. Their absence notes say so, because a
// missing file here usually describes the kernel build, not this machine's
// settings.

const thpDir = "/sys/kernel/mm/transparent_hugepage/"

func thpCollector() collect.Collector {
	const noTHP = "no such file: the kernel was built without transparent huge pages (CONFIG_TRANSPARENT_HUGEPAGE) or is too old for this file"
	choices := []struct{ id, file string }{
		{"mem.thp.enabled", "enabled"},
		{"mem.thp.defrag", "defrag"},
		{"mem.thp.shmem_enabled", "shmem_enabled"},
	}
	const pmdID = "mem.thp.pmd_size_bytes"
	return collector{
		id:   "memory.thp",
		owns: []string{"mem.thp.enabled", "mem.thp.defrag", "mem.thp.shmem_enabled", pmdID},
		read: func(env collect.Env) []obs.Observation {
			var out []obs.Observation
			for _, c := range choices {
				src := obs.File(thpDir+c.file, "")
				data, err := env.ReadFile(src.Path)
				if err != nil {
					out = append(out, unavailable(c.id, src, err, noTHP))
					continue
				}
				v, err := collect.ParseChoice(string(data))
				if err != nil {
					out = append(out, malformed(c.id, src, err))
					continue
				}
				out = append(out, obs.OK(c.id, v, src))
			}
			src := obs.File(thpDir+"hpage_pmd_size", "")
			data, err := env.ReadFile(src.Path)
			if err != nil {
				return append(out, unavailable(pmdID, src, err, noTHP))
			}
			v, err := parseInt(data)
			if err == nil && v <= 0 {
				err = fmt.Errorf("non-positive size %d", v)
			}
			if err != nil {
				return append(out, malformed(pmdID, src, err))
			}
			return append(out, obs.OK(pmdID, v, src))
		},
	}
}

var lruGenMask = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)

func lruGenCollector() collect.Collector {
	const id, p = "mem.lru_gen.enabled", "/sys/kernel/mm/lru_gen/enabled"
	return collector{
		id:   "memory.lru_gen",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(p, "")
			data, err := env.ReadFile(p)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no lru_gen file: the kernel is older than 6.1 or was built without Multi-Gen LRU (CONFIG_LRU_GEN), so reclaim uses the classic active and inactive lists")}
			}
			s := strings.TrimSpace(string(data))
			if !lruGenMask.MatchString(s) {
				return []obs.Observation{malformed(id, src, fmt.Errorf("expected a hex mask such as 0x0007, got %q", clipText(s)))}
			}
			return []obs.Observation{obs.OK(id, s, src)}
		},
	}
}

func zswapCollector() collect.Collector {
	const id, p = "mem.zswap.enabled", "/sys/module/zswap/parameters/enabled"
	return collector{
		id:   "memory.zswap",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(p, "")
			data, err := env.ReadFile(p)
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "no zswap parameters: the kernel was built without zswap (CONFIG_ZSWAP)")}
			}
			// Boolean module parameters print as Y or N.
			switch s := strings.TrimSpace(string(data)); s {
			case "Y":
				return []obs.Observation{obs.OK(id, true, src)}
			case "N":
				return []obs.Observation{obs.OK(id, false, src)}
			default:
				return []obs.Observation{malformed(id, src, fmt.Errorf("expected Y or N, got %q", clipText(s)))}
			}
		},
	}
}

// isZram reports whether a /sys/block entry is a zram device ("zram0").
func isZram(name string) bool {
	n, ok := strings.CutPrefix(name, "zram")
	if !ok || n == "" {
		return false
	}
	_, err := strconv.ParseUint(n, 10, 32)
	return err == nil
}

func zramCollector() collect.Collector {
	const id = "mem.zram.devices"
	return collector{
		id:   "memory.zram",
		owns: []string{id},
		read: func(env collect.Env) []obs.Observation {
			src := obs.File("/sys/block/zram*/disksize", "")
			names, err := env.ReadDir("/sys/block")
			if err != nil {
				return []obs.Observation{unavailable(id, src, err, "")}
			}
			rows := []map[string]any{}
			for _, name := range names {
				if !isZram(name) {
					continue
				}
				p := "/sys/block/" + name + "/disksize"
				data, err := env.ReadFile(p)
				if err != nil {
					return []obs.Observation{unavailable(id, src, err, "")}
				}
				size, err := parseInt(data)
				if err == nil && size < 0 {
					err = fmt.Errorf("negative size %d", size)
				}
				if err != nil {
					return []obs.Observation{malformed(id, src, fmt.Errorf("%s: %w", p, err))}
				}
				rows = append(rows, map[string]any{"name": name, "disksize_bytes": size})
			}
			return []obs.Observation{obs.OK(id, rows, src)}
		},
	}
}

const psiPath = "/proc/pressure/memory"

// psiLine is one line of a pressure file: the share of wall time stalled
// over three windows, in percent, and the total stall time in microseconds.
type psiLine struct {
	Avg10, Avg60, Avg300 float64
	Total                int64
}

// parsePSI parses /proc/pressure/memory: "some" and "full" lines of the form
// "some avg10=0.00 avg60=0.00 avg300=0.00 total=12345". Both lines must be
// present; the memory file has had both since PSI was introduced.
func parsePSI(data []byte) (map[string]psiLine, error) {
	out := map[string]psiLine{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		kind := f[0]
		if kind != "some" && kind != "full" {
			return nil, fmt.Errorf("line %d: expected \"some\" or \"full\", got %q", i+1, clipText(kind))
		}
		if _, dup := out[kind]; dup {
			return nil, fmt.Errorf("line %q appears twice", kind)
		}
		kv := map[string]string{}
		for _, pair := range f[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("line %d: expected key=value, got %q", i+1, clipText(pair))
			}
			kv[k] = v
		}
		var l psiLine
		var err error
		for _, a := range []struct {
			key string
			dst *float64
		}{{"avg10", &l.Avg10}, {"avg60", &l.Avg60}, {"avg300", &l.Avg300}} {
			if *a.dst, err = strconv.ParseFloat(kv[a.key], 64); err != nil || !(*a.dst >= 0 && *a.dst <= 100) { // also rejects NaN
				return nil, fmt.Errorf("%s %s: not a percentage: %q", kind, a.key, clipText(kv[a.key]))
			}
		}
		if l.Total, err = strconv.ParseInt(kv["total"], 10, 64); err != nil || l.Total < 0 {
			return nil, fmt.Errorf("%s total: not a duration in microseconds: %q", kind, clipText(kv["total"]))
		}
		out[kind] = l
	}
	for _, kind := range []string{"some", "full"} {
		if _, ok := out[kind]; !ok {
			return nil, errors.New("no " + kind + " line")
		}
	}
	return out, nil
}

func psiCollector() collect.Collector {
	var ids []string
	for _, kind := range []string{"some", "full"} {
		for _, f := range []string{"avg10", "avg60", "avg300", "total_us"} {
			ids = append(ids, "mem.psi."+kind+"."+f)
		}
	}
	return collector{
		id:   "memory.psi",
		owns: ids,
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(psiPath, "")
			data, err := env.ReadFile(psiPath)
			if err != nil {
				out := unavailableAll(ids, src, err, "no /proc/pressure/memory: the kernel is older than 4.20 or was built without pressure stall information (CONFIG_PSI)")
				for i := range out {
					if out[i].Status == obs.StatusUnsupported {
						// The file exists but reads fail with EOPNOTSUPP.
						out[i].Note = "pressure stall information is built in but disabled (psi=0 on the kernel command line, or CONFIG_PSI_DEFAULT_DISABLED): " + out[i].Note
					}
				}
				return out
			}
			lines, err := parsePSI(data)
			if err != nil {
				return malformedAll(ids, src, err)
			}
			var out []obs.Observation
			for _, kind := range []string{"some", "full"} {
				l := lines[kind]
				for _, v := range []struct {
					field, raw string
					value      any
				}{
					{"avg10", "avg10", l.Avg10}, {"avg60", "avg60", l.Avg60},
					{"avg300", "avg300", l.Avg300}, {"total_us", "total", l.Total},
				} {
					out = append(out, obs.OK("mem.psi."+kind+"."+v.field, v.value, obs.File(psiPath, kind+" "+v.raw)))
				}
			}
			return out
		},
	}
}
