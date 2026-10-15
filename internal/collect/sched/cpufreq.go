package sched

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	cpufreqDir      = cpuDir + "/cpufreq"
	policiesID      = "cpu.freq.policies"
	boostID         = "cpu.freq.boost"
	pstateStatusID  = "cpu.freq.intel_pstate.status"
	pstateNoTurboID = "cpu.freq.intel_pstate.no_turbo"
	amdStatusID     = "cpu.freq.amd_pstate.status"
)

// policyFile is one cpufreq policy attribute and its column.
type policyFile struct {
	file, col string
	required  bool
	khz       bool
}

var policyFiles = []policyFile{
	{"affected_cpus", "cpus", true, false},
	{"scaling_driver", "driver", true, false},
	{"scaling_governor", "governor", true, false},
	{"scaling_available_governors", "governors", false, false},
	{"scaling_cur_freq", "cur_khz", false, true},
	{"scaling_min_freq", "min_khz", true, true},
	{"scaling_max_freq", "max_khz", true, true},
	{"cpuinfo_min_freq", "hw_min_khz", true, true},
	{"cpuinfo_max_freq", "hw_max_khz", true, true},
	{"energy_performance_preference", "epp", false, false},
}

func cpufreqCollector() collect.Collector {
	return collector{
		id:   "sched.cpufreq",
		owns: []string{policiesID, boostID, pstateStatusID, pstateNoTurboID, amdStatusID},
		read: func(env collect.Env) []obs.Observation {
			out := []obs.Observation{policies(env)}
			src := obs.File(cpufreqDir+"/boost", "")
			if s, err := readTrim(env, src.Path); err != nil {
				out = append(out, unavailable(boostID, src, err, "no cpufreq/boost: the scaling driver here (intel_pstate, cpufreq-dt, or none at all) offers no global boost switch"))
			} else if b, err := parseBool01(s); err != nil {
				out = append(out, malformed(boostID, src, err))
			} else {
				out = append(out, obs.OK(boostID, b, src))
			}
			const noIntel = "no intel_pstate directory: the intel_pstate driver is not in use (another vendor's CPU, a virtual machine, or a kernel that prefers acpi-cpufreq)"
			out = append(out, choiceFile(env, pstateStatusID, cpuDir+"/intel_pstate/status", []string{"active", "passive", "off"}, noIntel))
			src = obs.File(cpuDir+"/intel_pstate/no_turbo", "")
			if s, err := readTrim(env, src.Path); err != nil {
				out = append(out, unavailable(pstateNoTurboID, src, err, noIntel))
			} else if b, err := parseBool01(s); err != nil {
				out = append(out, malformed(pstateNoTurboID, src, err))
			} else {
				out = append(out, obs.OK(pstateNoTurboID, b, src))
			}
			out = append(out, choiceFile(env, amdStatusID, cpuDir+"/amd_pstate/status", []string{"active", "passive", "guided", "disable"},
				"no amd_pstate directory: the amd-pstate driver is not in use (another vendor's CPU, a kernel before 6.3, or a virtual machine)"))
			return out
		},
	}
}

// choiceFile reads a file that holds one of a fixed set of words.
func choiceFile(env collect.Env, id, path string, words []string, absentNote string) obs.Observation {
	src := obs.File(path, "")
	s, err := readTrim(env, path)
	switch {
	case err != nil:
		return unavailable(id, src, err, absentNote)
	case !slices.Contains(words, s):
		return malformed(id, src, fmt.Errorf("expected one of %s, got %q", strings.Join(words, ", "), clip(s)))
	}
	return obs.OK(id, s, src)
}

// policies reads every cpufreq/policyN directory, in policy order.
func policies(env collect.Env) obs.Observation {
	src := obs.File(cpufreqDir, "")
	src.Detail = "policy*/affected_cpus, scaling_driver, scaling_governor, scaling_available_governors, scaling_{cur,min,max}_freq, cpuinfo_{min,max}_freq, energy_performance_preference"
	names, err := env.ReadDir(cpufreqDir)
	if err != nil {
		return unavailable(policiesID, src, err, "no cpufreq directory: the kernel controls no CPU frequency here, as in most virtual machines, where the hypervisor owns it, or the kernel was built without CONFIG_CPU_FREQ")
	}
	var nums []int
	for _, n := range names {
		if s, ok := strings.CutPrefix(n, "policy"); ok {
			if i, err := strconv.Atoi(s); err == nil && i >= 0 {
				nums = append(nums, i)
			}
		}
	}
	slices.Sort(nums)
	rows := make([]map[string]any, 0, len(nums))
	for _, p := range nums {
		row, err := readPolicy(env, fmt.Sprintf("%s/policy%d", cpufreqDir, p))
		if err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) {
				return obs.Errored(policiesID, src, err)
			}
			return malformed(policiesID, src, err)
		}
		row["policy"] = int64(p)
		rows = append(rows, row)
	}
	o := obs.OK(policiesID, rows, src)
	if len(rows) == 0 {
		o.Note = "the cpufreq directory exists but holds no policy: frequency scaling is built in, but no driver has taken charge of any CPU"
	}
	return o
}

func readPolicy(env collect.Env, dir string) (map[string]any, error) {
	row := map[string]any{}
	for _, f := range policyFiles {
		s, err := readTrim(env, dir+"/"+f.file)
		if err != nil {
			// A policy whose CPUs are all offline keeps its directory but
			// reads "<unknown>" or nothing for current values; optional
			// files are simply left out.
			if !f.required && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		switch {
		case f.khz:
			if !f.required && s == "<unknown>" {
				continue
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%s/%s: %q is not a frequency in kHz", dir, f.file, clip(s))
			}
			row[f.col] = n
		case f.col == "cpus":
			if _, err := collect.ParseCPUList(strings.ReplaceAll(s, " ", ",")); err != nil {
				return nil, fmt.Errorf("%s/%s: %w", dir, f.file, err)
			}
			row[f.col] = normalizeCPUWords(s)
		default:
			if s == "" && f.required {
				return nil, fmt.Errorf("%s/%s is empty", dir, f.file)
			}
			row[f.col] = strings.Join(strings.Fields(s), " ")
		}
	}
	return row, nil
}

// normalizeCPUWords turns affected_cpus ("0 1 2 3", space-separated) into
// the CPU list format used everywhere else ("0-3").
func normalizeCPUWords(s string) string {
	cpus, err := collect.ParseCPUList(strings.Join(strings.Fields(s), ","))
	if err != nil {
		return s
	}
	return formatCPUList(cpus)
}

// formatCPUList writes ascending CPU numbers in the kernel's list format.
func formatCPUList(cpus []int) string {
	var b strings.Builder
	for i := 0; i < len(cpus); {
		j := i
		for j+1 < len(cpus) && cpus[j+1] == cpus[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(cpus[i]))
		if j > i {
			b.WriteByte('-')
			b.WriteString(strconv.Itoa(cpus[j]))
		}
		i = j + 1
	}
	return b.String()
}
