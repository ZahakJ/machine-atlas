import { test } from 'node:test';
import assert from 'node:assert/strict';
import { read, readAll, whyMissing, isTable, isNumber } from '../js/diagrams/inputs.js';

const ctx = {
  obs: (id) => ({
    'mem.meminfo.MemTotal': { id, value: 1024, unit: 'bytes', status: 'ok' },
    'mem.zones': { id, value: null, status: 'permission_denied', note: 'open /proc/zoneinfo: permission denied' },
    'mem.weird': { id, value: 'twelve', status: 'ok' },
  })[id],
  derived: (id) => ({
    'derived.reclaim.lru_kind': { id, value: null, status: 'unknown', reason: 'mem.lru_gen.enabled is permission denied', inputs: ['mem.lru_gen.enabled'] },
  })[id],
};

test('read returns ok records and explains every other state', () => {
  assert.equal(read(ctx, 'mem.meminfo.MemTotal').ok, true);
  const denied = read(ctx, 'mem.zones');
  assert.equal(denied.ok, false);
  assert.equal(whyMissing(denied), 'mem.zones is permission denied: open /proc/zoneinfo: permission denied');
  const unknown = read(ctx, 'derived.reclaim.lru_kind');
  assert.equal(unknown.kind, 'derived');
  assert.match(whyMissing(unknown), /is unknown: mem.lru_gen.enabled is permission denied/);
  assert.deepEqual(unknown.inputs, ['mem.lru_gen.enabled']);
  const absent = read(ctx, 'mem.nothing');
  assert.equal(absent.status, 'missing');
  assert.equal(whyMissing(absent), 'mem.nothing is missing: not in this snapshot');
});

test('read tolerates an empty or throwing ctx', () => {
  assert.equal(read(undefined, 'mem.x').status, 'missing');
  assert.equal(read({}, 'derived.x').status, 'missing');
  assert.equal(read({ obs: () => { throw new Error('boom'); } }, 'mem.x').status, 'missing');
});

test('readAll treats a value of the wrong shape as missing', () => {
  const r = readAll(ctx, ['mem.meminfo.MemTotal', 'mem.weird', 'mem.zones'], { 'mem.weird': isNumber });
  assert.equal(r.ok, false);
  assert.deepEqual(r.missing.map((m) => [m.id, m.status]), [['mem.weird', 'failed'], ['mem.zones', 'permission_denied']]);
  assert.equal(r.get('mem.meminfo.MemTotal').value, 1024);
  assert.equal(readAll(ctx, ['mem.meminfo.MemTotal']).ok, true);
});

test('shape predicates', () => {
  assert.equal(isTable([{ a: 1 }]), true);
  assert.equal(isTable([]), true);
  assert.equal(isTable([[1]]), false);
  assert.equal(isTable('x'), false);
  assert.equal(isNumber(NaN), false);
});
