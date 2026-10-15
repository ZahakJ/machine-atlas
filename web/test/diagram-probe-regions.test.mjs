import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { regionRows, REGION_ORDER } from '../js/diagrams/probe-regions.model.js';

const fixture = JSON.parse(readFileSync(new URL('../dev/data/desktop-x86_64.json', import.meta.url)));
const regions = fixture.observations.find((o) => o.id === 'probe.regions').value;

test('regions come out in teaching order with the zero page detected', () => {
  const m = regionRows([...regions].reverse());
  assert.deepEqual(m.rows.map((r) => r.name), REGION_ORDER);
  const byName = Object.fromEntries(m.rows.map((r) => [r.name, r]));
  assert.equal(byName['anon-untouched'].rss, 0);
  assert.equal(byName['anon-untouched'].present, 0);
  assert.equal(byName['anon-untouched'].zeroPage, false);
  assert.equal(byName['anon-read'].zeroPage, true);
  assert.equal(byName['anon-written'].zeroPage, false);
  assert.equal(m.max, 64 * 1024 * 1024);
  assert.equal(m.skipped, 0);
});

test('rows with missing or negative numbers are dropped, unknown names sort last', () => {
  const good = regions[0];
  const m = regionRows([
    { ...good, name: 'zzz-extra' },
    { ...good, rss_bytes: -1 },
    { ...good, pages_total: undefined },
    { name: 7 },
    null,
    good,
  ]);
  assert.equal(m.skipped, 4);
  assert.deepEqual(m.rows.map((r) => r.name), [good.name, 'zzz-extra']);
  assert.deepEqual(regionRows(null), { rows: [], max: 0, skipped: 0 });
});
