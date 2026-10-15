import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createProgress, lessonStates, nextLesson, loadTheme, saveTheme, nextTheme } from '../js/store.js';

function memoryStorage(initial = {}) {
  const m = new Map(Object.entries(initial));
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => m.set(k, String(v)),
    removeItem: (k) => m.delete(k),
    dump: () => Object.fromEntries(m),
  };
}

test('progress persists, survives reload and resets', () => {
  const storage = memoryStorage();
  const p = createProgress(storage);
  let calls = 0;
  p.subscribe(() => calls++);
  p.set('cpu-addressing', true);
  p.set('mappings', true);
  p.set('mappings', false);
  assert.equal(calls, 3);
  assert.deepEqual(createProgress(storage).ids(), ['cpu-addressing']);
  p.reset();
  assert.deepEqual(createProgress(storage).ids(), []);
});

test('progress tolerates corrupt, hostile or missing storage', () => {
  for (const raw of ['not json', 'null', '[]', '{"understood": "x"}', '{"understood": [1, null, "ok"]}']) {
    const p = createProgress(memoryStorage({ 'machine-atlas.progress.v1': raw }));
    assert.deepEqual(p.ids().filter((id) => id !== 'ok'), [], raw);
  }
  const throwing = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); } };
  const p = createProgress(throwing);
  p.set('a', true);
  assert.equal(p.isDone('a'), true, 'kept in memory when storage throws');
  assert.equal(createProgress(undefined).isDone('a'), false);
});

const core = [
  { id: 'a', prerequisites: [] },
  { id: 'b', prerequisites: ['a'] },
  { id: 'c', prerequisites: ['a', 'b', 'planned-thing'] },
  { id: 'd', prerequisites: [] },
];

test('lessonStates: done, current, ready and locked with the missing list', () => {
  const done = new Set(['a']);
  const s = lessonStates(core, (id) => done.has(id), 'c');
  assert.deepEqual(s.get('a'), { state: 'done', missing: [] });
  assert.deepEqual(s.get('b'), { state: 'ready', missing: [] });
  assert.deepEqual(s.get('c'), { state: 'current', missing: ['b'] }, 'the open lesson is current even when locked');
  assert.deepEqual(s.get('d'), { state: 'ready', missing: [] });
  assert.deepEqual(lessonStates(core, () => false).get('c'), { state: 'locked', missing: ['a', 'b'] }, 'non-core prerequisites are ignored');
});

test('nextLesson prefers the first ready lesson', () => {
  assert.equal(nextLesson(core, () => false), 'a');
  assert.equal(nextLesson(core, (id) => id === 'a'), 'b');
  assert.equal(nextLesson(core, (id) => id !== 'd'), 'd');
  assert.equal(nextLesson(core, () => true), null);
  assert.equal(nextLesson([], () => false), null);
});

test('theme: saved choice and toggling always changes what is shown', () => {
  const storage = memoryStorage();
  assert.equal(loadTheme(storage), null);
  saveTheme(storage, 'dark');
  assert.equal(loadTheme(storage), 'dark');
  saveTheme(storage, null);
  assert.equal(loadTheme(storage), null);
  saveTheme(storage, 'evil');
  assert.equal(loadTheme(storage), null);
  assert.equal(loadTheme(memoryStorage({ 'machine-atlas.theme': 'purple' })), null);

  const shown = (saved, sys) => saved ?? (sys ? 'dark' : 'light');
  for (const sys of [false, true]) {
    for (const saved of [null, 'light', 'dark']) {
      assert.notEqual(shown(nextTheme(saved, sys), sys), shown(saved, sys), `${saved} on ${sys ? 'dark' : 'light'} system`);
    }
  }
  assert.equal(nextTheme('dark', true), 'light');
  assert.equal(nextTheme('light', true), null, 'back to following the system');
});

test('trackGroups follows the tracks and keeps unclaimed lessons', async () => {
  const { trackGroups } = await import('../js/store.js');
  const core = [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }];
  const groups = trackGroups({
    core,
    tracks: [
      { id: 'memory', title: 'Memory', lede: 'm', lessons: ['a', 'b'] },
      { id: 'cpu', title: 'CPU', lede: 'c', lessons: ['c', 'missing'] },
      { id: 'empty', title: 'Empty', lede: '', lessons: [] },
    ],
  });
  assert.deepEqual(groups.map((g) => [g.id, g.lessons.map((l) => l.id)]), [['memory', ['a', 'b']], ['cpu', ['c']], ['other', ['d']]]);
  assert.deepEqual(trackGroups({ core }).map((g) => [g.title, g.lessons.length]), [['The journey', 4]]);
  assert.deepEqual(trackGroups(null), []);
});
