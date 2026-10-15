"""Writes one fixture root from a machine description (see machines.py)."""
import json
import random
from datetime import datetime, timezone

from gen_sched import write_sched
import gen_net
import gen_storage

from genlib import (KB, Root, VMA, Zone, all_zones, buddyinfo, build_meminfo, format_meminfo, kb,
                    node_meminfo, node_stat_names, protections, smaps, ver_ge, watermarks, write_json,
                    zone_stat_names, zoneinfo, maps_line)


def ts(s):
    return datetime.strptime(s, '%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=timezone.utc)


def cpulist(cpus):
    cpus = sorted(cpus)
    out, i = [], 0
    while i < len(cpus):
        j = i
        while j + 1 < len(cpus) and cpus[j + 1] == cpus[j] + 1:
            j += 1
        out.append(str(cpus[i]) if i == j else f'{cpus[i]}-{cpus[j]}')
        i = j + 1
    return ','.join(out)


def generate(base, m):
    rng = random.Random(m['name'])
    r = Root(base, m['name'])
    now = ts(m['now'])
    m['online_cpus'] = [c['cpu'] for c in m['cpus']]
    write_meta(r, m)
    write_identity(r, m, now)
    write_cpu(r, m)
    mi = write_memory(r, m, rng)
    write_cgroup(r, m)
    write_virt(r, m)
    write_block(r, m)
    write_mounts(r, m)
    write_net(r, m)
    gen_net.generate(r, m)
    if m.get('probe', True):
        write_probe(r, m, mi, rng)
    write_sched(r, m)
    gen_storage.generate(r, m)
    r.w('README.md', m['readme'].strip() + '\n')
    return r


# ------------------------------------------------------------------ meta

def write_meta(r, m):
    meta = {
        'description': m['description'],
        'now': m['now'],
        'uname': {'machine': m['arch'], 'release': m['release']},
        'page_size': m['page_size'],
        'deny': m.get('deny', []),
        'env': m.get('env', {}),
        'commands': m['commands'],
    }
    write_json(r, '_meta.json', meta)


# -------------------------------------------------------------- identity

def write_identity(r, m, now):
    for path in ('/etc/os-release', '/usr/lib/os-release'):
        if path in m.get('os_release_paths', ['/etc/os-release']):
            r.w(path, m['os_release'])
    r.w('/proc/sys/kernel/hostname', m['hostname'] + '\n')
    r.w('/proc/sys/kernel/osrelease', m['release'] + '\n')
    r.w('/proc/version', m['version'] + '\n')
    r.w('/proc/sys/kernel/randomize_va_space', '2\n')
    up = m['uptime']
    btime = int(now.timestamp() - up)
    idle = up * len(m['cpus']) * m.get('idle_frac', 0.93)
    r.w('/proc/uptime', f'{up:.2f} {idle:.2f}\n')
    rng = random.Random(m['name'] + 'stat')
    hz = 100
    lines = []
    tot = [0] * 10
    per = []
    for c in m['cpus']:
        ticks = int(up * hz)
        idle_t = int(ticks * m.get('idle_frac', 0.93) * rng.uniform(0.97, 1.0))
        busy = ticks - idle_t
        row = [int(busy * 0.62), int(busy * 0.01), int(busy * 0.27), idle_t, int(ticks * 0.004),
               int(busy * 0.03), int(busy * 0.02), 0, 0, 0]
        per.append(row)
        tot = [a + b for a, b in zip(tot, row)]
    lines.append('cpu  ' + ' '.join(map(str, tot)))
    for c, row in zip(m['cpus'], per):
        lines.append(f'cpu{c["cpu"]} ' + ' '.join(map(str, row)))
    intr = int(up * 4100 * len(m['cpus']) / 8)
    lines.append(f'intr {intr} ' + ' '.join(str(rng.choice([0, 0, 0, rng.randint(1, 900000)])) for _ in range(64)))
    lines.append(f'ctxt {int(up * 9800 * len(m["cpus"]) / 8)}')
    lines.append(f'btime {btime}')
    lines.append(f'processes {int(up * 3.1) + 1200}')
    lines.append('procs_running 2')
    lines.append('procs_blocked 0')
    sirq = int(up * 1500)
    lines.append(f'softirq {sirq} ' + ' '.join(str(int(sirq * f)) for f in (0.0002, 0.31, 0.0001, 0.02, 0.06, 0, 0.0004, 0.29, 0, 0.31)))
    r.w('/proc/stat', '\n'.join(lines) + '\n')
    r.w('/proc/1/comm', m['pid1'] + '\n')
    r.w('/proc/1/cgroup', m['pid1_cgroup'])
    if m.get('systemd'):
        r.mkdir('/run/systemd/system')


# ------------------------------------------------------------------- cpu

def write_cpu(r, m):
    cpus = m['cpus']
    blocks = []
    for c in cpus:
        blocks.append(m['cpuinfo_block'](c))
    text = '\n\n'.join(blocks) + '\n\n'
    if m.get('cpuinfo_trailer'):
        text += m['cpuinfo_trailer']
    r.w('/proc/cpuinfo', text)
    sysd = '/sys/devices/system/cpu'
    ids = [c['cpu'] for c in cpus]
    r.w(f'{sysd}/online', cpulist(ids) + '\n')
    r.w(f'{sysd}/possible', cpulist(m.get('possible', ids)) + '\n')
    r.w(f'{sysd}/present', cpulist(ids) + '\n')
    r.w(f'{sysd}/kernel_max', f'{m.get("kernel_max", 8191)}\n')
    for c in cpus:
        sib = [d['cpu'] for d in cpus if d['pkg'] == c['pkg'] and d['core'] == c['core']]
        pkg = [d['cpu'] for d in cpus if d['pkg'] == c['pkg']]
        t = f'{sysd}/cpu{c["cpu"]}/topology'
        r.w(f'{t}/physical_package_id', f'{c["pkg"]}\n')
        r.w(f'{t}/core_id', f'{c["core"]}\n')
        r.w(f'{t}/thread_siblings_list', cpulist(sib) + '\n')
        r.w(f'{t}/core_siblings_list', cpulist(pkg) + '\n')
        if ver_ge(m['kver'], 5, 4):
            r.w(f'{t}/core_cpus_list', cpulist(sib) + '\n')
            r.w(f'{t}/package_cpus_list', cpulist(pkg) + '\n')
    for i, cache in enumerate(m.get('caches', [])):
        d = f'{sysd}/cpu0/cache/index{i}'
        for k in ('level', 'type', 'size', 'coherency_line_size', 'ways_of_associativity', 'shared_cpu_list'):
            if k in cache:
                r.w(f'{d}/{k}', f'{cache[k]}\n')
        if 'number_of_sets' not in cache and 'ways_of_associativity' in cache:
            size = int(cache['size'][:-1]) * 1024
            r.w(f'{d}/number_of_sets', f'{size // cache["coherency_line_size"] // cache["ways_of_associativity"]}\n')


# ---------------------------------------------------------------- memory

def write_memory(r, m, rng):
    ps = m['page_size']
    watermarks(m)
    protections(m)
    mi = build_meminfo(m, rng)
    r.w('/proc/meminfo', format_meminfo(m, mi))

    # Node-level page counters, shared by zoneinfo and vmstat.
    nodes = m['nodes']
    node_vals = {}
    for n in nodes:
        frac = sum(z.managed for z in m['zones'] if z.node == n) / sum(z.managed for z in m['zones'])
        pg = lambda k: int(mi[k] * frac) // (ps // KB)
        up = m['uptime']
        rate = m['rates']
        nv = {
            'nr_inactive_anon': pg('Inactive(anon)'), 'nr_active_anon': pg('Active(anon)'),
            'nr_inactive_file': pg('Inactive(file)'), 'nr_active_file': pg('Active(file)'),
            'nr_unevictable': pg('Unevictable'), 'nr_slab_reclaimable': pg('SReclaimable'),
            'nr_slab_unreclaimable': pg('SUnreclaim'), 'nr_anon_pages': pg('AnonPages'),
            'nr_mapped': pg('Mapped'), 'nr_file_pages': pg('Cached') + pg('Buffers') + pg('SwapCached'),
            'nr_dirty': pg('Dirty'), 'nr_writeback': pg('Writeback'), 'nr_shmem': pg('Shmem'),
            'nr_anon_transparent_hugepages': int(mi['AnonHugePages'] * frac) // 2048,
            'nr_kernel_stack': int(mi['KernelStack'] * frac),  # in kB, as the kernel counts it
            'nr_page_table_pages': pg('PageTables'), 'nr_sec_page_table_pages': pg('SecPageTables'),
            'nr_swapcached': pg('SwapCached'), 'nr_mlock': pg('Mlocked'),
            'nr_file_hugepages': int(mi['FileHugePages'] * frac) // 2048,
            'nr_file_pmdmapped': int(mi['FilePmdMapped'] * frac) // 2048,
            'workingset_nodes': int(rng.uniform(2000, 9000) * frac),
            'nr_dirtied': int(up * rate['dirtied'] * frac), 'nr_written': int(up * rate['dirtied'] * 0.96 * frac),
            'nr_vmscan_write': int(up * rate.get('swapout', 0) * frac),
            'nr_vmscan_immediate_reclaim': int(rng.uniform(0, 400) * frac),
            'nr_foll_pin_acquired': int(up * 900 * frac),
        }
        nv['nr_foll_pin_released'] = nv['nr_foll_pin_acquired'] - rng.randint(0, 9000)
        scan = int(up * rate['scan'] * frac)
        steal = int(scan * 0.86)
        nv.update({
            'workingset_refault_anon': int(up * rate.get('swapin', 0) * 0.8 * frac),
            'workingset_refault_file': int(scan * 0.21), 'workingset_activate_anon': int(up * rate.get('swapin', 0) * 0.2 * frac),
            'workingset_activate_file': int(scan * 0.05), 'workingset_restore_anon': int(up * rate.get('swapin', 0) * 0.05 * frac),
            'workingset_restore_file': int(scan * 0.03), 'workingset_nodereclaim': int(scan * 0.01),
            'workingset_refault': int(scan * 0.2), 'workingset_activate': int(scan * 0.05),
            'pgsteal_kswapd': int(steal * 0.93), 'pgsteal_direct': steal - int(steal * 0.93),
            'pgscan_kswapd': int(scan * 0.92), 'pgscan_direct': scan - int(scan * 0.92),
            'pgsteal_anon': int(up * rate.get('swapout', 0) * frac), 'pgscan_anon': int(up * rate.get('swapout', 0) * 2.1 * frac),
            'pgrefill': int(scan * 1.07), 'pgsteal_khugepaged': 0, 'pgscan_khugepaged': 0,
        })
        nv['pgsteal_file'] = steal - nv['pgsteal_anon'] if steal > nv['pgsteal_anon'] else 0
        nv['pgscan_file'] = scan - nv['pgscan_anon'] if scan > nv['pgscan_anon'] else 0
        node_vals[n] = nv

    r.w('/proc/zoneinfo', zoneinfo(m, node_vals, rng))
    r.w('/proc/buddyinfo', buddyinfo(m, rng))
    r.w('/proc/vmstat', vmstat(m, mi, node_vals, rng))

    # swap
    lines = ['Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority']
    for s in m.get('swaps', []):
        lines.append(f'{s["name"]:<40}{s["type"]}\t{s["size_kb"]}\t\t{s["used_kb"]}\t\t{s["prio"]}')
    r.w('/proc/swaps', '\n'.join(lines) + '\n')

    if m.get('psi'):
        p = m['psi']
        r.w('/proc/pressure/memory',
            f'some avg10={p[0]:.2f} avg60={p[1]:.2f} avg300={p[2]:.2f} total={p[3]}\n'
            f'full avg10={p[4]:.2f} avg60={p[5]:.2f} avg300={p[6]:.2f} total={p[7]}\n')
        r.w('/proc/pressure/cpu',
            f'some avg10=0.31 avg60=0.42 avg300=0.37 total={int(m["uptime"] * 3100)}\n' +
            (f'full avg10=0.00 avg60=0.00 avg300=0.00 total=0\n' if ver_ge(m['kver'], 5, 13) else ''))
        r.w('/proc/pressure/io',
            f'some avg10=0.00 avg60=0.04 avg300=0.06 total={int(m["uptime"] * 1800)}\n'
            f'full avg10=0.00 avg60=0.03 avg300=0.05 total={int(m["uptime"] * 1500)}\n')

    sysctl = {'swappiness': m.get('swappiness', 60), 'overcommit_memory': m.get('overcommit_memory', 0),
              'overcommit_ratio': m['overcommit_ratio'], 'min_free_kbytes': m['min_free_kbytes'],
              'watermark_scale_factor': m['watermark_scale_factor'], 'dirty_ratio': m.get('dirty_ratio', 20),
              'dirty_background_ratio': m.get('dirty_background_ratio', 10),
              'max_map_count': m.get('max_map_count', 65530), 'page-cluster': 3,
              'vfs_cache_pressure': 100, 'nr_hugepages': m.get('hugepages', {}).get(2048, (0, 0))[0],
              'dirty_bytes': 0, 'dirty_background_bytes': 0, 'dirty_expire_centisecs': 3000,
              'dirty_writeback_centisecs': 500, 'zone_reclaim_mode': 0, 'oom_kill_allocating_task': 0,
              'panic_on_oom': 0, 'admin_reserve_kbytes': 8192, 'user_reserve_kbytes': min(131072, mi['MemTotal'] // 32)}
    for k, v in sysctl.items():
        r.w(f'/proc/sys/vm/{k}', f'{v}\n')

    thp = m.get('thp')
    if thp:
        d = '/sys/kernel/mm/transparent_hugepage'
        r.w(f'{d}/enabled', ' '.join(f'[{x}]' if x == thp else x for x in ('always', 'madvise', 'never')) + '\n')
        defrag = ['always', 'defer', 'defer+madvise', 'madvise', 'never']
        r.w(f'{d}/defrag', ' '.join(f'[{x}]' if x == m.get('thp_defrag', 'madvise') else x for x in defrag) + '\n')
        shm = ['always', 'within_size', 'advise', 'never', 'deny', 'force']
        r.w(f'{d}/shmem_enabled', ' '.join(f'[{x}]' if x == 'never' else x for x in shm) + '\n')
        r.w(f'{d}/hpage_pmd_size', f'{2 * 1024 * 1024}\n')
        r.w(f'{d}/use_zero_page', '1\n')
        r.w(f'{d}/khugepaged/defrag', '1\n')
        r.w(f'{d}/khugepaged/max_ptes_none', '511\n')
        r.w(f'{d}/khugepaged/pages_to_scan', '4096\n')
        r.w(f'{d}/khugepaged/scan_sleep_millisecs', '10000\n')
    if m.get('lru_gen'):
        r.w('/sys/kernel/mm/lru_gen/enabled', m['lru_gen'] + '\n')
        r.w('/sys/kernel/mm/lru_gen/min_ttl_ms', '0\n')
    if m.get('zswap') is not None:
        z = '/sys/module/zswap/parameters'
        r.w(f'{z}/enabled', ('Y' if m['zswap'] else 'N') + '\n')
        r.w(f'{z}/compressor', m.get('zswap_compressor', 'zstd') + '\n')
        r.w(f'{z}/max_pool_percent', '20\n')
        if not ver_ge(m['kver'], 6, 9):
            r.w(f'{z}/zpool', 'zsmalloc\n' if ver_ge(m['kver'], 6, 0) else 'zbud\n')
    if m.get('numa_sysfs', True):
        nd = '/sys/devices/system/node'
        r.w(f'{nd}/online', cpulist(m['nodes']) + '\n')
        r.w(f'{nd}/possible', cpulist(m['nodes']) + '\n')
        r.w(f'{nd}/has_memory', cpulist(m['nodes']) + '\n')
        r.w(f'{nd}/has_normal_memory', cpulist(m['nodes']) + '\n')
        r.w(f'{nd}/has_cpu', cpulist(m['nodes']) + '\n')
        for n in m['nodes']:
            r.w(f'{nd}/node{n}/meminfo', node_meminfo(m, mi, n))
            r.w(f'{nd}/node{n}/cpulist', cpulist(c['cpu'] for c in m['cpus'] if c.get('node', 0) == n) + '\n')
            dist = ' '.join('10' if a == n else '21' for a in m['nodes'])
            r.w(f'{nd}/node{n}/distance', dist + '\n')
            for size_kb, (num, free) in m.get('hugepages', {}).items():
                hd = f'{nd}/node{n}/hugepages/hugepages-{size_kb}kB'
                r.w(f'{hd}/nr_hugepages', f'{num // len(m["nodes"])}\n')
                r.w(f'{hd}/free_hugepages', f'{free // len(m["nodes"])}\n')
                r.w(f'{hd}/surplus_hugepages', '0\n')
    for size_kb, (num, free) in m.get('hugepages', {}).items():
        hd = f'/sys/kernel/mm/hugepages/hugepages-{size_kb}kB'
        r.w(f'{hd}/nr_hugepages', f'{num}\n')
        r.w(f'{hd}/free_hugepages', f'{free}\n')
        r.w(f'{hd}/resv_hugepages', '0\n')
        r.w(f'{hd}/surplus_hugepages', '0\n')
        r.w(f'{hd}/nr_overcommit_hugepages', '0\n')
        r.w(f'{hd}/nr_hugepages_mempolicy', f'{num}\n')
    if m.get('memory_block_size'):
        r.w('/sys/devices/system/memory/block_size_bytes', f'{m["memory_block_size"]:x}\n')
    for z in m.get('zram', []):
        r.w(f'/sys/block/{z["name"]}/disksize', f'{z["disksize"]}\n')
    m['_mi'] = mi
    return mi


def vmstat(m, mi, node_vals, rng):
    v, ps = m['kver'], m['page_size']
    up, rate = m['uptime'], m['rates']
    total = {}
    for nv in node_vals.values():
        for k, val in nv.items():
            total[k] = total.get(k, 0) + val
    out = []
    zv = {'nr_free_pages': sum(z.free for z in m['zones']),
          'nr_zone_inactive_anon': total['nr_inactive_anon'], 'nr_zone_active_anon': total['nr_active_anon'],
          'nr_zone_inactive_file': total['nr_inactive_file'], 'nr_zone_active_file': total['nr_active_file'],
          'nr_zone_unevictable': total['nr_unevictable'],
          'nr_zone_write_pending': total['nr_dirty'] + total['nr_writeback'], 'nr_mlock': total['nr_mlock'],
          'nr_page_table_pages': total['nr_page_table_pages'], 'nr_kernel_stack': total['nr_kernel_stack'],
          'nr_zspages': mi.get('Zswap', 0) // (ps // KB) // 3, 'nr_free_cma': m.get('cma_free', 0) // (ps // KB)}
    for k in zone_stat_names(m):
        out.append((k, zv.get(k, 0)))
    if m.get('numa_config'):
        hit = int(up * rate['alloc'])
        miss = hit // 300 if len(m['nodes']) > 1 else 0
        out += [('numa_hit', hit), ('numa_miss', miss), ('numa_foreign', miss), ('numa_interleave', 3121),
                ('numa_local', hit - miss), ('numa_other', miss)]
    for k in node_stat_names(v):
        if k == 'nr_kernel_stack' or k == 'nr_page_table_pages':
            if not ver_ge(v, 5, 9):
                continue
        out.append((k, total.get(k, 0)))
    # Dirty thresholds from dirty_ratio applied to "dirtyable" memory
    # (free + file LRU, minus reserves).
    dirtyable = (mi['MemFree'] + mi['Active(file)'] + mi['Inactive(file)']) // (ps // KB)
    out.append(('nr_dirty_threshold', dirtyable * m.get('dirty_ratio', 20) // 100))
    out.append(('nr_dirty_background_threshold', dirtyable * m.get('dirty_background_ratio', 10) // 100))
    if ver_ge(v, 6, 11):
        memmap = sum(z.present for z in m['zones']) * 64 // ps
        out += [('nr_memmap_pages', 0), ('nr_memmap_boot_pages', memmap)]
    zones = [z.lower() for z in m['zone_types']]
    pgfault = int(up * rate['fault'])
    alloc = int(up * rate['alloc'])
    ev = [('pgpgin', int(up * rate['pgpgin'])), ('pgpgout', int(up * rate['pgpgin'] * 1.6)),
          ('pswpin', int(up * rate.get('swapin', 0))), ('pswpout', int(up * rate.get('swapout', 0)))]
    zshare = {'dma': 0.0000, 'dma32': 0.06, 'normal': 0.94, 'movable': 0, 'device': 0}
    if 'Normal' not in {z.name for z in m['zones'] if z.present}:
        zshare = {'dma': 0.0001, 'dma32': 0.72, 'normal': 0, 'movable': 0, 'device': 0}
        if m['arch'] != 'x86_64':
            zshare = {'dma': 0.25, 'dma32': 0.75, 'normal': 0, 'movable': 0, 'device': 0}
    ev += [(f'pgalloc_{z}', int(alloc * zshare[z]) + (rng.randint(1, 50) if z == 'dma' else 0)) for z in zones]
    stall = int(up * rate.get('stall', 0))
    ev += [(f'allocstall_{z}', stall if z == ('normal' if zshare['normal'] else 'dma32') else 0) for z in zones]
    ev += [(f'pgskip_{z}', 0) for z in zones]
    ev += [('pgfree', alloc + int(up * 40)), ('pgactivate', int(up * rate['scan'] * 0.4)),
           ('pgdeactivate', int(up * rate['scan'] * 0.3)), ('pglazyfree', int(up * 12)),
           ('pgfault', pgfault), ('pgmajfault', int(up * rate['majfault'])), ('pglazyfreed', 0)]
    if not ver_ge(v, 6, 7):
        ev.append(('pgrefill', total['pgrefill']))
    if ver_ge(v, 5, 10):
        ev.append(('pgreuse', pgfault // 9))
    if not ver_ge(v, 6, 7):
        ev += [('pgsteal_kswapd', total['pgsteal_kswapd']), ('pgsteal_direct', total['pgsteal_direct'])]
        if ver_ge(v, 6, 2):
            ev.append(('pgsteal_khugepaged', 0))
        ev += [('pgscan_kswapd', total['pgscan_kswapd']), ('pgscan_direct', total['pgscan_direct'])]
        if ver_ge(v, 6, 2):
            ev.append(('pgscan_khugepaged', 0))
    ev.append(('pgscan_direct_throttle', 0))
    if not ver_ge(v, 6, 7) and ver_ge(v, 5, 8):
        ev += [('pgscan_anon', total['pgscan_anon']), ('pgscan_file', total['pgscan_file']),
               ('pgsteal_anon', total['pgsteal_anon']), ('pgsteal_file', total['pgsteal_file'])]
    if m.get('numa_config'):
        ev.append(('zone_reclaim_failed', 0))
    ev += [('pginodesteal', 0), ('slabs_scanned', int(up * rate['scan'] * 0.2)), ('kswapd_inodesteal', int(up * 0.4)),
           ('kswapd_low_wmark_hit_quickly', int(up * rate['scan'] / 40000)),
           ('kswapd_high_wmark_hit_quickly', int(up * rate['scan'] / 90000)),
           ('pageoutrun', int(up * rate['scan'] / 8000)), ('pgrotated', int(up * 0.9)),
           ('drop_pagecache', 0), ('drop_slab', 0), ('oom_kill', m.get('oom_kills', 0))]
    if m.get('numa_config'):
        nb = m.get('numa_balancing', 0)
        ev += [('numa_pte_updates', int(up * 30 * nb)), ('numa_huge_pte_updates', int(up * 0.1 * nb)),
               ('numa_hint_faults', int(up * 25 * nb)), ('numa_hint_faults_local', int(up * 23 * nb)),
               ('numa_pages_migrated', int(up * 1.2 * nb))]
    ev += [('pgmigrate_success', int(up * 9)), ('pgmigrate_fail', int(up * 0.02))]
    if ver_ge(v, 5, 9):
        ev += [('thp_migration_success', 0), ('thp_migration_fail', 0), ('thp_migration_split', 0)]
    ev += [('compact_migrate_scanned', int(up * 80)), ('compact_free_scanned', int(up * 210)),
           ('compact_isolated', int(up * 18)), ('compact_stall', int(up * 0.002)), ('compact_fail', int(up * 0.0012)),
           ('compact_success', int(up * 0.0008)), ('compact_daemon_wake', int(up * 0.01)),
           ('compact_daemon_migrate_scanned', int(up * 3)), ('compact_daemon_free_scanned', int(up * 11)),
           ('htlb_buddy_alloc_success', sum(n for n, _ in m.get('hugepages', {}).values())),
           ('htlb_buddy_alloc_fail', 0)]
    if m.get('cma') and ver_ge(v, 5, 0):
        ev += [('cma_alloc_success', int(up * 0.01)), ('cma_alloc_fail', 0)]
    ev += [('unevictable_pgs_culled', int(up * 2)), ('unevictable_pgs_scanned', 0),
           ('unevictable_pgs_rescued', int(up * 1.4)), ('unevictable_pgs_mlocked', int(up * 0.9)),
           ('unevictable_pgs_munlocked', int(up * 0.85)), ('unevictable_pgs_cleared', 0),
           ('unevictable_pgs_stranded', 0)]
    thp_on = m.get('thp') != 'never'
    thp_rate = {'always': 0.4, 'madvise': 0.01, 'never': 0}.get(m.get('thp'), 0)
    ev += [('thp_fault_alloc', int(up * thp_rate)), ('thp_fault_fallback', int(up * thp_rate * 0.03))]
    if ver_ge(v, 5, 8):
        ev.append(('thp_fault_fallback_charge', 0))
    ev += [('thp_collapse_alloc', int(up * thp_rate * 0.05)), ('thp_collapse_alloc_failed', 0),
           ('thp_file_alloc', 0)]
    if ver_ge(v, 5, 8):
        ev += [('thp_file_fallback', 0), ('thp_file_fallback_charge', 0)]
    ev += [('thp_file_mapped', 0), ('thp_split_page', int(up * thp_rate * 0.02)), ('thp_split_page_failed', 0),
           ('thp_deferred_split_page', int(up * thp_rate * 0.1))]
    if ver_ge(v, 6, 12):
        ev.append(('thp_underused_split_page', int(up * thp_rate * 0.01)))
    ev.append(('thp_split_pmd', int(up * thp_rate * 0.12)))
    if ver_ge(v, 6, 0):
        ev += [('thp_scan_exceed_none_pte', int(up * thp_rate * 0.01)), ('thp_scan_exceed_swap_pte', 0),
               ('thp_scan_exceed_share_pte', 0)]
    ev += [('thp_split_pud', 0), ('thp_zero_page_alloc', 1 if thp_on else 0), ('thp_zero_page_alloc_failed', 0),
           ('thp_swpout', 0), ('thp_swpout_fallback', 0), ('balloon_inflate', 0), ('balloon_deflate', 0),
           ('balloon_migrate', 0), ('swap_ra', int(up * rate.get('swapin', 0) * 0.3)),
           ('swap_ra_hit', int(up * rate.get('swapin', 0) * 0.2))]
    if ver_ge(v, 6, 12):
        ev += [('swpin_zero', 0), ('swpout_zero', int(up * rate.get('swapout', 0) * 0.02))]
    if ver_ge(v, 6, 4):
        ev += [('ksm_swpin_copy', 0), ('cow_ksm', 0)]
    if ver_ge(v, 5, 19) and m.get('zswap') is not None:
        zs = 1 if m.get('zswap') else 0
        ev += [('zswpin', int(up * rate.get('swapin', 0) * 3 * zs)), ('zswpout', int(up * rate.get('swapout', 0) * 4 * zs))]
        if ver_ge(v, 6, 8):
            ev.append(('zswpwb', int(up * rate.get('swapout', 0) * zs)))
    if m['arch'] == 'x86_64' and ver_ge(v, 5, 0):
        ev += [('direct_map_level2_splits', 211), ('direct_map_level3_splits', 3)]
    if ver_ge(v, 5, 8):
        ev.append(('nr_unstable', 0))  # kept at the end for compatibility since 5.8
    out += ev
    return ''.join(f'{k} {val}\n' for k, val in out)


# ---------------------------------------------------------------- cgroup

def write_cgroup(r, m):
    cg = m['cgroup']
    r.w('/proc/self/cgroup', cg['self'])
    if cg['version'] == 'v2':
        base = '/sys/fs/cgroup'
        r.w(f'{base}/cgroup.controllers', cg.get('root_controllers', 'cpuset cpu io memory hugetlb pids rdma misc') + '\n')
        d = base + cg['path'].rstrip('/')
        if cg['path'] != '/':
            r.w(f'{d}/cgroup.controllers', cg.get('leaf_controllers', 'memory pids') + '\n')
        if cg.get('memory', True):
            f = cg['files']
            for name in ('memory.current', 'memory.max', 'memory.high', 'memory.low', 'memory.min',
                         'memory.swap.current', 'memory.swap.max'):
                r.w(f'{d}/{name}', f'{f[name]}\n')
            if ver_ge(m['kver'], 5, 19):
                r.w(f'{d}/memory.peak', f'{f["memory.peak"]}\n')
            ev = f['events']
            keys = ['low', 'high', 'max', 'oom', 'oom_kill']
            if ver_ge(m['kver'], 5, 17):
                keys.append('oom_group_kill')
            r.w(f'{d}/memory.events', ''.join(f'{k} {ev.get(k, 0)}\n' for k in keys))
            r.w(f'{d}/memory.events.local', ''.join(f'{k} {ev.get(k, 0)}\n' for k in keys))
            r.w(f'{d}/memory.stat', f['stat'])
            if m.get('psi'):
                r.w(f'{d}/memory.pressure', 'some avg10=0.00 avg60=0.00 avg300=0.00 total=1834\n'
                                            'full avg10=0.00 avg60=0.00 avg300=0.00 total=1720\n')
    else:
        d = '/sys/fs/cgroup/memory' + cg['path'].rstrip('/')
        for k, v in cg['files'].items():
            r.w(f'{d}/{k}', v if v.endswith('\n') else v + '\n')


# ------------------------------------------------------------------ virt

def write_virt(r, m):
    if m.get('dmi'):
        r.w('/sys/class/dmi/id/sys_vendor', m['dmi'][0] + '\n')
        r.w('/sys/class/dmi/id/product_name', m['dmi'][1] + '\n')
        r.w('/sys/class/dmi/id/bios_vendor', m['dmi'][2] + '\n')
    if m.get('dockerenv'):
        r.w('/.dockerenv', '')
    if m.get('containerenv'):
        r.w('/run/.containerenv', m['containerenv'])
    if m.get('dt_model'):
        r.w('/proc/device-tree/model', m['dt_model'] + '\x00')
        r.w('/proc/device-tree/compatible', m['dt_compatible'])


# ----------------------------------------------------------------- block

def write_block(r, m):
    for d in m.get('block', []):
        b = f'/sys/block/{d["name"]}'
        r.w(f'{b}/size', f'{d["sectors"]}\n')
        r.w(f'{b}/removable', f'{d.get("removable", 0)}\n')
        r.w(f'{b}/ro', f'{d.get("ro", 0)}\n')
        r.w(f'{b}/queue/rotational', f'{d.get("rotational", 0)}\n')
        r.w(f'{b}/queue/logical_block_size', f'{d.get("lbs", 512)}\n')
        r.w(f'{b}/queue/scheduler', d.get('scheduler', '[none] mq-deadline kyber bfq') + '\n')
    for path, content in m.get('cmd_files', {}).items():
        r.w(path, content)


def lsblk_json(devs):
    """util-linux 2.3x style: three-space indent, children nested."""
    def dev(d, ind):
        p = ' ' * ind
        fs = 'null' if d.get('fstype') is None else json.dumps(d['fstype'])
        s = (f'{p}{{\n{p}   "name": {json.dumps(d["name"])},\n{p}   "type": {json.dumps(d["type"])},\n'
             f'{p}   "size": {d["size"]},\n{p}   "fstype": {fs}')
        if d.get('children'):
            s += f',\n{p}   "children": [\n' + ',\n'.join(dev(c, ind + 6) for c in d['children']) + f'\n{p}   ]'
        return s + f'\n{p}}}'
    return '{\n   "blockdevices": [\n' + ',\n'.join(dev(d, 6) for d in devs) + '\n   ]\n}\n'


# ---------------------------------------------------------------- mounts

def write_mounts(r, m):
    lines = []
    for i, mnt in enumerate(m['mounts']):
        mid = 20 + i
        parent = 1 if i == 0 else mnt.get('parent', 20)
        opt = mnt.get('opts', 'rw,relatime')
        shared = mnt.get('shared', f'shared:{i + 1}' if m.get('shared_mounts', True) else '')
        fields = [str(mid), str(parent), mnt['dev'], mnt.get('root', '/'), mnt['mp'], opt]
        if shared:
            fields.append(shared)
        fields += ['-', mnt['fs'], mnt['src'], mnt.get('super', 'rw')]
        lines.append(' '.join(fields))
    r.w('/proc/self/mountinfo', '\n'.join(lines) + '\n')


# ------------------------------------------------------------------- net

def write_net(r, m):
    for n in m.get('net', []):
        d = f'/sys/class/net/{n["name"]}'
        r.w(f'{d}/type', f'{n["type"]}\n')
        r.w(f'{d}/operstate', n['operstate'] + '\n')
        r.w(f'{d}/mtu', f'{n.get("mtu", 1500)}\n')
        r.w(f'{d}/carrier', ('1' if n['operstate'] == 'up' else '0') + '\n')
        for sub in n.get('dirs', []):
            r.mkdir(f'{d}/{sub}')
    if m.get('bonding_masters'):
        r.w('/sys/class/net/bonding_masters', '\n')


# ----------------------------------------------------------------- probe

def write_probe(r, m, mi, rng):
    """The probe child's view of itself: report.json, maps, smaps, status."""
    p = m['probe_layout']
    page = m['page_size']
    MiB = 1024 * 1024
    thp = m.get('thp', 'never')
    x86 = m['arch'] == 'x86_64'
    sd = ' sd' if x86 else ''
    exe_dev, exe_ino = p['exe_dev'], p['exe_inode']
    vmas = []
    exe = p['exe']
    # Go static binary, not PIE: text, rodata, data, then bss.
    text_end, ro_end, data_end, bss_end = exe + 0x6f8000, exe + 0xd2a000, exe + 0xd76000, exe + 0xdb9000
    vmas.append(VMA(exe, text_end, 'r-xp', 0, exe_dev, exe_ino, 'atlas', 'file', rss_kb=4312,
                    shared_clean=1832, private_clean=2480, flags='rd ex mr mw me dw' + sd))
    vmas.append(VMA(text_end, ro_end, 'r--p', text_end - exe, exe_dev, exe_ino, 'atlas', 'file', rss_kb=2208,
                    shared_clean=0, private_clean=2208, flags='rd mr mw me dw' + sd))
    vmas.append(VMA(ro_end, data_end, 'rw-p', ro_end - exe, exe_dev, exe_ino, 'atlas', 'file', rss_kb=148,
                    anon_kb=96, private_clean=52, private_dirty=96, flags='rd wr mr mw me dw ac' + sd))
    vmas.append(VMA(data_end, bss_end, 'rw-p', 0, '00:00', 0, '', 'anon', rss_kb=124, anon_kb=124,
                    private_dirty=124, flags='rd wr mr mw me ac' + sd, thp_eligible=int(thp == 'always')))
    heap = p['heap_arena']
    # The Go runtime names its heap arena "[anon: Go: heap]" where the kernel
    # can name anonymous mappings (5.17+); there the probe reports it as the
    # heap (heap_kind go-arena). Without names nothing marks it, and the
    # probe reports no heap.
    named = ver_ge(m['kver'], 5, 17)
    vmas.append(VMA(heap, heap + 0x400000, 'rw-p', 0, '00:00', 0, '[anon: Go: heap]' if named else '', 'anon',
                    rss_kb=1312, anon_kb=1312,
                    private_dirty=1312, flags='rd wr mr mw me ac' + sd, thp_eligible=int(thp == 'always')))
    vmas.append(VMA(heap + 0x400000, heap + 0x4000000, '---p', 0, '00:00', 0, '', 'anon',
                    flags='mr mw me ac' + sd))

    # The five probe regions. On kernels that can name anonymous mappings
    # (5.17+, CONFIG_ANON_VMA_NAME) the probe maps each with a plain mmap,
    # top-down from mmap_base, and names the anonymous ones; Linux 6.7 and
    # later align an anonymous mapping whose length is a multiple of 2 MiB
    # on a 2 MiB boundary, older kernels only on a page. On older kernels
    # the probe falls back to its own layout: one PROT_NONE reservation with
    # each region on a 2 MiB boundary between guard pages.
    top = p['mmap_base']
    regions = []
    placement = 'mmap' if ver_ge(m['kver'], 5, 17) else 'guarded'
    thp_align = ver_ge(m['kver'], 6, 7)

    def take(size, align=page):
        nonlocal top
        start = (top - size) // align * align
        top = start
        return start

    hp = (thp == 'always')
    specs = [
        ('anon-untouched', 64 * MiB, 'anon'),
        ('anon-written', 8 * MiB, 'anon'),
        ('anon-read', 4 * MiB, 'anon'),
        ('file-private', 4 * MiB, 'file'),
        ('shmem-shared', 4 * MiB, 'shmem'),
    ]
    if placement == 'guarded':
        align2m = lambda n: (n + 2 * MiB - 1) // (2 * MiB) * (2 * MiB)
        total = 2 * MiB + sum(align2m(size + page) for _, size, _ in specs)
        base = take(total)
        starts, off = [], align2m(base + page) - base
        for _, size, _ in specs:
            starts.append(base + off)
            off = align2m(base + off + size + page) - base
        edges = [base] + [x for st, (_, size, _) in zip(starts, specs) for x in (st, st + size)] + [base + total]
        for lo, hi in zip(edges[::2], edges[1::2]):
            vmas.append(VMA(lo, hi, '---p', 0, '00:00', 0, '', 'anon', flags='mr mw me nr' + sd))
    for i, (name, size, backing) in enumerate(specs):
        if placement == 'guarded':
            start = starts[i]
        else:
            start = take(size, 2 * MiB if thp_align and backing == 'anon' else page)
        end = start + size
        npages = size // page
        label = f'[anon: atlas probe: {name}]' if placement == 'mmap' and backing == 'anon' else ''
        if name == 'anon-untouched':
            v = VMA(start, end, 'rw-p', 0, '00:00', 0, label, 'anon', flags='rd wr mr mw me ac' + sd,
                    thp_eligible=int(hp))
            touched, present = 0, 0
        elif name == 'anon-written':
            # A huge page needs an aligned 2 MiB range inside the region.
            huge_ranges = max(0, (end // (2 * MiB)) - ((start + 2 * MiB - 1) // (2 * MiB))) if hp else 0
            huge_kb = huge_ranges * 2048
            written_faults = huge_ranges + (size - huge_ranges * 2 * MiB) // page
            v = VMA(start, end, 'rw-p', 0, '00:00', 0, label, 'anon', rss_kb=8192, anon_kb=8192, private_dirty=8192,
                    anon_huge=huge_kb, flags='rd wr mr mw me ac' + sd, thp_eligible=int(hp))
            touched, present = size, npages
        elif name == 'anon-read':
            # Read faults on untouched anonymous memory map the shared zero
            # page: present in the page table, but not counted in Rss.
            v = VMA(start, end, 'rw-p', 0, '00:00', 0, label, 'anon', flags='rd wr mr mw me ac' + sd,
                    thp_eligible=int(hp))
            touched, present = size, npages
        elif name == 'file-private':
            v = VMA(start, end, 'r--p', 0, exe_dev, exe_ino, 'atlas', 'file', rss_kb=4096, shared_clean=1832,
                    private_clean=2264, flags='rd mr mw me' + sd)
            touched, present = size, npages
        else:
            v = VMA(start, end, 'rw-s', 0, '00:01', p['memfd_inode'], 'memfd:atlas-probe (deleted)', 'shmem',
                    rss_kb=1024, private_dirty=1024, flags='rd wr sh mr mw me ms' + sd)
            touched, present = MiB, MiB // page
        regions.append({'name': name, 'start': hex(start), 'end': hex(end), 'size_bytes': size,
                        'touched_bytes': touched, 'backing': backing, 'pages_total': npages,
                        'pages_present': present, 'pages_swapped': 0})
        vmas.append(v)
    # Go runtime's own small mappings below the probe regions.
    for size, rss in ((0x100000, 44), (0x40000, 16), (0x200000, 112), (0x10000, 8)):
        s = take(size)
        vmas.append(VMA(s, s + size, 'rw-p', 0, '00:00', 0, '', 'anon', rss_kb=rss, anon_kb=rss,
                        private_dirty=rss, flags='rd wr mr mw me ac' + sd))
    st = p['stack']
    vmas.append(VMA(st - 0x21000, st, 'rw-p', 0, '00:00', 0, '[stack]', 'stack', rss_kb=20, anon_kb=20,
                    private_dirty=20, flags='rd wr mr mw me gd ac' + sd))
    vv = p['vvar']
    vvar_size = (0x4000 if ver_ge(m['kver'], 5, 6) else 0x3000) if x86 else 0x2000
    vmas.append(VMA(vv, vv + vvar_size, 'r--p', 0, '00:00', 0, '[vvar]', 'vvar',
                    flags='rd mr pf io de dd' + sd))
    vmas.append(VMA(vv + vvar_size, vv + vvar_size + 0x2000, 'r-xp', 0, '00:00', 0, '[vdso]', 'vdso', rss_kb=8,
                    shared_clean=8, flags='rd ex mr mw me de' + sd, pss=4))
    vmas.sort(key=lambda v: v.start)
    if x86:
        vmas.append(VMA(0xffffffffff600000, 0xffffffffff601000, '--xp' if ver_ge(m['kver'], 5, 3) else 'r-xp', 0,
                        '00:00', 0, '[vsyscall]', 'vsyscall', flags='ex'))
    r.w('_probe/maps', ''.join(maps_line(v) + '\n' for v in vmas))
    r.w('_probe/smaps', smaps(m, vmas))

    real = [v for v in vmas if v.path != '[vsyscall]']
    vm_size = sum(v.size_kb for v in real)
    rss_anon = sum(v.anon for v in real)
    rss_shmem = sum(v.rss for v in real if v.kind == 'shmem')
    rss = sum(v.rss for v in real)
    rss_file = rss - rss_anon - rss_shmem
    exe_kb = (text_end - exe) // KB
    status = status_text(m, vm_size, rss, rss_anon, rss_file, rss_shmem, exe_kb, vmas)
    r.w('_probe/status', status)
    base = p['faults_before']
    delta = written_faults + p.get('extra_faults', 2)
    # Each run's stack is the start of its [stack] VMA (page-aligned); run
    # 1's is the one in maps, run 2's stack VMA has the same size.
    runs = []
    for run in p['layout_runs']:
        top = int(run['stack'], 16) // page * page if run['run'] != 1 else st
        out = dict(run, stack=hex(top - 0x21000))
        if named:
            out['heap'], out['heap_kind'] = hex(heap), 'go-arena'
        else:
            out.pop('heap', None)
        runs.append(out)
    report = {'regions': regions,
              'faults': {'minflt_before_touch': base, 'minflt_after_touch': base + delta,
                         'majflt_before_touch': 0, 'majflt_after_touch': 0},
              # System-wide THP counters across the write loop: one huge
              # page per aligned 2 MiB stretch written under THP always,
              # plus whatever other processes faulted in that instant.
              'thp_faults': {'fault_alloc_delta': huge_ranges + p.get('thp_other_allocs', 0),
                             'fault_fallback_delta': 0},
              'pagemap': {'readable': True, 'pfn_visible': False},
              'placement': placement,
              'layout_runs': runs}
    write_json(r, '_probe/report.json', report)


def status_text(m, vm_size, rss, anon, file, shmem, exe_kb, vmas):
    v = m['kver']
    data_kb = sum(v_.size_kb for v_ in vmas if v_.perms.startswith('rw') and v_.kind in ('anon',) and (v_.path == '' or v_.path.startswith('[anon')))
    lines = [('Name', 'atlas'), ('Umask', '0022'), ('State', 'S (sleeping)'), ('Tgid', '4242'), ('Ngid', '0'),
             ('Pid', '4242'), ('PPid', '4231'), ('TracerPid', '0'), ('Uid', '1000\t1000\t1000\t1000'),
             ('Gid', '1000\t1000\t1000\t1000'), ('FDSize', '64'), ('Groups', '1000 '), ('NStgid', '4242'),
             ('NSpid', '4242'), ('NSpgid', '4231'), ('NSsid', '4231')]
    if ver_ge(v, 6, 0):
        lines.append(('Kthread', '0'))
    lines += [('VmPeak', f'{vm_size + 1024:>8} kB'), ('VmSize', f'{vm_size:>8} kB'), ('VmLck', '       0 kB'),
              ('VmPin', '       0 kB'), ('VmHWM', f'{rss:>8} kB'), ('VmRSS', f'{rss:>8} kB'),
              ('RssAnon', f'{anon:>8} kB'), ('RssFile', f'{file:>8} kB'), ('RssShmem', f'{shmem:>8} kB'),
              ('VmData', f'{data_kb:>8} kB'), ('VmStk', '     132 kB'), ('VmExe', f'{exe_kb:>8} kB'),
              ('VmLib', '       8 kB'), ('VmPTE', f'{m["probe_layout"]["vmpte"]:>8} kB'), ('VmSwap', '       0 kB'),
              ('HugetlbPages', '       0 kB'), ('CoreDumping', '0')]
    if ver_ge(v, 5, 0):
        lines.append(('THP_enabled', '1'))
    if ver_ge(v, 6, 4) and m['arch'] == 'x86_64':
        lines.append(('untag_mask', '0xffffffffffffffff'))
    lines += [('Threads', '6'), ('SigQ', '0/31523'), ('SigPnd', '0000000000000000'), ('ShdPnd', '0000000000000000'),
              ('SigBlk', '0000000000000000'), ('SigIgn', '0000000000000000'), ('SigCgt', 'fffffffd7fc1feff'),
              ('CapInh', '0000000000000000'), ('CapPrm', '0000000000000000'), ('CapEff', '0000000000000000'),
              ('CapBnd', '000001ffffffffff' if ver_ge(v, 5, 9) else '0000003fffffffff'),
              ('CapAmb', '0000000000000000'), ('NoNewPrivs', '0'), ('Seccomp', '0')]
    if ver_ge(v, 5, 9):
        lines.append(('Seccomp_filters', '0'))
    lines += [('Speculation_Store_Bypass', 'thread vulnerable' if m['arch'] == 'x86_64' else 'not vulnerable')]
    if ver_ge(v, 5, 15):
        lines.append(('SpeculationIndirectBranch', 'conditional enabled' if m['arch'] == 'x86_64' else 'unknown'))
    ncpu = len(m['cpus'])
    mask = format((1 << ncpu) - 1, 'x')
    lines += [('Cpus_allowed', mask), ('Cpus_allowed_list', cpulist(m['online_cpus'])),
              ('Mems_allowed', '00000000,' * 31 + format((1 << len(m['nodes'])) - 1, '08x')),
              ('Mems_allowed_list', cpulist(m['nodes'])), ('voluntary_ctxt_switches', '37'),
              ('nonvoluntary_ctxt_switches', '2')]
    if ver_ge(v, 6, 6) and m['arch'] == 'x86_64':
        lines += [('x86_Thread_features', ''), ('x86_Thread_features_locked', '')]
    return ''.join(f'{k}:\t{val}\n' for k, val in lines)
