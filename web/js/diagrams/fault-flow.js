// fault-flow: the decision path of a page fault, from the access to the
// restarted instruction. Authored (GENERAL): true of Linux on every
// architecture it supports, not read from this machine.
//
// Wide drawings put the main path on the left with the two early exits to
// its right and the six resolutions in a grid; narrow drawings stack
// everything, with the exits indented under their decision.

import { el } from '../lib/dom.js';
import { mountFrame, svgRoot, nextId, rect, text } from './frame.js';
import { box, arrow, arrowDefs, edgeLabel } from './flow.js';
import { measureBox, columns } from './scale.js';

export const title = 'What happens on a page fault';

const STEPS = {
  access: { title: '1  Access', body: 'The CPU loads, stores or fetches an instruction at a virtual address.' },
  translate: { title: '2  Translate', body: 'The MMU looks in the TLB and, on a miss, walks the page tables. Is there a present entry that allows this kind of access?', cls: 'dg-box dg-box--decision' },
  hit: { title: 'No fault', body: 'The access completes. Nearly every access ends here.', cls: 'dg-box dg-box--good' },
  trap: { title: '3  Page-fault exception', body: 'The CPU stops the instruction, records the address and the kind of access, and enters the kernel’s fault handler.' },
  vma: { title: '4  Find the VMA', body: 'Is the address inside a mapping, and does the mapping allow this access (no write to read-only, no execute of data)?', cls: 'dg-box dg-box--decision' },
  segv: { title: 'SIGSEGV', body: 'No mapping there, or it forbids the access: the process receives a segmentation fault.', cls: 'dg-box dg-box--bad' },
  resolve: { title: '5  Resolve', body: 'What backs the mapping, and what does the page-table entry hold?', cls: 'dg-box dg-box--decision' },
  install: { title: '6  Install and restart', body: 'The kernel writes the page-table entry and returns from the exception. The CPU runs the same instruction again, and this time it succeeds.' },
};

const CASES = [
  { title: 'Anonymous, first read', body: 'Map the shared zero page, read-only. Nothing is allocated yet.', pill: 'minor' },
  { title: 'Anonymous, first write', body: 'Allocate a frame, fill it with zeros, map it writable; with THP possibly a whole huge page.', pill: 'minor' },
  { title: 'File, in the page cache', body: 'The data is already in memory: map the cached page.', pill: 'minor' },
  { title: 'File, not cached', body: 'Allocate a page-cache frame, read the data from storage and wait for it, then map it.', pill: 'major' },
  { title: 'Write to a shared read-only page', body: 'A private file page, or an anonymous page shared since fork: copy it to a new frame and map the copy writable (copy on write).', pill: 'minor' },
  { title: 'Swapped out', body: 'The entry holds a swap location: read the page back from swap (minor instead if it is still in the swap cache), then map it.', pill: 'major' },
];

const WIDE = 720;
const GAP = 34; // vertical space between boxes, room for an arrow and a label

export function mount(container, ctx) {
  return mountFrame(container, ctx, {
    labels: ['general'],
    intro: 'A page fault is how Linux fills in memory lazily: the first touch of a page traps into the kernel, which decides what the page should be and maps it.',
    draw,
    caption: 'Minor faults are resolved from memory; major faults wait for storage, and are counted separately (majflt) because they cost milliseconds instead of microseconds.',
    extra: el('ul', { class: 'dg__legend', 'aria-label': 'Key' },
      el('li', {}, el('span', { class: 'dg__swatch dg__swatch--pill', 'aria-hidden': 'true' }), 'minor: no I/O'),
      el('li', {}, el('span', { class: 'dg__swatch dg__swatch--pill-major', 'aria-hidden': 'true' }), 'major: waits for storage or swap')),
    alt: textAlternative(),
    altLabel: 'The path as a list',
  });
}

function draw(width) {
  const { defs, ids } = arrowDefs(nextId('dg-ff'));
  const wide = width >= WIDE;
  const nodes = [defs];
  const spineW = wide ? Math.min(440, Math.floor(width * 0.52)) : width;
  const exitX = wide ? spineW + 64 : 40;
  const exitW = wide ? Math.min(width - exitX, 340) : width - 40;
  const mid = wide ? spineW / 2 : 18; // where the main path's arrows run
  let y = 0;

  const step = (s) => {
    const b = box(0, y, spineW, s);
    nodes.push(b.node);
    const top = y;
    y += b.h;
    return { top, bottom: y, h: b.h };
  };
  const down = (from, label) => {
    const y0 = from;
    y += GAP;
    nodes.push(arrow([[mid, y0], [mid, y - 1]], ids));
    if (label) nodes.push(edgeLabel(mid + 6, y0 + 20, label));
  };

  // A decision with an exit: right of the box when wide, indented under it
  // when narrow. Returns the y the main path continues from.
  const decision = (s, exit, exitWord, mainWord, tone) => {
    const d = step(s);
    if (wide) {
      const b = box(exitX, d.top, exitW, exit);
      nodes.push(b.node);
      const ay = d.top + Math.min(d.h, b.h) / 2;
      nodes.push(arrow([[spineW, ay], [exitX - 1, ay]], ids, tone));
      nodes.push(edgeLabel(spineW + 8, ay - 6, exitWord));
      y = Math.max(y, d.top + b.h);
      down(d.bottom, mainWord);
    } else {
      const ey = y + GAP;
      const b = box(exitX, ey, exitW, exit);
      nodes.push(b.node);
      nodes.push(arrow([[exitX + 22, d.bottom], [exitX + 22, ey - 1]], ids, tone));
      nodes.push(edgeLabel(exitX + 28, d.bottom + 20, exitWord));
      const from = d.bottom;
      y = ey + b.h;
      const y0 = y;
      y += GAP;
      nodes.push(arrow([[mid, from], [mid, y - 1]], ids));
      nodes.push(edgeLabel(mid + 6, y0 + 20, mainWord));
    }
  };

  const a = step(STEPS.access);
  down(a.bottom);
  decision(STEPS.translate, STEPS.hit, 'yes', 'no', 'accent');
  const t = step(STEPS.trap);
  down(t.bottom);
  decision(STEPS.vma, STEPS.segv, 'no', 'yes', 'bad');
  const r = step(STEPS.resolve);

  // The six resolutions sit in one container: the kernel takes exactly one
  // of them, and every one ends at step 6.
  const pad = 12;
  const groupTop = r.bottom + GAP;
  nodes.push(arrow([[mid, r.bottom], [mid, groupTop - 1]], ids));
  const innerW = width - 2 * pad;
  const cols = wide ? columns(innerW, 220, 12, 3) : 1;
  const cardW = (innerW - (cols - 1) * 12) / cols;
  const pillRoom = 60; // box() keeps text clear of the pill
  const heights = CASES.map((c) => measureBox(c.title, c.body, cardW - pillRoom).height);
  const cards = [];
  let cy = groupTop + 30;
  for (let i = 0; i < CASES.length; i += cols) {
    const rowH = Math.max(...heights.slice(i, i + cols));
    for (let j = 0; j < cols && i + j < CASES.length; j++) {
      const c = CASES[i + j];
      cards.push(box(pad + j * (cardW + 12), cy, cardW, { ...c, cls: c.pill === 'major' ? 'dg-box dg-box--major' : 'dg-box dg-box--minor', pillCls: `dg-pill--${c.pill}` }).node);
    }
    cy += rowH + 12;
  }
  const groupBottom = cy - 12 + pad;
  nodes.push(rect(0, groupTop, width, groupBottom - groupTop, 'dg-box dg-box--list', 8));
  nodes.push(text(pad, groupTop + 20, 'Exactly one of these six cases:', 'dg-t dg-t--strong'));
  nodes.push(...cards);

  y = groupBottom + GAP;
  nodes.push(arrow([[mid, groupBottom], [mid, y - 1]], ids));
  step({ ...STEPS.install, cls: 'dg-box dg-box--good' });
  return svgRoot(width, y + 1, 'Flowchart of a page fault: access, translation, exception, VMA lookup, six ways to resolve the fault, then the instruction restarts.', nodes);
}

function textAlternative() {
  const item = (s) => el('li', {}, el('strong', {}, s.title.replace(/^\d\s+/, '')), ': ', s.body);
  return el('ol', { class: 'dg__steps' },
    item(STEPS.access),
    el('li', {}, el('strong', {}, 'Translate'), ': ', STEPS.translate.body, ' If yes: ', STEPS.hit.body),
    item(STEPS.trap),
    el('li', {}, el('strong', {}, 'Find the VMA'), ': ', STEPS.vma.body, ' If not: ', STEPS.segv.body),
    el('li', {}, el('strong', {}, 'Resolve'), ': ', STEPS.resolve.body,
      el('ul', {}, CASES.map((c) => el('li', {}, el('strong', {}, c.title), ` (${c.pill} fault): `, c.body)))),
    item(STEPS.install));
}
