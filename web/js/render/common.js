// Small DOM pieces every view repeats: chips, links, loading and error
// states. Text from the API only ever reaches the page through el()'s text
// nodes, and hrefs only through safeHref().

import { el } from '../lib/dom.js';
import { badge, statusPill, statusText } from '../lib/ui.js';
import { href, safeHref } from '../router.js';

export { badge, statusPill, statusText };

/**
 * A chip: an inline value that opens the inspector for its id.
 * v: {ref, kind: "obs"|"derived", display, status, title?}.
 * open(ref, button) is called on activation; the chip stays a <button> so
 * it is reachable and operable from the keyboard.
 */
export function chip(v, open) {
  const ok = v.status === 'ok' || v.status === undefined;
  // Derived facts are "unknown" rather than missing: they were never read.
  const missing = v.status === 'unknown' ? 'unknown' : `not available (${statusText(v.status)})`;
  const text = ok ? (v.display || '—') : (v.display || missing);
  const what = v.kind === 'derived' ? 'derived' : 'observed';
  const b = el('button', {
    class: 'chip',
    type: 'button',
    'data-kind': v.kind === 'derived' ? 'derived' : 'obs',
    'data-status': v.status === 'unknown' ? 'absent' : v.status ?? 'ok',
    'data-ref': v.ref,
    title: v.ref,
    'aria-label': `${v.title || v.ref}: ${text}, ${what}. Open in the inspector`,
  }, text);
  b.addEventListener('click', (e) => {
    e.stopPropagation();
    open(v.ref, b);
  });
  return b;
}

/**
 * A compact provenance label for a value in a list of readings: OBS or
 * DER, in the badge's shape and colour, so a list of values never tells
 * observed from derived by underline style alone. Hidden from assistive
 * technology because the chip's own name already says which it is.
 */
export function miniLabel(kind) {
  const derived = kind === 'derived';
  return el('span', {
    class: 'badge badge--mini',
    'data-label': derived ? 'derived' : 'observed',
    title: derived ? 'Derived: computed from observations by a stated rule' : 'Observed: read from this machine',
    'aria-hidden': 'true',
  }, derived ? 'Der' : 'Obs');
}

/** A chip followed by its compact label, for value lists (overview, Explore). */
export function labelledChip(chipEl, kind) {
  return el('span', { class: 'labelled-value' }, chipEl, miniLabel(kind));
}

/**
 * The key to the provenance labels, as a disclosure: the four badges, the
 * compact OBS/DER marks, the ¶ margin mark and the two underline styles.
 */
export function labelKey() {
  return el('details', { class: 'label-key' },
    el('summary', {}, 'How to read the labels'),
    el('dl', { class: 'label-key__list' },
      el('div', {}, el('dt', {}, badge('observed')), el('dd', {}, 'Read from this machine. In lists, marked OBS. Its value has a solid underline; select it to see its source and time.')),
      el('div', {}, el('dt', {}, badge('derived')), el('dd', {}, 'Computed from observations by a stated rule. In lists, marked DER; its value has a dashed underline.')),
      el('div', {}, el('dt', {}, badge('simulated')), el('dd', {}, 'Produced by a model with stated assumptions; not a measurement.')),
      el('div', {}, el('dt', {}, el('span', { class: 'blk__mark blk__mark--inline', 'aria-hidden': 'true' }, '¶'), ' General'), el('dd', {}, 'Unbadged prose, marked ¶ in the margin: true of Linux in general, not specific to this machine.'))));
}

/**
 * An id as text that may wrap only after its dots ("mem.zones.<wbr>…"), so
 * a long id breaks between its parts and never inside a word. A part too
 * long for a narrow column (compact_daemon_migrate_scanned) may also wrap
 * after an underscore.
 */
export function idText(id) {
  const parts = String(id).split('.');
  return parts.flatMap((p, i) => {
    const words = p.length > 20 ? p.split('_').flatMap((w, j, all) => (j < all.length - 1 ? [`${w}_`, el('wbr')] : [w])) : [p];
    return i < parts.length - 1 ? [...words.slice(0, -1), `${words[words.length - 1]}.`, el('wbr')] : words;
  });
}

/** A chip for a Value of /api/components or /api/roadmap ({id, kind, title, display, status}). */
export function valueChip(v, open) {
  return chip({ ref: v.id, kind: v.kind, display: v.display, status: v.status, title: v.title }, open);
}

/**
 * A link to a target that came from data: an external http(s) URL (new tab,
 * no referrer, no opener) or an internal route. An unsafe target renders as
 * plain text so the words survive and nothing is clickable.
 */
export function link(target, ...children) {
  const safe = safeHref(target);
  if (!safe) return el('span', { class: 'link-refused', title: 'Link target refused' }, ...children);
  if (safe.external) {
    return el('a', { href: safe.href, target: '_blank', rel: 'noopener noreferrer', class: 'link-external' },
      ...children, el('span', { class: 'visually-hidden' }, ' (opens in a new tab)'));
  }
  return el('a', { href: safe.href }, ...children);
}

/** A link to a route of this app. */
export function routeLink(view, id, ...children) {
  return el('a', { href: href(view, id) }, ...children);
}

/** A placeholder while a request is in flight. */
export function loading(what = 'Loading') {
  return el('div', { class: 'loading', role: 'status' }, el('span', { class: 'loading__dot', 'aria-hidden': 'true' }), `${what}…`);
}

/**
 * An error box with the server's message and, when given, a retry button.
 * err.details (import validation problems) are listed verbatim.
 */
export function errorBox(err, retry, title = 'Something went wrong') {
  const details = Array.isArray(err?.details) ? err.details : [];
  return el('div', { class: 'error-box', role: 'alert' },
    el('p', { class: 'error-box__title' }, title),
    el('p', {}, err?.message ?? String(err)),
    details.length ? el('ul', { class: 'error-box__list' }, details.map((d) => el('li', {}, d))) : null,
    retry ? el('div', { class: 'cluster' }, el('button', { class: 'btn btn--small', type: 'button', onclick: retry }, 'Try again')) : null);
}

/**
 * Fills `node` from an async producer: a loading state first, then the
 * produced element, or an error box with retry. Returns the promise.
 */
export async function fill(node, produce, what) {
  node.replaceChildren(loading(what));
  try {
    const out = await produce();
    if (out) node.replaceChildren(out);
  } catch (err) {
    console.error(err);
    node.replaceChildren(errorBox(err, () => fill(node, produce, what)));
  }
}

/** "Lesson title" for an id, using the journey graph's titles when known. */
export function titleOf(journey, id) {
  const node = journey?.graph?.nodes?.find((n) => n.id === id);
  return node?.title ?? id;
}

/** RFC 3339 → "2026-01-15 12:00:03 UTC", or the input when unparsable. */
export function formatTime(s) {
  if (!s) return '';
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return d.toISOString().replace('T', ' ').replace(/\.\d+Z$|Z$/, ' UTC');
}

/** A <time> element showing the exact time, machine-readable. */
export function timeEl(s) {
  return s ? el('time', { datetime: s, class: 'num' }, formatTime(s)) : null;
}
