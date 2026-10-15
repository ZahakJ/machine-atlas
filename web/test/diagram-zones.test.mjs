import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { zoneRows, zoneState, zoneScale, BREAK_FACTOR } from '../js/diagrams/zones.model.js';

const zones = (name) => JSON.parse(readFileSync(new URL(`../dev/data/${name}.json`, import.meta.url))).observations.find((o) => o.id === 'mem.zones');

test('zoneState names where free stands against the marks', () => {
  const z = { min: 10, low: 12, high: 15 };
  assert.equal(zoneState({ ...z, free: 9 }), 'below-min');
  assert.equal(zoneState({ ...z, free: 10 }), 'below-low');
  assert.equal(zoneState({ ...z, free: 14 }), 'below-high');
  assert.equal(zoneState({ ...z, free: 15 }), 'above-high');
});

test('empty zones are separated from populated ones (two-node fixture)', () => {
  const m = zoneRows(zones('numa-server-la57').value);
  assert.deepEqual(m.populated.map((z) => `${z.node}/${z.zone}`), ['0/DMA', '0/DMA32', '0/Normal', '1/Normal']);
  assert.equal(m.empty.length, 6);
  assert.deepEqual(m.nodes, [0, 1]);
});

test('the restricted fixture has no zone table to draw', () => {
  const o = zones('restricted');
  assert.equal(o.status, 'permission_denied');
  assert.equal(o.value, null);
  assert.deepEqual(zoneRows(o.value), { populated: [], empty: [], skipped: 0, nodes: [] });
});

test('malformed rows are skipped', () => {
  const m = zoneRows([{ node: 0, zone: 'Normal', free: 'x' }, { zone: 1 }, null]);
  assert.equal(m.skipped, 3);
});

test('each zone scale spreads its watermarks and still ends at managed', () => {
  for (const z of zoneRows(zones('desktop-x86_64').value).populated) {
    const s = zoneScale(z, 400);
    assert.equal(s.broken, true, z.zone);
    assert.equal(s.breakAt, Math.ceil(BREAK_FACTOR * z.high));
    assert.ok(s.x(z.min) <= s.x(z.low) && s.x(z.low) <= s.x(z.high) && s.x(z.high) < s.breakX0);
    assert.ok(s.x(z.high) - s.x(z.min) > 40, `${z.zone}: watermarks are readable`);
    assert.equal(s.x(z.managed), 400);
  }
  // A zone whose watermarks are a large part of it needs no break.
  assert.equal(zoneScale({ managed: 100, free: 50, min: 30, low: 40, high: 50 }, 400).broken, false);
});
