package collect

import (
	"reflect"
	"testing"
)

func TestParseFields(t *testing.T) {
	in := "MemTotal:       16318412 kB\nActive(anon):     123 kB\n\nHugePages_Total:       0\nName:\tatlas\n"
	got, err := ParseFields([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{{"MemTotal", "16318412 kB"}, {"Active(anon)", "123 kB"}, {"HugePages_Total", "0"}, {"Name", "atlas"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, bad := range []string{"MemTotal 1 kB\n", ": 3\n"} {
		if _, err := ParseFields([]byte(bad)); err == nil {
			t.Errorf("ParseFields(%q): want error", bad)
		}
	}
}

func TestParseKB(t *testing.T) {
	tests := []struct {
		in    string
		want  int64
		kb    bool
		error bool
	}{
		{"16318412 kB", 16318412 * 1024, true, false},
		{"  0 kB", 0, true, false},
		{"42", 42, false, false},
		{"12 MB", 0, false, true},
		{"x kB", 0, false, true},
		{"", 0, false, true},
		{"9223372036854775807 kB", 0, false, true},
	}
	for _, tt := range tests {
		got, kb, err := ParseKB(tt.in)
		if (err != nil) != tt.error || got != tt.want || kb != tt.kb {
			t.Errorf("ParseKB(%q) = %d, %v, %v; want %d, %v, error %v", tt.in, got, kb, err, tt.want, tt.kb, tt.error)
		}
	}
}

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		in   string
		want []int
		err  bool
	}{
		{"0-3,8", []int{0, 1, 2, 3, 8}, false},
		{"0\n", []int{0}, false},
		{"", []int{}, false},
		{"0-1,4-5", []int{0, 1, 4, 5}, false},
		{"3-1", nil, true},
		{"4,2", nil, true},
		{"0-4294967295", nil, true},
		{"a", nil, true},
		{"0,,1", nil, true},
		{"-1", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseCPUList(tt.in)
		if (err != nil) != tt.err || (!tt.err && !reflect.DeepEqual(got, tt.want)) {
			t.Errorf("ParseCPUList(%q) = %v, %v; want %v, error %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}

func TestParseChoice(t *testing.T) {
	tests := []struct {
		in, want string
		err      bool
	}{
		{"[always] madvise never\n", "always", false},
		{"always defer defer+madvise [madvise] never", "madvise", false},
		{"always within_size advise [never] deny force", "never", false},
		{"always madvise never", "", true},
		{"[always] [never]", "", true},
		{"[]", "", true},
	}
	for _, tt := range tests {
		got, err := ParseChoice(tt.in)
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("ParseChoice(%q) = %q, %v; want %q, error %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		err  bool
	}{
		{"32K", 32 << 10, false},
		{"1024K\n", 1 << 20, false},
		{"2M", 2 << 20, false},
		{"1G", 1 << 30, false},
		{"512 KB", 512 << 10, false},
		{"16MiB", 16 << 20, false},
		{"4096", 4096, false},
		{"", 0, true},
		{"K", 0, true},
		{"-1K", 0, true},
		{"1.5M", 0, true},
		{"9999999999999T", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d, error %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}
