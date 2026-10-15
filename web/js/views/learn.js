// Learn (#/learn, #/learn/<lesson>): the journey rail, the journey page with
// its prerequisite graph, and the lesson itself with the side inspector
// docked beside it.

import { el, replace } from '../lib/dom.js';
import { href } from '../router.js';
import { lessonStates, nextLesson, trackGroups } from '../store.js';
import { chip, errorBox, labelKey, loading, statusPill, titleOf } from '../render/common.js';
import { renderLesson, versionNotes, references } from '../render/lesson.js';
import { layered } from '../render/layout.js';
import { drawGraph, NODE_W, NODE_H } from '../render/graph.js';
import { shortValue } from '../render/values.js';

const STATE_NOTE = { done: 'Understood', current: 'Reading now', ready: 'Ready' };

export function createLearn(app) {
  const { session, progress } = app;
  const open = (id, from) => app.inspector.open(id, from);

  let root = null;
  let railBox = null;
  let mainBox = null;
  let currentId = null;
  let journey = null;
  let unmountLesson = null;
  let unsubscribe = null;
  let mediaQuery = null;
  let railDetails = null;

  // ---- rail

  function renderRail() {
    if (!railBox || !journey) return;
    const core = journey.core ?? [];
    const states = lessonStates(core, progress.isDone, currentId);
    const done = core.filter((l) => progress.isDone(l.id)).length;
    const item = (state, id, title, note, attrs = {}) => {
      const current = id === currentId;
      const inner = [el('span', { class: 'rail__marker', 'aria-hidden': 'true' }, attrs.marker ?? ''), el('span', { class: 'rail__title' }, title), note ? el('span', { class: 'rail__note' }, note) : null];
      return el('li', { class: 'rail__item', 'data-state': state },
        id ? el('a', { class: 'rail__link', href: href('learn', id), 'aria-current': current ? 'page' : null }, inner)
          : el('span', { class: 'rail__link', 'data-static': 'true' }, inner));
    };
    const branches = journey.branches ?? { relevant: [], available: [] };
    const planned = journey.planned ?? { relevant: [], available: [] };
    const groups = trackGroups(journey);
    const reading = groups.find((g) => g.lessons.some((l) => l.id === currentId));
    replace(railBox,
      el('p', { class: 'rail__heading' }, el('a', { href: href('learn'), class: 'rail__home' }, 'All lessons')),
      el('p', { class: 'rail__progress caption' }, `${done} of ${core.length} understood`),
      groups.map((g, gi) => {
        const gDone = g.lessons.filter((l) => progress.isDone(l.id)).length;
        return el('details', { class: 'rail__track', open: reading ? reading === g : gi === 0 },
          el('summary', { class: 'rail__track-title' }, g.title, el('span', { class: 'caption num' }, ` ${gDone}/${g.lessons.length}`)),
          el('ol', { class: 'rail__list' }, g.lessons.map((l, i) => {
            const s = states.get(l.id);
            const note = s.state === 'locked' ? `Needs: ${s.missing.map((m) => titleOf(journey, m)).join(', ')}` : STATE_NOTE[s.state];
            return item(s.state, l.id, l.title, note, { marker: String(i + 1) });
          })));
      }),
      branches.relevant.length || branches.available.length ? el('p', { class: 'rail__heading' }, 'Branches') : null,
      branches.relevant.length || branches.available.length
        ? el('ul', { class: 'rail__list' },
          branches.relevant.map((b) => item('relevant', b.id, b.title, `Relevant: ${matchedNote(b) ?? 'applies here'}`)),
          branches.available.map((b) => item('available', b.id, b.title, 'Available')))
        : null,
      el('p', { class: 'rail__heading' }, 'Not written yet'),
      el('ul', { class: 'rail__list' },
        planned.relevant.map((p) => item('planned', null, p.title, `Relevant here: ${matchedNote(p) ?? ''}`))),
      planned.available.length
        ? el('details', { class: 'rail__more' },
          el('summary', {}, `${planned.available.length} more planned concepts`),
          el('ul', { class: 'rail__list' }, planned.available.map((p) => item('planned', null, p.title, 'Planned'))))
        : null);
  }

  // ---- journey page

  function journeyPage() {
    const core = journey.core ?? [];
    const states = lessonStates(core, progress.isDone);
    const next = nextLesson(core, progress.isDone);
    const branches = journey.branches ?? { relevant: [], available: [] };
    const planned = journey.planned ?? { relevant: [], available: [] };
    const done = core.filter((l) => progress.isDone(l.id)).length;
    const reset = el('button', { class: 'btn btn--small btn--quiet', type: 'button', disabled: done === 0 }, 'Reset progress');
    reset.addEventListener('click', () => {
      if (confirm('Forget which lessons you marked as understood?')) {
        progress.reset();
        app.announce('Progress reset.');
      }
    });

    const groups = trackGroups(journey);
    return el('div', { class: 'journey' },
      el('header', { class: 'lesson__header' },
        el('p', { class: 'eyebrow' }, 'Learn'),
        el('h1', { class: 'lesson__title', tabindex: '-1' }, groups.length === 1 ? groups[0].title : `${countWords(groups.length)} tracks through this machine`),
        el('p', { class: 'lede' }, `${core.length} core lessons in all. Each lesson binds its explanation to readings from this machine and says plainly what those readings cannot show.`),
        el('div', { class: 'lesson__meta cluster' },
          el('span', {}, `${done} of ${core.length} lessons understood`),
          next ? el('a', { class: 'btn btn--primary btn--small', href: href('learn', next) }, done ? 'Continue' : 'Start', ': ', titleOf(journey, next), ' →') : null,
          reset),
        groups.length > 1
          ? el('nav', { class: 'journey__tracks cluster', 'aria-label': 'Tracks' }, groups.map((g) => el('a', { href: `#j-track-${g.id}`, 'data-track-link': g.id }, g.title)))
          : null),

      groups.map((g) => {
        const gDone = g.lessons.filter((l) => progress.isDone(l.id)).length;
        const gNext = nextLesson(g.lessons, progress.isDone);
        return el('section', { class: 'journey__section journey__track', 'aria-labelledby': `j-track-${g.id}` },
          el('h2', { class: 'journey__h2', id: `j-track-${g.id}` }, g.title),
          g.lede ? el('p', { class: 'journey__lede' }, g.lede) : null,
          el('p', { class: 'meta-row' },
            el('span', {}, `${stepWords(g.lessons.length)} · ${gDone} understood`),
            gNext && gDone < g.lessons.length ? el('a', { href: href('learn', gNext) }, gDone ? 'Continue' : 'Start', ': ', titleOf(journey, gNext), ' →') : null),
          el('ol', { class: 'journey-list' }, g.lessons.map((l, i) => {
            const s = states.get(l.id);
            return el('li', { class: 'journey-item', 'data-state': s.state },
              el('span', { class: 'journey-item__no', 'aria-hidden': 'true' }, String(i + 1)),
              el('div', { class: 'journey-item__body' },
                el('h3', { class: 'journey-item__title' }, el('a', { href: href('learn', l.id) }, l.title)),
                el('p', { class: 'journey-item__summary' }, l.summary),
                el('p', { class: 'meta-row' },
                  el('span', { class: 'journey-item__state' }, stateWords(s, journey)),
                  el('span', {}, `· evidence ${l.coverage?.ok ?? 0} of ${l.coverage?.total ?? 0} available here`)),
                notesList(l.notes)));
          })));
      }),

      el('section', { class: 'journey__section', 'aria-labelledby': 'j-layers' },
        el('h2', { class: 'journey__h2', id: 'j-layers' }, 'The memory journey as layers'),
        el('p', { class: 'caption' }, 'The same lessons stacked as the mechanisms rest on one another, from the CPU’s translation at the bottom to reclaim at the top.'),
        layersDiagram()),

      el('section', { class: 'journey__section', 'aria-labelledby': 'j-graph' },
        el('h2', { class: 'journey__h2', id: 'j-graph' }, 'How the concepts depend on each other'),
        el('p', { class: 'caption' }, 'Each arrow means “understand this first”. Written lessons are links; dashed boxes are planned concepts that are not written yet.'),
        prerequisiteGraph()),

      el('section', { class: 'journey__section', 'aria-labelledby': 'j-branches' },
        el('h2', { class: 'journey__h2', id: 'j-branches' }, 'Branch lessons'),
        branches.relevant.length + branches.available.length === 0
          ? el('p', { class: 'empty' }, 'No branch lessons are written yet. Branches go deeper into one mechanism and are suggested when this machine’s configuration makes them relevant.')
          : el('div', { class: 'split' },
            optionColumn('Relevant to this machine', branches.relevant, true),
            optionColumn('Available', branches.available, true))),

      el('section', { class: 'journey__section', 'aria-labelledby': 'j-planned' },
        el('h2', { class: 'journey__h2', id: 'j-planned' }, 'Planned concepts, not written yet'),
        el('p', { class: 'caption' }, 'The roadmap beyond the written tracks. Each is listed with why it would matter here, when this machine gives a reason.'),
        el('div', { class: 'split' },
          optionColumn('Relevant to this machine', planned.relevant, false),
          optionColumn('Planned', planned.available, false))));
  }

  // "Nine steps" for the lede; the count comes from the journey, never typed.
  function stepWords(n) {
    const words = ['No steps', 'One step', 'Two steps', 'Three steps', 'Four steps', 'Five steps', 'Six steps', 'Seven steps', 'Eight steps', 'Nine steps', 'Ten steps', 'Eleven steps', 'Twelve steps'];
    return words[n] ?? `${n} steps`;
  }

  function countWords(n) {
    return ['No', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine'][n] ?? String(n);
  }

  function optionColumn(title, list, written) {
    return el('div', { class: 'split__col' },
      el('h3', { class: 'split__h' }, title, el('span', { class: 'caption' }, ` ${list.length}`)),
      list.length === 0 ? el('p', { class: 'caption' }, 'None.') : el('ul', { class: 'option-list' }, list.map((o) => el('li', { class: 'option-item', 'data-written': written ? 'true' : 'false' },
        el('p', { class: 'option-item__title' }, written ? el('a', { href: href('learn', o.id) }, o.title) : o.title,
          el('span', { class: 'option-item__sub caption' }, ` · ${o.subsystem}${written ? '' : ' · not written yet'}`)),
        el('p', { class: 'option-item__summary' }, o.summary),
        notesList(o.notes, written)))));
  }

  // Relevance notes: matched ones say why this matters here; an unknown
  // fact says it could not be decided, never that it does not apply.
  function notesList(notes, showUnmatched = false) {
    const shown = (notes ?? []).filter((n) => n.matched || n.status !== 'ok' || showUnmatched);
    if (shown.length === 0) return null;
    return el('ul', { class: 'notes' }, shown.map((n) => el('li', { class: 'note', 'data-matched': n.matched ? 'true' : 'false' },
      el('span', { class: 'note__flag' }, n.matched ? 'On this machine' : n.status !== 'ok' ? 'Could not tell' : 'Not here'),
      ' ', n.text, ' ',
      chip({ ref: n.when, kind: n.when.startsWith('derived.') ? 'derived' : 'obs', display: n.value || '—', status: n.status, title: n.when }, open),
      n.reason ? el('span', { class: 'caption' }, ` (${n.reason})`) : null)));
  }

  // The graph shows the written lessons, the planned concepts next to them
  // and those relevant here; the full roadmap of planned concepts is one
  // switch away, since forty dashed boxes would bury the eight that exist.
  let showAllPlanned = false;

  function prerequisiteGraph() {
    const box = el('div', { class: 'graph-box' });
    const all = journey.graph ?? { nodes: [], edges: [] };
    const relevant = new Set((journey.planned?.relevant ?? []).map((p) => p.id));
    const written = new Set(all.nodes.filter((n) => n.written).map((n) => n.id));
    const near = new Set();
    for (const e of all.edges) {
      if (written.has(e.from)) near.add(e.to);
      if (written.has(e.to)) near.add(e.from);
    }
    const hidden = all.nodes.filter((n) => !n.written && !relevant.has(n.id) && !near.has(n.id)).length;
    const toggle = el('input', { type: 'checkbox', checked: showAllPlanned });
    const draw = () => {
      const keep = (n) => showAllPlanned || n.written || relevant.has(n.id) || near.has(n.id);
      const nodes = all.nodes.filter(keep);
      const ids = new Set(nodes.map((n) => n.id));
      box.replaceChildren(drawPrereq(nodes, all.edges.filter((e) => ids.has(e.from) && ids.has(e.to))));
    };
    toggle.addEventListener('change', () => { showAllPlanned = toggle.checked; draw(); });
    draw();
    return el('div', { class: 'stack' },
      hidden ? el('label', { class: 'check' }, toggle, ` Show every planned concept (${hidden} more)`) : null,
      box);
  }

  function drawPrereq(graphNodes, graphEdges) {
    const states = lessonStates(journey.core ?? [], progress.isDone, currentId);
    const { columns, place } = layered(graphNodes, graphEdges);
    const gapX = 56;
    const gapY = 14;
    const rows = Math.max(1, ...columns.map((c) => c.length));
    const width = columns.length * (NODE_W + gapX) - gapX + 8;
    const height = rows * (NODE_H + gapY) - gapY + 8;
    const nodes = graphNodes.map((n) => {
      const p = place.get(n.id);
      const st = states.get(n.id)?.state;
      const kindWords = n.kind === 'planned' ? 'planned, not written yet' : n.kind === 'branch' ? 'branch lesson' : `core lesson, ${STATE_NOTE[st]?.toLowerCase() ?? 'earlier lessons not marked understood yet'}`;
      return {
        id: n.id,
        x: 4 + p.col * (NODE_W + gapX),
        y: 4 + p.row * (NODE_H + gapY),
        title: n.title,
        href: n.written ? href('learn', n.id) : null,
        label: `${n.title} (${kindWords})`,
        mark: st === 'done' ? '✓' : null,
        attrs: { 'data-kind': n.kind, 'data-state': st ?? null, 'data-current': n.id === currentId ? 'true' : null },
      };
    });
    const edges = graphEdges.map((e) => ({ ...e, cls: 'graph__edge--prereq', marker: 'arrow' }));
    const hint = el('p', { class: 'graph-hint caption', hidden: true }, `Wider than the page: scroll sideways (or focus the graph and use the arrow keys) to see all ${columns.length} columns. A shadow marks the side with more.`);
    const wrap = el('div', { class: 'graph-wrap', tabindex: '0', role: 'region', 'aria-label': 'Prerequisite graph, scrollable' },
      drawGraph({ nodes, edges, width, height, label: 'Prerequisite graph of the lessons and planned concepts', className: 'graph--prereq' }));
    // Once laid out, say so when the graph overflows its box.
    requestAnimationFrame(() => { hint.hidden = !(wrap.isConnected && wrap.scrollWidth > wrap.clientWidth + 2); });
    return el('div', {}, hint, wrap);
  }

  // The authored memory-layers diagram (GENERAL): the journey on one page.
  let unmountLayers = null;

  function layersDiagram() {
    unmountLayers?.();
    unmountLayers = null;
    const host = el('figure', { class: 'diagram-slot', 'data-diagram': 'memory-layers' }, loading('Loading the diagram'));
    let gone = false;
    unmountLayers = () => { gone = true; };
    import('../diagrams/index.js').then((mod) => {
      if (gone) return;
      const d = mod.diagrams?.['memory-layers'];
      host.replaceChildren();
      host.setAttribute('aria-label', d.title);
      const stop = d.mount(host, app.simCtx()) ?? null;
      unmountLayers = () => { gone = true; stop?.(); };
    }).catch(() => {
      if (!gone) host.replaceChildren(el('p', { class: 'caption' }, 'The diagram is not available in this build.'));
    });
    return host;
  }

  // ---- lesson page

  async function lessonPage(id) {
    const [lesson, catalog, experiments] = await Promise.all([
      session.lesson(id),
      session.load('catalog').catch(() => null),
      session.load('experiments').catch(() => null),
    ]);
    if (id !== currentId) return null; // the reader moved on
    const core = journey.core ?? [];
    const index = core.findIndex((l) => l.id === id);
    const coreEntry = core[index];
    const group = trackGroups(journey).find((g) => g.lessons.some((l) => l.id === id));
    const inTrack = group ? group.lessons.findIndex((l) => l.id === id) : -1;
    const branchEntry = [...(journey.branches?.relevant ?? []), ...(journey.branches?.available ?? [])].find((b) => b.id === id);
    const { nodes: sections, unmount } = renderLesson(lesson, {
      open,
      session,
      catalog,
      experiments,
      live: session.state?.mode === 'live',
      announce: app.announce,
      simCtx: app.simCtx(),
    });
    unmountLesson = unmount;
    const states = lessonStates(core, progress.isDone, id);
    const prereqs = (lesson.prerequisites ?? []).map((p) => {
      const st = progress.isDone(p) ? 'understood' : 'not marked understood yet';
      return el('a', { href: href('learn', p), title: st, 'data-done': progress.isDone(p) ? 'true' : 'false' }, titleOf(journey, p));
    });
    const missing = states.get(id)?.missing ?? [];

    return el('article', { class: 'lesson', 'aria-labelledby': 'lesson-title' },
      el('header', { class: 'lesson__header' },
        el('p', { class: 'eyebrow' }, inTrack >= 0 ? `Lesson ${inTrack + 1} of ${group.lessons.length}` : lesson.kind === 'branch' ? 'Branch lesson' : 'Lesson', ' · ', group?.title ?? capital(lesson.subsystem)),
        el('h1', { class: 'lesson__title', id: 'lesson-title', tabindex: '-1' }, lesson.title),
        el('p', { class: 'lede' }, lesson.summary),
        el('div', { class: 'lesson__meta' },
          prereqs.length ? el('span', {}, 'Builds on: ', joinNodes(prereqs)) : el('span', {}, 'No prerequisites: a starting point.'),
          usesSummary(lesson)),
        missing.length
          ? el('p', { class: 'lesson__locked', role: 'note' }, 'You have not marked ', joinNodes(missing.map((m) => el('a', { href: href('learn', m) }, titleOf(journey, m)))),
            ' as understood. This lesson assumes it; read on anyway if it is familiar.')
          : null,
        notesList(coreEntry?.notes ?? branchEntry?.notes),
        labelKey()),
      sections,
      versionNotes(lesson.version_notes),
      references(lesson.references),
      lessonEnd(lesson, index));
  }

  function usesSummary(lesson) {
    const uses = lesson.uses ?? [];
    const cov = lesson.coverage ?? { ok: 0, total: uses.length };
    return el('details', { class: 'uses' },
      el('summary', {}, `Evidence: ${cov.ok} of ${cov.total} readings available here`),
      el('ul', { class: 'uses__list' }, uses.map((u) => {
        const found = session.lookup(u.id);
        const b = el('button', { class: 'uses__item', type: 'button' },
          el('code', {}, u.id), ' ', el('span', { class: 'uses__value' }, found ? shortValue(found.item) : 'not in this snapshot'));
        b.addEventListener('click', () => open(u.id, b));
        return el('li', {}, b, ' ', u.kind === 'derived'
          ? el('span', { class: 'status', 'data-status': u.status === 'ok' ? 'ok' : 'absent' }, u.status === 'ok' ? 'ok' : 'unknown')
          : statusPill(u.status));
      })));
  }

  function lessonEnd(lesson, index) {
    const core = journey.core ?? [];
    const done = progress.isDone(lesson.id);
    const mark = el('button', { class: done ? 'btn' : 'btn btn--primary', type: 'button', 'aria-pressed': done ? 'true' : 'false' },
      done ? '✓ Marked as understood' : 'Mark as understood');
    mark.addEventListener('click', () => {
      progress.set(lesson.id, !progress.isDone(lesson.id));
      const now = progress.isDone(lesson.id);
      mark.className = now ? 'btn' : 'btn btn--primary';
      mark.setAttribute('aria-pressed', String(now));
      mark.textContent = now ? '✓ Marked as understood' : 'Mark as understood';
      app.announce(now ? `${lesson.title}: marked as understood.` : `${lesson.title}: no longer marked as understood.`);
    });
    const nextItems = [
      ...(lesson.next ?? []).map((id) => ({ id, kind: 'next' })),
      ...(lesson.deeper ?? []).map((id) => ({ id, kind: 'deeper' })),
    ];
    const prev = index > 0 ? core[index - 1] : null;
    const next = index >= 0 && index < core.length - 1 ? core[index + 1] : null;
    return el('footer', { class: 'lesson-end' },
      el('section', { class: 'lesson-end__mark', 'aria-labelledby': 'end-h' },
        el('h2', { class: 'visually-hidden', id: 'end-h' }, 'Your progress'),
        el('p', {}, 'Could you explain this lesson’s question to someone else, using this machine’s numbers?'),
        el('div', { class: 'cluster' }, mark)),
      nextItems.length
        ? el('section', { class: 'lesson-end__next', 'aria-labelledby': 'next-h' },
          el('h2', { class: 'journey__h2', id: 'next-h' }, 'What to understand next'),
          el('ul', { class: 'next-list' }, nextItems.map(({ id, kind }) => {
            const planned = id.startsWith('planned:');
            const pid = planned ? id.slice('planned:'.length) : id;
            const node = journey.graph?.nodes?.find((n) => n.id === pid);
            const written = !planned && (node?.written ?? true);
            return el('li', { class: 'next-item', 'data-written': written ? 'true' : 'false' },
              el('span', { class: 'next-item__kind eyebrow' }, kind === 'next' ? 'Next' : 'Deeper'),
              written ? el('a', { href: href('learn', pid) }, node?.title ?? pid) : el('span', {}, node?.title ?? pid),
              written ? null : el('span', { class: 'caption' }, ' · not written yet'));
          })))
        : null,
      el('nav', { class: 'pager', 'aria-label': 'Journey' },
        prev ? el('a', { class: 'pager__link', href: href('learn', prev.id), rel: 'prev' }, el('span', { class: 'eyebrow' }, '← Previous'), el('span', {}, prev.title)) : el('span'),
        next ? el('a', { class: 'pager__link pager__link--next', href: href('learn', next.id), rel: 'next' }, el('span', { class: 'eyebrow' }, 'Next →'), el('span', {}, next.title)) : el('span')));
  }

  // ---- mount and routing

  function stopLesson() {
    unmountLesson?.();
    unmountLesson = null;
    unmountLayers?.();
    unmountLayers = null;
  }

  async function show(route) {
    stopLesson();
    currentId = route.id;
    renderRail();
    if (railDetails && !mediaQuery.matches) railDetails.open = false;
    if (!route.id) {
      mainBox.replaceChildren(journeyPage());
      return;
    }
    if (!journeyKnows(route.id)) {
      mainBox.replaceChildren(el('div', { class: 'stack' },
        el('h1', { class: 'lesson__title', tabindex: '-1' }, 'No such lesson'),
        el('p', {}, 'There is no written lesson called ', el('code', {}, route.id), ' in this version of the curriculum.'),
        el('p', {}, el('a', { href: href('learn') }, 'See the whole journey'))));
      return;
    }
    mainBox.replaceChildren(loading('Binding the lesson to this snapshot'));
    try {
      const node = await lessonPage(route.id);
      if (node) mainBox.replaceChildren(node);
    } catch (err) {
      if (route.id === currentId) mainBox.replaceChildren(errorBox(err, () => show(route), 'The lesson could not be loaded'));
    }
  }

  function journeyKnows(id) {
    return (journey.graph?.nodes ?? []).some((n) => n.id === id && n.written);
  }

  return {
    title: (route) => route.id ? titleOf(journey, route.id) : 'Learn',
    async mount(container, route) {
      root = container;
      railBox = el('nav', { class: 'rail', 'aria-label': 'Lessons' });
      railDetails = el('details', { class: 'rail-toggle', open: true }, el('summary', {}, 'Lessons'), railBox);
      mainBox = el('div', { class: 'learn__main' });
      const layout = el('div', { class: 'layout learn' },
        el('div', { class: 'layout__rail' }, railDetails),
        el('main', { class: 'layout__main', id: 'main', tabindex: '-1' }, mainBox));
      root.replaceChildren(layout);
      app.inspector.dock(layout);

      // Wide: the rail is a column and always open. Narrow: it folds into a
      // disclosure above the page, closed once a lesson is chosen.
      mediaQuery = matchMedia('(min-width: 1100px)');
      const sync = () => { railDetails.open = mediaQuery.matches || !currentId; };
      mediaQuery.addEventListener('change', sync);

      mainBox.replaceChildren(loading('Loading the journey'));
      try {
        journey = await session.load('journey');
      } catch (err) {
        mainBox.replaceChildren(errorBox(err, () => this.mount(container, route), 'The journey could not be loaded'));
        return;
      }
      unsubscribe = progress.subscribe(() => {
        renderRail();
        if (!currentId) mainBox.replaceChildren(journeyPage());
      });
      await show(route);
      sync();
    },
    async update(route) {
      if (!journey) return false;
      await show(route);
      return true;
    },
    unmount() {
      stopLesson();
      unsubscribe?.();
      app.inspector.dock(null);
      root = null;
    },
  };
}

function stateWords(s, journey) {
  if (s.state === 'locked') return `Needs ${s.missing.map((m) => titleOf(journey, m)).join(', ')} first`;
  return { done: 'Understood', ready: 'Ready to read', current: 'Reading now' }[s.state] ?? s.state;
}

function matchedNote(o) {
  return (o.notes ?? []).find((n) => n.matched)?.text;
}

function capital(s) {
  return s ? s[0].toUpperCase() + s.slice(1) : '';
}

function joinNodes(nodes) {
  const out = [];
  nodes.forEach((n, i) => {
    if (i > 0) out.push(i === nodes.length - 1 ? ' and ' : ', ');
    out.push(n);
  });
  return out;
}

