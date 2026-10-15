// zone-watermarks: for every populated zone, free memory against the min,
// low and high watermarks, each zone on its own scale broken after the
// watermarks (zones.model.js explains why).

import { el, svg } from '../lib/dom.js';
import { formatBytes, formatCount } from '../lib/format.js';
import { zoneRows, zoneScale, BREAK_FACTOR } from './zones.model.js';
import { mountFrame, svgRoot, rect, text, line, table, round } from './frame.js';
import { readAll, read, isTable, isNumber, whyMissing } from './inputs.js';
import { fitText, textWidth, clamp, wrap } from './scale.js';

export const title = 'Free memory against the zone watermarks';

const STATE_WORDS = {
  'above-high': 'above high, so no reclaim is needed',
  'below-high': 'between low and high; kswapd, if already awake, keeps reclaiming until high',
  'below-low': 'below low, so kswapd has been woken to reclaim in the background',
  'below-min': 'below min, so allocating tasks reclaim directly and stall',
};

const MARKS = [
  ['min', 'Below min:', 'an allocation that can wait must reclaim memory itself (direct reclaim) and stalls; only atomic and high-priority allocations may use the pages under min.'],
  ['low', 'Below low:', 'the allocator wakes kswapd, the per-node reclaim thread, to free pages in the background.'],
  ['high', 'Up to high:', 'once awake, kswapd keeps reclaiming until free is back above high, then sleeps.'],
];

export function mount(container, ctx) {
  const need = readAll(ctx, ['mem.zones'], { 'mem.zones': isTable });
  const cites = ['mem.zones', 'kernel.page_size_bytes'];
  if (!need.ok) return mountFrame(container, ctx, { labels: ['observed'], draw: null, missing: need.missing, cites });

  const model = zoneRows(need.get('mem.zones').value);
  if (!model.populated.length) {
    return mountFrame(container, ctx, {
      labels: ['observed'], draw: null, cites,
      missing: [{ id: 'mem.zones', status: 'failed', note: 'it lists no zone with managed pages' }],
    });
  }
  const ps = read(ctx, 'kernel.page_size_bytes');
  const pageSize = ps.ok && isNumber(ps.value) && ps.value > 0 ? ps.value : null;
  const amount = (pages) => (pageSize ? formatBytes(pages * pageSize) : `${formatCount(pages)} pages`);

  const notes = [];
  if (!pageSize) notes.push(`Sizes are in pages because ${whyMissing(ps)}.`);
  if (model.empty.length) {
    notes.push(`Not drawn, because they manage no memory here: ${model.empty.map((z) => `node ${z.node} ${z.zone}`).join(', ')}. A zone exists for every type the kernel was built with, whether or not this machine has memory in its address range.`);
  }
  if (model.skipped) notes.push(`${formatCount(model.skipped)} malformed rows were left out.`);

  return mountFrame(container, ctx, {
    labels: ['observed'],
    intro: 'The kernel keeps a reserve of free pages in every zone. Three thresholds per zone decide when it starts reclaiming and who does the work.',
    draw: (width) => draw(width, model, amount),
    caption: `Each zone is drawn on its own scale, because zones differ by orders of magnitude. The scale breaks (the slanted cut) at ${BREAK_FACTOR} × high: left of the break the watermarks are spread out to be readable, right of it the rest of the zone is compressed up to its managed size.`,
    notes,
    extra: marksList(),
    alt: table('Zones, in pages', [['Zone'], ['Managed', true], ['Free', true], ['min', true], ['low', true], ['high', true], ['State']],
      model.populated.map((z) => [`node ${z.node} ${z.zone}`, amount(z.managed), amount(z.free), formatCount(z.min), formatCount(z.low), formatCount(z.high), STATE_WORDS[z.state]])),
    altLabel: 'The zones as a table',
    cites,
  });
}

function draw(width, model, amount) {
  const trackY = 46;
  const trackH = 18;
  const multiNode = model.nodes.length > 1;
  const nodes = [];

  let y = 0;
  for (const z of model.populated) {
    const sc = zoneScale(z, width - 1);
    const x = (v) => sc.x(v) + 0.5;
    const name = multiNode ? `Node ${z.node} · ${z.zone}` : z.zone;
    nodes.push(text(0, y + 14, name, 'dg-t dg-t--title'));
    nodes.push(text(width, y + 14, fitText(`managed ${amount(z.managed)}`, width / 2, 12), 'dg-t', 'end'));

    const ty = y + trackY;
    nodes.push(rect(0.5, ty, width - 1, trackH, 'dg-track--outline', 3));
    if (z.free > 0) nodes.push(rect(0.5, ty, Math.max(2, x(z.free) - 0.5), trackH, 'dg-free', 3));

    // The break: a surface-coloured band with slanted edges over the track.
    if (sc.broken) {
      const a = round(sc.breakX0 + 0.5);
      const b = round(sc.breakX1 + 0.5);
      nodes.push(svg('path', { d: `M${a - 3} ${ty - 4}L${a + 3} ${ty + trackH + 4}L${b + 3} ${ty + trackH + 4}L${b - 3} ${ty - 4}Z`, class: 'dg-break' }));
    }

    // Watermarks: lines across the track, labels above it. min is labelled
    // to the left of its line and high to the right, low one line higher, so
    // the three never collide however close they are.
    for (const [mark] of MARKS) {
      const mx = x(z[mark]);
      const { lx, anchor, dy } = markLabel(mark, mx, width);
      nodes.push(line(mx, ty - 6 + dy, mx, ty + trackH + 3, `dg-mark dg-mark--${mark}`));
      nodes.push(text(lx, ty - 8 + dy, mark, 'dg-t dg-t--small dg-t--ink', anchor));
    }

    let by = ty + trackH + 16;
    for (const l of wrap(`free ${amount(z.free)}: ${STATE_WORDS[z.state]}`, width, 12)) {
      nodes.push(text(0, by, l, z.state === 'above-high' ? 'dg-t dg-t--ink' : 'dg-t dg-t--warn'));
      by += 16;
    }
    y = by + 20;
  }
  return svgRoot(width, y - 32, `Free pages against min, low and high watermarks for ${model.populated.length} zones.`, nodes);
}

// Watermark labels sit above the track: min to the left of its line, high
// to the right and low one line higher, so the three never collide however
// close they are; a label that would leave the drawing flips side.
function markLabel(mark, mx, width) {
  const w = textWidth(mark, 11) + 3;
  if (mark === 'low') return { lx: clamp(mx, w / 2, width - w / 2), anchor: 'middle', dy: -12 };
  if (mark === 'min') return mx - w >= 0 ? { lx: mx - 3, anchor: 'end', dy: 0 } : { lx: mx + 3, anchor: 'start', dy: 0 };
  return mx + w <= width ? { lx: mx + 3, anchor: 'start', dy: 0 } : { lx: mx - 3, anchor: 'end', dy: 0 };
}

function marksList() {
  return el('ul', { class: 'dg__marks', 'aria-label': 'What each watermark triggers' },
    MARKS.map(([mark, lead, words]) => el('li', {},
      svg('svg', { class: 'dg__mark-key', width: 14, height: 14, viewBox: '0 0 14 14', 'aria-hidden': 'true' },
        svg('line', { x1: 7, y1: 0, x2: 7, y2: 14, class: `dg-mark dg-mark--${mark}` })),
      el('strong', {}, lead), ' ', words)),
    el('li', {}, el('span', { class: 'dg__swatch dg-k-free', 'aria-hidden': 'true' }), el('strong', {}, 'Free:'), ' ', 'pages on the zone’s free lists when the snapshot was taken.'));
}
