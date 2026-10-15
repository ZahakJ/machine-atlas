package derive

import (
	"fmt"
	"strings"
)

const (
	mountsTable = "mounts.filesystems"
	queuesTable = "block.queues"
	statsTable  = "block.stats"
)

func storageFacts(in inputs) []Fact {
	var out []Fact

	c := in.calc()
	row := rootMount(c)
	out = append(out, c.fact("derived.storage.root_fstype", "the fstype of the mount at / in mounts.filesystems", strCell(c, mountsTable, row, "fstype")))
	c = in.calc()
	row = rootMount(c)
	out = append(out, c.fact("derived.storage.root_options", "the allow-listed options of the mount at / in mounts.filesystems", strCell(c, mountsTable, row, "options")))

	c = in.calc()
	row = rootMount(c)
	opts := strCell(c, mountsTable, row, "options")
	out = append(out, c.fact("derived.storage.root_atime", "noatime or relatime when the options of / name it, otherwise strictatime (the kernel prints no word for it); here the options are "+quoteOr(opts), atimePolicy(opts)))

	c = in.calc()
	disk, how := rootDisk(c)
	out = append(out, c.fact("derived.storage.root_disk", "the disk column of the mount at / in mounts.filesystems"+strings.TrimPrefix(of(how), "of the root disk"), disk))

	c = in.calc()
	disk, how = rootDisk(c)
	kind := ""
	if !c.failed() {
		kind = diskKind(disk)
	}
	out = append(out, c.fact("derived.storage.root_disk_kind",
		"the kernel's naming scheme for "+strings.TrimPrefix(of(how), "of ")+": nvme* NVMe, sd* SCSI (SATA, SAS, USB), vd* virtio, xvd* Xen, mmcblk* MMC, dm-* device-mapper, md* software RAID, zram*, loop*, nbd*", kind))

	// Queue settings of the root disk.
	for _, f := range []struct{ id, col, what string }{
		{"derived.storage.root_scheduler", "scheduler", "selected I/O scheduler"},
		{"derived.storage.root_schedulers", "schedulers", "available I/O schedulers"},
		{"derived.storage.root_write_cache", "write_cache", "write cache mode"},
	} {
		c = in.calc()
		disk, how = rootDisk(c)
		v := strCell(c, queuesTable, diskRow(c, queuesTable, disk), f.col)
		out = append(out, c.fact(f.id, "the "+f.what+" in block.queues "+of(how), v))
	}
	c = in.calc()
	disk, how = rootDisk(c)
	r := diskRow(c, queuesTable, disk)
	rot, ok := r["rotational"].(bool)
	if r != nil && !ok {
		c.fail("%s has no rotational flag for the root disk", queuesTable)
	}
	out = append(out, c.fact("derived.storage.root_rotational", "the rotational flag in block.queues "+of(how), rot))
	for _, f := range []struct{ id, col, what string }{
		{"derived.storage.root_hw_queues", "hw_queues", "hardware queue count"},
		{"derived.storage.root_nr_requests", "nr_requests", "nr_requests"},
		{"derived.storage.root_read_ahead_bytes", "read_ahead_bytes", "readahead window"},
		{"derived.storage.root_max_request_bytes", "max_request_bytes", "largest request size"},
	} {
		c = in.calc()
		disk, how = rootDisk(c)
		row := diskRow(c, queuesTable, disk)
		var v int64
		if _, has := row[f.col]; row != nil && !has && f.col == "hw_queues" {
			c.fail("%s has no hardware queues: it is bio-based (device-mapper, md) and passes each request to the disks beneath it", disk)
		} else {
			v = c.cell(queuesTable, row, f.col)
		}
		out = append(out, c.fact(f.id, "the "+f.what+" in block.queues "+of(how), v))
	}

	// I/O counters of the root disk.
	for _, f := range []struct{ id, col, what string }{
		{"derived.storage.root_read_bytes", "read_bytes", "sectors read × 512"},
		{"derived.storage.root_written_bytes", "write_bytes", "sectors written × 512"},
	} {
		c = in.calc()
		disk, how = rootDisk(c)
		v := c.cell(statsTable, diskRow(c, statsTable, disk), f.col)
		out = append(out, c.fact(f.id, f.what+" in block.stats "+of(how), v))
	}
	for _, f := range []struct{ id, ms, n, what string }{
		{"derived.storage.root_read_latency_seconds", "read_ms", "reads", "read"},
		{"derived.storage.root_write_latency_seconds", "write_ms", "writes", "write"},
	} {
		c = in.calc()
		disk, how = rootDisk(c)
		row := diskRow(c, statsTable, disk)
		ms, n := c.cell(statsTable, row, f.ms), c.cell(statsTable, row, f.n)
		if !c.failed() && n == 0 {
			c.fail("the root disk has completed no %ss since it appeared", f.what)
		}
		var v any
		if !c.failed() {
			v = float64(ms) / float64(n) / 1000
		}
		out = append(out, c.fact(f.id, f.ms+" / "+f.n+" in block.stats "+of(how)+", in seconds", v))
	}
	c = in.calc()
	disk, how = rootDisk(c)
	ioMS := c.cell(statsTable, diskRow(c, statsTable, disk), "io_ms")
	up := c.float("kernel.uptime_seconds")
	if !c.failed() && up <= 0 {
		c.fail("kernel.uptime_seconds is %g", up)
	}
	var util any
	if !c.failed() {
		util = float64(ioMS) / (up * 1000)
	}
	out = append(out, c.fact("derived.storage.root_util_fraction", "io_ms in block.stats "+of(how)+" / (kernel.uptime_seconds × 1000)", util))

	c = in.calc()
	disk, how = rootDisk(c)
	row = diskRow(c, statsTable, disk)
	weighted, busy := c.cell(statsTable, row, "weighted_ms"), c.cell(statsTable, row, "io_ms")
	if !c.failed() && busy == 0 {
		c.fail("the root disk has never been busy (io_ms is 0)")
	}
	var depth any
	if !c.failed() {
		depth = float64(weighted) / float64(busy)
	}
	out = append(out, c.fact("derived.storage.root_busy_queue_depth", "weighted_ms / io_ms in block.stats "+of(how), depth))

	return append(out, dirtyKernelFacts(in)...)
}

// rootMount returns the row of mounts.filesystems mounted at /, failing c
// when there is none. With several (a mount stacked over /), the last one
// listed is the one visible.
func rootMount(c *calc) map[string]any {
	t := c.table(mountsTable)
	if c.failed() {
		return nil
	}
	var root map[string]any
	for _, r := range t {
		if r["mount_point"] == "/" {
			root = r
		}
	}
	if root == nil {
		c.fail("%s has no mount at /", mountsTable)
	}
	return root
}

// of phrases "of the root disk", adding how it was found when it was.
func of(how string) string {
	if how == "" {
		return "of the root disk"
	}
	return "of the root disk (" + how + ")"
}

// rootDisk returns the disk holding the root filesystem and a phrase
// saying how it was found ("" when it was not), failing c when / is not on
// a block device or its disk is unknown.
func rootDisk(c *calc) (string, string) {
	row := rootMount(c)
	if row == nil {
		return "", ""
	}
	kind, _ := row["source_kind"].(string)
	fstype, _ := row["fstype"].(string)
	if kind != "block-device" {
		c.fail("/ is %s, whose source kind is %s rather than a block device: its data lives in directories of another filesystem, in memory or on a server", fstype, kind)
		return "", ""
	}
	disk, _ := row["disk"].(string)
	if disk == "" {
		c.fail("the device of / could not be traced to a disk in /sys/block")
		return "", ""
	}
	dev, _ := row["device"].(string)
	if dev == "" || dev == disk {
		return disk, fmt.Sprintf("%s holds / directly", disk)
	}
	return disk, fmt.Sprintf("%s, which holds / through %s", disk, dev)
}

// diskRow returns the row of a block table for disk, or nil once c failed.
func diskRow(c *calc, table, disk string) map[string]any {
	if c.failed() {
		c.look(table)
		return nil
	}
	return c.row(table, "name", disk)
}

// strCell returns a string cell of row, which came from table id.
func strCell(c *calc, id string, row map[string]any, col string) string {
	if row == nil {
		return ""
	}
	s, ok := row[col].(string)
	if !ok || s == "" {
		c.fail("%s: column %s is missing", id, col)
	}
	return s
}

// atimePolicy names the access-time policy a mount's options give.
func atimePolicy(opts string) string {
	for _, o := range strings.Split(opts, ",") {
		if o == "noatime" || o == "relatime" {
			return o
		}
	}
	return "strictatime"
}

func quoteOr(s string) string {
	if s == "" {
		return "unknown"
	}
	return `"` + s + `"`
}

// diskKind names the kind of block device from the kernel's naming scheme.
func diskKind(name string) string {
	for _, p := range []struct{ prefix, kind string }{
		{"nvme", "nvme"}, {"mmcblk", "mmc"}, {"xvd", "xen"}, {"vd", "virtio"}, {"sd", "scsi"},
		{"dm-", "dm"}, {"md", "md"}, {"zram", "zram"}, {"loop", "loop"}, {"nbd", "nbd"},
	} {
		if strings.HasPrefix(name, p.prefix) {
			return p.kind
		}
	}
	return "other"
}

// dirtyKernelFacts turn the thresholds the kernel itself computed, and
// published in /proc/vmstat, into bytes and a fill fraction.
func dirtyKernelFacts(in inputs) []Fact {
	var out []Fact
	for _, f := range []struct{ id, key string }{
		{"derived.dirty.kernel_threshold_bytes", "nr_dirty_threshold"},
		{"derived.dirty.kernel_background_threshold_bytes", "nr_dirty_background_threshold"},
	} {
		c := in.calc()
		pages, ps := c.int("mem.vmstat."+f.key), c.int("kernel.page_size_bytes")
		out = append(out, c.fact(f.id, f.key+" (pages) × kernel.page_size_bytes", pages*ps))
	}
	c := in.calc()
	dirty, wb, limit := c.int("mem.vmstat.nr_dirty"), c.int("mem.vmstat.nr_writeback"), c.int("mem.vmstat.nr_dirty_threshold")
	if !c.failed() && limit <= 0 {
		c.fail("nr_dirty_threshold is %d", limit)
	}
	var frac any
	if !c.failed() {
		frac = float64(dirty+wb) / float64(limit)
	}
	out = append(out, c.fact("derived.dirty.threshold_used_fraction", "(nr_dirty + nr_writeback) / nr_dirty_threshold, all from /proc/vmstat", frac))
	return out
}
