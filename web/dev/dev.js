// Shared by the development pages: theme switching, persisted per browser.

const KEY = 'machine-atlas.dev.theme';

function apply(theme) {
  if (theme === 'light' || theme === 'dark') document.documentElement.setAttribute('data-theme', theme);
  else document.documentElement.removeAttribute('data-theme');
  for (const b of document.querySelectorAll('[data-set-theme]')) {
    b.setAttribute('aria-pressed', String(b.dataset.setTheme === (theme || 'system')));
  }
}

export function setupThemeButtons() {
  let saved = null;
  try { saved = localStorage.getItem(KEY); } catch { /* storage unavailable */ }
  apply(saved);
  for (const b of document.querySelectorAll('[data-set-theme]')) {
    b.addEventListener('click', () => {
      const t = b.dataset.setTheme === 'system' ? null : b.dataset.setTheme;
      try { if (t) localStorage.setItem(KEY, t); else localStorage.removeItem(KEY); } catch { /* ignore */ }
      apply(t);
    });
  }
}
