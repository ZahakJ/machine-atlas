package system

import (
	"context"
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
	cpuInfoPath  = "/proc/cpuinfo"
	cpuSysDir    = "/sys/devices/system/cpu"
	cpuOnline    = cpuSysDir + "/online"
	cpu0CacheDir = cpuSysDir + "/cpu0/cache"
)

// x86Flags are the cpuinfo flags the lessons use, in id order.
var x86Flags = []string{"la57", "pse", "pdpe1gb", "hypervisor"}

var cpuCollector = collector{
	id: "system.cpu",
	owns: []string{
		"cpu.model_name", "cpu.vendor",
		"cpu.logical_count", "cpu.core_count", "cpu.socket_count", "cpu.threads_per_core",
		"cpu.address_bits.virtual", "cpu.address_bits.physical",
		"cpu.flags.la57", "cpu.flags.pse", "cpu.flags.pdpe1gb", "cpu.flags.hypervisor",
		"cpu.caches", "cpu.online",
	},
	run: collectCPU,
}

func collectCPU(_ context.Context, env collect.Env) []obs.Observation {
	info, infoErr := readParsed(env, cpuInfoPath, parseCPUInfo)

	// The architecture decides which cpuinfo dialect to expect and whether
	// x86 flags apply at all. uname is authoritative; the cpuinfo dialect
	// is the fallback when uname cannot be read.
	machine, fam := "", famOther
	if u, err := env.Uname(); err == nil && u.Machine != "" {
		machine, fam = u.Machine, familyOf(u.Machine)
	} else if infoErr == nil {
		fam = info.dialect()
		machine = string(fam)
	}

	var out []obs.Observation
	out = append(out, cpuTextFields(info, infoErr, fam, machine)...)
	out = append(out, cpuAddressBits(info, infoErr, fam, machine)...)
	out = append(out, cpuFlags(info, infoErr, fam, machine)...)
	out = append(out, cpuCounts(env, info, infoErr)...)
	out = append(out, cpuCaches(env), cpuOnlineObs(env))
	return out
}

// archFamily groups uname machine strings by cpuinfo dialect.
type archFamily string

const (
	famX86   archFamily = "x86"
	famARM   archFamily = "arm"
	famRISCV archFamily = "riscv"
	famOther archFamily = "other"
)

func familyOf(machine string) archFamily {
	switch {
	case machine == "x86_64" || machine == "amd64" ||
		len(machine) == 4 && machine[0] == 'i' && strings.HasSuffix(machine, "86"):
		return famX86
	case machine == "aarch64" || machine == "arm64" || strings.HasPrefix(machine, "arm"):
		return famARM
	case strings.HasPrefix(machine, "riscv"):
		return famRISCV
	}
	return famOther
}

// cpuInfo is /proc/cpuinfo split into one field map per processor, plus
// the fields printed outside any processor block (arm boards add
// Hardware, Revision and Model after the last CPU).
type cpuInfo struct {
	procs []map[string]string
	extra map[string]string
}

// parseCPUInfo parses /proc/cpuinfo: blocks of "key<tabs>: value" lines
// separated by blank lines. Values may be empty ("power management:").
// A block containing "processor" describes one logical CPU.
func parseCPUInfo(data []byte) (cpuInfo, error) {
	info := cpuInfo{extra: map[string]string{}}
	var block map[string]string
	flush := func() {
		if _, ok := block["processor"]; ok {
			info.procs = append(info.procs, block)
		} else {
			for k, v := range block {
				info.extra[k] = v
			}
		}
		block = nil
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return cpuInfo{}, fmt.Errorf("cpuinfo line %d: expected \"key: value\", got %q", i+1, line)
		}
		if block == nil {
			block = map[string]string{}
		}
		block[k] = strings.TrimSpace(v)
	}
	flush()
	if len(info.procs) == 0 {
		return cpuInfo{}, errors.New("cpuinfo lists no processor")
	}
	return info, nil
}

// dialect guesses the architecture from which fields cpuinfo prints.
func (c cpuInfo) dialect() archFamily {
	p := c.procs[0]
	switch {
	case p["vendor_id"] != "" && p["flags"] != "":
		return famX86
	case p["CPU implementer"] != "":
		return famARM
	case p["isa"] != "":
		return famRISCV
	}
	return famOther
}

// cpuTextFields reads the model and vendor, which every architecture names
// differently: x86 has "model name" and "vendor_id"; arm64 identifies the
// core only by implementer and part codes; RISC-V has "uarch" (the core's
// device-tree compatible string) and "mvendorid".
func cpuTextFields(info cpuInfo, infoErr error, fam archFamily, machine string) []obs.Observation {
	model, vendor := "model name", "vendor_id"
	switch fam {
	case famARM:
		vendor = "CPU implementer"
	case famRISCV:
		model, vendor = "uarch", "mvendorid"
	}
	if infoErr != nil {
		return []obs.Observation{
			obs.Errored("cpu.model_name", obs.File(cpuInfoPath, model), infoErr),
			obs.Errored("cpu.vendor", obs.File(cpuInfoPath, vendor), infoErr),
		}
	}
	p := info.procs[0]
	modelObs := obs.OK("cpu.model_name", p[model], obs.File(cpuInfoPath, model))
	if p[model] == "" {
		note := fmt.Sprintf("/proc/cpuinfo on %s has no %q field", machine, model)
		if fam == famARM {
			note = fmt.Sprintf("/proc/cpuinfo on %s has no model name: it identifies the core only by CPU implementer %s and CPU part %s",
				machine, orUnknown(p["CPU implementer"]), orUnknown(p["CPU part"]))
		}
		modelObs = obs.Absent("cpu.model_name", obs.File(cpuInfoPath, model), note)
	}
	vendorObs := obs.OK("cpu.vendor", p[vendor], obs.File(cpuInfoPath, vendor))
	if p[vendor] == "" {
		vendorObs = obs.Absent("cpu.vendor", obs.File(cpuInfoPath, vendor),
			fmt.Sprintf("/proc/cpuinfo on %s has no %q field", machine, vendor))
	}
	return []obs.Observation{modelObs, vendorObs}
}

func orUnknown(s string) string {
	if s == "" {
		return "(not shown)"
	}
	return s
}

// cpuAddressBits reads x86's "address sizes" line. Other architectures do
// not print one: on arm64 the virtual address size is a kernel build
// choice (CONFIG_ARM64_VA_BITS), and RISC-V prints its paging mode
// ("mmu: sv39") instead.
func cpuAddressBits(info cpuInfo, infoErr error, fam archFamily, machine string) []obs.Observation {
	src := obs.File(cpuInfoPath, "address sizes")
	ids := []string{"cpu.address_bits.virtual", "cpu.address_bits.physical"}
	if infoErr != nil {
		return []obs.Observation{obs.Errored(ids[0], src, infoErr), obs.Errored(ids[1], src, infoErr)}
	}
	p := info.procs[0]
	line := p["address sizes"]
	if line == "" {
		note := fmt.Sprintf("/proc/cpuinfo on %s has no \"address sizes\" line", machine)
		switch {
		case fam == famARM:
			note += "; the virtual address size is a kernel build option (CONFIG_ARM64_VA_BITS), not a CPU field"
		case fam == famRISCV && p["mmu"] != "":
			note += "; it reports the paging mode instead (mmu: " + p["mmu"] + ")"
		}
		return []obs.Observation{obs.Absent(ids[0], src, note), obs.Absent(ids[1], src, note)}
	}
	phys, virt, err := parseAddressSizes(line)
	if err != nil {
		return []obs.Observation{obs.Failed(ids[0], src, err.Error()), obs.Failed(ids[1], src, err.Error())}
	}
	return []obs.Observation{obs.OK(ids[0], virt, src), obs.OK(ids[1], phys, src)}
}

// parseAddressSizes parses "39 bits physical, 48 bits virtual".
func parseAddressSizes(s string) (phys, virt int, err error) {
	var p, v int
	n, err := fmt.Sscanf(s, "%d bits physical, %d bits virtual", &p, &v)
	if err != nil || n != 2 || p <= 0 || v <= 0 || p > 64 || v > 64 {
		return 0, 0, fmt.Errorf("malformed address sizes %q", s)
	}
	return p, v, nil
}

// cpuFlags reports the x86 flags the lessons use. They are unsupported by
// rule on other architectures, whose feature lists use different names.
func cpuFlags(info cpuInfo, infoErr error, fam archFamily, machine string) []obs.Observation {
	out := make([]obs.Observation, 0, len(x86Flags))
	var flags []string
	var flagsErr error
	switch {
	case fam != famX86:
	case infoErr != nil:
		flagsErr = infoErr
	case info.procs[0]["flags"] == "":
		flagsErr = errors.New("/proc/cpuinfo has no flags line")
	default:
		flags = strings.Fields(info.procs[0]["flags"])
	}
	for _, f := range x86Flags {
		id := "cpu.flags." + f
		src := obs.File(cpuInfoPath, "flags")
		src.Detail = "true when " + f + " is listed for the first processor"
		switch {
		case fam != famX86:
			out = append(out, obs.Unsupported(id, src, "an x86 CPU flag; this machine is "+machine))
		case flagsErr != nil:
			out = append(out, obs.Errored(id, src, flagsErr))
		default:
			out = append(out, obs.OK(id, slices.Contains(flags, f), src))
		}
	}
	return out
}

// topoUnit places one logical CPU: its package and the core it shares
// with its SMT siblings.
type topoUnit struct{ pkg, core string }

// topology is the summary lscpu prints.
type topology struct{ logical, cores, sockets, threadsPerCore int }

// summarize counts packages, cores and the largest number of threads on
// one core. The maximum rather than an average is reported because hybrid
// CPUs mix cores with and without SMT, and lscpu reports it the same way.
func summarize(units []topoUnit) topology {
	pkgs := map[string]bool{}
	perCore := map[topoUnit]int{}
	t := topology{logical: len(units)}
	for _, u := range units {
		pkgs[u.pkg] = true
		perCore[u]++
		t.threadsPerCore = max(t.threadsPerCore, perCore[u])
	}
	t.cores, t.sockets = len(perCore), len(pkgs)
	return t
}

// sysfsTopology reads each online CPU's package id and SMT sibling list.
// Siblings identify the core: core_id numbers repeat across packages and
// are not contiguous, but a sibling list is unique to its core.
func sysfsTopology(env collect.Env, online []int) ([]topoUnit, error) {
	units := make([]topoUnit, 0, len(online))
	for _, c := range online {
		dir := fmt.Sprintf("%s/cpu%d/topology", cpuSysDir, c)
		pkg, err := readTrim(env, dir+"/physical_package_id")
		if err != nil {
			return nil, err
		}
		if pkg == "-1" || pkg == "" {
			return nil, fmt.Errorf("%s/physical_package_id is %q: the firmware describes no package", dir, pkg)
		}
		sib, err := readTrim(env, dir+"/thread_siblings_list")
		if err != nil {
			return nil, err
		}
		cpus, err := collect.ParseCPUList(sib)
		if err != nil || !slices.Contains(cpus, c) {
			return nil, fmt.Errorf("%s/thread_siblings_list: malformed %q", dir, sib)
		}
		units = append(units, topoUnit{pkg: pkg, core: fmt.Sprint(cpus)})
	}
	return units, nil
}

// cpuinfoTopology reads the x86 "physical id" and "core id" fields, the
// fallback when sysfs topology is hidden (some containers, restricted
// sysfs). Other architectures do not print them.
func cpuinfoTopology(info cpuInfo) ([]topoUnit, error) {
	units := make([]topoUnit, 0, len(info.procs))
	for _, p := range info.procs {
		pkg, okP := p["physical id"]
		core, okC := p["core id"]
		if !okP || !okC {
			return nil, errors.New("/proc/cpuinfo has no physical id and core id fields on this architecture")
		}
		units = append(units, topoUnit{pkg: pkg, core: pkg + "/" + core})
	}
	return units, nil
}

// cpuCounts reports logical CPUs, cores, sockets and threads per core from
// sysfs topology, falling back to /proc/cpuinfo.
func cpuCounts(env collect.Env, info cpuInfo, infoErr error) []obs.Observation {
	ids := []string{"cpu.logical_count", "cpu.core_count", "cpu.socket_count", "cpu.threads_per_core"}
	sysSrc := obs.File(cpuSysDir, "")
	sysSrc.Detail = "online, cpuN/topology/physical_package_id and thread_siblings_list"

	online, err := readParsed(env, cpuOnline, func(b []byte) ([]int, error) { return collect.ParseCPUList(string(b)) })
	var units []topoUnit
	if err == nil {
		units, err = sysfsTopology(env, online)
	}
	if err == nil {
		t := summarize(units)
		return countObs(ids, t, sysSrc, "")
	}
	sysErr := err

	if infoErr != nil {
		out := make([]obs.Observation, len(ids))
		for i, id := range ids {
			out[i] = obs.Errored(id, sysSrc, sysErr)
		}
		return out
	}
	infoSrc := obs.File(cpuInfoPath, "processor, physical id, core id")
	note := fmt.Sprintf("sysfs topology unreadable (%v); counted from /proc/cpuinfo", sysErr)
	units, err = cpuinfoTopology(info)
	if err == nil {
		return countObs(ids, summarize(units), infoSrc, note)
	}
	out := []obs.Observation{obs.OK(ids[0], len(info.procs), infoSrc)}
	out[0].Note = note
	for _, id := range ids[1:] {
		o := obs.Errored(id, sysSrc, sysErr)
		o.Note += "; " + err.Error()
		out = append(out, o)
	}
	return out
}

func countObs(ids []string, t topology, src obs.Source, note string) []obs.Observation {
	vals := []int{t.logical, t.cores, t.sockets, t.threadsPerCore}
	out := make([]obs.Observation, len(ids))
	for i, id := range ids {
		out[i] = obs.OK(id, vals[i], src)
		out[i].Note = note
	}
	return out
}

// cpuCaches lists cpu0's caches from sysfs. Files a platform does not
// export (associativity is often missing on arm64) leave their cell out
// rather than guessing.
func cpuCaches(env collect.Env) obs.Observation {
	const id = "cpu.caches"
	src := obs.File(cpu0CacheDir, "")
	src.Detail = "index*/level, type, size, coherency_line_size, ways_of_associativity, shared_cpu_list"
	names, err := env.ReadDir(cpu0CacheDir)
	if err != nil {
		return obs.Errored(id, src, err)
	}
	var idx []int
	for _, n := range names {
		if s, ok := strings.CutPrefix(n, "index"); ok {
			if i, err := strconv.Atoi(s); err == nil {
				idx = append(idx, i)
			}
		}
	}
	slices.Sort(idx)
	if len(idx) == 0 {
		return obs.Absent(id, src, "cpu0 exports no cache descriptions (common in VMs and on arm64 firmware without cache tables)")
	}
	rows := make([]map[string]any, 0, len(idx))
	for _, i := range idx {
		row, err := readCacheIndex(env, fmt.Sprintf("%s/index%d", cpu0CacheDir, i))
		if err != nil {
			return obs.Errored(id, src, err)
		}
		rows = append(rows, row)
	}
	return obs.OK(id, rows, src)
}

// cacheFields maps sysfs file names to table columns and parsers.
var cacheFields = []struct {
	file, col string
	parse     func(string) (any, error)
}{
	{"level", "level", parseIntCell},
	{"type", "type", func(s string) (any, error) { return s, nil }},
	{"size", "size_bytes", func(s string) (any, error) { return collect.ParseSize(s) }},
	{"coherency_line_size", "line_size_bytes", parseIntCell},
	{"ways_of_associativity", "ways", parseIntCell},
	{"shared_cpu_list", "shared_cpu_list", func(s string) (any, error) {
		_, err := collect.ParseCPUList(s)
		return s, err
	}},
}

func parseIntCell(s string) (any, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("not a non-negative integer: %q", s)
	}
	return n, nil
}

func readCacheIndex(env collect.Env, dir string) (map[string]any, error) {
	row := map[string]any{}
	for _, f := range cacheFields {
		s, err := readTrim(env, dir+"/"+f.file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		v, err := f.parse(s)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, f.file, err)
		}
		row[f.col] = v
	}
	return row, nil
}

func cpuOnlineObs(env collect.Env) obs.Observation {
	o := stringFile(env, "cpu.online", cpuOnline)
	if o.Status == obs.StatusOK {
		if _, err := collect.ParseCPUList(o.Value.(string)); err != nil {
			return obs.Failed(o.ID, o.Source, err.Error())
		}
	}
	return o
}
