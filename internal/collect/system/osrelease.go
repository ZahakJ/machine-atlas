package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// os-release locations in lookup order. The specification
// (os-release(5)) says to read /etc/os-release and fall back to
// /usr/lib/os-release only when the first does not exist.
const (
	osReleaseEtc = "/etc/os-release"
	osReleaseLib = "/usr/lib/os-release"
)

// osReleaseFields maps each id suffix to the os-release key it reads.
var osReleaseFields = []struct{ id, key, missing string }{
	{"os.release.id", "ID", "os-release has no ID line (the specification's default would be \"linux\"; atlas does not assume it)"},
	{"os.release.id_like", "ID_LIKE", "os-release has no ID_LIKE line: the distribution names no parent"},
	{"os.release.name", "NAME", "os-release has no NAME line (the specification's default would be \"Linux\"; atlas does not assume it)"},
	{"os.release.version_id", "VERSION_ID", "os-release has no VERSION_ID line, as is usual for rolling releases"},
}

var osReleaseCollector = collector{
	id:   "system.os-release",
	owns: []string{"os.release.id", "os.release.id_like", "os.release.name", "os.release.version_id"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		path := osReleaseEtc
		data, err := env.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			path = osReleaseLib
			data, err = env.ReadFile(path)
		}
		var vars map[string]string
		if err == nil {
			vars, err = parseOSRelease(data)
		}
		out := make([]obs.Observation, 0, len(osReleaseFields))
		for _, f := range osReleaseFields {
			src := obs.File(path, f.key)
			v, ok := vars[f.key]
			switch {
			case err != nil:
				out = append(out, obs.Errored(f.id, src, err))
			case !ok || v == "":
				out = append(out, obs.Absent(f.id, src, f.missing))
			default:
				out = append(out, obs.OK(f.id, v, src))
			}
		}
		return out
	},
}

var hostnameCollector = collector{
	id:   "system.hostname",
	owns: []string{"os.hostname"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{stringFile(env, "os.hostname", "/proc/sys/kernel/hostname")}
	},
}

// parseOSRelease parses the os-release format: KEY=value lines in a subset
// of shell syntax, with "#" comments and blank lines. Values may be bare or
// in single or double quotes; inside double quotes a backslash escapes
// $ " \ and `. A line that is not an assignment, or an unterminated quote,
// is an error rather than something to skip, so a corrupt file is reported
// as failed instead of yielding half its fields.
func parseOSRelease(data []byte) (map[string]string, error) {
	vars := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || !validShellName(key) {
			return nil, fmt.Errorf("os-release line %d: expected KEY=value, got %q", i+1, line)
		}
		v, err := unquoteShell(raw)
		if err != nil {
			return nil, fmt.Errorf("os-release line %d: %w", i+1, err)
		}
		vars[key] = v
	}
	return vars, nil
}

func validShellName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func unquoteShell(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	switch q := s[0]; q {
	case '\'':
		if len(s) < 2 || s[len(s)-1] != '\'' || strings.Contains(s[1:len(s)-1], "'") {
			return "", fmt.Errorf("bad single-quoted value %s", s)
		}
		return s[1 : len(s)-1], nil
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '\\':
				if i+1 < len(s) && strings.IndexByte("$\"\\`", s[i+1]) >= 0 {
					i++
					b.WriteByte(s[i])
				} else {
					b.WriteByte(c)
				}
			case '"':
				if i != len(s)-1 {
					return "", fmt.Errorf("text after closing quote in %s", s)
				}
				return b.String(), nil
			default:
				b.WriteByte(c)
			}
		}
		return "", fmt.Errorf("unterminated double quote in %s", s)
	}
	if strings.ContainsAny(s, "\"'` \t") {
		return "", fmt.Errorf("unquoted value with quotes or spaces: %s", s)
	}
	return s, nil
}
