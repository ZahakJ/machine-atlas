// packet-path: the layers a packet crosses on the way out (left, downward)
// and on the way in (right, upward), each annotated with the value this
// machine uses there when the snapshot has it. The mechanism is general;
// the "Here" lines are read from this machine (packet-path.model.js).
//
// Wide drawings put the two directions side by side, row by row per layer;
// narrow ones stack the sending path above the receiving one.

import { el } from '../lib/dom.js';
import { mountFrame, svgRoot, nextId, rect, text } from './frame.js';
import { box, arrow, arrowDefs } from './flow.js';
import { measureBox, wrap } from './scale.js';
import { read, whyMissing } from './inputs.js';
import { IDS, pathModel } from './packet-path.model.js';

export const title = 'The path of a packet through the kernel';

const WIDE = 640;
const GAP = 26;

export function mount(container, ctx) {
  const records = new Map(IDS.map((id) => [id, read(ctx, id)]));
  const model = pathModel((id) => records.get(id));
  const notes = model.missing.map((r) => `Not shown: ${whyMissing(r)}.`);
  return mountFrame(container, ctx, {
    labels: model.observed ? ['general', 'observed'] : ['general'],
    intro: 'Every packet crosses the same layers in both directions; queues sit only where two sides run at different speeds.',
    draw: (width) => draw(width, model),
    caption: 'The boxes describe Linux in general. The “Here” lines are this machine’s settings and counters where they could be read; a layer without one has nothing collected for it.',
    notes,
    alt: textAlternative(model),
    altLabel: 'The path as a list',
    cites: model.used,
  });
}

// One stage box with its "Here" lines under the body. Returns {node, h}.
function stage(x, y, w, s) {
  const lines = s.here.flatMap((h) => wrap(`Here: ${h}`, w - 20, 12));
  const extra = lines.length ? lines.length * 16 + 4 : 0;
  const b = box(x, y, w, { title: s.title, body: s.body, extraH: extra, cls: s.here.length ? 'dg-box dg-box--good' : 'dg-box' });
  const top = y + measureBox(s.title, s.body, w).height;
  const nodes = [b.node];
  lines.forEach((l, i) => nodes.push(text(x + 10, top + 4 + i * 16, l, 'dg-t dg-t--strong')));
  return { nodes, h: b.h };
}

function draw(width, model) {
  const { defs, ids } = arrowDefs(nextId('dg-pp'));
  const nodes = [defs];
  const wide = width >= WIDE;
  const wireH = 30;

  if (wide) {
    const colW = Math.floor((width - 24) / 2);
    const rx = width - colW;
    nodes.push(text(0, 14, 'Sending', 'dg-t dg-t--title'));
    nodes.push(text(rx, 14, 'Receiving', 'dg-t dg-t--title'));
    let y = 28;
    const tops = [];
    const bottoms = [];
    for (const row of model.rows) {
      const hs = measureH(row.send, colW);
      const hr = measureH(row.receive, colW);
      const h = Math.max(hs, hr);
      const s = stage(0, y, colW, row.send);
      const r = stage(rx, y, colW, row.receive);
      nodes.push(...s.nodes, ...r.nodes);
      tops.push(y);
      bottoms.push([y + s.h, y + r.h]);
      y += h + GAP;
    }
    for (let i = 0; i + 1 < model.rows.length; i++) {
      nodes.push(arrow([[colW / 2, bottoms[i][0]], [colW / 2, tops[i + 1] - 1]], ids));
      nodes.push(arrow([[rx + colW / 2, tops[i + 1]], [rx + colW / 2, bottoms[i][1] + 1]], ids, 'accent'));
    }
    const wy = y;
    const last = model.rows.length - 1;
    nodes.push(arrow([[colW / 2, bottoms[last][0]], [colW / 2, wy - 1]], ids));
    nodes.push(arrow([[rx + colW / 2, wy], [rx + colW / 2, bottoms[last][1] + 1]], ids, 'accent'));
    nodes.push(rect(0, wy, width, wireH, 'dg-box dg-box--sink', 6));
    nodes.push(text(width / 2, wy + 19, 'The wire or the radio', 'dg-t dg-t--strong', 'middle'));
    return svgRoot(width, wy + wireH + 1, 'Two columns of stages: sending from send() down to the NIC, receiving from the NIC up to recv(), with this machine’s values where known.', nodes);
  }

  // Narrow: sending top to bottom, the wire, then receiving from the NIC up.
  nodes.push(text(0, 14, 'Sending', 'dg-t dg-t--title'));
  let y = 26;
  let prev = null;
  for (const row of model.rows) {
    if (prev !== null) nodes.push(arrow([[24, prev], [24, y - 1]], ids));
    const s = stage(0, y, width, row.send);
    nodes.push(...s.nodes);
    prev = y + s.h;
    y += s.h + GAP;
  }
  nodes.push(arrow([[24, prev], [24, y - 1]], ids));
  nodes.push(rect(0, y, width, wireH, 'dg-box dg-box--sink', 6));
  nodes.push(text(width / 2, y + 19, 'The wire or the radio', 'dg-t dg-t--strong', 'middle'));
  y += wireH + GAP;
  nodes.push(text(0, y + 2, 'Receiving, from the NIC up', 'dg-t dg-t--title'));
  y += 14;
  prev = null;
  for (const row of [...model.rows].reverse()) {
    if (prev !== null) nodes.push(arrow([[24, prev], [24, y - 1]], ids, 'accent'));
    const r = stage(0, y, width, row.receive);
    nodes.push(...r.nodes);
    prev = y + r.h;
    y += r.h + GAP;
  }
  return svgRoot(width, y - GAP + 1, 'Stages of sending from send() down to the NIC, then of receiving from the NIC up to recv(), with this machine’s values where known.', nodes);
}

function measureH(s, w) {
  const lines = s.here.flatMap((h) => wrap(`Here: ${h}`, w - 20, 12));
  return measureBox(s.title, s.body, w).height + (lines.length ? lines.length * 16 + 4 : 0);
}

function textAlternative(model) {
  const item = (layer, s) => el('li', {}, el('strong', {}, `${layer}, ${s.title}`), ': ', s.body,
    s.here.length ? ` Here: ${s.here.join('; ')}.` : '');
  return el('div', {},
    el('p', {}, 'Sending:'),
    el('ol', { class: 'dg__steps' }, model.rows.map((r) => item(r.layer, r.send))),
    el('p', {}, 'Receiving:'),
    el('ol', { class: 'dg__steps' }, [...model.rows].reverse().map((r) => item(r.layer, r.receive))));
}
