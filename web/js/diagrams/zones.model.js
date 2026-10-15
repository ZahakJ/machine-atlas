// Rows and scales for the zone-watermarks diagram. Pure; tested in
// web/test/diagram-zones.test.mjs.
//
// Watermarks are a fraction of a percent of a zone, while free memory is
// often a tenth of it and zones differ from each other by four orders of
// magnitude. Each zone is therefore drawn on its own scale, broken at
// BREAK_FACTOR × high: left of the break the watermarks are spread out
// linearly, right of it the rest of the zone up to its managed size.

import { brokenScale } from './scale.js';

export const BREAK_FACTOR = 1.6;

const COLUMNS = ['node', 'zone', 'free', 'min', 'low', 'high', 'spanned', 'present', 'managed'];

/**
 * Where free memory stands against the marks, and what that means:
 *   below-min   allocations that may sleep reclaim directly; only atomic
 *               and high-priority allocations may go lower
 *   below-low   kswapd has been woken
 *   below-high  kswapd, once awake, keeps reclaiming until free > high
 *   above-high  nothing to do
 */
export function zoneState(z) {
  if (z.free < z.min) return 'below-min';
  if (z.free < z.low) return 'below-low';
  if (z.free < z.high) return 'below-high';
  return 'above-high';
}

/**
 * Splits mem.zones rows into populated zones (managed > 0) and empty ones.
 * Rows with a missing or non-numeric column are dropped and counted in
 * `skipped`. Order: node, then the table's own zone order.
 */
export function zoneRows(table) {
  let skipped = 0;
  const populated = [];
  const empty = [];
  for (const r of table ?? []) {
    const ok = r && typeof r.zone === 'string' && COLUMNS.filter((c) => c !== 'zone').every((c) => Number.isInteger(r[c]) && r[c] >= 0);
    if (!ok) {
      skipped++;
      continue;
    }
    const z = { node: r.node, zone: r.zone, free: r.free, min: r.min, low: r.low, high: r.high, spanned: r.spanned, present: r.present, managed: r.managed };
    if (z.managed > 0) populated.push({ ...z, state: zoneState(z) });
    else empty.push(z);
  }
  return { populated, empty, skipped, nodes: [...new Set([...populated, ...empty].map((z) => z.node))] };
}

/**
 * The scale for one zone row `width` pixels wide: 0 to the zone's managed
 * pages (or free, if larger), broken after BREAK_FACTOR × high.
 */
export function zoneScale(z, width) {
  const max = Math.max(z.managed, z.free, z.high);
  return brokenScale({ max, breakAt: Math.ceil(BREAK_FACTOR * z.high), width });
}
