// Segments for the meminfo-breakdown diagram. Pure; tested in
// web/test/diagram-meminfo.test.mjs.
//
// The bar must divide MemTotal exactly the way derived.mem.accounted_bytes
// does, so the consumers are taken from that fact's own inputs. The list
// below is the fallback for a snapshot without the fact and mirrors
// accountedFields in internal/derive/mem.go.

export const ACCOUNTED_FALLBACK = [
  'MemFree', 'Buffers', 'Cached', 'AnonPages', 'Slab', 'KernelStack', 'PageTables', 'Percpu', 'VmallocUsed',
].map((f) => `mem.meminfo.${f}`);

/** What each consumer is, in a few words, for the legend and the table. */
export const FIELD_MEANING = {
  MemFree: 'not in use at all',
  Buffers: 'block-device metadata cache',
  Cached: 'page cache, including shared memory',
  AnonPages: 'process memory with no file behind it',
  Slab: 'kernel object caches',
  KernelStack: 'one stack per thread, in the kernel',
  PageTables: 'the page tables themselves',
  Percpu: 'per-CPU kernel data',
  VmallocUsed: 'kernel virtually contiguous allocations',
};

export const field = (id) => id.replace(/^mem\.meminfo\./, '');

/**
 * The observation ids of the consumers, from the accounted fact's inputs
 * when it lists meminfo ids, else the fallback list.
 */
export function consumerIds(accountedFact) {
  const inputs = accountedFact?.inputs;
  if (Array.isArray(inputs)) {
    const ids = inputs.filter((id) => typeof id === 'string' && id.startsWith('mem.meminfo.') && id !== 'mem.meminfo.MemTotal');
    if (ids.length) return ids;
  }
  return ACCOUNTED_FALLBACK;
}

/**
 * Divides `total` into the consumer `values` (an array of [id, bytes]).
 * returns {
 *   segments: [{id, name, bytes, start, fraction}]   start/fraction of scale
 *   unaccounted: total − sum (may be negative: the fields overlap)
 *   scale: the byte count the full bar width stands for, max(total, sum)
 *   overflow: true when the consumers sum to more than total
 * }
 */
export function breakdown(total, values) {
  let sum = 0;
  for (const [, v] of values) sum += v;
  const scale = Math.max(total, sum);
  let at = 0;
  const segments = values.map(([id, bytes]) => {
    const s = { id, name: field(id), bytes, start: scale ? at / scale : 0, fraction: scale ? bytes / scale : 0 };
    at += bytes;
    return s;
  });
  const unaccounted = total - sum;
  return { segments, unaccounted, sum, scale, overflow: unaccounted < 0, remainderStart: scale ? sum / scale : 0, remainderFraction: scale && unaccounted > 0 ? unaccounted / scale : 0 };
}
