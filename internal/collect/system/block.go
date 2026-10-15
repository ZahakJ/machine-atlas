package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

const sysBlock = "/sys/block"

var blockCollector = collector{
	id:   "system.block",
	owns: []string{"block.devices"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		return []obs.Observation{blockDevices(env)}
	},
}

func blockDevices(env collect.Env) obs.Observation {
	const id = "block.devices"
	src := obs.File(sysBlock, "")
	src.Detail = "*/size (512-byte sectors), queue/rotational, removable"
	names, err := env.ReadDir(sysBlock)
	if err != nil {
		return obs.Errored(id, src, err)
	}
	rows := []map[string]any{}
	for _, name := range names {
		dir := sysBlock + "/" + name
		sectors, err := readParsed(env, dir+"/size", parseSectors)
		if err != nil {
			return obs.Errored(id, src, err)
		}
		typ := blockType(name)
		// Distributions create eight unused loop devices and some kernels
		// sixteen ram disks; a zero size means nothing is attached.
		if (typ == "loop" || typ == "ram") && sectors == 0 {
			continue
		}
		row := map[string]any{"name": name, "size_bytes": sectors * 512, "type": typ}
		for _, f := range [...]struct{ col, file string }{{"rotational", "/queue/rotational"}, {"removable", "/removable"}} {
			b, err := readBoolFile(env, dir+f.file)
			switch {
			case err == nil:
				row[f.col] = b
			case !errors.Is(err, fs.ErrNotExist):
				return obs.Errored(id, src, err)
			}
		}
		rows = append(rows, row)
	}
	return obs.OK(id, rows, src)
}

// parseSectors parses a sysfs size file. The kernel always counts it in
// 512-byte sectors, whatever the device's logical block size.
func parseSectors(data []byte) (int64, error) {
	s := strings.TrimSpace(string(data))
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || n > (1<<63-1)/512 {
		return 0, fmt.Errorf("malformed sector count %q", s)
	}
	return n, nil
}

// readBoolFile reads a sysfs 0/1 flag.
func readBoolFile(env collect.Env, path string) (bool, error) {
	s, err := readTrim(env, path)
	if err != nil {
		return false, err
	}
	switch s {
	case "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, fmt.Errorf("%s: expected 0 or 1, got %q", path, s)
}

// blockType classifies a /sys/block name by its driver's naming scheme.
// Partitions never appear at this level, only whole devices.
func blockType(name string) string {
	for _, p := range []struct{ prefix, typ string }{
		{"loop", "loop"}, {"ram", "ram"}, {"zram", "zram"}, {"dm-", "dm"},
		{"md", "raid"}, {"sr", "rom"}, {"nbd", "nbd"},
	} {
		if strings.HasPrefix(name, p.prefix) {
			return p.typ
		}
	}
	return "disk"
}

// lsblkArgv is the exact command; util-linux added --json in 2.27.
var lsblkArgv = []string{"lsblk", "--json", "--bytes", "--output", "NAME,TYPE,SIZE,FSTYPE"}

var lsblkCollector = collector{
	id:   "system.lsblk",
	owns: []string{"block.lsblk"},
	run: func(ctx context.Context, env collect.Env) []obs.Observation {
		const id = "block.lsblk"
		src := obs.Command(lsblkArgv...)
		out, err := env.Run(ctx, lsblkArgv[0], lsblkArgv[1:]...)
		var exit *collect.ExitError
		switch {
		case errors.As(err, &exit) && rejectsJSON(exit.Stderr):
			return []obs.Observation{obs.Unsupported(id, src,
				"this lsblk does not accept --json (util-linux older than 2.27): "+strings.TrimSpace(exit.Stderr))}
		case errors.As(err, &exit) && strings.Contains(exit.Stderr, "Permission denied"):
			// lsblk reads sysfs; when that is restricted the tool reports
			// the kernel's refusal, which is what this status means.
			return []obs.Observation{obs.Denied(id, src, strings.TrimSpace(exit.Error()))}
		case err != nil:
			o := obs.Errored(id, src, err)
			if o.Status == obs.StatusAbsent {
				o.Note = "lsblk is not installed; block.devices reads the same devices from /sys/block"
			}
			return []obs.Observation{o}
		}
		rows, err := parseLsblk(out)
		if err != nil {
			return []obs.Observation{obs.Failed(id, src, err.Error())}
		}
		return []obs.Observation{obs.OK(id, rows, src)}
	},
}

// rejectsJSON recognises getopt's complaint about an unknown option.
func rejectsJSON(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "json") &&
		(strings.Contains(s, "unrecognized option") || strings.Contains(s, "invalid option") || strings.Contains(s, "unknown option"))
}

type lsblkDevice struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Size     json.RawMessage `json:"size"`
	FSType   *string         `json:"fstype"`
	Children []lsblkDevice   `json:"children"`
}

// maxLsblkDepth bounds the device tree (disk → partition → crypt → lvm is
// four levels), so hostile output cannot recurse without limit.
const maxLsblkDepth = 8

// parseLsblk flattens lsblk's JSON tree into rows, parents before
// children. Older util-linux versions print sizes as strings, newer ones
// as numbers; both are accepted. A null fstype leaves the cell out.
func parseLsblk(data []byte) ([]map[string]any, error) {
	var doc struct {
		BlockDevices *[]lsblkDevice `json:"blockdevices"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("lsblk output: %w", err)
	}
	if doc.BlockDevices == nil {
		return nil, errors.New("lsblk output has no blockdevices array")
	}
	rows := []map[string]any{}
	var walk func(devs []lsblkDevice, depth int) error
	walk = func(devs []lsblkDevice, depth int) error {
		if depth > maxLsblkDepth {
			return errors.New("lsblk device tree is implausibly deep")
		}
		for _, d := range devs {
			if d.Name == "" || d.Type == "" {
				return fmt.Errorf("lsblk device without name or type: %+v", d)
			}
			size, err := lsblkSize(d.Size)
			if err != nil {
				return fmt.Errorf("lsblk device %s: %w", d.Name, err)
			}
			row := map[string]any{"name": scrubUUID(d.Name), "type": d.Type, "size_bytes": size}
			if d.FSType != nil && *d.FSType != "" {
				row["fstype"] = *d.FSType
			}
			rows = append(rows, row)
			if err := walk(d.Children, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(*doc.BlockDevices, 0); err != nil {
		return nil, err
	}
	return rows, nil
}

func lsblkSize(raw json.RawMessage) (int64, error) {
	s := strings.Trim(string(raw), `"`)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("size %s is not a byte count (was --bytes ignored?)", raw)
	}
	return n, nil
}

var uuidLike = regexp.MustCompile(`(?i)[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}`)

// scrubUUID replaces UUIDs embedded in device names. Device-mapper names
// made by installers ("luks-<uuid>") carry the filesystem UUID, which
// identifies one machine; the rest of the name stays, since it says what
// kind of volume this is.
func scrubUUID(s string) string { return uuidLike.ReplaceAllString(s, "<uuid>") }
