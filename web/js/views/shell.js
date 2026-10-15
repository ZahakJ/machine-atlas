// The frame around every view: the top bar (name, the three views, theme)
// and the persistent mode banner, which says whose data is on screen and
// offers the actions that change it: re-collect, export, import,
// demonstration machines, back to live.

import { el } from '../lib/dom.js';
import { formatAge, formatCount } from '../lib/format.js';
import { api } from '../api.js';
import { href } from '../router.js';
import { loadTheme, saveTheme, nextTheme } from '../store.js';
import { errorBox, formatTime, idText, loading } from '../render/common.js';

const TABS = [['learn', 'Learn'], ['explore', 'Explore'], ['inspect', 'Inspect']];

export function createShell(app) {
  const { session } = app;
  const storage = safeStorage();

  // ---- top bar
  const tabs = TABS.map(([view, label]) => el('a', { class: 'tab', href: href(view), 'data-view': view }, label));
  const themeButton = el('button', { class: 'btn btn--small btn--quiet theme-toggle', type: 'button', onclick: toggleTheme });
  const topbar = el('header', { class: 'topbar' },
    el('a', { class: 'topbar__brand', href: href('overview') }, el('span', { class: 'topbar__name' }, 'Machine Atlas')),
    el('nav', { class: 'tabs', 'aria-label': 'Views' }, tabs),
    el('div', { class: 'topbar__tools' }, themeButton));

  // ---- banner
  const banner = el('div', { class: 'mode-banner', role: 'region', 'aria-label': 'Data source' });
  let ticker = null;

  function renderBanner() {
    const st = session.state ?? {};
    const snap = st.snapshot ?? {};
    const mode = st.mode ?? 'snapshot';
    banner.dataset.mode = mode;
    const words = { live: 'Live', snapshot: 'Snapshot', fixture: 'Demonstration' }[mode] ?? mode;
    let text;
    if (mode === 'live') {
      const at = snap.collection?.finished_at || snap.created_at;
      text = ['Collected from this machine at ', el('time', { datetime: at }, formatTime(at)), ', ', age(at), '.'];
    } else if (mode === 'fixture') {
      text = ['Demonstration data (', el('code', {}, st.fixture || snap.collection?.fixture || 'fixture'), '), not your machine. Captured ', age(snap.created_at), '.'];
    } else {
      text = ['Captured ', age(snap.created_at), ' (', el('time', { datetime: snap.created_at }, formatTime(snap.created_at)), ') — not this machine’s current state.'];
    }
    const red = snap.redaction ?? {};
    const redaction = red.applied
      ? `Redacted (${red.policy} policy, ${formatCount(red.replaced ?? 0)} value${red.replaced === 1 ? '' : 's'} replaced).`
      : mode === 'live' ? 'Not redacted: this view stays on this machine; export redacts.' : 'Not redacted.';

    const actions = [];
    if (session.capability('collect')) actions.push(action('Re-collect', recollect, 'Read this machine again now'));
    if (mode !== 'live' && session.capability('live')) actions.push(action('Back to live', backToLive, 'Show this machine instead'));
    actions.push(action('Export…', openExport, 'Download this snapshot, redacted'));
    actions.push(action('Import…', openImport, 'Open a snapshot file'));
    actions.push(action('Demonstration machines…', openFixtures, 'Show an embedded example machine'));

    banner.replaceChildren(
      el('div', { class: 'mode-banner__text' },
        el('strong', { class: 'mode-banner__mode' }, words),
        el('span', {}, text),
        el('span', { class: 'mode-banner__redaction' }, redaction),
        st.busy ? el('span', { class: 'mode-banner__busy' }, `Busy: ${st.busy}`) : null),
      el('div', { class: 'mode-banner__actions cluster' }, actions));
  }

  // Ages ("4 minutes ago") are relative to now; a timer refreshes just
  // these spans, so focus inside the banner is never disturbed.
  function age(at) {
    return el('span', { 'data-age': at }, formatAge(at));
  }

  function action(label, fn, title) {
    const b = el('button', { class: 'btn btn--small btn--quiet', type: 'button', title }, label);
    b.addEventListener('click', () => fn(b));
    return b;
  }

  async function runChange(button, label, request, done) {
    const old = button.textContent;
    button.disabled = true;
    button.textContent = label;
    app.announce(`${label}`);
    try {
      await session.change(request);
      app.announce(done);
    } catch (err) {
      showError(err, 'The data did not change');
    } finally {
      if (button.isConnected) {
        button.disabled = false;
        button.textContent = old;
      }
    }
  }

  const recollect = (b) => runChange(b, 'Collecting…', api.collect, 'Collected this machine again.');
  const backToLive = (b) => runChange(b, 'Collecting…', api.live, 'Now showing this machine, collected just now.');

  // ---- dialogs
  function dialog(title, body, footer) {
    const id = `dlg-${Math.random().toString(36).slice(2, 8)}`;
    const d = el('dialog', { class: 'dialog', 'aria-labelledby': id },
      el('header', { class: 'dialog__header' }, el('h2', { id }, title)),
      el('div', { class: 'dialog__body' }, body),
      el('footer', { class: 'dialog__footer' }, footer));
    d.addEventListener('close', () => d.remove());
    // A click on the backdrop (the dialog element itself) closes it.
    d.addEventListener('click', (e) => { if (e.target === d) d.close(); });
    document.body.append(d);
    d.showModal();
    return d;
  }

  function closeButton(label = 'Cancel') {
    const b = el('button', { class: 'btn', type: 'button' }, label);
    b.addEventListener('click', () => b.closest('dialog').close());
    return b;
  }

  function showError(err, title) {
    const d = dialog(title, errorBox(err, null, err?.code === 'network' ? 'No connection' : 'The server said'), closeButton('Close'));
    d.querySelector('.btn').focus();
  }

  function openExport() {
    const POLICIES = [
      ['default', 'Default', 'Replaces hostnames, usernames, addresses and serials; scrubs home directories, per-user paths, UUIDs and MAC or IP addresses inside any value.'],
      ['strict', 'Strict', 'Default, and also drops hardware and kernel names that could single out a machine: DMI vendor and product, CPU model, kernel version string, device and interface names.'],
      ['none', 'None', 'Writes every value as collected. Only for files that never leave your hands.'],
    ];
    let policy = 'default';
    const preview = el('div', { class: 'export-preview', 'aria-live': 'polite' });
    const choices = el('div', { class: 'policy-choices', role: 'radiogroup', 'aria-label': 'Redaction policy' },
      POLICIES.map(([value, name, text]) => {
        const input = el('input', { type: 'radio', name: 'export-policy', value, checked: value === policy });
        input.addEventListener('change', () => { policy = value; refresh(); });
        return el('label', { class: 'option policy-choice' }, input, el('span', {}, el('strong', {}, name), el('span', { class: 'caption policy-choice__text' }, text)));
      }));
    const download = el('button', { class: 'btn btn--primary', type: 'button' }, 'Download');
    download.addEventListener('click', () => {
      // A same-origin GET: the server names the file after the redacted
      // snapshot's id, so nothing here builds a file name from data.
      const a = el('a', { href: api.exportUrl(policy), download: '' });
      document.body.append(a);
      a.click();
      a.remove();
      app.announce(`Downloading the snapshot with the ${policy} redaction policy.`);
      download.closest('dialog').close();
    });

    let seq = 0;
    async function refresh() {
      const mine = ++seq;
      preview.replaceChildren(loading('Computing what would change'));
      try {
        const res = await api.preview(policy);
        if (mine !== seq) return;
        preview.replaceChildren(previewTable(res.changes ?? [], policy));
      } catch (err) {
        if (mine === seq) preview.replaceChildren(errorBox(err, refresh));
      }
    }

    dialog('Export this snapshot', [
      el('p', {}, 'The file is a versioned snapshot another copy of Machine Atlas can import. Choose how much to redact; the list shows exactly what will be replaced before anything is written.'),
      choices,
      preview,
    ], [closeButton(), download]);
    refresh();
  }

  function previewTable(changes, policy) {
    if (policy === 'none') {
      return el('div', { class: 'warning', role: 'note' },
        el('p', {}, el('strong', {}, 'Nothing will be redacted. '),
          'Hostnames, user names, paths and hardware names are written as collected. ',
          changes.length ? `The default policy would have replaced ${changes.length} value${changes.length === 1 ? '' : 's'}.` : ''));
    }
    if (changes.length === 0) return el('p', { class: 'empty' }, 'Nothing in this snapshot matches the policy: the file is written unchanged.');
    return el('div', {},
      el('p', { class: 'caption' }, `${formatCount(changes.length)} value${changes.length === 1 ? '' : 's'} will be replaced:`),
      // One change per item, stacked: what it is, then before → after, each
      // wrapping within the dialog so nothing hides off to the right.
      el('ol', { class: 'export-preview__table redact-list' }, changes.map((c) => el('li', { class: 'redact-item' },
        el('p', { class: 'redact-item__what' }, el('code', {}, idText(c.id)), c.field && c.field !== 'value' ? el('span', { class: 'caption' }, ` · ${c.field}`) : null,
          el('span', { class: 'tag' }, c.class)),
        el('p', { class: 'redact-item__change' },
          el('span', { class: 'visually-hidden' }, 'Before: '), el('span', { class: 'mono redact-before' }, c.before),
          el('span', { class: 'redact-item__arrow', 'aria-hidden': 'true' }, ' → '),
          el('span', { class: 'visually-hidden' }, ', after: '), el('span', { class: 'mono redact-after' }, c.after))))));
  }

  function openImport() {
    const input = el('input', { type: 'file', accept: '.json,application/json', id: 'import-file', class: 'input' });
    const result = el('div', { 'aria-live': 'polite' });
    const go = el('button', { class: 'btn btn--primary', type: 'button', disabled: true }, 'Import and show');
    input.addEventListener('change', () => {
      go.disabled = !input.files?.length;
      result.replaceChildren();
    });
    go.addEventListener('click', async () => {
      const file = input.files?.[0];
      if (!file) return;
      go.disabled = true;
      result.replaceChildren(loading('Validating'));
      try {
        await session.change(() => api.importSnapshot(file));
        app.announce('Imported snapshot is now shown.');
        go.closest('dialog')?.close();
      } catch (err) {
        // Validation problems come back one per line; they are shown as
        // the server wrote them, as text.
        result.replaceChildren(errorBox(err, null, 'This file was not imported'));
        go.disabled = false;
      }
    });
    dialog('Import a snapshot', [
      el('p', {}, 'Open a snapshot exported by Machine Atlas. It is validated first: schema, sizes, ids, value types.'),
      el('div', { class: 'field' }, el('label', { class: 'field__label', for: 'import-file' }, 'Snapshot file (.json, at most 8 MiB)'), input),
      result,
    ], [closeButton(), go]);
  }

  function openFixtures() {
    const list = el('div', {}, loading('Loading the demonstration machines'));
    const d = dialog('Demonstration machines', [
      el('p', {}, 'Embedded snapshots of invented but realistic machines, each exercising different behaviour. They show how lessons and evidence change from one machine to another.'),
      list,
    ], [closeButton('Close')]);
    const current = session.state?.mode === 'fixture' ? session.state.fixture : null;
    api.fixtures().then((fixtures) => {
      list.replaceChildren(el('ul', { class: 'fixture-list' }, fixtures.map((f) => {
        const b = el('button', { class: 'btn btn--small', type: 'button', disabled: f.name === current }, f.name === current ? 'Shown now' : 'Show');
        b.addEventListener('click', async () => {
          b.disabled = true;
          b.textContent = 'Loading…';
          try {
            await session.change(() => api.activateFixture(f.name));
            app.announce(`Now showing the demonstration machine ${f.name}.`);
            d.close();
          } catch (err) {
            list.prepend(errorBox(err));
            b.disabled = false;
            b.textContent = 'Show';
          }
        });
        return el('li', { class: 'fixture', 'data-current': f.name === current ? 'true' : null },
          el('div', {}, el('p', { class: 'fixture__name' }, el('code', {}, f.name)),
            el('p', { class: 'fixture__desc' }, f.description || ''),
            el('p', { class: 'caption' }, `${formatCount(f.observations ?? 0)} observations · captured ${formatTime(f.created_at)}`)),
          b);
      })));
    }).catch((err) => list.replaceChildren(errorBox(err, openFixtures)));
  }

  // ---- theme
  function applyTheme() {
    const saved = loadTheme(storage);
    if (saved) document.documentElement.setAttribute('data-theme', saved);
    else document.documentElement.removeAttribute('data-theme');
    const dark = saved ? saved === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
    themeButton.replaceChildren(el('span', { 'aria-hidden': 'true', class: 'theme-toggle__icon' }, dark ? '☾' : '☀'), el('span', { class: 'theme-toggle__label' }, dark ? 'Dark' : 'Light'));
    themeButton.setAttribute('aria-label', `Theme: ${dark ? 'dark' : 'light'}${saved ? '' : ' (system)'}. Switch to ${dark ? 'light' : 'dark'}`);
  }

  function toggleTheme() {
    const systemDark = matchMedia('(prefers-color-scheme: dark)').matches;
    saveTheme(storage, nextTheme(loadTheme(storage), systemDark));
    applyTheme();
  }

  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
  applyTheme();

  return {
    topbar,
    banner,
    render() {
      renderBanner();
      clearInterval(ticker);
      ticker = setInterval(() => {
        for (const span of banner.querySelectorAll('[data-age]')) span.textContent = formatAge(span.dataset.age);
      }, 30_000);
    },
    setView(view) {
      for (const t of tabs) {
        if (t.dataset.view === view) t.setAttribute('aria-current', 'page');
        else t.removeAttribute('aria-current');
      }
    },
  };
}

export function safeStorage() {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}
