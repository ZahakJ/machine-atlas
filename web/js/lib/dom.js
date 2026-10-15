// Tiny DOM builders. Everything the UI shows passes through here, and here
// only createElement(NS), createTextNode and setAttribute are used: text is
// never parsed as markup, so a string from a snapshot cannot inject HTML
// (ARCHITECTURE.md §9). Nothing runs at import time, so pure modules that import
// this file stay testable under node.

const SVG_NS = 'http://www.w3.org/2000/svg';

/**
 * Creates an HTML element.
 *
 *   el('button', { class: 'btn', type: 'button', onclick: go }, 'Run')
 *
 * Attributes: a string or number is set with setAttribute; `true` sets an
 * empty (boolean) attribute; false, null and undefined are skipped; a
 * function under an `on…` name becomes an event listener. Children may be
 * nodes, strings, numbers, nested arrays, or null/false (skipped).
 */
export function el(tag, attrs, ...children) {
  return build(document.createElement(tag), attrs, children);
}

/** Like el(), for SVG elements (created in the SVG namespace). */
export function svg(tag, attrs, ...children) {
  return build(document.createElementNS(SVG_NS, tag), attrs, children);
}

/** Replaces all children of `node` with `children` (same rules as el()). */
export function replace(node, ...children) {
  while (node.firstChild) node.removeChild(node.firstChild);
  append(node, children);
  return node;
}

function build(node, attrs, children) {
  for (const [name, value] of Object.entries(attrs ?? {})) {
    if (value === false || value === null || value === undefined) continue;
    if (typeof value === 'function' && name.startsWith('on')) {
      node.addEventListener(name.slice(2), value);
    } else {
      node.setAttribute(name, value === true ? '' : String(value));
    }
  }
  append(node, children);
  return node;
}

function append(node, children) {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    if (Array.isArray(child)) append(node, child);
    else if (typeof child === 'string' || typeof child === 'number' || typeof child === 'bigint') {
      node.appendChild(document.createTextNode(String(child)));
    } else node.appendChild(child);
  }
}
