// Reader state kept in the browser: lesson progress and the theme choice.
// Pure apart from the storage object passed in, so tests give it a Map-backed
// fake and the app gives it window.localStorage. Storage may be missing or
// throw (private windows, disabled storage); then progress lives for the
// page only and nothing breaks.

const PROGRESS_KEY = 'machine-atlas.progress.v1';
const THEME_KEY = 'machine-atlas.theme';
const THEMES = new Set(['light', 'dark']);

/**
 * Lesson progress keyed by lesson id. Only "understood" is recorded: the
 * reader says so at the end of a lesson, and can take it back or reset all.
 * Ids that no longer exist in the curriculum are kept (a lesson may be
 * renamed back) but are not counted by the views.
 */
export function createProgress(storage) {
  let done = new Set(read());

  function read() {
    try {
      const parsed = JSON.parse(storage?.getItem(PROGRESS_KEY) ?? '{}');
      const ids = parsed && typeof parsed === 'object' && Array.isArray(parsed.understood) ? parsed.understood : [];
      return ids.filter((id) => typeof id === 'string');
    } catch {
      return [];
    }
  }

  function write() {
    try {
      storage?.setItem(PROGRESS_KEY, JSON.stringify({ understood: [...done].sort() }));
    } catch {
      /* storage full or unavailable: keep the in-memory state */
    }
  }

  const listeners = new Set();
  const changed = () => { for (const fn of listeners) fn(); };

  return {
    isDone: (id) => done.has(id),
    set(id, understood) {
      if (understood) done.add(id);
      else done.delete(id);
      write();
      changed();
    },
    reset() {
      done = new Set();
      write();
      changed();
    },
    ids: () => [...done],
    /** Calls fn after every change; returns an unsubscribe function. */
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
}

/**
 * The state of each core lesson for the journey rail:
 *   done    the reader marked it understood
 *   current it is the lesson on screen (and not done)
 *   ready   every prerequisite is done
 *   locked  some prerequisite is not done; still openable, the rail says
 *           which ones are missing
 * Prerequisites that are not core lessons (none today) are ignored, since
 * the journey cannot ask the reader to finish something it does not list.
 * Returns Map id → {state, missing: [ids]}.
 */
export function lessonStates(core, isDone, currentId = null) {
  const coreIds = new Set(core.map((l) => l.id));
  const out = new Map();
  for (const lesson of core) {
    const missing = (lesson.prerequisites ?? []).filter((p) => coreIds.has(p) && !isDone(p));
    let state;
    if (isDone(lesson.id)) state = 'done';
    else if (lesson.id === currentId) state = 'current';
    else state = missing.length === 0 ? 'ready' : 'locked';
    out.set(lesson.id, { state, missing });
  }
  return out;
}

/**
 * The lesson a reader should open next: the first core lesson, in journey
 * order, that is not done and has all prerequisites done; else the first
 * not done; else null when the journey is complete.
 */
export function nextLesson(core, isDone) {
  const states = lessonStates(core, isDone);
  return core.find((l) => states.get(l.id).state === 'ready')?.id
    ?? core.find((l) => !isDone(l.id))?.id
    ?? null;
}

/** The saved theme, "light" | "dark", or null for the system setting. */
export function loadTheme(storage) {
  try {
    const t = storage?.getItem(THEME_KEY);
    return THEMES.has(t) ? t : null;
  } catch {
    return null;
  }
}

export function saveTheme(storage, theme) {
  try {
    if (THEMES.has(theme)) storage?.setItem(THEME_KEY, theme);
    else storage?.removeItem(THEME_KEY);
  } catch {
    /* ignore */
  }
}

/**
 * The saved theme after one press of the toggle. Every press flips what is
 * on screen; when the result equals the system setting the choice is
 * cleared (null), so the page follows the system again.
 */
export function nextTheme(saved, systemDark) {
  const shown = saved ?? (systemDark ? 'dark' : 'light');
  const next = shown === 'dark' ? 'light' : 'dark';
  return next === (systemDark ? 'dark' : 'light') ? null : next;
}

/**
 * The journey's core lessons grouped into tracks, in track order: each
 * {id, title, lede, lessons: [core lesson entries]}. Core lessons no track
 * claims (an older server sends no tracks at all) form one last group, so
 * nothing written is ever hidden.
 */
export function trackGroups(journey) {
  const core = journey?.core ?? [];
  const byId = new Map(core.map((l) => [l.id, l]));
  const claimed = new Set();
  const groups = [];
  for (const t of journey?.tracks ?? []) {
    const lessons = (t.lessons ?? []).map((id) => byId.get(id)).filter(Boolean);
    lessons.forEach((l) => claimed.add(l.id));
    if (lessons.length) groups.push({ id: t.id, title: t.title, lede: t.lede ?? '', lessons });
  }
  const rest = core.filter((l) => !claimed.has(l.id));
  if (rest.length) groups.push({ id: 'other', title: groups.length ? 'Other lessons' : 'The journey', lede: '', lessons: rest });
  return groups;
}
