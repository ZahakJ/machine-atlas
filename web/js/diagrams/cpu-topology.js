// cpu-topology: packages, the physical cores in them, the hardware threads
// (logical CPUs) of each core, and which caches each core keeps to itself
// or shares, drawn from cpu.topology and cpu.cache_domains
// (cpu-topology.model.js does the grouping).

import { el } from '../lib/dom.js';
import { formatBytes, formatCount } from '../lib/format.js';
import { topologyModel, cacheLine, parseCPUList } from './cpu-topology.model.js';
import { mountFrame, svgRoot, rect, text, table } from './frame.js';
import { readAll, read, isTable, whyMissing } from './inputs.js';
import { columns, packParts, wrap, fitText } from './scale.js';

export const title = 'Packages, cores, hardware threads and their caches';

const CORE_MIN = 96;
const GAP = 8;
const PAD = 10;
const CHIP_W = 30;
const CHIP_H = 18;

export function mount(container, ctx) {
  const cites = ['cpu.topology', 'cpu.cache_domains', 'cpu.isolated'];
  const need = readAll(ctx, ['cpu.topology'], { 'cpu.topology': isTable });
  if (!need.ok) return mountFrame(container, ctx, { labels: ['observed'], draw: null, missing: need.missing, cites });

  const cacheRec = read(ctx, 'cpu.cache_domains');
  const isoRec = read(ctx, 'cpu.isolated');
  const caches = cacheRec.ok && isTable(cacheRec.value) ? cacheRec.value : null;
  const isolated = isoRec.ok ? parseCPUList(isoRec.value) ?? [] : [];
  const model = topologyModel(need.get('cpu.topology').value, caches, { isolated });
  if (!model.packages.length) {
    return mountFrame(container, ctx, {
      labels: ['observed'], draw: null, cites,
      missing: [{ id: 'cpu.topology', status: 'failed', note: 'it has no row that names a CPU, its package and its siblings' }],
    });
  }

  const smt = model.threads > model.cores;
  const notes = [];
  if (!caches) notes.push(`Caches are not drawn because ${whyMissing(cacheRec)}.`);
  if (!isoRec.ok) notes.push(`Whether any CPU is isolated is not shown because ${whyMissing(isoRec)}.`);
  if (isolated.length) notes.push(`Dashed CPUs (${isoRec.value}) are isolated at boot: the scheduler balances no ordinary task onto them.`);
  if (model.hybrid) notes.push('The cores are not all alike: P marks performance cores and E efficiency cores, or the capacity the kernel gives each core is shown, 1024 being the fastest.');
  if (model.skipped) notes.push(`${formatCount(model.skipped)} malformed topology rows were left out.`);
  if (model.cacheSkipped) notes.push(`${formatCount(model.cacheSkipped)} cache rows naming CPUs outside the table were left out.`);
  if (model.machineShared.length) notes.push(`Shared across packages: ${model.machineShared.map((c) => cacheLine(c, formatBytes)).join('; ')}.`);

  return mountFrame(container, ctx, {
    labels: ['observed'],
    intro: `Each outer box is a package (a socket), each inner box one physical core, and each numbered chip a logical CPU, the unit the scheduler places tasks on. ${smt ? 'Cores here run more than one hardware thread, so neighbouring chips in a core share its execution units and its private caches.' : 'Each core here runs one hardware thread.'}`,
    draw: (width) => draw(width, model),
    caption: 'Drawn from the topology and cache tables the kernel publishes in /sys/devices/system/cpu. The arrangement is logical, not physical: it says what shares what, not where it sits on the die, and inside a virtual machine it is whatever the hypervisor describes.',
    notes,
    extra: legend(model, isolated.length > 0),
    alt: table('Cores, in packages', [['Package'], ['Core'], ['Logical CPUs'], ['Kind'], ['Private caches']],
      model.packages.flatMap((p) => p.groups.flatMap((g) => g.cores.map((c) => [
        String(p.id), c.coreId === null ? '—' : String(c.coreId), c.threads.join(', '),
        c.coreType ?? (c.capacity !== null ? `capacity ${c.capacity}` : '—'),
        c.caches.length ? c.caches.map((x) => `${x.name} ${x.size !== null ? formatBytes(x.size) : ''}`.trim()).join(', ') : '—'])))),
    altLabel: 'The cores as a table',
    cites,
  });
}

function coreTitle(c, model) {
  let t = c.coreId === null ? 'Core' : `Core ${c.coreId}`;
  if (c.coreType) t += c.coreType === 'performance' ? ' · P' : ' · E';
  else if (model.hybrid && c.capacity !== null) t += ` · ${c.capacity}`;
  return t;
}

function draw(width, model) {
  const nodes = [];
  let y = 0;
  for (const p of model.packages) {
    const inner = [];
    const x0 = 0;
    const w = width - 1;
    let py = y + PAD;
    inner.push(text(x0 + PAD, py + 13, `Package ${p.id}`, 'dg-t dg-t--title'));
    inner.push(text(x0 + w - PAD, py + 13, fitText(`${p.cpus.length} logical CPUs`, w / 2, 12), 'dg-t', 'end'));
    py += 20;
    for (const c of p.shared) {
      for (const line of wrap(cacheLine(c, formatBytes), w - 2 * PAD, 12)) {
        inner.push(text(x0 + PAD, py + 12, line, 'dg-t dg-t--ink'));
        py += 16;
      }
    }
    py += 6;
    for (const g of p.groups) {
      const boxed = !!g.cache;
      const gx = x0 + PAD;
      const gw = w - 2 * PAD;
      const groupNodes = [];
      let gy = py;
      const ix = boxed ? gx + GAP : gx;
      const iw = boxed ? gw - 2 * GAP : gw;
      if (boxed) {
        gy += 6;
        for (const line of wrap(cacheLine(g.cache, formatBytes), iw, 11)) {
          groupNodes.push(text(ix, gy + 11, line, 'dg-t dg-t--small dg-t--ink'));
          gy += 14;
        }
        gy += 4;
      }
      const cols = columns(iw, CORE_MIN, GAP, 16);
      const cw = (iw - (cols - 1) * GAP) / cols;
      for (let i = 0; i < g.cores.length; i += cols) {
        const row = g.cores.slice(i, i + cols);
        const layouts = row.map((c) => coreLayout(c, cw, model));
        const h = Math.max(...layouts.map((l) => l.height));
        row.forEach((c, j) => groupNodes.push(...drawCore(c, layouts[j], ix + j * (cw + GAP), gy, cw, h)));
        gy += h + GAP;
      }
      if (boxed) {
        gy += GAP / 2;
        inner.push(rect(gx + 0.5, py + 0.5, gw - 1, gy - py - 1, 'dg-box dg-box--list', 6));
      }
      inner.push(...groupNodes);
      py = gy + (boxed ? GAP : 0);
    }
    const h = py - y + PAD - GAP;
    nodes.push(rect(x0 + 0.5, y + 0.5, w, h, 'dg-box dg-box--layer', 8), ...inner);
    y += h + 14;
  }
  return svgRoot(width, Math.max(1, y - 13),
    `${model.packages.length} package${model.packages.length === 1 ? '' : 's'}, ${model.cores} cores, ${model.threads} logical CPUs.`, nodes);
}

function coreLayout(c, cw, model) {
  const perRow = Math.max(1, Math.floor((cw - 2 * 6 + 4) / (CHIP_W + 4)));
  const chipRows = Math.ceil(c.threads.length / perRow);
  const parts = c.caches.map((x) => `${x.name} ${x.size !== null ? formatBytes(x.size) : ''}`.trim());
  const cacheLines = parts.length ? packParts(parts, cw - 12, 11) : [];
  const height = 6 + 15 + chipRows * (CHIP_H + 4) + cacheLines.length * 14 + 6;
  return { perRow, chipRows, cacheLines, height, title: fitText(coreTitle(c, model), cw - 12, 12) };
}

function drawCore(c, l, x, y, w, h) {
  const out = [rect(x + 0.5, y + 0.5, w - 1, h - 1, c.coreType === 'efficiency' ? 'dg-box dg-box--sink' : 'dg-box', 4)];
  out.push(text(x + 6, y + 6 + 11, l.title, 'dg-t dg-t--strong'));
  let cy = y + 6 + 15;
  c.threads.forEach((cpu, i) => {
    const col = i % l.perRow;
    const row = Math.floor(i / l.perRow);
    const cx = x + 6 + col * (CHIP_W + 4);
    const yy = cy + row * (CHIP_H + 4);
    const iso = c.isolated.includes(cpu);
    out.push(rect(cx + 0.5, yy + 0.5, CHIP_W, CHIP_H, iso ? 'dg-cpu dg-cpu--isolated' : 'dg-cpu', 3));
    out.push(text(cx + CHIP_W / 2 + 0.5, yy + 13, String(cpu), 'dg-t dg-t--mono dg-t--ink', 'middle'));
  });
  cy += l.chipRows * (CHIP_H + 4);
  for (const line of l.cacheLines) {
    out.push(text(x + 6, cy + 11, line, 'dg-t dg-t--small'));
    cy += 14;
  }
  return out;
}

function legend(model, anyIsolated) {
  const items = [
    el('li', {}, el('span', { class: 'dg__swatch dg-k-cpu', 'aria-hidden': 'true' }), el('strong', {}, 'Numbered chip:'), ' a logical CPU, as in /proc/cpuinfo and taskset.'),
    el('li', {}, el('strong', {}, 'Core box:'), ' the CPUs sharing one core’s execution units, with the caches only that core uses.'),
  ];
  if (model.packages.some((p) => p.groups.some((g) => g.cache))) {
    items.push(el('li', {}, el('strong', {}, 'Shaded group:'), ' cores that share a cache below the package level, such as an L2 shared by a cluster of efficiency cores.'));
  }
  if (anyIsolated) items.push(el('li', {}, el('span', { class: 'dg__swatch dg-k-cpu-isolated', 'aria-hidden': 'true' }), el('strong', {}, 'Dashed chip:'), ' an isolated CPU (isolcpus=).'));
  return el('ul', { class: 'dg__marks', 'aria-label': 'How to read the drawing' }, items);
}
