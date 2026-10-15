// View for the fair-share simulation. mount(container, ctx) builds the
// controls once and redraws after every action; it returns an unmount
// function. All policy lives in fair-share.model.js.

import { el, svg, replace } from '../lib/dom.js';
import { badge } from '../lib/ui.js';
import {
  ASSUMPTIONS, SCENARIOS, initialState, step, seedFrom, shares, avgVruntime,
  pickNext, sliceFor, scenarioById, scenarioStart,
} from './fair-share.model.js';

const NICE_CHOICES = [-10, -5, 0, 5, 10, 19];
const SLICE_CHOICES = [0.75, 1.5, 3, 6, 12];

let instance = 0;

const r2 = (v) => (Math.round(v * 100) / 100).toFixed(2);
const pct = (v) => `${(Math.round(v * 1000) / 10).toFixed(1)}%`;

/**
 * Mounts the simulation. ctx: {obs(id), derived(id)}; lookups may return
 * undefined, in which case defaults are used and the view says so.
 */
export function mount(container, ctx = {}) {
  const uid = `fs${++instance}`;
  const seed = seedFrom(ctx.obs, ctx.derived);
  let state = initialState(seed.opts);
  let scenario = null;
  const slots = new Map(); // task id → colour slot, stable while the task lives

  const algoSelect = el('select', { class: 'select', id: `${uid}-algo`, onchange: () => apply({ type: 'algorithm', value: algoSelect.value }) },
    el('option', { value: 'eevdf' }, 'EEVDF (Linux 6.6 and later)'),
    el('option', { value: 'cfs' }, 'CFS (before 6.6)'));
  const sliceSelect = el('select', { class: 'select', id: `${uid}-slice`, onchange: () => apply({ type: 'slice', ms: Number(sliceSelect.value) }) });
  const addSelect = el('select', { class: 'select', id: `${uid}-add`, 'aria-label': 'Nice value of the new task' },
    NICE_CHOICES.map((n) => el('option', { value: String(n), selected: n === 0 }, `nice ${n}`)));
  const scenarioSelect = el('select', { class: 'select', id: `${uid}-scenario`, onchange: () => startScenario(scenarioSelect.value) },
    el('option', { value: '' }, 'Free play (no script)'),
    SCENARIOS.map((sc) => el('option', { value: sc.id }, sc.title)));
  const scenarioBox = el('div', { class: 'rc__scenario' });
  const queue = el('div');
  const timeline = el('div');
  const vline = el('div');
  const next = el('p', { class: 'fs__next' });
  const log = el('ol', { class: 'rc__log' });
  const announcer = el('p', { class: 'visually-hidden', 'aria-live': 'polite' });

  const root = el('section', { class: 'sim fs', 'aria-labelledby': `${uid}-title` },
    el('header', { class: 'sim__header' },
      el('div', {},
        el('p', { class: 'eyebrow' }, 'Simulation'),
        el('h3', { class: 'sim__title', id: `${uid}-title` }, 'Fair share: who runs next, and for how long?')),
      badge('simulated')),
    el('p', { class: 'sim__intro prose' },
      'A few tasks that always want the CPU share one CPU. Each has a nice value and so a weight. Step the scheduler and watch each task’s virtual runtime advance at 1024 ÷ weight per millisecond, which task is picked and why, and what share of the time each one ends up with. Nothing here is a measurement of your machine.'),
    el('ul', { class: 'sim__seed' }, seed.notes.map((n) => el('li', { 'data-defaults': n.defaults ? 'true' : null }, n.text))),
    el('section', { class: 'sim__assumptions sim__assumptions--prominent' },
      el('h4', {}, 'What this model leaves out'),
      el('ul', {}, ASSUMPTIONS.map((a) => el('li', {}, a)))),
    el('div', { class: 'rc__layout' },
      el('div', { class: 'rc__machine' },
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Run queue'), badge('simulated')),
          queue, next),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Virtual time'), el('span', { class: 'caption' }, 'each task’s vruntime, and V')),
          vline),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Who ran, in order'), el('span', { class: 'caption' }, 'latest slices, oldest left')),
          timeline)),
      el('div', { class: 'rc__side' },
        el('section', { class: 'sim__panel rc__actions-panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Run the scheduler')),
          el('div', { class: 'rc__actions' },
            el('button', { class: 'btn rc__action', type: 'button', onclick: () => apply({ type: 'step' }) },
              el('span', {}, 'One decision'), el('span', { class: 'rc__action-detail' }, 'pick a task, run its slice')),
            el('button', { class: 'btn rc__action', type: 'button', onclick: () => apply({ type: 'run', ms: 30 }) },
              el('span', {}, 'Run 30 ms'), el('span', { class: 'rc__action-detail' }, 'several decisions')),
            el('button', { class: 'btn rc__action', type: 'button', onclick: () => apply({ type: 'run', ms: 300 }) },
              el('span', {}, 'Run 300 ms'), el('span', { class: 'rc__action-detail' }, 'watch the shares settle'))),
          el('div', { class: 'cluster fs__add' },
            addSelect,
            el('button', { class: 'btn', type: 'button', onclick: () => apply({ type: 'add', nice: Number(addSelect.value) }) }, 'Add a task'))),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Change the scheduler')),
          el('div', { class: 'field' },
            el('label', { class: 'field__label', for: `${uid}-algo` }, 'Picker'), algoSelect,
            el('p', { class: 'field__hint' }, 'Does the smallest vruntime always run next?')),
          el('div', { class: 'field' },
            el('label', { class: 'field__label', for: `${uid}-slice` }, 'Base slice (EEVDF)'), sliceSelect,
            el('p', { class: 'field__hint' }, 'What does a shorter slice buy, and what does it cost?'))),
        el('section', { class: 'sim__panel' },
          el('div', { class: 'sim__panel-head' }, el('h4', {}, 'Scenarios')),
          el('div', { class: 'field' },
            el('label', { class: 'field__label', for: `${uid}-scenario` }, 'Run a scripted scenario'),
            scenarioSelect),
          scenarioBox),
        el('div', { class: 'cluster' },
          el('button', { class: 'btn btn--quiet', type: 'button', onclick: reset }, 'Reset')))),
    el('section', { class: 'sim__panel' },
      el('div', { class: 'sim__panel-head' }, el('h4', {}, 'What the scheduler decided, and why'), badge('simulated')),
      log),
    announcer);

  function slotOf(id) {
    if (!slots.has(id)) {
      const used = new Set(slots.values());
      let k = 0;
      while (used.has(k)) k++;
      slots.set(id, k % 6);
    }
    return slots.get(id);
  }

  function apply(action) {
    const before = state.events.length;
    state = step(state, action);
    for (const id of [...slots.keys()]) if (!state.tasks.some((t) => t.id === id)) slots.delete(id);
    const last = state.events[state.events.length - 1];
    replace(announcer, state.events.length > before && last ? last.text : '');
    render();
  }

  function reset() {
    scenario = null;
    scenarioSelect.value = '';
    slots.clear();
    state = initialState(seed.opts);
    replace(announcer, 'The run queue was reset.');
    render();
  }

  function startScenario(id) {
    const def = scenarioById(id);
    scenario = def ? { def, next: 0 } : null;
    slots.clear();
    state = def ? scenarioStart(def, { algorithm: state.config.algorithm, sliceMs: seed.opts.sliceMs }) : initialState(seed.opts);
    render();
  }

  function runNext() {
    apply(scenario.def.steps[scenario.next].action);
    scenario.next++;
    render();
  }

  function render() {
    algoSelect.value = state.config.algorithm;
    const choices = SLICE_CHOICES.includes(state.config.sliceMs) ? SLICE_CHOICES : [...SLICE_CHOICES, state.config.sliceMs].sort((a, b) => a - b);
    replace(sliceSelect, choices.map((ms) => el('option', { value: String(ms), selected: ms === state.config.sliceMs }, `${ms} ms`)));
    sliceSelect.disabled = state.config.algorithm !== 'eevdf';
    replace(queue, renderQueue(state, slotOf, apply));
    const { task, reason } = pickNext(state);
    replace(next, task
      ? [el('strong', {}, `Next: ${task.name}, for ${r2(sliceFor(state, task))} ms. `), reason]
      : 'No task is runnable: the CPU would idle.');
    replace(vline, renderVirtual(state, slotOf));
    replace(timeline, renderTimeline(state, slotOf));
    renderScenario();
    const newest = state.events.slice().reverse();
    replace(log, newest.map((e) => el('li', { class: 'rc__event', 'data-kind': e.kind },
      el('span', { class: 'rc__event-meta' }, el('span', { class: 'rc__event-step num' }, `${r2(e.time)} ms`)),
      el('span', { class: 'rc__event-text' }, e.text))));
    if (!state.events.length) log.append(el('li', { class: 'empty' }, 'Nothing has run yet. Take one decision, or pick a scenario.'));
  }

  function renderScenario() {
    if (!scenario) {
      replace(scenarioBox, el('p', { class: 'field__hint' }, 'Each scenario sets up a run queue and runs a short script, one step at a time, so you can predict each step first.'));
      return;
    }
    const { def, next: n } = scenario;
    const done = n >= def.steps.length;
    replace(scenarioBox,
      el('p', { class: 'rc__question' }, def.question),
      el('p', { class: 'caption' }, done ? `Finished all ${def.steps.length} steps.` : `Step ${n + 1} of ${def.steps.length}: ${def.steps[n].note}`),
      el('div', { class: 'cluster' },
        el('button', { class: 'btn btn--primary', type: 'button', disabled: done, onclick: runNext }, 'Run next step'),
        el('button', { class: 'btn btn--quiet', type: 'button', onclick: () => startScenario(def.id) }, 'Restart')),
      done ? el('p', { class: 'rc__expect' }, el('strong', {}, 'What to look for: '), def.expect) : null);
  }

  render();
  container.appendChild(root);
  return () => root.remove();
}

// ---------------------------------------------------------------- pieces

function renderQueue(s, slotOf, apply) {
  const eevdf = s.config.algorithm === 'eevdf';
  const rows = shares(s);
  const head = ['Task', 'Nice', 'Weight', 'vruntime', eevdf ? 'Deadline' : null, eevdf ? 'Lag (ms)' : null, eevdf ? 'Eligible' : null, 'CPU so far', 'Entitled', ''].filter((h) => h !== null);
  return el('div', { class: 'table-wrap' }, el('table', { class: 'datatable fs__queue' },
    el('caption', { class: 'visually-hidden' }, 'Tasks on the run queue'),
    el('thead', {}, el('tr', {}, head.map((h, i) => el('th', { scope: 'col', class: i > 0 && i < head.length - 1 ? 'num' : null }, h)))),
    el('tbody', {}, rows.map((r) => {
      const t = s.tasks.find((x) => x.id === r.id);
      const niceSel = el('select', { class: 'select select--compact', 'aria-label': `Nice value of ${r.name}`, onchange: () => apply({ type: 'nice', id: r.id, nice: Number(niceSel.value) }) },
        [...new Set([...NICE_CHOICES, r.nice])].sort((a, b) => a - b).map((n) => el('option', { value: String(n), selected: n === r.nice }, String(n))));
      return el('tr', { 'aria-current': s.current === r.id ? 'true' : null, 'data-sleeping': r.sleeping ? 'true' : null },
        el('th', { scope: 'row' }, el('span', { class: 'fs__chip', 'data-slot': slotOf(r.id) }, r.name), r.sleeping ? el('span', { class: 'caption' }, ' asleep') : null),
        el('td', { class: 'num' }, niceSel),
        el('td', { class: 'num' }, String(r.weight)),
        el('td', { class: 'num' }, r2(t.vruntime)),
        eevdf ? el('td', { class: 'num' }, r.sleeping ? '—' : r2(t.deadline)) : null,
        eevdf ? el('td', { class: 'num' }, r.sleeping ? '—' : r2(r.lag)) : null,
        eevdf ? el('td', { class: 'num' }, r.sleeping ? '—' : r.eligible ? 'yes' : 'no') : null,
        el('td', { class: 'num' }, s.time ? pct(r.share) : '—'),
        el('td', { class: 'num' }, r.sleeping ? '0%' : pct(r.entitled)),
        el('td', {}, el('div', { class: 'cluster fs__row-actions' },
          el('button', { class: 'btn btn--quiet btn--small', type: 'button', onclick: () => apply({ type: r.sleeping ? 'wake' : 'sleep', id: r.id }) }, r.sleeping ? 'Wake' : 'Sleep'),
          el('button', { class: 'btn btn--quiet btn--small', type: 'button', onclick: () => apply({ type: 'remove', id: r.id }), 'aria-label': `Remove ${r.name}` }, 'Exit'))));
    }))),
  el('p', { class: 'caption' }, `Entitled is weight ÷ the sum of runnable weights: ${weightsLine(s)}. CPU so far is the share of the ${r2(s.time)} ms that has passed; it approaches the entitlement as the tasks keep running.`));
}

function weightsLine(s) {
  const r = s.tasks.filter((t) => !t.sleeping);
  if (!r.length) return 'no task is runnable';
  return `${r.map((t) => t.weight).join(' + ')} = ${r.reduce((n, t) => n + t.weight, 0)}`;
}

// Each task on one row of a common virtual-time axis: its vruntime as a
// dot, its virtual deadline as the end of a bar (EEVDF), V as a line.
function renderVirtual(s, slotOf) {
  const eevdf = s.config.algorithm === 'eevdf';
  const tasks = s.tasks;
  if (!tasks.length) return el('p', { class: 'empty' }, 'No tasks.');
  const V = avgVruntime(s);
  const lo = Math.min(V, ...tasks.map((t) => t.vruntime));
  const hi = Math.max(V, ...tasks.map((t) => (eevdf ? t.deadline : t.vruntime)));
  const span = Math.max(hi - lo, 1e-6);
  const W = 600;
  const pad = 40;
  const rowH = 26;
  const H = tasks.length * rowH + 30;
  const x = (v) => pad + ((v - lo) / span) * (W - 2 * pad);
  const nodes = [];
  tasks.forEach((t, i) => {
    const y = 14 + i * rowH;
    nodes.push(svg('text', { x: 4, y: y + 4, class: 'fs__axis-label' }, t.name));
    nodes.push(svg('line', { x1: pad, y1: y, x2: W - pad, y2: y, class: 'fs__track' }));
    if (eevdf && !t.sleeping) nodes.push(svg('line', { x1: x(t.vruntime), y1: y, x2: x(t.deadline), y2: y, class: 'fs__deadline', 'data-slot': slotOf(t.id) }));
    nodes.push(svg('circle', { cx: x(t.vruntime), cy: y, r: 5, class: t.sleeping ? 'fs__dot fs__dot--asleep' : 'fs__dot', 'data-slot': slotOf(t.id) }));
  });
  const vx = x(V);
  nodes.push(svg('line', { x1: vx, y1: 2, x2: vx, y2: H - 18, class: 'fs__v' }));
  nodes.push(svg('text', { x: vx, y: H - 4, class: 'fs__axis-label', 'text-anchor': 'middle' }, eevdf ? `V ${r2(V)}` : `min_vruntime ${r2(s.minVruntime)}`));
  return [
    svg('svg', { class: 'fs__vt', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `Virtual runtimes: ${tasks.map((t) => `${t.name} ${r2(t.vruntime)}`).join(', ')}; V ${r2(V)}.` }, nodes),
    el('p', { class: 'caption' }, eevdf
      ? 'A dot is a task’s vruntime; its bar ends at its virtual deadline. Tasks left of the V line are owed time and eligible; among them the earliest deadline runs. A heavy task’s dot moves slowly, because each millisecond adds only 1024 ÷ weight.'
      : 'A dot is a task’s vruntime. CFS always runs the leftmost one; it moves right by 1024 ÷ weight per millisecond run, so a heavy task gets back to the left sooner.'),
  ];
}

function renderTimeline(s, slotOf) {
  if (!s.history.length) return el('p', { class: 'empty' }, 'Nothing has run yet.');
  const total = s.history.reduce((n, h) => n + h.ms, 0);
  const names = new Map(s.tasks.map((t) => [t.id, t.name]));
  const strip = el('div', { class: 'fs__strip', role: 'img', 'aria-label': `The last ${s.history.length} slices in order: ${s.history.map((h) => names.get(h.id) ?? 'exited').join(', ')}.` },
    s.history.map((h) => {
      const seg = el('span', { class: 'fs__seg', 'data-slot': names.has(h.id) ? slotOf(h.id) : null, title: `${names.get(h.id) ?? 'exited task'}: ${r2(h.ms)} ms from ${r2(h.start)} ms` }, h.ms / total > 0.04 ? (names.get(h.id) ?? '·') : '');
      seg.style.setProperty('--w', String(h.ms / total));
      return seg;
    }));
  return [strip, el('p', { class: 'caption' }, `The last ${s.history.length} slices, ${r2(total)} ms in all; each segment's width is the time that task ran.`)];
}
