// Machine Atlas in the browser: builds the shell, loads the active snapshot
// and routes between the overview and the three views (ARCHITECTURE.md §9).
//
// Each view is created by a factory and exposes mount(root, route),
// optionally update(route) for a new route within the same view (a lesson
// to the next lesson, an Inspect row to another) so the page around it is
// not rebuilt, and unmount().

import { el } from './lib/dom.js';
import { parseHash, href } from './router.js';
import { createProgress } from './store.js';
import { createSession } from './views/session.js';
import { createShell, safeStorage } from './views/shell.js';
import { createInspector } from './views/inspector.js';
import { createOverview } from './views/overview.js';
import { createLearn } from './views/learn.js';
import { createExplore } from './views/explore.js';
import { createInspect } from './views/inspect.js';
import { errorBox, loading } from './render/common.js';

const VIEWS = { overview: createOverview, learn: createLearn, explore: createExplore, inspect: createInspect };

const session = createSession();
const progress = createProgress(safeStorage());
const liveRegion = el('div', { class: 'visually-hidden', role: 'status', 'aria-live': 'polite' });

const app = {
  session,
  progress,
  /** Says something to screen readers without moving focus. */
  announce(text) {
    liveRegion.textContent = '';
    // A fresh text node after a frame makes repeated messages audible.
    requestAnimationFrame(() => { liveRegion.textContent = text; });
  },
  navigate(hash) {
    location.hash = hash;
  },
  /**
   * The ctx simulations and diagrams receive. A diagram citing an id calls
   * navigate('#/inspect/<id>'); that opens the side inspector in place, so a
   * reader never loses the lesson to follow a citation.
   */
  simCtx() {
    return {
      // live | snapshot | fixture: lets a view say where its data came from.
      mode: session.state?.mode,
      obs: (id) => session.obsById.get(id),
      derived: (id) => session.factById.get(id),
      navigate: (hash) => {
        const r = parseHash(hash);
        if (r.view === 'inspect' && r.id) app.inspector.open(r.id, document.activeElement);
        else if (r.view !== 'notfound') location.hash = hash;
      },
      inspect: (id, opener) => app.inspector.open(id, opener ?? document.activeElement),
    };
  },
};
app.inspector = createInspector(session, app.announce);
const shell = createShell(app);
const viewRoot = el('div', { class: 'view-root' });

let current = null; // {name, view, route}

async function route({ focus = true } = {}) {
  const r = parseHash(location.hash);
  if (current && current.name === r.view && current.view.update) {
    const handled = await current.view.update(r, current.route);
    if (handled !== false) {
      current.route = r;
      setTitle(r);
      return;
    }
  }
  current?.view.unmount?.();
  app.inspector.close();
  shell.setView(r.view);
  window.scrollTo(0, 0);
  if (r.view === 'notfound') {
    current = { name: 'notfound', view: {}, route: r };
    viewRoot.replaceChildren(el('main', { class: 'page', id: 'main', tabindex: '-1' },
      el('h1', { class: 'lesson__title', tabindex: '-1' }, 'Nothing at this address'),
      el('p', {}, 'Machine Atlas has no page at ', el('code', {}, location.hash), '.'),
      el('p', {}, el('a', { href: href('overview') }, 'Go to the overview'))));
    document.title = 'Not found · Machine Atlas';
  } else {
    const view = VIEWS[r.view](app);
    current = { name: r.view, view, route: r };
    await view.mount(viewRoot, r);
    setTitle(r);
  }
  // Move focus to the new page's heading so keyboard and screen reader
  // users start at the top of what changed.
  if (focus) viewRoot.querySelector('h1')?.focus({ preventScroll: true });
}

function setTitle(r) {
  const t = current?.view.title?.(r);
  document.title = t && t !== 'Overview' ? `${t} · Machine Atlas` : 'Machine Atlas';
}

async function start() {
  const body = document.body;
  body.replaceChildren(
    el('a', { class: 'skip-link', href: '#main', onclick: skipToMain }, 'Skip to content'),
    shell.topbar, shell.banner, viewRoot, liveRegion);
  app.inspector.dock(null);
  viewRoot.replaceChildren(el('main', { class: 'page', id: 'main' }, loading('Loading this machine’s snapshot')));
  try {
    await session.start();
  } catch (err) {
    viewRoot.replaceChildren(el('main', { class: 'page', id: 'main' },
      errorBox(err, start, 'Machine Atlas could not load its data')));
    return;
  }
  shell.render();
  session.onChange(() => {
    shell.render();
    app.inspector.close();
    // Same route, new data: rebuild the view from scratch.
    current?.view.unmount?.();
    current = null;
    route({ focus: false });
  });
  window.addEventListener('hashchange', () => route());
  await route({ focus: false });
}

// The skip link targets #main, which the hash router would read as a
// route; move focus by hand instead.
function skipToMain(e) {
  e.preventDefault();
  const main = document.getElementById('main');
  main?.focus();
}

start();
