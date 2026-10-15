package snapshot

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Redaction policies.
const (
	// PolicyNone leaves the snapshot unchanged.
	PolicyNone = "none"
	// PolicyDefault replaces hostname, username, address and serial values
	// wholesale and scrubs identifying patterns from every string.
	PolicyDefault = "default"
	// PolicyStrict also replaces free text (CPU model, DMI vendor and
	// product, device-tree model, kernel build string, driver names) and
	// numbers device and interface names.
	PolicyStrict = "strict"
)

var policies = []string{PolicyNone, PolicyDefault, PolicyStrict}

// Change is one replacement redaction makes. ID is the observation id, or
// empty for snapshot metadata; Field says where inside it ("value",
// "value[2].name", "note", "source.path", "collection.collectors[0].error").
type Change struct {
	ID     string `json:"id"`
	Field  string `json:"field"`
	Class  string `json:"class"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// pattern is one identifying shape found inside otherwise harmless strings.
type pattern struct {
	class catalog.Sensitivity
	re    *regexp.Regexp
	repl  string
}

// patterns are applied in order to every string. Placeholders contain '<',
// which none of the patterns accept where a name would be, so redacting an
// already redacted snapshot changes nothing.
var patterns = []pattern{
	{catalog.Username, regexp.MustCompile(`/home/[^/\s<>"']+`), "/home/<user>"},
	{catalog.Username, regexp.MustCompile(`/Users/[^/\s<>"']+`), "/Users/<user>"},
	{catalog.Username, regexp.MustCompile(`/run/user/\d+`), "/run/user/<uid>"},
	{catalog.Username, regexp.MustCompile(`\buser-\d+\.slice`), "user-<uid>.slice"},
	{catalog.Username, regexp.MustCompile(`@\d+\.service`), "@<uid>.service"},
	// user@host, as in a self-built kernel's version string or an email.
	{catalog.Username, regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z][A-Za-z0-9.-]*`), "<user>@<host>"},
	{catalog.Serial, regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	// 32 or more hex digits in a row: machine-id, boot-id and similar.
	{catalog.Serial, regexp.MustCompile(`(?i)\b[0-9a-f]{32,}\b`), "<id>"},
	{catalog.Address, regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}\b`), "<mac>"},
	// Interface names derived from a MAC address (enx001122334455).
	{catalog.Address, regexp.MustCompile(`(?i)\benx[0-9a-f]{12}\b`), "enx<mac>"},
	// Container network interfaces named after random ids (Docker bridges
	// br-<network id>, veth<hash>): unique to one machine's container setup.
	{catalog.Serial, regexp.MustCompile(`\bbr-[0-9a-f]{12}\b`), "br-<id>"},
	{catalog.Serial, regexp.MustCompile(`\bveth[0-9a-f]{7}\b`), "veth<id>"},
	{catalog.Address, regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`), "<ipv4>"},
	{catalog.Address, regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b|\b(?:[0-9a-f]{1,4}:){1,6}:(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,5}\b)?`), "<ipv6>"},
}

// replaced is how a whole value of a sensitive class reads after redaction.
func replaced(class catalog.Sensitivity) string { return "<redacted:" + string(class) + ">" }

// wholesale reports whether default redaction replaces values of class
// entirely, rather than scrubbing patterns from them.
func wholesale(class catalog.Sensitivity) bool {
	return class == catalog.Hostname || class == catalog.Username || class == catalog.Address || class == catalog.Serial
}

// Redact returns a redacted copy of s and the changes made, and seals the
// copy with its new id. The input is not modified.
func Redact(s *Snapshot, policy string) (*Snapshot, []Change, error) {
	if !slices.Contains(policies, policy) {
		return nil, nil, fmt.Errorf("unknown redaction policy %q (want none, default or strict)", policy)
	}
	out := clone(s)
	if policy == PolicyNone {
		return out, nil, nil
	}
	r := redactor{strict: policy == PolicyStrict, hostnames: hostnames(s)}
	for i := range out.Observations {
		r.observation(&out.Observations[i])
	}
	out.Collection.Fixture = r.scrub("", "collection.fixture", out.Collection.Fixture)
	for i := range out.Collection.Collectors {
		c := &out.Collection.Collectors[i]
		c.Error = r.scrub("", fmt.Sprintf("collection.collectors[%d].error", i), c.Error)
	}

	red := &out.Redaction
	red.Applied = true
	if red.Policy != PolicyStrict {
		red.Policy = policy
	}
	classes := slices.Clone(red.Classes)
	for _, c := range r.changes {
		classes = append(classes, c.Class)
	}
	slices.Sort(classes)
	red.Classes = slices.Compact(classes)
	red.Replaced += len(r.changes)
	out.Seal()
	return out, r.changes, nil
}

// Preview returns the changes Redact would make, without making them. The
// CLI and the export dialog show it before anything is written.
func Preview(s *Snapshot, policy string) ([]Change, error) {
	_, changes, err := Redact(s, policy)
	return changes, err
}

// clone deep-copies the parts of a snapshot redaction may change.
func clone(s *Snapshot) *Snapshot {
	c := *s
	c.Collection.Collectors = slices.Clone(s.Collection.Collectors)
	c.Redaction.Classes = slices.Clone(s.Redaction.Classes)
	c.Observations = slices.Clone(s.Observations)
	for i, o := range c.Observations {
		if rows, ok := o.Value.([]map[string]any); ok {
			copied := make([]map[string]any, len(rows))
			for j, row := range rows {
				copied[j] = maps.Clone(row)
			}
			c.Observations[i].Value = copied
		}
	}
	return &c
}

// hostnames collects the values of hostname-class observations, so the name
// can also be removed where it appears inside other text (a self-built
// kernel's version string, a note). Very short names are skipped: replacing
// every "a" or "pi" in a snapshot would destroy more than it protects.
func hostnames(s *Snapshot) []string {
	var out []string
	for _, o := range s.Observations {
		e, ok := catalog.Lookup(o.ID)
		h, isString := o.Value.(string)
		if ok && e.Sensitivity == catalog.Hostname && isString && len(h) >= 3 && !strings.Contains(h, "<") {
			out = append(out, h)
		}
	}
	return out
}

type redactor struct {
	strict    bool
	hostnames []string
	changes   []Change
	numbered  map[string]map[string]string // numbering key → original → placeholder
}

func (r *redactor) record(id, field string, class catalog.Sensitivity, before, after string) {
	r.changes = append(r.changes, Change{ID: id, Field: field, Class: string(class), Before: before, After: after})
}

func (r *redactor) observation(o *obs.Observation) {
	first := len(r.changes)
	// An id missing from this version's catalog has class "", and its
	// strings are scrubbed like free text.
	entry, _ := catalog.Lookup(o.ID)
	switch v := o.Value.(type) {
	case string:
		o.Value = r.scalar(o.ID, entry.Sensitivity, v)
	case []map[string]any:
		r.table(o.ID, entry, v)
	}
	o.Source.Path = r.scrub(o.ID, "source.path", o.Source.Path)
	o.Source.Field = r.scrub(o.ID, "source.field", o.Source.Field)
	o.Source.Detail = r.scrub(o.ID, "source.detail", o.Source.Detail)
	o.Note = r.scrub(o.ID, "note", o.Note)

	if len(r.changes) > first {
		var classes []string
		for _, c := range r.changes[first:] {
			classes = append(classes, c.Class)
		}
		slices.Sort(classes)
		mark := "redacted (" + strings.Join(slices.Compact(classes), ", ") + ")"
		if o.Note == "" {
			o.Note = mark
		} else {
			o.Note += "; " + mark
		}
	}
}

// scalar redacts a string value according to its class.
func (r *redactor) scalar(id string, class catalog.Sensitivity, v string) string {
	switch {
	case wholesale(class) || (r.strict && class == catalog.Freetext):
		if after := replaced(class); v != after {
			r.record(id, "value", class, v, after)
			return after
		}
		return v
	case class == catalog.None:
		// Values the catalog knows to be harmless configuration words are
		// spared the hostname search, which would otherwise erase a
		// distribution id ("debian") that happens to equal the machine's
		// name.
		return r.patterns(id, "value", v)
	default:
		return r.scrub(id, "value", v)
	}
}

// table redacts every string cell by its column's class
// (catalog.Entry.ColumnClass). Under strict, identifying columns are
// numbered first: per observation and column, or once per snapshot for
// columns in a shared namespace (interface names), so the same interface
// reads <if-2> in every table. Columns are visited in sorted order so the
// change list is deterministic.
func (r *redactor) table(id string, e catalog.Entry, rows []map[string]any) {
	for i, row := range rows {
		for _, col := range slices.Sorted(maps.Keys(row)) {
			v, ok := row[col].(string)
			if !ok {
				continue
			}
			field := fmt.Sprintf("value[%d].%s", i, col)
			class := e.ColumnClass(col)
			switch {
			case r.strict && slices.Contains(e.IdentifyingColumns, col):
				if strings.Contains(v, "<") {
					continue // already a placeholder
				}
				after := r.number(id, col, e.SharedIdentifiers[col], v)
				r.record(id, field, catalog.Freetext, v, after)
				row[col] = after
			case wholesale(class) || (r.strict && class == catalog.Freetext):
				if after := replaced(class); v != after {
					r.record(id, field, class, v, after)
					row[col] = after
				}
			default:
				row[col] = r.scrub(id, field, v)
			}
		}
	}
}

// number returns the numbered placeholder for v in an identifying column:
// <ns-N> counted across the whole snapshot when the column has a shared
// namespace, otherwise <col-N> counted within this observation's column.
func (r *redactor) number(id, col, ns, v string) string {
	key := "shared:" + ns
	if ns == "" {
		ns, key = col, id+"/"+col
	}
	if r.numbered == nil {
		r.numbered = map[string]map[string]string{}
	}
	m := r.numbered[key]
	if m == nil {
		m = map[string]string{}
		r.numbered[key] = m
	}
	after, seen := m[v]
	if !seen {
		after = fmt.Sprintf("<%s-%d>", ns, len(m)+1)
		m[v] = after
	}
	return after
}

// scrub removes identifying patterns and then known hostnames from free
// text. Patterns go first so "user@host" is recognised whole before the
// hostname inside it is replaced.
func (r *redactor) scrub(id, field, s string) string {
	s = r.patterns(id, field, s)
	for _, h := range r.hostnames {
		if after := replaceWord(s, h, replaced(catalog.Hostname)); after != s {
			r.record(id, field, catalog.Hostname, s, after)
			s = after
		}
	}
	return s
}

// patterns applies the identifying patterns, recording one change per
// pattern that matched.
func (r *redactor) patterns(id, field, s string) string {
	for _, p := range patterns {
		if after := p.re.ReplaceAllLiteralString(s, p.repl); after != s {
			r.record(id, field, p.class, s, after)
			s = after
		}
	}
	return s
}

// replaceWord replaces case-insensitive occurrences of word in s that are
// not part of a longer name (so "box" is replaced in "box.lan" but not in
// "mailbox").
func replaceWord(s, word, repl string) string {
	var b strings.Builder
	last := 0
	for i := 0; i+len(word) <= len(s); {
		end := i + len(word)
		if strings.EqualFold(s[i:end], word) && (i == 0 || !nameByte(s[i-1])) && (end == len(s) || !nameByte(s[end])) {
			b.WriteString(s[last:i])
			b.WriteString(repl)
			last, i = end, end
			continue
		}
		i++
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
