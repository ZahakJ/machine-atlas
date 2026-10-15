import { test } from 'node:test';
import assert from 'node:assert/strict';

// A small fake document: just enough DOM for the inspector. Focus behaves
// like a browser's: an element inside a panel that is not visible yet
// refuses focus, and the panel becomes visible only after a frame (as it
// does while its opening transition starts).
class Node {
  constructor(tag) {
    Object.assign(this, { tag, attrs: {}, children: [], parent: null, dataset: {}, listeners: {}, hidden: false });
    this.classList = { contains: (c) => (this.attrs.class ?? '').split(' ').includes(c) };
  }
  setAttribute(k, v) { this.attrs[k] = v; if (k === 'hidden') this.hidden = true; }
  getAttribute(k) { return this.attrs[k] ?? null; }
  removeAttribute(k) { delete this.attrs[k]; }
  appendChild(c) { if (c && typeof c === 'object') c.parent = this; this.children.push(c); return c; }
  append(...cs) { for (const c of cs) this.appendChild(c); }
  replaceChildren(...cs) { this.children = []; this.append(...cs); }
  get firstChild() { return this.children[0] ?? null; }
  removeChild(c) { this.children.splice(this.children.indexOf(c), 1); }
  set textContent(t) { this.children = [t]; }
  addEventListener(type, fn) { (this.listeners[type] ??= []).push(fn); }
  get isConnected() { return true; }
  focus() {
    let n = this;
    while (n) {
      if (n.dataset.visible === 'false') return;
      n = n.parent;
    }
    globalThis.document.activeElement = this;
  }
}

function setup() {
  const frames = [];
  globalThis.requestAnimationFrame = (fn) => frames.push(fn);
  globalThis.matchMedia = () => ({ matches: false, addEventListener() {} });
  globalThis.document = {
    activeElement: null,
    body: new Node('body'),
    createElement: (t) => new Node(t),
    createElementNS: (_, t) => new Node(t),
    createTextNode: (s) => String(s),
    querySelector: () => null,
    querySelectorAll: () => [],
    addEventListener() {},
  };
  return () => { while (frames.length) frames.shift()(); };
}

test('opening the inspector from a chip moves focus to its title, once the panel can take it', async () => {
  const flush = setup();
  const { createInspector } = await import('../js/views/inspector.js');
  const session = { lookup: () => null, load: () => Promise.reject(new Error('offline')) };
  const insp = createInspector(session, () => {});
  insp.dock(null);
  const aside = document.body.children.find((c) => c.tag === 'aside');
  const title = aside.children[0].children.find((c) => c.tag === 'h2');
  // The panel is still hidden when open() runs, as in a browser mid-transition.
  aside.dataset.visible = 'false';
  const chip = new Node('button');
  chip.attrs.class = 'chip';
  chip.focus();
  const opening = insp.open('mem.zones', chip);
  assert.equal(document.activeElement, chip, 'first try refused while hidden');
  aside.dataset.visible = 'true';
  flush();
  assert.equal(document.activeElement, title, 'focus lands on #inspector-title');
  assert.equal(title.attrs.id, 'inspector-title');
  await opening;
  // Closing returns focus to the chip that opened it.
  insp.close();
  assert.equal(document.activeElement, chip);
});
