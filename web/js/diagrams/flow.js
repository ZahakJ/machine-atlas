// Drawing helpers for the authored diagrams: text boxes, arrows with heads,
// and edge labels. Measurement is pure (scale.js measureBox); these build
// the SVG nodes.

import { svg } from '../lib/dom.js';
import { measureBox } from './scale.js';
import { rect, text, round } from './frame.js';

/**
 * A box at (x, y), `w` wide, with a bold title and a wrapped body. Returns
 * {node, h}. `extraH` leaves room under the text for the caller; `wrapW`
 * narrows the text column to leave room beside it. `pill` adds a small tag in the top right corner, such as
 * "minor" or "major", whose words carry the meaning (not only its colour).
 */
export function box(x, y, w, { title, body, cls = 'dg-box', pill = null, pillCls = '', extraH = 0, wrapW = w }) {
  const pillW = pill ? pill.length * 6.6 + 14 : 0;
  // Text keeps clear of the pill along the box's whole height: simpler
  // than flowing around it, and boxes with pills are short.
  const lines = measureBox(title, body, wrapW - (pill ? pillW + 4 : 0));
  const nodes = [rect(x, y, w, lines.height + extraH, cls, 6)];
  let ty = y + 10 + 12;
  for (const l of lines.title) {
    nodes.push(text(x + 10, ty, l, 'dg-t dg-t--title'));
    ty += 17;
  }
  if (lines.title.length) ty += 3;
  for (const l of lines.body) {
    nodes.push(text(x + 10, ty - 1, l, 'dg-t dg-t--ink'));
    ty += 16;
  }
  if (pill) {
    nodes.push(rect(x + w - pillW - 8, y + 8, pillW, 18, `dg-pill ${pillCls}`, 9));
    nodes.push(text(x + w - pillW / 2 - 8, y + 21, pill, 'dg-t dg-t--small dg-t--ink', 'middle'));
  }
  return { node: svg('g', {}, nodes), h: lines.height + extraH };
}

/** Arrowhead markers for one drawing; returns the defs node and marker ids by tone. */
export function arrowDefs(prefix) {
  const tones = ['plain', 'accent', 'bad', 'refault'];
  const ids = {};
  const markers = tones.map((tone) => {
    ids[tone] = `${prefix}-${tone}`;
    return svg('marker', { id: ids[tone], viewBox: '0 0 10 10', refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: 'auto-start-reverse' },
      svg('path', { d: 'M0 0L10 5L0 10Z', class: tone === 'plain' ? 'dg-arrowhead' : `dg-arrowhead dg-arrowhead--${tone}` }));
  });
  return { defs: svg('defs', {}, markers), ids };
}

/** A polyline arrow through `points` ([[x, y], …]) ending in a head. */
export function arrow(points, ids, tone = 'plain') {
  const d = points.map(([x, y], i) => `${i ? 'L' : 'M'}${round(x)} ${round(y)}`).join('');
  return svg('path', { d, class: tone === 'plain' ? 'dg-arrow' : `dg-arrow dg-arrow--${tone}`, 'marker-end': `url(#${ids[tone]})` });
}

/** A short label beside an arrow, on a surface-coloured backing so lines never cross the words. */
export function edgeLabel(x, y, words, anchor = 'start') {
  const w = words.length * 6.4 + 8;
  const x0 = anchor === 'middle' ? x - w / 2 : anchor === 'end' ? x - w : x;
  return svg('g', {}, rect(x0, y - 11, w, 15, 'dg-label-bg', 3), text(x0 + 4, y, words, 'dg-t dg-t--small dg-t--strong'));
}
