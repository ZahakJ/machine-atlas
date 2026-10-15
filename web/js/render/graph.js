// SVG drawing for node-and-edge graphs: the prerequisite graph in Learn and
// the component map in Explore. Positions come from layout.js; this module
// only draws, wires focus and hover highlighting, and keeps every node a
// real link so the graph works with a keyboard and a screen reader.

import { svg } from '../lib/dom.js';
import { curve } from './layout.js';

export const NODE_W = 176;
export const NODE_H = 44;

let graphs = 0;

/**
 * nodes: [{id, x, y, title, href?, attrs?, label?}] — x, y is the top-left
 *   corner; href makes the node a link; label is its accessible name.
 * edges: [{from, to, cls, dy?, marker?}] — drawn between the facing sides of
 *   the two nodes; cls styles it; dy offsets parallel edges; marker is
 *   "arrow" or "arrow-observed".
 * Returns the <svg>. Hovering or focusing a node marks it and its edges with
 * data-hl so CSS can dim the rest.
 */
export function drawGraph({ nodes, edges, width, height, label, className = '' }) {
  const uid = `graph${++graphs}`;
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const edgeEls = [];
  const edgeLayer = svg('g', { class: 'graph__edges' });
  for (const e of edges) {
    const a = byId.get(e.from);
    const b = byId.get(e.to);
    if (!a || !b) continue;
    const dy = e.dy ?? 0;
    let d;
    if (a.x === b.x) {
      // Same column (possible in banded layouts): bow out to the right.
      const x = a.x + NODE_W;
      const y1 = a.y + NODE_H / 2 + dy;
      const y2 = b.y + NODE_H / 2 + dy;
      d = `M${x},${y1} C${x + 40},${y1} ${x + 40},${y2} ${x},${y2}`;
    } else if (a.x < b.x) {
      d = curve(a.x + NODE_W, a.y + NODE_H / 2 + dy, b.x, b.y + NODE_H / 2 + dy);
    } else {
      d = curve(b.x + NODE_W, b.y + NODE_H / 2 + dy, a.x, a.y + NODE_H / 2 + dy);
    }
    const path = svg('path', { d, class: `graph__edge ${e.cls ?? ''}`, 'data-from': e.from, 'data-to': e.to, 'marker-end': e.marker ? `url(#${uid}-${e.marker})` : null });
    edgeEls.push(path);
    edgeLayer.append(path);
  }

  const nodeLayer = svg('g', { class: 'graph__nodes' });
  for (const n of nodes) {
    const lines = wrap(n.title, 24, 2);
    const body = [
      svg('rect', { x: n.x, y: n.y, width: NODE_W, height: NODE_H, rx: 6, class: 'graph__box' }),
      svg('text', { x: n.x + 10, y: n.y + (lines.length === 1 ? NODE_H / 2 + 4 : NODE_H / 2 - 3), class: 'graph__text' },
        lines.map((line, i) => svg('tspan', { x: n.x + 10, dy: i === 0 ? 0 : 14 }, line))),
      n.mark ? svg('text', { x: n.x + NODE_W - 8, y: n.y + 14, class: 'graph__mark', 'text-anchor': 'end', 'aria-hidden': 'true' }, n.mark) : null,
      svg('title', {}, n.label ?? n.title),
    ];
    const g = n.href
      ? svg('a', { href: n.href, class: 'graph__node', 'data-id': n.id, 'aria-label': n.label ?? n.title, ...(n.attrs ?? {}) }, body)
      : svg('g', { class: 'graph__node', 'data-id': n.id, role: 'img', 'aria-label': n.label ?? n.title, ...(n.attrs ?? {}) }, body);
    nodeLayer.append(g);
  }

  const root = svg('svg', {
    class: `graph ${className}`, viewBox: `0 0 ${width} ${height}`, width, height,
    role: 'group', 'aria-label': label,
  },
  svg('defs', {},
    marker(`${uid}-arrow`, 'graph__arrow'),
    marker(`${uid}-arrow-observed`, 'graph__arrow graph__arrow--observed')),
  edgeLayer, nodeLayer);

  const highlight = (id) => {
    root.toggleAttribute('data-hl', id !== null);
    for (const g of nodeLayer.children) g.toggleAttribute('data-hl', g.dataset.id === id);
    for (const p of edgeEls) p.toggleAttribute('data-hl', id !== null && (p.dataset.from === id || p.dataset.to === id));
    if (id !== null) {
      // Neighbours stay readable too.
      for (const p of edgeEls) {
        if (!p.hasAttribute('data-hl')) continue;
        const other = p.dataset.from === id ? p.dataset.to : p.dataset.from;
        nodeLayer.querySelector(`[data-id="${CSS.escape(other)}"]`)?.setAttribute('data-hl', '');
      }
    }
  };
  for (const g of nodeLayer.children) {
    g.addEventListener('mouseenter', () => highlight(g.dataset.id));
    g.addEventListener('focus', () => highlight(g.dataset.id));
  }
  nodeLayer.addEventListener('mouseleave', () => highlight(null));
  root.addEventListener('focusout', (e) => { if (!root.contains(e.relatedTarget)) highlight(null); });
  return root;
}

function marker(id, cls) {
  return svg('marker', { id, viewBox: '0 0 10 10', refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: 'auto-start-reverse' },
    svg('path', { d: 'M0,1 L9,5 L0,9 z', class: cls }));
}

/** Splits text into at most `max` lines of about `width` characters. */
export function wrap(text, width, max) {
  const words = String(text).split(/\s+/);
  const lines = [];
  let line = '';
  for (const w of words) {
    if (line && (line + ' ' + w).length > width) {
      lines.push(line);
      line = w;
    } else line = line ? `${line} ${w}` : w;
  }
  if (line) lines.push(line);
  if (lines.length > max) {
    const kept = lines.slice(0, max);
    kept[max - 1] = kept[max - 1].slice(0, width - 1).replace(/\s+\S*$/, '') + '…';
    return kept;
  }
  return lines;
}
