package memory

import "testing"

// The fuzz targets check that no input makes a parser panic, and that what
// a parser accepts is internally consistent.

func FuzzParseMeminfo(f *testing.F) {
	f.Add(readTestdata(f, "meminfo-6.8.txt"))
	f.Add(readTestdata(f, "meminfo-3.10.txt"))
	f.Add([]byte("MemTotal: 1 kB\nHugePages_Total: 3\n"))
	f.Add([]byte("MemTotal: 9223372036854775807 kB\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fields, err := parseMeminfo(data)
		if err != nil {
			return
		}
		for _, fl := range fields {
			if fl.Value < 0 {
				t.Errorf("negative value accepted: %+v", fl)
			}
		}
	})
}

func FuzzParseZoneinfo(f *testing.F) {
	f.Add(readTestdata(f, "zoneinfo-numa.txt"))
	f.Add(readTestdata(f, "zoneinfo-4.19.txt"))
	f.Add([]byte("Node 0, zone DMA\n  pages free 1\n min 1\n low 1\n high 1\n spanned 1\n present 1\n managed 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		zones, err := parseZoneinfo(data)
		if err != nil {
			return
		}
		if len(zones) == 0 {
			t.Error("no zones and no error")
		}
		for _, z := range zones {
			if z.Free < 0 || z.Min < 0 || z.Managed < 0 {
				t.Errorf("negative count accepted: %+v", z)
			}
		}
	})
}
