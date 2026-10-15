// Scales, address parsing and text fitting shared by the diagrams. Pure
// functions, no DOM: the layout decisions they encode are tested with
// node --test (web/test/diagram-scale.test.mjs).

/**
 * A linear map from the domain [d0, d1] to the range [r0, r1]. A degenerate
 * domain (d0 === d1) maps everything to r0 rather than dividing by zero.
 */
export function linear(d0, d1, r0, r1) {
  const span = d1 - d0;
  return (v) => (span === 0 ? r0 : r0 + ((v - d0) / span) * (r1 - r0));
}

/**
 * A scale from 0 to `max` drawn across `width` pixels that may break once.
 * Values up to `breakAt` take the first `split` of the width, linearly; the
 * rest of the domain is squeezed into what remains after a `gap` that the
 * caller marks as a break. Used where the interesting part of a quantity is a
 * sliver of its total (zone watermarks are well under 1% of a zone).
 *
 * When the break would not help (breakAt already covers at least `split` of
 * max, or there is nothing above it), the scale is plain linear and
 * `broken` is false.
 *
 * Returns {x(v), broken, breakAt, breakX0, breakX1}.
 */
export function brokenScale({ max, breakAt, width, split = 0.7, gap = 14 }) {
  if (!(max > 0)) {
    return { x: () => 0, broken: false, breakAt: 0, breakX0: 0, breakX1: 0 };
  }
  if (!(breakAt > 0) || breakAt >= max * split) {
    const x = linear(0, max, 0, width);
    return { x: (v) => x(clamp(v, 0, max)), broken: false, breakAt: max, breakX0: width, breakX1: width };
  }
  const breakX0 = width * split;
  const breakX1 = Math.min(width, breakX0 + gap);
  const left = linear(0, breakAt, 0, breakX0);
  const right = linear(breakAt, max, breakX1, width);
  return {
    x: (v) => {
      const c = clamp(v, 0, max);
      return c <= breakAt ? left(c) : right(c);
    },
    broken: true,
    breakAt,
    breakX0,
    breakX1,
  };
}

export function clamp(v, lo, hi) {
  return Math.min(hi, Math.max(lo, v));
}

/**
 * Parses a hexadecimal address such as "0x7f3c8a400000" into a BigInt.
 * Addresses go up to 2^64, beyond what a Number holds exactly, so they stay
 * BigInts end to end. Returns null for anything that is not 0x followed by
 * hex digits.
 */
export function parseAddr(s) {
  if (typeof s !== 'string' || !/^0x[0-9a-fA-F]{1,16}$/.test(s)) return null;
  return BigInt(s);
}

/** Converts a BigInt byte count to a Number for formatting and scaling. */
export function toNumber(big) {
  return typeof big === 'bigint' ? Number(big) : big;
}

// Average advance of a character, in em. Generous, so text estimated to fit
// does fit in every system font the CSS stacks may pick.
const EM = { sans: 0.54, mono: 0.62 };

/** Estimated rendered width in pixels of `text` at `size` px. */
export function textWidth(text, size, face = 'sans') {
  return String(text).length * size * (EM[face] ?? EM.sans);
}

/**
 * Shortens `text` with a trailing ellipsis so it fits `width` pixels at
 * `size` px. Returns '' when not even one character and the ellipsis fit.
 */
export function fitText(text, width, size, face = 'sans') {
  const s = String(text);
  if (textWidth(s, size, face) <= width) return s;
  const per = size * (EM[face] ?? EM.sans);
  const n = Math.floor(width / per) - 1;
  return n >= 1 ? s.slice(0, n).trimEnd() + '…' : '';
}

/**
 * Breaks `text` into lines no wider than `width` pixels at `size` px,
 * at spaces. A single word longer than the line is shortened with an
 * ellipsis rather than overflowing its box.
 */
export function wrap(text, width, size, face = 'sans') {
  const lines = [];
  let line = '';
  for (const word of String(text).split(/\s+/).filter(Boolean)) {
    const next = line ? `${line} ${word}` : word;
    if (textWidth(next, size, face) <= width) {
      line = next;
      continue;
    }
    if (line) lines.push(line);
    line = textWidth(word, size, face) <= width ? word : fitText(word, width, size, face);
  }
  if (line) lines.push(line);
  return lines;
}

/**
 * Joins `parts` with " · " into as few lines as fit `width` pixels at
 * `size` px; a part never splits across lines (one too long on its own is
 * shortened with an ellipsis).
 */
export function packParts(parts, width, size, face = 'sans') {
  const lines = [];
  let line = '';
  for (const part of parts.filter(Boolean)) {
    const next = line ? `${line} · ${part}` : part;
    if (textWidth(next, size, face) <= width) {
      line = next;
      continue;
    }
    if (line) lines.push(line);
    line = fitText(part, width, size, face);
  }
  if (line) lines.push(line);
  return lines;
}

/**
 * Measures a box holding a bold title and wrapped body text, `width` pixels
 * wide with `pad` pixels of padding. Returns {title: [lines], body: [lines],
 * height}. Title lines are 17 px apart (13 px text), body lines 16 px (12 px
 * text).
 */
export function measureBox(title, body, width, pad = 10) {
  const inner = Math.max(20, width - 2 * pad);
  const t = title ? wrap(title, inner, 13) : [];
  const b = body ? wrap(body, inner, 12) : [];
  const height = 2 * pad + t.length * 17 + b.length * 16 + (t.length && b.length ? 3 : 0) - 3;
  return { title: t, body: b, height: Math.ceil(height) };
}

/** How many columns of at least `min` pixels (with `gap` between) fit `width`, capped at `max`. */
export function columns(width, min, gap, max) {
  return Math.max(1, Math.min(max, Math.floor((width + gap) / (min + gap))));
}
