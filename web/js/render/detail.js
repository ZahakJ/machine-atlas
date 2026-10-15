// The full account of one observation or derived fact: its exact value,
// what kind of claim it is, where it came from, what the catalog says it
// cannot tell you, and which lessons and components rely on it. Shown in
// the side inspector and in Inspect's detail pane, so the two never
// disagree.

import { el } from '../lib/dom.js';
import { formatAge, formatCount } from '../lib/format.js';
import { href } from '../router.js';
import { badge, idText, miniLabel, statusPill, statusText, timeEl, titleOf } from './common.js';
import { shortValue, exactValue, isTable, tableColumns, cellText, columnTitle } from './values.js';

const TABLE_PREVIEW_ROWS = 200;

/**
 * Builds the detail for `id`. Resolves catalog, usage, journey and
 * components through the session; any of them failing only drops its part.
 * opts.compact (the side inspector) adds an "Open in Inspect" link.
 */
export async function renderDetail(session, id, opts = {}) {
  const found = session.lookup(id);
  const [catalog, usage, journey, components] = await Promise.all(
    ['catalog', 'usage', 'journey', 'components'].map((n) => session.load(n).catch(() => null)));
  const entry = catalog?.lookup(id);

  if (!found) {
    return el('div', { class: 'detail stack' },
      el('p', { class: 'empty' }, entry
        ? 'This id is in the catalog but this snapshot does not contain it.'
        : 'There is no observation or derived fact with this id in this snapshot.'),
      entry ? catalogPart(entry, id) : null,
      usedBy(usage?.[id], journey, components));
  }

  const { type, item } = found;
  const parts = [];

  parts.push(el('div', { class: 'detail__head' },
    entry?.title ? el('p', { class: 'detail__title' }, entry.title) : null,
    valuePart(item),
    el('div', { class: 'cluster' },
      badge(type === 'derived' ? 'derived' : 'observed'),
      type === 'derived'
        ? el('span', { class: 'status', 'data-status': item.status === 'ok' ? 'ok' : 'absent' }, item.status === 'ok' ? 'ok' : 'unknown')
        : statusPill(item.status),
      type === 'obs' ? kindTag(item.kind) : el('span', { class: 'tag', 'data-basis': item.basis }, item.basis === 'inference' ? 'inference' : 'arithmetic'))));

  if (type === 'obs' && item.status !== 'ok' && item.note) {
    parts.push(el('p', { class: 'detail__note', 'data-status': item.status }, el('strong', {}, `Why ${statusText(item.status)}: `), item.note));
  } else if (type === 'obs' && item.note) {
    parts.push(el('p', { class: 'detail__note' }, el('strong', {}, 'Note: '), item.note));
  }
  if (type === 'derived' && item.status !== 'ok' && item.reason) {
    parts.push(el('p', { class: 'detail__note', 'data-status': 'absent' }, el('strong', {}, 'Why unknown: '), item.reason));
  }

  if (isTable(item.value) && item.status === 'ok') parts.push(tablePart(item.value, entry?.columns));

  parts.push(type === 'obs' ? sourcePart(item) : rulePart(session, item));
  if (entry) parts.push(catalogPart(entry, id));
  else parts.push(el('p', { class: 'caption' }, 'Not in this version’s catalog: the snapshot carries an id this version of Machine Atlas does not describe.'));
  parts.push(usedBy(usage?.[id], journey, components));
  if (opts.compact) {
    parts.push(el('p', {}, el('a', { href: href('inspect', id), class: 'detail__more' }, 'Open in Inspect, with everything else →')));
  }
  return el('div', { class: 'detail' }, parts);
}

function valuePart(item) {
  const exact = exactValue(item);
  const unit = item.unit && !['bytes', 'pages', 'bits', 'count'].includes(item.unit) && typeof item.value === 'number' ? item.unit : '';
  return el('div', {},
    el('p', { class: 'inspector__value', 'data-status': item.status }, shortValue(item)),
    exact ? el('p', { class: 'inspector__exact' }, exact) : null,
    unit ? el('p', { class: 'inspector__exact' }, `unit: ${unit}`) : null);
}

function kindTag(kind) {
  return kind === 'live'
    ? el('span', { class: 'tag', 'data-kind': 'live', title: 'A measurement: it varies from moment to moment' }, 'live measurement')
    : el('span', { class: 'tag', 'data-kind': 'static', title: 'Configuration: stable across a boot or longer' }, 'static configuration');
}

// Columns in the catalog's order (the kernel's own order, usually), then
// any the catalog does not list; a "prefix*" entry stands for its family.
function tablePart(rows, declared = []) {
  const present = tableColumns(rows);
  const columns = [];
  for (const d of declared ?? []) {
    const match = d.endsWith('*') ? present.filter((c) => c.startsWith(d.slice(0, -1))).sort((a, b) => a.localeCompare(b, 'en', { numeric: true })) : present.filter((c) => c === d);
    for (const c of match) if (!columns.includes(c)) columns.push(c);
  }
  for (const c of present) if (!columns.includes(c)) columns.push(c);
  const shown = rows.slice(0, TABLE_PREVIEW_ROWS);
  return el('div', { class: 'table-wrap' },
    el('table', { class: 'datatable' },
      el('caption', { class: 'visually-hidden' }, 'Exact value'),
      el('thead', {}, el('tr', {}, columns.map((c) => el('th', { scope: 'col', class: numericColumn(rows, c) ? 'num' : null, title: c }, columnTitle(c))))),
      el('tbody', {}, shown.map((r) => el('tr', {}, columns.map((c) =>
        el('td', { class: typeof r[c] === 'number' ? 'num' : 'mono' }, cellText(r[c]))))))),
    el('p', { class: 'datatable__foot' },
      rows.length > shown.length ? `Showing ${formatCount(shown.length)} of ${formatCount(rows.length)} rows.` : `${formatCount(rows.length)} row${rows.length === 1 ? '' : 's'}.`));
}

function numericColumn(rows, c) {
  return rows.some((r) => typeof r[c] === 'number');
}

function sourcePart(o) {
  const s = o.source ?? {};
  return el('section', { class: 'detail__section' },
    el('h3', { class: 'detail__h' }, 'Where it comes from'),
    el('dl', { class: 'facts' },
      row('Source', s.type),
      row('Path', s.path ? el('code', {}, s.path) : null),
      row('Field', s.field ? el('code', {}, s.field) : null),
      row('Detail', s.detail),
      row('Collector', o.collector ? el('code', {}, o.collector) : null),
      row('Kind', o.kind === 'live' ? 'live: a measurement that varies' : 'static: configuration, stable across a boot'),
      row('Scope', o.scope),
      row('Collected', o.collected_at ? el('span', {}, timeEl(o.collected_at), el('span', { class: 'caption' }, ` · ${formatAge(o.collected_at)}`)) : null)));
}

function rulePart(session, f) {
  return el('section', { class: 'detail__section' },
    el('h3', { class: 'detail__h' }, f.basis === 'inference' ? 'How it was inferred' : 'How it was computed'),
    f.basis === 'inference'
      ? el('p', { class: 'caption' }, 'An inference from evidence, not something the machine reports directly.')
      : null,
    el('p', { class: 'detail__rule' }, f.rule || '—'),
    (f.inputs ?? []).length
      ? el('div', {},
        el('p', { class: 'detail__label' }, 'Inputs'),
        el('ul', { class: 'detail__inputs' }, f.inputs.map((input) => {
          const found = session.lookup(input);
          return el('li', {},
            el('a', { href: href('inspect', input) }, el('code', {}, idText(input))),
            found ? [el('span', { class: 'detail__input-value' }, ' = ', shortValue(found.item)), ' ', miniLabel(found.type === 'derived' ? 'derived' : 'obs')] : el('span', { class: 'caption' }, ' (not in this snapshot)'));
        })))
      : null);
}

function catalogPart(entry, id) {
  const family = entry.id !== id;
  return el('section', { class: 'detail__section' },
    el('h3', { class: 'detail__h' }, 'What it means'),
    family ? el('p', { class: 'caption' }, 'Described by the family ', el('code', {}, entry.id)) : null,
    entry.description ? el('p', { class: 'detail__desc' }, entry.description) : null,
    entry.caveat
      ? el('aside', { class: 'callout', 'data-kind': 'cannot' }, el('p', { class: 'callout__kind' }, 'What this cannot tell you'), el('p', {}, entry.caveat))
      : null,
    entry.privileged
      ? el('p', { class: 'detail__privileged' }, el('strong', {}, 'What root would add: '), entry.privileged)
      : null);
}

function usedBy(users, journey, components) {
  const lessons = users?.lessons ?? [];
  const comps = users?.components ?? [];
  const compTitle = (cid) => components?.nodes?.find((n) => n.id === cid)?.title ?? cid;
  return el('section', { class: 'detail__section' },
    el('h3', { class: 'detail__h' }, 'Used by'),
    lessons.length === 0 && comps.length === 0
      ? el('p', { class: 'caption' }, 'No lesson or component refers to this id yet.')
      : el('div', { class: 'used-by' },
        lessons.length ? el('div', {}, el('p', { class: 'detail__label' }, 'Lessons'),
          el('ul', { class: 'link-list' }, lessons.map((l) => el('li', {}, el('a', { href: href('learn', l) }, titleOf(journey, l)))))) : null,
        comps.length ? el('div', {}, el('p', { class: 'detail__label' }, 'Components'),
          el('ul', { class: 'link-list' }, comps.map((c) => el('li', {}, el('a', { href: href('explore', c) }, compTitle(c)))))) : null));
}

function row(term, value) {
  if (value === null || value === undefined || value === '') return null;
  return el('div', {}, el('dt', {}, term), el('dd', {}, value));
}
