import { test } from 'node:test';
import assert from 'node:assert/strict';

import { clause, because, collapse, unknownWording, lowerFirst } from '../js/render/conditions.js';

const ok = (id, value, extra = {}) => ({ id, value, status: 'ok', ...extra });

test('lowerFirst keeps acronyms', () => {
  assert.equal(lowerFirst('Swap used fraction'), 'swap used fraction');
  assert.equal(lowerFirst('THP mode'), 'THP mode');
  assert.equal(lowerFirst('cgroup memory limit'), 'cgroup memory limit');
});

test('a yes/no fact reads as a statement, never "were true"', () => {
  const c = { fact: 'derived.swap.configured', expect: null, title: 'Swap configured', type: 'bool', item: ok('derived.swap.configured', true) };
  assert.equal(clause(c, 'then', true), 'swap is configured');
  assert.equal(clause(c, 'else'), 'no swap is configured');
  assert.equal(clause(c, 'unknown'), 'Machine Atlas cannot determine whether swap is configured');
  const other = { fact: 'x.flag', expect: null, title: 'Some flag', type: 'bool', item: ok('x.flag', false) };
  assert.equal(clause(other, 'else', true), 'some flag: no');
});

test('a truthiness test on a number says non-zero, with the value in its unit', () => {
  const frac = { fact: 'derived.swap.used_fraction', expect: null, title: 'Swap used fraction', type: 'float', item: ok('derived.swap.used_fraction', 0.995371) };
  assert.equal(clause(frac, 'then', true), 'swap used fraction is non-zero (99.5%)');
  assert.equal(clause(frac, 'else'), 'swap used fraction is zero');
  const bytes = { fact: 'derived.zones.low_bytes', expect: null, title: 'Total low watermark', unit: 'bytes', type: 'int', item: ok('derived.zones.low_bytes', 101921587, { unit: 'bytes' }) };
  assert.equal(clause(bytes, 'then', true), 'total low watermark is non-zero (97.2 MiB)');
  // Unknown value: the catalog type still says it is a number.
  const unknown = { ...bytes, item: { id: bytes.fact, status: 'unknown', value: null } };
  assert.equal(clause(unknown, 'then'), 'total low watermark is non-zero');
});

test('a table reads as "could be read (N rows)"', () => {
  const c = { fact: 'mem.zones', expect: null, title: 'Memory zones', type: 'table', item: ok('mem.zones', [{}, {}, {}, {}, {}]) };
  assert.equal(clause(c, 'then', true), 'memory zones could be read (5 rows)');
  assert.equal(clause(c, 'else'), 'memory zones: no rows');
});

test('an equality test reads "is <value>", formatted by unit', () => {
  const c = { fact: 'kernel.page_size_bytes', expect: 4096, title: 'Base page size', unit: 'bytes', type: 'int', item: ok('kernel.page_size_bytes', 16384, { unit: 'bytes' }) };
  assert.equal(clause(c, 'then'), 'base page size is 4 KiB');
  assert.equal(clause(c, 'else', true), 'base page size is 16 KiB, not 4 KiB');
  assert.equal(clause(c, 'else'), 'base page size is not 4 KiB');
  const mode = { fact: 'derived.thp.mode', expect: 'always', title: 'THP mode', type: 'string', item: ok('derived.thp.mode', 'madvise') };
  assert.equal(clause(mode, 'then'), 'THP mode is always');
});

test('because joins nested conditions and states an unknown with its status', () => {
  const outer = { fact: 'derived.swap.configured', expect: null, title: 'Swap configured', chosen: 'then', item: ok('derived.swap.configured', true) };
  const inner = { fact: 'derived.swap.used_fraction', expect: null, title: 'Swap used fraction', chosen: 'then', item: ok('derived.swap.used_fraction', 0.0492) };
  assert.equal(because([outer, inner]), 'swap is configured and swap used fraction is non-zero (4.92%)');
  const lost = { fact: 'derived.swap.compressed', expect: null, title: 'Compressed swap present', chosen: 'unknown', item: { id: 'derived.swap.compressed', status: 'unknown', value: null } };
  assert.equal(because([lost]), 'Machine Atlas cannot determine whether compressed swap is present (unknown)');
  assert.match(unknownWording({ ...outer, item: { ...outer.item, source: { path: '/proc/meminfo' } } }), /^Machine Atlas could not determine whether swap is configured on this machine, .*\(\/proc\/meminfo\)\.$/);
});

test('collapse merges a branch whose chosen arm is only another branch', () => {
  const p = { t: 'p', inl: [] };
  const inner = { t: 'branch', fact: 'b', chosen: 'else', then: [p], else: [p, p], unknown: [] };
  const outer = { t: 'branch', fact: 'a', chosen: 'then', then: [inner], else: [p], unknown: [] };
  const { conditions, content } = collapse(outer);
  assert.deepEqual(conditions.map((c) => c.fact), ['a', 'b']);
  assert.equal(content.length, 2);
  // A chosen arm with more than the nested branch stays one level.
  const mixed = { ...outer, then: [p, inner] };
  assert.equal(collapse(mixed).conditions.length, 1);
});
