import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { ACCOUNTED_FALLBACK, consumerIds, breakdown } from '../js/diagrams/meminfo.model.js';

const load = (name) => JSON.parse(readFileSync(new URL(`../dev/data/${name}.json`, import.meta.url)));

test('the fallback list is the derive rule (internal/derive/mem.go accountedFields)', () => {
  const src = readFileSync(new URL('../../internal/derive/mem.go', import.meta.url), 'utf8');
  const block = src.match(/var accountedFields = \[\]string\{([^}]*)\}/)[1];
  const fields = [...block.matchAll(/"([A-Za-z_]+)"/g)].map((m) => `mem.meminfo.${m[1]}`);
  assert.deepEqual(ACCOUNTED_FALLBACK, fields);
});

test('consumers come from the accounted fact when it lists them', () => {
  assert.deepEqual(consumerIds({ inputs: ['mem.meminfo.MemFree', 'mem.meminfo.MemTotal', 'kernel.x'] }), ['mem.meminfo.MemFree']);
  assert.deepEqual(consumerIds(undefined), ACCOUNTED_FALLBACK);
  assert.deepEqual(consumerIds({ inputs: [] }), ACCOUNTED_FALLBACK);
});

for (const name of ['desktop-x86_64', 'rpi-arm64-noswap', 'numa-server-la57', 'restricted']) {
  test(`segments add up to the derived facts (${name})`, () => {
    const data = load(name);
    const obs = (id) => data.observations.find((o) => o.id === id)?.value;
    const fact = (id) => data.derived.find((f) => f.id === id);
    const accounted = fact('derived.mem.accounted_bytes');
    const ids = consumerIds(accounted);
    const total = obs('mem.meminfo.MemTotal');
    const b = breakdown(total, ids.map((id) => [id, obs(id)]));
    assert.equal(b.sum, accounted.value);
    assert.equal(b.unaccounted, fact('derived.mem.unaccounted_bytes').value);
    // Segments tile the bar from 0 with no overlap.
    let at = 0;
    for (const s of b.segments) {
      assert.ok(Math.abs(s.start - at) < 1e-12);
      at += s.fraction;
    }
    assert.ok(Math.abs(at + b.remainderFraction - 1) < 1e-9);
  });
}

test('fields that sum past MemTotal are an overlap, not a negative segment', () => {
  const b = breakdown(100, [['mem.meminfo.MemFree', 70], ['mem.meminfo.Cached', 50]]);
  assert.equal(b.overflow, true);
  assert.equal(b.unaccounted, -20);
  assert.equal(b.scale, 120);
  assert.equal(b.remainderFraction, 0);
  assert.equal(b.segments[1].start, 70 / 120);
  const zero = breakdown(0, []);
  assert.equal(zero.unaccounted, 0);
  assert.equal(zero.remainderFraction, 0);
});
