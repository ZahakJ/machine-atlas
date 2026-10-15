// Pure model of page reclaim on a toy machine, for the reclaim simulation
// (ARCHITECTURE.md §9).
//
// A textbook two-list model, not this kernel's policy: one zone, one node,
// 96 frames, an active and an inactive list per page type, min/low/high
// watermarks, an optional swap area and swappiness. `step(state, action)` is a
// deterministic reducer: the same state and action always give the same next
// state, and the input state is never modified. Nothing here is random and
// nothing touches the DOM.
//
// Every kernel decision appends an event whose text states the decision and
// its cause. The counters are named after their /proc/vmstat counterparts so
// a learner can find the real ones, but they count events in this toy only.

/** The assumptions and simplifications the view must show prominently. */
export const ASSUMPTIONS = Object.freeze([
  'A two-list model (active and inactive per type) with second chances; Linux 6.1+ may use the Multi-Gen LRU instead, which ranks pages by generation.',
  'One node and one zone; real machines have several zones, each with its own watermarks.',
  '96 frames and 48 swap slots: each frame stands for a large amount of memory, not a 4 KiB page.',
  'No slab caches, no shrinkers, no kernel allocations, no memory cgroups, no compaction.',
  'Writeback completes only when you let time pass; real writeback runs continuously and writers are throttled when too much is dirty.',
  'Swap-out is immediate and a swapped-in page frees its slot; real swap goes through the swap cache and keeps the copy while it can.',
  'Scan balance uses swappiness and a fixed batch of 8 pages; the kernel also weighs the recent cost of refaults and scans more as pressure rises.',
  'The OOM killer is never run: the simulator only says that it would be invoked.',
]);

const BATCH = 8;          // pages scanned per reclaim pass
const MAX_PASSES = 32;    // bound on reclaim passes in one go; a real kernel also gives up eventually
const LOG_LIMIT = 300;

/** Default configuration; seeded values override swapPresent and swappiness. */
export const DEFAULTS = Object.freeze({
  frames: 96,
  swapSlots: 48,
  swapPresent: true,
  swappiness: 60,
  watermarks: Object.freeze({ min: 6, low: 10, high: 14 }),
});

/** Counter names, in display order. */
export const COUNTERS = Object.freeze([
  'pgscan_kswapd', 'pgscan_direct', 'pgsteal_kswapd', 'pgsteal_direct',
  'pswpout', 'pswpin', 'workingset_refault_file', 'workingset_refault_anon',
  'workingset_activate', 'pgmajfault', 'allocstall', 'oom_would_kill',
]);

/**
 * A fresh machine: every frame free, swap configured or not.
 * `opts` may set swapPresent and swappiness (other fields keep defaults).
 */
export function initialState(opts = {}) {
  const config = {
    frames: DEFAULTS.frames,
    swapSlots: DEFAULTS.swapSlots,
    swapPresent: opts.swapPresent ?? DEFAULTS.swapPresent,
    swappiness: clampSwappiness(opts.swappiness ?? DEFAULTS.swappiness),
    watermarks: { ...DEFAULTS.watermarks },
  };
  return {
    config,
    step: 0,
    frames: new Array(config.frames).fill(null), // page id per frame, null = free
    swap: new Array(config.swapSlots).fill(null), // page id per slot, null = free
    pages: {},                                     // id -> page record
    owned: { file: [], anon: [] },                 // the workload's pages, oldest first
    // Each list is ordered tail first: index 0 is the oldest page, the end is
    // the head where newly added pages go.
    lru: { anon: { active: [], inactive: [] }, file: { active: [], inactive: [] } },
    kswapd: 'asleep',
    age: 0, // the workingset clock: advances on every eviction and activation
    counters: Object.fromEntries(COUNTERS.map((c) => [c, 0])),
    events: [],
    nextId: { file: 0, anon: 0 },
  };
}

function clampSwappiness(v) {
  const n = Math.round(Number(v));
  return Number.isFinite(n) ? Math.min(200, Math.max(0, n)) : DEFAULTS.swappiness;
}

/** Number of free frames. */
export function freeFrames(state) {
  return state.frames.reduce((n, f) => n + (f === null ? 1 : 0), 0);
}

/** Resident pages by kind: {free, fileClean, fileDirty, anon}. */
export function frameSummary(state) {
  const out = { free: 0, fileClean: 0, fileDirty: 0, anon: 0 };
  for (const id of state.frames) {
    if (id === null) out.free++;
    else if (state.pages[id].type === 'anon') out.anon++;
    else if (state.pages[id].dirty) out.fileDirty++;
    else out.fileClean++;
  }
  return out;
}

/**
 * Seeds the toy from the snapshot. `obs` and `derived` are the ctx lookups;
 * values are used only when their status is ok. Returns the options for
 * initialState and one note per input ({text, defaults}) saying where it
 * came from.
 */
export function seedFrom(obs, derived) {
  const opts = {};
  const notes = [];
  const swap = derived?.('derived.swap.configured');
  if (swap?.status === 'ok' && typeof swap.value === 'boolean') {
    opts.swapPresent = swap.value;
    notes.push({ text: `Swap ${swap.value ? 'present' : 'absent'}, as on this machine (derived.swap.configured).`, defaults: false });
  } else {
    notes.push({ text: 'Defaults in use: swap present, because whether this machine has swap is not known (derived.swap.configured unavailable).', defaults: true });
  }
  const sw = obs?.('mem.sysctl.vm.swappiness');
  if (sw?.status === 'ok' && typeof sw.value === 'number') {
    opts.swappiness = sw.value;
    notes.push({ text: `Swappiness ${clampSwappiness(sw.value)}, as on this machine (mem.sysctl.vm.swappiness).`, defaults: false });
  } else {
    notes.push({ text: `Defaults in use: swappiness ${DEFAULTS.swappiness}, the kernel default, because this machine's value is not available (mem.sysctl.vm.swappiness).`, defaults: true });
  }
  return { opts, notes };
}

/**
 * The reducer. Actions:
 *   {type:'read', pages}      read new file data into the page cache
 *   {type:'write', pages}     write new file data (the pages start dirty)
 *   {type:'alloc', pages}     allocate and write anonymous memory
 *   {type:'touch', kind, pages}  access the oldest `pages` pages of kind
 *                             'file' or 'anon' again (refaulting evicted ones)
 *   {type:'tick'}             let time pass: the flusher writes dirty pages back
 *   {type:'swap', present}    swapon / swapoff
 *   {type:'swappiness', value}
 */
export function step(state, action) {
  const s = structuredClone(state);
  s.step++;
  const act = newActionLog();
  switch (action.type) {
    case 'read':
    case 'write':
      addFilePages(s, act, count(action.pages), action.type === 'write');
      break;
    case 'alloc':
      addAnonPages(s, act, count(action.pages));
      break;
    case 'touch':
      touch(s, act, action.kind === 'anon' ? 'anon' : 'file', count(action.pages));
      break;
    case 'tick':
      flush(s);
      if (freeFrames(s) < s.config.watermarks.low) {
        s.kswapd = 'awake';
        emit(s, 'kswapd', 'kswapd-wake', `Free frames are still below the low watermark (${s.config.watermarks.low}); with writeback finished there may be clean pages to drop, so kswapd tries again.`);
      }
      break;
    case 'swap':
      setSwap(s, Boolean(action.present));
      break;
    case 'swappiness':
      setSwappiness(s, action.value);
      break;
    default:
      throw new TypeError(`unknown action ${action.type}`);
  }
  finishAction(s, act);
  // kswapd runs in the background, which the toy models as "after the task's
  // action": that is why a large burst can outrun it and hit direct reclaim.
  if (s.kswapd === 'awake') runKswapd(s);
  if (s.events.length > LOG_LIMIT) s.events.splice(0, s.events.length - LOG_LIMIT);
  return s;
}

function count(n) {
  const v = Math.floor(Number(n));
  return Number.isFinite(v) && v > 0 ? Math.min(v, 96) : 1;
}

function emit(s, actor, kind, text) {
  s.events.push({ step: s.step, actor, kind, text });
}

const plural = (n, word, many = word + 's') => `${n} ${n === 1 ? word : many}`;

// ---------------------------------------------------------------- workload

// Per-action tallies, so one action yields a few explanatory sentences rather
// than one per page.
function newActionLog() {
  return {
    direct: newReclaimStats(), directEntries: 0, waitedWriteback: 0,
    refaults: { file: 0, anon: 0, activated: 0 }, touched: 0, oom: false, shortBy: 0,
  };
}

function newReclaimStats() {
  return { scanned: 0, dropped: 0, swapped: 0, writebackStarted: 0, dirtySkipped: 0, activated: 0, deactivated: 0, rotated: 0, anonPinned: false, balance: null };
}

// Changes that free nothing now but can let the next pass free something: a
// second chance spent, a page moved between lists, writeback started.
function listProgress(st) {
  return st.activated + st.deactivated + st.rotated + st.writebackStarted;
}

function addFilePages(s, act, n, dirty) {
  emit(s, 'task', dirty ? 'write' : 'read', dirty
    ? `The task writes ${plural(n, 'new page')} of file data. They go into the page cache as dirty pages: the disk does not have this data yet.`
    : `The task reads ${plural(n, 'page')} of a file. The data stays in the page cache as clean pages on the inactive file list, because keeping it costs nothing while memory is free.`);
  let made = 0;
  for (; made < n; made++) {
    const frame = allocateFrame(s, act);
    if (frame < 0) break;
    const id = `F${s.nextId.file++}`;
    s.pages[id] = { id, type: 'file', where: 'ram', list: 'inactive', referenced: true, dirty, writeback: false, frame, slot: null, shadow: null };
    s.frames[frame] = id;
    s.lru.file.inactive.push(id);
    s.owned.file.push(id);
  }
  act.shortBy += n - made;
}

function addAnonPages(s, act, n) {
  emit(s, 'task', 'alloc', `The task allocates and writes ${plural(n, 'page')} of anonymous memory (heap or stack). No file holds a copy, so the only place it could ever go is swap.`);
  let made = 0;
  for (; made < n; made++) {
    const frame = allocateFrame(s, act);
    if (frame < 0) break;
    const id = `A${s.nextId.anon++}`;
    // Written on allocation, so its page-table entry is marked accessed.
    s.pages[id] = { id, type: 'anon', where: 'ram', list: 'inactive', referenced: true, dirty: false, writeback: false, frame, slot: null, shadow: null };
    s.frames[frame] = id;
    s.lru.anon.inactive.push(id);
    s.owned.anon.push(id);
  }
  act.shortBy += n - made;
}

// Accesses the oldest `n` pages the workload owns of one kind, the way a
// program loops over its working set.
function touch(s, act, kind, n) {
  const ids = s.owned[kind].slice(0, n);
  emit(s, 'task', 'touch', ids.length === 0
    ? `The task has no ${kind === 'anon' ? 'anonymous' : 'file'} pages to touch yet.`
    : `The task accesses its oldest ${plural(ids.length, kind === 'anon' ? 'anonymous page' : 'file page')} again. Resident pages are marked referenced; a page referenced a second time while inactive moves to the active list.`);
  for (const id of ids) {
    const p = s.pages[id];
    if (p.where === 'ram') {
      markAccessed(s, p);
      continue;
    }
    if (!refault(s, act, p)) {
      act.shortBy += 1;
      break;
    }
  }
  act.touched = ids.length;
}

// The two-touch rule of mark_page_accessed(): inactive and unreferenced ->
// referenced; inactive and referenced -> active.
function markAccessed(s, p) {
  if (p.list === 'inactive' && p.referenced) {
    moveTo(s, p, 'active');
    p.referenced = false;
    s.age++;
  } else {
    p.referenced = true;
  }
}

// Brings an evicted file page or a swapped anonymous page back: a major fault.
// Returns false when no frame could be found.
function refault(s, act, p) {
  const frame = allocateFrame(s, act);
  if (frame < 0) return false;
  s.counters.pgmajfault++;
  if (p.where === 'swap') {
    s.swap[p.slot] = null;
    p.slot = null;
    s.counters.pswpin++;
  }
  // Workingset detection (mm/workingset.c): the shadow entry left at eviction
  // says how much the lists aged since. If the page was evicted fewer
  // evictions/activations ago than the active list is long, it would have
  // stayed resident had the active list been smaller: it belongs to the
  // working set and goes straight to the active list.
  let list = 'inactive';
  if (p.shadow !== null) {
    act.refaults[p.type]++;
    s.counters[p.type === 'anon' ? 'workingset_refault_anon' : 'workingset_refault_file']++;
    const distance = s.age - p.shadow;
    const activeSize = s.lru.file.active.length + (s.config.swapPresent ? s.lru.anon.active.length : 0);
    if (distance <= activeSize) {
      list = 'active';
      act.refaults.activated++;
      s.counters.workingset_activate++;
    }
  }
  Object.assign(p, { where: 'ram', frame, list, referenced: true, dirty: false, writeback: false, shadow: null });
  s.frames[frame] = p.id;
  s.lru[p.type][list].push(p.id);
  return true;
}

// ---------------------------------------------------------------- allocation

// Returns a free frame for the task, reclaiming first if needed, or -1 when
// nothing could be reclaimed (the point where the kernel would OOM).
function allocateFrame(s, act) {
  const { min, low } = s.config.watermarks;
  if (freeFrames(s) <= min) {
    // The min reserve is for the kernel's own emergencies: an ordinary
    // allocation may not take it, so the task reclaims for itself.
    act.directEntries++;
    s.counters.allocstall++;
    directReclaim(s, act);
    if (freeFrames(s) <= min) {
      act.oom = true;
      return -1;
    }
  }
  const frame = s.frames.indexOf(null);
  if (freeFrames(s) - 1 < low && s.kswapd === 'asleep') {
    s.kswapd = 'awake';
    emit(s, 'kswapd', 'kswapd-wake', `Free frames fell below the low watermark (${low}), so the allocator woke kswapd to reclaim in the background until free reaches the high watermark (${s.config.watermarks.high}).`);
  }
  return frame;
}

function directReclaim(s, act) {
  for (let pass = 0; pass < MAX_PASSES && freeFrames(s) <= s.config.watermarks.min; pass++) {
    const before = listProgress(act.direct);
    if (reclaimPass(s, 'direct', act.direct) > 0 || listProgress(act.direct) > before) continue;
    // A real task in direct reclaim is throttled until writeback it is
    // waiting on completes; model that by completing it.
    const waiting = Object.values(s.pages).filter((p) => p.writeback);
    if (waiting.length === 0) return;
    for (const p of waiting) Object.assign(p, { dirty: false, writeback: false });
    act.waitedWriteback += waiting.length;
  }
}

function runKswapd(s) {
  const { high } = s.config.watermarks;
  const stats = newReclaimStats();
  let passes = 0;
  while (freeFrames(s) < high && passes < MAX_PASSES) {
    passes++;
    const before = listProgress(stats);
    if (reclaimPass(s, 'kswapd', stats) === 0 && listProgress(stats) === before) break;
  }
  const free = freeFrames(s);
  s.kswapd = 'asleep';
  describeReclaim(s, 'kswapd', stats);
  if (free >= high) {
    emit(s, 'kswapd', 'kswapd-sleep', `Free frames reached ${free}, at or above the high watermark (${high}), so kswapd went back to sleep.`);
  } else {
    emit(s, 'kswapd', 'kswapd-stuck', `kswapd stopped with only ${free} free frames, below the high watermark (${high}): ${stats.dirtySkipped ? 'what remains reclaimable is dirty and waiting for writeback' : 'nothing on the lists can be reclaimed right now'}. It sleeps until the next allocation below low wakes it.`);
  }
}

// ---------------------------------------------------------------- reclaim

/**
 * How a reclaim pass divides its scanning between anonymous and file pages,
 * following get_scan_count(): no swap means file only; swappiness 0 means
 * file only unless file pages are almost gone; otherwise in proportion to
 * swappiness : (200 − swappiness).
 */
export function scanBalance(state) {
  const swapFree = state.swap.some((x) => x === null);
  if (!state.config.swapPresent) return { anon: 0, file: 1, reason: 'no-swap' };
  if (!swapFree) return { anon: 0, file: 1, reason: 'swap-full' };
  const fileResident = state.lru.file.active.length + state.lru.file.inactive.length;
  if (fileResident + freeFrames(state) <= state.config.watermarks.high) return { anon: 1, file: 0, reason: 'file-tiny' };
  if (state.config.swappiness === 0) return { anon: 0, file: 1, reason: 'swappiness-0' };
  return { anon: state.config.swappiness, file: 200 - state.config.swappiness, reason: 'proportional' };
}

// One pass: scan up to BATCH pages split by scanBalance. Like the kernel,
// a pass scans both types in proportion even when the first one alone would
// have freed enough; that is what makes swappiness bite. Returns frames freed.
function reclaimPass(s, who, stats) {
  const bal = scanBalance(s);
  stats.balance = bal.reason;
  const anonResident = s.lru.anon.active.length + s.lru.anon.inactive.length;
  if ((bal.reason === 'no-swap' || bal.reason === 'swap-full') && anonResident > 0) stats.anonPinned = true;
  const anonShare = Math.round((BATCH * bal.anon) / (bal.anon + bal.file));
  const budget = { anon: anonShare, file: BATCH - anonShare };
  const before = freeFrames(s);
  // Scan the type that gets more of the budget first.
  const order = budget.file >= budget.anon ? ['file', 'anon'] : ['anon', 'file'];
  let leftover = 0;
  for (const type of order) {
    const allowed = type === 'file' || bal.anon > 0;
    const quota = budget[type] + (allowed ? leftover : 0);
    const used = allowed ? shrinkList(s, type, quota, who, stats) : 0;
    leftover = quota - used;
  }
  return freeFrames(s) - before;
}

// Scans up to `quota` pages from the tail of one type's inactive list,
// refilling it from the active list when it is the shorter of the two.
// Returns the number of scan slots used.
function shrinkList(s, type, quota, who, stats) {
  const lists = s.lru[type];
  let used = 0;
  while (used < quota) {
    if (lists.inactive.length <= lists.active.length && lists.active.length > 0) {
      deactivate(s, type, stats);
    }
    if (lists.inactive.length === 0) break;
    used++;
    s.counters[`pgscan_${who}`]++;
    stats.scanned++;
    const p = s.pages[lists.inactive[0]];
    if (type === 'anon' && p.referenced) {
      // Its page-table entry was accessed since the last scan: a mapped
      // page in use gets activated instead of reclaimed.
      p.referenced = false;
      moveTo(s, p, 'active');
      s.age++;
      stats.activated++;
      continue;
    }
    if (type === 'file' && p.dirty) {
      // Cannot drop data the disk does not have. Queue writeback and move it
      // to the head so the scan carries on with other pages.
      if (!p.writeback) stats.writebackStarted++;
      p.writeback = true;
      stats.dirtySkipped++;
      rotate(s, p);
      continue;
    }
    if (type === 'anon') {
      const slot = s.swap.indexOf(null);
      if (slot < 0) break; // swap filled up during this pass
      s.swap[slot] = p.id;
      Object.assign(p, { where: 'swap', slot });
      s.counters.pswpout++;
      stats.swapped++;
    } else {
      Object.assign(p, { where: 'disk' });
      stats.dropped++;
    }
    evict(s, p);
    s.counters[`pgsteal_${who}`]++;
  }
  return used;
}

// Moves the oldest active page to the inactive list. A page referenced while
// active gets one more trip around the active list instead.
function deactivate(s, type, stats) {
  const p = s.pages[s.lru[type].active[0]];
  if (p.referenced) {
    p.referenced = false;
    rotate(s, p);
    stats.rotated++;
    return;
  }
  moveTo(s, p, 'inactive');
  stats.deactivated++;
}

function evict(s, p) {
  removeFromList(s, p);
  s.frames[p.frame] = null;
  p.frame = null;
  p.list = null;
  p.referenced = false;
  p.shadow = s.age++;
}

function removeFromList(s, p) {
  const list = s.lru[p.type][p.list];
  list.splice(list.indexOf(p.id), 1);
}

function moveTo(s, p, list) {
  removeFromList(s, p);
  p.list = list;
  s.lru[p.type][list].push(p.id);
}

function rotate(s, p) {
  moveTo(s, p, p.list);
}

// ---------------------------------------------------------------- time and settings

function flush(s) {
  const dirty = Object.values(s.pages).filter((p) => p.where === 'ram' && p.dirty);
  for (const p of dirty) Object.assign(p, { dirty: false, writeback: false });
  emit(s, 'flusher', 'writeback-done', dirty.length
    ? `Time passed and the flusher threads wrote ${plural(dirty.length, 'dirty page')} back to disk. They are now clean: reclaim can drop them without any further I/O.`
    : 'Time passed. No file pages were dirty, so there was nothing to write back.');
}

function setSwap(s, present) {
  if (present === s.config.swapPresent) return;
  if (present) {
    s.config.swapPresent = true;
    emit(s, 'admin', 'swapon', 'Swap was enabled (swapon). Anonymous pages now have somewhere to go, so reclaim may scan them.');
    return;
  }
  // swapoff reads every swapped page back in before the area goes away, and
  // only file pages may be reclaimed to make room. Try it on a copy so a
  // failure leaves the machine exactly as it was, as the real swapoff does.
  const trial = structuredClone(s);
  trial.config.swapPresent = false;
  const swapped = Object.values(trial.pages).filter((p) => p.where === 'swap');
  const act = newActionLog();
  for (const p of swapped) {
    p.shadow = null; // brought in by swapoff, not by a fault: not a refault
    if (!refault(trial, act, p)) {
      emit(s, 'admin', 'swapoff-refused', `swapoff refused: the ${plural(swapped.length, 'page')} in swap would all have to come back into RAM, and this toy cannot make room for them. A real swapoff would keep reclaiming, slowly, to bring them back, and would fail with ENOMEM only if that proved impossible. Swap stays on.`);
      return;
    }
  }
  Object.assign(s, trial);
  finishAction(s, act);
  emit(s, 'admin', 'swapoff', `Swap was disabled (swapoff): ${plural(swapped.length, 'page')} came back from swap first. From now on anonymous memory cannot be reclaimed at all.`);
}

function setSwappiness(s, value) {
  const v = clampSwappiness(value);
  s.config.swappiness = v;
  const how = v === 0
    ? 'reclaim now leaves anonymous pages alone unless file pages are nearly exhausted'
    : `when both kinds are reclaimable, scanning is split ${v} : ${200 - v} between anonymous and file pages`;
  emit(s, 'admin', 'swappiness', `vm.swappiness set to ${v}: ${how}. It is a relative I/O cost, not a threshold of free memory.`);
}

// ---------------------------------------------------------------- explanations

function finishAction(s, act) {
  if (act.directEntries > 0) {
    const waited = act.waitedWriteback ? ` It also had to wait for ${plural(act.waitedWriteback, 'dirty page')} to finish writeback before they could be freed.` : '';
    emit(s, 'direct', 'direct-reclaim', `Free frames were at the min watermark (${s.config.watermarks.min}), which ordinary allocations may not dip into, so the allocating task reclaimed memory itself ${plural(act.directEntries, 'time')} and stalled meanwhile (direct reclaim, counted in allocstall).${waited}`);
    describeReclaim(s, 'direct', act.direct);
  }
  const { file, anon, activated } = act.refaults;
  if (file + anon > 0) {
    const verdict = activated
      ? `${activated} had been evicted fewer evictions ago than the active list is long, so the kernel judged them part of the working set and put them straight on the active list.`
      : 'None passed the workingset test (evicted fewer evictions ago than the active list is long), so they return to the inactive list.';
    emit(s, 'task', 'refault', `${plural(file + anon, 'page')} the task needed had been evicted earlier (${file} file, ${anon} from swap): each one is a major fault and a refault. ${verdict}`);
    if (file + anon >= act.touched / 2) {
      emit(s, 'task', 'thrashing', `The task keeps needing pages that were evicted moments ago: its working set no longer fits in the memory left over, so every pass evicts what the next pass needs. This is thrashing; the machine spends its time on I/O instead of work, though memory is not "full".`);
    }
  }
  if (act.oom) {
    s.counters.oom_would_kill++;
    emit(s, 'oom', 'oom', `Direct reclaim found nothing left to reclaim and free frames are still at the min watermark: the kernel would now invoke the OOM killer to end a process. Simulated: nothing is killed here; the remaining ${plural(act.shortBy, 'page')} of this request were not allocated.`);
  }
}

function describeReclaim(s, who, st) {
  const actor = who === 'kswapd' ? 'kswapd' : 'Direct reclaim';
  if (st.scanned === 0 && st.deactivated === 0) return;
  const parts = [];
  if (st.dropped) parts.push(`dropped ${plural(st.dropped, 'clean file page')} (no I/O needed: the file on disk still has the data)`);
  if (st.swapped) parts.push(`wrote ${plural(st.swapped, 'anonymous page')} to swap (their only copy, so each costs a write)`);
  if (parts.length) emit(s, who, 'reclaimed', `${actor} scanned ${plural(st.scanned, 'page')} and ${parts.join(' and ')}.`);
  if (st.dirtySkipped) emit(s, who, 'writeback-needed', `${actor} found dirty file pages on the inactive list; they must be written back before their frames can be reused, so ${st.writebackStarted ? `writeback of ${plural(st.writebackStarted, 'page')} was started and ` : ''}they were skipped for now.`);
  if (st.activated) emit(s, who, 'activated', `${actor} found ${plural(st.activated, 'anonymous page')} accessed since the last scan and moved them to the active list instead of reclaiming them (second chance).`);
  if (st.anonPinned) {
    emit(s, who, 'anon-pinned', st.balance === 'no-swap'
      ? `${actor} did not scan anonymous pages at all: without swap there is nowhere to put them, so all pressure falls on the page cache.`
      : `${actor} did not scan anonymous pages: swap is full.`);
  }
}

// ---------------------------------------------------------------- scenarios

/**
 * Scripted scenarios. `setup` fields left undefined keep the seeded value.
 * Each step has a `note` the view shows before running it, so the learner
 * can predict what will happen. `compare` names a scenario run side by side.
 */
export const SCENARIOS = Object.freeze([
  {
    id: 'fill-cache-then-allocate',
    title: 'Fill the cache, then allocate',
    question: 'Memory looks full of cache. What happens when a program then needs memory?',
    setup: {},
    steps: [
      { action: { type: 'read', pages: 24 }, note: 'Read a file: the page cache grows into free memory.' },
      { action: { type: 'read', pages: 24 }, note: 'Read more: still no pressure, just fewer free frames.' },
      { action: { type: 'read', pages: 24 }, note: 'Most of memory is now cache. Nothing has been reclaimed yet.' },
      { action: { type: 'alloc', pages: 8 }, note: 'The program allocates: free frames shrink towards the low watermark.' },
      { action: { type: 'alloc', pages: 8 }, note: 'Another allocation takes free frames below low: watch kswapd wake and which pages it gives up.' },
      { action: { type: 'alloc', pages: 8 }, note: 'The program keeps growing at a pace kswapd can keep up with.' },
      { action: { type: 'alloc', pages: 8 }, note: 'And once more.' },
    ],
    expect: 'Each time free frames fall below low, kswapd wakes and, in the background, drops clean file pages until free is back at high: cache gives way to the program at no I/O cost, and the program never has to reclaim for itself (no direct reclaim). At a very high swappiness kswapd would rather swap idle anonymous pages out.',
  },
  {
    id: 'no-swap',
    title: 'No swap',
    question: 'Without swap, what can the kernel take when a program keeps growing?',
    setup: { swapPresent: false },
    steps: [
      { action: { type: 'read', pages: 24 }, note: 'Fill some cache.' },
      { action: { type: 'read', pages: 16 }, note: 'More cache.' },
      { action: { type: 'alloc', pages: 24 }, note: 'The program grows.' },
      { action: { type: 'alloc', pages: 24 }, note: 'Only file pages can be reclaimed.' },
      { action: { type: 'alloc', pages: 24 }, note: 'The cache is nearly gone.' },
      { action: { type: 'alloc', pages: 16 }, note: 'Anonymous memory fills the machine.' },
      { action: { type: 'alloc', pages: 8 }, note: 'One more allocation, with nothing left to reclaim.' },
    ],
    expect: 'Anonymous pages are never scanned; the cache shrinks to nothing; then the kernel would invoke the OOM killer.',
  },
  {
    id: 'swappiness-0',
    title: 'Swappiness 0',
    question: 'With swap available, does swappiness 0 mean "never swap"?',
    setup: { swapPresent: true, swappiness: 0 },
    steps: swappinessSteps(),
    expect: 'Reclaim takes only file pages while there are enough of them; nothing is written to swap.',
    compare: 'swappiness-100',
  },
  {
    id: 'swappiness-100',
    title: 'Swappiness 100',
    question: 'With swappiness 100, how is the work split between cache and anonymous memory?',
    setup: { swapPresent: true, swappiness: 100 },
    steps: swappinessSteps(),
    expect: 'Anonymous and file pages are scanned equally: idle anonymous pages go to swap and come back as major faults when touched.',
    compare: 'swappiness-0',
  },
  {
    id: 'thrash',
    title: 'Thrash',
    question: 'Can a machine without swap thrash before it runs out of memory?',
    setup: { swapPresent: false },
    steps: [
      { action: { type: 'read', pages: 30 }, note: 'The program reads its data files: 30 pages it will keep using.' },
      { action: { type: 'alloc', pages: 24 }, note: 'It allocates working memory.' },
      { action: { type: 'alloc', pages: 24 }, note: 'More working memory: it cannot be reclaimed without swap.' },
      { action: { type: 'alloc', pages: 18 }, note: 'Now 66 of 96 frames are anonymous; far fewer than 30 remain for the files.' },
      { action: { type: 'touch', kind: 'file', pages: 30 }, note: 'The program loops over its data files again.' },
      { action: { type: 'touch', kind: 'file', pages: 30 }, note: 'And again.' },
      { action: { type: 'touch', kind: 'file', pages: 30 }, note: 'And again: every loop rereads pages evicted moments before.' },
    ],
    expect: 'Every loop refaults most of the file pages: memory is not "full", yet the machine spends its time rereading the same data.',
  },
]);

function swappinessSteps() {
  return [
    { action: { type: 'alloc', pages: 24 }, note: 'The program allocates memory it will use only at start-up.' },
    { action: { type: 'alloc', pages: 16 }, note: 'More of it.' },
    { action: { type: 'read', pages: 24 }, note: 'Then it streams through files.' },
    { action: { type: 'read', pages: 24 }, note: 'More file data: memory is short now.' },
    { action: { type: 'read', pages: 24 }, note: 'More still.' },
    { action: { type: 'read', pages: 24 }, note: 'The stream goes on.' },
    { action: { type: 'touch', kind: 'anon', pages: 40 }, note: 'Finally it touches its start-up memory again.' },
  ];
}

/** Returns the scenario with this id, or undefined. */
export function scenarioById(id) {
  return SCENARIOS.find((sc) => sc.id === id);
}

/** The initial state of a scenario, given the seeded options. */
export function scenarioStart(scenario, seed = {}) {
  return initialState({ ...seed, ...scenario.setup });
}

/** Runs a whole scenario and returns its final state. */
export function runScenario(scenario, seed = {}) {
  return scenario.steps.reduce((st, s) => step(st, s.action), scenarioStart(scenario, seed));
}
