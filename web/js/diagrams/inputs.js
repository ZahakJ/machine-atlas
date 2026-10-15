// Reads observations and derived facts through a diagram's ctx and reports
// what is missing and why. Pure: no DOM. A diagram draws only values whose
// status is ok; everything else becomes a sentence naming the id, its status
// and the collector's or the rule's own explanation.

import { statusWords } from './words.js';

/**
 * Looks up `id` (an observation, or a derived fact when it starts with
 * "derived.") through ctx and returns a uniform record:
 *   {id, kind: 'obs'|'derived', ok, status, value, unit, note}
 * An id the snapshot does not contain at all has status "missing".
 */
export function read(ctx, id) {
  const derived = id.startsWith('derived.');
  const kind = derived ? 'derived' : 'obs';
  let rec;
  try {
    rec = derived ? ctx?.derived?.(id) : ctx?.obs?.(id);
  } catch {
    rec = undefined;
  }
  if (!rec || typeof rec !== 'object') {
    return { id, kind, ok: false, status: 'missing', value: null, unit: '', note: 'not in this snapshot' };
  }
  const status = rec.status ?? 'missing';
  return {
    id,
    kind,
    ok: status === 'ok' && rec.value !== null && rec.value !== undefined,
    status,
    value: rec.value ?? null,
    unit: rec.unit ?? '',
    note: (derived ? rec.reason : rec.note) ?? '',
    inputs: Array.isArray(rec.inputs) ? rec.inputs : undefined,
    rule: rec.rule,
  };
}

/**
 * Reads several ids. Returns {ok, get(id) -> record, missing: [record]}:
 * ok is true when every id was read with status ok. A record with status
 * ok but a value of the wrong shape counts as missing (status "failed",
 * with a note), because drawing it would mean guessing.
 *
 * `checks` optionally maps an id to a predicate on its value.
 */
export function readAll(ctx, ids, checks = {}) {
  const map = new Map();
  const missing = [];
  for (const id of ids) {
    let r = read(ctx, id);
    if (r.ok && checks[id] && !checks[id](r.value)) {
      r = { ...r, ok: false, status: 'failed', note: 'its value does not have the expected shape' };
    }
    map.set(id, r);
    if (!r.ok) missing.push(r);
  }
  return { ok: missing.length === 0, get: (id) => map.get(id), missing };
}

/** One sentence saying why `rec` cannot be drawn. */
export function whyMissing(rec) {
  const status = rec.status === 'ok' ? 'empty' : statusWords(rec.status);
  return rec.note ? `${rec.id} is ${status}: ${rec.note}` : `${rec.id} is ${status}`;
}

export const isNumber = (v) => typeof v === 'number' && Number.isFinite(v);
export const isTable = (v) => Array.isArray(v) && v.every((r) => r && typeof r === 'object' && !Array.isArray(r));
