import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { IDS, pathModel } from '../js/diagrams/packet-path.model.js';
import { read } from '../js/diagrams/inputs.js';

// A ctx over one of the dev fixture files, as the diagram page builds it.
function ctxOf(name) {
  const d = JSON.parse(readFileSync(new URL(`../dev/data/${name}.json`, import.meta.url)));
  const obs = new Map(d.observations.map((o) => [o.id, o]));
  const der = new Map(d.derived.map((f) => [f.id, f]));
  return { obs: (id) => obs.get(id), derived: (id) => der.get(id) };
}
const model = (name) => {
  const ctx = ctxOf(name);
  return pathModel((id) => read(ctx, id));
};
const hereOf = (m) => m.rows.flatMap((r) => [...r.send.here, ...r.receive.here]);

test('six layers in both directions, from the socket down to the NIC', () => {
  const m = model('desktop-x86_64');
  assert.deepEqual(m.rows.map((r) => r.layer), ['Socket', 'Protocol', 'IP', 'Device', 'Driver', 'NIC']);
  for (const r of m.rows) {
    assert.ok(r.send.title && r.send.body && r.receive.title && r.receive.body, r.layer);
  }
});

test('the desktop shows its default route, qdisc, MTU and conntrack', () => {
  const m = model('desktop-x86_64');
  const here = hereOf(m);
  assert.ok(here.includes('Default route: enp3s0'), here.join(' | '));
  assert.ok(here.includes('Qdisc: fq_codel on enp3s0'));
  assert.ok(here.some((h) => h.startsWith('Frames of at most 1') && h.endsWith('bytes (MTU)')));
  assert.ok(here.some((h) => h.startsWith('Connection tracking:') && h.endsWith('flows')));
  assert.ok(m.observed);
  assert.deepEqual(m.missing, []);
  assert.ok(m.used.every((id) => IDS.includes(id)));
});

test('the server names the qdisc of its bond members and its backlog drops', () => {
  const here = hereOf(model('numa-server-la57'));
  assert.ok(here.includes('Qdisc: fq on bond0, 16 transmit queues'), here.join(' | '));
  assert.ok(here.some((h) => h.startsWith('Backlog limit:') && h.endsWith('17 dropped since boot')));
  assert.ok(here.includes('Congestion control: bbr'));
});

test('without conntrack the Pi says it is not loaded, and nothing is missing for it', () => {
  const m = model('rpi-arm64-noswap');
  assert.ok(hereOf(m).includes('Connection tracking: not loaded'));
  assert.ok(!m.missing.some((r) => r.id === 'net.conntrack.count'));
});

test('the restricted machine falls back to the default qdisc and lists what it cannot read', () => {
  const m = model('restricted');
  const here = hereOf(m);
  assert.ok(here.includes('Default qdisc for new interfaces: fq_codel'), here.join(' | '));
  assert.ok(!here.some((h) => h.startsWith('Default route')));
  const missing = m.missing.map((r) => r.id);
  assert.ok(missing.includes('derived.net.default_route_interface'));
  assert.ok(missing.includes('derived.net.default_route_qdisc'));
});

test('an empty snapshot still draws the general path, observing nothing', () => {
  const m = pathModel(() => undefined);
  assert.equal(m.rows.length, 6);
  assert.equal(m.observed, false);
  assert.deepEqual(hereOf(m), []);
  assert.equal(m.missing.length, IDS.length); // a conntrack count not in the snapshot at all is a gap too
});

let FAKE_WIDTH = 900;

// Mounting into a minimal fake document: the drawing builds without
// markup and carries the machine's values as text.
class FakeNode {
  constructor(tag) {
    // clientWidth decides the layout; FAKE_WIDTH switches it per run.
    Object.assign(this, { tag, attrs: {}, children: [], clientWidth: FAKE_WIDTH });
  }
  setAttribute(name, value) { this.attrs[name] = value; }
  appendChild(child) { this.children.push(child); return child; }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; }
  addEventListener() {}
  remove() {}
  set innerHTML(_) { throw new Error('innerHTML used'); }
}
function texts(n, out = []) {
  if (n && typeof n.text === 'string') out.push(n.text);
  for (const c of n?.children ?? []) texts(c, out);
  return out;
}

test('mounts in a fake document for every dev fixture, wide and narrow', async () => {
  globalThis.document = {
    createElement: (tag) => new FakeNode(tag),
    createElementNS: (_, tag) => new FakeNode(tag),
    createTextNode: (text) => ({ text }),
  };
  const { mount } = await import('../js/diagrams/packet-path.js');
  for (const width of [900, 390]) {
    FAKE_WIDTH = width;
    for (const name of ['desktop-x86_64', 'numa-server-la57', 'restricted', 'rpi-arm64-noswap']) {
      const container = new FakeNode('div');
      const unmount = mount(container, ctxOf(name));
      const all = texts(container).join(' | ');
      assert.ok(all.includes('Sending') && all.includes('The wire or the radio'), `${name} at ${width}`);
      assert.equal(all.includes('Receiving, from the NIC up'), width < 640, `${name} at ${width}: layout`);
      if (name === 'desktop-x86_64') assert.ok(all.includes('Here: Default route: enp3s0'), all);
      unmount();
    }
  }
  delete globalThis.document;
});
