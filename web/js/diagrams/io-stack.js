// io-stack: the layers a read or write crosses from the system call to the
// device, top to bottom, each annotated with the value this machine uses
// there for the disk holding /, when the snapshot has it. The mechanism is
// general; the "Here" lines are read from this machine (io-stack.model.js).

import { el } from '../lib/dom.js';
import { mountFrame, svgRoot, nextId, text } from './frame.js';
import { box, arrow, arrowDefs } from './flow.js';
import { measureBox, wrap } from './scale.js';
import { read, whyMissing } from './inputs.js';
import { IDS, stackModel } from './io-stack.model.js';

export const title = 'From a system call to the device';

const GAP = 22;

export function mount(container, ctx) {
  const records = new Map(IDS.map((id) => [id, read(ctx, id)]));
  const model = stackModel((id) => records.get(id));
  const notes = [...(model.rootNote ? [model.rootNote] : []), ...model.missing.map((r) => `Not shown: ${whyMissing(r)}.`)];
  return mountFrame(container, ctx, {
    labels: model.observed ? ['general', 'observed'] : ['general'],
    intro: 'Every file read or write that misses the page cache, and every writeback, passes these layers on its way to the device and back.',
    draw: (width) => draw(width, model),
    caption: 'The boxes describe Linux in general. The “Here” lines are this machine’s settings for the disk that holds /, where they could be read.',
    notes,
    alt: textAlternative(model),
    altLabel: 'The layers as a list',
    cites: model.used,
  });
}

function hereLines(row, w) {
  return row.here.flatMap((h) => wrap(`Here: ${h}`, w - 20, 12));
}

function draw(width, model) {
  const { defs, ids } = arrowDefs(nextId('dg-io'));
  const nodes = [defs];
  const labelW = width >= 560 ? 110 : 0;
  const x = labelW;
  const w = width - labelW;
  let y = 4;
  let prev = null;
  for (const row of model.rows) {
    if (prev !== null) nodes.push(arrow([[x + 24, prev], [x + 24, y - 1]], ids));
    const lines = hereLines(row, w);
    const extra = lines.length ? lines.length * 16 + 4 : 0;
    const b = box(x, y, w, { title: row.title, body: row.body, extraH: extra, cls: row.here.length ? 'dg-box dg-box--good' : 'dg-box' });
    nodes.push(b.node);
    const top = y + measureBox(row.title, row.body, w).height;
    lines.forEach((l, i) => nodes.push(text(x + 10, top + 4 + i * 16, l, 'dg-t dg-t--strong')));
    if (labelW) nodes.push(text(0, y + 18, row.layer, 'dg-t dg-t--title'));
    prev = y + b.h;
    y += b.h + GAP;
  }
  return svgRoot(width, y - GAP + 1, 'Layers from the system call down to the device, with this machine’s values for the disk holding / where known.', nodes);
}

function textAlternative(model) {
  return el('ol', { class: 'dg__steps' }, model.rows.map((r) => el('li', {},
    el('strong', {}, `${r.layer}, ${r.title}`), ': ', r.body, r.here.length ? ` Here: ${r.here.join('; ')}.` : '')));
}
