// Inspect (#/inspect, #/inspect/<id>): every observation and derived fact
// in one searchable table, the full account of the selected one, the
// collector run report and the snapshot's metadata. Filters live in the
// route's query, so a filtered view can be linked to.

import { el } from '../lib/dom.js';
import { formatAge, formatCount } from '../lib/format.js';
import { href } from '../router.js';
import { badge, errorBox, formatTime, idText, loading, statusPill, statusText, timeEl } from '../render/common.js';
import { renderDetail } from '../render/detail.js';
import { buildRows, facet, filtersFrom, matches } from '../render/values.js';

const STATUS_ORDER = ['ok', 'absent', 'permission_denied', 'unsupported', 'failed', 'not_collected', 'unknown'];

export function createInspect(app) {
  const { session } = app;
  let rows = [];
  let filters = {};
  let selected = null;
  let tbody = null;
  let countLine = null;
  let detailBox = null;
  let grid = null;
  let controls = {};
  let rowEls = new Map();
  let token = 0;

  function filterBar() {
    const select = (name, label, options) => {
      const s = el('select', { class: 'select', id: `f-${name}`, 'data-filter': name },
        el('option', { value: '' }, 'Any'),
        options.map(({ value, label: text, count }) => el('option', { value }, `${text ?? value} (${formatCount(count)})`)));
      s.value = filters[name] ?? '';
      s.addEventListener('change', () => setFilter(name, s.value));
      controls[name] = s;
      return el('div', { class: 'field' }, el('label', { class: 'field__label', for: `f-${name}` }, label), s);
    };
    const search = el('input', {
      class: 'input', id: 'f-q', type: 'search', placeholder: 'id, title, value or source path', value: filters.q ?? '',
      autocomplete: 'off', spellcheck: 'false',
    });
    let timer = null;
    search.addEventListener('input', () => {
      clearTimeout(timer);
      timer = setTimeout(() => setFilter('q', search.value.trim()), 120);
    });
    controls.q = search;
    const statusOpts = facet(rows, 'status').sort((a, b) => STATUS_ORDER.indexOf(a.value) - STATUS_ORDER.indexOf(b.value))
      .map((f) => ({ ...f, label: f.value === 'unknown' ? 'unknown (derived)' : statusText(f.value) }));
    const clear = el('button', { class: 'btn btn--small btn--quiet', type: 'button' }, 'Clear filters');
    clear.addEventListener('click', () => {
      filters = {};
      for (const [name, c] of Object.entries(controls)) c.value = '';
      apply();
      controls.q.focus();
    });
    return el('form', { class: 'filters', role: 'search', 'aria-label': 'Filter observations', onsubmit: (e) => e.preventDefault() },
      el('div', { class: 'field filters__search' }, el('label', { class: 'field__label', for: 'f-q' }, 'Search'), search),
      select('status', 'Status', statusOpts),
      select('kind', 'Kind', facet(rows, 'kind').map((f) => ({ ...f, label: { static: 'static configuration', live: 'live measurement', derived: 'derived fact' }[f.value] ?? f.value }))),
      select('origin', 'Collector or basis', facet(rows, 'origin')),
      select('ns', 'Namespace', facet(rows, 'namespace')),
      el('div', { class: 'filters__clear' }, clear));
  }

  function setFilter(name, value) {
    if (value) filters[name] = value;
    else delete filters[name];
    apply();
  }

  // Applies the filters to the table and records them in the address bar
  // without a navigation, so the table keeps its scroll position.
  function apply() {
    const shown = rows.filter((r) => matches(r, filters));
    tbody.replaceChildren(...shown.map(rowEl));
    countLine.textContent = shown.length === rows.length
      ? `${formatCount(rows.length)} observations and derived facts.`
      : `Showing ${formatCount(shown.length)} of ${formatCount(rows.length)}.`;
    history.replaceState(null, '', href('inspect', selected, filters));
    for (const a of document.querySelectorAll('.inspect a[data-keep-filters]')) a.setAttribute('href', href('inspect', a.dataset.keepFilters || null, filters));
  }

  function rowEl(r) {
    if (rowEls.has(r.id)) {
      const tr = rowEls.get(r.id);
      tr.querySelector('a').setAttribute('href', href('inspect', r.id, filters));
      return tr;
    }
    const o = r.item;
    const kindCell = r.type === 'derived'
      ? el('span', { class: 'tag', 'data-basis': o.basis }, o.basis === 'inference' ? 'derived · inference' : 'derived')
      : o.kind === 'live'
        ? el('span', { class: 'kind-live' }, el('span', { class: 'tag', 'data-kind': 'live' }, 'live'), ' ', el('time', { datetime: o.collected_at, class: 'num caption', title: formatTime(o.collected_at) }, clock(o.collected_at)))
        : el('span', { class: 'tag', 'data-kind': 'static' }, 'static');
    const tr = el('tr', { 'data-id': r.id, 'aria-current': r.id === selected ? 'true' : null },
      el('td', { class: 'inspect__id' }, el('a', { href: href('inspect', r.id, filters), class: 'mono' }, idText(r.id)),
        r.title ? el('span', { class: 'inspect__title' }, r.title) : null),
      el('td', { class: 'inspect__value', 'data-status': r.status }, r.status === 'ok' ? r.value : '—'),
      el('td', {}, r.type === 'derived'
        ? el('span', { class: 'status', 'data-status': r.status === 'ok' ? 'ok' : 'absent' }, r.status === 'ok' ? 'ok' : 'unknown')
        : statusPill(r.status)),
      el('td', {}, kindCell));
    tr.addEventListener('click', (e) => {
      if (e.target.closest('a')) return;
      location.hash = href('inspect', r.id, filters);
    });
    rowEls.set(r.id, tr);
    return tr;
  }

  async function showDetail(id) {
    selected = id;
    grid.dataset.selected = id ? 'true' : 'false';
    for (const [rid, tr] of rowEls) {
      if (rid === id) tr.setAttribute('aria-current', 'true');
      else tr.removeAttribute('aria-current');
    }
    const mine = ++token;
    if (!id) {
      detailBox.replaceChildren(el('div', { class: 'inspect__empty' },
        el('h2', { class: 'inspect__detail-title' }, 'Select a row'),
        el('p', { class: 'caption' }, 'The detail shows the exact value, its unit, kind and scope, where it was read and when, what the catalog says it cannot tell you, and which lessons and components use it.')));
      return;
    }
    const found = session.lookup(id);
    const back = el('a', { class: 'inspect__back', href: href('inspect', null, filters), 'data-keep-filters': '' }, '← All observations');
    detailBox.replaceChildren(back,
      el('p', { class: 'eyebrow' }, found?.type === 'derived' ? 'Derived fact' : found ? 'Observation' : 'Not in this snapshot'),
      el('h2', { class: 'inspect__detail-title', tabindex: '-1' }, el('code', {}, idText(id))),
      loading());
    try {
      const detail = await renderDetail(session, id);
      if (mine !== token) return;
      detailBox.lastChild.replaceWith(detail);
    } catch (err) {
      if (mine === token) detailBox.lastChild.replaceWith(errorBox(err, () => showDetail(id)));
    }
  }

  // Arriving on a link to one id: on wide screens bring its highlighted row
  // into view (the detail beside it is sticky, so it stays visible); on
  // narrow ones only the detail is shown, from its top.
  function reveal(id) {
    const tr = rowEls.get(id);
    if (matchMedia('(min-width: 1100px)').matches && tr?.isConnected) {
      const top = grid.getBoundingClientRect().top + window.scrollY - 80;
      const rowTop = tr.getBoundingClientRect().top + window.scrollY;
      // Keep the list's start in view when the row is near it.
      window.scrollTo(0, rowTop - top > window.innerHeight * 0.6 ? rowTop - window.innerHeight / 2 : top);
    } else {
      window.scrollTo(0, detailBox.getBoundingClientRect().top + window.scrollY - 70);
    }
  }

  function snapshotPanel() {
    const snap = session.snapshot ?? {};
    const st = session.state ?? {};
    const col = snap.collection ?? {};
    const red = snap.redaction ?? {};
    const counts = new Map();
    for (const r of rows) counts.set(r.status, (counts.get(r.status) ?? 0) + 1);
    const collectors = col.collectors ?? [];
    // Open on wide screens; on a phone the table matters more, so the
    // metadata starts folded.
    // A link to one id folds it too, so the selected detail is in view.
    return el('details', { class: 'snapshot-panel', open: !selected && matchMedia('(min-width: 1100px)').matches },
      el('summary', { class: 'snapshot-panel__summary' }, 'This snapshot: metadata, counts by status, collector run'),
      el('div', { class: 'snapshot-panel__grid' },
        el('div', { class: 'snapshot-panel__meta' },
          el('h3', { class: 'detail__h' }, 'Snapshot'),
          el('dl', { class: 'facts' },
            fact('Id', el('code', { class: 'snapshot-id', title: snap.id }, snap.id)),
            fact('Schema', `${snap.schema ?? ''} v${snap.schema_version ?? '?'}`),
            fact('Collected by', `Machine Atlas ${snap.app_version ?? '?'}`, st.app_version && st.app_version !== snap.app_version ? ` (shown by ${st.app_version})` : ''),
            fact('Created', timeEl(snap.created_at), el('span', { class: 'caption' }, ` · ${formatAge(snap.created_at)}`)),
            fact('Mode', `${col.mode ?? '?'}${col.fixture ? ` (${col.fixture})` : ''}`, st.mode && st.mode !== col.mode ? `, shown as ${st.mode}` : ''),
            fact('Privileged', col.privileged ? 'yes, collected as root' : 'no'),
            fact('Redaction', red.applied ? `${red.policy}: ${formatCount(red.replaced ?? 0)} replaced${red.classes?.length ? ` (${red.classes.join(', ')})` : ''}` : 'none applied'))),
        el('div', { class: 'snapshot-panel__counts' },
          el('h3', { class: 'detail__h' }, 'By status'),
          el('ul', { class: 'status-counts__list' }, STATUS_ORDER.filter((s) => counts.has(s)).map((s) => {
            const a = el('a', { href: href('inspect', null, { ...filters, status: s }), class: 'status-counts__link' },
              el('span', { class: 'status', 'data-status': s === 'unknown' ? 'absent' : s }, s === 'unknown' ? 'unknown (derived)' : statusText(s)),
              el('span', { class: 'num status-counts__n' }, formatCount(counts.get(s))));
            return el('li', {}, a);
          }))),
        el('div', { class: 'snapshot-panel__run' },
          el('h3', { class: 'detail__h' }, 'Collector run'),
          el('p', { class: 'caption' }, 'From ', timeEl(col.started_at), ' to ', timeEl(col.finished_at), '.'),
          el('details', { class: 'run-report' },
            el('summary', {}, `${collectors.length} collectors: ${summarize(collectors)}`),
            el('div', { class: 'table-wrap' },
              el('table', { class: 'datatable' },
                el('thead', {}, el('tr', {}, el('th', { scope: 'col' }, 'Collector'), el('th', { scope: 'col' }, 'Status'), el('th', { scope: 'col', class: 'num' }, 'Took'), el('th', { scope: 'col', class: 'num' }, 'Ids'), el('th', { scope: 'col' }, 'Error'))),
                el('tbody', {}, collectors.map((c) => el('tr', {},
                  el('td', {}, el('a', { href: href('inspect', null, { origin: c.id }) }, el('code', {}, c.id))),
                  el('td', {}, el('span', { class: 'status', 'data-status': runStatus(c.status) }, c.status)),
                  el('td', { class: 'num' }, `${c.duration_ms ?? 0} ms`),
                  el('td', { class: 'num' }, formatCount(c.observations ?? 0)),
                  el('td', { class: 'caption' }, c.error || ''))))))))));
  }

  return {
    title: (route) => route.id ?? 'Inspect',
    async mount(container, route) {
      filters = filtersFrom(route.params);
      selected = route.id ?? null;
      rowEls = new Map();
      controls = {};
      let catalog = null;
      container.replaceChildren(el('main', { class: 'page inspect', id: 'main', tabindex: '-1' }, loading('Loading the catalog')));
      try {
        catalog = await session.load('catalog');
      } catch (err) {
        // Without the catalog the table still works; titles are missing.
        console.error(err);
      }
      rows = buildRows(session.snapshot?.observations ?? [], session.facts, catalog?.lookup);
      tbody = el('tbody', {});
      countLine = el('p', { class: 'caption inspect__count', role: 'status', 'aria-live': 'polite' });
      detailBox = el('aside', { class: 'inspect__detail', 'aria-label': 'Selected observation' });
      grid = el('div', { class: 'inspect__grid' },
        el('section', { class: 'inspect__list', 'aria-labelledby': 'list-h' },
          el('h2', { class: 'visually-hidden', id: 'list-h' }, 'Observations and derived facts'),
          filterBar(),
          countLine,
          el('div', { class: 'table-wrap inspect__table-wrap' },
            el('table', { class: 'datatable inspect__table' },
              el('thead', {}, el('tr', {},
                el('th', { scope: 'col' }, 'Id and title'), el('th', { scope: 'col' }, 'Value'),
                el('th', { scope: 'col' }, 'Status'), el('th', { scope: 'col' }, 'Kind'))),
              tbody))),
        detailBox);
      container.replaceChildren(el('main', { class: 'page inspect', id: 'main', tabindex: '-1' },
        el('header', { class: 'inspect__head' },
          el('p', { class: 'eyebrow' }, 'Inspect'),
          el('h1', { class: 'lesson__title', tabindex: '-1' }, 'Every reading, with its source'),
          el('p', { class: 'lede' }, 'All observations and derived facts behind the lessons. ', badge('observed'), ' values were read from the machine, live ones at the time shown; ', badge('derived'), ' values were computed from them by the stated rule.')),
        snapshotPanel(),
        grid));
      apply();
      await showDetail(route.id);
      if (route.id) reveal(route.id);
    },
    update(route) {
      const next = filtersFrom(route.params);
      if (JSON.stringify(next) !== JSON.stringify(filters)) {
        filters = next;
        for (const [name, c] of Object.entries(controls)) c.value = filters[name] ?? '';
        apply();
      }
      showDetail(route.id).then(() => {
        if (route.id && !matchMedia('(min-width: 1100px)').matches) window.scrollTo(0, detailBox.offsetTop - 60);
        if (route.id) detailBox.querySelector('.inspect__detail-title')?.focus({ preventScroll: true });
      });
      return true;
    },
    unmount() {},
  };
}

function fact(term, ...value) {
  return el('div', {}, el('dt', {}, term), el('dd', {}, value));
}

function runStatus(s) {
  return { ok: 'ok', partial: 'absent', failed: 'failed', skipped: 'not_collected' }[s] ?? 'absent';
}

function summarize(collectors) {
  const by = new Map();
  for (const c of collectors) by.set(c.status, (by.get(c.status) ?? 0) + 1);
  return [...by].map(([s, n]) => `${n} ${s}`).join(', ') || 'none';
}

// "19:42:10": live readings of one snapshot share a date, so the time of day
// is what tells them apart; the full time is in the title.
function clock(at) {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString().slice(11, 19);
}
