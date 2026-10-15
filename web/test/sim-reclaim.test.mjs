import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  initialState, step, freeFrames, frameSummary, seedFrom, scanBalance,
  SCENARIOS, scenarioById, scenarioStart, runScenario, COUNTERS, DEFAULTS,
} from '../js/sim/reclaim.model.js';

// Every structural invariant of the toy machine; returns the first violation.
function violations(s) {
  const out = [];
  const resident = Object.values(s.pages).filter((p) => p.where === 'ram');
  if (freeFrames(s) + resident.length !== s.config.frames) out.push('frames not conserved');
  for (const p of resident) {
    if (s.frames[p.frame] !== p.id) out.push(`${p.id} frame mismatch`);
    if (!s.lru[p.type][p.list]?.includes(p.id)) out.push(`${p.id} missing from ${p.type}/${p.list}`);
  }
  const onLists = Object.values(s.lru).flatMap((l) => [...l.active, ...l.inactive]);
  if (onLists.length !== resident.length || new Set(onLists).size !== onLists.length) out.push('lists do not hold each resident page once');
  for (const p of Object.values(s.pages)) {
    if (p.where === 'swap' && s.swap[p.slot] !== p.id) out.push(`${p.id} slot mismatch`);
    if (p.where !== 'ram' && p.frame !== null) out.push(`${p.id} non-resident with a frame`);
    if (p.type === 'file' && p.where === 'swap') out.push(`${p.id} file page in swap`);
  }
  const swapped = s.swap.filter((x) => x !== null);
  if (swapped.length !== Object.values(s.pages).filter((p) => p.where === 'swap').length) out.push('swap slots mismatch');
  if (!s.config.swapPresent && swapped.length) out.push('pages in swap with swap off');
  for (const c of COUNTERS) if (!(s.counters[c] >= 0)) out.push(`counter ${c} invalid`);
  return out;
}

// A deterministic, varied sequence of actions (linear congruential choice).
function actionSequence(n, seed) {
  const all = [
    { type: 'read', pages: 8 }, { type: 'write', pages: 8 }, { type: 'alloc', pages: 8 },
    { type: 'alloc', pages: 24 }, { type: 'touch', kind: 'file', pages: 16 },
    { type: 'touch', kind: 'anon', pages: 16 }, { type: 'tick' },
    { type: 'swap', present: false }, { type: 'swap', present: true },
    { type: 'swappiness', value: 0 }, { type: 'swappiness', value: 200 },
  ];
  const out = [];
  let x = seed;
  for (let i = 0; i < n; i++) {
    x = (x * 1103515245 + 12345) % 2147483648;
    out.push(all[x % all.length]);
  }
  return out;
}

const run = (s, actions) => actions.reduce(step, s);
const kinds = (s) => new Set(s.events.map((e) => e.kind));

test('initial state: all frames free, configuration seeded', () => {
  const s = initialState({ swapPresent: false, swappiness: 10 });
  assert.equal(freeFrames(s), DEFAULTS.frames);
  assert.equal(s.config.swapPresent, false);
  assert.equal(s.config.swappiness, 10);
  assert.equal(initialState({ swappiness: 300 }).config.swappiness, 200);
  assert.equal(initialState({ swappiness: -5 }).config.swappiness, 0);
});

test('invariants hold after every step of long random runs', () => {
  for (const seed of [1, 7, 42, 2024]) {
    let s = initialState();
    for (const a of actionSequence(300, seed)) {
      s = step(s, a);
      assert.deepEqual(violations(s), [], `seed ${seed} step ${s.step} ${a.type}`);
    }
  }
});

test('invariants hold through every scenario', () => {
  for (const sc of SCENARIOS) {
    let s = scenarioStart(sc);
    for (const st of sc.steps) {
      s = step(s, st.action);
      assert.deepEqual(violations(s), [], `${sc.id} step ${s.step}`);
    }
  }
});

test('step is deterministic and does not modify its input', () => {
  const actions = actionSequence(120, 99);
  assert.deepEqual(run(initialState(), actions), run(initialState(), actions));
  const s = run(initialState(), actions.slice(0, 60));
  const before = JSON.stringify(s);
  step(s, { type: 'alloc', pages: 24 });
  assert.equal(JSON.stringify(s), before);
});

test('unknown actions are rejected', () => {
  assert.throws(() => step(initialState(), { type: 'explode' }), TypeError);
});

test('free above low wakes nothing; crossing low wakes kswapd, which restores high', () => {
  let s = step(initialState(), { type: 'read', pages: 80 });
  assert.equal(freeFrames(s), 16);
  assert.ok(!kinds(s).has('kswapd-wake'));
  s = step(s, { type: 'alloc', pages: 7 }); // 9 free < low 10
  assert.ok(kinds(s).has('kswapd-wake'));
  assert.ok(freeFrames(s) >= DEFAULTS.watermarks.high);
  assert.equal(s.kswapd, 'asleep');
  assert.ok(s.counters.pgsteal_kswapd > 0);
});

test('a burst that outruns kswapd enters direct reclaim', () => {
  const s = run(initialState(), [{ type: 'read', pages: 80 }, { type: 'alloc', pages: 24 }]);
  assert.ok(s.counters.allocstall > 0);
  assert.ok(kinds(s).has('direct-reclaim'));
});

test('dirty pages need writeback before reclaim; time passing cleans them', () => {
  let s = run(initialState(), [{ type: 'write', pages: 80 }, { type: 'alloc', pages: 12 }]);
  assert.ok(kinds(s).has('writeback-needed'));
  s = step(s, { type: 'tick' });
  assert.ok(kinds(s).has('writeback-done'));
  assert.equal(frameSummary(s).fileDirty, 0);
});

test('refault of a recently evicted page is detected', () => {
  const s = run(initialState({ swapPresent: false }), [
    { type: 'read', pages: 40 }, { type: 'alloc', pages: 50 }, { type: 'touch', kind: 'file', pages: 40 },
  ]);
  assert.ok(s.counters.workingset_refault_file > 0);
  assert.ok(kinds(s).has('refault'));
});

test('swapoff brings pages back, or refuses and changes nothing', () => {
  const swapped = run(initialState({ swappiness: 200 }), [{ type: 'alloc', pages: 60 }, { type: 'read', pages: 40 }, { type: 'read', pages: 40 }]);
  assert.ok(swapped.counters.pswpout > 0);
  const full = run(swapped, [{ type: 'alloc', pages: 40 }]);
  const refused = step(full, { type: 'swap', present: false });
  assert.ok(kinds(refused).has('swapoff-refused'));
  assert.equal(refused.config.swapPresent, true);
  assert.deepEqual(refused.swap, full.swap);

  const light = run(initialState({ swappiness: 200 }), [{ type: 'alloc', pages: 30 }, { type: 'read', pages: 60 }, { type: 'read', pages: 20 }]);
  const inSwap = light.swap.filter((x) => x !== null).length;
  assert.ok(inSwap > 0);
  const off = step(light, { type: 'swap', present: false });
  assert.ok(kinds(off).has('swapoff'));
  assert.equal(off.config.swapPresent, false);
  assert.equal(off.swap.filter((x) => x !== null).length, 0);
  assert.equal(off.counters.pswpin, light.counters.pswpin + inSwap);
});

test('scan balance follows get_scan_count rules', () => {
  const noSwap = initialState({ swapPresent: false });
  assert.equal(scanBalance(noSwap).reason, 'no-swap');
  let s = step(initialState({ swappiness: 0 }), { type: 'read', pages: 40 });
  assert.equal(scanBalance(s).reason, 'swappiness-0');
  s = step(initialState({ swappiness: 100 }), { type: 'read', pages: 40 });
  assert.deepEqual([scanBalance(s).anon, scanBalance(s).file], [100, 100]);
  // File pages plus free at or below high: anonymous only, whatever swappiness says.
  s = run(initialState({ swappiness: 0 }), [{ type: 'alloc', pages: 84 }]);
  assert.equal(scanBalance(s).reason, 'file-tiny');
});

test('scenario: fill the cache, then allocate', () => {
  // What the description promises: kswapd wakes below low and, in the
  // background, drops clean cache back to high; the program never reclaims
  // for itself, nothing goes to swap, and no file page needs writeback.
  for (const seed of [{ swapPresent: true, swappiness: 60 }, { swapPresent: true, swappiness: 100 }, { swapPresent: false }]) {
    const s = runScenario(scenarioById('fill-cache-then-allocate'), seed);
    const wakes = s.events.filter((e) => e.kind === 'kswapd-wake');
    assert.ok(wakes.length >= 2, `kswapd wakes more than once (${JSON.stringify(seed)})`);
    assert.equal(s.events.filter((e) => e.kind === 'kswapd-sleep').length, wakes.length, 'kswapd reaches high every time');
    assert.ok(s.events.some((e) => e.actor === 'kswapd' && e.kind === 'reclaimed' && /clean file page/.test(e.text)));
    assert.ok(!kinds(s).has('direct-reclaim'), 'no direct reclaim');
    assert.equal(s.counters.allocstall, 0);
    assert.equal(s.counters.pgscan_direct, 0);
    assert.equal(s.counters.pswpout, 0);
    assert.equal(s.counters.oom_would_kill, 0);
    assert.ok(s.counters.pgsteal_kswapd > 0);
    assert.deepEqual(frameSummary(s), { free: 96 - 32 - (72 - s.counters.pgsteal_kswapd), fileClean: 72 - s.counters.pgsteal_kswapd, fileDirty: 0, anon: 32 },
      'every page kswapd freed was clean cache');
  }
});

test('swapoff refusal says what a real swapoff would do', () => {
  const full = run(initialState({ swappiness: 200 }), [
    { type: 'alloc', pages: 60 }, { type: 'read', pages: 40 }, { type: 'read', pages: 40 }, { type: 'alloc', pages: 40 },
  ]);
  const text = step(full, { type: 'swap', present: false }).events.find((e) => e.kind === 'swapoff-refused').text;
  assert.match(text, /would fail with ENOMEM only if/);
});

test('scenario: no swap pins anonymous memory and ends at the OOM point', () => {
  const s = runScenario(scenarioById('no-swap'));
  assert.ok(kinds(s).has('anon-pinned'));
  assert.ok(kinds(s).has('oom'));
  assert.equal(s.counters.pswpout, 0);
  assert.equal(s.counters.oom_would_kill, 1);
  assert.match(s.events.find((e) => e.kind === 'oom').text, /nothing is killed/);
});

test('scenario: swappiness 0 versus 100', () => {
  const zero = runScenario(scenarioById('swappiness-0'));
  const hundred = runScenario(scenarioById('swappiness-100'));
  assert.equal(zero.counters.pswpout, 0);
  assert.ok(hundred.counters.pswpout > 0);
  assert.ok(hundred.counters.pswpin > 0);
  assert.equal(scenarioById('swappiness-0').compare, 'swappiness-100');
});

test('scenario: thrash refaults on every loop', () => {
  const s = runScenario(scenarioById('thrash'));
  const thrashSteps = s.events.filter((e) => e.kind === 'thrashing').map((e) => e.step);
  assert.ok(thrashSteps.length >= 2);
  assert.ok(s.counters.workingset_refault_file >= 30);
  assert.equal(s.counters.oom_would_kill, 0);
});

test('scenario setup overrides the seed only where it says so', () => {
  assert.equal(scenarioStart(scenarioById('no-swap'), { swapPresent: true, swappiness: 30 }).config.swappiness, 30);
  assert.equal(scenarioStart(scenarioById('no-swap'), { swapPresent: true }).config.swapPresent, false);
});

test('seedFrom uses ok values and says when defaults are in use', () => {
  const obs = (id) => (id === 'mem.sysctl.vm.swappiness' ? { status: 'ok', value: 10 } : undefined);
  const derived = (id) => (id === 'derived.swap.configured' ? { status: 'ok', value: false } : undefined);
  const seeded = seedFrom(obs, derived);
  assert.deepEqual(seeded.opts, { swapPresent: false, swappiness: 10 });
  assert.ok(seeded.notes.every((n) => !n.defaults));

  const none = seedFrom(undefined, () => ({ status: 'unknown', value: null }));
  assert.deepEqual(none.opts, {});
  assert.ok(none.notes.every((n) => n.defaults && /Defaults in use/.test(n.text)));
});

test('every event has a step, an actor and a sentence', () => {
  const s = run(initialState(), actionSequence(200, 5));
  for (const e of s.events) {
    assert.ok(e.step > 0 && e.actor && e.kind);
    assert.match(e.text, /[.!]$/);
  }
  assert.ok(s.events.length <= 300);
});
