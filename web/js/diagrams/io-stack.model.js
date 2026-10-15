// The rows of the io-stack diagram: each layer a read or write crosses
// between a system call and the device, with what it does and, where this
// machine's snapshot has it, the value that layer uses here for the disk
// holding the root filesystem. Pure; tested in web/test/diagram-io-stack.test.mjs.

import { formatBytes, formatCount } from '../lib/format.js';

/** Every id the diagram reads; all are optional. */
export const IDS = [
  'mem.meminfo.Dirty',
  'derived.dirty.kernel_threshold_bytes',
  'derived.storage.root_read_ahead_bytes',
  'derived.storage.root_fstype',
  'derived.storage.root_options',
  'derived.storage.root_max_request_bytes',
  'derived.storage.root_scheduler',
  'derived.storage.root_schedulers',
  'derived.storage.root_nr_requests',
  'derived.storage.root_hw_queues',
  'derived.storage.root_disk',
  'derived.storage.root_disk_kind',
  'derived.storage.root_write_cache',
];

const KIND = {
  nvme: 'an NVMe drive', scsi: 'a SCSI-layer disk (SATA, SAS or USB)', virtio: 'a virtio disk',
  xen: 'a Xen virtual disk', mmc: 'an SD card or eMMC', dm: 'a device-mapper volume', md: 'a software RAID volume',
  zram: 'a compressed RAM disk', loop: 'a loop device', nbd: 'a network block device', other: 'a block device',
};

export const LAYERS = [
  { layer: 'System call', title: 'read(), write(), fsync()', body: 'The program names a file and a range; the kernel finds the open file and its inode.' },
  { layer: 'Page cache', title: 'Page cache', body: 'Reads are served from cached folios or start readahead; writes dirty folios and return.' },
  { layer: 'Filesystem', title: 'Filesystem', body: 'Maps file offsets to disk blocks, allocates on writeback, journals its metadata.' },
  { layer: 'Block layer', title: 'bio and request', body: 'A bio describes one transfer; it is split to the device’s limits and merged with neighbours.' },
  { layer: 'Scheduler', title: 'I/O scheduler', body: 'May reorder and hold requests before dispatch: none, mq-deadline, bfq or kyber.' },
  { layer: 'Driver', title: 'Hardware queues', body: 'blk-mq maps each CPU to a hardware queue; the driver posts requests to the device’s submission queue.' },
  { layer: 'Device', title: 'The device', body: 'Executes requests, maybe out of order, may hold writes in a volatile cache until told to flush.' },
];

/**
 * Builds the rows from `get(id)`, which returns a record from
 * inputs.read(): {ok, value, status, ...}. Each row is
 * {layer, title, body, here}, where `here` lists sentences about this
 * machine. `missing` lists records that could not be read, `observed`
 * whether any value of this machine was drawn, `rootNote` a sentence when
 * the root disk itself is unknown.
 */
export function stackModel(get) {
  const rec = (id) => get(id) ?? { id, ok: false, status: 'missing' };
  const ok = (id) => rec(id).ok;
  const val = (id) => rec(id).value;
  const used = new Set();
  const use = (...ids) => ids.forEach((id) => used.add(id));
  const here = LAYERS.map(() => []);

  if (ok('mem.meminfo.Dirty')) {
    let d = `${formatBytes(val('mem.meminfo.Dirty'))} dirty`;
    if (ok('derived.dirty.kernel_threshold_bytes')) {
      d += `; writers are held at ${formatBytes(val('derived.dirty.kernel_threshold_bytes'))}`;
      use('derived.dirty.kernel_threshold_bytes');
    }
    here[1].push(d);
    use('mem.meminfo.Dirty');
  }
  if (ok('derived.storage.root_read_ahead_bytes')) {
    here[1].push(`Readahead up to ${formatBytes(val('derived.storage.root_read_ahead_bytes'))}`);
    use('derived.storage.root_read_ahead_bytes');
  }
  if (ok('derived.storage.root_fstype')) {
    let f = `/ is ${val('derived.storage.root_fstype')}`;
    if (ok('derived.storage.root_options')) {
      f += ` (${val('derived.storage.root_options')})`;
      use('derived.storage.root_options');
    }
    here[2].push(f);
    use('derived.storage.root_fstype');
  }
  if (ok('derived.storage.root_max_request_bytes')) {
    here[3].push(`Requests of at most ${formatBytes(val('derived.storage.root_max_request_bytes'))}`);
    use('derived.storage.root_max_request_bytes');
  }
  if (ok('derived.storage.root_scheduler')) {
    let s = `Scheduler: ${val('derived.storage.root_scheduler')}`;
    if (ok('derived.storage.root_schedulers')) {
      const others = String(val('derived.storage.root_schedulers')).split(' ').filter((x) => x && x !== val('derived.storage.root_scheduler'));
      s += others.length ? ` (also offered: ${others.join(', ')})` : ' (no other choice)';
      use('derived.storage.root_schedulers');
    }
    here[4].push(s);
    use('derived.storage.root_scheduler');
  }
  if (ok('derived.storage.root_nr_requests')) {
    here[4].push(`Up to ${formatCount(val('derived.storage.root_nr_requests'))} requests per queue`);
    use('derived.storage.root_nr_requests');
  }
  if (ok('derived.storage.root_hw_queues')) {
    const n = val('derived.storage.root_hw_queues');
    here[5].push(n === 1 ? 'One hardware queue, shared by every CPU' : `${formatCount(n)} hardware queues`);
    use('derived.storage.root_hw_queues');
  }
  if (ok('derived.storage.root_disk')) {
    let d = `Root disk: ${val('derived.storage.root_disk')}`;
    if (ok('derived.storage.root_disk_kind')) {
      d += `, ${KIND[val('derived.storage.root_disk_kind')] ?? KIND.other}`;
      use('derived.storage.root_disk_kind');
    }
    here[6].push(d);
    use('derived.storage.root_disk');
  }
  if (ok('derived.storage.root_write_cache')) {
    here[6].push(val('derived.storage.root_write_cache') === 'write back'
      ? 'Volatile write cache: fsync must send a flush'
      : 'No volatile write cache reported: a completed write is durable');
    use('derived.storage.root_write_cache');
  }

  const disk = rec('derived.storage.root_disk');
  const rootNote = disk.ok ? '' : `The disk under / is not known here${disk.note ? `: ${disk.note}` : ''}.`;
  const rows = LAYERS.map((l, i) => ({ ...l, here: here[i] }));
  // When the root disk is unknown, every per-disk value is missing for
  // the same reason; one note says so instead of eleven.
  const missing = IDS.map(rec).filter((r) => !r.ok && !(rootNote && r.id.startsWith('derived.storage.root_') && r.id !== 'derived.storage.root_fstype' && r.id !== 'derived.storage.root_options'));
  return { rows, missing, rootNote, observed: used.size > 0, used: IDS.filter((id) => used.has(id)) };
}
