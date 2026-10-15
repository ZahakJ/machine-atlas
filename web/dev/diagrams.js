// Mounts every diagram against a fixture snapshot from data/ (generated from
// fixtures/roots/<name>; see data/*.json "note"). Query: ?fixture=<name>&only=<diagram>.

import { el } from '../js/lib/dom.js';
import { diagrams } from '../js/diagrams/index.js';
import { setupThemeButtons } from './dev.js';

setupThemeButtons();

const params = new URLSearchParams(location.search);
const fixtureSelect = document.getElementById('fixture');
const onlySelect = document.getElementById('only');
const host = document.getElementById('diagrams');
for (const [name, d] of Object.entries(diagrams)) onlySelect.append(el('option', { value: name }, `${name}: ${d.title}`));
fixtureSelect.value = params.get('fixture') ?? 'desktop-x86_64';
onlySelect.value = params.get('only') ?? '';

let unmounts = [];

async function load(name) {
  if (name === 'empty') return { observations: [], derived: [] };
  const res = await fetch(`data/${encodeURIComponent(name)}.json`);
  if (!res.ok) throw new Error(`data/${name}.json: ${res.status}`);
  return res.json();
}

function ctxFrom(data) {
  const o = new Map(data.observations.map((x) => [x.id, x]));
  const d = new Map(data.derived.map((x) => [x.id, x]));
  return {
    obs: (id) => o.get(id),
    derived: (id) => d.get(id),
    navigate: (hash) => console.info('navigate', hash),
  };
}

async function render() {
  for (const u of unmounts.splice(0)) u();
  host.replaceChildren();
  const ctx = ctxFrom(await load(fixtureSelect.value));
  for (const [name, d] of Object.entries(diagrams)) {
    if (onlySelect.value && onlySelect.value !== name) continue;
    const slot = el('div', { class: 'diagram-host' });
    host.append(el('figure', { class: 'diagram-slot', 'data-diagram': name, id: name },
      el('figcaption', { class: 'diagram-slot__title' }, d.title), slot));
    unmounts.push(d.mount(slot, ctx));
  }
}

for (const s of [fixtureSelect, onlySelect]) {
  s.addEventListener('change', () => {
    params.set('fixture', fixtureSelect.value);
    if (onlySelect.value) params.set('only', onlySelect.value); else params.delete('only');
    history.replaceState(null, '', `?${params}`);
    render();
  });
}

render();
