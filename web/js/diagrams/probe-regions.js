// probe-regions: for each of the probe's five mappings, the size it was
// promised against what is resident in RAM. All bars share one scale, so the
// untouched region's long empty bar is drawn at its true length.

import { el, svg } from '../lib/dom.js';
import { formatBytes, formatCount } from '../lib/format.js';
import { regionRows } from './probe-regions.model.js';
import { mountFrame, svgRoot, rect, text, table, nextId } from './frame.js';
import { readAll, isTable } from './inputs.js';
import { linear, fitText, textWidth, packParts } from './scale.js';

export const title = 'Promised versus resident: the probe’s five regions';

const BACKING = { anon: 'anonymous', file: 'file', shmem: 'shared memory' };

export function mount(container, ctx) {
  const need = readAll(ctx, ['probe.regions'], { 'probe.regions': isTable });
  if (!need.ok) {
    return mountFrame(container, ctx, { labels: ['observed'], draw: null, missing: need.missing, cites: ['probe.regions'] });
  }
  const model = regionRows(need.get('probe.regions').value);
  if (!model.rows.length) {
    return mountFrame(container, ctx, {
      labels: ['observed'], draw: null, cites: ['probe.regions'],
      missing: [{ id: 'probe.regions', status: 'failed', note: 'it has no usable rows' }],
    });
  }
  const notes = [];
  if (model.rows.some((r) => r.zeroPage)) {
    notes.push('A region read but never written has its pages present in the page table yet adds nothing to resident memory: each read fault mapped the kernel’s single shared zero page, which belongs to no process.');
  }
  if (model.rows.some((r) => r.huge > 0)) {
    notes.push('Hatched: the part of the resident memory backed by transparent huge pages, where one fault mapped a whole huge page at once.');
  }
  if (model.skipped) notes.push(`${formatCount(model.skipped)} malformed rows were left out.`);

  return mountFrame(container, ctx, {
    labels: ['observed'],
    intro: 'Each bar is one mapping the probe made, as long as its size. The filled part is what was resident in RAM when the probe was measured.',
    draw: (width) => draw(width, model),
    caption: 'Bar length: the mapping’s size, on one scale for all regions. Fill: resident bytes (RSS from smaps). Pages present counts page-table entries that point at a frame, from pagemap.',
    notes,
    extra: legend(model),
    alt: table('The probe’s regions',
      [['Region'], ['Backing'], ['Size', true], ['Resident', true], ['Huge-page backed', true], ['Pages present', true], ['Swapped', true]],
      model.rows.map((r) => [r.name, BACKING[r.backing] ?? r.backing, formatBytes(r.size), formatBytes(r.rss), formatBytes(r.huge),
        `${formatCount(r.present)} of ${formatCount(r.total)}`, formatBytes(r.swap)])),
    altLabel: 'The regions as a table',
    cites: ['probe.regions'],
  });
}

function draw(width, model) {
  const barH = 18;
  const x = linear(0, model.max, 0, width - 2);
  const pattern = nextId('dg-huge');
  const nodes = [svg('defs', {}, svg('pattern', { id: pattern, width: 5, height: 5, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
    svg('line', { x1: 0, y1: 0, x2: 0, y2: 5, class: 'dg-huge-line' })))];

  let y = 0;
  for (const r of model.rows) {
    // Name, then what the probe did: on the same line when both fit.
    nodes.push(text(0, y + 13, r.name, 'dg-t dg-t--strong'));
    const after = 12 + textWidth(r.name, 12);
    if (after + textWidth(r.action, 12) <= width) {
      nodes.push(text(after, y + 13, r.action, 'dg-t dg-t--faint'));
    } else {
      y += 16;
      nodes.push(text(0, y + 13, fitText(r.action, width, 12), 'dg-t dg-t--faint'));
    }

    const by = y + 20;
    const kind = BACKING[r.backing] ? `dg-k-${r.backing}` : 'dg-k-neutral';
    nodes.push(rect(0.5, by + 0.5, x(r.size), barH - 1, 'dg-track--outline', 3));
    if (r.rss > 0) nodes.push(rect(0.5, by + 0.5, x(r.rss), barH - 1, kind, 3));
    if (r.huge > 0) nodes.push(svg('rect', { x: 0.5, y: by + 0.5, width: Math.max(0, x(Math.min(r.huge, r.rss))), height: barH - 1, rx: 3, fill: `url(#${pattern})` }));

    // A thin rule under the bar: the share of the mapping's pages that have
    // a page-table entry. It differs from the fill exactly where the zero
    // page is mapped (present, not resident).
    const py = by + barH + 3;
    nodes.push(rect(0.5, py, x(r.size), 4, 'dg-track', 2));
    if (r.present > 0 && r.total > 0) nodes.push(rect(0.5, py, x(r.size) * Math.min(1, r.present / r.total), 4, 'dg-present', 2));

    const parts = [
      `resident ${formatBytes(r.rss)} of ${formatBytes(r.size)}`,
      `${formatCount(r.present)} of ${formatCount(r.total)} pages present`,
      r.swap > 0 ? `${formatBytes(r.swap)} swapped` : null,
      r.zeroPage ? 'zero page' : null,
    ];
    let ty = py + 20;
    for (const l of packParts(parts, width, 12)) {
      nodes.push(text(0, ty, l, 'dg-t'));
      ty += 16;
    }
    y = ty + 12;
  }
  return svgRoot(width, y - 20, `Size versus resident memory for ${model.rows.length} probe regions.`, nodes);
}

function legend(model) {
  const kinds = [...new Set(model.rows.map((r) => r.backing))].filter((b) => BACKING[b]);
  return el('ul', { class: 'dg__legend', 'aria-label': 'Key' },
    el('li', {}, el('span', { class: 'dg__swatch dg__swatch--outline', 'aria-hidden': 'true' }), 'promised: the mapping’s size'),
    kinds.map((k) => el('li', {}, el('span', { class: `dg__swatch dg-k-${k}`, 'aria-hidden': 'true' }), `resident, ${BACKING[k]}`)),
    el('li', {}, el('span', { class: 'dg__swatch dg__swatch--present', 'aria-hidden': 'true' }), 'thin rule: pages present in the page table'),
    model.rows.some((r) => r.huge > 0) ? el('li', {}, el('span', { class: 'dg__swatch dg__swatch--huge', 'aria-hidden': 'true' }), 'resident in huge pages') : null);
}
