// Values, rows and filters for Inspect and the inspector. Pure functions, no
// DOM, so they are tested under node.
//
// Lesson text arrives formatted by Go; Inspect lists every observation and
// derived fact, which the API sends raw, so this module formats them with the
// same conventions (IEC bytes, thousands separators) and always offers the
// exact value beside the short one.

import { formatBytes, formatCount } from '../lib/format.js';
import { statusText } from '../lib/ui.js';

/** True when value is a table: an array of flat objects. */
export function isTable(value) {
  return Array.isArray(value);
}

/**
 * The short form of a value for a table cell or a heading: "15.5 GiB",
 * "2,049 pages", "yes", "table · 4 rows". A value whose status is not ok
 * reads as its status ("permission denied"), never as a number.
 */
export function shortValue(item) {
  if (!item) return '—';
  if (item.status !== 'ok') return item.status === 'unknown' ? 'unknown' : statusText(item.status);
  const v = item.value;
  if (v === null || v === undefined) return '—';
  if (isTable(v)) return `table · ${formatCount(v.length)} row${v.length === 1 ? '' : 's'}`;
  if (typeof v === 'boolean') return v ? 'yes' : 'no';
  if (typeof v === 'number') return withUnit(v, unitOf(item), false);
  const s = String(v);
  return s.length > 80 ? s.slice(0, 79) + '…' : s;
}

/**
 * The exact form: every digit, with the unit spelled out
 * ("16,642,486,272 bytes"), or null when the short form is already exact.
 */
export function exactValue(item) {
  if (!item || item.status !== 'ok' || typeof item.value !== 'number') return null;
  const exact = withUnit(item.value, unitOf(item), true);
  return exact === shortValue(item) ? null : exact;
}

// A derived "…_fraction" fact carries no unit in the snapshot; it is a
// ratio from 0 to 1 all the same, and reads as a percentage.
function unitOf(item) {
  if (item.unit) return item.unit;
  return typeof item.id === 'string' && item.id.endsWith('_fraction') ? 'fraction' : item.unit;
}

function withUnit(n, unit, exact) {
  switch (unit) {
    case 'bytes':
      return exact ? `${formatCount(n)} bytes` : formatBytes(n);
    case 'pages':
      return `${formatCount(n)} page${n === 1 ? '' : 's'}`;
    case 'bits':
      return `${formatCount(n)} bit${n === 1 ? '' : 's'}`;
    case 'percent':
      return `${formatNumber(n)} %`;
    case 'seconds':
      return `${formatNumber(n)} s`;
    case 'microseconds':
      return `${formatCount(n)} µs`;
    case 'fraction':
      // Three significant digits and no space, as lesson text has it (4.92%).
      return exact ? String(n) : `${Number((n * 100).toPrecision(3))}%`;
    case 'count':
    case '':
    case undefined:
      return formatNumber(n);
    default:
      return `${formatNumber(n)} ${unit}`;
  }
}

function formatNumber(n) {
  return Number.isInteger(n) ? formatCount(n) : String(Number(n.toPrecision(6)));
}

/** A table cell as text: numbers with separators, booleans as yes/no. */
export function cellText(v) {
  if (v === null || v === undefined) return '';
  if (typeof v === 'boolean') return v ? 'yes' : 'no';
  if (typeof v === 'number') return Number.isInteger(v) ? formatCount(v) : String(v);
  return String(v);
}

/**
 * True when a formatted table cell is a number, possibly with a short unit:
 * "3 670 016" (grouped by thin or narrow no-break spaces, or commas),
 * "7.19 GiB", "−12", "99.5 %". Empty cells count, so a sparse numeric column
 * stays right-aligned.
 */
export function isNumericCell(s) {
  if (s === '' || s === null || s === undefined) return true;
  return /^[−-]?\d{1,3}([,\u2009\u202f ]?\d{3})*(\.\d+)?( ?[\u2009\u202f]?[A-Za-zµ%]{1,4})?$/.test(String(s));
}

const COLUMN_TITLES = {
  nr: 'Pages in pool',
  cpulist: 'CPUs',
  shared_cpu_list: 'Shared by CPUs',
  mem_total_bytes: 'Total',
  mem_free_bytes: 'Free',
  disksize_bytes: 'Disk size',
  fstype: 'Filesystem',
  operstate: 'State',
  pages_total: 'Pages',
  pages_present: 'Present',
  pages_swapped: 'Swapped',
};

/**
 * A readable header for a table column key: "size_bytes" → "Size",
 * "order3" → "Order 3", "nr" → "Pages in pool". The raw key stays available
 * to the caller (as a title attribute); units are already in the cells.
 */
export function columnTitle(key) {
  const k = String(key ?? '');
  if (COLUMN_TITLES[k]) return COLUMN_TITLES[k];
  const order = /^order(\d+)$/.exec(k);
  if (order) return `Order ${order[1]}`;
  const words = k.replace(/_(bytes|kb|kib)$/, '').replace(/_/g, ' ').trim();
  return words ? words[0].toUpperCase() + words.slice(1) : k;
}

/** The columns of a table value, in first-seen order across all rows. */
export function tableColumns(rows) {
  const seen = new Set();
  for (const row of rows) for (const k of Object.keys(row ?? {})) seen.add(k);
  return [...seen];
}

/**
 * One row of Inspect, merging an observation or a derived fact with its
 * catalog entry. type is "obs" | "derived". kind is "static", "live" or
 * "derived" (derived facts are not readings). origin is the collector for an
 * observation and the basis (arithmetic | inference) for a fact.
 */
export function toRow(item, type, entry) {
  const id = item.id;
  return {
    id,
    type,
    item,
    status: item.status,
    kind: type === 'derived' ? 'derived' : item.kind,
    origin: type === 'derived' ? item.basis : item.collector,
    namespace: id.split('.')[0],
    title: entry?.title ?? '',
    value: shortValue(item),
    source: type === 'derived' ? item.rule ?? '' : [item.source?.path, item.source?.field].filter(Boolean).join(' '),
  };
}

/**
 * Builds every row, observations first, each list in id order. catalogEntry
 * is a function id → entry or undefined.
 */
export function buildRows(observations, facts, catalogEntry = () => undefined) {
  const byId = (a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  return [
    ...[...observations].sort(byId).map((o) => toRow(o, 'obs', catalogEntry(o.id))),
    ...[...facts].sort(byId).map((f) => toRow(f, 'derived', catalogEntry(f.id))),
  ];
}

/**
 * Filters for Inspect. Every field is optional; "" means any.
 *   q         words that must all appear in the id, title, value or source
 *   status    one status ("unknown" for derived facts)
 *   kind      static | live | derived
 *   origin    a collector id or a basis
 *   ns        the first segment of the id (mem, cpu, derived, …)
 */
export function matches(row, f = {}) {
  if (f.status && row.status !== f.status) return false;
  if (f.kind && row.kind !== f.kind) return false;
  if (f.origin && row.origin !== f.origin) return false;
  if (f.ns && row.namespace !== f.ns) return false;
  const words = String(f.q ?? '').toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const hay = `${row.id} ${row.title} ${row.value} ${row.source}`.toLowerCase();
  return words.every((w) => hay.includes(w));
}

/** The distinct values of a row field with their counts, sorted by value. */
export function facet(rows, field) {
  const counts = new Map();
  for (const r of rows) {
    const v = r[field];
    if (v === undefined || v === null || v === '') continue;
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  return [...counts].sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0)).map(([value, count]) => ({ value, count }));
}

/** Only the filter fields Inspect understands, from route params. */
export function filtersFrom(params = {}) {
  const out = {};
  for (const k of ['q', 'status', 'kind', 'origin', 'ns']) {
    if (typeof params[k] === 'string' && params[k] !== '') out[k] = params[k];
  }
  return out;
}

/**
 * Finds the catalog entry for an id: the exact entry, else the longest
 * wildcard family ("mem.vmstat.*") that covers it.
 */
export function catalogLookup(entries) {
  const exact = new Map();
  const families = [];
  for (const e of entries ?? []) {
    if (typeof e?.id !== 'string') continue;
    if (e.id.endsWith('.*')) families.push([e.id.slice(0, -1), e]);
    else exact.set(e.id, e);
  }
  families.sort((a, b) => b[0].length - a[0].length);
  return (id) => exact.get(id) ?? families.find(([prefix]) => id.startsWith(prefix))?.[1];
}
