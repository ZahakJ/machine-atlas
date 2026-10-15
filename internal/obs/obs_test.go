package obs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

func TestFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Status
	}{
		{"nil", nil, StatusOK},
		{"ENOENT", syscall.ENOENT, StatusAbsent},
		{"wrapped ENOENT", &fs.PathError{Op: "open", Path: "/proc/pressure/memory", Err: syscall.ENOENT}, StatusAbsent},
		{"fs.ErrNotExist", fs.ErrNotExist, StatusAbsent},
		{"ENOTDIR", &fs.PathError{Op: "open", Path: "/sys/x/y", Err: syscall.ENOTDIR}, StatusAbsent},
		{"missing executable", &exec.Error{Name: "lsblk", Err: exec.ErrNotFound}, StatusAbsent},
		{"EACCES", &fs.PathError{Op: "open", Path: "/proc/slabinfo", Err: syscall.EACCES}, StatusPermissionDenied},
		{"EPERM", syscall.EPERM, StatusPermissionDenied},
		{"ErrUnsupported", errors.ErrUnsupported, StatusUnsupported},
		{"ENOSYS", syscall.ENOSYS, StatusUnsupported},
		{"timeout", context.DeadlineExceeded, StatusFailed},
		{"wrapped timeout", fmt.Errorf("run lsblk: %w", context.DeadlineExceeded), StatusFailed},
		{"EIO", syscall.EIO, StatusFailed},
		{"plain", errors.New("unexpected format"), StatusFailed},
	}
	for _, tt := range tests {
		if got := FromError(tt.err); got != tt.want {
			t.Errorf("%s: FromError = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestErrored(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/proc/slabinfo", Err: syscall.EACCES}
	o := Errored("mem.slabinfo.cache_count", File("/proc/slabinfo", ""), err)
	if o.Status != StatusPermissionDenied || o.Value != nil || o.Note != err.Error() {
		t.Fatalf("Errored = %+v", o)
	}
}

func TestConstructorsLeaveValueNull(t *testing.T) {
	src := File("/x", "")
	for _, o := range []Observation{
		Absent("a.b", src, "n"), Denied("a.b", src, "n"), Unsupported("a.b", src, "n"),
		Failed("a.b", src, "n"), NotCollected("a.b", src, "n"),
	} {
		if o.Value != nil || o.Status == StatusOK || !o.Status.Valid() {
			t.Errorf("%+v: want null value and a valid non-ok status", o)
		}
	}
}

func TestFieldID(t *testing.T) {
	tests := map[string]string{
		"Active(anon)":    "Active_anon",
		"Inactive(file)":  "Inactive_file",
		"MemTotal":        "MemTotal",
		"HugePages_Total": "HugePages_Total",
		"page-cluster":    "page-cluster",
		"odd name/x":      "odd_name_x",
	}
	for in, want := range tests {
		if got := FieldID(in); got != want {
			t.Errorf("FieldID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidID(t *testing.T) {
	for id, want := range map[string]bool{
		"mem.meminfo.Active_anon":  true,
		"mem.sysctl.vm.swappiness": true,
		"probe.status.VmRSS":       true,
		"Mem.x":                    false,
		"":                         false,
		"mem.meminfo.*":            false,
		"1mem":                     false,
		"mem x":                    false,
	} {
		if got := ValidID(id); got != want {
			t.Errorf("ValidID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any
		err  bool
	}{
		{"int", 3, int64(3), false},
		{"uint64", uint64(1 << 40), int64(1 << 40), false},
		{"uint64 overflow", uint64(math.MaxUint64), nil, true},
		{"float", 0.5, 0.5, false},
		{"NaN", math.NaN(), nil, true},
		{"json int", json.Number("17179869184"), int64(17179869184), false},
		{"json float", json.Number("2.5"), 2.5, false},
		{"string", "x", "x", false},
		{"nil", nil, nil, false},
		{"decoded table", []any{map[string]any{"name": "sda", "size_bytes": json.Number("512")}},
			[]map[string]any{{"name": "sda", "size_bytes": int64(512)}}, false},
		{"table of non-objects", []any{"x"}, nil, true},
		{"nested cell", []any{map[string]any{"a": map[string]any{}}}, nil, true},
		{"nested table cell", []map[string]any{{"a": []any{}}}, nil, true},
		{"map", map[string]any{}, nil, true},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in)
		if (err != nil) != tt.err {
			t.Errorf("%s: err = %v, want error %v", tt.name, err, tt.err)
			continue
		}
		if !tt.err && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %#v, want %#v", tt.name, got, tt.want)
		}
	}
}

func TestIntFloat(t *testing.T) {
	if v, ok := Int(2.0); !ok || v != 2 {
		t.Errorf("Int(2.0) = %v, %v", v, ok)
	}
	if _, ok := Int(2.5); ok {
		t.Error("Int(2.5) should fail")
	}
	if _, ok := Int("2"); ok {
		t.Error("Int(string) should fail")
	}
	if v, ok := Float(int64(3)); !ok || v != 3 {
		t.Errorf("Float(3) = %v, %v", v, ok)
	}
}
