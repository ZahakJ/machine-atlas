// Value formatting for the browser. Pure functions, no DOM.
//
// Lesson text arrives already formatted by Go (curriculum.Format); these
// helpers format values the browser computes itself (simulations, inspector
// extras) with the same conventions: IEC units, at most three significant
// digits, trailing zeros dropped, digits grouped by narrow no-break spaces.

const IEC = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];

/**
 * Bytes in IEC units with at most three significant digits: 4096 → "4 KiB",
 * 1536 → "1.5 KiB", 123456789 → "118 MiB". Values from 1000 to 1023 of a unit
 * keep four integer digits ("1020 KiB") rather than switching to exponent
 * notation. Accepts numbers and BigInts; exactness beyond three digits is the
 * inspector's job, not this one's.
 */
export function formatBytes(value) {
  let n = typeof value === 'bigint' ? Number(value) : value;
  if (typeof n !== 'number' || !Number.isFinite(n)) return '—';
  const sign = n < 0 ? '−' : '';
  n = Math.abs(n);
  let unit = 0;
  while (n >= 1024 && unit < IEC.length - 1) {
    n /= 1024;
    unit++;
  }
  if (unit === 0) return `${sign}${Math.round(n)} B`;
  let text = significant(n);
  // Rounding can carry into the next unit: 1023.9 KiB is "1 MiB", not "1024 KiB".
  if (Number(text) >= 1024 && unit < IEC.length - 1) {
    unit++;
    text = significant(Number(text) / 1024);
  }
  return `${sign}${text} ${IEC[unit]}`;
}

function significant(n) {
  if (n >= 100) return String(Math.round(n));
  return String(Number(n.toPrecision(3)));
}

/**
 * The digit-group separator: U+202F NARROW NO-BREAK SPACE, as in lesson
 * text (curriculum.Format). It reads as a thin gap and, unlike a comma or a
 * plain thin space, never lets one number break across two lines.
 */
export const GROUP = '\u202f';

/** An integer with grouped digits: 1234567 → "1 234 567" (narrow no-break spaces). */
export function formatCount(value) {
  const big = typeof value === 'bigint' ? value : Number.isInteger(value) ? BigInt(value) : null;
  if (big === null) return typeof value === 'number' && Number.isFinite(value) ? String(value) : '—';
  const sign = big < 0n ? '−' : '';
  const digits = (big < 0n ? -big : big).toString();
  return sign + digits.replace(/\B(?=(\d{3})+(?!\d))/g, GROUP);
}

/**
 * Lower-case hexadecimal with a 0x prefix. `digits` pads with leading zeros;
 * `group` inserts an underscore every `group` digits from the right, which
 * makes 64-bit addresses readable (0x7ffd_5e8c_3a10).
 */
export function formatHex(value, { digits = 0, group = 0 } = {}) {
  const big = typeof value === 'bigint' ? value : BigInt(value);
  if (big < 0n) throw new RangeError('formatHex expects a non-negative value');
  let hex = big.toString(16).padStart(digits, '0');
  if (group > 0) hex = hex.replace(new RegExp(`\\B(?=([0-9a-f]{${group}})+(?![0-9a-f]))`, 'g'), '_');
  return '0x' + hex;
}

const AGE_UNITS = [
  ['year', 365 * 86400],
  ['month', 30 * 86400],
  ['week', 7 * 86400],
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
];

/**
 * How long ago `then` was, relative to `now`, in the largest whole unit:
 * "just now", "5 minutes ago", "3 days ago". A time in the future (a clock
 * that disagrees with the snapshot's) reads "in the future" rather than a
 * negative age. Both arguments accept a Date, an RFC 3339 string or epoch ms.
 * Returns "unknown time" for anything unparsable.
 */
export function formatAge(then, now = Date.now()) {
  const t = toMillis(then);
  const n = toMillis(now);
  if (Number.isNaN(t) || Number.isNaN(n)) return 'unknown time';
  const seconds = Math.floor((n - t) / 1000);
  if (seconds < -60) return 'in the future';
  if (seconds < 60) return 'just now';
  for (const [name, size] of AGE_UNITS) {
    const count = Math.floor(seconds / size);
    if (count >= 1) return `${count} ${name}${count === 1 ? '' : 's'} ago`;
  }
  return 'just now';
}

function toMillis(v) {
  if (v instanceof Date) return v.getTime();
  if (typeof v === 'number') return v;
  if (typeof v === 'string' && v !== '') return Date.parse(v);
  return NaN;
}
