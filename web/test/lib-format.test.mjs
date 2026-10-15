import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatBytes, formatCount, formatHex, formatAge } from '../js/lib/format.js';

test('formatBytes: IEC units, at most three significant digits', () => {
  const cases = [
    [0, '0 B'], [1, '1 B'], [1023, '1023 B'], [1024, '1 KiB'], [1536, '1.5 KiB'],
    [4096, '4 KiB'], [10752, '10.5 KiB'], [123456, '121 KiB'], [1023 * 1024, '1023 KiB'],
    [1048575, '1 MiB'], [16642486272, '15.5 GiB'], [2 ** 47, '128 TiB'],
    [2n ** 64n, '16 EiB'], [-2048, '−2 KiB'], [NaN, '—'], ['12', '—'],
  ];
  for (const [input, want] of cases) assert.equal(formatBytes(input), want, String(input));
});

test('formatCount: thousands separators', () => {
  const cases = [[0, '0'], [999, '999'], [1000, '1\u202f000'], [1234567, '1\u202f234\u202f567'], [-4096, '−4\u202f096'], [12345678901234567890n, '12\u202f345\u202f678\u202f901\u202f234\u202f567\u202f890'], [1.5, '1.5'], [null, '—']];
  for (const [input, want] of cases) assert.equal(formatCount(input), want, String(input));
});

test('formatHex: prefix, padding, grouping', () => {
  assert.equal(formatHex(255), '0xff');
  assert.equal(formatHex(0x7ffd5e8c3a10n, { group: 4 }), '0x7ffd_5e8c_3a10');
  assert.equal(formatHex(0x1000n, { digits: 16, group: 4 }), '0x0000_0000_0000_1000');
  assert.equal(formatHex(0xabcden, { group: 4 }), '0xa_bcde');
  assert.equal(formatHex(0), '0x0');
  assert.throws(() => formatHex(-1n), RangeError);
});

test('formatAge: largest whole unit', () => {
  const now = Date.parse('2026-01-15T12:00:00Z');
  const ago = (s) => formatAge(new Date(now - s * 1000), now);
  assert.equal(ago(0), 'just now');
  assert.equal(ago(59), 'just now');
  assert.equal(ago(60), '1 minute ago');
  assert.equal(ago(5 * 60), '5 minutes ago');
  assert.equal(ago(3600), '1 hour ago');
  assert.equal(ago(3 * 86400), '3 days ago');
  assert.equal(ago(14 * 86400), '2 weeks ago');
  assert.equal(ago(60 * 86400), '2 months ago');
  assert.equal(ago(400 * 86400), '1 year ago');
  assert.equal(ago(-3600), 'in the future');
  assert.equal(formatAge('2026-01-12T12:00:00Z', '2026-01-15T12:00:00Z'), '3 days ago');
  assert.equal(formatAge('not a date', now), 'unknown time');
});
