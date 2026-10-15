package storage

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const pressurePath = "/proc/pressure/io"

// psiLine is one line of a pressure file.
type psiLine struct {
	avg   [3]float64
	total int64
}

// parsePSI parses /proc/pressure/io: a some and a full line, each with
// avg10, avg60 and avg300 percentages and a total in microseconds. The io
// file has had both lines since pressure stall information was added.
func parsePSI(data []byte) (map[string]psiLine, error) {
	out := map[string]psiLine{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		kind := f[0]
		if kind != "some" && kind != "full" {
			return nil, fmt.Errorf("line %d: expected some or full, got %q", i+1, clip(kind))
		}
		if _, dup := out[kind]; dup {
			return nil, fmt.Errorf("line %q appears twice", kind)
		}
		kv := map[string]string{}
		for _, pair := range f[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("line %d: expected key=value, got %q", i+1, clip(pair))
			}
			kv[k] = v
		}
		var l psiLine
		for j, key := range []string{"avg10", "avg60", "avg300"} {
			v, err := strconv.ParseFloat(kv[key], 64)
			if err != nil || !(v >= 0 && v <= 100) {
				return nil, fmt.Errorf("%s %s: not a percentage: %q", kind, key, clip(kv[key]))
			}
			l.avg[j] = v
		}
		t, err := strconv.ParseInt(kv["total"], 10, 64)
		if err != nil || t < 0 {
			return nil, fmt.Errorf("%s total: not a duration in microseconds: %q", kind, clip(kv["total"]))
		}
		l.total = t
		out[kind] = l
	}
	for _, kind := range []string{"some", "full"} {
		if _, ok := out[kind]; !ok {
			return nil, errors.New("no " + kind + " line")
		}
	}
	return out, nil
}

var psiFields = []string{"avg10", "avg60", "avg300", "total_us"}

func psiIDs() []string {
	var ids []string
	for _, kind := range []string{"some", "full"} {
		for _, f := range psiFields {
			ids = append(ids, "block.psi."+kind+"."+f)
		}
	}
	return ids
}

func psiCollector() collect.Collector {
	ids := psiIDs()
	return collector{
		id:   "storage.psi",
		owns: ids,
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(pressurePath, "")
			data, err := env.ReadFile(pressurePath)
			if err != nil {
				out := make([]obs.Observation, len(ids))
				for i, id := range ids {
					out[i] = obs.Errored(id, src, err)
					switch out[i].Status {
					case obs.StatusAbsent:
						out[i].Note = "no /proc/pressure/io: the kernel is older than 4.20 or was built without pressure stall information (CONFIG_PSI) (" + out[i].Note + ")"
					case obs.StatusUnsupported:
						out[i].Note = "pressure stall information is built in but disabled (psi=0 on the kernel command line, or CONFIG_PSI_DEFAULT_DISABLED): " + out[i].Note
					}
				}
				return out
			}
			lines, err := parsePSI(data)
			if err != nil {
				out := make([]obs.Observation, len(ids))
				for i, id := range ids {
					out[i] = obs.Failed(id, src, "unexpected format: "+err.Error())
				}
				return out
			}
			var out []obs.Observation
			for _, kind := range []string{"some", "full"} {
				l := lines[kind]
				for j, f := range psiFields {
					id := "block.psi." + kind + "." + f
					s := obs.File(pressurePath, kind+" "+strings.TrimSuffix(f, "_us"))
					if j < 3 {
						out = append(out, obs.OK(id, l.avg[j], s))
					} else {
						out = append(out, obs.OK(id, l.total, s))
					}
				}
			}
			return out
		},
	}
}
