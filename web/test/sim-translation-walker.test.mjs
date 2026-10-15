import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  PROFILES, profileById, parseAddress, canonical, splitAddress, walk, pageSizes,
  coverage, tlbReach, levelShift, defaultExamples, observedExamples,
} from '../js/sim/translation-walker.model.js';

const KiB = 1024n, MiB = KiB * KiB, GiB = MiB * KiB, TiB = GiB * KiB;

test('every profile translates exactly its VA bits', () => {
  for (const p of PROFILES) {
    const sum = p.levels.reduce((a, l) => a + l.bits, 0) + p.offsetBits;
    assert.equal(sum, p.vaBits, p.id);
  }
});

test('the profiles named in ARCHITECTURE.md §9 exist', () => {
  for (const id of ['x86_64-4level', 'x86_64-5level', 'arm64-4k-48', 'arm64-4k-39', 'arm64-16k-47', 'arm64-64k-48', 'riscv-sv39', 'riscv-sv48']) {
    assert.ok(profileById(id), id);
  }
});

test('level shapes match the architecture manuals', () => {
  const shape = (id) => [profileById(id).levels.map((l) => `${l.name}:${l.bits}`).join(' '), profileById(id).offsetBits];
  assert.deepEqual(shape('x86_64-4level'), ['PML4:9 PDPT:9 PD:9 PT:9', 12]);
  assert.deepEqual(shape('x86_64-5level'), ['PML5:9 PML4:9 PDPT:9 PD:9 PT:9', 12]);
  assert.deepEqual(shape('arm64-4k-48'), ['L0:9 L1:9 L2:9 L3:9', 12]);
  assert.deepEqual(shape('arm64-4k-39'), ['L1:9 L2:9 L3:9', 12]);
  assert.deepEqual(shape('arm64-16k-47'), ['L1:11 L2:11 L3:11', 14]);
  assert.deepEqual(shape('arm64-16k-48'), ['L0:1 L1:11 L2:11 L3:11', 14]);
  assert.deepEqual(shape('arm64-64k-48'), ['L1:6 L2:13 L3:13', 16]);
  assert.deepEqual(shape('riscv-sv39'), ['VPN[2]:9 VPN[1]:9 VPN[0]:9', 12]);
  assert.deepEqual(shape('riscv-sv48'), ['VPN[3]:9 VPN[2]:9 VPN[1]:9 VPN[0]:9', 12]);
});

// Addresses composed by hand from chosen indices: index_k << shift_k | offset.
const VECTORS = [
  ['x86_64-4level', 0x7ffd5e8c3a10n, [255, 501, 244, 195], 0xa10n],
  ['x86_64-4level', 0xffff3f205123n, [511, 508, 505, 5], 0x123n],
  ['x86_64-5level', 0x1fffe7e7ec05123n, [511, 508, 505, 502, 5], 0x123n],
  ['arm64-4k-48', 0xffff3f205123n, [511, 508, 505, 5], 0x123n],
  ['arm64-4k-39', 0x7fff805123n, [511, 508, 5], 0x123n],
  ['arm64-16k-47', 0x7ffff8014123n, [2047, 2044, 5], 0x123n],
  ['arm64-16k-48', 0xffcff2014123n, [1, 2044, 2041, 5], 0x123n],
  ['arm64-64k-48', 0xffff80050123n, [63, 8188, 5], 0x123n],
  ['riscv-sv39', 0x7fff805123n, [511, 508, 5], 0x123n],
  ['riscv-sv48', 0xffff3f205123n, [511, 508, 505, 5], 0x123n],
];

test('index arithmetic matches hand-composed vectors', () => {
  for (const [id, addr, indices, offset] of VECTORS) {
    const fields = splitAddress(profileById(id), addr);
    assert.deepEqual(fields.filter((f) => f.kind === 'index').map((f) => Number(f.value)), indices, id);
    assert.equal(fields.at(-1).value, offset, id);
  }
});

test('fields tile bits 63..0 without gaps', () => {
  for (const p of PROFILES) {
    for (let leaf = 0; leaf < p.levels.length; leaf++) {
      const f = splitAddress(p, 0n, leaf);
      assert.equal(f[0].hi, 63);
      assert.equal(f.at(-1).lo, 0);
      for (let i = 1; i < f.length; i++) assert.equal(f[i].hi, f[i - 1].lo - 1, `${p.id} leaf ${leaf}`);
    }
  }
});

test('canonical checks per architecture', () => {
  const cases = [
    ['x86_64-4level', 0x00007fffffffffffn, 'user'],
    ['x86_64-4level', 0x0000800000000000n, null],
    ['x86_64-4level', 0xffff800000000000n, 'kernel'],
    ['x86_64-4level', 0xffff7fffffffffffn, null],
    ['x86_64-5level', 0x00ffffffffffffffn, 'user'],
    ['x86_64-5level', 0x0100000000000000n, null],
    ['x86_64-5level', 0xff00000000000000n, 'kernel'],
    // arm64: bit va-1 is an ordinary index bit; only bits 63..va choose the half.
    ['arm64-4k-48', 0x0000800000000000n, 'user'],
    ['arm64-4k-48', 0xffff000000000000n, 'kernel'],
    ['arm64-4k-48', 0x0001000000000000n, null],
    ['arm64-4k-39', 0x0000007fffffffffn, 'user'],
    ['arm64-4k-39', 0x0000008000000000n, null],
    ['arm64-4k-39', 0xffffff8000000000n, 'kernel'],
    ['arm64-16k-47', 0x0000800000000000n, null],
    ['arm64-16k-47', 0xffff800000000000n, 'kernel'],
    ['arm64-64k-48', 0xffff000000000000n, 'kernel'],
    ['riscv-sv39', 0x0000003fffffffffn, 'user'],
    ['riscv-sv39', 0x0000004000000000n, null],
    ['riscv-sv39', 0xffffffc000000000n, 'kernel'],
    ['riscv-sv48', 0x0000800000000000n, null],
    ['riscv-sv48', 0xffff800000000000n, 'kernel'],
  ];
  for (const [id, addr, half] of cases) {
    const c = canonical(profileById(id), addr);
    assert.equal(c.ok, half !== null, `${id} ${addr.toString(16)}`);
    assert.equal(c.half, half, `${id} ${addr.toString(16)}`);
  }
  assert.equal(canonical(profileById('arm64-4k-48'), 0xffff000000000000n).register, 'TTBR1_EL1');
  assert.match(canonical(profileById('x86_64-4level'), 0x0000800000000000n).consequence, /#GP/);
});

test('page sizes per profile', () => {
  const sizes = (id) => pageSizes(profileById(id)).map((s) => s.bytes);
  assert.deepEqual(sizes('x86_64-4level'), [4n * KiB, 2n * MiB, GiB]);
  assert.deepEqual(sizes('x86_64-5level'), [4n * KiB, 2n * MiB, GiB]);
  assert.deepEqual(sizes('arm64-4k-39'), [4n * KiB, 2n * MiB, GiB]);
  assert.deepEqual(sizes('arm64-16k-47'), [16n * KiB, 32n * MiB]);
  assert.deepEqual(sizes('arm64-16k-48'), [16n * KiB, 32n * MiB]);
  assert.deepEqual(sizes('arm64-64k-48'), [64n * KiB, 512n * MiB]);
  assert.deepEqual(sizes('riscv-sv39'), [4n * KiB, 2n * MiB, GiB]);
  // The privileged spec: "Sv48 supports 2 MiB megapages, 1 GiB gigapages,
  // and 512 GiB terapages"; the terapage is a leaf in the root table.
  assert.deepEqual(sizes('riscv-sv48'), [4n * KiB, 2n * MiB, GiB, 512n * GiB]);
  const tera = walk(profileById('riscv-sv48'), 0x7f8012345678n, 0);
  assert.equal(tera.steps.length, 1);
  assert.equal(tera.pageBytes, 512n * GiB);
  assert.equal(tera.offset, 0x7f8012345678n & (512n * GiB - 1n));
  assert.ok(tera.frame > 0n && tera.frame % (512n * GiB) === 0n, 'a terapage frame is non-zero and 512 GiB aligned');
});

test('coverage of one entry at each level', () => {
  const cov = (id) => coverage(profileById(id)).map((c) => c.entryBytes);
  assert.deepEqual(cov('x86_64-4level'), [512n * GiB, GiB, 2n * MiB, 4n * KiB]);
  assert.deepEqual(cov('x86_64-5level'), [256n * TiB, 512n * GiB, GiB, 2n * MiB, 4n * KiB]);
  assert.deepEqual(cov('arm64-16k-48'), [128n * TiB, 64n * GiB, 32n * MiB, 16n * KiB]);
  assert.deepEqual(cov('arm64-64k-48'), [4n * TiB, 512n * MiB, 64n * KiB]);
  // The root table spans the whole translated range (one half for arm64).
  for (const p of PROFILES) assert.equal(coverage(p)[0].tableBytes, 1n << BigInt(p.vaBits), p.id);
  assert.equal(levelShift(profileById('x86_64-4level'), 3), 12);
});

test('TLB reach is entries times page size', () => {
  assert.equal(tlbReach(64, 4096n), 256n * KiB);
  assert.equal(tlbReach(1536, 2n * MiB), 3n * GiB);
});

test('walk: one step per level, exact offsets, aligned invented addresses', () => {
  const p = profileById('x86_64-4level');
  const w = walk(p, 0x7ffd5e8c3a10n);
  assert.equal(w.steps.length, 4);
  assert.deepEqual(w.steps.map((s) => s.entryOffset), [255n * 8n, 501n * 8n, 244n * 8n, 195n * 8n]);
  for (const s of w.steps) {
    assert.equal(s.tableBase % 4096n, 0n);
    assert.equal(s.entryAddress, s.tableBase + s.entryOffset);
  }
  for (let i = 1; i < 4; i++) assert.equal(w.steps[i].tableBase, w.steps[i - 1].target);
  assert.equal(w.physical, w.frame + 0xa10n);
  assert.equal(w.frameNumber, w.frame >> 12n);
  assert.ok(w.physical < 1n << 36n);
});

test('walk: a huge page ends early and widens the offset', () => {
  const p = profileById('x86_64-4level');
  const addr = 0x7ffd5e8c3a10n;
  const two = walk(p, addr, 2);
  assert.equal(two.steps.length, 3);
  assert.equal(two.pageBytes, 2n * MiB);
  assert.equal(two.offset, addr & (2n * MiB - 1n));
  assert.equal(two.frame % (2n * MiB), 0n);
  assert.equal(walk(p, addr, 1).steps.length, 2);
  const arm = walk(profileById('arm64-64k-48'), 0xffff80050123n, 1);
  assert.equal(arm.pageBytes, 512n * MiB);
  assert.throws(() => walk(p, addr, 0), RangeError);
});

test('walk: deterministic, and arm64 halves use different roots', () => {
  const p = profileById('arm64-4k-48');
  assert.deepEqual(walk(p, 0x1234567000n), walk(p, 0x1234567000n));
  const user = walk(p, 0x0000001234567000n);
  const kernel = walk(p, 0xffff001234567000n);
  assert.notEqual(user.steps[0].tableBase, kernel.steps[0].tableBase);
  const x86 = profileById('x86_64-4level');
  assert.equal(walk(x86, 0x1000n).steps[0].tableBase, walk(x86, 0xffff800000001000n).steps[0].tableBase);
});

test('walk: a non-canonical address has no steps', () => {
  const w = walk(profileById('x86_64-4level'), 0x0000800000000000n);
  assert.equal(w.canonical.ok, false);
  assert.deepEqual(w.steps, []);
  assert.equal(w.physical, null);
});

test('parseAddress', () => {
  const ok = [
    ['0x7ffd_5e8c_3a10', 0x7ffd5e8c3a10n],
    ['7FFD5E8C3A10', 0x7ffd5e8c3a10n],
    ['  0xffffffffffffffff ', 0xffffffffffffffffn],
    ["0x7ffd'5e8c", 0x7ffd5e8cn],
    ['0', 0n],
  ];
  for (const [text, value] of ok) assert.deepEqual(parseAddress(text), { ok: true, value }, text);
  for (const bad of ['', '0x', 'xyz', '0x1g', '0x10000000000000000', '-1']) {
    assert.equal(parseAddress(bad).ok, false, bad);
  }
});

test('default examples are canonical except the one meant not to be', () => {
  for (const p of PROFILES) {
    for (const ex of defaultExamples(p)) {
      const c = canonical(p, ex.address);
      if (ex.label.startsWith('non-canonical')) assert.equal(c.ok, false, `${p.id} ${ex.label}`);
      else if (ex.label.startsWith('kernel')) assert.equal(c.half, 'kernel', `${p.id} ${ex.label}`);
      else assert.equal(c.half, 'user', `${p.id} ${ex.label}`);
    }
  }
});

test('observed examples come from probe tables and skip bad rows', () => {
  const tables = {
    'probe.regions': [{ name: 'anon-written', start: '0x7f3e9b600000' }, { name: 'broken', start: 'n/a' }],
    'probe.layout.runs': [{ run: 1, stack: '0x7ffc4a1d2000', mmap_base: '0x7f3ea4e00000', heap: '0x55b1c5e41000', exe: '0x55b1c3a00000' }],
  };
  const ex = observedExamples((id) => tables[id]);
  assert.deepEqual(ex.map((e) => e.address), [0x7f3e9b600000n, 0x55b1c3a00000n, 0x55b1c5e41000n, 0x7f3ea4e00000n, 0x7ffc4a1d2000n]);
  assert.equal(ex[0].source, 'probe.regions');
  assert.deepEqual(observedExamples(() => undefined), []);
});
