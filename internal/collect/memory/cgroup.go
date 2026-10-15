package memory

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const (
	procCgroupPath = "/proc/self/cgroup"
	mountinfoPath  = "/proc/self/mountinfo"
)

// cgroupEntry is one line of /proc/self/cgroup: "hierarchy-ID:controllers:path".
// The cgroup v2 line is "0::/path"; v1 lines have a non-zero hierarchy id and
// the controllers bound to that hierarchy.
type cgroupEntry struct {
	Hierarchy   int
	Controllers []string
	Path        string
}

// parseProcCgroup parses /proc/self/cgroup. The path may itself contain
// colons, so each line is split into at most three parts.
func parseProcCgroup(data []byte) ([]cgroupEntry, error) {
	var out []cgroupEntry
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], "/") {
			return nil, fmt.Errorf("line %d: expected \"id:controllers:/path\", got %q", i+1, clipText(line))
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil || id < 0 {
			return nil, fmt.Errorf("line %d: bad hierarchy id %q", i+1, clipText(parts[0]))
		}
		e := cgroupEntry{Hierarchy: id, Path: parts[2]}
		if parts[1] != "" {
			e.Controllers = strings.Split(parts[1], ",")
		}
		if (id == 0) != (len(e.Controllers) == 0) {
			return nil, fmt.Errorf("line %d: hierarchy 0 is the v2 line and only it has no controllers: %q", i+1, clipText(line))
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, errors.New("no cgroup lines")
	}
	return out, nil
}

// cgroupView is what /proc/self/cgroup says about memory accounting.
type cgroupView struct {
	Version string // "v2", "v1" or "hybrid"
	// Path is the cgroup whose memory accounting applies: the v1 memory
	// hierarchy's when one exists, otherwise the v2 one. Empty when no
	// hierarchy carries the memory controller.
	Path string
	// MemoryOnV2 is true when the memory controller is on the unified
	// hierarchy, so the v2 memory.* files describe this process.
	MemoryOnV2 bool
}

// classify decides the cgroup version. The kernel prints the "0::" line only
// once a cgroup2 hierarchy has been mounted, so v1 lines alone mean v1, the
// "0::" line alone means v2, and both mean a hybrid layout (systemd's
// /sys/fs/cgroup/unified beside the v1 controllers). On a hybrid system the
// memory controller is normally bound to v1.
func classify(entries []cgroupEntry) cgroupView {
	var v2, v1mem *cgroupEntry
	v1 := false
	for i := range entries {
		e := &entries[i]
		switch {
		case e.Hierarchy == 0:
			v2 = e
		default:
			v1 = true
			if slices.Contains(e.Controllers, "memory") {
				v1mem = e
			}
		}
	}
	var view cgroupView
	switch {
	case v2 != nil && !v1:
		view.Version = "v2"
	case v2 == nil:
		view.Version = "v1"
	default:
		view.Version = "hybrid"
	}
	switch {
	case v1mem != nil:
		view.Path = v1mem.Path
	case v2 != nil:
		view.Path, view.MemoryOnV2 = v2.Path, true
	}
	return view
}

// mountEntry is the part of a /proc/self/mountinfo line the cgroup collector
// needs.
type mountEntry struct {
	Root, Point, FSType string
}

// parseMountinfo parses /proc/self/mountinfo: "id parent maj:min root
// mount-point options [optional fields…] - fstype source super-options".
// Root and mount point are octal-escaped by the kernel (\040 for a space).
func parseMountinfo(data []byte) ([]mountEntry, error) {
	var out []mountEntry
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		sep := slices.Index(f, "-")
		if sep < 6 || sep+1 >= len(f) {
			return nil, fmt.Errorf("line %d: no \" - fstype\" separator in %q", i+1, clipText(line))
		}
		out = append(out, mountEntry{Root: unescapeOctal(f[3]), Point: unescapeOctal(f[4]), FSType: f[sep+1]})
	}
	return out, nil
}

// unescapeOctal undoes the kernel's \ooo escaping of whitespace and
// backslashes in mountinfo paths. Malformed escapes are kept as written.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// cgroupDir locates the cgroup at cgPath in the mounted cgroup2 hierarchy
// and returns the mount point and the cgroup's path below it. A mount's root
// is the part of the hierarchy it shows, so a cgroup is visible only below a
// mount whose root contains it; inside a container without its own cgroup
// namespace, /proc/self/cgroup names a host path that no mount here shows.
func cgroupDir(mounts []mountEntry, cgPath string) (point, rel string, ok bool) {
	for _, m := range mounts {
		if m.FSType != "cgroup2" {
			continue
		}
		rel, ok := cgPath, true
		if m.Root != "/" {
			rel, ok = strings.CutPrefix(cgPath, m.Root)
		}
		if ok && (rel == "" || strings.HasPrefix(rel, "/")) {
			return m.Point, rel, true
		}
	}
	return "", "", false
}

// parseLimit parses a cgroup v2 limit file: a byte count, or "max" for no
// limit, which is kept as the string so nothing is invented for "unlimited".
func parseLimit(data []byte) (any, error) {
	s := strings.TrimSpace(string(data))
	if s == "max" {
		return "max", nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("expected a byte count or \"max\", got %q", clipText(s))
	}
	return n, nil
}

// parseFlatKeyed parses a cgroup "key value" file such as memory.events.
func parseFlatKeyed(data []byte) (map[string]int64, error) {
	out := map[string]int64{}
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 2 {
			return nil, fmt.Errorf("line %d: expected \"key value\", got %q", i+1, clipText(line))
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("line %d: %s: not a count: %q", i+1, f[0], clipText(f[1]))
		}
		out[f[0]] = n
	}
	return out, nil
}

// cgroupValue is one v2 id read from a single-value file.
type cgroupValue struct {
	id, file string
	limit    bool // "max" is allowed
	swap     bool // exists only with swap accounting
}

var cgroupValues = []cgroupValue{
	{"mem.cgroup.memory.max", "memory.max", true, false},
	{"mem.cgroup.memory.high", "memory.high", true, false},
	{"mem.cgroup.memory.current", "memory.current", false, false},
	{"mem.cgroup.memory.swap_max", "memory.swap.max", true, true},
	{"mem.cgroup.memory.swap_current", "memory.swap.current", false, true},
}

var cgroupEvents = []string{"oom", "oom_kill", "max", "high"}

const (
	cgVersionID = "mem.cgroup.version"
	cgPathID    = "mem.cgroup.path"
)

// cgroupV2IDs are the ids that describe v2 memory interface files.
func cgroupV2IDs() []string {
	var ids []string
	for _, v := range cgroupValues {
		ids = append(ids, v.id)
	}
	for _, e := range cgroupEvents {
		ids = append(ids, "mem.cgroup.events."+e)
	}
	return ids
}

func cgroupCollector() collect.Collector {
	v2ids := cgroupV2IDs()
	return collector{
		id:   "memory.cgroup",
		owns: append([]string{cgVersionID, cgPathID}, v2ids...),
		read: func(env collect.Env) []obs.Observation {
			src := obs.File(procCgroupPath, "")
			all := append([]string{cgVersionID, cgPathID}, v2ids...)
			data, err := env.ReadFile(procCgroupPath)
			if err != nil {
				return unavailableAll(all, src, err, "no /proc/self/cgroup: the kernel was built without control groups (CONFIG_CGROUPS)")
			}
			entries, err := parseProcCgroup(data)
			if err != nil {
				return malformedAll(all, src, err)
			}
			view := classify(entries)
			out := []obs.Observation{obs.OK(cgVersionID, view.Version, src)}
			if view.Path == "" {
				out = append(out, obs.Absent(cgPathID, src, "no cgroup hierarchy carries the memory controller"))
			} else {
				out = append(out, obs.OK(cgPathID, view.Path, src))
			}
			return append(out, readCgroupV2(env, view, v2ids)...)
		},
	}
}

// readCgroupV2 reads the v2 memory interface of the cgroup in view, or
// explains why the v2 ids do not apply.
func readCgroupV2(env collect.Env, view cgroupView, ids []string) []obs.Observation {
	if !view.MemoryOnV2 {
		note := "cgroup " + view.Version + ": the memory controller is on a v1 hierarchy, whose files (memory.limit_in_bytes, memory.usage_in_bytes, …) have different meanings from these v2 ones"
		if view.Path == "" {
			note = "no cgroup hierarchy carries the memory controller"
		}
		out := make([]obs.Observation, len(ids))
		for i, id := range ids {
			out[i] = obs.Unsupported(id, obs.File("/sys/fs/cgroup/<cgroup>/", ""), note)
		}
		return out
	}
	msrc := obs.File(mountinfoPath, "")
	data, err := env.ReadFile(mountinfoPath)
	if err != nil {
		return unavailableAll(ids, msrc, err, "")
	}
	mounts, err := parseMountinfo(data)
	if err != nil {
		return malformedAll(ids, msrc, err)
	}
	point, rel, ok := cgroupDir(mounts, view.Path)
	if !ok {
		out := make([]obs.Observation, len(ids))
		for i, id := range ids {
			out[i] = obs.Absent(id, msrc, "no cgroup2 mount in this mount namespace shows this process's cgroup (typical of a container without its own cgroup namespace)")
		}
		return out
	}
	// The source names the mount point and a placeholder for the cgroup:
	// the path itself is mem.cgroup.path, which can identify a user and is
	// redacted on export; repeating it in every source would defeat that.
	dir := path.Join(point, rel)
	source := func(file string) obs.Source {
		s := obs.File(path.Join(point, "<cgroup>", file), "")
		s.Detail = "<cgroup> is this process's cgroup, mem.cgroup.path"
		return s
	}
	missingNote := func(swap bool) string {
		switch {
		case view.Path == "/":
			return "atlas runs in the root cgroup, which has no memory limit or usage files; those exist only for child cgroups"
		case swap:
			return "no such file: the kernel has no swap accounting for cgroups, or the memory controller is not enabled for this cgroup"
		default:
			return "no such file: the memory controller is not enabled for this cgroup (see the parent's cgroup.subtree_control)"
		}
	}
	read := func(id, file string, swap bool) ([]byte, obs.Observation, bool) {
		b, err := env.ReadFile(path.Join(dir, file))
		if err == nil {
			return b, obs.Observation{}, true
		}
		o := obs.Errored(id, source(file), err)
		o.Note = errnoText(err)
		if o.Status == obs.StatusAbsent {
			o.Note = missingNote(swap) + " (" + o.Note + ")"
		}
		return nil, o, false
	}

	var out []obs.Observation
	for _, v := range cgroupValues {
		data, failed, ok := read(v.id, v.file, v.swap)
		if !ok {
			out = append(out, failed)
			continue
		}
		var val any
		var err error
		if v.limit {
			val, err = parseLimit(data)
		} else {
			val, err = parseInt(data)
			if err == nil && val.(int64) < 0 {
				err = fmt.Errorf("negative usage %d", val)
			}
		}
		if err != nil {
			out = append(out, malformed(v.id, source(v.file), err))
			continue
		}
		o := obs.OK(v.id, val, source(v.file))
		if val == "max" {
			o.Source.Detail += "; \"max\" means no limit"
		}
		out = append(out, o)
	}

	evIDs := make([]string, len(cgroupEvents))
	for i, e := range cgroupEvents {
		evIDs[i] = "mem.cgroup.events." + e
	}
	data, failed, ok := read(evIDs[0], "memory.events", false)
	if !ok {
		for _, id := range evIDs {
			f := failed
			f.ID = id
			out = append(out, f)
		}
		return out
	}
	events, err := parseFlatKeyed(data)
	for i, key := range cgroupEvents {
		src := source("memory.events")
		src.Field = key
		switch n, have := events[key]; {
		case err != nil:
			out = append(out, malformed(evIDs[i], src, err))
		case !have:
			out = append(out, obs.Absent(evIDs[i], src, "this kernel's memory.events has no "+key+" line"))
		default:
			out = append(out, obs.OK(evIDs[i], n, src))
		}
	}
	return out
}
