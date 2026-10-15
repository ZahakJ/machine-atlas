import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  initialState, step, shares, avgVruntime, lagOf, eligible, pickNext, sliceFor, weightOf, virtual,
  seedFrom, SCENARIOS, scenarioById, scenarioStart, runScenario, WEIGHTS, NICE0, ASSUMPTIONS, CFS,
} from '../js/sim/fair-share.model.js';

const close = (a, b, tol, msg) => assert.ok(Math.abs(a - b) <= tol, `${msg}: ${a} is not within ${tol} of ${b}`);

test('the weight table is the kernel\'s: 1024 at nice 0, about 1.25 per step', () => {
  assert.equal(WEIGHTS.length, 40);
  assert.equal(weightOf(0), 1024);
  assert.equal(weightOf(-20), 88761);
  assert.equal(weightOf(19), 15);
  assert.equal(weightOf(99), 15, 'clamped');
  assert.equal(weightOf(-99), 88761, 'clamped');
  for (let n = -19; n <= 19; n++) {
    const r = weightOf(n - 1) / weightOf(n);
    assert.ok(r >= 1.19 && r < 1.3, `nice ${n - 1}→${n}: ${r}`);
  }
  assert.equal(virtual(3, 1024), 3);
  assert.equal(virtual(3, 2048), 1.5);
});

test('the initial state has the default tasks, level, and nothing run yet', () => {
  const s = initialState();
  assert.equal(s.tasks.length, 3);
  assert.ok(s.tasks.every((t) => t.vruntime === 0 && t.runtime === 0));
  assert.equal(s.time, 0);
  assert.equal(s.config.algorithm, 'eevdf');
  assert.ok(ASSUMPTIONS.length >= 4);
});

for (const algorithm of ['eevdf', 'cfs']) {
  test(`${algorithm}: long-run shares follow the weights`, () => {
    let s = initialState({ algorithm, tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 5 }, { name: 'C', nice: -3 }] });
    s = step(s, { type: 'run', ms: 3000 });
    const W = 1024 + 335 + 1991;
    for (const sh of shares(s)) {
      close(sh.share, sh.weight / W, 0.01, `${sh.name} share`);
      close(sh.entitled, sh.weight / W, 1e-12, `${sh.name} entitlement`);
    }
    const total = shares(s).reduce((n, x) => n + x.runtime, 0);
    close(total, s.time, 1e-6, 'time is conserved');
  });

  test(`${algorithm}: vruntimes stay within about one slice of each other`, () => {
    let s = initialState({ algorithm, tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 10 }, { name: 'C', nice: -10 }] });
    for (let i = 0; i < 200; i++) {
      s = step(s, { type: 'step' });
      const v = s.tasks.map((t) => t.vruntime);
      const maxSlice = Math.max(...s.tasks.map((t) => virtual(Math.max(sliceFor(s, t), s.config.sliceMs), t.weight)));
      assert.ok(Math.max(...v) - Math.min(...v) <= 2 * maxSlice + 1e-9, `spread at step ${i}`);
    }
  });
}

test('eevdf: lag sums to zero and only tasks at or behind V are eligible', () => {
  let s = initialState({ tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 3 }, { name: 'C', nice: -2 }] });
  for (let i = 0; i < 50; i++) {
    s = step(s, { type: 'step' });
    const sum = s.tasks.reduce((n, t) => n + lagOf(s, t), 0);
    close(sum, 0, 1e-6, 'Σ lag');
    const V = avgVruntime(s);
    for (const t of s.tasks) assert.equal(eligible(s, t), t.vruntime <= V + 1e-9);
    const { task } = pickNext(s);
    assert.ok(eligible(s, task), 'the picked task is eligible');
    const earliest = Math.min(...s.tasks.filter((t) => eligible(s, t)).map((t) => t.deadline));
    assert.equal(task.deadline, earliest, 'and has the earliest deadline among them');
  }
});

test('eevdf: two equal tasks alternate, A first', () => {
  let s = initialState({ tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }] });
  const order = [];
  for (let i = 0; i < 6; i++) {
    s = step(s, { type: 'step' });
    order.push(s.tasks.find((t) => t.id === s.current).name);
  }
  assert.deepEqual(order, ['A', 'B', 'A', 'B', 'A', 'B']);
});

test('cfs: the smallest vruntime runs, for its weighted share of the period', () => {
  let s = initialState({ algorithm: 'cfs', tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 5 }] });
  const a = s.tasks[0];
  close(sliceFor(s, a), (CFS.latencyMs * 1024) / (1024 + 335), 1e-9, 'A slice');
  s = step(s, { type: 'step' });
  assert.equal(s.current, 't0');
  assert.equal(pickNext(s).task.name, 'B', 'B now has the smaller vruntime');
});

test('the reducer is pure and deterministic', () => {
  const s0 = initialState();
  const snapshot = JSON.stringify(s0);
  const actions = [{ type: 'run', ms: 20 }, { type: 'add', nice: -5 }, { type: 'nice', id: 't1', nice: 10 },
    { type: 'sleep', id: 't2' }, { type: 'run', ms: 15 }, { type: 'wake', id: 't2' }, { type: 'algorithm', value: 'cfs' },
    { type: 'run', ms: 15 }, { type: 'remove', id: 't0' }, { type: 'slice', ms: 1 }, { type: 'algorithm', value: 'eevdf' }, { type: 'step' }];
  const a = actions.reduce(step, s0);
  const b = actions.reduce(step, s0);
  assert.equal(JSON.stringify(s0), snapshot, 'input untouched');
  assert.deepEqual(a, b);
  assert.ok(a.events.length > 0 && a.events.every((e) => typeof e.text === 'string' && e.text.length > 10));
});

test('unknown and impossible actions leave the state unchanged', () => {
  const s = initialState();
  assert.equal(step(s, { type: 'bogus' }), s);
  assert.equal(step(s, { type: 'remove', id: 'nope' }), s);
  assert.equal(step(s, { type: 'wake', id: 't0' }), s, 'waking a running task');
  assert.equal(step(s, { type: 'algorithm', value: 'eevdf' }), s, 'same algorithm');
});

test('a new task joins with zero lag under eevdf and one slice behind under cfs', () => {
  let s = step(initialState({ tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }] }), { type: 'run', ms: 30 });
  s = step(s, { type: 'add', nice: 0, name: 'N' });
  const n = s.tasks.find((t) => t.name === 'N');
  close(lagOf(s, n), 0, 1e-9, 'zero lag');

  let c = step(initialState({ algorithm: 'cfs', tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }] }), { type: 'run', ms: 30 });
  c = step(c, { type: 'add', nice: 0, name: 'N' });
  const m = c.tasks.find((t) => t.name === 'N');
  assert.ok(m.vruntime > c.minVruntime, 'start debit');
});

test('renicing under eevdf keeps the lag', () => {
  let s = step(initialState({ tasks: [{ name: 'A', nice: 0 }, { name: 'B', nice: 0 }, { name: 'C', nice: 0 }] }), { type: 'step' });
  const before = lagOf(s, s.tasks[1]);
  s = step(s, { type: 'nice', id: 't1', nice: 7 });
  close(lagOf(s, s.tasks[1]), before, 1e-9, 'lag');
  assert.equal(s.tasks[1].weight, weightOf(7));
});

test('a sleeper does not bank CPU time', () => {
  for (const algorithm of ['eevdf', 'cfs']) {
    const sc = scenarioById('sleeper');
    const end = runScenario(sc, { algorithm });
    const [a, b] = shares(end);
    assert.ok(a.runtime > b.runtime + 40, `${algorithm}: A ran while B slept and keeps that lead`);
  }
});

test('every scenario runs and states what to look for', () => {
  assert.ok(SCENARIOS.length >= 4);
  for (const sc of SCENARIOS) {
    assert.ok(sc.title && sc.question && sc.expect && sc.steps.length, sc.id);
    const st = scenarioStart(sc, { algorithm: 'eevdf' });
    assert.equal(st.step, 0);
    const end = runScenario(sc);
    assert.ok(end.time > 0, sc.id);
  }
  const nf = shares(runScenario(scenarioById('nice-five')));
  close(nf[0].share, 1024 / 1359, 0.03, 'nice-five A');
  const many = shares(runScenario(scenarioById('many-against-one')));
  close(many[0].share, 3121 / 7217, 0.03, 'heavy task');
});

test('seedFrom uses only ok values and says where each came from', () => {
  const ok = (value) => ({ status: 'ok', value });
  const seeded = seedFrom((id) => ({ 'sched.self.slice_us': ok(2800), 'sched.self.nice': ok(10) })[id], (id) => ({ 'derived.sched.fair_scheduler': ok('cfs') })[id]);
  assert.equal(seeded.opts.algorithm, 'cfs');
  assert.equal(seeded.opts.sliceMs, 2.8);
  assert.equal(seeded.opts.tasks[0].nice, 10);
  assert.ok(seeded.notes.every((n) => !n.defaults));
  const none = seedFrom(() => ({ status: 'absent', value: null }), () => undefined);
  assert.deepEqual(none.opts, {});
  assert.ok(none.notes.length === 3 && none.notes.every((n) => n.defaults && /unavailable|not available|not known/.test(n.text)));
  assert.equal(NICE0, 1024);
});
