"""Shared helpers for generating Machine Atlas fixture roots.

Every number that appears in more than one file is computed once here, so the
files agree with each other the way a real kernel's do.
"""
import json
import math
import os
import random
import shutil

KB = 1024


class Root:
    def __init__(self, base, name):
        self.dir = os.path.join(base, name)
        if os.path.exists(self.dir):
            shutil.rmtree(self.dir)
        os.makedirs(self.dir)

    def w(self, path, content):
        p = os.path.join(self.dir, path.lstrip('/'))
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, 'w') as f:
            f.write(content)

    def mkdir(self, path):
        # git keeps no empty directories; a .keep marks one that must exist.
        self.w(path.rstrip('/') + '/.keep', '')


def ver_ge(ver, *v):
    return tuple(ver) >= v


# ---------------------------------------------------------------- memory model

class Zone:
    def __init__(self, node, name, start_pfn, spanned, present, managed):
        self.node, self.name = node, name
        self.start_pfn, self.spanned, self.present, self.managed = start_pfn, spanned, present, managed
        self.free = 0
        self.min = self.low = self.high = self.promo = 0
        self.protection = []


LOWMEM_RATIO = {'DMA': 256, 'DMA32': 256, 'Normal': 32, 'Movable': 0, 'Device': 0}


def watermarks(m):
    """Per-zone min/low/high from vm.min_free_kbytes and watermark_scale_factor
    (mm/page_alloc.c __setup_per_zone_wmarks)."""
    page_kb = m['page_size'] // KB
    min_pages = m['min_free_kbytes'] // page_kb
    lowmem = sum(z.managed for z in m['zones'])
    for z in m['zones']:
        if z.managed == 0:
            continue
        zmin = min_pages * z.managed // lowmem
        step = max(zmin // 4, z.managed * m['watermark_scale_factor'] // 10000)
        z.min, z.low, z.high, z.promo = zmin, zmin + step, zmin + 2 * step, zmin + 3 * step


def protections(m):
    """lowmem_reserve: zone i keeps managed(i+1..j)/ratio(i) pages back from
    allocations that could have used zone j."""
    names = m['zone_types']
    for node in sorted({z.node for z in m['zones']}):
        zs = {z.name: z for z in m['zones'] if z.node == node}
        for i, n in enumerate(names):
            if n not in zs:
                continue
            prot = []
            for j in range(len(names)):
                if j <= i or LOWMEM_RATIO[n] == 0:
                    prot.append(0)
                    continue
                s = sum(zs[k].managed for k in names[i + 1:j + 1] if k in zs)
                prot.append(s // LOWMEM_RATIO[n])
            zs[n].protection = prot


def all_zones(m):
    """Zones as zoneinfo lists them: every zone type for every node,
    populated or not."""
    out = []
    for node in m['nodes']:
        for name in m['zone_types']:
            z = next((z for z in m['zones'] if z.node == node and z.name == name), None)
            if z is None:
                z = Zone(node, name, 0, 0, 0, 0)
                if name == 'Movable' and ver_ge(m['kver'], 5, 0):
                    # Movable (like highmem) gets a minimum clamped to
                    # [SWAP_CLUSTER_MAX, 128] even when empty.
                    z.min = z.low = z.high = z.promo = 32
            out.append(z)
    return out


def distribute_free(m, free_pages, rng):
    """Spread free pages over the zones: small zones keep most of their pages
    free (little allocates from DMA), the rest goes proportionally."""
    zones = [z for z in m['zones'] if z.managed]
    left = free_pages
    for z in zones:
        if z.name == 'DMA':
            z.free = m.get('dma_free', min(z.managed * 3 // 4, left))
            left -= z.free
    big = [z for z in zones if z.name != 'DMA']
    total = sum(z.managed for z in big)
    for i, z in enumerate(big):
        if i == len(big) - 1:
            z.free = left
        else:
            z.free = int(left * z.managed / total * rng.uniform(0.9, 1.1))
            z.free = min(z.free, left)
            left -= z.free
            total -= z.managed


def buddy(free, orders, rng, frag):
    """Free block counts per order summing exactly to free pages. frag (0..1)
    moves free memory towards low orders, like a long-running machine."""
    counts = [0] * orders
    left = free
    for o in range(orders - 1, 0, -1):
        share = (1 - frag) ** 2 * 0.5 if o >= orders - 2 else 0.12 + 0.1 * frag
        n = int(left * share * rng.uniform(0.6, 1.0)) >> o
        counts[o] = n
        left -= n << o
    counts[0] = left
    return counts


def kb(pages, page_size):
    return pages * page_size // KB


# ------------------------------------------------------------------- meminfo

def meminfo_fields(m):
    """Field list for the kernel version and architecture, in kernel order."""
    v, x86 = m['kver'], m['arch'] == 'x86_64'
    f = ['MemTotal', 'MemFree']
    f += ['MemAvailable']  # 3.14+
    f += ['Buffers', 'Cached', 'SwapCached', 'Active', 'Inactive', 'Active(anon)', 'Inactive(anon)',
          'Active(file)', 'Inactive(file)', 'Unevictable', 'Mlocked', 'SwapTotal', 'SwapFree']
    if ver_ge(v, 6, 0) and m.get('zswap_built', True):
        f += ['Zswap', 'Zswapped']
    f += ['Dirty', 'Writeback', 'AnonPages', 'Mapped', 'Shmem']
    if ver_ge(v, 4, 20):
        f += ['KReclaimable']
    f += ['Slab', 'SReclaimable', 'SUnreclaim', 'KernelStack', 'PageTables']
    if ver_ge(v, 6, 0):
        f += ['SecPageTables']
    f += ['NFS_Unstable', 'Bounce', 'WritebackTmp', 'CommitLimit', 'Committed_AS', 'VmallocTotal',
          'VmallocUsed', 'VmallocChunk', 'Percpu']
    if x86:
        f += ['HardwareCorrupted']
    f += ['AnonHugePages', 'ShmemHugePages', 'ShmemPmdMapped']
    if ver_ge(v, 5, 4):
        f += ['FileHugePages', 'FilePmdMapped']
    if m.get('cma'):
        f += ['CmaTotal', 'CmaFree']
    if ver_ge(v, 6, 5) and x86:
        f += ['Unaccepted']
    f += ['HugePages_Total', 'HugePages_Free', 'HugePages_Rsvd', 'HugePages_Surp', 'Hugepagesize', 'Hugetlb']
    if x86:
        f += ['DirectMap4k', 'DirectMap2M']
        if m.get('directmap1g'):
            f += ['DirectMap1G']
    return f


def build_meminfo(m, rng):
    """Compute every meminfo value in kB from a few chosen quantities, set the
    free pages to what is left, and return the dict."""
    ps = m['page_size']
    total_kb = sum(kb(z.managed, ps) for z in m['zones'])
    u = m['use']  # chosen consumers, kB
    mi = {'MemTotal': total_kb}
    for k in ('Buffers', 'Cached', 'AnonPages', 'Shmem', 'Mapped', 'SReclaimable', 'SUnreclaim',
              'KernelStack', 'PageTables', 'Percpu', 'Dirty', 'Writeback', 'AnonHugePages',
              'ShmemHugePages', 'Unevictable', 'Mlocked', 'VmallocUsed', 'Committed_AS'):
        mi[k] = u.get(k, 0)
    mi['SecPageTables'] = u.get('SecPageTables', 0)
    mi['Slab'] = mi['SReclaimable'] + mi['SUnreclaim']
    mi['KReclaimable'] = mi['SReclaimable']
    swap_total = m.get('swap_total_kb', 0)
    swap_used = m.get('swap_used_kb', 0)
    mi['SwapTotal'], mi['SwapFree'] = swap_total, swap_total - swap_used
    mi['SwapCached'] = u.get('SwapCached', 0)
    mi['Zswap'], mi['Zswapped'] = u.get('Zswap', 0), u.get('Zswapped', 0)
    hp = m.get('hugepages', {})
    hugetlb_kb = sum(size_kb * n for size_kb, (n, free) in hp.items())
    mi['Hugetlb'] = hugetlb_kb
    misc = int(total_kb * m.get('unaccounted_frac', 0.02))  # drivers, firmware buffers: not itemised
    used = (mi['AnonPages'] + mi['Cached'] + mi['Buffers'] + mi['SwapCached'] + mi['Slab'] +
            mi['KernelStack'] + mi['PageTables'] + mi['SecPageTables'] + mi['Percpu'] + hugetlb_kb + misc +
            mi['Zswap'])
    free_kb = total_kb - used
    assert free_kb > 0, (m['name'], free_kb)
    free_pages = free_kb // (ps // KB)
    distribute_free(m, free_pages, rng)
    mi['MemFree'] = sum(kb(z.free, ps) for z in m['zones'])

    anon_lru = mi['AnonPages'] + mi['Shmem'] + mi['SwapCached'] - mi['Unevictable'] // 2
    file_lru = mi['Cached'] + mi['Buffers'] - mi['Shmem'] - mi['Unevictable'] // 2
    a = m.get('active_anon_frac', 0.7)
    fa = m.get('active_file_frac', 0.45)
    mi['Active(anon)'] = int(anon_lru * a)
    mi['Inactive(anon)'] = anon_lru - mi['Active(anon)']
    mi['Active(file)'] = int(file_lru * fa)
    mi['Inactive(file)'] = file_lru - mi['Active(file)']
    mi['Active'] = mi['Active(anon)'] + mi['Active(file)']
    mi['Inactive'] = mi['Inactive(anon)'] + mi['Inactive(file)']
    # MemAvailable (si_mem_available): free minus reserves, plus the
    # reclaimable half-or-better of page cache and reclaimable slab.
    reserve = sum(kb(z.high + max(z.protection or [0]), ps) for z in m['zones'])
    pagecache = mi['Active(file)'] + mi['Inactive(file)']
    pagecache -= min(pagecache // 2, kb(sum(z.low for z in m['zones']), ps))
    reclaimable = mi['SReclaimable']
    reclaimable -= min(reclaimable // 2, kb(sum(z.low for z in m['zones']), ps))
    mi['MemAvailable'] = max(0, mi['MemFree'] - reserve + pagecache + reclaimable)
    mi['NFS_Unstable'] = mi['Bounce'] = mi['WritebackTmp'] = 0
    hp_default = m.get('default_hugepage_kb', 2048)
    n_def, free_def = hp.get(hp_default, (0, 0))
    mi['CommitLimit'] = (total_kb - hugetlb_kb) * m['overcommit_ratio'] // 100 + swap_total
    mi['VmallocTotal'] = m['vmalloc_total_kb']
    mi['VmallocChunk'] = 0
    mi['HardwareCorrupted'] = 0
    mi['ShmemPmdMapped'] = 0
    mi['FileHugePages'] = u.get('FileHugePages', 0)
    mi['FilePmdMapped'] = u.get('FilePmdMapped', 0)
    mi['CmaTotal'] = m.get('cma', 0)
    mi['CmaFree'] = m.get('cma_free', 0)
    mi['Unaccepted'] = 0
    mi['HugePages_Total'], mi['HugePages_Free'] = n_def, free_def
    mi['HugePages_Rsvd'] = mi['HugePages_Surp'] = 0
    mi['Hugepagesize'] = hp_default
    if 'directmap' in m:
        mi['DirectMap4k'], mi['DirectMap2M'], mi['DirectMap1G'] = m['directmap']
    if m['kver'] < (5, 3):
        mi['VmallocUsed'] = 0  # 4.4 to 5.2 printed zero here
    return mi


def format_meminfo(m, mi, scale=1.0, prefix=''):
    lines = []
    for f in meminfo_fields(m):
        v = mi[f]
        if f == 'HardwareCorrupted':
            lines.append(f'HardwareCorrupted: {v:>5} kB')
        elif f.startswith('HugePages_'):
            lines.append(f'{prefix}{f + ":":<16}{v:>8}' if not prefix else f'{prefix}{f}:{v:>6}')
        else:
            lines.append(f'{prefix}{f + ":":<16}{v:>8} kB' if not prefix else f'{prefix}{f + ":":<16}{v:>8} kB')
    return '\n'.join(lines) + '\n'


def node_meminfo(m, mi, node):
    """/sys/devices/system/node/nodeN/meminfo: the node's share of each value
    (proportional to its managed pages), MemTotal and MemFree exact."""
    ps = m['page_size']
    zs = [z for z in m['zones'] if z.node == node]
    total = sum(kb(z.managed, ps) for z in zs)
    free = sum(kb(z.free, ps) for z in zs)
    frac = total / mi['MemTotal']
    used = total - free

    def p(k):
        return int(mi[k] * frac)
    rows = [('MemTotal', total), ('MemFree', free), ('MemUsed', used), ('SwapCached', p('SwapCached')),
            ('Active', p('Active(anon)') + p('Active(file)')), ('Inactive', p('Inactive(anon)') + p('Inactive(file)')),
            ('Active(anon)', p('Active(anon)')), ('Inactive(anon)', p('Inactive(anon)')),
            ('Active(file)', p('Active(file)')), ('Inactive(file)', p('Inactive(file)')),
            ('Unevictable', p('Unevictable')), ('Mlocked', p('Mlocked')), ('Dirty', p('Dirty')),
            ('Writeback', p('Writeback')), ('FilePages', p('Cached') + p('Buffers')), ('Mapped', p('Mapped')),
            ('AnonPages', p('AnonPages')), ('Shmem', p('Shmem')), ('KernelStack', p('KernelStack')),
            ('PageTables', p('PageTables'))]
    if ver_ge(m['kver'], 6, 0):
        rows.append(('SecPageTables', p('SecPageTables')))
    rows += [('NFS_Unstable', 0), ('Bounce', 0), ('WritebackTmp', 0)]
    if ver_ge(m['kver'], 4, 20):
        rows.append(('KReclaimable', p('KReclaimable')))
    rows += [('Slab', p('SReclaimable') + p('SUnreclaim')), ('SReclaimable', p('SReclaimable')),
             ('SUnreclaim', p('SUnreclaim')), ('AnonHugePages', p('AnonHugePages') // 2048 * 2048),
             ('ShmemHugePages', 0), ('ShmemPmdMapped', 0)]
    if ver_ge(m['kver'], 5, 4):
        rows += [('FileHugePages', 0), ('FilePmdMapped', 0)]
    if ver_ge(m['kver'], 6, 5) and m['arch'] == 'x86_64':
        rows.append(('Unaccepted', 0))
    hp = m.get('hugepages', {})
    n_def, free_def = hp.get(m.get('default_hugepage_kb', 2048), (0, 0))
    nn = len(m['nodes'])
    out = []
    for k, v in rows:
        out.append(f'Node {node} {k + ":":<15} {v:>8} kB')
    for k, v in (('HugePages_Total', n_def // nn), ('HugePages_Free', free_def // nn), ('HugePages_Surp', 0)):
        out.append(f'Node {node} {k + ":":<15} {v:>5}')
    return '\n'.join(out) + '\n'


# ------------------------------------------------------------------ zoneinfo

NODE_STATS_619 = ['nr_inactive_anon', 'nr_active_anon', 'nr_inactive_file', 'nr_active_file', 'nr_unevictable',
                  'nr_slab_reclaimable', 'nr_slab_unreclaimable', 'nr_isolated_anon', 'nr_isolated_file',
                  'workingset_refault', 'workingset_activate', 'workingset_nodereclaim', 'nr_anon_pages',
                  'nr_mapped', 'nr_file_pages', 'nr_dirty', 'nr_writeback', 'nr_writeback_temp', 'nr_shmem',
                  'nr_shmem_hugepages', 'nr_shmem_pmdmapped', 'nr_anon_transparent_hugepages', 'nr_unstable',
                  'nr_vmscan_write', 'nr_vmscan_immediate_reclaim', 'nr_dirtied', 'nr_written']


def node_stat_names(v):
    if not ver_ge(v, 5, 0):
        return NODE_STATS_619
    n = ['nr_inactive_anon', 'nr_active_anon', 'nr_inactive_file', 'nr_active_file', 'nr_unevictable',
         'nr_slab_reclaimable', 'nr_slab_unreclaimable', 'nr_isolated_anon', 'nr_isolated_file',
         'workingset_nodes', 'workingset_refault_anon', 'workingset_refault_file', 'workingset_activate_anon',
         'workingset_activate_file', 'workingset_restore_anon', 'workingset_restore_file',
         'workingset_nodereclaim', 'nr_anon_pages', 'nr_mapped', 'nr_file_pages', 'nr_dirty', 'nr_writeback',
         'nr_writeback_temp', 'nr_shmem', 'nr_shmem_hugepages', 'nr_shmem_pmdmapped', 'nr_file_hugepages',
         'nr_file_pmdmapped', 'nr_anon_transparent_hugepages', 'nr_vmscan_write', 'nr_vmscan_immediate_reclaim',
         'nr_dirtied', 'nr_written']
    if ver_ge(v, 5, 18):
        n.append('nr_throttled_written')
    n += ['nr_kernel_misc_reclaimable', 'nr_foll_pin_acquired', 'nr_foll_pin_released', 'nr_kernel_stack']
    n.append('nr_page_table_pages')
    if ver_ge(v, 6, 0):
        n.append('nr_sec_page_table_pages')
    if ver_ge(v, 6, 11):
        n.append('nr_iommu_pages')
    n.append('nr_swapcached')
    if ver_ge(v, 5, 18):
        n.append('pgpromote_success')
    if ver_ge(v, 6, 0):
        n.append('pgpromote_candidate')
    if ver_ge(v, 6, 1):
        n += ['pgdemote_kswapd', 'pgdemote_direct']
    if ver_ge(v, 6, 9):
        n.append('pgdemote_khugepaged')
    if ver_ge(v, 6, 7):
        n += ['pgsteal_kswapd', 'pgsteal_direct', 'pgsteal_khugepaged', 'pgsteal_anon', 'pgsteal_file',
              'pgscan_kswapd', 'pgscan_direct', 'pgscan_khugepaged', 'pgscan_anon', 'pgscan_file', 'pgrefill']
    return n


def zone_stat_names(m):
    v = m['kver']
    n = ['nr_free_pages', 'nr_zone_inactive_anon', 'nr_zone_active_anon', 'nr_zone_inactive_file',
         'nr_zone_active_file', 'nr_zone_unevictable', 'nr_zone_write_pending', 'nr_mlock']
    if not ver_ge(v, 5, 9):
        n += ['nr_page_table_pages', 'nr_kernel_stack']
    if not ver_ge(v, 6, 0):
        n.append('nr_bounce')
    n.append('nr_zspages')
    n.append('nr_free_cma')
    if ver_ge(v, 6, 5) and m['arch'] == 'x86_64':
        n.append('nr_unaccepted')
    return n


def numa_stat_names():
    return ['numa_hit', 'numa_miss', 'numa_foreign', 'numa_interleave', 'numa_local', 'numa_other']


def zoneinfo(m, node_vals, rng):
    v = m['kver']
    out = []
    first_populated = {}
    for z in all_zones(m):
        if z.present and z.node not in first_populated:
            first_populated[z.node] = z.name
    for z in all_zones(m):
        out.append(f'Node {z.node}, zone {z.name:>8}')
        if first_populated.get(z.node) == z.name:
            out.append('  per-node stats')
            for name in node_stat_names(v):
                out.append(f'      {name:<12} {node_vals[z.node].get(name, 0)}')
        out.append(f'  pages free     {z.free}')
        if ver_ge(v, 5, 0):
            out.append(f'        boost    0')
        out.append(f'        min      {z.min}')
        out.append(f'        low      {z.low}')
        out.append(f'        high     {z.high}')
        if ver_ge(v, 6, 1):
            out.append(f'        promo    {z.promo}')
        out.append(f'        spanned  {z.spanned}')
        out.append(f'        present  {z.present}')
        out.append(f'        managed  {z.managed}')
        if ver_ge(v, 5, 8):
            out.append(f'        cma      {m.get("cma", 0) // 4 if z.name == m.get("cma_zone") else 0}')
        prot = z.protection or [0] * len(m['zone_types'])
        out.append('        protection: (' + ', '.join(str(p) for p in prot) + ')')
        if not z.present:
            continue
        frac = z.managed / sum(x.managed for x in m['zones'] if x.node == z.node)
        nv = node_vals[z.node]
        zv = {'nr_free_pages': z.free,
              'nr_zone_inactive_anon': int(nv['nr_inactive_anon'] * frac),
              'nr_zone_active_anon': int(nv['nr_active_anon'] * frac),
              'nr_zone_inactive_file': int(nv['nr_inactive_file'] * frac),
              'nr_zone_active_file': int(nv['nr_active_file'] * frac),
              'nr_zone_unevictable': int(nv['nr_unevictable'] * frac),
              'nr_zone_write_pending': int((nv['nr_dirty'] + nv['nr_writeback']) * frac),
              'nr_mlock': int(nv.get('nr_mlock', 0) * frac)}
        if z.name == 'DMA':
            for k in list(zv):
                if k != 'nr_free_pages':
                    zv[k] = 0
        for name in zone_stat_names(m):
            out.append(f'      {name:<12} {zv.get(name, 0)}')
        if m.get('numa_config'):
            hit = int(m['rates']['alloc'] * m['uptime'] * frac) if z.name != 'DMA' else rng.randint(1, 4)
            for name in numa_stat_names():
                val = {'numa_hit': hit, 'numa_local': hit}.get(name, 0)
                if len(m['nodes']) > 1 and name in ('numa_miss', 'numa_foreign', 'numa_other') and z.name == 'Normal':
                    val = hit // 200
                out.append(f'      {name:<12} {val}')
        out.append('  pagesets')
        for cpu in m['online_cpus']:
            high = 4 if z.name == 'DMA' else (min(z.managed // 1000, 600) if ver_ge(v, 6, 7) else 186)
            batch = 1 if z.name == 'DMA' else 63
            out.append(f'    cpu: {cpu}')
            if ver_ge(v, 6, 7):
                out.append(f'              count:    {rng.randint(0, high) if z.name != "DMA" else 0}')
                out.append(f'              high:     {high}')
                out.append(f'              batch:    {batch}')
                out.append(f'              high_min: {high}')
                out.append(f'              high_max: {high * 8 if z.name != "DMA" else 30}')
            else:
                out.append(f'              count: {rng.randint(0, high) if z.name != "DMA" else 0}')
                out.append(f'              high:  {high if z.name != "DMA" else 0}')
                out.append(f'              batch: {batch}')
            out.append(f'  vm stats threshold: {10 if z.name == "DMA" else 64 if len(m["online_cpus"]) >= 16 else 40}')
        out.append(f'  node_unreclaimable:  0')
        out.append(f'  start_pfn:           {z.start_pfn}')
    return '\n'.join(out) + '\n'


def buddyinfo(m, rng):
    orders = m.get('max_order', 11)
    lines = []
    for z in m['zones']:
        if not z.present:
            continue
        c = buddy(z.free, orders, rng, m.get('frag', 0.5))
        z.buddy = c
        assert sum(n << o for o, n in enumerate(c)) == z.free
        lines.append(f'Node {z.node}, zone {z.name:>8} ' + ''.join(f'{n:>7}' for n in c) + ' ')
    return '\n'.join(lines) + '\n'


# ------------------------------------------------------------------- probe

class VMA:
    def __init__(self, start, end, perms, offset, dev, inode, path, kind, rss_kb=0, anon_kb=0,
                 shared_clean=0, private_clean=0, private_dirty=0, shared_dirty=0, anon_huge=0,
                 referenced=None, pss=None, flags='', thp_eligible=0, swap=0):
        self.start, self.end, self.perms, self.offset = start, end, perms, offset
        self.dev, self.inode, self.path, self.kind = dev, inode, path, kind
        self.rss, self.anon = rss_kb, anon_kb
        self.shared_clean, self.private_clean = shared_clean, private_clean
        self.private_dirty, self.shared_dirty = private_dirty, shared_dirty
        self.anon_huge = anon_huge
        self.referenced = rss_kb if referenced is None else referenced
        self.pss = private_clean + private_dirty + (shared_clean + shared_dirty) // 2 if pss is None else pss
        self.flags, self.thp_eligible, self.swap = flags, thp_eligible, swap
        assert self.rss == shared_clean + shared_dirty + private_clean + private_dirty, (path, hex(start))

    @property
    def size_kb(self):
        return (self.end - self.start) // KB


def maps_line(v):
    s = f'{v.start:08x}-{v.end:08x} {v.perms} {v.offset:08x} {v.dev} {v.inode}'
    if v.path:
        return f'{s:<73}{v.path}'
    return s + ' '


def smaps(m, vmas):
    ver, x86 = m['kver'], m['arch'] == 'x86_64'
    out = []
    for v in vmas:
        out.append(maps_line(v))
        f = [('Size', v.size_kb), ('KernelPageSize', 4), ('MMUPageSize', 4), ('Rss', v.rss), ('Pss', v.pss)]
        if ver_ge(ver, 6, 0):
            f.append(('Pss_Dirty', v.private_dirty + v.shared_dirty // 2))
        f += [('Shared_Clean', v.shared_clean), ('Shared_Dirty', v.shared_dirty),
              ('Private_Clean', v.private_clean), ('Private_Dirty', v.private_dirty),
              ('Referenced', v.referenced), ('Anonymous', v.anon)]
        if ver_ge(ver, 6, 6):
            f.append(('KSM', 0))
        f += [('LazyFree', 0), ('AnonHugePages', v.anon_huge), ('ShmemPmdMapped', 0)]
        if ver_ge(ver, 5, 4):
            f.append(('FilePmdMapped', 0))
        f += [('Shared_Hugetlb', 0), ('Private_Hugetlb', 0), ('Swap', v.swap), ('SwapPss', v.swap), ('Locked', 0)]
        for k, val in f:
            out.append(f'{k + ":":<16}{val:>8} kB')
        if ver_ge(ver, 5, 0):
            out.append(f'THPeligible:    {v.thp_eligible:>8}')
        if x86 and m.get('pku'):
            out.append(f'ProtectionKey:  {0:>8}')
        out.append(f'VmFlags: {v.flags} ')
    return '\n'.join(out) + '\n'


def write_json(root, path, obj):
    root.w(path, json.dumps(obj, indent=2) + '\n')
