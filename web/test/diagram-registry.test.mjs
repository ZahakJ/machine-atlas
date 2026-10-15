import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { diagrams } from '../js/diagrams/index.js';
import { LAYERS, lessonHref } from '../js/diagrams/memory-layers.js';

test('the registry has the ten diagrams of ARCHITECTURE.md §9, each mountable', () => {
  assert.deepEqual(Object.keys(diagrams).sort(), [
    'address-space-map', 'cpu-topology', 'fault-flow', 'io-stack', 'lru-lists', 'meminfo-breakdown', 'memory-layers', 'packet-path', 'probe-regions', 'zone-watermarks',
  ]);
  for (const [name, d] of Object.entries(diagrams)) {
    assert.equal(typeof d.title, 'string', name);
    assert.ok(d.title.length > 0, name);
    assert.equal(typeof d.mount, 'function', name);
  }
});

test('memory-layers links only to lessons and subsystems that exist', () => {
  const roadmap = JSON.parse(readFileSync(new URL('../../curriculum/roadmap.json', import.meta.url)));
  const subsystems = new Set(roadmap.subsystems.map((s) => s.id));
  for (const layer of LAYERS) {
    for (const id of layer.lessons) {
      assert.doesNotThrow(() => readFileSync(new URL(`../../curriculum/lessons/${id}.md`, import.meta.url)), id);
      assert.equal(lessonHref(id), `#/learn/${id}`);
    }
    for (const id of layer.subsystems) assert.ok(subsystems.has(id), id);
  }
});
