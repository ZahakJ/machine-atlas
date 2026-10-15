import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { buildRows, layoutRows, displayKind, isSample } from '../js/diagrams/address-space.model.js';

const fixture = JSON.parse(readFileSync(new URL('../dev/data/desktop-x86_64.json', import.meta.url)));
const obs = (id) => fixture.observations.find((o) => o.id === id).value;
const USER_END = 1n << 47n;

test('displayKind refines the collector kinds', () => {
  assert.equal(displayKind({ kind: 'file', perms: 'r-xp', path: 'atlas' }), 'executable');
  assert.equal(displayKind({ kind: 'file', perms: 'r--p', path: 'atlas' }), 'file');
  assert.equal(displayKind({ kind: 'file', perms: 'rw-s', path: 'memfd:atlas-probe (deleted)' }), 'shmem');
  assert.equal(displayKind({ kind: 'anon', perms: '---p', path: '' }), 'reserved');
  assert.equal(displayKind({ kind: 'kernel', perms: 'r-xp', path: '[vdso]' }), 'kernel');
  assert.equal(displayKind({ kind: 'mystery', perms: 'rw-p' }), 'anon');
});

test('the fixture probe fills the whole user range, top first, with gaps between mappings', () => {
  const m = buildRows({ maps: obs('probe.maps.sample'), regions: obs('probe.regions'), userEnd: USER_END, vmaCount: obs('probe.maps.vma_count') });
  assert.equal(m.rows[0].type, 'gap');
  assert.equal(m.rows[0].edge, 'top');
  assert.equal(m.rows[0].end, USER_END);
  const last = m.rows[m.rows.length - 1];
  assert.equal(last.edge, 'bottom');
  assert.equal(last.start, 0n);
  // Contiguous from 0 to the end of user space: every byte is a row.
  let total = 0n;
  for (let i = 0; i < m.rows.length; i++) {
    total += m.rows[i].size;
    if (i) assert.equal(m.rows[i].end, m.rows[i - 1].start, `row ${i} touches the one above`);
  }
  assert.equal(total, USER_END);
  // The vsyscall page lies above user space and is listed, not drawn.
  assert.deepEqual(m.above.map((r) => r.path), ['[vsyscall]']);
  // All five regions are placed on drawn mappings.
  assert.deepEqual(m.regions.map((r) => r.name).sort(), ['anon-read', 'anon-untouched', 'anon-written', 'file-private', 'shmem-shared']);
  assert.deepEqual(m.unplaced, []);
  assert.equal(isSample(m), false);
});

test('a sample is flagged when the VMA count exceeds the rows', () => {
  const m = buildRows({ maps: obs('probe.maps.sample'), userEnd: USER_END, vmaCount: 640 });
  assert.equal(isSample(m), true);
  assert.equal(isSample(buildRows({ maps: obs('probe.maps.sample'), userEnd: USER_END })), false, 'unknown count is not claimed to be a sample');
});

test('missing optional inputs: no end of user space, no regions', () => {
  const m = buildRows({ maps: obs('probe.maps.sample'), regions: null, userEnd: null });
  assert.equal(m.rows[0].type, 'map', 'without the user range the strip starts at the highest mapping');
  assert.equal(m.above.length, 0);
  assert.deepEqual(m.regions, []);
});

test('malformed and overlapping rows are dropped and counted', () => {
  const m = buildRows({
    maps: [
      { start: '0x1000', end: '0x2000', perms: 'r-xp', kind: 'file', path: 'a' },
      { start: '0x1800', end: '0x3000', perms: 'rw-p', kind: 'anon' },
      { start: 'nope', end: '0x5000' },
      { start: '0x6000', end: '0x6000' },
      null,
    ],
    regions: [{ name: 'lost', start: '0x9000', end: '0xa000' }, { name: 'bad', start: 'x' }],
    userEnd: 0x10000n,
  });
  assert.equal(m.skipped, 4);
  assert.deepEqual(m.rows.filter((r) => r.type === 'map').map((r) => r.start), [0x1000n]);
  assert.deepEqual(m.unplaced, ['lost', 'bad']);
  assert.equal(buildRows({ maps: [] }).rows.length, 0);
});

test('a region spanning merged mappings covers every row it overlaps', () => {
  const m = buildRows({
    maps: [
      { start: '0x10000', end: '0x20000', perms: 'rw-p', kind: 'anon' },
      { start: '0x20000', end: '0x30000', perms: 'rw-p', kind: 'anon' },
    ],
    regions: [{ name: 'r', start: '0x18000', end: '0x28000' }],
    userEnd: 0x40000n,
  });
  assert.equal(m.regions[0].rows.length, 2);
});

test('layoutRows: heights grow with size, gaps are fixed, nothing overlaps', () => {
  const m = buildRows({ maps: obs('probe.maps.sample'), userEnd: USER_END });
  const { rows, height } = layoutRows(m.rows, { pageSize: 4096, gapH: 26 });
  for (const r of rows.filter((x) => x.type === 'gap')) assert.equal(r.h, 26);
  const maps = rows.filter((x) => x.type === 'map');
  const small = maps.reduce((a, b) => (a.size < b.size ? a : b));
  const big = maps.reduce((a, b) => (a.size > b.size ? a : b));
  assert.ok(big.h > small.h);
  for (let i = 1; i < rows.length; i++) assert.ok(rows[i].y >= rows[i - 1].y + rows[i - 1].h);
  assert.equal(height, rows.at(-1).y + rows.at(-1).h);
});

test('a sample never claims the space above it is unmapped', () => {
  const maps = obs('probe.maps.sample').filter((m) => !m.path?.startsWith('['));
  const kept = maps.slice(0, 6);
  const highestEnd = BigInt(obs('probe.maps.highest_end'));
  const m = buildRows({ maps: kept, userEnd: USER_END, vmaCount: 19, highestEnd });
  assert.equal(isSample(m), true);
  const band = m.rows.find((r) => r.type === 'unsampled');
  assert.ok(band, 'an unsampled band is drawn');
  assert.equal(band.count, 19 - 6);
  assert.equal(band.end, highestEnd);
  // Only the space above the highest mapping is called unmapped, to the top.
  assert.equal(m.rows[0].type, 'gap');
  assert.equal(m.rows[0].edge, 'top');
  assert.equal(m.rows[0].start, highestEnd);
  // Still contiguous from 0 to the end of user space.
  let total = 0n;
  for (const r of m.rows) total += r.size;
  assert.equal(total, USER_END);
  // Without the highest address the band runs to the end of user space.
  const m2 = buildRows({ maps: kept, userEnd: USER_END, vmaCount: 19 });
  assert.equal(m2.rows[0].type, 'unsampled');
  assert.equal(m2.rows.some((r) => r.edge === 'top'), false);
});
