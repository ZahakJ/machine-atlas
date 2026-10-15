package system

import (
	"io/fs"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// identifying matches values that must never appear in a committed
// fixture: home directories, IPv4 and MAC addresses, and UUIDs. Addresses
// the fixture machines are given on purpose are let through by invented.
var identifying = map[string]*regexp.Regexp{
	"home directory": regexp.MustCompile(`/home/[A-Za-z0-9._-]+`),
	"IPv4 address":   regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`),
	"MAC address":    regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`),
	"UUID":           regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
}

// TestFixturesHoldNothingIdentifying walks every fixture root and fails on
// anything that looks like it came from a real machine, including this
// one's hostname and the current user's name.
func TestFixturesHoldNothingIdentifying(t *testing.T) {
	local := map[string]string{}
	if h, err := os.Hostname(); err == nil && len(h) >= 4 {
		local["this machine's hostname"] = h
	}
	if u, err := user.Current(); err == nil && len(u.Username) >= 4 && u.Username != "root" {
		local["the current user name"] = u.Username
	}
	var files int
	err := filepath.WalkDir(rootsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		text := string(data)
		for what, re := range identifying {
			for _, m := range re.FindAllString(text, -1) {
				if !invented(what, m) {
					t.Errorf("%s: contains a %s: %q", path, what, m)
					break
				}
			}
		}
		lower := strings.ToLower(text)
		for what, s := range local {
			if strings.Contains(lower, strings.ToLower(s)) {
				t.Errorf("%s: contains %s", path, what)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no fixture files found")
	}
}

// inventedRanges are the IPv4 ranges fixture machines may use: the
// documentation ranges (RFC 5737), the private ones (RFC 1918), loopback
// and the unspecified address. None identifies a real machine.
var inventedRanges = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("0.0.0.0/32"),
}

// invented reports whether a match is an address fixtures may hold: an
// IPv4 address in inventedRanges, or a MAC that is all zeros (loopback) or
// locally administered (second-lowest bit of the first byte set), which no
// manufacturer assigns.
func invented(what, m string) bool {
	switch what {
	case "IPv4 address":
		a, err := netip.ParseAddr(m)
		for _, p := range inventedRanges {
			if err == nil && p.Contains(a) {
				return true
			}
		}
	case "MAC address":
		if m == "00:00:00:00:00:00" {
			return true
		}
		var first uint8
		for _, c := range strings.ToLower(m[:2]) {
			first <<= 4
			if c >= 'a' {
				first |= uint8(c-'a') + 10
			} else {
				first |= uint8(c - '0')
			}
		}
		return first&0x02 != 0
	}
	return false
}

func TestInvented(t *testing.T) {
	for _, c := range []struct {
		what, m string
		ok      bool
	}{
		{"IPv4 address", "192.168.1.23", true}, {"IPv4 address", "203.0.113.9", true},
		{"IPv4 address", "8.8.8.8", false}, {"IPv4 address", "100.64.0.1", false},
		{"MAC address", "02:42:ac:11:00:02", true}, {"MAC address", "00:00:00:00:00:00", true},
		{"MAC address", "52:54:00:12:34:56", true}, {"MAC address", "a8:00:00:00:00:01", false},
		{"UUID", "3f2a9c1e-0b7d-4e8a-9c31-5d2e7f6a8b90", false},
	} {
		if got := invented(c.what, c.m); got != c.ok {
			t.Errorf("invented(%s, %s) = %v", c.what, c.m, got)
		}
	}
}

func TestIdentifyingPatterns(t *testing.T) {
	for what, s := range map[string]string{
		"home directory": "path=/home/alice/x", "IPv4 address": "addr 192.168.1.20 up",
		"MAC address": "ether 52:54:00:12:34:56", "UUID": "luks-3f2a9c1e-0b7d-4e8a-9c31-5d2e7f6a8b90",
	} {
		if !identifying[what].MatchString(s) {
			t.Errorf("%s pattern misses %q", what, s)
		}
	}
	for _, s := range []string{"6.12.10-arch1-1", "gcc 13.2.0", "Debian 1:6.6.51-1+rpt3", "0x7f3c8e400000"} {
		for what, re := range identifying {
			if re.MatchString(s) {
				t.Errorf("%s pattern matches harmless %q", what, s)
			}
		}
	}
}
