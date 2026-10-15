import { test } from 'node:test';
import assert from 'node:assert/strict';

globalThis.document = { createElement: (t) => ({ t, appendChild() {}, setAttribute() {} }), createTextNode: (s) => s };
const { idText } = await import('../js/render/common.js');

const show = (id) => idText(id).map((x) => (typeof x === 'string' ? x : '|')).join('');

test('idText: ids wrap after dots, and long parts after underscores, never mid-word', () => {
  assert.equal(show('cpu.address_bits.physical'), 'cpu.|address_bits.|physical');
  assert.equal(show('mem.vmstat.compact_daemon_migrate_scanned'), 'mem.|vmstat.|compact_|daemon_|migrate_|scanned');
  assert.equal(show('mem'), 'mem');
});
