package storage

import (
	"reflect"
	"testing"
)

func TestParseScheduler(t *testing.T) {
	for _, tt := range []struct{ in, sel, all string }{
		{"[none] mq-deadline kyber bfq\n", "none", "none mq-deadline kyber bfq"},
		{"[mq-deadline] kyber bfq none", "mq-deadline", "mq-deadline kyber bfq none"},
		{"mq-deadline kyber [bfq] none", "bfq", "mq-deadline kyber bfq none"},
		{"none", "none", "none"},
		{"noop deadline [cfq]", "cfq", "noop deadline cfq"},
	} {
		sel, all, err := parseScheduler(tt.in)
		if err != nil || sel != tt.sel || all != tt.all {
			t.Errorf("parseScheduler(%q) = %q, %q, %v", tt.in, sel, all, err)
		}
	}
	for _, bad := range []string{"", "none mq-deadline", "[none] [bfq]", "[No/ne]", "[none] <script>"} {
		if _, _, err := parseScheduler(bad); err == nil {
			t.Errorf("parseScheduler(%q) accepted", bad)
		}
	}
}

func TestParseDiskStat(t *testing.T) {
	v419 := "  2048  10  40960  300  1024  5  20480  900  1  1100  1250  0  0  0  0\n"
	got, err := parseDiskStat(v419)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"reads": int64(2048), "reads_merged": int64(10), "read_bytes": int64(40960 * 512), "read_ms": int64(300),
		"writes": int64(1024), "writes_merged": int64(5), "write_bytes": int64(20480 * 512), "write_ms": int64(900),
		"in_flight": int64(1), "io_ms": int64(1100), "weighted_ms": int64(1250),
		"discards": int64(0), "discards_merged": int64(0), "discard_bytes": int64(0), "discard_ms": int64(0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("4.19 stat = %v", got)
	}
	old, err := parseDiskStat("1 2 3 4 5 6 7 8 9 10 11")
	if err != nil || len(old) != 11 || old["weighted_ms"] != int64(11) || old["discards"] != nil {
		t.Errorf("11-field stat = %v, %v", old, err)
	}
	modern, err := parseDiskStat("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17")
	if err != nil || modern["flushes"] != int64(16) || modern["flush_ms"] != int64(17) {
		t.Errorf("17-field stat = %v, %v", modern, err)
	}
	if future, err := parseDiskStat("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18"); err != nil || len(future) != 17 {
		t.Errorf("a field beyond 17 must be ignored: %v, %v", future, err)
	}
	for _, bad := range []string{"", "1 2 3", "1 2 3 4 5 6 7 8 9 10 11 12", "1 2 3 4 5 6 7 8 9 10 x", "1 2 -3 4 5 6 7 8 9 10 11",
		"1 2 99999999999999999 4 5 6 7 8 9 10 11"} {
		if _, err := parseDiskStat(bad); err == nil {
			t.Errorf("parseDiskStat(%q) accepted", bad)
		}
	}
}

func TestParsePSI(t *testing.T) {
	got, err := parsePSI([]byte("some avg10=1.50 avg60=0.75 avg300=0.25 total=123456\nfull avg10=0.50 avg60=0.25 avg300=0.10 total=65432\n"))
	if err != nil || got["some"].avg[0] != 1.5 || got["full"].total != 65432 {
		t.Errorf("parsePSI = %v, %v", got, err)
	}
	for _, bad := range []string{
		"", "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"some avg10=0.00 avg60=0.00 avg300=0.00 total=1\nfull avg10=101 avg60=0 avg300=0 total=1\n",
		"some avg10=0 avg60=0 avg300=0 total=-1\nfull avg10=0 avg60=0 avg300=0 total=1\n",
		"some avg10=0 avg60=0 avg300=0 total=1\nsome avg10=0 avg60=0 avg300=0 total=1\n",
		"half avg10=0\n", "some avg10\n",
	} {
		if _, err := parsePSI([]byte(bad)); err == nil {
			t.Errorf("parsePSI(%q) accepted", bad)
		}
	}
}

func TestParseFilesystems(t *testing.T) {
	got, err := parseFilesystems("nodev\tsysfs\nnodev\ttmpfs\n\text4\nnodev\toverlay\n\tvfat\n")
	want := []map[string]any{
		{"name": "sysfs", "nodev": true}, {"name": "tmpfs", "nodev": true}, {"name": "ext4", "nodev": false},
		{"name": "overlay", "nodev": true}, {"name": "vfat", "nodev": false},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("parseFilesystems = %v, %v", got, err)
	}
	for _, bad := range []string{"", "ext4\n", "dev\text4\n", "nodev\t/etc/passwd\n", "\t\n"} {
		if _, err := parseFilesystems(bad); err == nil {
			t.Errorf("parseFilesystems(%q) accepted", bad)
		}
	}
}
