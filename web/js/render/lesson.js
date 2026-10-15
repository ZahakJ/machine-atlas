// The lesson renderer: turns a bound lesson (ARCHITECTURE.md §7, "Bound lesson")
// into DOM. Every block type and inline type of the contract has a case
// here; an unknown type renders as a visible note instead of vanishing.
//
// Labels. Observed, derived and simulated blocks carry a badge above them,
// read before the claim. General blocks — most of the prose — get only a
// faint margin mark. The lesson header's "How to read the labels" is the
// key; sections repeat nothing, so no label is shown twice in a row.

import { el } from '../lib/dom.js';
import { formatCount } from '../lib/format.js';
import { linkTarget } from '../router.js';
import { badge, chip, link, statusPill, statusText } from './common.js';
import { columnTitle, isNumericCell, shortValue } from './values.js';
import { renderExperiment } from './experiment.js';
import { because, clause, collapse, unknownWording } from './conditions.js';

const MARKED = new Set(['observed', 'derived', 'simulated']);
const SIMS = {
  'translation-walker': () => import('../sim/translation-walker.view.js'),
  reclaim: () => import('../sim/reclaim.view.js'),
  'fair-share': () => import('../sim/fair-share.view.js'),
};

let instance = 0;

/**
 * Renders `lesson`. ctx: {
 *   open(id, opener)  open the side inspector
 *   session           for fact values, catalog titles and live mode
 *   catalog           {lookup} or null
 *   experiments       Map id → info, or null
 *   simCtx            {obs, derived, navigate, inspect} for sims and diagrams
 *   live              whether the active snapshot is this machine now
 *   announce(text)
 * }
 * Returns {nodes, unmount}: one <section> per lesson section, and a
 * function that stops every simulation and diagram.
 */
export function renderLesson(lesson, ctx) {
  const r = new Renderer(ctx);
  const sections = (lesson.sections ?? []).map((s, i) => r.section(s, i + 1));
  return { nodes: sections, unmount: () => r.unmount() };
}

class Renderer {
  constructor(ctx) {
    this.ctx = ctx;
    this.cleanups = [];
    this.uid = `l${++instance}`;
    this.n = 0;
  }

  unmount() {
    for (const fn of this.cleanups.splice(0)) {
      try { fn(); } catch (err) { console.error(err); }
    }
  }

  section(s, number) {
    const id = `sec-${s.id}`;
    return el('section', { class: 'lesson__section', id, 'aria-labelledby': `${id}-h` },
      el('h2', { class: 'section-heading', id: `${id}-h` }, el('span', { class: 'section-heading__no' }, String(number)), ' ', s.title),
      this.blocks(s.blocks));
  }

  blocks(list, opts = {}) {
    return (list ?? []).map((b) => this.block(b, opts));
  }

  /** One block, wrapped with its label unless it is inside a list item. */
  block(b, opts = {}) {
    const content = this.content(b);
    if (opts.bare) return content;
    // Containers and per-row-labelled blocks label their own insides.
    if (['branch', 'predict', 'callout', 'evidence', 'sim', 'diagram', 'experiment', 'quote'].includes(b.t)) return content;
    if (MARKED.has(b.label)) {
      return el('div', { class: 'blk labelled', 'data-label': b.label }, badge(b.label), content);
    }
    return el('div', { class: 'blk', 'data-label': 'general' },
      el('span', { class: 'blk__mark', role: 'img', 'aria-label': 'General', title: 'General: true of Linux in general, not specific to this machine' }, '¶'),
      content);
  }

  content(b) {
    switch (b.t) {
      case 'p': return el('p', { class: 'prose' }, this.inl(b.inl));
      case 'h': return el(b.level >= 4 ? 'h4' : 'h3', { class: 'lesson__subhead' }, this.inl(b.inl));
      case 'ul':
      case 'ol':
        return el(b.t, { class: 'prose' }, (b.items ?? []).map((item) => el('li', {}, this.blocks(item, { bare: true }).map(unwrapParagraph))));
      case 'code':
        return el('figure', { class: 'code' },
          b.lang ? el('figcaption', { class: 'code__caption' }, b.lang) : null,
          el('pre', { tabindex: '0' }, el('code', {}, b.text)));
      case 'table': return this.mdTable(b);
      case 'quote': return el('blockquote', { class: 'prose lesson__quote' }, this.blocks(b.blocks));
      case 'evidence': return this.evidence(b);
      case 'datatable': return this.datatable(b);
      case 'diagram': return this.diagram(b);
      case 'sim': return this.sim(b);
      case 'experiment': return this.experiment(b);
      case 'predict': return this.predict(b);
      case 'callout': return this.callout(b);
      case 'branch': return this.branch(b);
      default:
        return el('p', { class: 'unavailable' }, `This version cannot display a “${b.t}” block.`);
    }
  }

  inl(list) {
    return (list ?? []).map((i) => {
      switch (i.t) {
        case 'text': return i.s;
        case 'code': return el('code', {}, i.s);
        case 'em': return el('em', {}, this.inl(i.inl));
        case 'strong': return el('strong', {}, this.inl(i.inl));
        case 'chip': return chip({ ref: i.ref, kind: i.kind, display: i.display, status: i.status, title: this.title(i.ref) }, this.ctx.open);
        case 'link': return this.link(i);
        default: return i.s ?? '';
      }
    });
  }

  link(i) {
    const children = this.inl(i.inl);
    const target = linkTarget(i.kind, i.href);
    if (!target) return el('span', { class: 'link-refused', title: 'Link target refused' }, children);
    if (i.kind === 'obs') {
      // An observation link opens the side inspector in place; with a
      // modifier key it behaves as the link it is.
      const a = el('a', { href: target.href, class: 'link-obs' }, children);
      a.addEventListener('click', (e) => {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        this.ctx.open(i.href, a);
      });
      return a;
    }
    return link(target.href, children);
  }

  title(ref) {
    return this.ctx.catalog?.lookup(ref)?.title ?? '';
  }

  mdTable(b) {
    return el('div', { class: 'table-wrap' },
      el('table', { class: 'datatable lesson__table' },
        el('thead', {}, el('tr', {}, (b.head ?? []).map((cell) => el('th', { scope: 'col' }, this.inl(cell))))),
        el('tbody', {}, (b.rows ?? []).map((row) => el('tr', {}, row.map((cell) => el('td', {}, this.inl(cell))))))));
  }

  evidence(b) {
    const rows = b.rows ?? [];
    return el('div', { class: 'table-wrap' },
      el('table', { class: 'evidence' },
        el('caption', {}, b.title || 'What this machine reports'),
        el('thead', {}, el('tr', {},
          el('th', { scope: 'col' }, 'Value and status'), el('th', { scope: 'col' }, 'What it is'),
          el('th', { scope: 'col' }, 'Source and time'))),
        el('tbody', {}, rows.map((row) => {
          const c = chip({ ref: row.ref, kind: row.kind, display: row.display, status: row.status, title: row.caption }, this.ctx.open);
          const tr = el('tr', { class: 'evidence__row', 'data-ref': row.ref },
            // The status is written out only when it is not ok: an ok
            // reading speaks for itself, a missing one must say why.
            el('td', { class: 'evidence__value' }, c, row.status !== 'ok' ? [el('br'), statusPill(row.status)] : null),
            el('td', {}, el('span', { class: 'evidence__caption' }, row.caption || this.title(row.ref) || row.ref), ' ', badge(row.label),
              el('br'), el('code', { class: 'evidence__id' }, row.ref)),
            el('td', { class: 'evidence__source' }, row.source, row.collected_at ? [el('br'), timeShort(row.collected_at)] : null));
          // The whole row is a target for the pointer; the chip inside is
          // the keyboard target, so the row needs no tabindex of its own.
          tr.addEventListener('click', (e) => {
            if (e.target.closest('a, button')) return;
            this.ctx.open(row.ref, c);
          });
          return tr;
        }))));
  }

  datatable(b) {
    const open = el('button', { class: 'btn btn--small btn--quiet', type: 'button' }, 'Inspect ', el('code', {}, b.ref));
    open.addEventListener('click', () => this.ctx.open(b.ref, open));
    if (b.status !== 'ok') {
      return el('div', { class: 'datatable-missing' },
        el('p', {}, el('code', {}, b.ref), ': ', b.display || `not available (${statusText(b.status)})`, ' ', statusPill(b.status)),
        open);
    }
    const rows = b.rows ?? [];
    const numeric = (b.columns ?? []).map((_, i) => rows.length > 0 && rows.every((r) => isNumericCell(r[i] ?? '')));
    return el('div', { class: 'table-wrap table-wrap--data' },
      el('table', { class: 'datatable' },
        el('caption', {}, this.title(b.ref) || b.ref),
        el('thead', {}, el('tr', {}, (b.columns ?? []).map((c, i) => el('th', { scope: 'col', class: numeric[i] ? 'num' : null, title: c }, columnTitle(c))))),
        el('tbody', {}, rows.map((r) => el('tr', {}, r.map((cell, i) => el('td', { class: numeric[i] ? 'num' : null }, cell)))))),
      el('div', { class: 'datatable__foot cluster' },
        el('span', {}, rows.length < b.total ? `Showing ${formatCount(rows.length)} of ${formatCount(b.total)} rows.` : `${formatCount(b.total)} row${b.total === 1 ? '' : 's'}.`),
        open));
  }

  diagram(b) {
    const host = el('div', { class: 'diagram-host' }, el('p', { class: 'loading' }, 'Loading diagram…'));
    const figure = el('figure', { class: 'diagram-slot', 'data-diagram': b.name }, host);
    let unmount = null;
    let gone = false;
    this.cleanups.push(() => { gone = true; unmount?.(); });
    import('../diagrams/index.js')
      .then((mod) => {
        if (gone) return;
        const d = mod.diagrams?.[b.name];
        if (!d || typeof d.mount !== 'function') throw new Error('missing');
        // A diagram draws its own title and provenance badges.
        host.replaceChildren();
        figure.setAttribute('aria-label', d.title ?? b.name);
        unmount = d.mount(host, this.ctx.simCtx) ?? null;
      })
      .catch(() => {
        if (gone) return;
        host.replaceChildren(unavailable('Diagram not available', `The diagram “${b.name}” is not part of this build.`));
      });
    return figure;
  }

  sim(b) {
    const host = el('div', { class: 'sim-host' }, el('p', { class: 'loading' }, 'Loading simulation…'));
    let unmount = null;
    let gone = false;
    this.cleanups.push(() => { gone = true; unmount?.(); });
    const load = SIMS[b.name];
    (load ? load() : Promise.reject(new Error('unknown')))
      .then((mod) => {
        if (gone) return;
        host.replaceChildren();
        unmount = mod.mount(host, this.ctx.simCtx) ?? null;
      })
      .catch(() => {
        if (gone) return;
        host.replaceChildren(unavailable('Simulation not available', `The simulation “${b.name}” is not part of this build.`));
      });
    return host;
  }

  experiment(b) {
    const info = this.ctx.experiments?.get(b.id);
    return renderExperiment(b.id, info, { live: this.ctx.live, announce: this.ctx.announce });
  }

  predict(b) {
    const uid = `${this.uid}-p${++this.n}`;
    const options = (b.options ?? []).map((opt, i) => {
      const input = el('input', { type: 'radio', name: uid, value: String(i) });
      return { input, label: el('label', { class: 'option' }, input, ' ', this.inl(opt)) };
    });
    const choice = el('p', { class: 'predict__choice' });
    const reveal = el('div', { class: 'predict__answer', id: `${uid}-reveal`, hidden: true, tabindex: '-1' },
      choice, this.blocks(b.reveal));
    const button = el('button', {
      class: 'btn', type: 'button', disabled: true, 'aria-expanded': 'false', 'aria-controls': `${uid}-reveal`,
    }, 'Reveal what happens here');
    const hint = el('p', { class: 'caption' }, 'Choose an answer first.');
    for (const { input } of options) {
      input.addEventListener('change', () => { button.disabled = false; hint.hidden = true; });
    }
    button.addEventListener('click', () => {
      const picked = options.find((o) => o.input.checked);
      if (!picked) return;
      choice.replaceChildren(el('span', { class: 'eyebrow' }, 'Your prediction'), ' ', picked.label.textContent.trim());
      reveal.hidden = false;
      button.setAttribute('aria-expanded', 'true');
      button.hidden = true;
      for (const o of options) o.input.disabled = true;
      reveal.focus();
    });
    return el('section', { class: 'predict', 'aria-labelledby': `${uid}-k` },
      el('p', { class: 'predict__kind', id: `${uid}-k` }, 'Predict'),
      el('div', { class: 'blocks' }, this.blocks(b.question)),
      el('fieldset', { class: 'predict__options' },
        el('legend', { class: 'visually-hidden' }, 'Your prediction'),
        options.map((o) => o.label)),
      el('div', { class: 'cluster' }, button),
      hint,
      reveal);
  }

  callout(b) {
    const kind = b.kind === 'misconception' ? 'Misconception' : b.kind === 'cannot' ? 'What these measurements cannot establish' : 'Note';
    return el('aside', { class: 'callout', 'data-kind': b.kind },
      el('p', { class: 'callout__kind' }, kind),
      b.title ? el('h3', { class: 'callout__title' }, b.title) : null,
      el('div', { class: 'blocks' }, this.blocks(b.blocks)));
  }

  /**
   * A branch reads as ordinary prose: the chosen text with its own block
   * labels, then one quiet line ("⑂ Written for this machine") that opens
   * the reason and the other readings. A branch whose chosen text is only
   * another branch collapses into the same line, listing every condition.
   */
  branch(b) {
    const { conditions, content } = collapse(b);
    const conds = conditions.map((c) => this.condition(c));
    const uid = `${this.uid}-b${++this.n}`;
    const unknown = conds.some((c) => c.chosen === 'unknown');

    const alts = [];
    for (const c of conds) {
      for (const arm of ['then', 'else', 'unknown']) {
        if (arm === c.chosen) continue;
        const list = c.branch[arm] ?? [];
        if (arm === 'unknown' && c.branch.generated_unknown) {
          alts.push(el('li', { class: 'branch__alt' },
            el('p', {}, el('span', { class: 'branch__alt-when' }, 'Where it could not be determined: '), unknownWording(c))));
          continue;
        }
        if (list.length === 0) continue;
        alts.push(el('li', { class: 'branch__alt' },
          el('details', {},
            el('summary', {}, `Where ${clause(c, arm)}`),
            el('div', { class: 'blocks' }, this.blocks(list)))));
      }
    }

    // The facts the choice rests on, each a chip that opens the inspector.
    const facts = conds.map((c) => chip({
      ref: c.fact,
      kind: c.fact.startsWith('derived.') ? 'derived' : 'obs',
      display: c.item ? shortValue(c.item) : 'not in this snapshot',
      status: c.item ? c.item.status : 'not_collected',
      title: c.title,
    }, this.ctx.open));

    const marker = el('details', { class: 'branch__marker', id: uid },
      el('summary', { class: 'branch__summary' },
        el('span', { class: 'branch__glyph', 'aria-hidden': 'true' }, '⑂'),
        unknown ? ' Written for what could not be determined here' : ' Written for this machine',
        alts.length ? el('span', { class: 'branch__count' }, ` · ${alts.length} other reading${alts.length === 1 ? '' : 's'}`) : null),
      el('div', { class: 'branch__panel' },
        el('p', { class: 'branch__why' }, `Shown because ${because(conds)}.`),
        el('p', { class: 'branch__facts caption' }, 'Decided by ', joinWith(facts.map((f, i) => [el('span', {}, conds[i].title, ' '), f]))),
        alts.length ? el('p', { class: 'branch__alts-h' }, 'Elsewhere this would read') : null,
        alts.length ? el('ul', { class: 'branch__alts' }, alts) : null));

    return el('div', { class: 'branch', 'data-chosen': conds[conds.length - 1].chosen },
      el('div', { class: 'blocks' }, this.blocks(content)),
      marker);
  }

  /** A branch's condition with the catalog and snapshot facts clause() needs. */
  condition(branch) {
    const entry = this.ctx.catalog?.lookup(branch.fact);
    const found = this.ctx.session.lookup(branch.fact);
    return {
      branch,
      fact: branch.fact,
      expect: branch.expect ?? null,
      chosen: branch.chosen,
      title: entry?.title || branch.fact,
      unit: entry?.unit,
      type: entry?.type,
      item: found?.item ?? null,
    };
  }
}

function joinWith(groups) {
  const out = [];
  groups.forEach((g, i) => {
    if (i > 0) out.push(i === groups.length - 1 ? ' and ' : ', ');
    out.push(...g);
  });
  return out;
}

// A list item's paragraphs read as plain text, not as separate blocks.
function unwrapParagraph(node) {
  if (node.tagName === 'P') {
    const span = el('span', {});
    span.append(...node.childNodes);
    return span;
  }
  return node;
}

function unavailable(title, text) {
  return el('div', { class: 'unavailable', role: 'note' }, el('p', { class: 'unavailable__title' }, title), el('p', {}, text));
}

function timeShort(s) {
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return el('time', { datetime: s, class: 'evidence__time', title: s }, d.toISOString().slice(0, 19).replace('T', ' ') + ' UTC');
}

/** Version notes as callouts. */
export function versionNotes(notes) {
  if (!notes?.length) return null;
  return el('section', { class: 'lesson__section', 'aria-labelledby': 'sec-version-h' },
    el('h2', { class: 'section-heading', id: 'sec-version-h' }, 'Version notes'),
    notes.map((n) => el('aside', { class: 'callout', 'data-kind': 'version' },
      el('p', { class: 'callout__kind' }, `Version note · ${n.applies}`),
      el('p', { class: 'prose' }, n.text))));
}

const REFERENCE_KINDS = {
  'kernel-doc': 'Kernel documentation',
  man: 'Manual pages',
  spec: 'Specifications and manuals',
  paper: 'Papers',
  book: 'Books',
  article: 'Articles',
  lwn: 'LWN.net articles',
  source: 'Source code',
};

/** References grouped by kind, each an external link. */
export function references(refs) {
  if (!refs?.length) return null;
  const groups = new Map();
  for (const r of refs) {
    const k = r.kind || 'other';
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(r);
  }
  return el('section', { class: 'lesson__section references', 'aria-labelledby': 'sec-refs-h' },
    el('h2', { class: 'section-heading', id: 'sec-refs-h' }, 'References'),
    [...groups].map(([kind, list]) => el('div', { class: 'references__group' },
      el('h3', { class: 'references__kind' }, REFERENCE_KINDS[kind] ?? kind.replace(/[-_]/g, ' ')),
      el('ul', { class: 'references__list' }, list.map((r) => el('li', {}, link(r.url, r.title), el('span', { class: 'references__host' }, host(r.url))))))));
}

function host(url) {
  try {
    return ` · ${new URL(url).hostname}`;
  } catch {
    return '';
  }
}

