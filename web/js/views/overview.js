// Overview (#/): what this environment appears to be — an inference, shown
// as one — the nine roadmap subsystems with their status, and the
// way into the lesson tracks.

import { el } from '../lib/dom.js';
import { formatCount } from '../lib/format.js';
import { href } from '../router.js';
import { nextLesson, trackGroups } from '../store.js';
import { badge, chip, fill, labelKey, labelledChip, statusText, valueChip } from '../render/common.js';
import { shortValue } from '../render/values.js';

const ENV_WORDS = {
  container: ['A container', 'Atlas sees the host’s kernel through a container’s namespaces and cgroups; what it reports about hardware belongs to the host.'],
  'vm-guest': ['A virtual machine guest', 'The hardware described here is what a hypervisor presents; the physical machine underneath is not visible.'],
  'no-virtualization-evidence': ['No sign of virtualization', 'Nothing Atlas can read suggests a container or a hypervisor, which is consistent with a physical machine but does not prove it.'],
  unknown: ['Unknown', 'The evidence Atlas needs to tell a container, a virtual machine and a physical machine apart was not available.'],
};

const LAYERS = [['hardware', 'Hardware'], ['kernel', 'Kernel'], ['userspace', 'User space']];
const STATUS = {
  implemented: ['ok', 'implemented'],
  overview: ['not_collected', 'overview only'],
  planned: ['unsupported', 'planned'],
};

const KEY_FACTS = [
  ['kernel.arch', 'Architecture'],
  ['os.release.name', 'Distribution'],
  ['kernel.release', 'Kernel'],
  ['cpu.model_name', 'CPU'],
  ['mem.meminfo.MemTotal', 'Memory'],
  ['kernel.page_size_bytes', 'Page size'],
  ['derived.swap.configured', 'Swap configured'],
];

export function createOverview(app) {
  const { session } = app;
  const open = (id, from) => app.inspector.open(id, from);

  function factChip(id, title) {
    const found = session.lookup(id);
    const kind = found?.type === 'derived' || id.startsWith('derived.') ? 'derived' : 'obs';
    return labelledChip(chip({
      ref: id,
      kind,
      display: found && found.item.status === 'ok' ? shortValue(found.item) : found ? '' : 'not in this snapshot',
      status: !found ? 'not_collected' : found.item.status,
      title,
    }, open), kind);
  }

  function environment() {
    const env = session.factById.get('derived.env.kind');
    const kind = env?.status === 'ok' ? String(env.value) : 'unknown';
    const [words, meaning] = ENV_WORDS[kind] ?? [kind, ''];
    return el('section', { class: 'env', 'aria-labelledby': 'env-h' },
      el('p', { class: 'eyebrow', id: 'env-h' }, 'What this environment appears to be'),
      el('div', { class: 'env__claim' },
        el('p', { class: 'env__value' }, words),
        badge('derived')),
      el('p', { class: 'env__meaning' }, meaning),
      el('p', { class: 'env__rule' },
        el('span', { class: 'env__rule-label' }, 'Inferred, not observed: '),
        env?.status === 'ok' ? env.rule : env?.reason || 'the fact derived.env.kind is not available.',
        ' ', factChip('derived.env.kind', 'Environment kind')),
      el('dl', { class: 'facts env__facts' }, KEY_FACTS.map(([id, title]) =>
        el('div', {}, el('dt', {}, title), el('dd', {}, factChip(id, title))))));
  }

  function journeyCard(journey) {
    const core = journey?.core ?? [];
    const done = core.filter((l) => app.progress.isDone(l.id)).length;
    const next = nextLesson(core, app.progress.isDone);
    const nextTitle = core.find((l) => l.id === next)?.title;
    const groups = trackGroups(journey);
    return el('section', { class: 'card card--feature journey-card', 'aria-labelledby': 'journey-h' },
      el('header', { class: 'card__header' },
        el('h2', { class: 'card__title', id: 'journey-h' }, groups.length === 1 ? groups[0].title : 'Lessons'),
        el('span', { class: 'caption num' }, `${done} of ${core.length} understood`)),
      el('p', { class: 'card__body' },
        'Each step bound to readings from this machine, and each saying what those readings cannot show.'),
      el('ul', { class: 'journey-card__tracks' }, groups.map((g) => {
        const gDone = g.lessons.filter((l) => app.progress.isDone(l.id)).length;
        const gNext = nextLesson(g.lessons, app.progress.isDone);
        return el('li', { class: 'journey-card__track' },
          el('p', { class: 'journey-card__track-title' },
            el('a', { href: gNext ? href('learn', gNext) : href('learn') }, g.title),
            el('span', { class: 'caption num' }, ` ${gDone} of ${g.lessons.length}`)),
          g.lede ? el('p', { class: 'caption' }, g.lede) : null,
          el('div', { class: 'progress', role: 'img', 'aria-label': `${g.title}: ${gDone} of ${g.lessons.length} lessons understood` },
            g.lessons.map((l) => el('span', { class: 'progress__step', 'data-done': app.progress.isDone(l.id) ? 'true' : 'false', title: l.title }))));
      })),
      el('footer', { class: 'card__footer cluster' },
        next
          ? el('a', { class: 'btn btn--primary', href: href('learn', next) }, done === 0 ? `Start: ${nextTitle}` : `Continue: ${nextTitle}`, ' →')
          : el('span', {}, 'Every lesson is marked understood.'),
        el('a', { href: href('learn') }, 'Every track and its branches')));
  }

  function subsystemCard(s) {
    const [pill, words] = STATUS[s.status] ?? ['absent', s.status];
    const planned = s.planned_concepts ?? [];
    return el('section', { class: 'card subsystem', 'data-status': s.status, 'aria-labelledby': `sub-${s.id}` },
      el('header', { class: 'card__header' },
        el('h3', { class: 'card__title', id: `sub-${s.id}` }, s.title),
        el('span', { class: 'status', 'data-status': pill }, words)),
      el('p', { class: 'card__body' }, s.summary),
      s.memory_link ? el('p', { class: 'subsystem__link' }, el('span', { class: 'subsystem__link-label' }, 'Where memory comes in. '), s.memory_link) : null,
      (s.observe ?? []).length
        ? el('dl', { class: 'facts' }, s.observe.slice(0, 5).map((v) => el('div', {}, el('dt', {}, v.title || v.id), el('dd', {}, labelledChip(valueChip(v, open), v.kind)))))
        : null,
      el('footer', { class: 'card__footer' },
        (s.lessons ?? []).length
          ? el('p', {}, s.status === 'implemented' ? 'Lessons: ' : 'Written so far: ',
            s.lessons.map((id, i) => [i ? ', ' : '', el('a', { href: href('learn', id) }, lessonTitle(id))]))
          : null,
        planned.length
          ? el('details', { class: 'subsystem__planned' },
            el('summary', {}, `${planned.length} concept${planned.length === 1 ? '' : 's'} planned, not written yet`),
            el('ul', {}, planned.map((p) => el('li', {}, el('strong', {}, p.title), ' — ', p.summary))))
          : null));
  }

  let journeyCache = null;
  const lessonTitle = (id) => journeyCache?.graph?.nodes?.find((n) => n.id === id)?.title ?? id;

  function counts() {
    const by = new Map();
    for (const o of session.snapshot?.observations ?? []) by.set(o.status, (by.get(o.status) ?? 0) + 1);
    const order = ['ok', 'absent', 'permission_denied', 'unsupported', 'failed', 'not_collected'];
    const total = session.snapshot?.observations?.length ?? 0;
    return el('section', { class: 'status-counts', 'aria-labelledby': 'counts-h' },
      el('h2', { class: 'overview__h2', id: 'counts-h' }, 'What Atlas could read'),
      el('p', { class: 'caption' }, `${formatCount(total)} observations and ${formatCount(session.facts.length)} derived facts. A missing reading is never filled in: each says why it is missing.`),
      el('ul', { class: 'status-counts__list' }, order.filter((s) => by.has(s)).map((s) => el('li', {},
        el('a', { href: href('inspect', null, { status: s }), class: 'status-counts__link' },
          el('span', { class: 'status', 'data-status': s }, statusText(s)),
          el('span', { class: 'num status-counts__n' }, formatCount(by.get(s))))))));
  }

  return {
    title: () => 'Overview',
    mount(root) {
      const roadmapBox = el('div', { class: 'roadmap' });
      const journeyBox = el('div', { class: 'overview__journey' });
      const fixture = session.snapshot?.collection?.mode === 'fixture';
      root.replaceChildren(el('main', { class: 'page overview', id: 'main', tabindex: '-1' },
        el('header', { class: 'overview__intro' },
          el('h1', { class: 'overview__title', tabindex: '-1' }, fixture ? 'An atlas of a demonstration machine' : 'An atlas of this machine'),
          el('p', { class: 'lede' },
            'An interactive textbook of the Linux system it runs on: what each part is, why it exists, and where to see it working here. Every claim says whether it was observed, derived, is general knowledge, or comes from a simulation.'),
          labelKey()),
        el('div', { class: 'overview__top' }, environment(), journeyBox),
        el('section', { 'aria-labelledby': 'roadmap-h', class: 'overview__roadmap' },
          el('h2', { class: 'overview__h2', id: 'roadmap-h' }, 'From hardware to the desktop'),
          el('p', { class: 'caption overview__note' },
            'Nine subsystems. Memory is written in depth; the others are overviews connected to it, or planned. Each card says which, and shows a few real readings.'),
          roadmapBox),
        counts()));

      fill(journeyBox, async () => {
        journeyCache = await session.load('journey');
        return journeyCard(journeyCache);
      }, 'Loading the journey');
      fill(roadmapBox, async () => {
        const [roadmap, journey] = await Promise.all([session.load('roadmap'), session.load('journey').catch(() => null)]);
        journeyCache = journey;
        const subs = roadmap?.subsystems ?? [];
        return el('div', {}, LAYERS.map(([layer, name]) => {
          const list = subs.filter((s) => s.layer === layer);
          if (list.length === 0) return null;
          return el('div', { class: 'roadmap__layer' },
            el('h3', { class: 'roadmap__layer-name eyebrow' }, name),
            el('div', { class: 'card-grid' }, list.map(subsystemCard)));
        }));
      }, 'Loading the roadmap');
    },
  };
}
