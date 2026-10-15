// Packages, cores, hardware threads and the caches they share, for the
// cpu-topology diagram. Pure; tested in web/test/diagram-cpu-topology.test.mjs.
//
// Input is cpu.topology (one row per online logical CPU) and, optionally,
// cpu.cache_domains (one row per distinct cache with the CPUs sharing it).
// A core is the set of logical CPUs listed in each other's thread_siblings
// within one package: core ids repeat across packages and need not be
// contiguous, so they cannot identify a core on their own. Each cache is
// placed where its sharing puts it: inside one core (private), around a
// group of cores within a package (a cluster, such as the L2 shared by
// four efficiency cores), across a whole package (typically L3), or across
// packages.

/** Parses the kernel's CPU list format ("0-3,8"); null when malformed. */
export function parseCPUList(s) {
  if (typeof s !== 'string') return null;
  const t = s.trim();
  if (t === '') return [];
  const out = [];
  for (const part of t.split(',')) {
    const m = /^(\d+)(?:-(\d+))?$/.exec(part);
    if (!m) return null;
    const a = Number(m[1]);
    const b = m[2] === undefined ? a : Number(m[2]);
    if (b < a || b - a > 65536 || (out.length && a <= out[out.length - 1])) return null;
    for (let c = a; c <= b; c++) out.push(c);
  }
  return out;
}

/** "L1d", "L1i", "L2": the usual short name of a cache. */
export function cacheName(c) {
  const suffix = { Data: 'd', Instruction: 'i' }[c.type] ?? '';
  return `L${c.level}${suffix}`;
}

const int = (v) => Number.isInteger(v);

/**
 * Builds the drawing model. Returns:
 *   {packages: [{id, cpus, shared: [cache], groups: [{cache|null, cores: [core]}]}],
 *    machineShared: [cache], cores, threads, hybrid, skipped, cacheSkipped, cachesKnown}
 * where core = {key, package, coreId, threads: [cpu], coreType, capacity, caches: [cache], isolated: [cpu]}
 * and cache = {name, level, type, size, cpus: [cpu]}.
 */
export function topologyModel(topo, caches, { isolated = [] } = {}) {
  let skipped = 0;
  const iso = new Set(isolated);
  const coreMap = new Map();
  for (const r of Array.isArray(topo) ? topo : []) {
    const sib = r && parseCPUList(r.thread_siblings);
    if (!r || !int(r.cpu) || !int(r.package) || !sib || !sib.includes(r.cpu)) {
      skipped++;
      continue;
    }
    const key = `${r.package}:${sib.join(',')}`;
    let core = coreMap.get(key);
    if (!core) {
      core = { key, package: r.package, coreId: int(r.core_id) ? r.core_id : null, threads: [], coreType: null, capacity: null, caches: [], isolated: [] };
      coreMap.set(key, core);
    }
    core.threads.push(r.cpu);
    if (typeof r.core_type === 'string') core.coreType = r.core_type;
    if (int(r.capacity)) core.capacity = Math.max(core.capacity ?? 0, r.capacity);
    if (iso.has(r.cpu)) core.isolated.push(r.cpu);
  }
  const cores = [...coreMap.values()];
  for (const c of cores) {
    c.threads.sort((a, b) => a - b);
    c.isolated.sort((a, b) => a - b);
  }
  cores.sort((a, b) => a.package - b.package || a.threads[0] - b.threads[0]);

  const pkgMap = new Map();
  for (const c of cores) {
    if (!pkgMap.has(c.package)) pkgMap.set(c.package, { id: c.package, cpus: [], shared: [], clusters: [], cores: [] });
    const p = pkgMap.get(c.package);
    p.cores.push(c);
    p.cpus.push(...c.threads);
  }
  const packages = [...pkgMap.values()];
  for (const p of packages) p.cpus.sort((a, b) => a - b);

  // Caches.
  let cacheSkipped = 0;
  const machineShared = [];
  const cachesKnown = Array.isArray(caches);
  const coreOf = new Map();
  for (const c of cores) for (const t of c.threads) coreOf.set(t, c);
  for (const r of cachesKnown ? caches : []) {
    const cpus = r && parseCPUList(r.shared_cpu_list);
    if (!r || !int(r.level) || typeof r.type !== 'string' || !cpus || !cpus.length || cpus.some((c) => !coreOf.has(c))) {
      cacheSkipped++;
      continue;
    }
    const cache = { name: cacheName(r), level: r.level, type: r.type, size: int(r.size_bytes) ? r.size_bytes : null, cpus };
    const touched = [...new Set(cpus.map((c) => coreOf.get(c)))];
    const pkgs = [...new Set(touched.map((c) => c.package))];
    if (touched.length === 1) {
      const core = touched[0];
      if (!core.caches.some((x) => x.name === cache.name)) core.caches.push(cache);
    } else if (pkgs.length > 1) {
      machineShared.push(cache);
    } else {
      const p = pkgMap.get(pkgs[0]);
      if (touched.length === p.cores.length) p.shared.push(cache);
      else p.clusters.push({ cache, cores: touched });
    }
  }
  const byLevel = (a, b) => a.level - b.level || a.name.localeCompare(b.name);
  for (const c of cores) c.caches.sort(byLevel);
  machineShared.sort(byLevel);

  // Group each package's cores by the lowest-level cluster cache, so cores
  // that share one sit together; cores in no cluster stand alone.
  for (const p of packages) {
    p.shared.sort(byLevel);
    const level = Math.min(...p.clusters.map((x) => x.cache.level));
    const clusters = p.clusters.filter((x) => x.cache.level === level);
    const placed = new Set();
    const groups = [];
    for (const cl of clusters) {
      const members = cl.cores.filter((c) => !placed.has(c));
      if (!members.length) continue;
      members.forEach((c) => placed.add(c));
      groups.push({ cache: cl.cache, cores: members.sort((a, b) => a.threads[0] - b.threads[0]) });
    }
    for (const c of p.cores) if (!placed.has(c)) groups.push({ cache: null, cores: [c] });
    groups.sort((a, b) => a.cores[0].threads[0] - b.cores[0].threads[0]);
    // Adjacent stand-alone cores share one unboxed group.
    p.groups = [];
    for (const g of groups) {
      const last = p.groups[p.groups.length - 1];
      if (!g.cache && last && !last.cache) last.cores.push(...g.cores);
      else p.groups.push({ cache: g.cache, cores: [...g.cores] });
    }
    delete p.clusters;
    delete p.cores;
  }

  const capacities = new Set(cores.map((c) => c.capacity).filter((v) => v !== null));
  return {
    packages,
    machineShared,
    cores: cores.length,
    threads: cores.reduce((n, c) => n + c.threads.length, 0),
    hybrid: cores.some((c) => c.coreType) || capacities.size > 1,
    skipped,
    cacheSkipped,
    cachesKnown,
  };
}

/** One line describing a cache: "L3 16 MiB, shared by 16 CPUs". */
export function cacheLine(c, formatSize) {
  const size = c.size !== null ? ` ${formatSize(c.size)}` : '';
  const who = c.cpus.length === 1 ? 'one CPU' : `${c.cpus.length} CPUs`;
  return `${c.name}${size}, shared by ${who}`;
}
