package storage

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const filesystemsPath = "/proc/filesystems"

var fsTypesCollector = collector{
	id:   "storage.fs_types",
	owns: []string{"fs.types"},
	read: func(env collect.Env) []obs.Observation {
		const id = "fs.types"
		src := obs.File(filesystemsPath, "")
		data, err := env.ReadFile(filesystemsPath)
		if err != nil {
			return []obs.Observation{obs.Errored(id, src, err)}
		}
		rows, err := parseFilesystems(string(data))
		if err != nil {
			return []obs.Observation{obs.Failed(id, src, "unexpected format: "+err.Error())}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

var fsName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// parseFilesystems parses /proc/filesystems: one type per line, preceded
// by "nodev" and a tab when the type needs no block device, by a tab alone
// otherwise.
func parseFilesystems(s string) ([]map[string]any, error) {
	rows := []map[string]any{}
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		flag, name, ok := strings.Cut(line, "\t")
		if !ok || (flag != "" && flag != "nodev") || !fsName.MatchString(name) {
			return nil, fmt.Errorf("line %d: %q", i+1, clip(line))
		}
		rows = append(rows, map[string]any{"name": name, "nodev": flag == "nodev"})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no filesystem types")
	}
	return rows, nil
}
