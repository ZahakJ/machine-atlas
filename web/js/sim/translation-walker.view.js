// View for the translation-walker simulation. mount(container, ctx) builds
// the controls once and re-renders the results on every change; it returns
// an unmount function. All arithmetic lives in translation-walker.model.js.

import { el, replace } from '../lib/dom.js';
import { badge } from '../lib/ui.js';
import { formatBytes, formatCount, formatHex } from '../lib/format.js';
import {
  PROFILES, DEFAULT_PROFILE_ID, profileById, parseAddress, walk, pageSizes,
  coverage, tlbReach, defaultExamples, observedExamples, levelShift,
} from './translation-walker.model.js';

const TLB_PRESETS = [
  { entries: 64, label: '64 entries, the size of many first-level data TLBs' },
  { entries: 1536, label: '1536 entries, the size of many second-level TLBs' },
];

let instance = 0;

/**
 * Mounts the simulation. ctx: {obs(id), derived(id), navigate(hash)}; every
 * lookup may return undefined, in which case defaults are used and the view
 * says so.
 */
export function mount(container, ctx = {}) {
  const uid = `tw${++instance}`;
  const seed = seedProfile(ctx);
  const observed = observedExamples((id) => {
    const o = ctx.obs?.(id);
    return o?.status === 'ok' && Array.isArray(o.value) ? o.value : undefined;
  });

  const state = {
    profile: profileById(seed.id),
    leaf: null, // null = base page
    text: '',
    tlbEntries: TLB_PRESETS[0].entries,
  };
  const firstExample = () => (observed[0] ?? defaultExamples(state.profile)[0]).address;
  state.text = formatHex(firstExample(), { group: 4 });

  // ---- controls (built once, so focus is never lost on re-render)
  const profileSelect = el('select', { class: 'select', id: `${uid}-profile`, onchange: () => {
    state.profile = profileById(profileSelect.value);
    state.leaf = null;
    renderExamples();
    renderPageSizes();
    render();
  } }, PROFILES.map((p) => el('option', { value: p.id, selected: p.id === state.profile.id }, `${p.title} (${p.id})`)));

  const addressInput = el('input', {
    class: 'input mono', id: `${uid}-addr`, type: 'text', inputmode: 'text', spellcheck: 'false',
    autocomplete: 'off', value: state.text, 'aria-describedby': `${uid}-addr-hint ${uid}-addr-err`,
    oninput: () => { state.text = addressInput.value; render(); },
  });
  const addressError = el('p', { class: 'field__error', id: `${uid}-addr-err`, role: 'alert' });
  const examplesBox = el('div', { class: 'cluster', role: 'group', 'aria-label': 'Example addresses' });
  const pageSizeBox = el('div', { class: 'tw__pagesizes' });
  const results = el('div', { class: 'tw__results', 'aria-live': 'polite', 'aria-atomic': 'false' });

  const tlbInput = el('input', {
    class: 'input mono tw__tlb-input', id: `${uid}-tlb`, type: 'number', min: '1', max: '100000', step: '1',
    value: String(state.tlbEntries),
    oninput: () => {
      const n = Math.floor(Number(tlbInput.value));
      if (n >= 1 && n <= 100000) { state.tlbEntries = n; renderTlb(); }
    },
  });
  const tlbBox = el('div');

  const root = el('section', { class: 'sim tw', 'aria-labelledby': `${uid}-title` },
    el('header', { class: 'sim__header' },
      el('div', {},
        el('p', { class: 'eyebrow' }, 'Simulation'),
        el('h3', { class: 'sim__title', id: `${uid}-title` }, 'Walk a virtual address through the page tables')),
      badge('simulated')),
    el('p', { class: 'sim__intro prose' },
      'Type any address, or pick one from the list, to see how the memory management unit splits it into table indices and follows them to a physical frame. The arithmetic is exact; the tables themselves are invented.'),
    el('ul', { class: 'sim__seed' }, seedNotes(seed, observed, ctx.mode).map((n) => el('li', { 'data-defaults': n.defaults ? 'true' : null }, n.text))),
    el('div', { class: 'sim__controls tw__controls' },
      el('div', { class: 'field' },
        el('label', { class: 'field__label', for: `${uid}-profile` }, 'Which paging format?'),
        profileSelect),
      el('div', { class: 'field' },
        el('label', { class: 'field__label', for: `${uid}-addr` }, 'Which virtual address?'),
        addressInput,
        el('p', { class: 'field__hint', id: `${uid}-addr-hint` }, 'Hexadecimal, up to 64 bits; 0x and underscores are optional.'),
        addressError),
      el('div', { class: 'field tw__examples' },
        el('span', { class: 'field__label' }, 'Examples'),
        examplesBox),
      pageSizeBox),
    results,
    el('section', { class: 'sim__panel' },
      panelHead('How much memory can the TLB translate without a walk?', 'derived'),
      el('div', { class: 'tw__tlb-controls' },
        el('div', { class: 'field' },
          el('label', { class: 'field__label', for: `${uid}-tlb` }, 'TLB entries'),
          tlbInput),
        el('div', { class: 'cluster' }, TLB_PRESETS.map((p) => el('button', {
          class: 'btn btn--small', type: 'button', title: p.label,
          onclick: () => { state.tlbEntries = p.entries; tlbInput.value = String(p.entries); renderTlb(); },
        }, `${p.entries}`)))),
      tlbBox),
    el('section', { class: 'sim__assumptions' },
      el('h4', {}, 'Assumptions and simplifications'),
      el('ul', { class: 'tw__assumptions' })),
  );

  function renderExamples() {
    const list = observed.length ? observed : defaultExamples(state.profile);
    replace(examplesBox, list.map((ex) => el('button', {
      class: 'btn btn--small tw__example', type: 'button',
      title: ex.source === 'default' ? 'Invented example' : `From ${ex.source} (observed on this machine)`,
      onclick: () => {
        state.text = formatHex(ex.address, { group: 4 });
        addressInput.value = state.text;
        render();
      },
    }, el('span', {}, ex.label), el('span', { class: 'mono tw__example-addr' }, formatHex(ex.address)))));
  }

  function renderPageSizes() {
    const sizes = pageSizes(state.profile);
    const name = `${uid}-leaf`;
    replace(pageSizeBox,
      el('fieldset', { class: 'field tw__fieldset' },
        el('legend', { class: 'field__label' }, 'Where does the walk end?'),
        el('div', { class: 'segmented' }, sizes.map((sz, i) => el('label', { title: sz.requires ? `Needs ${sz.requires}` : null },
          el('input', {
            type: 'radio', name, value: String(sz.leaf), checked: (state.leaf ?? sizes[0].leaf) === sz.leaf,
            onchange: () => { state.leaf = i === 0 ? null : sz.leaf; render(); },
          }),
          `${formatBytes(sz.bytes)} ${i === 0 ? 'page' : sz.term.split(' (')[0]}`))),
        el('p', { class: 'field__hint' }, 'A huge page is a leaf entry higher in the tree: the walk stops early and the remaining index bits become offset.',
          state.profile.hugeNote ? ` ${state.profile.hugeNote}` : '')));
  }

  function renderTlb() {
    const sizes = pageSizes(state.profile);
    replace(tlbBox, el('div', { class: 'table-wrap' }, el('table', { class: 'datatable' },
      el('thead', {}, el('tr', {}, el('th', {}, 'Page size'), el('th', { class: 'num' }, 'Reach'), el('th', { class: 'num' }, 'Exact bytes'))),
      el('tbody', {}, sizes.map((sz) => {
        const reach = tlbReach(state.tlbEntries, sz.bytes);
        return el('tr', {},
          el('td', {}, `${formatBytes(sz.bytes)} ${sz.term}`),
          el('td', { class: 'num' }, formatBytes(reach)),
          el('td', { class: 'num' }, formatCount(reach)));
      })))),
    el('p', { class: 'caption' }, `${formatCount(state.tlbEntries)} entries × page size. Beyond that, each miss costs up to ${state.profile.levels.length} dependent memory reads (fewer with page-walk caches, more inside a virtual machine).`));
  }

  function render() {
    const parsed = parseAddress(state.text);
    addressInput.setAttribute('aria-invalid', parsed.ok ? 'false' : 'true');
    replace(addressError, parsed.ok ? '' : parsed.error);
    if (!parsed.ok) {
      replace(results, el('p', { class: 'empty' }, 'Enter a valid address to see its translation.'));
      renderTlb();
      renderAssumptions();
      return;
    }
    const p = state.profile;
    const leaf = state.leaf ?? p.levels.length - 1;
    const w = walk(p, parsed.value, leaf);
    replace(results,
      renderSplit(p, w, parsed.value),
      renderCanonical(p, w),
      renderWalk(p, w),
      renderCoverage(p, leaf));
    renderTlb();
    renderAssumptions();
  }

  function renderAssumptions() {
    const list = root.querySelector('.tw__assumptions');
    replace(list, assumptions(state.profile).map((a) => el('li', {}, a)));
  }

  renderExamples();
  renderPageSizes();
  render();
  container.appendChild(root);
  return () => root.remove();
}

// ---------------------------------------------------------------- pieces

function seedProfile(ctx) {
  const f = ctx.derived?.('derived.paging.profile');
  if (f?.status === 'ok' && profileById(f.value)) return { id: f.value, fromSnapshot: true };
  const why = f ? `status ${f.status}${f.reason ? `: ${f.reason}` : ''}` : 'no snapshot';
  return { id: DEFAULT_PROFILE_ID, fromSnapshot: false, why };
}

// Where the probe's addresses came from, by the mode of the active snapshot.
const PROBE_SOURCE = {
  live: 'Example addresses are real ones from the probe child Machine Atlas ran when it collected this machine (probe.regions, probe.layout.runs).',
  snapshot: 'Example addresses are real ones from the probe child recorded in this snapshot, on the machine it was captured from (probe.regions, probe.layout.runs).',
  fixture: 'Example addresses come from the demonstration snapshot: a probe on the machine it models, not on yours (probe.regions, probe.layout.runs).',
};

function seedNotes(seed, observed, mode) {
  const notes = [];
  notes.push(seed.fromSnapshot
    ? { text: `Paging format ${seed.id}, as derived for this machine (derived.paging.profile).` }
    : { text: `Defaults in use: this machine's paging format is not known (derived.paging.profile, ${seed.why}), so ${DEFAULT_PROFILE_ID} is shown.`, defaults: true });
  notes.push(observed.length
    ? { text: PROBE_SOURCE[mode] ?? 'Example addresses are the ones recorded by the snapshot’s probe child (probe.regions, probe.layout.runs).' }
    : { text: 'Defaults in use: no probe data, so the example addresses are invented in the shape of a typical process.', defaults: true });
  return notes;
}

function panelHead(title, label) {
  return el('div', { class: 'sim__panel-head' }, el('h4', {}, title), badge(label));
}

function bitRange(f) {
  return f.hi === f.lo ? `bit ${f.hi}` : `bits ${f.hi}–${f.lo}`;
}

function binary(value, width) {
  return value.toString(2).padStart(width, '0');
}

function renderSplit(p, w, address) {
  const fields = w.fields;
  return el('section', { class: 'sim__panel' },
    panelHead('1 · Split the address', 'derived'),
    el('p', { class: 'tw__address mono', 'aria-label': `Address ${formatHex(address)}` }, formatHex(address, { digits: 16, group: 4 })),
    el('ol', { class: 'bitfield', 'aria-label': 'Bit fields, most significant first' }, fields.map((f) => {
      const width = f.hi - f.lo + 1;
      const isIndex = f.kind === 'index';
      const seg = el('li', { class: 'bitfield__seg', 'data-kind': f.kind, 'data-level': isIndex ? String(f.level % 5) : null },
        el('span', { class: 'bitfield__name' }, f.name),
        el('span', { class: 'bitfield__range' }, `${bitRange(f)} · ${width} bit${width === 1 ? '' : 's'}`),
        el('span', { class: 'bitfield__value mono' }, isIndex ? `index ${f.value}` : formatHex(f.value)),
        el('span', { class: 'bitfield__bits mono', 'aria-hidden': 'true' }, binary(f.value, width)));
      // Width in proportion to the bit count. CSSOM, not a style attribute,
      // so the page's CSP (style-src 'self') allows it.
      seg.style.setProperty('--bits', String(width));
      return seg;
    })),
    el('p', { class: 'caption' }, splitCaption(p, w)));
}

function splitCaption(p, w) {
  const idx = w.fields.filter((f) => f.kind === 'index');
  const off = w.fields.find((f) => f.kind === 'offset');
  return `${idx.map((f) => f.hi - f.lo + 1).join(' + ')} index bits + ${off.hi + 1} offset bits = ${p.vaBits} translated bits. ` +
    `The offset passes through translation unchanged; each index selects one of the entries in a table.`;
}

function renderCanonical(p, w) {
  const c = w.canonical;
  return el('section', { class: 'sim__panel' },
    panelHead('2 · Is the address canonical?', 'derived'),
    el('p', { class: 'tw__verdict', 'data-ok': c.ok ? 'true' : 'false' },
      el('strong', {}, c.ok ? 'Yes. ' : 'No. '),
      c.ok
        ? `It is in the ${c.half} half; the walk starts from the table in ${c.register}.`
        : c.consequence),
    el('p', { class: 'caption' }, c.rule, ' ', p.split === 'ttbr'
      ? 'On arm64 each half has its own root table, so all translated bits index the table for that half.'
      : `Bit ${p.vaBits - 1} is both the sign and part of the top index: user entries fill the lower half of the root table, kernel entries the upper half.`));
}

function sim(value) {
  return el('span', { class: 'val val--sim', title: 'Invented (simulated)' }, formatHex(value));
}

function der(text) {
  return el('span', { class: 'val val--derived', title: 'Exact (derived from the address)' }, text);
}

function renderWalk(p, w) {
  const panel = el('section', { class: 'sim__panel' },
    panelHead('3 · Walk the tables', 'simulated'),
    el('p', { class: 'caption tw__why-sim' },
      'Indices and offsets below are exact. The table addresses and the frame are invented from the address with a fixed hash, because an unprivileged program cannot read the real page tables (and /proc/self/pagemap hides frame numbers from it).'));
  if (!w.canonical.ok) {
    panel.appendChild(el('p', { class: 'empty' }, 'No walk: the hardware rejects a non-canonical address before reading any table.'));
    return panel;
  }
  panel.appendChild(el('ol', { class: 'walk' },
    el('li', { class: 'walk__step walk__step--root' },
      el('span', { class: 'walk__level' }, p.root.split(' / ')[w.canonical.half === 'kernel' && p.split === 'ttbr' ? 1 : 0]),
      el('span', {}, 'holds the physical address of the root table: ', sim(w.steps[0].tableBase))),
    w.steps.map((s) => {
      const leafSize = s.points === 'frame' ? formatBytes(w.pageBytes) : null;
      return el('li', { class: 'walk__step', 'data-points': s.points },
        el('span', { class: 'walk__level' }, s.name, el('span', { class: 'walk__linux' }, s.linux)),
        el('span', {},
          'entry ', der(String(s.index)), ' × 8 bytes = ', der(formatHex(s.entryOffset)),
          ' into the table at ', sim(s.tableBase), ' → reads ', sim(s.entryAddress), '. ',
          s.points === 'table'
            ? ['It points to the next table at ', sim(s.target), '.']
            : ['It is a leaf: a ', leafSize, ' frame at ', sim(s.target), '.']));
    }),
    el('li', { class: 'walk__step walk__step--result' },
      el('span', { class: 'walk__level' }, 'Result'),
      el('span', {},
        'frame ', sim(w.frame), ' + offset ', der(formatHex(w.offset)), ' = physical ', sim(w.physical),
        `. ${w.steps.length} table read${w.steps.length === 1 ? '' : 's'} on a TLB miss; none on a hit.`))));
  return panel;
}

function renderCoverage(p, leaf) {
  return el('section', { class: 'sim__panel' },
    panelHead('4 · How much does one entry map?', 'derived'),
    el('div', { class: 'table-wrap' }, el('table', { class: 'datatable' },
      el('thead', {}, el('tr', {},
        el('th', {}, 'Level'), el('th', { class: 'num' }, 'Index bits'), el('th', { class: 'num' }, 'Entries'),
        el('th', { class: 'num' }, 'One entry maps'), el('th', { class: 'num' }, 'Whole table maps'))),
      el('tbody', {}, coverage(p).map((c) => el('tr', { 'aria-current': c.level === leaf ? 'true' : null, class: c.level > leaf ? 'is-skipped' : null },
        el('td', {}, c.name, ' ', el('span', { class: 'caption mono' }, c.linux)),
        el('td', { class: 'num' }, String(c.bits)),
        el('td', { class: 'num' }, formatCount(c.entries)),
        el('td', { class: 'num' }, formatBytes(c.entryBytes)),
        el('td', { class: 'num' }, formatBytes(c.tableBytes))))))),
    el('p', { class: 'caption' }, `An entry at level n maps 2^(bits below it) bytes: ${p.offsetBits} offset bits for the last level, plus each lower level's index bits above that. The highlighted row is where the current walk ends${leaf < p.levels.length - 1 ? '; the rows below it are skipped' : ''}.`));
}

function assumptions(p) {
  const list = [
    `Index arithmetic follows ${p.reference} for ${p.id}; it is exact.`,
    'Table addresses, entry contents and frames are invented (deterministically, from the address) within a 64 GiB physical space; real ones are visible only to the kernel.',
    'Every entry is shown as present. A real walk can stop at a non-present entry, which is a page fault (see the page-faults lesson).',
    'Permission, accessed and dirty bits, PCID/ASID tags and memory attributes are not modelled.',
    'Inside a virtual machine every guest table read is itself translated by a second stage the guest cannot see; that is not shown.',
  ];
  if (p.split === 'ttbr') list.push('Top-byte ignore is not modelled: Linux enables it for user addresses on arm64, so tags in bits 63–56 of a user pointer are ignored by the hardware.');
  for (const h of p.huge) if (h.requires) list.push(`A ${formatBytes(2n ** BigInt(levelShift(p, h.level)))} page needs ${h.requires}.`);
  return list;
}
