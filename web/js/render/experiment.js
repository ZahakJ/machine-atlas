// An experiment card (ARCHITECTURE.md §8): what the experiment asks and does,
// the most it will use, what it will not do, the outcomes it can have, an
// explicit acknowledgement, and after a run the timestamped measurements
// with the matched outcome highlighted and the alternatives still listed.

import { el } from '../lib/dom.js';
import { formatBytes, formatCount } from '../lib/format.js';
import { api } from '../api.js';
import { badge, errorBox, formatTime } from './common.js';

let instance = 0;

/**
 * info: an entry of GET /api/experiments, or undefined when the server does
 * not know the id. live: whether the active snapshot is this machine now —
 * experiments measure the running kernel, so they are refused otherwise.
 */
export function renderExperiment(id, info, { live, announce }) {
  const uid = `exp${++instance}`;
  if (!info) {
    return el('section', { class: 'experiment', 'data-unavailable': 'true' },
      el('p', { class: 'experiment__kind' }, 'Experiment'),
      el('p', { class: 'caption' }, 'This version of Machine Atlas does not provide the experiment ', el('code', {}, id), '.'));
  }

  const r = info.resources ?? {};
  const result = el('div', { class: 'experiment__result', 'aria-live': 'polite', id: `${uid}-result` });
  const ack = el('input', { type: 'checkbox', id: `${uid}-ack` });
  const run = el('button', { class: 'btn btn--primary', type: 'button', disabled: true, 'aria-describedby': `${uid}-why` }, 'Run experiment');
  const why = el('p', { class: 'caption', id: `${uid}-why` });

  let blocked = '';
  if (!info.available) blocked = info.unavailable_reason || 'This experiment cannot run here.';
  else if (!live) blocked = 'Experiments measure the running machine, so they only run when Machine Atlas shows live data. Return to live to run it.';
  why.textContent = blocked;

  ack.addEventListener('change', () => { run.disabled = !ack.checked || !!blocked; });
  ack.disabled = !!blocked;

  run.addEventListener('click', async () => {
    run.disabled = true;
    ack.disabled = true;
    run.textContent = 'Running…';
    result.replaceChildren(el('p', { class: 'loading', role: 'status' }, el('span', { class: 'loading__dot', 'aria-hidden': 'true' }), `Running ${info.title}; this takes at most ${r.time_bound_seconds ?? '?'} s.`));
    try {
      const res = await api.runExperiment(info.id);
      result.replaceChildren(renderResult(res, info));
      announce?.(`Experiment finished: ${outcomeTitle(info, res.outcome)}`);
    } catch (err) {
      result.replaceChildren(errorBox(err, null, 'The experiment did not complete'));
      announce?.('The experiment did not complete');
    } finally {
      run.textContent = 'Run again';
      ack.disabled = false;
      run.disabled = !ack.checked;
    }
  });

  return el('section', { class: 'experiment', 'aria-labelledby': `${uid}-title` },
    el('p', { class: 'experiment__kind' }, 'Experiment · opt-in'),
    el('h3', { class: 'experiment__title', id: `${uid}-title` }, info.title),
    info.question ? el('p', { class: 'prose' }, info.question) : null,
    el('p', { class: 'detail__label' }, 'What it does'),
    el('ol', { class: 'experiment__steps prose' }, (info.steps ?? []).map((s) => el('li', {}, s))),
    el('dl', { class: 'experiment__resources' },
      el('div', {}, el('dt', {}, 'Memory'), el('dd', {}, r.memory_bytes ? `at most ${formatBytes(r.memory_bytes)}` : 'none')),
      el('div', {}, el('dt', {}, 'Disk'), el('dd', {}, r.disk_bytes ? `at most ${formatBytes(r.disk_bytes)}, deleted afterwards` : 'none')),
      el('div', {}, el('dt', {}, 'Time'), el('dd', {}, r.time_bound_seconds ? `stopped after ${r.time_bound_seconds} s` : '—'))),
    (info.does_not ?? []).length
      ? el('div', { class: 'experiment__not' }, el('p', {}, 'What it does not do:'), el('ul', {}, info.does_not.map((d) => el('li', {}, d))))
      : null,
    outcomesList(info, null),
    el('label', { class: 'experiment__ack', for: `${uid}-ack` }, ack, ' I have read what this does and want to run it on this machine.'),
    el('div', { class: 'cluster' }, run),
    blocked ? why : null,
    result);
}

function outcomeTitle(info, outcomeId) {
  return (info.outcomes ?? []).find((o) => o.id === outcomeId)?.title ?? outcomeId ?? 'no outcome matched';
}

function outcomesList(info, matched) {
  const outcomes = info.outcomes ?? [];
  if (outcomes.length === 0) return null;
  const items = outcomes.map((o) => el('li', { class: 'outcome', 'data-matched': o.id === matched ? 'true' : null },
    el('p', { class: 'outcome__title' }, o.id === matched ? el('span', { class: 'outcome__flag' }, 'Matched · ') : null, o.title),
    el('p', { class: 'outcome__text' }, o.interpretation)));
  if (matched === null) {
    return el('details', { class: 'experiment__outcomes' },
      el('summary', {}, `The ${outcomes.length} outcomes it can have`),
      el('ul', { class: 'outcome-list' }, items));
  }
  return el('ul', { class: 'outcome-list' }, items);
}

function renderResult(res, info) {
  const matched = (info.outcomes ?? []).find((o) => o.id === res.outcome);
  const measurements = res.measurements ?? [];
  return el('div', { class: 'stack' },
    el('div', { class: 'cluster' }, badge('observed'), el('span', { class: 'tag', 'data-kind': 'live' }, 'live measurement'),
      el('span', { class: 'caption' }, 'Run ', formatTime(res.started_at), ' → ', formatTime(res.finished_at))),
    el('div', { class: 'table-wrap' },
      el('table', { class: 'datatable' },
        el('caption', {}, 'What the run measured'),
        el('thead', {}, el('tr', {}, el('th', { scope: 'col' }, 'Step'), el('th', { scope: 'col' }, 'Measurement and time'), el('th', { scope: 'col', class: 'num' }, 'Value'))),
        el('tbody', {}, measurements.map((m) => el('tr', {},
          el('td', { class: 'experiment__step' }, m.step),
          // The time sits under the label, so the table fits a lesson
          // column without a fourth column to scroll to.
          el('td', {}, m.label || m.id, el('br'), el('time', { class: 'mono experiment__at', datetime: m.at ?? null }, clock(m.at))),
          el('td', { class: 'num' }, measurementValue(m))))))),
    el('p', { class: 'experiment__verdict' }, matched
      ? ['This run matched: ', el('strong', {}, matched.title), '. Outcomes are chosen by a fixed rule from the numbers above; the alternatives stay listed because another run, or another machine, can land on them.']
      : 'No listed outcome matched these measurements.'),
    outcomesList(info, res.outcome),
    (res.notes ?? []).length ? el('ul', { class: 'experiment__notes caption' }, res.notes.map((n) => el('li', {}, n))) : null);
}

function measurementValue(m) {
  const v = m.value;
  if (typeof v === 'boolean') return v ? 'yes' : 'no';
  if (typeof v !== 'number') return String(v ?? '—');
  if (m.unit === 'bytes') return el('span', { title: `${formatCount(v)} bytes` }, formatBytes(v));
  const n = Number.isInteger(v) ? formatCount(v) : String(Number(v.toPrecision(4)));
  return m.unit && m.unit !== 'count' ? `${n} ${m.unit}` : n;
}

// "12:00:03.142 UTC": runs last seconds, so the time of day with
// milliseconds is what tells the steps apart.
function clock(at) {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at ?? '';
  return d.toISOString().slice(11, 23) + ' UTC';
}
