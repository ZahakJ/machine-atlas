// meminfo-breakdown: MemTotal as one bar divided into the consumers that
// derived.mem.accounted_bytes adds up, with the remainder drawn hatched as a
// discrepancy; then MemFree against MemAvailable on the same scale.

import { el, svg } from '../lib/dom.js';
import { formatBytes } from '../lib/format.js';
import { breakdown, consumerIds, FIELD_MEANING } from './meminfo.model.js';
import { mountFrame, missingNotice, svgRoot, rect, text, line, nextId } from './frame.js';
import { read, readAll, isNumber } from './inputs.js';
import { linear, textWidth, wrap } from './scale.js';

export const title = 'Where MemTotal goes';

const CLASS = {
  MemFree: 'dg-k-neutral',
  Cached: 'dg-k-file',
  Buffers: 'dg-k-heap',
  AnonPages: 'dg-k-anon',
  Slab: 'dg-k-kernel',
  KernelStack: 'dg-k-stack',
  PageTables: 'dg-k-executable',
  Percpu: 'dg-k-shmem',
  VmallocUsed: 'dg-k-other',
};
const TOTAL = 'mem.meminfo.MemTotal';
const FREE = 'mem.meminfo.MemFree';
const AVAIL = 'mem.meminfo.MemAvailable';

const pct = (v, total) => `${((v / total) * 100).toFixed(v / total < 0.01 ? 2 : 1)}%`;

export function mount(container, ctx) {
  const accountedFact = read(ctx, 'derived.mem.accounted_bytes');
  const ids = consumerIds(accountedFact);
  const checks = Object.fromEntries([TOTAL, FREE, AVAIL, ...ids].map((id) => [id, isNumber]));
  const split = readAll(ctx, [TOTAL, ...ids], checks);
  const avail = readAll(ctx, [TOTAL, FREE, AVAIL], checks);

  if (!split.ok && !avail.ok) {
    const missing = [...split.missing, ...avail.missing.filter((m) => !split.missing.some((s) => s.id === m.id))];
    return mountFrame(container, ctx, { labels: ['observed', 'derived'], draw: null, missing, cites: [TOTAL, ...ids, AVAIL, 'derived.mem.unaccounted_bytes'] });
  }

  const total = (split.ok ? split : avail).get(TOTAL).value;
  const model = split.ok ? breakdown(total, ids.map((id) => [id, split.get(id).value])) : null;
  const pattern = nextId('dg-unacc');
  const notes = [
    'The fields overlap by design, so only these are added up: Cached already contains shared memory (Shmem) and tmpfs files, Active and Inactive divide the same pages a second way, and MemAvailable is an estimate built from MemFree and the reclaimable part of Cached and Slab.',
  ];
  if (model?.overflow) notes.push(`Here the fields add up to more than MemTotal, by ${formatBytes(-model.unaccounted)}: the bar is drawn to their sum, and the line marks MemTotal.`);

  return mountFrame(container, ctx, {
    labels: ['observed', 'derived'],
    intro: 'The kernel reports dozens of memory counters. Nine of them, added together, account for almost all of MemTotal; the rest is memory the counters do not attribute.',
    draw: (width) => draw(width, total, model, avail.ok ? { free: avail.get(FREE).value, avail: avail.get(AVAIL).value } : null, pattern),
    caption: 'Both bars are drawn on one scale, MemTotal wide. The segments are the fields in the rule of derived.mem.accounted_bytes; the hatched end is derived.mem.unaccounted_bytes, what is left over.',
    notes,
    extra: [
      model ? breakdownTable(model, total) : missingNotice(split.missing, 'The split of MemTotal is not drawn:'),
      avail.ok ? null : missingNotice(avail.missing, 'MemFree against MemAvailable is not drawn:'),
    ],
    cites: [TOTAL, ...ids, AVAIL, 'derived.mem.accounted_bytes', 'derived.mem.unaccounted_bytes'],
  });
}

function draw(width, total, model, avail, pattern) {
  const nodes = [svg('defs', {}, svg('pattern', { id: pattern, width: 6, height: 6, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
    svg('line', { x1: 0, y1: 0, x2: 0, y2: 6, class: 'dg-hatch-line' })))];
  let y = 0;
  const barH = 30;

  if (model) {
    const x = linear(0, model.scale, 0, width);
    nodes.push(text(0, y + 13, 'MemTotal, divided', 'dg-t dg-t--title'));
    nodes.push(text(width, y + 13, formatBytes(total), 'dg-t dg-t--ink', 'end'));
    y += 22;
    // Segments with a 2 px surface gap between neighbours.
    for (const s of model.segments) {
      const x0 = x(s.start * model.scale);
      const w = x(s.fraction * model.scale) - 2;
      if (w > 0.4) nodes.push(rect(x0, y, w, barH, CLASS[s.name] ?? 'dg-k-neutral'));
    }
    if (model.unaccounted > 0) {
      const x0 = x(model.sum);
      nodes.push(rect(x0, y, x(total) - x0, barH, 'dg-hatch-box'));
      nodes.push(svg('rect', { x: x0, y, width: Math.max(0, x(total) - x0), height: barH, fill: `url(#${pattern})` }));
    }
    if (model.overflow) nodes.push(line(x(total), y - 6, x(total), y + barH + 6, 'dg-axis'));

    // Direct labels under the segments wide enough to carry one; the table
    // below the drawing names every segment.
    const ly = y + barH + 16;
    let free = 0;
    for (const s of model.segments) {
      const x0 = x(s.start * model.scale);
      const w = x(s.fraction * model.scale);
      const words = `${s.name} ${formatBytes(s.bytes)}`;
      if (x0 >= free && textWidth(words, 12) + 8 <= w) {
        nodes.push(line(x0 + 0.5, y + barH, x0 + 0.5, ly - 10, 'dg-leader'));
        nodes.push(text(x0 + 4, ly, words, 'dg-t dg-t--ink'));
        free = x0 + textWidth(words, 12) + 12;
      }
    }
    if (model.unaccounted > 0) {
      const words = `unaccounted ${formatBytes(model.unaccounted)}`;
      const tw = textWidth(words, 12);
      const xr = x(total);
      const tx = Math.max(xr - tw, free);
      if (tx + tw <= width + 1) {
        nodes.push(line(xr - 0.5, y + barH, xr - 0.5, ly + 6, 'dg-leader'));
        nodes.push(text(xr - 4, ly + 16, words, 'dg-t dg-t--warn', 'end'));
      }
    }
    y = ly + 36;
  }

  if (avail) {
    const x = linear(0, total, 0, width);
    nodes.push(text(0, y + 13, 'Free is not the same as available', 'dg-t dg-t--title'));
    y += 24;
    const rows = [
      ['MemFree', avail.free, 'dg-k-neutral', 'not used for anything'],
      ['MemAvailable', avail.avail, 'dg-free', 'could be handed out without swapping'],
    ];
    for (const [name, v, cls, gloss] of rows) {
      nodes.push(rect(0.5, y + 0.5, width - 1, 18, 'dg-track--outline', 3));
      nodes.push(rect(0.5, y + 0.5, x(Math.min(v, total)), 18, cls, 3));
      let ty = y + 34;
      for (const l of wrap(`${name} ${formatBytes(v)} (${pct(v, total)} of MemTotal): ${gloss}`, width, 12)) {
        nodes.push(text(0, ty, l, 'dg-t dg-t--ink'));
        ty += 16;
      }
      y = ty + 14;
    }
    y -= 8;
  }
  return svgRoot(width, y, model ? `MemTotal of ${formatBytes(total)} divided into ${model.segments.length} meminfo fields and an unaccounted remainder.` : `MemFree and MemAvailable against MemTotal of ${formatBytes(total)}.`, nodes);
}

function breakdownTable(model, total) {
  const sorted = [...model.segments].sort((a, b) => b.bytes - a.bytes);
  // The meaning sits under the field name rather than in its own column,
  // so the table fits a phone without hiding the numbers.
  const row = (swatch, name, what, bytes) => el('tr', {},
    el('td', {}, el('span', { class: 'dg__key' }, swatch, name), el('span', { class: 'dg__what' }, what)),
    el('td', { class: 'num' }, formatBytes(bytes)),
    el('td', { class: 'num' }, pct(bytes, total)));
  return el('div', { class: 'table-wrap' },
    el('table', { class: 'datatable' },
      el('caption', {}, `MemTotal ${formatBytes(total)}, by field (largest first); every size is observed except the last row`),
      el('thead', {}, el('tr', {},
        el('th', { scope: 'col' }, 'Field'),
        el('th', { scope: 'col', class: 'num' }, 'Size'), el('th', { scope: 'col', class: 'num' }, 'Share'))),
      el('tbody', {},
        sorted.map((s) => row(el('span', { class: `dg__swatch ${CLASS[s.name] ?? 'dg-k-neutral'}`, 'aria-hidden': 'true' }), s.name, FIELD_MEANING[s.name] ?? '', s.bytes)),
        row(el('span', { class: 'dg__swatch dg__swatch--hatch', 'aria-hidden': 'true' }), model.unaccounted < 0 ? 'Overlap' : 'Unaccounted',
          model.unaccounted < 0
            ? 'derived: the fields add up to more than MemTotal'
            : 'derived, a discrepancy and not a category: hugetlb pool, driver and network buffers, per-CPU page lists and other memory no field names',
          model.unaccounted))));
}
