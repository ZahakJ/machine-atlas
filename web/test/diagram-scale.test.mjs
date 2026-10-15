import { test } from 'node:test';
import assert from 'node:assert/strict';
import { linear, brokenScale, parseAddr, toNumber, textWidth, fitText, wrap, packParts, measureBox, columns } from '../js/diagrams/scale.js';

test('linear maps the domain onto the range and survives a degenerate domain', () => {
  const x = linear(0, 10, 100, 200);
  assert.equal(x(0), 100);
  assert.equal(x(5), 150);
  assert.equal(x(10), 200);
  assert.equal(linear(3, 3, 7, 9)(3), 7);
});

test('brokenScale spreads the low part and compresses the rest', () => {
  const s = brokenScale({ max: 100000, breakAt: 100, width: 300 });
  assert.equal(s.broken, true);
  assert.equal(s.x(0), 0);
  assert.equal(s.x(100), s.breakX0);
  assert.equal(s.x(100000), 300);
  assert.ok(s.breakX1 > s.breakX0);
  // Monotonic across the break, and the low part gets most of the width.
  let last = -1;
  for (const v of [0, 1, 50, 99, 100, 101, 5000, 99999, 100000]) {
    assert.ok(s.x(v) >= last, `x(${v})`);
    last = s.x(v);
  }
  assert.ok(s.x(50) > 100, 'half the watermark range is drawn wide');
  // Out-of-domain values are clamped rather than drawn off the track.
  assert.equal(s.x(-5), 0);
  assert.equal(s.x(1e9), 300);
});

test('brokenScale stays linear when a break would not help', () => {
  const s = brokenScale({ max: 100, breakAt: 90, width: 200 });
  assert.equal(s.broken, false);
  assert.equal(s.x(50), 100);
  assert.equal(brokenScale({ max: 100, breakAt: 0, width: 200 }).broken, false);
  const empty = brokenScale({ max: 0, breakAt: 0, width: 200 });
  assert.equal(empty.x(10), 0);
});

test('parseAddr keeps 64-bit addresses exact and rejects anything else', () => {
  assert.equal(parseAddr('0x400000'), 0x400000n);
  assert.equal(parseAddr('0xffffffffff601000'), 0xffffffffff601000n);
  assert.equal(toNumber(parseAddr('0x10')), 16);
  for (const bad of ['400000', '0x', '0xzz', '0x1ffffffffffffffff', '', null, 42, ' 0x10']) {
    assert.equal(parseAddr(bad), null, String(bad));
  }
});

test('fitText and wrap never exceed the width they are given', () => {
  const s = 'memfd:atlas-probe (deleted) and a long tail of words';
  const fitted = fitText(s, 100, 12);
  assert.ok(fitted.endsWith('…'));
  assert.ok(textWidth(fitted, 12) <= 100);
  assert.equal(fitText('ok', 100, 12), 'ok');
  assert.equal(fitText('anything', 3, 12), '');
  for (const line of wrap(`${s} supercalifragilisticexpialidocious`, 90, 12)) assert.ok(textWidth(line, 12) <= 90, line);
  assert.deepEqual(wrap('', 90, 12), []);
});

test('packParts keeps each part whole and joins what fits', () => {
  assert.deepEqual(packParts(['a', 'b', null, 'c'], 500, 12), ['a · b · c']);
  const lines = packParts(['resident 8 MiB of 8 MiB', '2,048 of 2,048 pages present'], 200, 12);
  assert.deepEqual(lines, ['resident 8 MiB of 8 MiB', '2,048 of 2,048 pages present']);
});

test('measureBox grows with the text and columns respects its bounds', () => {
  const short = measureBox('Title', 'Body', 400);
  const long = measureBox('Title', 'Body '.repeat(60), 400);
  assert.ok(long.height > short.height);
  assert.equal(measureBox('', '', 400).title.length, 0);
  assert.equal(columns(1000, 220, 14, 3), 3);
  assert.equal(columns(300, 220, 14, 3), 1);
  assert.equal(columns(10, 220, 14, 3), 1);
});
