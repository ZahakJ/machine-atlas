import { test } from 'node:test';
import assert from 'node:assert/strict';

// A small fake document, enough to mount the fair-share view under node and
// press every button: the view must never throw and must keep showing the
// tasks, whatever the order of actions.
class Node {
  constructor(tag) {
    Object.assign(this, { tag, attrs: {}, children: [], listeners: {}, value: '', disabled: false });
    this.style = { setProperty: (k, v) => { this.attrs[`style:${k}`] = v; } };
  }
  setAttribute(k, v) { this.attrs[k] = v; if (k === 'value') this.value = v; }
  appendChild(c) { this.children.push(c); return c; }
  append(...cs) { for (const c of cs) this.appendChild(c); }
  removeChild(c) { this.children.splice(this.children.indexOf(c), 1); }
  remove() {}
  get firstChild() { return this.children[0] ?? null; }
  addEventListener(type, fn) { (this.listeners[type] ??= []).push(fn); }
  fire(type) { for (const fn of this.listeners[type] ?? []) fn({ preventDefault() {}, currentTarget: this }); }
}

function all(n, pred, out = []) {
  if (n && typeof n === 'object') {
    if (pred(n)) out.push(n);
    for (const c of n.children) all(c, pred, out);
  }
  return out;
}

function text(n) {
  return typeof n === 'string' ? n : n.children.map(text).join('');
}

test('the fair-share view mounts, survives every control and keeps its labels', async () => {
  globalThis.document = {
    createElement: (t) => new Node(t),
    createElementNS: (_, t) => new Node(t),
    createTextNode: (s) => String(s),
  };
  const { mount } = await import('../js/sim/fair-share.view.js');
  const host = new Node('div');
  const ok = (value) => ({ status: 'ok', value });
  const unmount = mount(host, {
    obs: (id) => ({ 'sched.self.nice': ok(10), 'sched.self.slice_us': ok(2800) })[id],
    derived: (id) => ({ 'derived.sched.fair_scheduler': ok('eevdf') })[id],
  });
  assert.equal(typeof unmount, 'function');
  assert.match(text(host), /atlas/);
  assert.match(text(host), /Simulated/);
  for (let round = 0; round < 3; round++) {
    for (const b of all(host, (n) => n.tag === 'button')) b.fire('click');
    for (const s of all(host, (n) => n.tag === 'select')) {
      const opts = all(s, (n) => n.tag === 'option');
      s.value = opts[(round + 1) % opts.length]?.attrs.value ?? '';
      s.fire('change');
    }
  }
  all(host, (n) => n.tag === 'button' && /One decision/.test(text(n)))[0].fire('click');
  assert.ok(all(host, (n) => n.attrs.class === 'rc__event').length > 0, 'the log has events');
  const empty = new Node('div');
  mount(empty, {});
  assert.match(text(empty), /Defaults in use/);
});
