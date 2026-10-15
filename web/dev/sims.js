// Mounts every simulation with a chosen mock ctx (?ctx=desktop|rpi-noswap|empty).

import { MOCKS } from './mock-ctx.js';
import { setupThemeButtons } from './dev.js';
import { mount as mountWalker } from '../js/sim/translation-walker.view.js';
import { mount as mountReclaim } from '../js/sim/reclaim.view.js';
import { mount as mountFairShare } from '../js/sim/fair-share.view.js';

setupThemeButtons();

const params = new URLSearchParams(location.search);
const name = params.get('ctx') in MOCKS ? params.get('ctx') : 'desktop';
const select = document.getElementById('ctx');
select.value = name;

let unmount = [];
function mountAll(ctx) {
  for (const u of unmount) u();
  unmount = [
    mountWalker(document.getElementById('walker'), ctx),
    mountReclaim(document.getElementById('reclaim'), ctx),
    mountFairShare(document.getElementById('fair-share'), ctx),
  ];
}

select.addEventListener('change', () => {
  params.set('ctx', select.value);
  history.replaceState(null, '', `?${params}`);
  mountAll(MOCKS[select.value]);
});

mountAll(MOCKS[name]);
