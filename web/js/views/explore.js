// Explore (#/explore, #/explore/<component>): the parts of the machine,
// stacked as user space over kernel over hardware, and a detail panel that
// answers the six questions of the specification in order.
//
// The map draws no lines over its grid of cards (they could only run behind
// other cards). Instead, for the selected (or hovered) component it marks
// the related cards in place: those it relies on are tagged "↓ relied on",
// those that rely on it "↑ relies on it", and the rest dim. Authored
// architecture relations and relations observed between instances in this
// snapshot are outlined differently and tagged separately, never merged;
// the legend says which is which.

import { el } from '../lib/dom.js';
import { href } from '../router.js';
import { badge, errorBox, labelKey, labelledChip, loading, titleOf, valueChip } from '../render/common.js';
import { lessonStates } from '../store.js';

const LAYERS = [
  ['userspace', 'User space', 'Programs and the libraries they use, each in its own address space.'],
  ['kernel', 'Kernel', 'Shared mechanisms that manage memory on behalf of every program.'],
  ['hardware', 'Hardware', 'What the CPU and the memory system provide.'],
];
const STATUS = { implemented: ['ok', 'lessons'], overview: ['not_collected', 'overview'], planned: ['unsupported', 'planned'] };

export function createExplore(app) {
  const { session } = app;
  const open = (id, from) => app.inspector.open(id, from);
  let graph = null;
  let journey = null;
  let selected = null;
  let mapBox = null;
  let detailBox = null;
  let nodeEls = new Map();
  let mapEl = null;
  let root = null;

  const node = (id) => graph.nodes.find((n) => n.id === id);
  const componentOf = (instanceId) => instanceId.split('/')[0];

  // Depth in the dependency graph: foundations are 0. Within a band the
  // map lists higher-level components first, so each band reads top-down
  // like the stack as a whole.
  function depths() {
    const memo = new Map();
    const visit = (id, seen = new Set()) => {
      if (memo.has(id)) return memo.get(id);
      if (seen.has(id)) return 0;
      seen.add(id);
      const n = node(id);
      const d = (n?.depends_on ?? []).filter((x) => node(x)).reduce((m, x) => Math.max(m, visit(x, seen) + 1), 0);
      memo.set(id, d);
      return d;
    };
    for (const n of graph.nodes) visit(n.id);
    return memo;
  }

  function renderMap() {
    const depth = depths();
    nodeEls = new Map();
    const bands = LAYERS.map(([layer, name, text]) => {
      const list = graph.nodes.filter((n) => n.layer === layer)
        .sort((a, b) => depth.get(b.id) - depth.get(a.id) || a.title.localeCompare(b.title));
      if (list.length === 0) return null;
      return el('section', { class: 'cmap__band', 'data-layer': layer, 'aria-labelledby': `band-${layer}` },
        el('header', { class: 'cmap__band-head' },
          el('h2', { class: 'cmap__band-name', id: `band-${layer}` }, name),
          el('p', { class: 'caption' }, text)),
        el('ul', { class: 'cmap__nodes' }, list.map((n) => {
          const [pill, words] = STATUS[n.status] ?? ['absent', n.status];
          const insts = n.instances ?? [];
          const a = el('a', { class: 'cmap__node', href: href('explore', n.id), 'data-id': n.id, 'data-status': n.status },
            el('span', { class: 'cmap__title' }, n.title),
            el('span', { class: 'cmap__meta' },
              el('span', { class: 'status', 'data-status': pill }, words),
              insts.length ? el('span', { class: 'cmap__count' }, `${insts.length} found here`) : null),
            insts.length
              ? el('span', { class: 'cmap__instances' }, insts.slice(0, 3).map((i) => i.title).join(' · '), insts.length > 3 ? ` · and ${insts.length - 3} more` : '')
              : null,
            el('span', { class: 'cmap__tags' }));
          a.addEventListener('mouseenter', () => markRelated(n.id));
          a.addEventListener('focus', () => markRelated(n.id));
          a.addEventListener('mouseleave', () => markRelated(selected));
          a.addEventListener('blur', () => markRelated(selected));
          nodeEls.set(n.id, a);
          return el('li', {}, a);
        })));
    });
    mapEl = el('div', { class: 'cmap' }, bands);
    return mapEl;
  }

  // Relations of one component, from its point of view: the components it
  // relies on ("depends") and those that rely on it ("used-by"), each with
  // its basis. An observed relation between instances counts once per pair
  // of components.
  function relationsOf(id) {
    const out = new Map(); // other id → Set of "depends:architecture" …
    const add = (other, dir, basis) => {
      if (other === id) return;
      if (!out.has(other)) out.set(other, new Set());
      out.get(other).add(`${dir}:${basis}`);
    };
    for (const e of graph.edges) {
      const from = e.basis === 'observed' ? componentOf(e.from) : e.from;
      const to = e.basis === 'observed' ? componentOf(e.to) : e.to;
      const basis = e.basis === 'observed' ? 'observed' : 'architecture';
      if (from === id) add(to, 'depends', basis);
      if (to === id) add(from, 'used-by', basis);
    }
    return out;
  }

  const TAGS = {
    'depends:architecture': '↓ relied on',
    'used-by:architecture': '↑ relies on it',
    // An observed edge says an instance of one lies within an instance of
    // the other: the selected part lies within this card's ("contains it")
    // or this card's lies within the selected part's.
    'depends:observed': '↓ contains it (observed)',
    'used-by:observed': '↑ lies within it (observed)',
  };

  /** Marks the cards related to `id` in place; with no id, clears them. */
  function markRelated(id) {
    if (!mapEl) return;
    const rel = id && nodeEls.has(id) ? relationsOf(id) : new Map();
    mapEl.toggleAttribute('data-focus', rel.size > 0 || !!(id && nodeEls.has(id)));
    for (const [nid, a] of nodeEls) {
      const kinds = rel.get(nid);
      const tags = a.querySelector('.cmap__tags');
      a.toggleAttribute('data-focus', nid === id);
      if (!kinds) {
        a.removeAttribute('data-related');
        a.removeAttribute('data-basis');
        tags.replaceChildren();
        continue;
      }
      const list = [...kinds].sort();
      a.setAttribute('data-related', [...new Set(list.map((k) => k.split(':')[0]))].join(' '));
      a.setAttribute('data-basis', [...new Set(list.map((k) => k.split(':')[1]))].join(' '));
      tags.replaceChildren(...list.map((k) => el('span', { class: 'cmap__tag', 'data-kind': k }, TAGS[k])));
    }
  }

  // ---- detail panel

  function detail(id) {
    const n = node(id);
    if (!n) {
      return el('div', { class: 'explore__intro' },
        el('h2', { class: 'explore__detail-title', tabindex: '-1' }, id ? 'No such component' : 'Choose a component'),
        id ? el('p', {}, 'There is no component called ', el('code', {}, id), '.') : null,
        el('p', {}, 'Each box is one part of the memory system. Select one to see what it is, why it exists, what depends on it, where it shows up on this machine, what would change elsewhere, and which lesson explains it.'),
        legend());
    }
    const [pill, words] = STATUS[n.status] ?? ['absent', n.status];
    const dependsOn = n.depends_on ?? [];
    const usedBy = n.used_by ?? [];
    const relations = graph.edges.filter((e) => e.basis === 'observed' && (componentOf(e.from) === id || componentOf(e.to) === id));
    const instTitle = (iid) => graph.nodes.flatMap((x) => x.instances ?? []).find((i) => i.id === iid)?.title ?? iid;
    const states = lessonStates(journey?.core ?? [], app.progress.isDone);

    return el('article', { class: 'explore__article', 'aria-labelledby': 'cmp-title' },
      el('header', {},
        el('p', { class: 'eyebrow' }, `${layerName(n.layer)} · ${n.subsystem}`),
        el('h2', { class: 'explore__detail-title', id: 'cmp-title', tabindex: '-1' }, n.title),
        el('div', { class: 'cluster' }, el('span', { class: 'status', 'data-status': pill }, words), badge('general')),
        el('nav', { class: 'explore__jump', 'aria-label': 'Questions' }, QUESTIONS.map(([qid, text]) => el('a', { href: `#${qid}`, onclick: (e) => jump(e, qid) }, text)))),
      q('q-what', 'What is this?', el('p', { class: 'prose' }, n.what)),
      q('q-why', 'Why does it exist?', el('p', { class: 'prose' }, n.why)),
      q('q-deps', 'What depends on it?',
        usedBy.length ? el('ul', { class: 'link-list' }, usedBy.map((u) => el('li', {}, compLink(u)))) : el('p', { class: 'caption' }, 'Nothing in this map depends on it directly.'),
        dependsOn.length ? el('p', { class: 'explore__also' }, 'It relies on ', join(dependsOn.map(compLink)), '.') : null,
        el('p', { class: 'caption' }, 'These relations are authored: general knowledge of Linux, not read from this machine.')),
      q('q-see', 'Where can I see it on my machine?',
        (n.observe ?? []).length
          ? el('dl', { class: 'facts' }, n.observe.map((v) => el('div', {}, el('dt', {}, v.title || v.id), el('dd', {}, labelledChip(valueChip(v, open), v.kind)))))
          : el('p', { class: 'caption' }, 'Nothing Atlas collects shows this component directly.'),
        instances(n),
        relations.length
          ? el('div', {}, el('p', { class: 'detail__label' }, badge('observed'), ' Relations found in this snapshot'),
            el('ul', { class: 'relations' }, relations.map((e) => el('li', {}, instTitle(e.from), ` ${e.relation} `, instTitle(e.to)))))
          : null),
      q('q-varies', 'What would change under different conditions?', el('p', { class: 'prose' }, n.varies)),
      q('q-next', 'What should I understand next?',
        (n.lessons ?? []).length
          ? el('ul', { class: 'link-list' }, n.lessons.map((l) => {
            const written = (journey?.graph?.nodes ?? []).some((g) => g.id === l && g.written);
            const st = states.get(l)?.state;
            return el('li', {}, written ? el('a', { href: href('learn', l) }, titleOf(journey, l)) : el('span', {}, titleOf(journey, l), el('span', { class: 'caption' }, ' · not written yet')),
              st === 'done' ? el('span', { class: 'caption' }, ' · understood') : null);
          }))
          : el('p', { class: 'caption' }, 'No lesson covers this component yet.')));
  }

  function instances(n) {
    const list = n.instances ?? [];
    if (list.length === 0) return null;
    const cols = [...new Set(list.flatMap((i) => Object.keys(i.cells ?? {})))];
    const src = n.instance_source;
    return el('div', { class: 'instances' },
      el('p', { class: 'detail__label' }, badge('observed'), ` ${list.length} found here`, src ? [' · from ', valueChip(src, open)] : null),
      el('div', { class: 'table-wrap' },
        el('table', { class: 'datatable' },
          el('thead', {}, el('tr', {}, el('th', { scope: 'col' }, 'Instance'), cols.map((c) => el('th', { scope: 'col' }, c)))),
          el('tbody', {}, list.map((i) => el('tr', {}, el('th', { scope: 'row' }, i.title), cols.map((c) => el('td', { class: 'mono' }, i.cells?.[c] ?? ''))))))));
  }

  function compLink(id) {
    return el('a', { href: href('explore', id) }, node(id)?.title ?? id);
  }

  function legend() {
    return el('div', { class: 'cmap-legend', role: 'note', 'aria-label': 'Legend' },
      el('p', { class: 'detail__label' }, 'Legend'),
      el('ul', {},
        el('li', {}, el('span', { class: 'cmap-legend__card', 'data-basis': 'architecture', 'aria-hidden': 'true' }), ' ',
          el('strong', {}, 'Architecture'), ': authored, true of Linux in general. With a part selected, a solid outline marks what it ',
          el('span', { class: 'cmap__tag', 'data-kind': 'depends:architecture' }, '↓ relied on'), ' and what ',
          el('span', { class: 'cmap__tag', 'data-kind': 'used-by:architecture' }, '↑ relies on it'), '.'),
        el('li', {}, el('span', { class: 'cmap-legend__card', 'data-basis': 'observed', 'aria-hidden': 'true' }), ' ',
          el('strong', {}, 'Observed'), ': a dashed outline in the observed colour marks a relation found between instances in this snapshot; with a zone selected, the NUMA node is tagged ',
          el('span', { class: 'cmap__tag', 'data-kind': 'depends:observed' }, '↓ contains it (observed)'), '.'),
        el('li', {}, 'Unrelated parts fade while one is selected.'),
        el('li', {}, el('span', { class: 'status', 'data-status': 'ok' }, 'lessons'), ' explained in lessons; ', el('span', { class: 'status', 'data-status': 'not_collected' }, 'overview'), ' described here only')));
  }

  function select(id, focus) {
    selected = id;
    mapBox.parentElement.dataset.selected = id ? 'true' : 'false';
    for (const [nid, a] of nodeEls) {
      if (nid === id) a.setAttribute('aria-current', 'true');
      else a.removeAttribute('aria-current');
    }
    detailBox.replaceChildren(detail(id));
    markRelated(id);
    if (focus && id) {
      const h = detailBox.querySelector('h2');
      h?.focus({ preventScroll: true });
      if (!matchMedia('(min-width: 1100px)').matches) detailBox.scrollIntoView({ block: 'start' });
    }
  }

  return {
    title: (route) => route.id ? (graph ? node(route.id)?.title ?? route.id : route.id) : 'Explore',
    async mount(container, route) {
      root = container;
      mapBox = el('div', { class: 'explore__map' }, loading('Loading the components'));
      // Focusable so the keyboard can scroll it when its answers overflow.
      detailBox = el('aside', { class: 'explore__detail', 'aria-label': 'Component detail', tabindex: '0' });
      root.replaceChildren(el('main', { class: 'page explore', id: 'main', tabindex: '-1' },
        el('header', { class: 'explore__head' },
          el('p', { class: 'eyebrow' }, 'Explore'),
          el('h1', { class: 'lesson__title', tabindex: '-1' }, 'The memory system, part by part'),
          el('p', { class: 'lede' }, 'From the programs at the top to the hardware at the bottom. Select a part to mark, on the map, what it relies on and what relies on it.'),
          labelKey()),
        el('div', { class: 'explore__grid' }, mapBox, detailBox)));
      try {
        [graph, journey] = await Promise.all([session.load('components'), session.load('journey').catch(() => null)]);
      } catch (err) {
        mapBox.replaceChildren(errorBox(err, () => this.mount(container, route), 'The components could not be loaded'));
        return;
      }
      if (!root) return;
      mapBox.replaceChildren(renderMap(), el('div', { class: 'explore__legend-wide' }, legend()));
      select(route.id, false);
    },
    update(route) {
      if (!graph) return false;
      select(route.id, true);
      return true;
    },
    unmount() {
      mapEl = null;
      root = null;
    },
  };
}

// The six questions of the specification, with short names for the jump
// list at the top of the panel.
const QUESTIONS = [
  ['q-what', 'What is it'], ['q-why', 'Why'], ['q-deps', 'Depends on it'],
  ['q-see', 'On this machine'], ['q-varies', 'Elsewhere'], ['q-next', 'Next'],
];

function q(id, question, ...body) {
  return el('section', { class: 'question', 'aria-labelledby': id }, el('h3', { class: 'question__q', id, tabindex: '-1' }, question), body);
}

// The hash belongs to the router, so a jump scrolls and focuses instead.
function jump(e, id) {
  e.preventDefault();
  const h = document.getElementById(id);
  h?.scrollIntoView({ block: 'start' });
  h?.focus({ preventScroll: true });
}

function layerName(layer) {
  return LAYERS.find(([l]) => l === layer)?.[1] ?? layer;
}

function join(nodes) {
  const out = [];
  nodes.forEach((n, i) => {
    if (i > 0) out.push(i === nodes.length - 1 ? ' and ' : ', ');
    out.push(n);
  });
  return out;
}
