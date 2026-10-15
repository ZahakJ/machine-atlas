import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseHash, href, safeHref, linkTarget, samePage } from '../js/router.js';

test('parseHash: the four route families', () => {
  const cases = [
    ['', { view: 'overview', id: null }],
    ['#', { view: 'overview', id: null }],
    ['#/', { view: 'overview', id: null }],
    ['#/learn', { view: 'learn', id: null }],
    ['#/learn/', { view: 'learn', id: null }],
    ['#/learn/page-faults', { view: 'learn', id: 'page-faults' }],
    ['#/explore/page-cache', { view: 'explore', id: 'page-cache' }],
    ['#/inspect', { view: 'inspect', id: null }],
    ['#/inspect/mem.meminfo.Active_anon', { view: 'inspect', id: 'mem.meminfo.Active_anon' }],
    ['#/inspect/derived.paging.levels', { view: 'inspect', id: 'derived.paging.levels' }],
  ];
  for (const [hash, want] of cases) {
    const got = parseHash(hash);
    assert.equal(got.view, want.view, hash);
    assert.equal(got.id, want.id, hash);
  }
});

test('parseHash: unknown views, extra segments and bad escapes are notfound', () => {
  for (const hash of ['#/nope', '#/learn/a/b', '#/inspect/%E0%A4%A', '#/overview']) {
    assert.equal(parseHash(hash).view, 'notfound', hash);
  }
});

test('parseHash: query parameters', () => {
  const r = parseHash('#/inspect?status=permission_denied&q=meminfo%20active');
  assert.equal(r.view, 'inspect');
  assert.deepEqual(r.params, { status: 'permission_denied', q: 'meminfo active' });
  assert.deepEqual(parseHash('#/inspect/cpu.online?ns=cpu').params, { ns: 'cpu' });
});

test('href builds routes that parse back', () => {
  assert.equal(href('overview'), '#/');
  assert.equal(href('learn', 'page-faults'), '#/learn/page-faults');
  assert.equal(href('inspect', null, { status: 'failed', q: '' }), '#/inspect?status=failed');
  for (const [view, id] of [['inspect', 'mem.meminfo.Active_anon'], ['explore', 'swap-areas'], ['learn', null]]) {
    const r = parseHash(href(view, id, { q: 'a b&c' }));
    assert.equal(r.view, view);
    assert.equal(r.id, id);
    assert.equal(r.params.q, 'a b&c');
  }
  assert.ok(samePage(parseHash('#/learn/x?a=1'), parseHash('#/learn/x')));
  assert.ok(!samePage(parseHash('#/learn/x'), parseHash('#/learn/y')));
});

test('safeHref accepts http(s) and internal routes only', () => {
  assert.deepEqual(safeHref('https://docs.kernel.org/admin-guide/mm/'), { href: 'https://docs.kernel.org/admin-guide/mm/', external: true });
  assert.equal(safeHref('http://example.org/x').external, true);
  assert.deepEqual(safeHref('#/learn/page-faults'), { href: '#/learn/page-faults', external: false });
  const refused = [
    'javascript:alert(1)', 'JAVASCRIPT:alert(1)', ' javascript:alert(1)', 'java\tscript:alert(1)',
    'data:text/html,<b>x</b>', 'vbscript:x', '//evil.example/x', '/api/state', 'page-faults',
    'https://user:pw@example.org/', 'https://', '#/nope', '', null, undefined, 42, 'https://exa mple.org',
  ];
  for (const t of refused) assert.equal(safeHref(t), null, String(t));
});

test('linkTarget maps lesson link kinds to routes and refuses bad ids', () => {
  assert.equal(linkTarget('lesson', 'page-cache').href, '#/learn/page-cache');
  assert.equal(linkTarget('component', 'swap-areas').href, '#/explore/swap-areas');
  assert.equal(linkTarget('obs', 'mem.meminfo.MemTotal').href, '#/inspect/mem.meminfo.MemTotal');
  assert.equal(linkTarget('url', 'https://man7.org/linux/man-pages/man2/mmap.2.html').external, true);
  assert.equal(linkTarget('url', 'javascript:alert(1)'), null);
  assert.equal(linkTarget('lesson', '../x'), null);
  assert.equal(linkTarget('obs', 'Mem Total'), null);
  assert.equal(linkTarget('other', 'x'), null);
});
