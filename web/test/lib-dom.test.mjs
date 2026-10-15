import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

// A minimal document that records how nodes were built and refuses
// innerHTML, so the test proves el() never parses markup.
class FakeNode {
  constructor(tag, ns = null) {
    Object.assign(this, { tag, ns, attrs: {}, children: [], listeners: {} });
  }
  setAttribute(name, value) { this.attrs[name] = value; }
  appendChild(child) { this.children.push(child); return child; }
  removeChild(child) { this.children.splice(this.children.indexOf(child), 1); return child; }
  get firstChild() { return this.children[0] ?? null; }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  set innerHTML(_) { throw new Error('innerHTML used'); }
}

beforeEach(() => {
  globalThis.document = {
    createElement: (tag) => new FakeNode(tag),
    createElementNS: (ns, tag) => new FakeNode(tag, ns),
    createTextNode: (text) => ({ text }),
  };
});

const { el, svg, replace } = await import('../js/lib/dom.js');

test('el sets attributes, skips empty ones and wires listeners', () => {
  const click = () => {};
  const node = el('button', { class: 'btn', disabled: true, hidden: false, title: null, 'data-n': 3, onclick: click }, 'Run');
  assert.equal(node.tag, 'button');
  assert.deepEqual(node.attrs, { class: 'btn', disabled: '', 'data-n': '3' });
  assert.equal(node.listeners.click, click);
  assert.deepEqual(node.children, [{ text: 'Run' }]);
});

test('markup in strings stays text', () => {
  const node = el('p', {}, '<img src=x onerror=alert(1)>');
  assert.deepEqual(node.children, [{ text: '<img src=x onerror=alert(1)>' }]);
});

test('children: nested arrays flatten, null and false are skipped, numbers become text', () => {
  const node = el('ul', null, [el('li', {}, 'a'), [null, el('li', {}, 'b')]], false, 7, 8n);
  assert.deepEqual(node.children.map((c) => c.tag ?? c.text), ['li', 'li', '7', '8']);
});

test('svg creates nodes in the SVG namespace', () => {
  const node = svg('rect', { width: 10 });
  assert.equal(node.ns, 'http://www.w3.org/2000/svg');
  assert.deepEqual(node.attrs, { width: '10' });
});

test('replace swaps all children', () => {
  const node = el('div', {}, 'old', el('span'));
  replace(node, 'new');
  assert.deepEqual(node.children, [{ text: 'new' }]);
});
