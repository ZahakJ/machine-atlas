// Mock ctx objects for the development pages. Every value is invented; the
// shapes follow ARCHITECTURE.md §4 (observations) and §6 (derived facts).

const at = '2026-01-15T12:00:00Z';

function obs(id, value, extra = {}) {
  return { id, value, unit: '', kind: 'static', scope: 'kernel', status: 'ok', source: { type: 'file' }, collected_at: at, collector: 'dev.mock', ...extra };
}

function fact(id, value) {
  return { id, value, status: 'ok', rule: 'mock', inputs: [], basis: 'inference' };
}

const desktop = {
  observations: [
    obs('mem.sysctl.vm.swappiness', 60),
    obs('sched.self.nice', 0, { scope: 'process' }),
    obs('sched.self.slice_us', 3000, { scope: 'process', unit: 'microseconds' }),
    obs('probe.regions', [
      { name: 'anon-untouched', start: '0x7f3e9c000000', end: '0x7f3ea0000000' },
      { name: 'anon-written', start: '0x7f3e9b600000', end: '0x7f3e9be00000' },
      { name: 'file-private', start: '0x55b1c3a00000', end: '0x55b1c3e00000' },
    ], { scope: 'process', source: { type: 'probe' } }),
    obs('probe.layout.runs', [
      { run: 1, stack: '0x7ffc4a1d2000', mmap_base: '0x7f3ea4e00000', heap: '0x55b1c5e41000', exe: '0x55b1c3a00000' },
      { run: 2, stack: '0x7ffd0b6a9000', mmap_base: '0x7f81c2a00000', heap: '0x5603f7c12000', exe: '0x5603f5e00000' },
    ], { scope: 'process', source: { type: 'probe' } }),
  ],
  derived: [fact('derived.paging.profile', 'x86_64-4level'), fact('derived.swap.configured', true), fact('derived.sched.fair_scheduler', 'eevdf')],
};

const pi = {
  observations: [
    obs('mem.sysctl.vm.swappiness', 60),
    { ...obs('probe.regions', null), status: 'not_collected', note: 'probe disabled' },
  ],
  derived: [fact('derived.paging.profile', 'arm64-4k-39'), fact('derived.swap.configured', false), fact('derived.sched.fair_scheduler', 'cfs')],
};

function ctxFrom(data) {
  const o = new Map(data.observations.map((x) => [x.id, x]));
  const d = new Map(data.derived.map((x) => [x.id, x]));
  return {
    obs: (id) => o.get(id),
    derived: (id) => d.get(id),
    navigate: (hash) => console.info('navigate', hash),
  };
}

/** Named mock contexts: a desktop with swap, a swapless arm64 board, and nothing at all. */
export const MOCKS = {
  desktop: ctxFrom(desktop),
  'rpi-noswap': ctxFrom(pi),
  empty: {},
};
