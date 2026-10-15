import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { topologyModel, parseCPUList, cacheName, cacheLine } from '../js/diagrams/cpu-topology.model.js';

const data = (name) => JSON.parse(readFileSync(new URL(`../dev/data/${name}.json`, import.meta.url)));
const obs = (name, id) => data(name).observations.find((o) => o.id === id);
const model = (name) => topologyModel(obs(name, 'cpu.topology').value, obs(name, 'cpu.cache_domains')?.value ?? null,
  { isolated: parseCPUList(obs(name, 'cpu.isolated')?.value ?? '') ?? [] });

test('parseCPUList reads the kernel list format and rejects anything else', () => {
  assert.deepEqual(parseCPUList('0-3,8,10-11'), [0, 1, 2, 3, 8, 10, 11]);
  assert.deepEqual(parseCPUList(''), []);
  for (const bad of ['3-1', '1,1', 'x', '1-', null, 4]) assert.equal(parseCPUList(bad), null, String(bad));
  assert.equal(cacheName({ level: 1, type: 'Data' }), 'L1d');
  assert.equal(cacheName({ level: 3, type: 'Unified' }), 'L3');
  assert.equal(cacheLine({ name: 'L3', size: 1024, cpus: [0, 1] }, (n) => `${n} B`), 'L3 1024 B, shared by 2 CPUs');
});

test('desktop: one package, eight two-thread cores, private L1 and L2, one shared L3', () => {
  const m = model('desktop-x86_64');
  assert.equal(m.packages.length, 1);
  assert.equal(m.cores, 8);
  assert.equal(m.threads, 16);
  assert.equal(m.hybrid, false);
  const p = m.packages[0];
  assert.deepEqual(p.shared.map((c) => c.name), ['L3']);
  assert.equal(p.groups.length, 1, 'no clusters: one unboxed group');
  const first = p.groups[0].cores[0];
  assert.deepEqual(first.threads, [0, 8]);
  assert.deepEqual(first.caches.map((c) => c.name), ['L1d', 'L1i', 'L2']);
});

test('numa server: two packages of sixteen cores, isolated CPUs marked', () => {
  const m = model('numa-server-la57');
  assert.deepEqual(m.packages.map((p) => p.cpus.length), [32, 32]);
  assert.equal(m.cores, 32);
  assert.deepEqual(m.packages.map((p) => p.shared.map((c) => c.name)), [['L3'], ['L3']]);
  const iso = m.packages.flatMap((p) => p.groups.flatMap((g) => g.cores.flatMap((c) => c.isolated)));
  assert.deepEqual(iso.sort((a, b) => a - b), [8, 9, 10, 11, 12, 13, 14, 15, 40, 41, 42, 43, 44, 45, 46, 47]);
  assert.equal(m.machineShared.length, 0);
});

test('Raspberry Pi: four single-thread cores sharing the L2', () => {
  const m = model('rpi-arm64-noswap');
  assert.equal(m.threads, 4);
  assert.deepEqual(m.packages[0].shared.map((c) => c.name), ['L2']);
  assert.equal(m.hybrid, false, 'equal capacities');
});

test('restricted: nothing to draw from a denied table', () => {
  const o = obs('restricted', 'cpu.topology');
  assert.equal(o.status, 'permission_denied');
  const m = topologyModel(o.value, null);
  assert.equal(m.packages.length, 0);
  assert.equal(m.cachesKnown, false);
});

test('hybrid: efficiency cores sharing an L2 form one cluster group', () => {
  const topo = [
    { cpu: 0, package: 0, core_id: 0, thread_siblings: '0-1', core_type: 'performance' },
    { cpu: 1, package: 0, core_id: 0, thread_siblings: '0-1', core_type: 'performance' },
    ...[2, 3, 4, 5].map((c) => ({ cpu: c, package: 0, core_id: 8 + c, thread_siblings: String(c), core_type: 'efficiency' })),
  ];
  const caches = [
    { level: 1, type: 'Data', size_bytes: 49152, shared_cpu_list: '0-1' },
    { level: 2, type: 'Unified', size_bytes: 1310720, shared_cpu_list: '0-1' },
    ...[2, 3, 4, 5].map((c) => ({ level: 1, type: 'Data', size_bytes: 32768, shared_cpu_list: String(c) })),
    { level: 2, type: 'Unified', size_bytes: 2097152, shared_cpu_list: '2-5' },
    { level: 3, type: 'Unified', size_bytes: 12582912, shared_cpu_list: '0-5' },
    { level: 3, type: 'Unified', size_bytes: 1, shared_cpu_list: '0-9' },
  ];
  const m = topologyModel(topo, caches);
  assert.equal(m.hybrid, true);
  assert.equal(m.cacheSkipped, 1, 'a cache naming CPUs outside the table');
  const g = m.packages[0].groups;
  assert.equal(g.length, 2);
  assert.equal(g[0].cache, null);
  assert.deepEqual(g[0].cores.map((c) => c.coreType), ['performance']);
  assert.equal(g[1].cache.name, 'L2');
  assert.deepEqual(g[1].cores.map((c) => c.threads[0]), [2, 3, 4, 5]);
  assert.deepEqual(m.packages[0].shared.map((c) => c.name), ['L3']);
});

test('malformed topology rows are skipped, not guessed', () => {
  const m = topologyModel([{ cpu: 0, package: 0, thread_siblings: '0' }, { cpu: 1, package: 0, thread_siblings: '0' }, { cpu: 'x' }, null], null);
  assert.equal(m.skipped, 3);
  assert.equal(m.threads, 1);
});

// Mount the diagram on a minimal fake DOM against every fixture.
class Node {
  constructor(tag) { Object.assign(this, { tag, attrs: {}, children: [] }); }
  setAttribute(k, v) { this.attrs[k] = v; }
  appendChild(c) { this.children.push(c); return c; }
  append(...cs) { for (const c of cs) this.appendChild(c); }
  replaceChildren(...cs) { this.children = []; this.append(...cs); }
  remove() {}
  addEventListener() {}
}
const textOf = (n) => (typeof n === 'string' ? n : n.children.map(textOf).join(' '));

test('the diagram mounts on every fixture and says why when it cannot draw', async () => {
  globalThis.document = { createElement: (t) => new Node(t), createElementNS: (_, t) => new Node(t), createTextNode: (s) => String(s) };
  const { mount } = await import('../js/diagrams/cpu-topology.js');
  for (const name of ['desktop-x86_64', 'numa-server-la57', 'rpi-arm64-noswap', 'restricted']) {
    const d = data(name);
    const o = new Map(d.observations.map((x) => [x.id, x]));
    const host = new Node('div');
    const unmount = mount(host, { obs: (id) => o.get(id), derived: () => undefined });
    assert.equal(typeof unmount, 'function');
    const t = textOf(host);
    if (name === 'restricted') assert.match(t, /cpu\.topology is permission denied/);
    else assert.match(t, /Package 0/, name);
    if (name === 'numa-server-la57') assert.match(t, /isolated/);
  }
});
