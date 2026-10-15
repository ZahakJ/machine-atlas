// Pure model of a hardware page-table walk, for the translation-walker
// simulation (ARCHITECTURE.md §9).
//
// Two kinds of numbers come out of this module and the view labels them
// differently. The split of an address into indices and offset, the canonical
// check, page sizes, coverage and TLB reach are exact consequences of the
// paging format: DERIVED. Table base addresses, entry contents and frame
// numbers are invented, deterministically from the address, because
// unprivileged user space cannot read the real page tables: SIMULATED.
//
// All address arithmetic is BigInt; a JavaScript number cannot hold a 64-bit
// address exactly. The module never touches the DOM.

const MASK64 = (1n << 64n) - 1n;

/** Width of the invented physical address space the simulated frames live in. */
export const SIM_PHYS_BITS = 36;

/** Bytes per page-table entry; 8 in every profile here. */
export const ENTRY_BYTES = 8n;

const lvl = (name, linux, bits) => Object.freeze({ name, linux, bits });

/**
 * The paging formats the simulator knows. `levels` run from the root table
 * down to the last-level table; `linux` is the kernel's generic name for the
 * same level (pgd, p4d, pud, pmd, pte), with folded levels omitted.
 * `huge` lists the levels at which a leaf entry may end the walk early.
 *
 * `split` says how the bits above the translated range are interpreted:
 * - "sign-extend": x86-64 and RISC-V. The bits above va-1 must copy bit va-1;
 *   one root table covers both halves and bit va-1 is part of the top index.
 * - "ttbr": arm64. The bits above va-1 must be all zeros (TTBR0_EL1, user) or
 *   all ones (TTBR1_EL1, kernel); each half has its own root table and the
 *   full va bits index it.
 */
export const PROFILES = Object.freeze([
  {
    id: 'x86_64-4level',
    arch: 'x86_64',
    title: 'x86-64, 4-level paging, 4 KiB pages',
    vaBits: 48,
    offsetBits: 12,
    split: 'sign-extend',
    root: 'CR3',
    levels: [lvl('PML4', 'pgd', 9), lvl('PDPT', 'pud', 9), lvl('PD', 'pmd', 9), lvl('PT', 'pte', 9)],
    huge: [
      { level: 2, term: 'large page (PD entry with PS=1)' },
      { level: 1, term: 'huge page (PDPT entry with PS=1)', requires: 'the pdpe1gb CPU flag' },
    ],
    reference: 'Intel SDM vol. 3A §4.5; docs.kernel.org arch/x86/x86_64/mm.html',
  },
  {
    id: 'x86_64-5level',
    arch: 'x86_64',
    title: 'x86-64, 5-level paging (LA57), 4 KiB pages',
    vaBits: 57,
    offsetBits: 12,
    split: 'sign-extend',
    root: 'CR3',
    levels: [
      lvl('PML5', 'pgd', 9), lvl('PML4', 'p4d', 9), lvl('PDPT', 'pud', 9),
      lvl('PD', 'pmd', 9), lvl('PT', 'pte', 9),
    ],
    huge: [
      { level: 3, term: 'large page (PD entry with PS=1)' },
      { level: 2, term: 'huge page (PDPT entry with PS=1)', requires: 'the pdpe1gb CPU flag' },
    ],
    reference: 'Intel SDM vol. 3A §4.5; docs.kernel.org arch/x86/x86_64/5level-paging.html',
  },
  {
    id: 'arm64-4k-48',
    arch: 'arm64',
    title: 'arm64, 4 KiB granule, 48-bit VA',
    vaBits: 48,
    offsetBits: 12,
    split: 'ttbr',
    root: 'TTBR0_EL1 / TTBR1_EL1',
    levels: [lvl('L0', 'pgd', 9), lvl('L1', 'pud', 9), lvl('L2', 'pmd', 9), lvl('L3', 'pte', 9)],
    huge: [
      { level: 2, term: 'level 2 block' },
      { level: 1, term: 'level 1 block' },
    ],
    reference: 'Arm ARM D8 (VMSAv8-64 translation); docs.kernel.org arch/arm64/memory.html',
  },
  {
    id: 'arm64-4k-39',
    arch: 'arm64',
    title: 'arm64, 4 KiB granule, 39-bit VA',
    vaBits: 39,
    offsetBits: 12,
    split: 'ttbr',
    root: 'TTBR0_EL1 / TTBR1_EL1',
    levels: [lvl('L1', 'pgd', 9), lvl('L2', 'pmd', 9), lvl('L3', 'pte', 9)],
    huge: [
      { level: 1, term: 'level 2 block' },
      { level: 0, term: 'level 1 block' },
    ],
    reference: 'Arm ARM D8 (VMSAv8-64 translation); docs.kernel.org arch/arm64/memory.html',
  },
  {
    // 47 = 14 + 3 × 11: three full levels and no level 0. Linux selects three
    // levels for ARM64_16K_PAGES with ARM64_VA_BITS_47.
    id: 'arm64-16k-47',
    arch: 'arm64',
    title: 'arm64, 16 KiB granule, 47-bit VA',
    vaBits: 47,
    offsetBits: 14,
    split: 'ttbr',
    root: 'TTBR0_EL1 / TTBR1_EL1',
    levels: [lvl('L1', 'pgd', 11), lvl('L2', 'pmd', 11), lvl('L3', 'pte', 11)],
    huge: [{ level: 1, term: 'level 2 block' }],
    hugeNote: 'With a 16 KiB granule a level 1 block (64 GiB) exists only with FEAT_LPA2, so it is not offered.',
    reference: 'Arm ARM D8 (VMSAv8-64 translation); docs.kernel.org arch/arm64/memory.html',
  },
  {
    // 48 = 14 + 1 + 3 × 11: the level 0 table resolves a single bit and so
    // has two entries.
    id: 'arm64-16k-48',
    arch: 'arm64',
    title: 'arm64, 16 KiB granule, 48-bit VA',
    vaBits: 48,
    offsetBits: 14,
    split: 'ttbr',
    root: 'TTBR0_EL1 / TTBR1_EL1',
    levels: [lvl('L0', 'pgd', 1), lvl('L1', 'pud', 11), lvl('L2', 'pmd', 11), lvl('L3', 'pte', 11)],
    huge: [{ level: 2, term: 'level 2 block' }],
    hugeNote: 'With a 16 KiB granule a level 1 block (64 GiB) exists only with FEAT_LPA2, so it is not offered.',
    reference: 'Arm ARM D8 (VMSAv8-64 translation); docs.kernel.org arch/arm64/memory.html',
  },
  {
    id: 'arm64-64k-48',
    arch: 'arm64',
    title: 'arm64, 64 KiB granule, 48-bit VA',
    vaBits: 48,
    offsetBits: 16,
    split: 'ttbr',
    root: 'TTBR0_EL1 / TTBR1_EL1',
    levels: [lvl('L1', 'pgd', 6), lvl('L2', 'pmd', 13), lvl('L3', 'pte', 13)],
    huge: [{ level: 1, term: 'level 2 block' }],
    hugeNote: 'With a 64 KiB granule a level 1 block (4 TiB) exists only with FEAT_LPA, so it is not offered.',
    reference: 'Arm ARM D8 (VMSAv8-64 translation); docs.kernel.org arch/arm64/memory.html',
  },
  {
    id: 'riscv-sv39',
    arch: 'riscv64',
    title: 'RISC-V Sv39, 4 KiB pages',
    vaBits: 39,
    offsetBits: 12,
    split: 'sign-extend',
    root: 'satp',
    levels: [lvl('VPN[2]', 'pgd', 9), lvl('VPN[1]', 'pmd', 9), lvl('VPN[0]', 'pte', 9)],
    huge: [
      { level: 1, term: 'megapage' },
      { level: 0, term: 'gigapage' },
    ],
    reference: 'RISC-V Privileged Architecture §12.4 (Sv39)',
  },
  {
    id: 'riscv-sv48',
    arch: 'riscv64',
    title: 'RISC-V Sv48, 4 KiB pages',
    vaBits: 48,
    offsetBits: 12,
    split: 'sign-extend',
    root: 'satp',
    levels: [
      lvl('VPN[3]', 'pgd', 9), lvl('VPN[2]', 'pud', 9),
      lvl('VPN[1]', 'pmd', 9), lvl('VPN[0]', 'pte', 9),
    ],
    // "Any level of PTE may be a leaf PTE, so in addition to 4 KiB pages,
    // Sv48 supports 2 MiB megapages, 1 GiB gigapages, and 512 GiB
    // terapages" (privileged spec, Sv48): a leaf in the root table too.
    huge: [
      { level: 2, term: 'megapage' },
      { level: 1, term: 'gigapage' },
      { level: 0, term: 'terapage' },
    ],
    reference: 'RISC-V Privileged Architecture §12.5 (Sv48)',
  },
].map((p) => Object.freeze(p)));

/** The profile used when the snapshot does not name one. */
export const DEFAULT_PROFILE_ID = 'x86_64-4level';

/** Returns the profile with this id, or undefined. */
export function profileById(id) {
  return PROFILES.find((p) => p.id === id);
}

const pow2 = (n) => 1n << BigInt(n);
const bitsOf = (value, hi, lo) => (value >> BigInt(lo)) & (pow2(hi - lo + 1) - 1n);

/**
 * Number of address bits translated below level `i` (the offset plus every
 * lower index). An entry at level i therefore covers 2^shift(i) bytes.
 */
export function levelShift(profile, i) {
  let shift = profile.offsetBits;
  for (let j = i + 1; j < profile.levels.length; j++) shift += profile.levels[j].bits;
  return shift;
}

/**
 * Parses a user-typed virtual address. Hex digits with an optional 0x prefix;
 * underscores, spaces and the apostrophes some debuggers print are ignored.
 * Returns {ok: true, value: BigInt} or {ok: false, error}.
 */
export function parseAddress(text) {
  const cleaned = String(text).trim().replace(/[\s_'`]/g, '').replace(/^0x/i, '');
  if (cleaned === '') return { ok: false, error: 'Enter an address in hexadecimal, for example 0x7ffd5e8c3a10.' };
  if (!/^[0-9a-f]+$/i.test(cleaned)) return { ok: false, error: 'Only hexadecimal digits 0–9 and a–f are allowed.' };
  const value = BigInt('0x' + cleaned);
  if (value > MASK64) return { ok: false, error: 'An address has at most 64 bits (16 hex digits).' };
  return { ok: true, value };
}

/**
 * The canonical-address check: whether the bits above the translated range
 * have a form the MMU accepts, and if so which half of the address space
 * (and which root register) the address belongs to.
 */
export function canonical(profile, address) {
  const va = profile.vaBits;
  const upper = bitsOf(address, 63, va); // bits 63..va
  const allOnes = pow2(64 - va) - 1n;
  if (profile.split === 'ttbr') {
    if (upper === 0n) return { ok: true, half: 'user', register: 'TTBR0_EL1', rule: ruleText(profile) };
    if (upper === allOnes) return { ok: true, half: 'kernel', register: 'TTBR1_EL1', rule: ruleText(profile) };
    return {
      ok: false, half: null, register: null, rule: ruleText(profile),
      consequence: 'The access takes a level 0 translation fault: the address lies in neither half.',
    };
  }
  const signBit = bitsOf(address, va - 1, va - 1);
  if (upper === (signBit ? allOnes : 0n)) {
    return { ok: true, half: signBit ? 'kernel' : 'user', register: profile.root, rule: ruleText(profile) };
  }
  return {
    ok: false, half: null, register: null, rule: ruleText(profile),
    consequence: profile.arch === 'x86_64'
      ? 'The access raises a general-protection fault (#GP), not a page fault; no table is read.'
      : 'The access raises a page-fault exception before any table is read.',
  };
}

function ruleText(profile) {
  const va = profile.vaBits;
  if (profile.split === 'ttbr') {
    return `Bits 63–${va} must be all zeros (user half, TTBR0_EL1) or all ones (kernel half, TTBR1_EL1).`;
  }
  return `Bits 63–${va} must all equal bit ${va - 1} (sign extension).`;
}

/** The page sizes a walk can end in: the base page, then each huge leaf. */
export function pageSizes(profile) {
  const base = { leaf: profile.levels.length - 1, bytes: pow2(profile.offsetBits), term: 'base page' };
  const huge = profile.huge.map((h) => ({ leaf: h.level, bytes: pow2(levelShift(profile, h.level)), term: h.term, requires: h.requires }));
  return [base, ...huge];
}

/**
 * Splits an address into fields, most significant first. `leaf` is the level
 * whose entry maps the page (the last level for a base page); the bits of the
 * levels below it join the offset.
 */
export function splitAddress(profile, address, leaf = profile.levels.length - 1) {
  const fields = [];
  const va = profile.vaBits;
  if (va < 64) {
    fields.push({
      kind: profile.split === 'ttbr' ? 'select' : 'sign',
      name: profile.split === 'ttbr' ? 'half select' : 'sign extension',
      hi: 63, lo: va, value: bitsOf(address, 63, va),
    });
  }
  let hi = va - 1;
  for (let i = 0; i <= leaf; i++) {
    const { name, linux, bits } = profile.levels[i];
    fields.push({ kind: 'index', level: i, name, linux, hi, lo: hi - bits + 1, value: bitsOf(address, hi, hi - bits + 1) });
    hi -= bits;
  }
  fields.push({ kind: 'offset', name: 'page offset', hi, lo: 0, value: bitsOf(address, hi, 0) });
  return fields;
}

// splitmix64: a well-mixed, deterministic 64-bit hash, so the invented table
// addresses look random but are the same on every run for the same input.
function mix64(x) {
  let z = (x + 0x9e3779b97f4a7c15n) & MASK64;
  z = ((z ^ (z >> 30n)) * 0xbf58476d1ce4e5b9n) & MASK64;
  z = ((z ^ (z >> 27n)) * 0x94d049bb133111ebn) & MASK64;
  return z ^ (z >> 31n);
}

/**
 * An invented, `alignBits`-aligned, non-zero physical address derived from
 * `key`. A page too large for SIM_PHYS_BITS (a 512 GiB Sv48 terapage) gets a
 * physical space just wide enough to hold a few such frames.
 */
function inventPhysical(key, alignBits) {
  const slots = pow2(Math.max(SIM_PHYS_BITS, alignBits + 4) - alignBits);
  return ((mix64(key) % (slots - 1n)) + 1n) << BigInt(alignBits);
}

/**
 * Walks the (simulated) tables for `address`, ending at level `leaf`.
 * Returns the canonical check, the fields, one step per level visited and
 * the resulting physical address. For a non-canonical address the walk is
 * empty: the hardware never starts it.
 *
 * Exact (DERIVED): every index, entry offset and the page offset.
 * Invented (SIMULATED): every table base, entry content and frame.
 */
export function walk(profile, address, leaf = profile.levels.length - 1) {
  if (leaf !== profile.levels.length - 1 && !profile.huge.some((h) => h.level === leaf)) {
    throw new RangeError(`${profile.id} has no leaf entries at level ${leaf}`);
  }
  const check = canonical(profile, address);
  const fields = splitAddress(profile, address, leaf);
  const pageShift = levelShift(profile, leaf);
  const pageBytes = pow2(pageShift);
  const offset = address & (pageBytes - 1n);
  if (!check.ok) return { canonical: check, fields, steps: [], pageBytes, offset, frame: null, frameNumber: null, physical: null };

  // Each half of an arm64 address space has its own root; x86-64 and RISC-V
  // share one root for both halves.
  const salt = profile.split === 'ttbr' && check.half === 'kernel' ? 0x5eedn : 0n;
  const tableAlign = profile.offsetBits;
  let base = inventPhysical(salt ^ 0xc0ffeen, tableAlign);
  const steps = [];
  let frame = null;
  for (let i = 0; i <= leaf; i++) {
    const shift = levelShift(profile, i);
    const index = bitsOf(address, shift + profile.levels[i].bits - 1, shift);
    // The address bits above this level's index name the next table uniquely.
    const key = (address & ~(pow2(shift) - 1n)) ^ salt ^ BigInt(i + 1);
    const isLeaf = i === leaf;
    const target = inventPhysical(key, isLeaf ? pageShift : tableAlign);
    steps.push({
      level: i,
      name: profile.levels[i].name,
      linux: profile.levels[i].linux,
      index,
      tableBase: base,
      entryOffset: index * ENTRY_BYTES,
      entryAddress: base + index * ENTRY_BYTES,
      points: isLeaf ? 'frame' : 'table',
      target,
    });
    if (isLeaf) frame = target;
    base = target;
  }
  return {
    canonical: check, fields, steps, pageBytes, offset,
    frame, frameNumber: frame >> BigInt(profile.offsetBits), physical: frame + offset,
  };
}

/**
 * What each level of the tree spans: entries per table and how much address
 * space one entry, and so one whole table, maps.
 */
export function coverage(profile) {
  return profile.levels.map((l, i) => {
    const entryBytes = pow2(levelShift(profile, i));
    const entries = pow2(l.bits);
    return { level: i, name: l.name, linux: l.linux, bits: l.bits, entries, entryBytes, tableBytes: entries * entryBytes };
  });
}

/** Memory a TLB with `entries` entries maps without a walk, for one page size. */
export function tlbReach(entries, pageBytes) {
  return BigInt(entries) * BigInt(pageBytes);
}

/** First address past the user half: the user half is [0, userTop). */
export function userTop(profile) {
  return pow2(profile.split === 'ttbr' ? profile.vaBits : profile.vaBits - 1);
}

/**
 * Example addresses shaped like a real process on this profile, used when the
 * snapshot has none: a PIE executable at two thirds of the user half (where
 * Linux's ELF_ET_DYN_BASE puts it), a heap after it, a library mapping and a
 * stack just under the top, one kernel-half address and, where the profile
 * leaves bits unused, one non-canonical address.
 */
export function defaultExamples(profile) {
  const top = userTop(profile);
  const page = (x) => x & ~0xfffn;
  const exe = page((top * 2n) / 3n) + 0x1040n;
  const examples = [
    { label: 'executable code (invented)', address: exe },
    { label: 'heap (invented)', address: page(exe + 0x2a61000n) + 0x2a0n },
    { label: 'shared library (invented)', address: page(top - (top >> 7n)) + 0x3a2c1f80n },
    { label: 'stack (invented)', address: top - 0x1e5f0n },
  ];
  const kernelBase = profile.split === 'ttbr'
    ? MASK64 & ~(pow2(profile.vaBits) - 1n)
    : MASK64 & ~(pow2(profile.vaBits - 1) - 1n);
  examples.push({ label: 'kernel half (invented)', address: kernelBase + (top >> 4n) + 0x1000n });
  if (profile.vaBits < 63) {
    examples.push({ label: 'non-canonical (invented)', address: pow2(62) + exe });
  }
  return examples.map((e) => ({ ...e, source: 'default' }));
}

/**
 * Example addresses from observations: the start of each probe region and the
 * stack, mmap base, heap and executable of the first ASLR run. `rows(id)`
 * returns the table rows of an `ok` table observation or undefined.
 */
export function observedExamples(rows) {
  const out = [];
  for (const r of rows('probe.regions') ?? []) {
    const p = parseAddress(r.start ?? '');
    if (p.ok) out.push({ label: `probe region ${r.name}`, address: p.value, source: 'probe.regions' });
  }
  const run = (rows('probe.layout.runs') ?? [])[0];
  if (run) {
    for (const key of ['exe', 'heap', 'mmap_base', 'stack']) {
      const p = parseAddress(run[key] ?? '');
      if (p.ok) out.push({ label: `${key.replace('_', ' ')} (ASLR run ${run.run})`, address: p.value, source: 'probe.layout.runs' });
    }
  }
  return out;
}
