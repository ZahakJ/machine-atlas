// lru-lists: the classic active and inactive lists, one pair for anonymous
// pages and one for file pages, with the moves between them, eviction and
// refault. Authored (GENERAL). Below it, what this machine's kernel uses
// instead, read from mem.lru_gen.enabled through derived.reclaim.lru_kind.

import { el, svg } from '../lib/dom.js';
import { badge } from '../lib/ui.js';
import { mountFrame, svgRoot, nextId, rect, text } from './frame.js';
import { box, arrow, arrowDefs, edgeLabel } from './flow.js';
import { measureBox } from './scale.js';
import { read, whyMissing } from './inputs.js';

export const title = 'Active and inactive lists';

const LANES = [
  {
    kind: 'anon',
    name: 'Anonymous pages',
    sink: { title: 'Swap', body: 'Written to a swap area, then the frame is freed. Without swap, anonymous pages cannot be evicted at all.' },
  },
  {
    kind: 'file',
    name: 'File pages',
    sink: { title: 'Storage', body: 'Clean pages are simply dropped; dirty pages are written back to their file first.' },
  },
];

const INACTIVE = {
  anon: { title: 'Inactive list', body: 'New anonymous pages start here (since Linux 5.9; before, on the active list). Reclaim takes pages from the tail.' },
  file: { title: 'Inactive list', body: 'New file pages start here, so a file read once never displaces the working set. Reclaim takes pages from the tail.' },
};
const ACTIVE = { title: 'Active list', body: 'Pages used more than once: the working set, protected from eviction.' };
const WIDE = 640;

export function mount(container, ctx) {
  return mountFrame(container, ctx, {
    labels: ['general'],
    intro: 'To pick which pages to evict, the kernel keeps every page that could be reclaimed on a list ordered roughly by last use, separately for anonymous and file pages.',
    draw,
    caption: 'Promote: a page on the inactive list referenced again moves to the active list. Demote: when the active list grows too large relative to the inactive one, pages from its tail move back to the inactive list, keeping their second chance. Evict: reclaim frees pages from the inactive tail. Refault: an evicted page needed again soon is read back and, if it was evicted too early, placed straight on the active list.',
    extra: lruPanel(ctx),
    alt: el('ul', { class: 'dg__steps' },
      LANES.map((l) => el('li', {}, el('strong', {}, l.name), ': inactive list → (referenced again) active list → (aged) back to inactive → (evicted from the tail) ', l.sink.title.toLowerCase(), '. ', l.sink.body, ' A refault reads the page back, onto the active list when it was evicted too early.'))),
    altLabel: 'The lists as text',
    cites: ['mem.lru_gen.enabled', 'derived.reclaim.lru_kind'],
  });
}

function draw(width) {
  const { defs, ids } = arrowDefs(nextId('dg-lru'));
  const nodes = [defs];
  const wide = width >= WIDE;
  let y = 0;
  for (const lane of LANES) {
    nodes.push(text(0, y + 14, lane.name, 'dg-t dg-t--title'));
    y += 26;
    y = wide ? wideLane(nodes, ids, lane, y, width) : narrowLane(nodes, ids, lane, y, width);
    y += 30;
  }
  return svgRoot(width, y - 30, 'Active and inactive LRU lists for anonymous and file pages, with promotion, demotion, eviction and refault.', nodes);
}

// A list box: title and body, then a row of small pages, head on the left.
// `textH`, when given, pads the text area to that height so boxes side by
// side end level.
function listBox(nodes, x, y, w, spec, kind, cls, textH = 0) {
  const own = measureBox(spec.title, spec.body, w).height;
  const b = box(x, y, w, { ...spec, cls, extraH: 20 + Math.max(0, textH - own) });
  nodes.push(b.node);
  const pages = Math.max(4, Math.min(12, Math.floor((w - 80) / 16)));
  const py = y + b.h - 24;
  nodes.push(text(x + 10, py + 10, 'head', 'dg-t dg-t--small dg-t--faint'));
  for (let i = 0; i < pages; i++) nodes.push(rect(x + 42 + i * 16, py, 12, 12, `dg-k-${kind} dg-page`, 2));
  nodes.push(text(x + 46 + pages * 16, py + 10, 'tail', 'dg-t dg-t--small dg-t--faint'));
  return b.h;
}

function wideLane(nodes, ids, lane, y, width) {
  const gap = 110;
  const w = (width - gap) / 2;
  const textH = Math.max(measureBox(INACTIVE[lane.kind].title, INACTIVE[lane.kind].body, w).height, measureBox(ACTIVE.title, ACTIVE.body, w).height);
  const listsH = listBox(nodes, 0, y, w, INACTIVE[lane.kind], lane.kind, 'dg-box dg-box--list', textH);
  listBox(nodes, w + gap, y, w, ACTIVE, lane.kind, 'dg-box dg-box--active', textH);

  const ax0 = w;
  const ax1 = w + gap;
  nodes.push(arrow([[ax0, y + 22], [ax1 - 1, y + 22]], ids, 'accent'));
  nodes.push(edgeLabel((ax0 + ax1) / 2, y + 16, 'promote', 'middle'));
  nodes.push(arrow([[ax1, y + listsH - 26], [ax0 + 1, y + listsH - 26]], ids));
  nodes.push(edgeLabel((ax0 + ax1) / 2, y + listsH - 32, 'demote', 'middle'));

  const sy = y + listsH + 44;
  const sink = box(0, sy, w, { ...lane.sink, cls: 'dg-box dg-box--sink' });
  nodes.push(sink.node);
  nodes.push(arrow([[w - 40, y + listsH], [w - 40, sy - 1]], ids, 'bad'));
  nodes.push(edgeLabel(w - 34, y + listsH + 26, 'evict from the tail'));

  // Refault: from the sink back up to the active list.
  const ry = sy + sink.h / 2;
  const rx = w + gap + w / 2;
  nodes.push(arrow([[w, ry], [rx, ry], [rx, y + listsH + 1]], ids, 'refault'));
  nodes.push(edgeLabel(w + 10, ry - 6, 'refault: needed again soon'));
  return sy + sink.h;
}

function narrowLane(nodes, ids, lane, y, width) {
  const x = 28; // a rail on the left carries the refault arrow
  const w = width - x;
  const ah = listBox(nodes, x, y, w, ACTIVE, lane.kind, 'dg-box dg-box--active');
  const iy = y + ah + 50;
  const ih = listBox(nodes, x, iy, w, INACTIVE[lane.kind], lane.kind, 'dg-box dg-box--list');

  const px = x + w * 0.3;
  const dx = x + w * 0.7;
  nodes.push(arrow([[px, iy], [px, y + ah + 1]], ids, 'accent'));
  nodes.push(edgeLabel(px + 6, y + ah + 30, 'promote'));
  nodes.push(arrow([[dx, y + ah], [dx, iy - 1]], ids));
  nodes.push(edgeLabel(dx + 6, y + ah + 30, 'demote'));

  const sy = iy + ih + 50;
  const sink = box(x, sy, w, { ...lane.sink, cls: 'dg-box dg-box--sink' });
  nodes.push(sink.node);
  nodes.push(arrow([[dx, iy + ih], [dx, sy - 1]], ids, 'bad'));
  nodes.push(edgeLabel(dx - 6, iy + ih + 30, 'evict', 'end'));

  const ry = sy + sink.h / 2;
  nodes.push(arrow([[x, ry], [10, ry], [10, y + ah / 2], [x - 1, y + ah / 2]], ids, 'refault'));
  nodes.push(edgeLabel(12, iy + ih + 30, 'refault'));
  return sy + sink.h;
}

// What this machine's kernel uses: observed value, derived kind, and the
// generations picture when it is MGLRU.
function lruPanel(ctx) {
  const kind = read(ctx, 'derived.reclaim.lru_kind');
  const raw = read(ctx, 'mem.lru_gen.enabled');
  let words;
  if (kind.ok && kind.value === 'mglru') {
    words = 'This kernel has Multi-Gen LRU enabled, so it does not use the two lists drawn above. Each type keeps up to four generations instead: new and recently used pages join the youngest, aging scans page tables to move referenced pages forward, and eviction takes from the oldest. The moves are the same ideas with finer steps.';
  } else if (kind.ok && kind.value === 'classic') {
    words = 'This kernel uses the two lists drawn above: Multi-Gen LRU is not enabled here.';
  } else {
    words = `Machine Atlas could not tell which LRU this kernel uses: ${whyMissing(kind)}.`;
  }
  const value = raw.ok ? `mem.lru_gen.enabled reads ${raw.value}` : whyMissing(raw);
  return el('div', { class: 'dg__panel' },
    el('div', { class: 'dg__panel-head' }, el('span', {}, 'On this machine'), el('span', { class: 'dg__badges' }, raw.ok ? badge('observed') : null, kind.ok ? badge('derived') : null)),
    el('p', {}, words),
    kind.ok && kind.value === 'mglru' ? generations() : null,
    el('p', { class: 'dg__panel-note' }, `${value}.`));
}

function generations() {
  const gens = ['oldest generation', 'older', 'younger', 'youngest generation'];
  const w = 360;
  const cw = (w - 3 * 6) / 4;
  return svg('svg', { class: 'dg__svg dg__gens', width: w, height: 52, viewBox: `0 0 ${w} 52`, role: 'img', 'aria-label': 'Four generations from oldest, where eviction happens, to youngest, where new pages enter.' },
    gens.map((g, i) => [rect(i * (cw + 6), 0, cw, 26, i === 0 ? 'dg-box dg-box--list' : i === 3 ? 'dg-box dg-box--active' : 'dg-box', 4),
      text(i * (cw + 6) + cw / 2, 17, `gen ${i}`, 'dg-t dg-t--small dg-t--ink', 'middle')]),
    text(0, 44, 'evicted from here', 'dg-t dg-t--small dg-t--faint'),
    text(w, 44, 'new pages join here', 'dg-t dg-t--small dg-t--faint', 'end'));
}
