// Hash routes and link safety. Pure functions, no DOM: app.js listens for
// hashchange and hands the parsed route to the view (ARCHITECTURE.md §9).
//
//   #/                       overview
//   #/learn                  the journey
//   #/learn/<lesson>         one lesson
//   #/explore[/<component>]  the component map
//   #/inspect[/<id>]         observations and derived facts
//
// Any route may carry a query (#/inspect?status=failed) for view state that
// is worth sharing, such as Inspect's filters.

const VIEWS = new Set(['learn', 'explore', 'inspect']);

/**
 * Parses a location hash into {view, id, params}. view is "overview",
 * "learn", "explore", "inspect" or "notfound"; id is the decoded second
 * segment or null; params is a plain object of the query. Never throws:
 * a malformed escape makes the route "notfound" rather than breaking the app.
 */
export function parseHash(hash) {
  let path = String(hash ?? '').replace(/^#/, '');
  let query = '';
  const q = path.indexOf('?');
  if (q >= 0) {
    query = path.slice(q + 1);
    path = path.slice(0, q);
  }
  const params = {};
  for (const [k, v] of new URLSearchParams(query)) params[k] = v;

  const parts = path.split('/').filter((p) => p !== '');
  if (parts.length === 0) return { view: 'overview', id: null, params };
  const [view, raw, ...rest] = parts;
  if (!VIEWS.has(view) || rest.length > 0) return { view: 'notfound', id: null, params, path };
  if (raw === undefined) return { view, id: null, params };
  let id;
  try {
    id = decodeURIComponent(raw);
  } catch {
    return { view: 'notfound', id: null, params, path };
  }
  return { view, id, params };
}

/**
 * Builds a hash for a route: href('inspect', 'mem.meminfo.MemTotal') →
 * "#/inspect/mem.meminfo.MemTotal". Empty params are dropped so links stay
 * short.
 */
export function href(view, id = null, params = {}) {
  let out = view === 'overview' ? '#/' : `#/${view}`;
  if (id !== null && id !== undefined && id !== '') out += '/' + encodeURIComponent(id);
  const query = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') query.set(k, String(v));
  }
  const qs = query.toString();
  return qs ? `${out}?${qs}` : out;
}

/** True when two parsed routes show the same page (ignoring params). */
export function samePage(a, b) {
  return !!a && !!b && a.view === b.view && a.id === b.id;
}

/**
 * Checks a link target that came from data. Returns {href, external} when it
 * is safe to put in an <a href>, or null:
 *   - absolute http: and https: URLs (external; the caller opens them in a
 *     new tab with rel="noopener noreferrer"),
 *   - internal routes that parse to a known view ("#/learn/page-faults").
 * Everything else (javascript:, data:, relative paths, protocol-relative
 * "//host", URLs with credentials) is refused.
 */
export function safeHref(target) {
  if (typeof target !== 'string' || target === '' || /[\u0000-\u001f\s]/.test(target)) return null;
  if (target.startsWith('#/') || target === '#') {
    return parseHash(target).view === 'notfound' ? null : { href: target, external: false };
  }
  if (!/^https?:\/\//i.test(target)) return null;
  let url;
  try {
    url = new URL(target);
  } catch {
    return null;
  }
  if ((url.protocol !== 'https:' && url.protocol !== 'http:') || url.username || url.password || !url.hostname) return null;
  return { href: url.href, external: true };
}

/**
 * The internal route a lesson inline link points at. Bound lessons carry
 * links as {kind, href}: kind url|lesson|component|obs and href the URL or
 * the bare id (ARCHITECTURE.md §7). Returns a hash, an external URL, or null.
 */
export function linkTarget(kind, target) {
  switch (kind) {
    case 'lesson': return validId(target) ? safeHref(href('learn', target)) : null;
    case 'component': return validId(target) ? safeHref(href('explore', target)) : null;
    case 'obs': return validId(target) ? safeHref(href('inspect', target)) : null;
    case 'url': return safeHref(target);
    default: return null;
  }
}

// Lesson, component, observation and derived ids share this alphabet
// (ARCHITECTURE.md §4); anything else is refused before it becomes a route.
function validId(id) {
  return typeof id === 'string' && /^[a-z][A-Za-z0-9_.-]*$/.test(id);
}
