package catalog

import "testing"

// Every column that names a block device is numbered in the shared "dev"
// namespace under strict redaction, so one device reads the same in the
// device list, the queue, statistics and BDI tables, the mount table and
// the swap areas.
func TestDeviceColumnsShareOneNamespace(t *testing.T) {
	for id, cols := range map[string][]string{
		"block.devices": {"name"}, "block.lsblk": {"name"}, "block.queues": {"name"}, "block.stats": {"name"},
		"block.bdi": {"name"}, "mem.swap.areas": {"name"}, "mounts.filesystems": {"device", "disk"},
	} {
		e, ok := Lookup(id)
		if !ok {
			t.Fatalf("%s not declared", id)
		}
		for _, col := range cols {
			if e.SharedIdentifiers[col] != devColumn {
				t.Errorf("%s: column %s is not in the %q namespace", id, col, devColumn)
			}
		}
	}
	if e, _ := Lookup("mounts.filesystems"); e.ColumnClass("options") != Path || !e.hasColumn("options") {
		t.Error("mounts.filesystems options must be a declared column of the table's path class")
	}
}
