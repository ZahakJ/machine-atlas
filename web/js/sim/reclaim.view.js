// View for the reclaim simulation. mount(container, ctx) builds the controls
// once and redraws the machine after every action; it returns an unmount
// function. All policy lives in reclaim.model.js.

import { el, replace } from '../lib/dom.js';
import { badge } from '../lib/ui.js';
import {
  ASSUMPTIONS, COUNTERS, SCENARIOS, initialState, step, seedFrom,
  frameSummary, scanBalance, scenarioById, scenarioStart, runScenario,
} from './reclaim.model.js';

// Each control answers a question a learner would ask.
const ACTIONS = [
  { action: { type: 'read', pages: 8 }, label: 'Read a file', detail: '8 pages', question: 'What does reading a file leave behind in memory?' },
  { action: { type: 'write', pages: 8 }, label: 'Write a file', detail: '8 pages, dirty', question: 'Why can written data not simply be dropped?' },
  { action: { type: 'alloc', pages: 8 }, label: 'Allocate memory', detail: '8 pages', question: 'Where does a program’s new memory come from?' },
  { action: { type: 'alloc', pages: 24 }, label: 'Allocate a burst', detail: '24 pages', question: 'What if the program allocates faster than kswapd reclaims?' },
  { action: { type: 'touch', kind: 'file', pages: 16 }, label: 'Reread old file pages', detail: 'oldest 16', question: 'What does it cost to need cache that was dropped?' },
  { action: { type: 'touch', kind: 'anon', pages: 16 }, label: 'Touch old memory', detail: 'oldest 16', question: 'What happens when a program uses memory that went to swap?' },
  { action: { type: 'tick' }, label: 'Let time pass', detail: 'writeback', question: 'What changes once dirty data reaches the disk?' },
];

const SWAPPINESS_CHOICES = [
  [0, 'avoid swap while file pages remain'],
  [60, 'kernel default'],
  [100, 'anonymous and file pages cost the same'],
  [200, 'prefer swapping (fast swap such as zram)'],
];

const COUNTER_MEANING = {
  pgscan_kswapd: 'pages kswapd examined',
  pgscan_direct: 'pages examined by stalled tasks',
  pgsteal_kswapd: 'pages kswapd reclaimed',
  pgsteal_direct: 'pages stalled tasks reclaimed',
  pswpout: 'pages written to swap',
  pswpin: 'pages read back from swap',
  workingset_refault_file: 'evicted file pages needed again',
  workingset_refault_anon: 'swapped pages needed again',
  workingset_activate: 'refaults judged part of the working set',
  pgmajfault: 'faults that waited for I/O',
  allocstall: 'times a task entered direct reclaim',
  oom_would_kill: 'times the OOM killer would run (simulated only)',
};

let instance = 0;

/**
 * Mounts the simulation. ctx: {obs(id), derived(id), navigate(hash)}; lookups
 * may return undefined, in which case defaults are used and the view says so.
 */
export function mount(container, ctx = {}) {
  const uid = `rc${++instance}`;
  const seed = seedFrom(ctx.obs, ctx.derived);
  let state = initialState(seed.opts);
  let scenario = null; // {def, next}

  const swapBox = el('input', { type: 'checkbox', id: `${uid}-swap`, checked: state.config.swapPresent, onchange: () => apply({ type: 'swap', present: swapBox.checked }) });
  const swappinessSelect = el('select', { class: 'select', id: `${uid}-swappiness`, onchange: () => apply({ type: 'swappiness', value: Number(swappinessSelect.value) }) });
  const scenarioSelect = el('select', { class: 'select', id: `${uid}-scenario`, onchange: () => startScenario(scenarioSelect.value) },
    el('option', { value: '' }, 'Free play (no script)'),
    SCENARIOS.map((sc) => el('option', { value: sc.id }, sc.title)));
  const scenarioBox = el('div', { class: 'rc__scenario' });
  const machine = el('div', { class: 'rc__machine' });
  const counters = el('div');
  const log = el('ol', { class: 'rc__log' });
  const announcer = el('p', { class: 'visually-hidden', 'aria-live': 'polite' });
  // The latest decision, next to the buttons that caused it (the live
  // region above already announces it, so this copy is not re-read).
  const lastLine = el('p', { class: 'rc__last caption', 'aria-hidden': 'true' });

  const root = el('section', { class: 'sim rc', 'aria-labelledby': `${uid}-title` },
    el('header', { class: 'sim__header' },
      el('div', {},
        el('p', { class: 'eyebrow' }, 'Simulation'),
        el('h3', { class: 'sim__title', id: `${uid}-title` }, 'Reclaim: where does a free frame come from?')),
      badge('simulated')),
    el('p', { class: 'sim__intro prose' },
      'A toy machine with 96 frames. Make a program read, write and allocate, and watch the kernel’s choices: when kswapd wakes, what it takes first, when a task has to stall, and when nothing is left. Nothing here is a measurement of your machine.'),
    el('ul', { class: 'sim__seed' }, seed.notes.map((n) => el('li', { 'data-defaults': n.defaults ? 'true' : null }, n.text))),
    el('section', { class: 'sim__assumptions sim__assumptions--prominent' },
      el('h4', {}, 'What this model leaves out'),
      el('ul', {}, ASSUMPTIONS.map((a) => el('li', {}, a)))),
    el('div', { class: 'rc__layout' },
      el('div', { class: 'rc__main' }, machine),
      el('div', { class: 'rc__side' },
        // On narrow screens this panel moves above the frames (CSS order),
        // so what an action changes is in view when it is pressed.
        el('section', { class: 'sim__panel rc__actions-panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Make the program…')),
          el('div', { class: 'rc__actions' }, ACTIONS.map((a) => el('button', {
            class: 'btn rc__action', type: 'button', title: a.question,
            onclick: () => apply(a.action),
          }, el('span', {}, a.label), el('span', { class: 'rc__action-detail' }, a.detail)))),
          lastLine),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Change the machine')),
          el('label', { class: 'check', for: `${uid}-swap` }, swapBox, 'Swap area present'),
          el('p', { class: 'field__hint' }, 'Without swap, can anonymous memory be reclaimed at all?'),
          el('div', { class: 'field' },
            el('label', { class: 'field__label', for: `${uid}-swappiness` }, 'Swappiness'),
            swappinessSelect,
            el('p', { class: 'field__hint' }, 'How expensive does the kernel consider swapping compared with dropping cache?'))),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Scenarios')),
          el('div', { class: 'field' },
            el('label', { class: 'field__label', for: `${uid}-scenario` }, 'Run a scripted scenario'),
            scenarioSelect),
          scenarioBox),
        el('div', { class: 'cluster' },
          el('button', { class: 'btn btn--quiet', type: 'button', onclick: reset }, 'Reset the machine')))),
    el('div', { class: 'rc__bottom' },
      el('section', { class: 'sim__panel' },
        el('div', { class: 'sim__panel-head' }, el('h4', {}, 'What the kernel decided, and why'), badge('simulated')),
        log),
      el('section', { class: 'sim__panel' },
        el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Simulated counters'), badge('simulated')),
        el('p', { class: 'caption' }, 'Named after the /proc/vmstat fields that count the same events on a real kernel, so you know where to look. These count events in this toy only.'),
        counters)),
    announcer);

  function apply(action) {
    const before = state.events.length;
    state = step(state, action);
    announce(state.events.length - before);
    render();
  }

  function announce(n) {
    const last = state.events[state.events.length - 1];
    replace(announcer, last ? `Step ${state.step}: ${n} event${n === 1 ? '' : 's'}. ${last.text}` : '');
  }

  function reset() {
    scenario = null;
    scenarioSelect.value = '';
    state = initialState(seed.opts);
    replace(announcer, 'The machine was reset.');
    render();
  }

  function startScenario(id) {
    const def = scenarioById(id);
    scenario = def ? { def, next: 0 } : null;
    state = def ? scenarioStart(def, seed.opts) : initialState(seed.opts);
    render();
  }

  function runNext() {
    const { def } = scenario;
    apply(def.steps[scenario.next].action);
    scenario.next++;
    render();
  }

  function runAll() {
    while (scenario.next < scenario.def.steps.length) {
      state = step(state, scenario.def.steps[scenario.next].action);
      scenario.next++;
    }
    replace(announcer, `Scenario finished after ${state.step} steps.`);
    render();
  }

  function render() {
    swapBox.checked = state.config.swapPresent;
    renderSwappiness();
    renderScenario();
    replace(machine, renderMachine(state));
    replace(counters, renderCounters(state));
    // Newest step first, but each step's events in the order they happened,
    // so a cause is always read before its effect.
    const newestFirst = state.events.slice().sort((a, b) => b.step - a.step);
    replace(log, newestFirst.map((e) => el('li', { class: 'rc__event', 'data-actor': e.actor, 'data-kind': e.kind },
      el('span', { class: 'rc__event-meta' }, el('span', { class: 'rc__event-step num' }, `step ${e.step}`), el('span', { class: 'rc__actor' }, e.actor)),
      el('span', { class: 'rc__event-text' }, e.text))));
    if (state.events.length === 0) log.appendChild(el('li', { class: 'empty' }, 'Nothing has happened yet. Choose an action or a scenario.'));
    const last = state.events[state.events.length - 1];
    replace(lastLine, last ? [el('strong', {}, `Last, step ${last.step} (${last.actor}): `), last.text] : 'Nothing has happened yet.');
  }

  function renderSwappiness() {
    const choices = SWAPPINESS_CHOICES.slice();
    const v = state.config.swappiness;
    if (!choices.some(([n]) => n === v)) choices.push([v, 'current value']);
    choices.sort((a, b) => a[0] - b[0]);
    replace(swappinessSelect, choices.map(([n, why]) => el('option', { value: String(n), selected: n === v }, `${n} — ${why}`)));
  }

  function renderScenario() {
    if (!scenario) {
      replace(scenarioBox, el('p', { class: 'field__hint' }, 'Each scenario sets up the machine, then runs a short script one step at a time so you can predict each step first.'));
      return;
    }
    const { def, next } = scenario;
    const done = next >= def.steps.length;
    const compare = done && def.compare ? scenarioById(def.compare) : null;
    replace(scenarioBox,
      el('p', { class: 'rc__question' }, def.question),
      el('p', { class: 'caption' }, done
        ? `Finished all ${def.steps.length} steps.`
        : `Step ${next + 1} of ${def.steps.length}: ${def.steps[next].note}`),
      el('div', { class: 'cluster' },
        el('button', { class: 'btn btn--primary', type: 'button', disabled: done, onclick: runNext }, 'Run next step'),
        el('button', { class: 'btn', type: 'button', disabled: done, onclick: runAll }, 'Run to the end'),
        el('button', { class: 'btn btn--quiet', type: 'button', onclick: () => startScenario(def.id) }, 'Restart')),
      done ? el('p', { class: 'rc__expect' }, el('strong', {}, 'What to look for: '), def.expect) : null,
      compare ? renderComparison(def, compare, state, runScenario(compare, seed.opts)) : null);
  }

  render();
  container.appendChild(root);
  return () => root.remove();
}

// ---------------------------------------------------------------- pieces

function renderMachine(s) {
  const sum = frameSummary(s);
  const free = sum.free;
  const { min, low, high } = s.config.watermarks;
  const zone = free <= min ? 'min' : free < low ? 'low' : free < high ? 'high' : 'ok';
  const zoneText = {
    min: 'At or below min: allocations must reclaim directly and stall.',
    low: 'Below low: kswapd has been woken.',
    high: 'Between low and high: kswapd reclaims up to high once woken.',
    ok: 'At or above high: no reclaim needed.',
  }[zone];

  const grid = el('div', { class: 'frames', role: 'img', 'aria-label': `${s.config.frames} frames: ${sum.free} free, ${sum.fileClean} clean file, ${sum.fileDirty} dirty file, ${sum.anon} anonymous` },
    s.frames.map((id, i) => {
      const p = id === null ? null : s.pages[id];
      const kind = !p ? 'free' : p.type === 'anon' ? 'anon' : p.dirty ? 'dirty' : 'file';
      return el('span', { class: 'frame', 'data-kind': kind, 'data-list': p?.list ?? null, title: p ? `frame ${i}: ${p.id}, ${kind}, ${p.list}` : `frame ${i}: free` },
        { free: '', file: 'F', dirty: 'D', anon: 'A' }[kind]);
    }));

  const gauge = el('div', { class: 'gauge', 'data-zone': zone });
  const fill = el('span', { class: 'gauge__fill' });
  fill.style.setProperty('--value', String(free / s.config.frames));
  gauge.appendChild(fill);
  for (const [name, v] of [['min', min], ['low', low], ['high', high]]) {
    const mark = el('span', { class: 'gauge__mark', 'data-mark': name }, el('span', { class: 'gauge__label' }, `${name} ${v}`));
    mark.style.setProperty('--at', String(v / s.config.frames));
    gauge.appendChild(mark);
  }

  const bal = scanBalance(s);
  const balanceText = {
    'no-swap': 'Reclaim scans file pages only: there is no swap.',
    'swap-full': 'Reclaim scans file pages only: swap is full.',
    'file-tiny': 'Reclaim scans anonymous pages only: file pages are nearly gone.',
    'swappiness-0': 'Reclaim scans file pages only: swappiness is 0 and file pages remain.',
    proportional: `Reclaim splits scanning ${bal.anon} : ${bal.file} between anonymous and file pages.`,
  }[bal.reason];

  return [
    el('section', { class: 'sim__panel' },
      el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Physical memory'), el('span', { class: 'rc__kswapd', 'data-state': s.kswapd }, `kswapd ${s.kswapd}`)),
      grid,
      el('ul', { class: 'legend' },
        legendItem('free', 'Free', sum.free),
        legendItem('file', 'File, clean', sum.fileClean),
        legendItem('dirty', 'File, dirty', sum.fileDirty),
        legendItem('anon', 'Anonymous', sum.anon)),
      el('div', { class: 'rc__watermarks' },
        el('p', { class: 'rc__free' }, el('strong', { class: 'num' }, `${free} free`), ` of ${s.config.frames}. ${zoneText}`),
        gauge)),
    el('section', { class: 'sim__panel' },
      el('div', { class: 'sim__panel-head' }, el('h4', {}, 'LRU lists'), el('span', { class: 'caption' }, 'oldest first: reclaim takes from the left')),
      el('div', { class: 'lru' },
        lruRow(s, 'anon', 'active', 'Anonymous, active'),
        lruRow(s, 'anon', 'inactive', 'Anonymous, inactive'),
        lruRow(s, 'file', 'active', 'File, active'),
        lruRow(s, 'file', 'inactive', 'File, inactive')),
      el('p', { class: 'caption' }, balanceText)),
    el('section', { class: 'sim__panel' },
      el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Swap area'),
        el('span', { class: 'caption num' }, s.config.swapPresent ? `${s.swap.filter((x) => x !== null).length} of ${s.config.swapSlots} slots used` : 'none')),
      s.config.swapPresent
        ? el('div', { class: 'frames frames--swap', role: 'img', 'aria-label': `${s.swap.filter((x) => x !== null).length} of ${s.config.swapSlots} swap slots in use` },
          s.swap.map((id) => el('span', { class: 'frame', 'data-kind': id ? 'anon' : 'free', title: id ?? 'free slot' }, id ? 'A' : '')))
        : el('p', { class: 'empty' }, 'No swap area: anonymous pages have nowhere to go.')),
  ];
}

function legendItem(kind, label, n) {
  return el('li', {}, el('span', { class: 'frame frame--key', 'data-kind': kind, 'aria-hidden': 'true' }, { free: '', file: 'F', dirty: 'D', anon: 'A' }[kind]), `${label} `, el('strong', { class: 'num' }, String(n)));
}

function lruRow(s, type, list, label) {
  const ids = s.lru[type][list];
  return el('div', { class: 'lru__row' },
    el('span', { class: 'lru__label' }, label, ' ', el('strong', { class: 'num' }, String(ids.length))),
    el('span', { class: 'lru__pages', 'aria-hidden': 'true' }, ids.map((id) => {
      const p = s.pages[id];
      return el('span', { class: 'lru__page', 'data-kind': type === 'anon' ? 'anon' : p.dirty ? 'dirty' : 'file', 'data-ref': p.referenced ? 'true' : null, title: `${id}${p.referenced ? ', referenced' : ''}` });
    })));
}

function renderCounters(s) {
  return el('div', { class: 'table-wrap' }, el('table', { class: 'datatable' },
    el('thead', {}, el('tr', {}, el('th', {}, 'Counter'), el('th', { class: 'num' }, 'Count'), el('th', {}, 'Counts'))),
    el('tbody', {}, COUNTERS.map((c) => el('tr', {},
      el('td', {}, el('code', {}, c)),
      el('td', { class: 'num' }, String(s.counters[c])),
      el('td', { class: 'caption' }, COUNTER_MEANING[c]))))));
}

function renderComparison(def, other, mine, theirs) {
  const rows = ['pswpout', 'pswpin', 'pgsteal_kswapd', 'pgsteal_direct', 'allocstall', 'pgmajfault'];
  return el('div', { class: 'rc__compare' },
    el('p', { class: 'caption' }, `The same script with ${other.title.toLowerCase()} instead:`),
    el('div', { class: 'table-wrap' }, el('table', { class: 'datatable' },
      el('thead', {}, el('tr', {}, el('th', {}, 'Counter'), el('th', { class: 'num' }, def.title), el('th', { class: 'num' }, other.title))),
      el('tbody', {}, rows.map((c) => el('tr', {},
        el('td', {}, el('code', {}, c)),
        el('td', { class: 'num' }, String(mine.counters[c])),
        el('td', { class: 'num' }, String(theirs.counters[c]))))))));
}
