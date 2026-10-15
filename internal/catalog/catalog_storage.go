package catalog

import "github.com/ZahakJ/machine-atlas/internal/obs"

// Storage observations: each block device's request queue, its I/O
// counters and its writeback settings (the backing device, BDI), I/O
// pressure, and the filesystem types this kernel knows. Block device names
// are identifying under strict redaction and numbered in one namespace
// shared with block.devices, block.lsblk, mounts.filesystems and
// mem.swap.areas, so <dev-2> is the same device in every table.

// devColumn is the namespace strict redaction numbers block device names in.
const devColumn = "dev"

const blockCaveat = "Read from /sys/block, which lists the host's devices even inside a container: a container can see the queue of a disk its own files live on, but not which of its files are on it. Unused loop and ram devices are skipped, as in block.devices."

// storagePSI declares one /proc/pressure/io figure.
func storagePSI(line, field string) Entry {
	e := Entry{
		ID: "block.psi." + line + "." + field, Kind: obs.KindLive, Scope: obs.ScopeKernel, Privileged: privSame,
		Caveat: "Pressure stall information measures time lost, not which device or process caused it; it is absent when the kernel is older than 4.20, was built without CONFIG_PSI or was booted with psi=0. A task counts as stalled on I/O while it waits for a block read or write, including waiting for writeback throttling and for pages to be read in; time waiting for the network, a pipe or a lock is not counted.",
	}
	who := "at least one task was waiting for I/O"
	if line == "full" {
		who = "every non-idle task was waiting for I/O at once, so the CPUs did no useful work for them"
	}
	if field == "total_us" {
		e.Title = "I/O pressure (" + line + "), total stall time"
		e.Description = "Microseconds since boot during which " + who + ". Two readings give the stall share over any interval."
		e.Unit, e.Type = obs.UnitMicroseconds, obs.TypeInt
	} else {
		window := map[string]string{"avg10": "10 seconds", "avg60": "60 seconds", "avg300": "300 seconds"}[field]
		e.Title = "I/O pressure (" + line + "), " + window + " average"
		e.Description = "Percentage of wall time over the last " + window + " (exponentially weighted) during which " + who + "."
		e.Unit, e.Type = obs.UnitPercent, obs.TypeFloat
	}
	return e
}

var storageEntries = []Entry{
	{
		ID: "block.queues", Title: "Block request queues",
		Description: "The request queue of every block device in /sys/block: the I/O scheduler selected and those available, the logical and physical block size, how many requests the queue holds (nr_requests), the largest request the kernel builds and the device accepts, the number of hardware queues blk-mq maps CPUs onto, whether the device has a volatile write cache and honours FUA, the largest discard it accepts, the readahead window and the writeback-throttling latency target.",
		Type:        obs.TypeTable,
		Columns: []string{"name", "scheduler", "schedulers", "rotational", "logical_block_bytes", "physical_block_bytes",
			"nr_requests", "max_request_bytes", "max_hw_request_bytes", "hw_queues", "write_cache", "fua",
			"discard_max_bytes", "read_ahead_bytes", "wbt_latency_us"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		IdentifyingColumns: []string{"name"}, SharedIdentifiers: map[string]string{"name": devColumn},
		Caveat:     "What the driver and kernel report, not measured behaviour: a disk may claim 512-byte physical sectors while it writes 4 KiB internally, a virtual disk reports whatever the hypervisor says, and write_cache says whether the kernel sends cache flushes, not whether the hardware really loses data on power loss. Attributes a device or kernel lacks (hw_queues on a bio-based device-mapper volume, fua before Linux 4.18, wbt without CONFIG_BLK_WBT) are left out of its row. " + blockCaveat,
		Privileged: "Nothing more to read; root could change the scheduler, nr_requests, read_ahead_kb and wbt_lat_usec, which Machine Atlas never does.",
	},
	{
		ID: "block.stats", Title: "Block I/O statistics",
		Description: "Each block device's I/O counters since it appeared (normally boot), from /sys/block/<dev>/stat: reads and writes completed and merged, bytes moved, milliseconds spent on them (summed over all requests, so overlapping requests count several times), requests in flight now, milliseconds during which at least one request was in flight (io_ms, the basis of %util) and the weighted time (each millisecond multiplied by the requests in flight). Discard counters exist since Linux 4.18, flush counters since 5.5.",
		Type:        obs.TypeTable,
		Columns: []string{"name", "reads", "reads_merged", "read_bytes", "read_ms", "writes", "writes_merged", "write_bytes",
			"write_ms", "in_flight", "io_ms", "weighted_ms", "discards", "discards_merged", "discard_bytes", "discard_ms",
			"flushes", "flush_ms"},
		Kind: obs.KindLive, Scope: obs.ScopeKernel,
		IdentifyingColumns: []string{"name"}, SharedIdentifiers: map[string]string{"name": devColumn},
		Caveat:     "Counters since the device appeared, not rates: two readings and a subtraction give a rate. The times are sums over requests, so read_ms divided by reads is the average time a read spent from submission to completion, including time queued in the kernel; io_ms counts a millisecond once however many requests overlapped in it, which is why 100% busy says little about a device that serves many requests in parallel. A device-mapper or md volume counts its own requests and its disks count theirs again. " + blockCaveat,
		Privileged: privSame,
	},
	{
		ID: "block.bdi", Title: "Writeback settings per device",
		Description: "The backing device info (BDI) of every block device, through /sys/block/<dev>/bdi: the share of the global dirty limit this device may use at least (min_ratio) and at most (max_ratio, in percent; max_bytes on Linux 6.2 and later), whether that limit is enforced even below the global background threshold (strict_limit, Linux 6.2 and later, used for FUSE), and whether pages must stay unchanged while they are written (stable_pages_required, for devices that checksum data in flight).",
		Type:        obs.TypeTable,
		Columns:     []string{"name", "min_ratio", "max_ratio", "max_bytes", "strict_limit", "stable_pages_required"},
		Kind:        obs.KindStatic, Scope: obs.ScopeKernel,
		IdentifyingColumns: []string{"name"}, SharedIdentifiers: map[string]string{"name": devColumn},
		Caveat:     "Settings, not usage: how much dirty data each device holds now is not exported unprivileged (it is in debugfs). Network and FUSE filesystems have BDIs of their own under /sys/class/bdi that are not block devices and are not listed. " + blockCaveat,
		Privileged: "Root could read each device's current dirty and writeback figures in /sys/kernel/debug/bdi/<dev>/stats, and change these ratios; Machine Atlas does neither.",
	},
	{
		ID: "fs.types", Title: "Filesystem types",
		Description: "Every filesystem type this kernel can mount now, from /proc/filesystems, and whether it needs a block device (nodev types such as tmpfs, proc and overlay do not).",
		Type:        obs.TypeTable, Columns: []string{"name", "nodev"},
		Kind: obs.KindStatic, Scope: obs.ScopeKernel,
		Caveat:     "Lists types built in or provided by a module already loaded; a type whose module would be loaded on the first mount (btrfs, xfs on many distributions) is missing until then.",
		Privileged: privSame,
	},
	storagePSI("some", "avg10"), storagePSI("some", "avg60"), storagePSI("some", "avg300"), storagePSI("some", "total_us"),
	storagePSI("full", "avg10"), storagePSI("full", "avg60"), storagePSI("full", "avg300"), storagePSI("full", "total_us"),
}

const rootDiskCaveat = "Found through the filesystem mounted at / in atlas's mount namespace: unknown when that is overlay, tmpfs or a network filesystem (a container's root), or when the mount's device could not be traced to a disk in /sys/block. A root on device-mapper or md is that volume, not the disks beneath it."

// storageDerivedEntries are the derived facts about storage, computed in
// internal/derive/storage.go. "Root disk" is the whole block device that
// holds the filesystem mounted at /.
var storageDerivedEntries = []Entry{
	derived("storage.root_fstype", obs.TypeString, "", Arithmetic, "Root filesystem type",
		"The filesystem type of the mount at /, as atlas's mount namespace sees it.",
		"In a container this is the container's root (usually overlay), not the host's."),
	derived("storage.root_options", obs.TypeString, "", Arithmetic, "Root mount options",
		"The mount options of / that Machine Atlas keeps (an allow-list: ro or rw, the atime policy, sync, discard, commit=, data=, compress=, barrier settings, ssd), in the order the kernel printed them.",
		"Only options the kernel prints: a filesystem's defaults (ext4's data=ordered, for example) are often left out."),
	derived("storage.root_atime", obs.TypeString, "", Inference, "Root access-time policy",
		"When reads of files under / update their access time: noatime (never), relatime (only when the access time is older than the last change, or a day old) or strictatime (on every read), from the options the kernel printed for the mount.",
		"The kernel prints relatime or noatime when set and nothing for strictatime, so a missing word means strictatime. lazytime, which keeps timestamp updates in memory, is a separate option, and a filesystem can ignore access times on its own."),
	derived("storage.root_disk", obs.TypeString, "", Inference, "Root disk",
		"The block device in /sys/block that holds the root filesystem: the mount's device, or the disk its partition belongs to.",
		rootDiskCaveat+" Under strict redaction this is the device's placeholder."),
	derived("storage.root_disk_kind", obs.TypeString, "", Inference, "Root disk kind",
		"The kind of device holding the root filesystem, from the kernel's naming scheme: nvme (NVMe), scsi (sd*: SATA, SAS and USB disks all use the SCSI layer), virtio (vd*), xen (xvd*), mmc (SD cards and eMMC), dm (device-mapper: LVM, encryption), md (software RAID), zram, loop, nbd or other.",
		rootDiskCaveat+" Inferred from the name alone: a SATA SSD and a USB stick are both scsi."),
	derived("storage.root_scheduler", obs.TypeString, "", Arithmetic, "Root disk I/O scheduler",
		"The I/O scheduler selected for the root disk's request queue: none, mq-deadline, bfq or kyber.",
		rootDiskCaveat),
	derived("storage.root_schedulers", obs.TypeString, "", Arithmetic, "Root disk schedulers available",
		"Every I/O scheduler the kernel offers for the root disk's queue, the selected one included.",
		"Schedulers built as modules appear only once loaded. "+rootDiskCaveat),
	derived("storage.root_rotational", obs.TypeBool, "", Arithmetic, "Root disk rotates",
		"Whether the driver reports the root disk as rotational (a spinning disk), which changes readahead, scheduling and filesystem choices.",
		"Virtual disks often claim to rotate whatever is behind them. "+rootDiskCaveat),
	derived("storage.root_hw_queues", obs.TypeInt, obs.UnitCount, Arithmetic, "Root disk hardware queues",
		"How many hardware dispatch queues blk-mq has for the root disk: one per submission queue the driver created, typically one per CPU on NVMe and one on SATA.",
		"Absent on a bio-based volume (device-mapper linear, md), which has no request queue of its own. "+rootDiskCaveat),
	derived("storage.root_nr_requests", obs.TypeInt, obs.UnitCount, Arithmetic, "Root disk queue depth (nr_requests)",
		"How many requests the root disk's queue may hold per hardware queue before submitters wait: the depth the scheduler sorts within.",
		"A software limit; the device's own queue depth (NCQ's 32 on SATA, up to thousands on NVMe) is separate and not exported uniformly. "+rootDiskCaveat),
	derived("storage.root_read_ahead_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Root disk readahead window",
		"The largest readahead window for files on the root disk (read_ahead_kb).",
		"A ceiling: the window starts smaller and grows while reads stay sequential; filesystems such as btrfs can widen it per file. "+rootDiskCaveat),
	derived("storage.root_max_request_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Root disk largest request",
		"The largest request the block layer builds for the root disk (max_sectors_kb); larger I/O is split.",
		rootDiskCaveat),
	derived("storage.root_write_cache", obs.TypeString, "", Arithmetic, "Root disk write cache",
		"\"write back\" when the root disk reports a volatile write cache, so the kernel must send cache flushes for durability, \"write through\" when it does not.",
		"What the device reports; a drive with power-loss protection may report write back and lose nothing, and a misreporting one can lose data the kernel believed durable. "+rootDiskCaveat),
	derived("storage.root_read_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Read from the root disk since boot",
		"Bytes read from the root disk since it appeared: sectors read × 512.",
		"Counts every read the device served, including readahead that was never used and reads by other filesystems on the same disk. "+rootDiskCaveat),
	derived("storage.root_written_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Written to the root disk since boot",
		"Bytes written to the root disk since it appeared: sectors written × 512.",
		"Includes journal and metadata writes, swap on that disk and every partition's traffic; the flash inside an SSD may write more (write amplification), which no unprivileged interface reports. "+rootDiskCaveat),
	derived("storage.root_read_latency_seconds", obs.TypeFloat, obs.UnitSeconds, Arithmetic, "Average read time on the root disk",
		"read_ms / reads completed since boot: the average time a read took from entering the block layer to completion.",
		"An average over the whole uptime, dominated by whatever the disk did most; it includes time waiting in the scheduler queue, and one slow burst disappears in it. "+rootDiskCaveat),
	derived("storage.root_write_latency_seconds", obs.TypeFloat, obs.UnitSeconds, Arithmetic, "Average write time on the root disk",
		"write_ms / writes completed since boot: the average time a write took from entering the block layer to completion.",
		"Writes are mostly background writeback that nobody waited for, so this is not the latency programs felt. "+rootDiskCaveat),
	derived("storage.root_util_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Root disk busy fraction since boot",
		"io_ms / uptime: the share of time since boot during which at least one request was in flight on the root disk, what iostat reports as %util.",
		"Busy is not saturated: a device that serves many requests in parallel (any SSD, NVMe above all) can be busy 100% of the time and still have most of its capacity unused. The device's counters start when it appeared, normally at boot. "+rootDiskCaveat),
	derived("storage.root_busy_queue_depth", obs.TypeFloat, obs.UnitCount, Arithmetic, "Root disk requests in flight while busy",
		"weighted_ms / io_ms: by Little's law, the average number of requests in flight on the root disk during the time it was busy.",
		"An average since boot; above 1 the device was working on several requests at once, which %util cannot show. "+rootDiskCaveat),
	derived("dirty.kernel_threshold_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Dirty threshold (kernel)",
		"The global dirty threshold the kernel computed when /proc/vmstat was read (nr_dirty_threshold × page size): the amount of dirty and writeback data above which writers are made to wait.",
		"Recomputed by the kernel at every read from the memory it finds dirtyable then; per-device and per-cgroup limits below it, and the throttling that starts between the background threshold and this one, are not shown."),
	derived("dirty.kernel_background_threshold_bytes", obs.TypeInt, obs.UnitBytes, Arithmetic, "Background writeback threshold (kernel)",
		"The global threshold above which flusher threads write back dirty data without waiting for it to expire (nr_dirty_background_threshold × page size), as the kernel computed it when /proc/vmstat was read.",
		"A global figure; each device's share of it depends on how fast that device has been writing."),
	derived("dirty.threshold_used_fraction", obs.TypeFloat, obs.UnitFraction, Arithmetic, "Dirty data against the threshold",
		"(nr_dirty + nr_writeback) / nr_dirty_threshold: how close dirty and in-flight data are to the point where writers are throttled to a stop.",
		"One instant; dirty data rises and falls within seconds. Throttling of individual writers starts earlier, halfway between the background threshold and this one, and earlier still for a device whose share is small."),
}
