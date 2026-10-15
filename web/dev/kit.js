// Component kit behaviour: copies the light specimen into the dark section
// (so both themes always show the same markup) and wires the dialog and
// drawer demonstrations.

const light = document.getElementById('specimen');
const dark = light.cloneNode(true);
dark.id = 'specimen-dark';
// Keep ids, label targets and radio groups unique in the copy.
for (const node of dark.querySelectorAll('[id]')) node.id += '-dark';
for (const node of dark.querySelectorAll('[for]')) node.setAttribute('for', node.getAttribute('for') + '-dark');
for (const node of dark.querySelectorAll('[aria-labelledby]')) node.setAttribute('aria-labelledby', node.getAttribute('aria-labelledby') + '-dark');
for (const node of dark.querySelectorAll('input[name]')) node.name += '-dark';
document.getElementById('dark').appendChild(dark);

for (const button of document.querySelectorAll('[data-open-dialog]')) {
  button.addEventListener('click', () => button.closest('section').querySelector('dialog').showModal());
}

const drawer = document.getElementById('kit-drawer');
let opener = null;
function setDrawer(open) {
  drawer.dataset.open = String(open);
  if (open) drawer.querySelector('[data-close-drawer]').focus();
  else opener?.focus();
}
for (const button of document.querySelectorAll('[data-open-drawer]')) {
  button.addEventListener('click', () => { opener = button; setDrawer(true); });
}
drawer.querySelector('[data-close-drawer]').addEventListener('click', () => setDrawer(false));
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && drawer.dataset.open === 'true') setDrawer(false);
});
