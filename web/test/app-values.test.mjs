import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  shortValue, exactValue, cellText, tableColumns, buildRows, matches, facet, filtersFrom, catalogLookup,
} from '../js/render/values.js';
import { layered, curve } from '../js/render/layout.js';

const ok = (id, value, extra = {}) => ({ id, value, unit: '', kind: 'static', status: 'ok', collector: 'memory.meminfo', source: { type: 'file', path: '/proc/meminfo' }, ...extra });

test('shortValue and exactValue by unit and status', () => {
  const total = ok('mem.meminfo.MemTotal', 16642486272, { unit: 'bytes' });
  assert.equal(shortValue(total), '15.5 GiB');
  assert.equal(exactValue(total), '16\u202f642\u202f486\u202f272 bytes');
  assert.equal(shortValue(ok('x', 2049, { unit: 'pages' })), '2\u202f049 pages');
  assert.equal(shortValue(ok('x', 1, { unit: 'pages' })), '1 page');
  assert.equal(exactValue(ok('x', 48, { unit: 'bits' })), null, 'already exact');
  assert.equal(shortValue(ok('x', true)), 'yes');
  assert.equal(shortValue(ok('x', 'max')), 'max');
  assert.equal(shortValue(ok('x', 0.4321)), '0.4321');
  assert.equal(shortValue(ok('x', [{ a: 1 }, { a: 2 }])), 'table · 2 rows');
  assert.equal(shortValue(ok('x', 'y'.repeat(200))).length, 80);
  assert.equal(shortValue({ id: 'x', value: null, status: 'permission_denied' }), 'permission denied');
  assert.equal(shortValue({ id: 'derived.x', value: null, status: 'unknown' }), 'unknown');
  assert.equal(exactValue({ id: 'x', value: 5, status: 'failed' }), null);
  assert.equal(shortValue(undefined), '—');
});

test('cellText and tableColumns', () => {
  assert.equal(cellText(1234567), '1\u202f234\u202f567');
  assert.equal(cellText(false), 'no');
  assert.equal(cellText(null), '');
  assert.equal(cellText('<b>'), '<b>');
  assert.deepEqual(tableColumns([{ a: 1, b: 2 }, { c: 3, a: 4 }]), ['a', 'b', 'c']);
});

const observations = [
  ok('mem.meminfo.MemTotal', 16642486272, { unit: 'bytes', kind: 'live' }),
  { ...ok('mem.slabinfo.cache_count', null), status: 'permission_denied', collector: 'memory.slab', source: { type: 'file', path: '/proc/slabinfo' } },
  ok('cpu.model_name', 'Example CPU', { collector: 'system.cpu', source: { type: 'file', path: '/proc/cpuinfo', field: 'model name' } }),
];
const facts = [
  { id: 'derived.paging.levels', value: 4, unit: 'count', status: 'ok', rule: 'x86_64 without la57', inputs: ['cpu.flags.la57'], basis: 'inference' },
  { id: 'derived.swap.used_fraction', value: null, status: 'unknown', reason: 'mem.meminfo.SwapTotal is absent', rule: 'SwapUsed / SwapTotal', inputs: [], basis: 'arithmetic' },
];
const lookup = catalogLookup([
  { id: 'mem.meminfo.MemTotal', title: 'Total usable memory' },
  { id: 'mem.meminfo.*', title: 'A /proc/meminfo field' },
  { id: 'mem.*', title: 'too broad' },
]);

test('catalogLookup prefers exact entries, then the longest family', () => {
  assert.equal(lookup('mem.meminfo.MemTotal').title, 'Total usable memory');
  assert.equal(lookup('mem.meminfo.Dirty').title, 'A /proc/meminfo field');
  assert.equal(lookup('mem.vmstat.pgfault').title, 'too broad');
  assert.equal(lookup('cpu.model_name'), undefined);
  assert.equal(catalogLookup(null)('x'), undefined);
});

test('buildRows merges observations, facts and catalog titles', () => {
  const rows = buildRows(observations, facts, lookup);
  assert.deepEqual(rows.map((r) => r.id), ['cpu.model_name', 'mem.meminfo.MemTotal', 'mem.slabinfo.cache_count', 'derived.paging.levels', 'derived.swap.used_fraction']);
  const total = rows[1];
  assert.equal(total.title, 'Total usable memory');
  assert.equal(total.kind, 'live');
  assert.equal(total.origin, 'memory.meminfo');
  assert.equal(total.namespace, 'mem');
  assert.equal(rows[3].kind, 'derived');
  assert.equal(rows[3].origin, 'inference');
  assert.equal(rows[0].source, '/proc/cpuinfo model name');
});

test('matches: every filter narrows, words are ANDed, case-insensitive', () => {
  const rows = buildRows(observations, facts, lookup);
  const ids = (f) => rows.filter((r) => matches(r, f)).map((r) => r.id);
  assert.equal(ids({}).length, 5);
  assert.deepEqual(ids({ status: 'permission_denied' }), ['mem.slabinfo.cache_count']);
  assert.deepEqual(ids({ status: 'unknown' }), ['derived.swap.used_fraction']);
  assert.deepEqual(ids({ kind: 'live' }), ['mem.meminfo.MemTotal']);
  assert.deepEqual(ids({ kind: 'static' }), ['cpu.model_name', 'mem.slabinfo.cache_count']);
  assert.deepEqual(ids({ origin: 'inference' }), ['derived.paging.levels']);
  assert.deepEqual(ids({ ns: 'derived' }), ['derived.paging.levels', 'derived.swap.used_fraction']);
  assert.deepEqual(ids({ q: 'TOTAL usable' }), ['mem.meminfo.MemTotal']);
  assert.deepEqual(ids({ q: '15.5 GiB' }), ['mem.meminfo.MemTotal'], 'searches the shown value');
  assert.deepEqual(ids({ q: 'cpuinfo' }), ['cpu.model_name'], 'searches the source path');
  assert.deepEqual(ids({ q: 'total', ns: 'cpu' }), []);
});

test('facet counts and filtersFrom keeps known keys only', () => {
  const rows = buildRows(observations, facts, lookup);
  assert.deepEqual(facet(rows, 'namespace'), [{ value: 'cpu', count: 1 }, { value: 'derived', count: 2 }, { value: 'mem', count: 2 }]);
  assert.deepEqual(filtersFrom({ q: 'a', status: '', evil: 'x', kind: 'live' }), { q: 'a', kind: 'live' });
});

test('layered: columns follow prerequisites, rows follow predecessors', () => {
  const nodes = ['a', 'b', 'c', 'd', 'e'].map((id) => ({ id }));
  const edges = [{ from: 'a', to: 'c' }, { from: 'b', to: 'd' }, { from: 'c', to: 'e' }, { from: 'd', to: 'e' }, { from: 'x', to: 'a' }];
  const { columns, place } = layered(nodes, edges);
  assert.deepEqual(columns, [['a', 'b'], ['c', 'd'], ['e']]);
  for (const e of edges.filter((x) => x.from !== 'x')) assert.ok(place.get(e.from).col < place.get(e.to).col);

  // Barycentre ordering: d's predecessor is above c's, so d moves up.
  const crossed = layered([{ id: 'p' }, { id: 'q' }, { id: 'r' }, { id: 's' }], [{ from: 'p', to: 's' }, { from: 'q', to: 'r' }]);
  assert.deepEqual(crossed.columns[1], ['s', 'r']);
});

test('layered: a root moves next to its first successor', () => {
  const nodes = ['a', 'b', 'c', 'late'].map((id) => ({ id }));
  const { place } = layered(nodes, [{ from: 'a', to: 'b' }, { from: 'b', to: 'c' }, { from: 'late', to: 'c' }]);
  assert.equal(place.get('late').col, 1);
  assert.equal(place.get('a').col, 0);
});

test('layered survives cycles and is deterministic', () => {
  const nodes = [{ id: 'a' }, { id: 'b' }];
  const cyc = layered(nodes, [{ from: 'a', to: 'b' }, { from: 'b', to: 'a' }]);
  assert.equal(cyc.place.size, 2);
  assert.deepEqual(layered(nodes, []), layered(nodes, []));
  assert.match(curve(0, 0, 100, 50), /^M0,0 C50,0 50,50 100,50$/);
});

test('wrap: graph labels fit two lines and say when they were cut', async () => {
  const { wrap } = await import('../js/render/graph.js');
  assert.deepEqual(wrap('Page faults', 24, 2), ['Page faults']);
  assert.deepEqual(wrap('Every process gets the whole machine', 24, 2), ['Every process gets the', 'whole machine']);
  const cut = wrap('Signals, pipes, sockets and shared memory between processes', 24, 2);
  assert.equal(cut.length, 2);
  assert.ok(cut[1].endsWith('…'));
});

test('isNumericCell: grouped numbers and short units, not words', async () => {
  const { isNumericCell } = await import('../js/render/values.js');
  for (const s of ['0', '3 670 016', '7 724 800', '1,044,480', '7.19 GiB', '−12', '99.5 %', '', '4 KiB'])
    assert.equal(isNumericCell(s), true, s);
  for (const s of ['DMA32', 'Normal', '0-15', '1 2', '12345 apples', 'node 0'])
    assert.equal(isNumericCell(s), false, s);
});

test('columnTitle: readable headers for catalog column keys', async () => {
  const { columnTitle } = await import('../js/render/values.js');
  const cases = [['size_bytes', 'Size'], ['used_bytes', 'Used'], ['nr', 'Pages in pool'], ['order10', 'Order 10'],
    ['mount_point', 'Mount point'], ['zone', 'Zone'], ['line_size_bytes', 'Line size'], ['', '']];
  for (const [k, want] of cases) assert.equal(columnTitle(k), want, k);
});
