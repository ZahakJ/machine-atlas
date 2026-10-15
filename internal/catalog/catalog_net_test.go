package catalog

import (
	"slices"
	"strings"
	"testing"
)

// Per-column classes and shared identifiers must name real columns, and a
// shared identifier is always an identifying column too.
func TestColumnRedactionFieldsNameColumns(t *testing.T) {
	for _, e := range All() {
		for col, class := range e.ColumnSensitivity {
			if !e.hasColumn(col) || !class.Valid() {
				t.Errorf("%s: column_sensitivity %q: %q", e.ID, col, class)
			}
		}
		for col, ns := range e.SharedIdentifiers {
			if !e.hasColumn(col) || !slices.Contains(e.IdentifyingColumns, col) || ns == "" {
				t.Errorf("%s: shared identifier %q (%q) must be a declared identifying column", e.ID, col, ns)
			}
		}
	}
}

// Every network table that names an interface shares the "if" namespace,
// so strict redaction numbers an interface the same way everywhere.
func TestInterfaceColumnsShareOneNamespace(t *testing.T) {
	var n int
	for _, e := range All() {
		if !strings.HasPrefix(e.ID, "net.") {
			continue
		}
		for _, col := range []string{"interface", "name", "master"} {
			if e.hasColumn(col) {
				n++
				if e.SharedIdentifiers[col] != ifaceColumn {
					t.Errorf("%s: column %s is not in the %q namespace", e.ID, col, ifaceColumn)
				}
			}
		}
	}
	if n < 7 {
		t.Errorf("only %d interface columns found", n)
	}
	for _, id := range []string{"net.addresses.ipv4", "net.addresses.ipv6", "net.routes.ipv4", "net.routes.ipv6", "net.interfaces"} {
		e, _ := Lookup(id)
		for _, col := range []string{"address", "destination", "gateway", "mac"} {
			if e.hasColumn(col) && e.ColumnClass(col) != Address {
				t.Errorf("%s: column %s is %q, want address", id, col, e.ColumnClass(col))
			}
		}
		if e.ColumnClass("class") != None || e.ColumnClass("type") != None {
			t.Errorf("%s: computed columns must stay class none", id)
		}
	}
}
