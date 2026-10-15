import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { IDS, LAYERS, stackModel } from '../js/diagrams/io-stack.model.js';
import { read } from '../js/diagrams/inputs.js';

function ctxOf(name) {
  const d = JSON.parse(readFileSync(new URL(`../dev/data/${name}.json`, import.meta.url)));
  const obs = new Map(d.observations.map((o) => [o.id, o]));
  const der = new Map(d.derived.map((f) => [f.id, f]));
  return { obs: (id) => obs.get(id), derived: (id) => der.get(id) };
}
const model = (name) => {
  const ctx = ctxOf(name);
  return stackModel((id) => read(ctx, id));
};
const hereOf = (m) => m.rows.flatMap((r) => r.here);

test('seven layers from the system call down to the device', () => {
  const m = model('desktop-x86_64');
  assert.deepEqual(m.rows.map((r) => r.layer), LAYERS.map((l) => l.layer));
  assert.equal(m.rows.length, 7);
  for (const r of m.rows) assert.ok(r.title && r.body, r.layer);
});

test('the desktop shows its SATA disk, mq-deadline, one hardware queue and a volatile cache', () => {
  const m = model('desktop-x86_64');
  const here = hereOf(m);
  assert.ok(here.includes('Scheduler: mq-deadline (also offered: kyber, bfq, none)'), here.join(' | '));
  assert.ok(here.includes('One hardware queue, shared by every CPU'));
  assert.ok(here.includes('Root disk: sda, a SCSI-layer disk (SATA, SAS or USB)'));
  assert.ok(here.includes('Volatile write cache: fsync must send a flush'));
  assert.ok(here.includes('/ is ext4 (rw,relatime)'));
  assert.ok(here.some((h) => h.startsWith('Readahead up to 128 KiB')));
  assert.ok(here.some((h) => /dirty; writers are held at/.test(h)));
  assert.deepEqual(m.missing, []);
  assert.equal(m.rootNote, '');
  assert.ok(m.observed);
  assert.ok(m.used.every((id) => IDS.includes(id)));
});

test('the Pi reports an SD card without a volatile cache', () => {
  const here = hereOf(model('rpi-arm64-noswap'));
  assert.ok(here.includes('Root disk: mmcblk0, an SD card or eMMC'), here.join(' | '));
  assert.ok(here.includes('No volatile write cache reported: a completed write is durable'));
});

test('a restricted machine says once why the disk is unknown, and still names the filesystem', () => {
  const m = model('restricted');
  assert.match(m.rootNote, /not known here/);
  assert.ok(hereOf(m).some((h) => h.startsWith('/ is ext4')));
  assert.ok(!m.missing.some((r) => r.id === 'derived.storage.root_scheduler'));
  assert.ok(m.missing.length < IDS.length);
});

test('with nothing readable it draws only the general layers', () => {
  const m = stackModel(() => undefined);
  assert.equal(m.observed, false);
  assert.deepEqual(hereOf(m), []);
  assert.ok(m.rootNote.length > 0);
});

let FAKE_WIDTH = 900;

// Mounting into a minimal fake document: the drawing builds without
// markup and carries the machine's values as text.
class FakeNode {
  constructor(tag) {
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
  const { mount } = await import('../js/diagrams/io-stack.js');
  for (const width of [900, 390]) {
    FAKE_WIDTH = width;
    for (const name of ['desktop-x86_64', 'numa-server-la57', 'restricted', 'rpi-arm64-noswap']) {
      const container = new FakeNode('div');
      const unmount = mount(container, ctxOf(name));
      const all = texts(container).join(' | ');
      assert.ok(all.includes('I/O scheduler') && all.includes('The device'), `${name} at ${width}`);
      if (name === 'desktop-x86_64') assert.ok(all.includes('Here: Scheduler: mq-deadline'), all);
      if (name === 'numa-server-la57') assert.ok(all.includes('Here: Root disk: sda'), all);
      if (name === 'restricted') assert.ok(all.includes('not known here'), all);
      unmount();
    }
  }
  delete globalThis.document;
});
