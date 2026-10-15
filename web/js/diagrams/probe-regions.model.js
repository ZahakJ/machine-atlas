// Rows for the probe-regions diagram: per region, what was promised (the
// mapping's size) against what is resident. Pure; tested in
// web/test/diagram-probe-regions.test.mjs.

/** The probe's regions in the order the lessons introduce them. */
export const REGION_ORDER = ['anon-untouched', 'anon-written', 'anon-read', 'file-private', 'shmem-shared'];

/** What the probe did to each region (ARCHITECTURE.md §4), shown beside its bar. */
export const REGION_ACTION = {
  'anon-untouched': 'mapped, never touched',
  'anon-written': 'wrote one byte per page',
  'anon-read': 'read one byte per page',
  'file-private': 'read every page of its own executable',
  'shmem-shared': 'memfd, wrote the first quarter',
};

const NUMERIC = ['size_bytes', 'rss_bytes', 'anon_huge_bytes', 'swap_bytes', 'pages_total', 'pages_present', 'pages_swapped'];

/**
 * Turns probe.regions rows into drawable rows, in REGION_ORDER (unknown
 * names after, alphabetically). A row whose sizes are not non-negative
 * integers is dropped and counted in `skipped` rather than drawn wrong.
 *
 * returns {rows: [{name, backing, action, size, rss, huge, swap, total,
 *                  present, swapped, zeroPage}], max, skipped}
 * zeroPage is true for an anonymous region whose pages are present in the
 * page table yet count nothing towards RSS: read faults on anonymous memory
 * map the kernel's shared zero page, which belongs to no process.
 */
export function regionRows(table) {
  let skipped = 0;
  const rows = [];
  for (const r of table ?? []) {
    if (!r || typeof r.name !== 'string' || !NUMERIC.every((k) => Number.isInteger(r[k]) && r[k] >= 0)) {
      skipped++;
      continue;
    }
    rows.push({
      name: r.name,
      backing: String(r.backing ?? ''),
      action: REGION_ACTION[r.name] ?? '',
      size: r.size_bytes,
      rss: r.rss_bytes,
      huge: r.anon_huge_bytes,
      swap: r.swap_bytes,
      total: r.pages_total,
      present: r.pages_present,
      swapped: r.pages_swapped,
      zeroPage: r.backing === 'anon' && r.pages_present > 0 && r.rss_bytes === 0,
    });
  }
  const rank = (n) => {
    const i = REGION_ORDER.indexOf(n);
    return i < 0 ? REGION_ORDER.length : i;
  };
  rows.sort((a, b) => rank(a.name) - rank(b.name) || a.name.localeCompare(b.name));
  const max = rows.reduce((m, r) => Math.max(m, r.size, r.rss), 0);
  return { rows, max, skipped };
}
