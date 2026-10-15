package probe

import "testing"

func FuzzParseMaps(f *testing.F) {
	f.Add([]byte(sampleMaps))
	f.Add([]byte("00400000-004ab000 r-xp 00000000 00:00 0\n"))
	f.Add([]byte("7f3a14400000-7f3a14800000 rw-s 00000000 00:01 2048   /memfd:x (deleted)\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		vmas, err := parseMaps(data)
		if err != nil {
			return
		}
		for i, v := range vmas {
			if v.End <= v.Start || (i > 0 && v.Start < vmas[i-1].End) {
				t.Errorf("accepted inconsistent mapping %+v", v)
			}
		}
	})
}

func FuzzParseSmaps(f *testing.F) {
	f.Add([]byte(sampleSmaps()))
	f.Add([]byte("7f3a10000000-7f3a10800000 rw-p 00000000 00:00 0\nRss: 4 kB\nVmFlags: rd\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		vmas, err := parseSmaps(data)
		if err != nil {
			return
		}
		for _, v := range vmas {
			for k, n := range v.Fields {
				if n < 0 {
					t.Errorf("negative %s accepted", k)
				}
			}
		}
	})
}
