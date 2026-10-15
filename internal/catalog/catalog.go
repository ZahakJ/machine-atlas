// Package catalog is the data dictionary: every observation and derived id
// Machine Atlas knows, with what it means, its unit and type, and what it
// cannot tell you. Collectors may only emit declared ids, snapshot validation
// type-checks values against it, redaction reads its sensitivity classes, and
// the inspector shows its descriptions next to every value.
//
// Declarations live in one file per namespace (catalog_mem.go,
// catalog_system.go, catalog_sched.go, catalog_net.go, catalog_probe.go,
// catalog_derived.go); adding a namespace means adding a file and listing
// its slice in index below.
package catalog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Sensitivity classifies how identifying a value can be; snapshot redaction
// decides what to replace by class.
type Sensitivity string

const (
	None     Sensitivity = "none"
	Hostname Sensitivity = "hostname"
	Username Sensitivity = "username"
	Path     Sensitivity = "path"
	Address  Sensitivity = "address"
	Serial   Sensitivity = "serial"
	// Freetext is text written by firmware, vendors or the kernel build
	// (CPU model, DMI product, kernel version string): not secret, but
	// specific enough to narrow down a machine.
	Freetext Sensitivity = "freetext"
)

// Valid reports whether s is a defined class.
func (s Sensitivity) Valid() bool {
	switch s {
	case None, Hostname, Username, Path, Address, Serial, Freetext:
		return true
	}
	return false
}

// TypeLimit is the value type of cgroup limits: an integer number of bytes,
// or the string "max" when no limit is set. The kernel writes "max" rather
// than a number, and inventing a number for "unlimited" would be fabrication.
const TypeLimit = "limit"

// Basis values for derived facts.
const (
	Arithmetic = "arithmetic"
	Inference  = "inference"
)

// Entry describes one id, or a family of ids when ID ends in ".*".
type Entry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	// Type is obs.TypeInt, TypeFloat, TypeString, TypeBool, TypeTable or
	// TypeLimit.
	Type string `json:"type"`
	// Columns lists a table's columns; a name ending in "*" matches any
	// column with that prefix (buddyinfo has one column per order, and the
	// number of orders depends on the kernel configuration).
	Columns []string `json:"columns,omitempty"`
	// Kind and Scope are empty for derived facts, which are not readings.
	Kind        obs.Kind    `json:"kind,omitempty"`
	Scope       obs.Scope   `json:"scope,omitempty"`
	Sensitivity Sensitivity `json:"sensitivity"`
	// IdentifyingColumns are table columns that name specific hardware or
	// configuration (device and interface names); the strict redaction
	// policy replaces them with numbered placeholders.
	IdentifyingColumns []string `json:"identifying_columns,omitempty"`
	// SharedIdentifiers maps identifying columns whose values name objects
	// that other tables name too (network interfaces) to a namespace;
	// strict redaction numbers them once per snapshot under that namespace
	// (<if-1>, <if-2>, ...), so a placeholder means the same interface in
	// every table and joins between tables still work.
	SharedIdentifiers map[string]string `json:"shared_identifiers,omitempty"`
	// ColumnSensitivity overrides Sensitivity for single table columns, so
	// a table can hold an identifying cell (an address, a MAC) next to
	// harmless ones that survive redaction (that address's class).
	ColumnSensitivity map[string]Sensitivity `json:"column_sensitivity,omitempty"`
	// Basis is "arithmetic" or "inference" for derived facts, empty otherwise.
	Basis string `json:"basis,omitempty"`
	// Caveat says what this number cannot tell you.
	Caveat string `json:"caveat"`
	// Privileged says what running as root would add.
	Privileged string `json:"privileged"`
}

// ColumnClass is the sensitivity of a table column: its own class when
// the entry declares one, otherwise the entry's.
func (e Entry) ColumnClass(col string) Sensitivity {
	if c, ok := e.ColumnSensitivity[col]; ok {
		return c
	}
	return e.Sensitivity
}

// Family reports whether e declares a wildcard family rather than one id.
func (e Entry) Family() bool { return strings.HasSuffix(e.ID, ".*") }

// Check reports whether v, a normalized non-null value, has the type this
// entry declares and a plausible value; for tables it also checks every
// cell's column is declared.
func (e Entry) Check(v any) error {
	if err := e.checkType(v); err != nil {
		return err
	}
	return e.checkRange(v)
}

// checkRange rejects integers no kernel interface produces: a negative
// size, page count or bit width, and a base page size that is not a power
// of two between 4 KiB and 1 MiB. Such values in an imported snapshot would
// otherwise flow into arithmetic and come out as confident nonsense.
func (e Entry) checkRange(v any) error {
	n, isInt := v.(int64)
	if !isInt {
		return nil
	}
	switch e.Unit {
	case obs.UnitBytes, obs.UnitPages, obs.UnitBits:
		if n < 0 {
			return fmt.Errorf("%d is negative, which no %s can be", n, e.Unit)
		}
	}
	if e.ID == "kernel.page_size_bytes" && (n < 4096 || n > 1<<20 || n&(n-1) != 0) {
		return fmt.Errorf("%d is not a base page size (a power of two from 4 KiB to 1 MiB)", n)
	}
	return nil
}

func (e Entry) checkType(v any) error {
	got := obs.TypeOf(v)
	switch e.Type {
	case obs.TypeFloat:
		if got == obs.TypeFloat || got == obs.TypeInt {
			return nil
		}
	case TypeLimit:
		if got == obs.TypeInt || v == "max" {
			return nil
		}
	case obs.TypeTable:
		if got != obs.TypeTable {
			break
		}
		for i, row := range v.([]map[string]any) {
			for col := range row {
				if !e.hasColumn(col) {
					return fmt.Errorf("row %d has column %q, which %s does not declare", i, col, e.ID)
				}
			}
		}
		return nil
	default:
		if got == e.Type {
			return nil
		}
	}
	return fmt.Errorf("value of type %s, catalog declares %s", got, e.Type)
}

func (e Entry) hasColumn(name string) bool {
	for _, c := range e.Columns {
		if c == name || (strings.HasSuffix(c, "*") && strings.HasPrefix(name, strings.TrimSuffix(c, "*"))) {
			return true
		}
	}
	return false
}

var exact, families = index(memEntries, systemEntries, schedEntries, storageEntries, netEntries, probeEntries, derivedEntries, schedDerivedEntries, storageDerivedEntries, netDerivedEntries)

// index builds the lookup tables. Duplicate ids are a programming error in
// this package and fail at start-up rather than resolving silently.
func index(groups ...[]Entry) (map[string]Entry, []Entry) {
	ex := map[string]Entry{}
	var fam []Entry
	for _, g := range groups {
		for _, e := range g {
			if e.Sensitivity == "" {
				e.Sensitivity = None
			}
			if e.Family() {
				fam = append(fam, e)
				continue
			}
			if _, dup := ex[e.ID]; dup {
				panic("catalog: duplicate id " + e.ID)
			}
			ex[e.ID] = e
		}
	}
	// Longest prefix first, so the most specific family wins.
	slices.SortFunc(fam, func(a, b Entry) int { return len(b.ID) - len(a.ID) })
	return ex, fam
}

// Lookup returns the entry for id: an exact declaration if there is one,
// otherwise the most specific wildcard family that covers it.
func Lookup(id string) (Entry, bool) {
	if e, ok := exact[id]; ok {
		return e, true
	}
	for _, f := range families {
		prefix := strings.TrimSuffix(f.ID, "*")
		if strings.HasPrefix(id, prefix) && len(id) > len(prefix) {
			return f, true
		}
	}
	return Entry{}, false
}

// All returns every exact entry, sorted by id.
func All() []Entry {
	out := make([]Entry, 0, len(exact))
	for _, e := range exact {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Families returns the wildcard family entries, sorted by id.
func Families() []Entry {
	out := slices.Clone(families)
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Text shared by many entries.
const (
	privSame      = "Nothing: the file is world-readable and root reads the same value."
	cumulative    = "A counter since boot, system-wide: one reading cannot give a rate or say which process caused it; take two readings and subtract."
	instantaneous = "An instantaneous reading, not synchronised with other files read moments apart."
)
