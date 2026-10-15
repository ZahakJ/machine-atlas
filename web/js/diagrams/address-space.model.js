// Layout of the address-space map: the probe's mappings placed on the whole
// user range, with every gap compressed to a fixed height. Pure (no DOM);
// tested in web/test/diagram-address-space.test.mjs.
//
// A user address space is 128 TiB or more and the probe uses a few hundred
// MiB of it, so nothing can be drawn to scale: a 4 KiB mapping would be a
// billionth of a pixel. The map is therefore an ordered strip, top = high
// addresses, where each mapping's height grows with the logarithm of its
// size and each gap between mappings is a fixed-height, visibly broken row
// labelled with its true size.

import { parseAddr } from './scale.js';

/** The display kinds, in legend order. */
export const KINDS = ['executable', 'file', 'shmem', 'heap', 'anon', 'stack', 'kernel', 'reserved'];

export const KIND_NAMES = {
  executable: 'executable code',
  file: 'file',
  shmem: 'shared memory',
  heap: 'heap',
  anon: 'anonymous',
  stack: 'stack',
  kernel: 'kernel-provided (vdso, vvar)',
  reserved: 'reserved, no access',
};

/**
 * Refines the collector's kind (anon, file, shmem, heap, stack, kernel) for
 * drawing: executable file mappings are told apart from data, memfd and
 * /dev/shm mappings count as shared memory even when the collector saw a
 * file name, and a mapping with no permissions at all (---p) is a
 * reservation or guard area rather than usable memory.
 */
export function displayKind({ kind, perms = '', path = '' }) {
  if (perms.startsWith('---')) return 'reserved';
  if (kind === 'shmem' || /^\/?memfd:/.test(path) || path.startsWith('/dev/shm/')) return 'shmem';
  if (kind === 'file') return perms[2] === 'x' ? 'executable' : 'file';
  if (KINDS.includes(kind)) return kind;
  return 'anon';
}

/**
 * Builds the rows of the map, top (highest address) first.
 *
 * input: {
 *   maps:     probe.maps.sample rows ({start, end, perms, offset, kind, path})
 *   regions:  probe.regions rows, or null when unavailable
 *   userEnd:  BigInt end of user space (exclusive), or null when unknown
 *   vmaCount: probe.maps.vma_count, or null
 *   highestEnd: BigInt probe.maps.highest_end (end of the highest mapping),
 *            or null when unknown
 * }
 * When the sample holds fewer mappings than the VMA count, the missing ones
 * are the highest (the probe keeps the first lines of its maps file, which
 * the kernel lists in address order). The space above the last sampled
 * mapping, up to highestEnd (or the end of user space when that is
 * unknown), is then one 'unsampled' row, never an unmapped gap: the stack,
 * vvar and vdso are usually up there.
 * returns {
 *   rows:    [{type:'map', start, end, size, kind, perms, path, offset}
 *            | {type:'gap', start, end, size, edge:'top'|'bottom'|null}
 *            | {type:'unsampled', start, end, size, count}]
 *   above:   mappings at or above userEnd (the legacy vsyscall page sits in
 *            kernel space), listed rather than drawn
 *   regions: [{name, start, end, size, rows:[row indexes]}] for regions that
 *            overlap a drawn mapping; unplaced: names of regions that do not
 *   skipped: count of malformed rows dropped
 *   shown, total: rows drawn vs the VMA count (total null when unknown)
 * }
 * All addresses and sizes are BigInts.
 */
export function buildRows({ maps, regions = null, userEnd = null, vmaCount = null, highestEnd = null }) {
  let skipped = 0;
  const parsed = [];
  for (const m of maps ?? []) {
    const start = parseAddr(m?.start);
    const end = parseAddr(m?.end);
    if (start === null || end === null || end <= start) {
      skipped++;
      continue;
    }
    parsed.push({ type: 'map', start, end, size: end - start, kind: displayKind(m), perms: String(m.perms ?? ''), path: String(m.path ?? ''), offset: String(m.offset ?? '') });
  }
  parsed.sort((a, b) => (a.start < b.start ? -1 : a.start > b.start ? 1 : 0));

  const above = userEnd === null ? [] : parsed.filter((m) => m.end > userEnd);
  const inside = userEnd === null ? parsed : parsed.filter((m) => m.end <= userEnd);

  // Bottom-up with gaps, then reversed so the top of the strip comes first.
  const up = [];
  let cursor = 0n;
  for (const m of inside) {
    if (m.start < cursor) {
      // Overlapping rows cannot come from one maps file; keep the first.
      skipped++;
      continue;
    }
    if (m.start > cursor) up.push({ type: 'gap', start: cursor, end: m.start, size: m.start - cursor, edge: cursor === 0n ? 'bottom' : null });
    up.push(m);
    cursor = m.end;
  }
  const total = Number.isInteger(vmaCount) ? vmaCount : null;
  const shown = inside.length + above.length;
  const missing = total !== null && total > shown ? total - shown : 0;
  if (missing > 0) {
    // The unsampled mappings lie between the last sampled one and the
    // highest mapped address; above that, user space is really unmapped.
    let top = highestEnd !== null && highestEnd > cursor ? highestEnd : userEnd;
    if (userEnd !== null && top !== null && top > userEnd) top = userEnd;
    if (top !== null && top > cursor) {
      up.push({ type: 'unsampled', start: cursor, end: top, size: top - cursor, count: missing });
      cursor = top;
    } else {
      up.push({ type: 'unsampled', start: cursor, end: cursor, size: 0n, count: missing });
    }
  }
  if (userEnd !== null && userEnd > cursor) up.push({ type: 'gap', start: cursor, end: userEnd, size: userEnd - cursor, edge: 'top' });
  const rows = up.reverse();

  const placed = [];
  const unplaced = [];
  for (const r of regions ?? []) {
    const start = parseAddr(r?.start);
    const end = parseAddr(r?.end);
    if (start === null || end === null || end <= start) {
      if (r?.name) unplaced.push(String(r.name));
      continue;
    }
    const idx = [];
    rows.forEach((row, i) => {
      if (row.type === 'map' && row.start < end && row.end > start) idx.push(i);
    });
    if (idx.length) placed.push({ name: String(r.name ?? ''), start, end, size: end - start, rows: idx });
    else unplaced.push(String(r.name ?? ''));
  }

  return { rows, above, regions: placed, unplaced, skipped, shown, total };
}

/** True when the drawn rows are a sample of a larger address space. */
export function isSample(model) {
  return model.total !== null && model.total > model.shown;
}

/**
 * Assigns each row a vertical position. Mappings get
 * minH + perDouble × log2(size / pageSize) pixels, capped at maxH; gaps get
 * gapH, and the band of unsampled mappings a little more. Returns {rows: [{...row, y, h}], height}.
 */
export function layoutRows(rows, { pageSize = 4096, minH = 30, perDouble = 2.4, maxH = 64, gapH = 26, spacer = 2 } = {}) {
  let y = 0;
  const out = rows.map((row) => {
    let h = row.type === 'unsampled' ? Math.round(gapH * 1.6) : gapH;
    if (row.type === 'map') {
      const pages = Math.max(1, Number(row.size) / pageSize);
      h = Math.min(maxH, Math.round(minH + perDouble * Math.log2(pages)));
    }
    const placed = { ...row, y, h };
    y += h + spacer;
    return placed;
  });
  return { rows: out, height: Math.max(0, y - spacer) };
}
