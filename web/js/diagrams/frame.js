// The chrome every diagram shares: provenance badges, the drawing (redrawn
// when its width changes, so text stays at reading size from 390 px up),
// a caption, a text alternative, the ids drawn from as links into Inspect,
// and the notice that replaces a drawing whose data is missing.
//
// Markup (classes in web/css/diagrams.css):
//   <div class="dg">
//     <div class="dg__badges">badge…</div>
//     <p class="dg__intro">…</p>
//     <div class="dg__canvas"><svg class="dg__svg">…</svg></div>   or  <div class="dg__missing">…</div>
//     <p class="dg__caption">…</p>  <ul class="dg__notes">…</ul>
//     <details class="dg__alt"><summary>…</summary>table or list</details>
//     <p class="dg__cites">Drawn from <a class="dg__cite">id</a>…</p>
//   </div>

import { el, svg } from '../lib/dom.js';
import { badge } from '../lib/ui.js';
import { whyMissing } from './inputs.js';

const FALLBACK_WIDTH = 640;
let uid = 0;

/** A fresh id prefix for one drawing's <defs> (patterns must not collide). */
export function nextId(prefix) {
  return `${prefix}-${++uid}`;
}

/**
 * Mounts a diagram into `container` and returns its unmount function.
 * spec: {
 *   labels:   provenance labels for the whole figure, e.g. ['observed']
 *   intro:    one sentence above the drawing (optional)
 *   draw:     (width) => Node, or null when nothing can be drawn
 *   missing:  records from inputs.readAll() that stopped the drawing
 *   caption:  text or nodes under the drawing
 *   notes:    extra sentences (sampling, optional inputs missing, caveats)
 *   alt:      a table or list with the same values (optional)
 *   altLabel: summary of the text alternative
 *   cites:    ids the drawing shows, linked to the inspector
 *   extra:    nodes appended after the drawing (optional)
 * }
 */
export function mountFrame(container, ctx, spec) {
  const canvas = el('div', { class: 'dg__canvas' });
  const root = el('div', { class: 'dg' },
    el('div', { class: 'dg__badges' }, (spec.labels ?? ['general']).map(badge)),
    spec.intro ? el('p', { class: 'dg__intro' }, spec.intro) : null,
    spec.draw ? canvas : missingNotice(spec.missing ?? []),
    spec.caption ? el('p', { class: 'dg__caption' }, spec.caption) : null,
    spec.notes?.length ? el('ul', { class: 'dg__notes' }, spec.notes.map((n) => el('li', {}, n))) : null,
    spec.extra ?? null,
    spec.alt ? el('details', { class: 'dg__alt' }, el('summary', {}, spec.altLabel ?? 'The values as a table'), spec.alt) : null,
    spec.cites?.length ? cites(spec.cites, ctx) : null,
  );
  container.append(root);

  let observer = null;
  let frame = 0;
  let drawnWidth = -1;
  const redraw = () => {
    frame = 0;
    const width = Math.floor(canvas.clientWidth) || FALLBACK_WIDTH;
    if (width === drawnWidth) return;
    drawnWidth = width;
    canvas.replaceChildren(spec.draw(width));
  };
  if (spec.draw) {
    redraw();
    if (typeof ResizeObserver === 'function') {
      observer = new ResizeObserver(() => {
        if (!frame) frame = requestAnimationFrame(redraw);
      });
      observer.observe(canvas);
    }
  }
  return () => {
    observer?.disconnect();
    if (frame) cancelAnimationFrame(frame);
    root.remove();
  };
}

/** The block shown instead of a drawing: what is missing and why. */
export function missingNotice(missing, lead = 'Not drawn: the snapshot does not have the values this diagram needs.') {
  return el('div', { class: 'dg__missing', role: 'note' },
    el('p', { class: 'dg__missing-lead' }, lead),
    el('ul', {}, missing.map((m) => el('li', {}, whyMissing(m)))));
}

// An id that may wrap only after its dots.
function idWords(id) {
  const parts = String(id).split('.');
  return parts.flatMap((p, i) => (i < parts.length - 1 ? [`${p}.`, el('wbr')] : [p]));
}

function cites(ids, ctx) {
  const links = [];
  for (const id of [...new Set(ids)]) {
    if (links.length) links.push(' ');
    links.push(el('a', {
      class: 'dg__cite',
      href: `#/inspect/${encodeURIComponent(id)}`,
      onclick: (e) => {
        if (typeof ctx?.inspect !== 'function') return;
        e.preventDefault();
        ctx.inspect(id, e.currentTarget);
      },
    }, idWords(id)));
  }
  return el('p', { class: 'dg__cites' }, el('span', { class: 'dg__cites-label' }, 'Drawn from'), ' ', links);
}

/** An <svg> of the given pixel size with an accessible name. */
export function svgRoot(width, height, label, ...children) {
  return svg('svg', {
    class: 'dg__svg',
    width,
    height,
    viewBox: `0 0 ${width} ${height}`,
    role: 'img',
    'aria-label': label,
  }, children);
}

/** SVG text with a class; `anchor` is start, middle or end. */
export function text(x, y, s, cls = 'dg-t', anchor = 'start') {
  return svg('text', { x: round(x), y: round(y), class: cls, 'text-anchor': anchor === 'start' ? null : anchor }, s);
}

/** A rectangle; r rounds the corners. */
export function rect(x, y, w, h, cls, r = 0) {
  return svg('rect', { x: round(x), y: round(y), width: round(Math.max(0, w)), height: round(Math.max(0, h)), class: cls, rx: r || null });
}

export function line(x1, y1, x2, y2, cls) {
  return svg('line', { x1: round(x1), y1: round(y1), x2: round(x2), y2: round(y2), class: cls });
}

export function path(d, cls) {
  return svg('path', { d, class: cls });
}

/** Rounds to a tenth of a pixel so attributes stay short. */
export function round(v) {
  return Math.round(v * 10) / 10;
}

/** A simple data table: head is a list of [text, numeric?]; rows are arrays of cells. */
export function table(caption, head, rows) {
  return el('div', { class: 'table-wrap' },
    el('table', { class: 'datatable' },
      el('caption', {}, caption),
      el('thead', {}, el('tr', {}, head.map(([h, num]) => el('th', { scope: 'col', class: num ? 'num' : null }, h)))),
      el('tbody', {}, rows.map((r) => el('tr', {}, r.map((c, i) => el('td', { class: head[i]?.[1] ? 'num' : null }, c)))))));
}
