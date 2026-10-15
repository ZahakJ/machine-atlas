// The session: what the server says is active (live machine, imported
// snapshot or fixture) and everything fetched for it. Views read through
// here so each resource is requested once per active snapshot, and a mode
// change (re-collect, import, fixture, back to live) drops every cache in
// one place.

import { api, setToken } from '../api.js';
import { catalogLookup } from '../render/values.js';

const RESOURCES = {
  journey: api.journey,
  roadmap: api.roadmap,
  components: api.components,
  usage: api.usage,
  // {entries, families, unknown}: entries for the ids present, every
  // wildcard family, and the ids this version does not describe.
  catalog: async () => {
    const body = await api.catalog();
    const entries = [...(body?.entries ?? []), ...(body?.families ?? [])];
    return { entries, unknown: new Set(body?.unknown ?? []), lookup: catalogLookup(entries) };
  },
  experiments: async () => {
    const list = await api.experiments();
    return new Map((Array.isArray(list) ? list : []).map((e) => [e.id, e]));
  },
};

export function createSession() {
  let cache = new Map();
  let lessons = new Map();
  let generation = 0;
  const listeners = new Set();

  const session = {
    state: null,
    snapshot: null,
    facts: [],
    obsById: new Map(),
    factById: new Map(),
    generation: () => generation,

    /** Loads /api/state, the snapshot and the derived facts. */
    async start() {
      const state = await api.state();
      setToken(state?.token);
      const [snapshot, derived] = await Promise.all([api.snapshot(), api.derived()]);
      const facts = Array.isArray(derived) ? derived : [];
      session.state = state;
      session.snapshot = snapshot;
      session.facts = facts;
      session.obsById = new Map((snapshot?.observations ?? []).map((o) => [o.id, o]));
      session.factById = new Map(facts.map((f) => [f.id, f]));
    },

    /**
     * Runs a request that changes the active snapshot, then reloads
     * everything and tells the listeners (the shell and the current view).
     */
    async change(request) {
      const result = await request();
      cache = new Map();
      lessons = new Map();
      generation++;
      await session.start();
      for (const fn of listeners) fn();
      return result;
    },

    onChange(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },

    /** A cached resource by name (journey, roadmap, components, usage, catalog, experiments). */
    load(name) {
      if (!cache.has(name)) {
        const p = RESOURCES[name]();
        // A failed request is not cached, so "Try again" really retries.
        p.catch(() => cache.delete(name));
        cache.set(name, p);
      }
      return cache.get(name);
    },

    lesson(id) {
      if (!lessons.has(id)) {
        const p = api.lesson(id);
        p.catch(() => lessons.delete(id));
        lessons.set(id, p);
      }
      return lessons.get(id);
    },

    /** An observation or derived fact by id, with its type, or null. */
    lookup(id) {
      if (session.obsById.has(id)) return { type: 'obs', item: session.obsById.get(id) };
      if (session.factById.has(id)) return { type: 'derived', item: session.factById.get(id) };
      return null;
    },

    /** live (this build can collect), collect (now), experiments (now). */
    capability(name) {
      return !!session.state?.capabilities?.[name];
    },
  };
  return session;
}
