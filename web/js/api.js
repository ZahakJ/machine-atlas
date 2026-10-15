// The HTTP API of ARCHITECTURE.md §10, as promises. Every response is data from a
// local server that may be serving an imported (untrusted) snapshot, so
// nothing here interprets strings: views render them with textContent.
//
// State-changing requests carry the per-process token from /api/state in
// X-Atlas-Token; the server also checks the Origin, which the browser sets.

/** An error the server or the network reported, ready to show a person. */
export class ApiError extends Error {
  constructor(status, code, message, details = []) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    // Validation problems of an import, one string each, shown verbatim.
    this.details = details;
  }
}

let token = '';

/** Remembers the token from GET /api/state for later POSTs. */
export function setToken(t) {
  token = typeof t === 'string' ? t : '';
}

async function request(method, path, { body, raw = false, signal } = {}) {
  const headers = { Accept: 'application/json' };
  if (method !== 'GET') headers['X-Atlas-Token'] = token;
  let payload;
  if (body instanceof Blob) {
    payload = body;
    headers['Content-Type'] = 'application/json';
  } else if (body !== undefined) {
    payload = JSON.stringify(body);
    headers['Content-Type'] = 'application/json';
  }
  let res;
  try {
    res = await fetch(path, { method, headers, body: payload, signal, credentials: 'same-origin', cache: 'no-store' });
  } catch (err) {
    if (err?.name === 'AbortError') throw err;
    throw new ApiError(0, 'network', 'Could not reach the Machine Atlas server. Is "atlas serve" still running?');
  }
  if (!res.ok) throw await toError(res);
  if (raw) return res;
  try {
    return await res.json();
  } catch {
    throw new ApiError(res.status, 'bad_response', `The server sent a response to ${path} that is not JSON.`);
  }
}

// Error bodies are {"error": {"code", "message", "details"?}}; anything else
// still becomes a readable message.
async function toError(res) {
  let code = `http_${res.status}`;
  let message = `${res.status} ${res.statusText || 'error'}`;
  let details = [];
  try {
    const body = await res.json();
    const e = body?.error;
    if (e && typeof e === 'object') {
      if (typeof e.code === 'string') code = e.code;
      if (typeof e.message === 'string' && e.message) message = e.message;
      if (Array.isArray(e.details)) details = e.details.filter((d) => typeof d === 'string');
    }
  } catch {
    /* not JSON: keep the status line */
  }
  return new ApiError(res.status, code, message, details);
}

const get = (path, opts) => request('GET', path, opts);
const post = (path, body, opts) => request('POST', path, { ...opts, body: body ?? {} });
const id = (s) => encodeURIComponent(s);

export const api = {
  state: () => get('/api/state'),
  snapshot: () => get('/api/snapshot'),
  derived: () => get('/api/derived'),
  catalog: () => get('/api/catalog'),
  journey: () => get('/api/journey'),
  lesson: (lessonId) => get(`/api/lessons/${id(lessonId)}`),
  components: () => get('/api/components'),
  roadmap: () => get('/api/roadmap'),
  usage: () => get('/api/usage'),
  experiments: () => get('/api/experiments'),
  runExperiment: (expId) => post(`/api/experiments/${id(expId)}/run`, { acknowledged: true }),
  collect: () => post('/api/collect'),
  importSnapshot: (file) => post('/api/snapshot/import', file),
  preview: (policy) => post('/api/snapshot/preview', { policy }),
  exportUrl: (policy) => `/api/snapshot/export?policy=${encodeURIComponent(policy)}`,
  live: () => post('/api/snapshot/live'),
  fixtures: () => get('/api/fixtures'),
  activateFixture: (name) => post(`/api/fixtures/${id(name)}/activate`),
};
