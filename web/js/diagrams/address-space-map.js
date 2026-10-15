// address-space-map: the probe process's mappings on the whole user address
// range, as a vertical strip with high addresses at the top. Layout lives in
// address-space.model.js; this file draws it.

import { el, svg } from '../lib/dom.js';
import { formatBytes, formatCount, formatHex } from '../lib/format.js';
import { buildRows, layoutRows, isSample, KINDS, KIND_NAMES } from './address-space.model.js';
import { mountFrame, svgRoot, rect, text, path, table, round } from './frame.js';
import { readAll, read, isTable, isNumber, whyMissing } from './inputs.js';
import { fitText, parseAddr, toNumber } from './scale.js';

const REQUIRED = ['probe.maps.sample'];
const OPTIONAL = ['probe.regions', 'derived.paging.user_space_bytes', 'probe.maps.vma_count', 'probe.maps.highest_end', 'kernel.page_size_bytes'];

const hex = (big) => formatHex(big, { group: 4 });
const size = (big) => formatBytes(toNumber(big));

export const title = 'The probe’s address space';

export function mount(container, ctx) {
  const need = readAll(ctx, REQUIRED, { 'probe.maps.sample': isTable });
  if (!need.ok) {
    return mountFrame(container, ctx, { labels: ['observed'], draw: null, missing: need.missing, cites: REQUIRED });
  }
  const opt = Object.fromEntries(OPTIONAL.map((id) => [id, read(ctx, id)]));
  const notes = [];

  const regions = opt['probe.regions'].ok && isTable(opt['probe.regions'].value) ? opt['probe.regions'].value : null;
  if (!regions) notes.push(`The probe’s five regions are not outlined: ${whyMissing(opt['probe.regions'])}.`);

  const user = opt['derived.paging.user_space_bytes'];
  const userEnd = user.ok && isNumber(user.value) && user.value > 0 ? BigInt(user.value) : null;
  if (userEnd === null) notes.push(`The strip ends at the highest mapping, not at the end of user space: ${whyMissing(user)}.`);

  const count = opt['probe.maps.vma_count'];
  const pageSize = opt['kernel.page_size_bytes'].ok && isNumber(opt['kernel.page_size_bytes'].value) ? opt['kernel.page_size_bytes'].value : 4096;

  const highest = opt['probe.maps.highest_end'];
  const highestEnd = highest.ok ? parseAddr(highest.value) : null;
  const model = buildRows({ maps: need.get('probe.maps.sample').value, regions, userEnd, vmaCount: count.ok ? count.value : null, highestEnd });
  const sample = isSample(model);
  if (sample) {
    notes.push(`The snapshot keeps the probe’s lowest ${formatCount(model.shown)} mappings. The other ${formatCount(model.total - model.shown)} lie above them${highestEnd !== null ? `, up to ${hex(highestEnd)}` : ''}; the hatched band stands for them without placing them. On most systems they include more shared libraries, the stack, and the kernel-provided vvar and vdso pages.`);
  }
  if (model.above.length) {
    notes.push(`Above user space, not drawn: ${model.above.map((m) => `${m.path || KIND_NAMES[m.kind]} at ${hex(m.start)}`).join(', ')}. ${model.above.some((m) => m.path === '[vsyscall]') ? 'The vsyscall page is a legacy fixed mapping in the kernel’s half, kept for old binaries.' : ''}`.trim());
  }
  if (model.unplaced.length) notes.push(`Probe regions outside the sampled mappings, not outlined: ${model.unplaced.join(', ')}.`);
  if (model.skipped) notes.push(`${formatCount(model.skipped)} malformed or overlapping rows were left out.`);

  const regionAt = new Map();
  for (const r of model.regions) regionAt.set(r.rows[0], r);
  const kindsPresent = KINDS.filter((k) => model.rows.some((r) => r.type === 'map' && r.kind === k));

  const label = `Address space of the probe process: ${formatCount(model.rows.filter((r) => r.type === 'map').length)} mappings drawn${userEnd ? ` across ${size(userEnd)} of user space` : ''}.`;

  return mountFrame(container, ctx, {
    labels: ['observed'],
    intro: sample
      ? `The lowest ${formatCount(model.shown)} of the ${formatCount(model.total)} mappings the probe process had when it was measured, from address 0 at the bottom; the hatched band above them stands for the ${formatCount(model.total - model.shown)} the snapshot did not keep. The five regions the probe made are outlined.`
      : 'Every mapping the probe process had when it was measured, from address 0 at the bottom to the top of user space. The five regions the probe made are outlined.',
    draw: (width) => draw(width, model, pageSize, regionAt, userEnd, label),
    caption: 'Not to scale. Each mapping’s height grows with the logarithm of its size, and every unmapped gap is cut to the same height (the zigzag marks the cut) with its true size written beside it. Addresses are hexadecimal.',
    notes,
    extra: legend(kindsPresent, sample),
    alt: table('The probe’s mappings, highest address first',
      [['Start'], ['End'], ['Size', true], ['Kind'], ['Permissions'], ['Name']],
      [...model.above, ...model.rows].filter((r) => r.type === 'map').map((r) => [
        el('code', {}, hex(r.start)), el('code', {}, hex(r.end)), size(r.size), KIND_NAMES[r.kind], el('code', {}, r.perms),
        regionName(model, r) || r.path || '—',
      ])),
    altLabel: 'The mappings as a table',
    cites: ['probe.maps.sample', 'probe.maps.vma_count', 'probe.regions', 'derived.paging.user_space_bytes'],
  });
}

function regionName(model, row) {
  return model.regions.find((g) => g.start < row.end && g.end > row.start)?.name ?? '';
}

// Wide drawings put each mapping's address and name on one line, in two
// aligned columns; narrow ones stack them, which needs taller rows.
const WIDE = 560;

function draw(width, model, pageSize, regionAt, userEnd, label) {
  const wide = width >= WIDE;
  const laid = layoutRows(model.rows, wide ? { pageSize, minH: 20, perDouble: 2.2, maxH: 52, gapH: 22 } : { pageSize });
  const stripW = wide ? 64 : 40;
  const top = wide ? 26 : 40;
  const labelX = stripW + 14;
  const labelW = width - labelX;
  const addrW = 136;
  const nameX = wide ? labelX + addrW : labelX;
  const nameW = width - nameX;
  const nodes = [];

  // End of user space above the strip, 0 below it.
  if (userEnd) {
    nodes.push(text(labelX, 14, hex(userEnd), 'dg-t dg-t--mono dg-t--faint'));
    const words = `end of user space (${size(userEnd)})`;
    nodes.push(wide ? text(nameX, 14, fitText(words, nameW, 12), 'dg-t dg-t--faint') : text(labelX, 29, fitText(words, labelW, 12), 'dg-t dg-t--faint'));
  } else {
    nodes.push(text(labelX, 14, fitText('the highest mapping shown; the end of user space is unknown', labelW, 12), 'dg-t dg-t--faint'));
  }
  nodes.push(path(`M0 ${top - 4}H${stripW}`, 'dg-axis'));

  for (let i = 0; i < laid.rows.length; i++) {
    const r = laid.rows[i];
    const y = top + r.y;
    if (r.type === 'unsampled') {
      nodes.push(rect(0, y, stripW, r.h, 'dg-unsampled'));
      nodes.push(path(hatch(0, y, stripW, r.h, 6), 'dg-unsampled-hatch'));
      const upTo = r.size > 0n ? `, up to ${hex(r.end)}` : '';
      const words = `${formatCount(r.count)} mapping${r.count === 1 ? '' : 's'} not in the sample${upTo}`;
      nodes.push(text(nameX, y + r.h / 2 + 4, fitText(words, nameW, 12), 'dg-t dg-t--ink dg-t--italic'));
      continue;
    }
    if (r.type === 'gap') {
      nodes.push(rect(0, y, stripW, r.h, 'dg-gap'));
      nodes.push(path(zigzag(0, stripW, y + r.h / 2, 4), 'dg-gap-zig'));
      const words = r.edge === 'top' ? 'unmapped, to the top' : r.edge === 'bottom' ? 'unmapped, from address 0' : 'unmapped gap';
      nodes.push(text(nameX, y + r.h / 2 + 4, fitText(`${size(r.size)} ${words}`, nameW, 12), 'dg-t dg-t--faint dg-t--italic'));
      continue;
    }
    nodes.push(rect(0, y, stripW, r.h, `dg-k-${r.kind}`, 2));
    const region = regionAt.get(i);
    const name = region ? region.name : r.path || KIND_NAMES[r.kind];
    const line1 = region ? `${name} · ${size(region.size)} · probe region` : `${name} · ${r.perms} · ${size(r.size)}`;
    const nameY = wide ? y + Math.min(r.h / 2 + 4, 15) : y + 13;
    nodes.push(text(nameX, nameY, fitText(line1, nameW, 12), region ? 'dg-t dg-t--strong' : 'dg-t dg-t--ink'));
    nodes.push(text(labelX, wide ? nameY : y + 26, fitText(hex(r.start), wide ? addrW : labelW, 11, 'mono'), 'dg-t dg-t--mono dg-t--faint'));
  }

  // Outline each region over the rows it covers (one, unless the kernel
  // merged neighbouring mappings).
  for (const region of regionAt.values()) {
    const first = laid.rows[region.rows[0]];
    const last = laid.rows[region.rows[region.rows.length - 1]];
    nodes.push(rect(1, top + first.y + 1, stripW - 2, last.y + last.h - first.y - 2, 'dg-region', 2));
  }

  const bottom = top + laid.height;
  nodes.push(path(`M0 ${bottom + 4}H${stripW}`, 'dg-axis'));
  nodes.push(text(labelX, bottom + 16, '0x0', 'dg-t dg-t--mono dg-t--faint'));
  nodes.push(text(wide ? nameX : labelX + 40, bottom + 16, 'address 0', 'dg-t dg-t--faint'));
  return svgRoot(width, bottom + 22, label, nodes);
}

/** A zigzag across [x0, x1] at height y: the conventional mark of a cut axis. */
function zigzag(x0, x1, y, amp) {
  const step = 6;
  let d = `M${x0} ${y}`;
  let up = true;
  for (let x = x0 + step / 2; x <= x1; x += step / 2) {
    d += `L${round(x)} ${round(y + (up ? -amp : amp))}`;
    up = !up;
  }
  return d;
}

/** Diagonal hatching inside the rectangle (x, y, w, h), one line every `step`. */
function hatch(x, y, w, h, step) {
  let d = '';
  for (let k = step; k < w + h; k += step) {
    // The line x' + y' = k (in box coordinates), clipped to the box.
    const x0 = Math.max(0, k - h);
    const x1 = Math.min(w, k);
    d += `M${round(x + x0)} ${round(y + k - x0)}L${round(x + x1)} ${round(y + k - x1)}`;
  }
  return d;
}

function legend(kinds, sample) {
  return el('ul', { class: 'dg__legend', 'aria-label': 'Key' },
    kinds.map((k) => el('li', {}, el('span', { class: `dg__swatch dg-k-${k}`, 'aria-hidden': 'true' }), KIND_NAMES[k])),
    el('li', {}, gapSwatch(), 'unmapped gap, compressed'),
    sample ? el('li', {}, svg('svg', { class: 'dg__swatch', width: 14, height: 14, viewBox: '0 0 14 14', 'aria-hidden': 'true' },
      svg('rect', { width: 14, height: 14, class: 'dg-unsampled' }), svg('path', { d: hatch(0, 0, 14, 14, 4), class: 'dg-unsampled-hatch' })), 'mappings not in the sample') : null,
    el('li', {}, el('span', { class: 'dg__swatch dg__swatch--outline', 'aria-hidden': 'true' }), 'outlined: a probe region'));
}

function gapSwatch() {
  return svg('svg', { class: 'dg__swatch', width: 14, height: 14, viewBox: '0 0 14 14', 'aria-hidden': 'true' },
    svg('rect', { width: 14, height: 14, class: 'dg-gap' }),
    svg('path', { d: zigzag(0, 14, 7, 3), class: 'dg-gap-zig' }));
}
