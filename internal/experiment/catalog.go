package experiment

// The authored descriptions; each must match what the code does.

const (
	idFaultCount = "fault-count"
	idResidency  = "page-cache-residency"
)

const mib = 1 << 20

// experiment is an authored description; info adds this build's
// availability.
type experiment struct{ Info }

func (e experiment) info() Info {
	in := e.Info
	in.Steps = append([]string(nil), e.Steps...)
	in.DoesNot = append([]string(nil), e.DoesNot...)
	in.Outcomes = append([]Outcome(nil), e.Outcomes...)
	in.Available, in.UnavailableReason = available()
	return in
}

var experiments = []experiment{
	{Info{
		ID:       idFaultCount,
		Title:    "Counting page faults",
		Question: "When a program touches memory it has just mapped, how many page faults does the kernel take, and do transparent huge pages change the count?",
		Steps: []string{
			"Map two private anonymous regions of 32 MiB each on 2 MiB boundaries: region A with the default huge page policy, region B after madvise(MADV_NOHUGEPAGE).",
			"For region A: read this thread's minor and major fault counters (getrusage with RUSAGE_THREAD), write one byte to every page, and read the counters again.",
			"Read the region's Rss and AnonHugePages from /proc/self/smaps.",
			"Write every page a second time and count the faults again.",
			"Release the region's pages with madvise(MADV_DONTNEED), read smaps again, then write every page once more and count.",
			"Repeat the last four steps for region B; also record the base page size and the transparent huge page mode from /sys/kernel/mm/transparent_hugepage/enabled.",
			"Unmap both regions, exit, and pick the outcome by a fixed rule from the counts.",
		},
		Resources: Resources{MemoryBytes: 64 * mib, DiskBytes: 0, TimeBoundSeconds: 5},
		DoesNot: []string{
			"It writes no files and changes no system setting.",
			"It does not read or touch any other process's memory.",
			"It does not need root and does not lock memory; on a machine short of memory the kernel reclaims other pages to make room, as it would for any program allocating 64 MiB.",
			"It does not store its results: they are not part of any snapshot.",
		},
		Outcomes: []Outcome{
			{"small-pages", "Both regions faulted about once per page",
				"Every page was a separate first-touch fault in both regions, so region A was not given transparent huge pages. That is expected when the THP mode is \"madvise\" or \"never\", or when the kernel found no free 2 MiB block at the moment of each fault. The second write took almost no faults because the pages were already mapped; after MADV_DONTNEED the region was empty again and every page faulted anew."},
			{"huge-pages", "Region A was backed by huge pages",
				"Region A took far fewer faults than it has pages and smaps shows nearly all of it as AnonHugePages: a fault anywhere in an aligned 2 MiB range allocated and mapped a whole huge page at once. Region B, which opted out with MADV_NOHUGEPAGE, still faulted once per base page. The same writes cost the kernel a few dozen faults instead of thousands, which is why THP exists; the price is more zeroing per fault and memory committed before it is used."},
			{"partial-huge", "Region A was partly backed by huge pages",
				"Region A shows some AnonHugePages, but not across the whole region with few faults. Either the kernel could not find a free 2 MiB block for every range (physical memory is fragmented; the THP defrag setting decides whether a fault waits for compaction), or khugepaged collapsed small pages into huge ones after they were faulted. Region B faulted once per page, as its MADV_NOHUGEPAGE advice requires."},
			{"unexpected", "None of the expected patterns",
				"The counts fit none of the three patterns: for example region B faulted far less than once per page, or region A took fewer faults than pages without any AnonHugePages. Likely causes are multi-size THP giving region A folios smaller than 2 MiB (smaps does not count those as AnonHugePages), madvise being refused (a seccomp filter in a container), a memory cgroup limit forcing reclaim during the run, or an unusual base page size. The counts are still accurate measurements of this run."},
		},
	}},
	{Info{
		ID:       idResidency,
		Title:    "Watching the page cache",
		Question: "After a file is written, is its data kept in memory, can a program ask the kernel to drop it, and how does a read from storage compare with a read from the page cache?",
		Steps: []string{
			"Create the cache directory ($XDG_CACHE_HOME/machine-atlas, or ~/.cache/machine-atlas) with mode 0700 if needed, delete files left by an earlier run that crashed, and refuse if its filesystem has less than 256 MiB free.",
			"Write a 16 MiB file of non-zero pseudo-random bytes from a fixed seed and fsync it, so that its cached pages are clean.",
			"Map the file read-only only to ask mincore which of its pages are in the page cache (nothing is read through the mapping), and record the resident fraction.",
			"Call posix_fadvise(POSIX_FADV_DONTNEED) on the whole file and measure residency again.",
			"Read the whole file with read(2), timing it (the cold read), and measure residency.",
			"Read the whole file again, timing it (the warm read).",
			"Delete the file, record the filesystem type from statfs, and pick the outcome by a fixed rule.",
		},
		Resources: Resources{MemoryBytes: 18 * mib, DiskBytes: 16 * mib, TimeBoundSeconds: 20},
		DoesNot: []string{
			"It writes nothing outside its cache directory and deletes its file before it exits, also when a step fails.",
			"It does not drop any other file's cached pages, does not use /proc/sys/vm/drop_caches, and does not need root.",
			"It does not tell which storage device holds the data: mincore reports page cache residency only.",
			"It does not benchmark the storage: one 16 MiB read measures this device at this moment.",
			"It does not store its results: they are not part of any snapshot.",
		},
		Outcomes: []Outcome{
			{"evicted-then-cached", "Dropped by the advice, cached again by the read",
				"Right after the write the file was in the page cache; after POSIX_FADV_DONTNEED almost none of it was, and reading it brought all of it back. The kernel dropped the clean pages because nothing was using them, so the cold read fetched the data from storage and the warm read copied it from memory. On a fast NVMe drive the two times can be close, because 16 MiB takes milliseconds either way; the residency figures, not the times, are the evidence."},
			{"not-evictable", "Not evictable: the file lives in memory",
				"The file is on tmpfs or ramfs, where the page cache is the only copy of the data, so the kernel cannot drop it on advice: there would be nowhere to read it back from. Under memory pressure tmpfs pages can be swapped out, but never simply discarded. Both reads were copies from memory and take about the same time."},
			{"partially-evicted", "Partly dropped",
				"Only part of the file left the page cache after the advice, and the read brought it fully back. POSIX_FADV_DONTNEED is advice: the kernel skips pages that are dirty, under writeback, mapped by a process or otherwise in use at that moment, and some filesystems handle the request their own way."},
			{"unexpected", "None of the expected patterns",
				"The residency figures fit none of the three patterns: for example nothing was dropped on a disk filesystem, or the file was not fully resident after being read. Likely causes are a network or FUSE filesystem that manages its own cache, a stacked filesystem (overlay, encryption) whose pages belong to the layer below, direct-access (DAX) storage that bypasses the page cache, or memory pressure evicting pages during the run. The individual measurements are still accurate for this run."},
		},
	}},
}
