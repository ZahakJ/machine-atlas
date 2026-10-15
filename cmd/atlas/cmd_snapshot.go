package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
)

const snapshotSynopsis = `atlas snapshot validate FILE
       atlas snapshot preview FILE [--policy default|strict]
       atlas snapshot redact FILE -o OUT [--policy default|strict]
       atlas snapshot show FILE [--status STATUS] [--prefix ID]`

func cmdSnapshot(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s\n", snapshotSynopsis)
		return 2
	}
	switch args[0] {
	case "validate":
		return snapshotValidate(args[1:])
	case "preview":
		return snapshotPreview(args[1:])
	case "redact":
		return snapshotRedact(args[1:])
	case "show":
		return snapshotShow(args[1:])
	case "-h", "-help", "--help", "help":
		fmt.Printf("Usage: %s\n", snapshotSynopsis)
		return 0
	}
	fmt.Fprintf(os.Stderr, "atlas snapshot: unknown subcommand %q\nUsage: %s\n", args[0], snapshotSynopsis)
	return 2
}

// oneFile parses flags and requires exactly one positional FILE.
func oneFile(fs *flag.FlagSet, args []string) (string, int) {
	pos, code := parseArgs(fs, args)
	if code >= 0 {
		return "", code
	}
	if len(pos) != 1 {
		return "", usageError(fs, "want exactly one snapshot FILE")
	}
	return pos[0], -1
}

// loadFile reads and validates a snapshot file. Snapshot files are
// untrusted input: Load bounds the read and rejects anything off-schema.
func loadFile(path string) (*snapshot.Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return snapshot.Load(f)
}

func snapshotValidate(args []string) int {
	fs := newFlags("snapshot validate", "atlas snapshot validate FILE")
	path, code := oneFile(fs, args)
	if code >= 0 {
		return code
	}
	f, err := os.Open(path)
	if err != nil {
		return fail("snapshot validate", err)
	}
	defer f.Close()
	s, err := snapshot.Decode(f)
	if err != nil {
		return fail("snapshot validate", err)
	}
	if errs := snapshot.Validate(s); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "%s: invalid, %d problems:\n", path, len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  %v\n", e)
		}
		return 1
	}
	unknown := 0
	for _, o := range s.Observations {
		if _, ok := catalog.Lookup(o.ID); !ok {
			unknown++
		}
	}
	fmt.Printf("%s: valid schema %d snapshot, %d observations (%d not in this version's catalog), %s\n",
		path, s.SchemaVersion, len(s.Observations), unknown, s.ID)
	return 0
}

func snapshotPreview(args []string) int {
	fs := newFlags("snapshot preview", "atlas snapshot preview FILE [--policy default|strict]")
	policy := fs.String("policy", snapshot.PolicyDefault, "redaction `policy`: default or strict")
	path, code := oneFile(fs, args)
	if code >= 0 {
		return code
	}
	s, err := loadFile(path)
	if err != nil {
		return fail("snapshot preview", err)
	}
	changes, err := snapshot.Preview(s, *policy)
	if err != nil {
		return usageError(fs, "%v", err)
	}
	if len(changes) == 0 {
		fmt.Printf("redaction (%s) would change nothing\n", *policy)
		return 0
	}
	fmt.Printf("redaction (%s) would make %d replacements:\n", *policy, len(changes))
	printChanges(os.Stdout, changes)
	return 0
}

func snapshotRedact(args []string) int {
	fs := newFlags("snapshot redact", "atlas snapshot redact FILE -o OUT [--policy default|strict]")
	out := fs.String("o", "", "write the redacted snapshot to `OUT` (required)")
	policy := fs.String("policy", snapshot.PolicyDefault, "redaction `policy`: default or strict")
	path, code := oneFile(fs, args)
	if code >= 0 {
		return code
	}
	if *out == "" {
		return usageError(fs, "-o OUT is required")
	}
	s, err := loadFile(path)
	if err != nil {
		return fail("snapshot redact", err)
	}
	red, changes, err := snapshot.Redact(s, *policy)
	if err != nil {
		return usageError(fs, "%v", err)
	}
	printRedactionSummary(os.Stderr, *policy, changes)
	data, err := snapshot.Encode(red)
	if err == nil {
		err = writeFile(*out, data)
	}
	if err != nil {
		return fail("snapshot redact", err)
	}
	return 0
}

func snapshotShow(args []string) int {
	fs := newFlags("snapshot show", "atlas snapshot show FILE [--status STATUS] [--prefix ID]")
	status := fs.String("status", "", "only observations with this `status` (ok, absent, permission_denied, unsupported, failed, not_collected)")
	prefix := fs.String("prefix", "", "only ids starting with `ID`")
	path, code := oneFile(fs, args)
	if code >= 0 {
		return code
	}
	if *status != "" && !obs.Status(*status).Valid() {
		return usageError(fs, "unknown status %q", *status)
	}
	s, err := loadFile(path)
	if err != nil {
		return fail("snapshot show", err)
	}
	fmt.Printf("snapshot %s\ncreated %s, mode %s, app %s, redaction %s\n\n",
		s.ID, s.CreatedAt, describeMode(s), s.AppVersion, s.Redaction.Policy)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tVALUE\tUNIT")
	for _, o := range s.Observations {
		if (*status != "" && string(o.Status) != *status) || !strings.HasPrefix(o.ID, *prefix) {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", o.ID, o.Status, showValue(o), o.Unit)
	}
	if err := tw.Flush(); err != nil {
		return fail("snapshot show", err)
	}
	return 0
}

func describeMode(s *snapshot.Snapshot) string {
	if s.Collection.Mode == "fixture" {
		return fmt.Sprintf("fixture %q", s.Collection.Fixture)
	}
	return s.Collection.Mode
}

// showValue renders a value for the terminal. Strings are quoted, which
// escapes control characters an imported snapshot might carry.
func showValue(o obs.Observation) string {
	switch v := o.Value.(type) {
	case nil:
		if o.Note != "" {
			return fmt.Sprintf("(%q)", o.Note)
		}
		return "-"
	case string:
		return fmt.Sprintf("%q", v)
	case []map[string]any:
		return fmt.Sprintf("table, %d rows", len(v))
	default:
		return fmt.Sprint(v)
	}
}
