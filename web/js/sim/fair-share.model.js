// Pure model of weighted fair sharing of one CPU, for the fair-share
// simulation (ARCHITECTURE.md §9).
//
// A few always-runnable (or sleeping) tasks share one CPU. Each has a nice
// value and so a weight from the kernel's sched_prio_to_weight table. Two
// pickers are modelled:
//
//   eevdf  Linux 6.6 and later. V is the weighted average of the runnable
//          tasks' vruntimes. A task is eligible when its vruntime is at or
//          below V (its lag, w × (V − v), is not negative). Among eligible
//          tasks the one with the earliest virtual deadline runs, for one
//          slice; its deadline is v + slice × 1024 / w.
//   cfs    Before 6.6. The task with the smallest vruntime runs, for its
//          share of a scheduling period: period × w / Σw.
//
// Running for t milliseconds advances a task's vruntime by t × 1024 / w,
// so a heavy task's virtual clock runs slowly and it is picked more often.
//
// `step(state, action)` is a deterministic reducer: the same state and action
// always give the same next state, the input is never modified, and nothing
// here is random or touches the DOM.

/** The assumptions the view must show prominently. */
export const ASSUMPTIONS = Object.freeze([
  'One CPU and one run queue; real machines have one run queue per CPU and move tasks between them.',
  'Every task always wants the CPU unless you put it to sleep: no I/O, no locks, no partial slices.',
  'A task runs its whole slice before the next decision; the kernel also decides at every tick, wakeup and fork, and a waking task with an earlier deadline may preempt.',
  'All tasks are SCHED_OTHER in one group: no autogroup, no cgroup weights, no real-time tasks.',
  'Times are in milliseconds of a toy clock; the kernel counts nanoseconds.',
  'Waking tasks keep their lag under EEVDF and get at most half a period of credit under CFS; the kernel also decays lag with time asleep and has more placement rules.',
]);

/** sched_prio_to_weight from kernel/sched/core.c, nice -20 to 19. */
export const WEIGHTS = Object.freeze([
  88761, 71755, 56483, 46273, 36291,
  29154, 23254, 18705, 14949, 11916,
  9548, 7620, 6100, 4904, 3906,
  3121, 2501, 1991, 1586, 1277,
  1024, 820, 655, 526, 423,
  335, 272, 215, 172, 137,
  110, 87, 70, 56, 45,
  36, 29, 23, 18, 15,
]);

export const NICE0 = 1024;
const LOG_LIMIT = 200;
const HISTORY_LIMIT = 48;
const MAX_TASKS = 6;
const EPS = 1e-9;

/** CFS's period parameters (sysctl_sched_latency, min_granularity), ms. */
export const CFS = Object.freeze({ latencyMs: 6, minGranMs: 0.75 });

export const DEFAULTS = Object.freeze({
  algorithm: 'eevdf',
  sliceMs: 3,
  tasks: Object.freeze([{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }, { name: 'C', nice: 5 }]),
});

/** The weight of a nice value; out-of-range values are clamped. */
export function weightOf(nice) {
  return WEIGHTS[clampNice(nice) + 20];
}

function clampNice(n) {
  const v = Math.round(Number(n));
  return Number.isFinite(v) ? Math.min(19, Math.max(-20, v)) : 0;
}

function clampSlice(ms) {
  const v = Number(ms);
  return Number.isFinite(v) && v > 0 ? Math.min(100, Math.max(0.1, v)) : DEFAULTS.sliceMs;
}

/** Virtual time for t real milliseconds at weight w. */
export function virtual(t, w) {
  return (t * NICE0) / w;
}

/**
 * A fresh run queue. opts: {algorithm, sliceMs, tasks: [{name, nice}]}.
 */
export function initialState(opts = {}) {
  let s = {
    config: {
      algorithm: opts.algorithm === 'cfs' ? 'cfs' : 'eevdf',
      sliceMs: clampSlice(opts.sliceMs ?? DEFAULTS.sliceMs),
    },
    step: 0,
    time: 0,
    tasks: [],
    minVruntime: 0,
    current: null,
    history: [],
    events: [],
    nextId: 0,
  };
  for (const t of opts.tasks ?? DEFAULTS.tasks) s = addTask(s, t.name, t.nice, true);
  return s;
}

const runnable = (s) => s.tasks.filter((t) => !t.sleeping);

/** V: the weighted average vruntime of the runnable tasks (EEVDF's zero-lag point). */
export function avgVruntime(s) {
  const r = runnable(s);
  const W = r.reduce((n, t) => n + t.weight, 0);
  if (!W) return s.minVruntime;
  return r.reduce((n, t) => n + t.weight * t.vruntime, 0) / W;
}

/** A task's lag in real milliseconds: how much CPU it is owed (+) or ahead by (−). */
export function lagOf(s, task) {
  return ((avgVruntime(s) - task.vruntime) * task.weight) / NICE0;
}

/** Whether a task may be picked under EEVDF: its vruntime is not past V. */
export function eligible(s, task) {
  return !task.sleeping && task.vruntime <= avgVruntime(s) + EPS;
}

/** The task the scheduler would run next, and why. */
export function pickNext(s) {
  const r = runnable(s);
  if (!r.length) return { task: null, reason: 'No task is runnable: the CPU idles.' };
  if (s.config.algorithm === 'cfs') {
    const t = r.reduce((a, b) => (b.vruntime < a.vruntime - EPS ? b : a));
    return { task: t, reason: `${t.name} has the smallest vruntime (${fmt(t.vruntime)}).` };
  }
  const V = avgVruntime(s);
  const el = r.filter((t) => t.vruntime <= V + EPS);
  const pool = el.length ? el : r;
  const t = pool.reduce((a, b) => (b.deadline < a.deadline - EPS ? b : a));
  const others = r.filter((x) => x !== t && !el.includes(x)).map((x) => x.name);
  const skipped = others.length ? ` ${others.join(', ')} ${others.length === 1 ? 'is' : 'are'} not eligible: ahead of V, owed nothing.` : '';
  return { task: t, reason: `${t.name} is eligible (vruntime ${fmt(t.vruntime)} ≤ V ${fmt(V)}) and has the earliest virtual deadline (${fmt(t.deadline)}).${skipped}` };
}

/** How long the picked task runs, in real ms. */
export function sliceFor(s, task) {
  if (s.config.algorithm === 'eevdf') return s.config.sliceMs;
  const r = runnable(s);
  const W = r.reduce((n, t) => n + t.weight, 0);
  const period = Math.max(CFS.latencyMs, r.length * CFS.minGranMs);
  return (period * task.weight) / W;
}

/** Share of CPU time each task has had, and the share its weight entitles it to now. */
export function shares(s) {
  const r = runnable(s);
  const W = r.reduce((n, t) => n + t.weight, 0);
  return s.tasks.map((t) => ({
    id: t.id,
    name: t.name,
    nice: t.nice,
    weight: t.weight,
    runtime: t.runtime,
    share: s.time > 0 ? t.runtime / s.time : 0,
    entitled: !t.sleeping && W ? t.weight / W : 0,
    lag: lagOf(s, t),
    eligible: s.config.algorithm === 'eevdf' ? eligible(s, t) : null,
    sleeping: t.sleeping,
  }));
}

function fmt(v) {
  return (Math.round(v * 100) / 100).toFixed(2);
}

function log(s, kind, text) {
  s.events.push({ step: s.step, time: Math.round(s.time * 1000) / 1000, kind, text });
  if (s.events.length > LOG_LIMIT) s.events.splice(0, s.events.length - LOG_LIMIT);
}

function clone(s) {
  return {
    ...s,
    config: { ...s.config },
    tasks: s.tasks.map((t) => ({ ...t })),
    history: s.history.slice(),
    events: s.events.slice(),
  };
}

function addTask(s0, name, nice, quiet = false) {
  const s = quiet ? s0 : clone(s0);
  if (s.tasks.length >= MAX_TASKS) {
    if (!quiet) log(s, 'note', `At most ${MAX_TASKS} tasks fit in this toy.`);
    return s;
  }
  const n = clampNice(nice);
  const w = weightOf(n);
  const id = `t${s.nextId++}`;
  const label = name || String.fromCharCode(65 + (s.nextId - 1) % 26);
  let v;
  if (quiet) {
    v = 0; // the tasks a run queue starts with all start level
  } else if (s.config.algorithm === 'eevdf') {
    v = avgVruntime(s); // placed with zero lag: owed nothing, owing nothing
  } else {
    // START_DEBIT: a new task starts one of its slices behind min_vruntime.
    const probe = { weight: w, sleeping: false };
    v = s.minVruntime + virtual(sliceFor({ ...s, tasks: [...s.tasks, probe] }, probe), w);
  }
  const t = { id, name: label, nice: n, weight: w, vruntime: v, deadline: v + virtual(s.config.sliceMs, w), runtime: 0, slices: 0, sleeping: false, vlag: 0 };
  s.tasks.push(t);
  if (!quiet) {
    s.step++;
    log(s, 'join', s.config.algorithm === 'eevdf'
      ? `${label} (nice ${n}, weight ${w}) joins at V = ${fmt(v)}, with zero lag: it is owed nothing and owes nothing.`
      : `${label} (nice ${n}, weight ${w}) joins at min_vruntime plus one slice (${fmt(v)}), so a stream of new tasks cannot starve the old ones.`);
  }
  return s;
}

function runOne(s) {
  const { task, reason } = pickNext(s);
  if (!task) {
    log(s, 'idle', reason);
    return false;
  }
  const t = s.tasks.find((x) => x.id === task.id);
  const ms = sliceFor(s, t);
  const before = t.vruntime;
  t.vruntime += virtual(ms, t.weight);
  t.runtime += ms;
  t.slices++;
  if (s.config.algorithm === 'eevdf') t.deadline = t.vruntime + virtual(s.config.sliceMs, t.weight);
  s.history.push({ id: t.id, start: s.time, ms });
  if (s.history.length > HISTORY_LIMIT) s.history.splice(0, s.history.length - HISTORY_LIMIT);
  s.time += ms;
  s.current = t.id;
  const r = runnable(s);
  s.minVruntime = Math.max(s.minVruntime, Math.min(...r.map((x) => x.vruntime)));
  log(s, 'run', `${reason} It runs ${fmt(ms)} ms; at weight ${t.weight} its vruntime advances ${fmt(t.vruntime - before)} (${fmt(ms)} × 1024 / ${t.weight}).`);
  return true;
}

/**
 * The reducer. Actions:
 *   {type:'step'}                       one scheduling decision
 *   {type:'run', ms}                    decisions until the clock has advanced ms
 *   {type:'add', nice, name?}           a new task
 *   {type:'remove', id}                 a task exits
 *   {type:'nice', id, nice}             renice a task
 *   {type:'sleep', id} / {type:'wake', id}
 *   {type:'algorithm', value}           'eevdf' or 'cfs'
 *   {type:'slice', ms}                  the base slice (EEVDF)
 */
export function step(state, action) {
  switch (action?.type) {
    case 'add':
      return addTask(state, action.name, action.nice);
    case 'step': {
      const s = clone(state);
      s.step++;
      runOne(s);
      return s;
    }
    case 'run': {
      const s = clone(state);
      s.step++;
      const until = s.time + Math.max(0, Math.min(1000, Number(action.ms) || 0));
      let guard = 0;
      while (s.time < until - EPS && guard++ < 2000 && runOne(s));
      return s;
    }
    case 'remove': {
      const s = clone(state);
      const t = s.tasks.find((x) => x.id === action.id);
      if (!t) return state;
      s.step++;
      s.tasks = s.tasks.filter((x) => x.id !== action.id);
      log(s, 'leave', `${t.name} exits after ${fmt(t.runtime)} ms of CPU time. ${s.config.algorithm === 'eevdf' ? 'V moves, because the average no longer includes it.' : ''}`.trim());
      return s;
    }
    case 'nice': {
      const s = clone(state);
      const t = s.tasks.find((x) => x.id === action.id);
      if (!t) return state;
      s.step++;
      const n = clampNice(action.nice);
      const w = weightOf(n);
      if (s.config.algorithm === 'eevdf' && !t.sleeping) {
        // Lag is preserved across the change: vlag scales by old/new weight.
        const V = avgVruntime(s);
        t.vruntime = V - ((V - t.vruntime) * t.weight) / w;
        t.deadline = V + ((t.deadline - V) * t.weight) / w;
      }
      log(s, 'nice', `${t.name} goes from nice ${t.nice} (weight ${t.weight}) to nice ${n} (weight ${w}): from now on its vruntime advances ${fmt(NICE0 / w)} per real millisecond.`);
      t.nice = n;
      t.weight = w;
      return s;
    }
    case 'sleep': {
      const s = clone(state);
      const t = s.tasks.find((x) => x.id === action.id);
      if (!t || t.sleeping) return state;
      s.step++;
      t.vlag = avgVruntime(s) - t.vruntime;
      t.sleeping = true;
      log(s, 'sleep', `${t.name} sleeps (waits for I/O): it leaves the run queue${s.config.algorithm === 'eevdf' ? `, remembering its lag of ${fmt((t.vlag * t.weight) / NICE0)} ms` : ''}.`);
      return s;
    }
    case 'wake': {
      const s = clone(state);
      const t = s.tasks.find((x) => x.id === action.id);
      if (!t || !t.sleeping) return state;
      s.step++;
      t.sleeping = false;
      if (s.config.algorithm === 'eevdf') {
        // Place with the lag it left with (PLACE_LAG), measured from the new V.
        const others = runnable(s).filter((x) => x !== t);
        const W = others.reduce((n, x) => n + x.weight, 0);
        const V = W ? others.reduce((n, x) => n + x.weight * x.vruntime, 0) / W : t.vruntime;
        t.vruntime = V - t.vlag;
        t.deadline = t.vruntime + virtual(s.config.sliceMs, t.weight);
        log(s, 'wake', `${t.name} wakes and is placed with the lag it left with: vruntime ${fmt(t.vruntime)} against V ${fmt(avgVruntime(s))}. Sleeping earned it no extra credit.`);
      } else {
        // GENTLE_FAIR_SLEEPERS: at most half a latency period of credit.
        const floor = s.minVruntime - virtual(CFS.latencyMs / 2, NICE0);
        const was = t.vruntime;
        t.vruntime = Math.max(t.vruntime, floor);
        log(s, 'wake', `${t.name} wakes. Its vruntime ${fmt(was)} ${t.vruntime > was ? `was far behind, so it is raised to min_vruntime minus half a period (${fmt(t.vruntime)})` : 'is kept'}: it runs soon, but cannot bank the whole time it slept.`);
      }
      return s;
    }
    case 'algorithm': {
      const value = action.value === 'cfs' ? 'cfs' : 'eevdf';
      if (value === state.config.algorithm) return state;
      const s = clone(state);
      s.step++;
      s.config.algorithm = value;
      for (const t of s.tasks) t.deadline = t.vruntime + virtual(s.config.sliceMs, t.weight);
      log(s, 'note', value === 'eevdf'
        ? 'Switched to EEVDF: from now on, the eligible task with the earliest virtual deadline runs, for one slice.'
        : 'Switched to CFS: from now on, the task with the smallest vruntime runs, for its weighted share of a period.');
      return s;
    }
    case 'slice': {
      const s = clone(state);
      s.step++;
      s.config.sliceMs = clampSlice(action.ms);
      log(s, 'note', `The base slice is now ${fmt(s.config.sliceMs)} ms: ${s.config.algorithm === 'eevdf' ? 'shorter slices mean earlier deadlines and lower latency, at the cost of more switches' : 'CFS ignores it and uses its own period'}.`);
      return s;
    }
    default:
      return state;
  }
}

/**
 * Seeds the toy from the snapshot. `obs` and `derived` are the ctx lookups;
 * values are used only when their status is ok. Returns {opts, notes}: the
 * options for initialState and one note per input ({text, defaults}).
 */
export function seedFrom(obs, derived) {
  const opts = {};
  const notes = [];
  const kind = derived?.('derived.sched.fair_scheduler');
  if (kind?.status === 'ok' && (kind.value === 'eevdf' || kind.value === 'cfs')) {
    opts.algorithm = kind.value;
    notes.push({ text: `Picker: ${kind.value === 'eevdf' ? 'EEVDF' : 'CFS'}, the algorithm this kernel uses (derived.sched.fair_scheduler, an inference).`, defaults: false });
  } else {
    notes.push({ text: 'Defaults in use: EEVDF, because which algorithm this kernel uses is not known (derived.sched.fair_scheduler unavailable).', defaults: true });
  }
  const sl = obs?.('sched.self.slice_us');
  if (sl?.status === 'ok' && typeof sl.value === 'number' && sl.value > 0) {
    opts.sliceMs = clampSlice(sl.value / 1000);
    notes.push({ text: `Base slice ${fmt(opts.sliceMs)} ms, the slice this kernel gave atlas (sched.self.slice_us).`, defaults: false });
  } else {
    notes.push({ text: `Defaults in use: a base slice of ${DEFAULTS.sliceMs} ms, because this kernel does not report one (sched.self.slice_us unavailable).`, defaults: true });
  }
  const nice = obs?.('sched.self.nice');
  if (nice?.status === 'ok' && typeof nice.value === 'number') {
    const n = clampNice(nice.value);
    opts.tasks = [{ name: 'atlas', nice: n }, { name: 'A', nice: 0 }, { name: 'B', nice: 5 }];
    notes.push({ text: `One task, "atlas", has atlas's own nice value here, ${n} (sched.self.nice); the others are invented.`, defaults: false });
  } else {
    notes.push({ text: 'Defaults in use: three invented tasks, because atlas\'s own nice value is not available (sched.self.nice).', defaults: true });
  }
  return { opts, notes };
}

/** Scripted scenarios: each starts from its own setup and runs a few steps. */
export const SCENARIOS = Object.freeze([
  {
    id: 'equal',
    title: 'Two equal tasks',
    question: 'Two CPU-bound tasks at nice 0 share one CPU. How is the time divided, and who runs when?',
    setup: { tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }] },
    steps: [
      { action: { type: 'step' }, note: 'Both start with zero lag; the tie goes to A.' },
      { action: { type: 'step' }, note: 'A ran a slice and is now ahead of V: B is the only eligible task.' },
      { action: { type: 'run', ms: 60 }, note: 'Let 60 ms pass.' },
    ],
    expect: 'They alternate slice by slice and each gets half the CPU. Their vruntimes never drift more than one slice apart.',
  },
  {
    id: 'nice-five',
    title: 'nice 0 against nice 5',
    question: 'One task at nice 0 and one at nice 5. Five nice steps: is that 5% less CPU, or something else?',
    setup: { tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 5 }] },
    steps: [
      { action: { type: 'run', ms: 30 }, note: 'Run 30 ms.' },
      { action: { type: 'run', ms: 120 }, note: 'Run a further 120 ms and compare the shares.' },
    ],
    expect: 'Weights 1024 and 335: A gets 1024 / 1359, about 75%, and B about 25%. Each nice step is a factor of about 1.25, so five steps are a factor of about 3, not 5%.',
  },
  {
    id: 'many-against-one',
    title: 'One heavy task against four light ones',
    question: 'A task at nice −5 competes with four at nice 0. Does it get a fifth of the CPU?',
    setup: { tasks: [{ name: 'H', nice: -5 }, { name: 'A', nice: 0 }, { name: 'B', nice: 0 }, { name: 'C', nice: 0 }, { name: 'D', nice: 0 }] },
    steps: [
      { action: { type: 'run', ms: 200 }, note: 'Run 200 ms.' },
    ],
    expect: 'H weighs 3121, the others 1024 each: H gets 3121 / 7217, about 43%, and each of the others about 14%. Weight, not the number of tasks, decides.',
  },
  {
    id: 'sleeper',
    title: 'A task that sleeps',
    question: 'B sleeps for a while (waiting for a disk) while A runs. When B wakes, does it get to run for as long as it slept?',
    setup: { tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }] },
    steps: [
      { action: { type: 'run', ms: 12 }, note: 'Both run for a while.' },
      { action: { type: 'sleep', id: 't1' }, note: 'B sleeps.' },
      { action: { type: 'run', ms: 60 }, note: 'A has the CPU to itself for 60 ms.' },
      { action: { type: 'wake', id: 't1' }, note: 'B wakes.' },
      { action: { type: 'run', ms: 30 }, note: 'Watch the next 30 ms.' },
    ],
    expect: 'No. Time asleep is not owed: B wakes with the lag it had when it slept (EEVDF), or at most half a period behind (CFS), so it runs promptly and then the two share equally again. A program that sleeps cannot save up CPU time for later.',
  },
]);

export function scenarioById(id) {
  return SCENARIOS.find((sc) => sc.id === id);
}

/** The initial state of a scenario; the seed supplies the algorithm and slice. */
export function scenarioStart(scenario, seed = {}) {
  return initialState({ ...seed, ...scenario.setup });
}

/** Runs a whole scenario and returns its final state. */
export function runScenario(scenario, seed = {}) {
  return scenario.steps.reduce((st, s) => step(st, s.action), scenarioStart(scenario, seed));
}
