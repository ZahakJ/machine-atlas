// memory-layers: the whole memory journey on one page, bottom-up, from the
// CPU's address translation to reclaim. Each layer links to its lessons and
// names the roadmap subsystems it touches. Authored (GENERAL).

import { el, svg } from '../lib/dom.js';
import { mountFrame, svgRoot, nextId, text } from './frame.js';
import { box, arrow, arrowDefs } from './flow.js';
import { measureBox, textWidth } from './scale.js';

export const title = 'The memory journey, layer by layer';

const LESSONS = {
  'cpu-addressing': 'CPU addressing',
  'virtual-address-spaces': 'Virtual address spaces',
  mappings: 'Mappings',
  'page-faults': 'Page faults',
  'physical-memory': 'Physical memory',
  'anonymous-and-file-pages': 'Anonymous and file pages',
  'page-cache': 'Page cache',
  'reclaim-and-swap': 'Reclaim and swap',
  'memory-pressure-and-oom': 'Pressure and OOM',
};

// Titles as in curriculum/roadmap.json.
const SUBSYSTEMS = {
  cpu: 'CPU',
  memory: 'Memory',
  processes: 'Processes',
  storage: 'Storage',
  'boot-and-init': 'Boot and init',
  'devices-and-drivers': 'Devices and drivers',
  distribution: 'Distribution',
};

/** Bottom-up: the first layer is the hardware every other one rests on. */
export const LAYERS = [
  {
    title: 'CPU and MMU',
    body: 'Programs use virtual addresses. The MMU translates every one, and the TLB caches recent translations so that translating is usually free.',
    lessons: ['cpu-addressing'],
    subsystems: ['cpu', 'memory'],
  },
  {
    title: 'Address spaces and page tables',
    body: 'Each process has its own page tables. Mappings record what may be at an address; page faults fill the tables in on first touch.',
    lessons: ['virtual-address-spaces', 'mappings', 'page-faults'],
    subsystems: ['memory', 'processes'],
  },
  {
    title: 'Physical frames and zones',
    body: 'RAM is cut into page frames, grouped by NUMA node and zone and handed out by the buddy allocator, with a reserve kept by the watermarks.',
    lessons: ['physical-memory'],
    subsystems: ['memory', 'boot-and-init', 'devices-and-drivers'],
  },
  {
    title: 'Anonymous and file pages',
    body: 'Every frame in use holds anonymous memory, with no file behind it, or a page of a file. Which one decides where it can go when it is evicted.',
    lessons: ['anonymous-and-file-pages'],
    subsystems: ['memory', 'processes'],
  },
  {
    title: 'Page cache and swap',
    body: 'File pages stay cached after use because reading them again costs I/O; swap gives anonymous pages somewhere on storage to go.',
    lessons: ['page-cache'],
    subsystems: ['storage', 'memory'],
  },
  {
    title: 'Reclaim',
    body: 'When free frames run short, reclaim evicts the pages used least recently. If it cannot keep up, tasks stall, and in the end the OOM killer acts.',
    lessons: ['reclaim-and-swap', 'memory-pressure-and-oom'],
    subsystems: ['memory', 'distribution'],
  },
];

export const lessonHref = (id) => `#/learn/${encodeURIComponent(id)}`;
// There is no route for a roadmap subsystem; Explore filtered to it is the
// closest view of one.
export const subsystemHref = (id) => `#/explore?subsystem=${encodeURIComponent(id)}`;

const WIDE = 640;
const GAP = 22;

export function mount(container, ctx) {
  return mountFrame(container, ctx, {
    labels: ['general'],
    intro: 'Each layer below solves a problem the layer under it leaves open. Read from the bottom up; every layer links to the lessons that explain it.',
    draw,
    caption: 'Arrows point up: each layer builds on the one beneath. Subsystems are the parts of the roadmap each layer touches.',
    alt: el('ol', { class: 'dg__steps' }, LAYERS.map((l) => el('li', {},
      el('strong', {}, l.title), ': ', l.body, ' Lessons: ',
      join(l.lessons.map((id) => el('a', { href: lessonHref(id) }, LESSONS[id] ?? id))),
      '. Subsystems: ',
      join(l.subsystems.map((id) => el('a', { href: subsystemHref(id) }, SUBSYSTEMS[id] ?? id))), '.'))),
    altLabel: 'The layers as a list',
  });
}

function join(nodes) {
  return nodes.flatMap((n, i) => (i ? [', ', n] : [n]));
}

function draw(width) {
  const { defs, ids } = arrowDefs(nextId('dg-ml'));
  const wide = width >= WIDE;
  const linkW = wide ? Math.min(260, Math.floor(width * 0.34)) : width - 20;
  const textW = wide ? width - linkW - 12 : width;
  const nodes = [defs];

  // Top-down drawing order (reclaim first) so the CPU ends at the bottom.
  const order = LAYERS.map((l, i) => ({ ...l, n: i + 1 })).reverse();
  let y = 0;
  const tops = [];
  for (const l of order) {
    const head = `${l.n}  ${l.title}`;
    const m = measureBox(head, l.body, textW);
    const links = linkBlock(l, linkW);
    const h = wide ? Math.max(m.height, links.height + 16) : m.height + links.height + 4;
    nodes.push(box(0, y, width, { title: head, body: l.body, cls: 'dg-box dg-box--layer', wrapW: textW, extraH: h - m.height }).node);
    const lx = wide ? textW + 2 : 10;
    const ly = wide ? y + 12 : y + m.height - 4;
    nodes.push(...links.draw(lx, ly));
    tops.push([y, y + h]);
    y += h + GAP;
  }
  // Upward arrows in the gaps: each layer rests on the one below it.
  for (let i = 1; i < tops.length; i++) {
    nodes.push(arrow([[28, tops[i][0]], [28, tops[i - 1][1] + 1]], ids));
  }
  return svgRoot(width, y - GAP + 1, 'Six layers of Linux memory management, from CPU and MMU at the bottom to reclaim at the top.', nodes);
}

// "Lessons" and "Subsystems" rows of links, wrapped to `width`.
function linkBlock(layer, width) {
  const rows = [
    ['Lessons', layer.lessons.map((id) => [LESSONS[id] ?? id, lessonHref(id)])],
    ['Subsystems', layer.subsystems.map((id) => [SUBSYSTEMS[id] ?? id, subsystemHref(id)])],
  ];
  const lines = [];
  for (const [label, items] of rows) {
    let line = { label, items: [], used: textWidth(label, 11) + 10 };
    for (const [words, href] of items) {
      const w = textWidth(`${words} · `, 12);
      if (line.items.length && line.used + w > width) {
        lines.push(line);
        line = { label: '', items: [], used: textWidth(label, 11) + 10 };
      }
      line.items.push({ words, href, x: line.used });
      line.used += w;
    }
    lines.push(line);
  }
  const indent = Math.max(textWidth('Subsystems', 11), textWidth('Lessons', 11)) + 10;
  return {
    height: lines.length * 18,
    draw(x, y) {
      const out = [];
      lines.forEach((line, i) => {
        const ly = y + 13 + i * 18;
        if (line.label) out.push(text(x, ly, line.label, 'dg-t dg-t--small dg-t--faint'));
        // One <text> per line, so the browser spaces the links itself;
        // only the line breaks come from the width estimate.
        const parts = line.items.flatMap((item, j) => [
          j ? svg('tspan', { class: 'dg-t--faint' }, ' · ') : null,
          svg('a', { href: item.href }, svg('tspan', { class: 'dg-t--link' }, item.words)),
        ]);
        out.push(svg('text', { x: x + indent, y: ly, class: 'dg-t' }, parts));
      });
      return out;
    },
  };
}
