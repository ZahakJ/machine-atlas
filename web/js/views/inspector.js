// The side inspector: opened by a chip, an evidence row, a component
// observation or a diagram citation, it shows the full detail of one id
// without leaving the page.
//
// Below 1440 px it is a drawer over the page (with a scrim on narrow
// screens). In Learn at 1440 px and wider it is docked as a third column
// that is always present, showing a hint while empty, so opening it never
// moves the lesson text.

import { el } from '../lib/dom.js';
import { renderDetail } from '../render/detail.js';
import { errorBox, idText, loading } from '../render/common.js';

const NARROW = '(max-width: 1099px)';

export function createInspector(session, announce) {
  const title = el('h2', { class: 'inspector__title', id: 'inspector-title', tabindex: '-1' }, 'Inspector');
  const eyebrow = el('p', { class: 'eyebrow' }, 'Inspector');
  const closeBtn = el('button', { class: 'btn btn--icon btn--quiet', type: 'button', 'aria-label': 'Close inspector', onclick: () => close() }, '✕');
  const body = el('div', { class: 'inspector__body' });
  const aside = el('aside', { class: 'inspector', 'aria-labelledby': 'inspector-title', 'data-open': 'false' },
    el('header', { class: 'inspector__header' }, eyebrow, title, closeBtn), body);
  const scrim = el('div', { class: 'inspector-scrim', hidden: true, onclick: () => close() });

  let current = null;
  let opener = null;
  let docked = false;
  let token = 0;

  function showEmpty() {
    aside.dataset.empty = 'true';
    eyebrow.textContent = 'Inspector';
    title.replaceChildren('Where a value comes from');
    body.replaceChildren(el('p', { class: 'caption inspector__hint' },
      'Select any value in the lesson, marked by an underline, to see its exact reading, its source, when it was collected, what it cannot tell you and what else uses it.'));
  }

  async function open(id, from = null) {
    current = id;
    opener = from;
    delete aside.dataset.empty;
    const mine = ++token;
    const found = session.lookup(id);
    eyebrow.textContent = found?.type === 'derived' ? 'Derived fact' : found ? 'Observation' : 'Not in this snapshot';
    title.replaceChildren(el('code', {}, idText(id)));
    body.replaceChildren(loading());
    aside.dataset.open = 'true';
    scrim.hidden = !matchMedia(NARROW).matches;
    if (from) for (const c of document.querySelectorAll('.chip[aria-expanded="true"]')) c.setAttribute('aria-expanded', 'false');
    from?.classList?.contains('chip') && from.setAttribute('aria-expanded', 'true');
    // Focus the heading so a keyboard or screen reader user lands in the
    // panel, docked or not: its place in the DOM is after the lesson, so
    // otherwise they would have to Tab through the rest of the page.
    // Escape or the close button returns focus to the opener.
    focusTitle(mine);
    announce?.(`Inspector shows ${id}`);
    try {
      const detail = await renderDetail(session, id, { compact: true });
      if (mine === token) body.replaceChildren(detail);
    } catch (err) {
      if (mine === token) body.replaceChildren(errorBox(err, () => open(id, from)));
    }
  }

  // A panel that is still becoming visible refuses focus; try again on the
  // next frames until it lands, as long as this opening is still current.
  function focusTitle(mine, tries = 10) {
    if (mine !== token || aside.dataset.open !== 'true') return;
    title.focus({ preventScroll: true });
    if (document.activeElement !== title && tries > 0) requestAnimationFrame(() => focusTitle(mine, tries - 1));
  }

  function close() {
    if (aside.dataset.open !== 'true' && current === null) return;
    current = null;
    token++;
    aside.dataset.open = 'false';
    scrim.hidden = true;
    for (const c of document.querySelectorAll('.chip[aria-expanded="true"]')) c.setAttribute('aria-expanded', 'false');
    if (docked) showEmpty();
    if (opener?.isConnected) opener.focus();
    opener = null;
  }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && aside.dataset.open === 'true' && !document.querySelector('dialog[open]')) {
      e.preventDefault();
      close();
    }
  });

  return {
    open,
    close,
    get current() { return current; },
    /**
     * Docks the inspector into a .layout container (Learn) or returns it to
     * the body (every other view). Docking reserves its column at wide
     * widths, so it shows a hint until something is opened.
     */
    dock(layout) {
      docked = !!layout;
      if (layout) {
        layout.classList.add('layout--with-inspector');
        layout.append(aside);
        if (current === null) showEmpty();
      } else {
        document.body.append(scrim, aside);
        if (current === null) close();
      }
    },
    /** Re-renders the open id after the active snapshot changed. */
    refresh() {
      if (current) open(current, null);
    },
  };
}
