"""Writes the CPU and scheduler files of one fixture root: per-CPU topology and
caches, cpufreq, load, /proc/schedstat, CPU pressure, scheduler sysctls,
sched_ext, atlas's own /proc/self scheduling files and the cpu controller of
its cgroup. Called from machine.generate after everything else except the
README, with its own random sequence so the other generators' numbers do not
shift. Per-machine choices live in KNOBS below; every value is invented."""
import os
import random

from genlib import ver_ge

HZ = 100  # USER_HZ, the unit of /proc/stat

# Frequencies are in kHz, as cpufreq prints them.
KNOBS = {
    'desktop-x86_64': dict(
        loadavg=(1.24, 0.98, 0.87), entities=1187, last_pid=482113, running=3, blocked=0,
        psi=(0.42, 0.61, 0.55), wait_ratio=0.06, slice_ms=1.1,
        freq=dict(driver='intel_pstate', governor='powersave', governors='performance powersave',
                  hw=(800000, 5100000), lim=(800000, 5100000), epp='balance_performance', base=3800000,
                  per_cpu=True, cur=(800000, 4700000)),
        intel_pstate=('active', 0), smt='on', isolated='', nohz_full='',
        autogroup=1, uclamp=True, sched_ext='disabled',
        cg=dict(controllers='cpu memory pids', max='max 100000', weight=100, usage=(18_204_331_552, 0.81)),
        self=dict(nice=0, policy=0, vol=48, invol=3, run_ns=5_812_330, wait_ns=401_224, slices=61),
        readme=('The CPU and scheduler files show `intel_pstate` in active mode with the `powersave` '
                'governor and an energy-performance preference of `balance_performance`, turbo allowed '
                '(`no_turbo` 0, and no global `cpufreq/boost` file, as with every intel_pstate system); '
                'an EEVDF kernel whose `/proc/self/sched` prints `se.slice`; sched_ext built in and '
                'disabled; the cpu controller enabled for the session with no quota.'),
    ),
    'rpi-arm64-noswap': dict(
        loadavg=(0.18, 0.22, 0.19), entities=203, last_pid=9132, running=1, blocked=0,
        psi=(0.05, 0.12, 0.10), wait_ratio=0.03, slice_ms=2.4,
        freq=dict(driver='cpufreq-dt', governor='schedutil',
                  governors='conservative ondemand userspace powersave performance schedutil',
                  hw=(600000, 1800000), lim=(600000, 1800000), per_cpu=False, cur=(600000, 1500000),
                  latency=355000),
        smt='notimplemented', isolated='', nohz_full=None, capacity=1024, cluster=0,
        autogroup=1, uclamp=False, sched_ext=None,
        cg=dict(controllers='memory pids', max=None, usage=(2_918_402_118, 0.74)),
        self=dict(nice=0, policy=0, vol=31, invol=1, run_ns=9_304_112, wait_ns=181_502, slices=40),
        readme=('The CPU and scheduler files show one cpufreq policy for all four cores, driven by '
                '`cpufreq-dt` with the `schedutil` governor; every CPU has `cpu_capacity` 1024 (four '
                'identical Cortex-A72 cores); SMT control is `notimplemented` on arm64; no `nohz_full` '
                'file and no utilization-clamp sysctls in this kernel build; the cpu controller is not '
                'enabled for the session, so `cpu.max` is missing and `cpu.stat` has only usage lines.'),
    ),
    'alpine-container': dict(
        loadavg=(3.12, 2.87, 2.61), entities=431, last_pid=87, running=4, blocked=0,
        psi=(1.10, 0.95, 0.88), wait_ratio=0.25, slice_ms=1.6, steal=0.002,
        freq=None, smt='on', isolated='', nohz_full='', allowed=[0, 1],
        autogroup=1, uclamp=True, sched_ext=None,
        cg=dict(controllers='cpuset cpu io memory hugetlb pids rdma misc', max='150000 100000', weight=100,
                cpuset='0-1', periods=(812_345, 40_213, 3_112_948_201), usage=(1_218_557_092_311, 0.88)),
        self=dict(nice=0, policy=0, vol=27, invol=9, run_ns=7_204_881, wait_ns=2_884_102, slices=52),
        readme=('The CPU options of that command give `cpu.max` '
                '`150000 100000` (one and a half CPUs per 100 ms period) and `cpu.stat` shows about one '
                'period in twenty throttled, while `cpuset.cpus.effective` and atlas\'s '
                '`Cpus_allowed_list` are `0-1`. `/proc/loadavg`, `/proc/stat`, `/proc/schedstat` and '
                'CPU pressure describe the whole host, as nothing virtualises them. The EC2 guest has no '
                'cpufreq directory and no intel_pstate (the hypervisor owns the frequency) and a little '
                'steal time.'),
    ),
    'numa-server-la57': dict(
        loadavg=(21.36, 19.82, 18.95), entities=2894, last_pid=1987345, running=23, blocked=1,
        psi=(2.15, 3.40, 3.12), wait_ratio=0.18, slice_ms=3.0,
        freq=dict(driver='intel_pstate', governor='performance', governors='performance powersave',
                  hw=(800000, 3400000), lim=(800000, 3400000), epp='performance', base=2400000,
                  per_cpu=True, cur=(2400000, 3400000)),
        intel_pstate=('active', 0), smt='on',
        isolated=[8, 9, 10, 11, 12, 13, 14, 15, 40, 41, 42, 43, 44, 45, 46, 47],
        nohz_full=[8, 9, 10, 11, 12, 13, 14, 15, 40, 41, 42, 43, 44, 45, 46, 47],
        autogroup=1, uclamp=True, sched_ext=None,
        cg=dict(controllers='cpu memory pids', max='max 100000', weight=100, usage=(91_224_118_090, 0.77)),
        self=dict(nice=0, policy=0, vol=40, invol=6, run_ns=6_402_118, wait_ns=902_331, slices=58),
        readme=('The CPU and scheduler files show the `performance` governor of intel_pstate on all 64 '
                'CPUs; CPUs 8-15 and 40-47 (eight cores of each socket, with their SMT siblings) are '
                'booted with `isolcpus=` and `nohz_full=` for the database\'s pinned WAL writers, so '
                'atlas\'s own affinity leaves them out; a long run-queue wait share from a busy '
                'machine; an EEVDF kernel (6.8) whose `/proc/self/sched` has no `se.slice` line.'),
    ),
    'old-vm-cgroupv1': dict(
        loadavg=(2.31, 2.08, 1.97), entities=203, last_pid=29481, running=3, blocked=1,
        psi=None, wait_ratio=0.35, slice_ms=4.0, steal=0.06,
        freq=None, smt='notsupported', isolated='', nohz_full='',
        autogroup=0, uclamp=False, sched_ext=None,
        cg_v1=dict(quota=150000, period=100000, shares=1024, periods=(2_401_993, 182_330, 5_920_118_402_331),
                   usage_ns=41_208_331_902_118_000),
        self=dict(nice=0, policy=0, vol=22, invol=4, run_ns=11_402_993, wait_ns=6_001_442, slices=37),
        readme=('The CPU and scheduler files show a CFS kernel (4.19: `/proc/self/sched` without '
                '`se.slice`, schedstat version 15, no sched_ext, no CPU pressure); no cpufreq directory '
                'in the guest; steal time in `/proc/stat`; autogroup switched off; and on the v1 '
                '`cpu,cpuacct` hierarchy a quota on `user.slice` (`cpu.cfs_quota_us` 150000 of 100000, '
                'systemd\'s `CPUQuota=150%`) with throttled periods. SMT control reads `notsupported` '
                'because the hypervisor presents no SMT siblings.'),
    ),
    'restricted': dict(
        loadavg=(0.52, 0.47, 0.41), entities=287, last_pid=1442, running=2, blocked=0,
        psi=(0.08, 0.11, 0.09), wait_ratio=0.05, slice_ms=2.0,
        freq=dict(driver='intel_pstate', governor='powersave', governors='performance powersave',
                  hw=(700000, 3400000), lim=(700000, 3400000), epp='balance_power', base=800000,
                  per_cpu=True, cur=(700000, 2900000)),
        intel_pstate=('active', 0), smt='notsupported', isolated='', nohz_full='',
        autogroup=1, uclamp=False, sched_ext=None,
        cg=dict(controllers='cpu memory pids', max='max 100000', weight=20, usage=(31_220_118, 0.69)),
        self=dict(nice=10, policy=3, vol=19, invol=0, run_ns=4_102_773, wait_ns=88_120, slices=33),
        readme=('CPU and scheduler: `/sys/devices/system/cpu` (topology, caches, cpufreq, SMT) and '
                '`/proc/pressure` are denied, and so is `/proc/schedstat`, whose per-CPU timings an '
                'AppArmor profile keeps from the service as a side channel. Still readable: '
                '`/proc/loadavg`, `/proc/stat`, the scheduler sysctls, atlas\'s own `/proc/self` files '
                'and its cgroup. The service runs with `Nice=10`, `CPUSchedulingPolicy=batch` and '
                '`CPUWeight=20`, so it yields to everything else on the machine.'),
    ),
}

POLICY_NAMES = {0: 'normal', 3: 'batch'}


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


def cpumask(cpus, nbits):
    """The kernel's %*pb bitmap: 32-bit hex groups, most significant first."""
    v = 0
    for c in cpus:
        v |= 1 << c
    groups = []
    for g in range((nbits + 31) // 32):
        groups.append((v >> (32 * g)) & 0xffffffff)
    groups.reverse()
    first = format(groups[0], 'x')
    if nbits % 32:
        first = format(groups[0], f'0{(nbits % 32 + 3) // 4}x')
    return ','.join([first] + [format(x, '08x') for x in groups[1:]])


def exists(r, path):
    return os.path.exists(os.path.join(r.dir, path.lstrip('/')))


def read(r, path):
    with open(os.path.join(r.dir, path.lstrip('/'))) as f:
        return f.read()


def write_sched(r, m):
    k = KNOBS[m['name']]
    rng = random.Random(m['name'] + ':sched')
    cpus = [c['cpu'] for c in m['cpus']]
    write_topology(r, m, k)
    write_cpufreq(r, m, k, rng)
    write_load(r, m, k, rng)
    write_settings(r, m, k)
    write_self(r, m, k)
    write_cgroup_cpu(r, m, k)
    allowed = k.get('allowed') or [c for c in cpus if c not in (k['isolated'] or [])]
    if exists(r, '_probe/status'):
        # The probe child inherits atlas's affinity.
        r.w('_probe/status', patch_status(read(r, '_probe/status'), allowed, len(cpus)))
    m['readme'] = m['readme'].rstrip() + '\n\n' + wrap(k['readme']) + '\n'


def wrap(text, width=78):
    out, line = [], ''
    for w in text.split():
        if line and len(line) + 1 + len(w) > width:
            out.append(line)
            line = w
        else:
            line = f'{line} {w}' if line else w
    out.append(line)
    return '\n'.join(out)


# ------------------------------------------------------------------ topology

def write_topology(r, m, k):
    sysd = '/sys/devices/system/cpu'
    cpus = m['cpus']
    ids = [c['cpu'] for c in cpus]
    possible = m.get('possible', ids)
    r.w(f'{sysd}/offline', cpulist(set(possible) - set(ids)) + '\n')
    for name in ('isolated', 'nohz_full'):
        v = k[name]
        if v is None:
            continue
        r.w(f'{sysd}/{name}', (cpulist(v) if isinstance(v, list) else v) + '\n')
    if ver_ge(m['kver'], 4, 19):
        r.w(f'{sysd}/smt/control', k['smt'] + '\n')
        r.w(f'{sysd}/smt/active', ('1' if m['cpus'][0].get('siblings', 1) > m['cpus'][0].get('cores', 1) else '0') + '\n')
    for c in cpus:
        t = f'{sysd}/cpu{c["cpu"]}/topology'
        if ver_ge(m['kver'], 5, 2):
            r.w(f'{t}/die_id', '0\n')
        if ver_ge(m['kver'], 5, 16):
            r.w(f'{t}/cluster_id', f'{k.get("cluster", 65535)}\n')
        if k.get('capacity'):
            r.w(f'{sysd}/cpu{c["cpu"]}/cpu_capacity', f'{k["capacity"]}\n')
    write_caches(r, m)


def write_caches(r, m):
    """cpu0's caches come from the machine description; every other CPU gets
    the same caches, shared the way cpu0's are: with its SMT siblings, its
    package, every CPU, or nobody."""
    cpus = m['cpus']
    by = {c['cpu']: c for c in cpus}
    allc = sorted(by)

    def siblings(n):
        c = by[n]
        return [d['cpu'] for d in cpus if d['pkg'] == c['pkg'] and d['core'] == c['core']]

    def package(n):
        return [d['cpu'] for d in cpus if d['pkg'] == by[n]['pkg']]

    sysd = '/sys/devices/system/cpu'
    for i, cache in enumerate(m.get('caches', [])):
        shared = cache['shared_cpu_list']
        if shared == cpulist(siblings(0)):
            rule = siblings
        elif shared == cpulist(package(0)):
            rule = package
        elif shared == cpulist(allc):
            rule = lambda n: allc  # noqa: E731
        else:
            rule = lambda n: [n]  # noqa: E731
        for n in allc[1:]:
            d = f'{sysd}/cpu{n}/cache/index{i}'
            for key in ('level', 'type', 'size', 'coherency_line_size', 'ways_of_associativity'):
                if key in cache:
                    r.w(f'{d}/{key}', f'{cache[key]}\n')
            r.w(f'{d}/shared_cpu_list', cpulist(rule(n)) + '\n')


# ------------------------------------------------------------------- cpufreq

def write_cpufreq(r, m, k, rng):
    f = k.get('freq')
    if f is None:
        return
    sysd = '/sys/devices/system/cpu'
    ids = [c['cpu'] for c in m['cpus']]
    groups = [[c] for c in ids] if f['per_cpu'] else [ids]
    for g in groups:
        p = f'{sysd}/cpufreq/policy{g[0]}'
        r.w(f'{p}/affected_cpus', ' '.join(map(str, g)) + '\n')
        r.w(f'{p}/related_cpus', ' '.join(map(str, g)) + '\n')
        r.w(f'{p}/scaling_driver', f['driver'] + '\n')
        r.w(f'{p}/scaling_governor', f['governor'] + '\n')
        r.w(f'{p}/scaling_available_governors', f['governors'] + ' \n')
        lo, hi = f['cur']
        cur = hi if f['governor'] == 'performance' else int(rng.uniform(lo, hi) / 1000) * 1000
        r.w(f'{p}/scaling_cur_freq', f'{cur}\n')
        r.w(f'{p}/scaling_min_freq', f'{f["lim"][0]}\n')
        r.w(f'{p}/scaling_max_freq', f'{f["lim"][1]}\n')
        r.w(f'{p}/cpuinfo_min_freq', f'{f["hw"][0]}\n')
        r.w(f'{p}/cpuinfo_max_freq', f'{f["hw"][1]}\n')
        r.w(f'{p}/cpuinfo_transition_latency', f'{f.get("latency", 0)}\n')
        r.w(f'{p}/scaling_setspeed', '<unsupported>\n')
        if f.get('base'):
            r.w(f'{p}/base_frequency', f'{f["base"]}\n')
        if f.get('epp'):
            r.w(f'{p}/energy_performance_preference', f['epp'] + '\n')
            r.w(f'{p}/energy_performance_available_preferences',
                'default performance balance_performance balance_power power \n')
    if k.get('intel_pstate'):
        status, no_turbo = k['intel_pstate']
        d = f'{sysd}/intel_pstate'
        r.w(f'{d}/status', status + '\n')
        r.w(f'{d}/no_turbo', f'{no_turbo}\n')
        r.w(f'{d}/max_perf_pct', '100\n')
        r.w(f'{d}/min_perf_pct', f'{f["hw"][0] * 100 // f["hw"][1]}\n')
        r.w(f'{d}/hwp_dynamic_boost', '0\n')


# ---------------------------------------------------------------------- load

def write_load(r, m, k, rng):
    a = k['loadavg']
    r.w('/proc/loadavg', f'{a[0]:.2f} {a[1]:.2f} {a[2]:.2f} {k["running"]}/{k["entities"]} {k["last_pid"]}\n')

    # /proc/stat is written by write_identity; give VMs their steal time and
    # every machine its own run-queue counts.
    lines = read(r, '/proc/stat').splitlines()
    rows, out = {}, []
    for line in lines:
        f = line.split()
        if f and f[0].startswith('cpu') and f[0] != 'cpu':
            row = [int(x) for x in f[1:]]
            if k.get('steal'):
                total = sum(row[:8])
                st = int(total * k['steal'] * rng.uniform(0.8, 1.2))
                row[3] -= st
                row[7] += st
            rows[f[0]] = row
    total_row = [sum(r_[i] for r_ in rows.values()) for i in range(10)]
    for line in lines:
        f = line.split()
        if f and f[0] == 'cpu':
            out.append('cpu  ' + ' '.join(map(str, total_row)))
        elif f and f[0] in rows:
            out.append(f'{f[0]} ' + ' '.join(map(str, rows[f[0]])))
        elif f and f[0] == 'procs_running':
            out.append(f'procs_running {k["running"]}')
        elif f and f[0] == 'procs_blocked':
            out.append(f'procs_blocked {k["blocked"]}')
        else:
            out.append(line)
    r.w('/proc/stat', '\n'.join(out) + '\n')

    # /proc/schedstat: run time from the busy ticks of /proc/stat, wait time
    # a machine-specific share of it, timeslices from an average slice.
    v = m['kver']
    version = 17 if ver_ge(v, 6, 13) else 16 if ver_ge(v, 6, 10) else 15
    jiffies = int(m['uptime'] * 250) + 4294667296
    sl = [f'version {version}', f'timestamp {jiffies}']
    ncpu = len(rows)
    for name, row in rows.items():
        busy = sum(row[i] for i in (0, 1, 2, 5, 6))
        run = busy * (1_000_000_000 // HZ) + rng.randrange(1_000_000)
        wait = int(run * k['wait_ratio'] * rng.uniform(0.8, 1.2))
        slices = int(run / (k['slice_ms'] * 1_000_000) * rng.uniform(0.9, 1.1))
        goidle = int(slices * 0.4)
        ttwu = int(slices * 0.55)
        sl.append(f'{name} 0 0 {slices + goidle} {goidle} {ttwu} {int(ttwu * 0.6)} {run} {wait} {slices}')
        if ncpu > 1:
            span = cpumask(range(ncpu), ncpu)
            dom = 'domain0 ' + ('MC ' if version >= 17 else '') + span + ' ' + ' '.join('0' for _ in range(45 if version >= 17 else 36))
            sl.append(dom)
    r.w('/proc/schedstat', '\n'.join(sl) + '\n')

    if k['psi'] and exists(r, '/proc/pressure/cpu'):
        p = k['psi']
        total = int(m['uptime'] * 1e6 * p[2] / 100)
        text = f'some avg10={p[0]:.2f} avg60={p[1]:.2f} avg300={p[2]:.2f} total={total}\n'
        if ver_ge(v, 5, 13):
            text += 'full avg10=0.00 avg60=0.00 avg300=0.00 total=0\n'
        r.w('/proc/pressure/cpu', text)


# ------------------------------------------------------------------ settings

def write_settings(r, m, k):
    v = m['kver']
    ks = '/proc/sys/kernel'
    r.w(f'{ks}/sched_autogroup_enabled', f'{k["autogroup"]}\n')
    r.w(f'{ks}/sched_rt_runtime_us', '950000\n')
    r.w(f'{ks}/sched_rt_period_us', '1000000\n')
    r.w(f'{ks}/sched_rr_timeslice_ms', '100\n')
    r.w(f'{ks}/sched_cfs_bandwidth_slice_us', '5000\n')
    if k['uclamp'] and ver_ge(v, 5, 3):
        r.w(f'{ks}/sched_util_clamp_min', '1024\n')
        r.w(f'{ks}/sched_util_clamp_max', '1024\n')
        r.w(f'{ks}/sched_util_clamp_min_rt_default', '1024\n')
    if k['sched_ext'] and ver_ge(v, 6, 12):
        d = '/sys/kernel/sched_ext'
        r.w(f'{d}/state', k['sched_ext'] + '\n')
        r.w(f'{d}/switch_all', '0\n')
        r.w(f'{d}/nr_rejected', '0\n')
        r.w(f'{d}/hotplug_seq', '0\n')
        r.w(f'{d}/enable_seq', '0\n')


# ------------------------------------------------------------- atlas itself

def patch_status(text, allowed, nbits):
    out = []
    for line in text.splitlines():
        key = line.split(':', 1)[0]
        if key == 'Cpus_allowed':
            line = f'Cpus_allowed:\t{cpumask(allowed, nbits)}'
        elif key == 'Cpus_allowed_list':
            line = f'Cpus_allowed_list:\t{cpulist(allowed)}'
        out.append(line)
    return '\n'.join(out) + '\n'


def write_self(r, m, k):
    s = k['self']
    v = m['kver']
    cpus = [c['cpu'] for c in m['cpus']]
    allowed = k.get('allowed') or [c for c in cpus if c not in (k['isolated'] or [])]
    # atlas's own status: the probe child's, with the parent's counters.
    status = patch_status(read(r, '_probe/status'), allowed, len(cpus))
    out = []
    for line in status.splitlines():
        key = line.split(':', 1)[0]
        if key in ('Pid', 'Tgid', 'NStgid', 'NSpid'):
            line = f'{key}:\t4231'
        elif key == 'PPid':
            line = 'PPid:\t4220'
        elif key == 'Threads':
            line = 'Threads:\t9'
        elif key == 'voluntary_ctxt_switches':
            line = f'voluntary_ctxt_switches:\t{s["vol"]}'
        elif key == 'nonvoluntary_ctxt_switches':
            line = f'nonvoluntary_ctxt_switches:\t{s["invol"]}'
        out.append(line)
    r.w('/proc/self/status', '\n'.join(out) + '\n')

    prio = 20 + s['nice']
    stat = ['4231', '(atlas)', 'S', '4220', '4231', '4220', '34816', '4231', '4194560', '1208', '0', '0', '0',
            '41', '12', '0', '0', str(prio), str(s['nice']), '9', '0', str(int(m['uptime'] * HZ) - 210),
            '1310720000', '9811', '18446744073709551615', '4194304', '18563072', '140728000000000', '0', '0',
            '0', '0', '0', '2143420159', '0', '0', '0', '17', str(allowed[0]), '0', str(s['policy']), '0',
            '0', '0', '18690048', '19003392', '25165824', '140728000004000', '140728000004100',
            '140728000004100', '140728000008000', '0']
    r.w('/proc/self/stat', ' '.join(stat) + '\n')
    r.w('/proc/self/schedstat', f'{s["run_ns"]} {s["wait_ns"]} {s["slices"]}\n')

    weight = [1024, 820, 655, 526, 423, 335, 272, 215, 172, 137, 110][s['nice']] if s['nice'] >= 0 else 1024
    vr = int(m['uptime'] * 1e6 * 0.37)
    lines = ['atlas (4231, #threads: 9)', '-' * 67]

    def p(key, val):
        lines.append(f'{key:<45}:{val:>21}')

    ms = lambda ns: f'{ns // 1_000_000}.{ns % 1_000_000:06d}'  # noqa: E731
    p('se.exec_start', ms(int(m['uptime'] * 1e9)))
    p('se.vruntime', ms(vr))
    if ver_ge(v, 6, 6):
        p('se.deadline', ms(vr + 2_800_000))
    p('se.sum_exec_runtime', ms(s['run_ns']))
    p('se.nr_migrations', '3')
    p('nr_switches', str(s['slices']))
    p('nr_voluntary_switches', str(s['vol']))
    p('nr_involuntary_switches', str(s['invol']))
    p('se.load.weight', str(weight * 1024))
    p('se.avg.load_sum', '46931')
    p('se.avg.runnable_sum', '1302112')
    p('se.avg.util_sum', '1302112')
    p('se.avg.load_avg', str(weight))
    p('se.avg.runnable_avg', '27')
    p('se.avg.util_avg', '27')
    p('se.avg.last_update_time', str(int(m['uptime'] * 1e9) - 1024))
    if ver_ge(v, 5, 3) and k['uclamp']:
        p('uclamp.min', '0')
        p('uclamp.max', '1024')
        p('effective uclamp.min', '0')
        p('effective uclamp.max', '1024')
    p('policy', str(s['policy']))
    p('prio', str(prio))
    if ver_ge(v, 6, 12):
        # se.slice: sysctl_sched_base_slice (0.75 ms, 0.7 ms from 6.13)
        # scaled by 1 + log2 of the online CPUs, counting at most 8.
        factor = min(len(cpus), 8).bit_length()
        base = 700_000 if ver_ge(v, 6, 13) else 750_000
        p('se.slice', str(base * factor))
        p('ext.enabled', '0')
    p('clock-delta', '41')
    lines.append(f'mm->numa_scan_seq{"":<28}:{"0":>21}')
    r.w('/proc/self/sched', '\n'.join(lines) + '\n')


# -------------------------------------------------------------- cpu cgroup

def write_cgroup_cpu(r, m, k):
    cg = m['cgroup']
    if cg['version'] == 'v1':
        c = k['cg_v1']
        d = '/sys/fs/cgroup/cpu,cpuacct' + cg['path'].rstrip('/')
        r.w(f'{d}/cpu.cfs_quota_us', f'{c["quota"]}\n')
        r.w(f'{d}/cpu.cfs_period_us', f'{c["period"]}\n')
        r.w(f'{d}/cpu.shares', f'{c["shares"]}\n')
        r.w(f'{d}/cpu.rt_runtime_us', '0\n')
        r.w(f'{d}/cpu.rt_period_us', '1000000\n')
        per, thr, ns = c['periods']
        r.w(f'{d}/cpu.stat', f'nr_periods {per}\nnr_throttled {thr}\nthrottled_time {ns}\n')
        r.w(f'{d}/cpuacct.usage', f'{c["usage_ns"]}\n')
        user = c['usage_ns'] * 7 // 10 // 10_000_000
        r.w(f'{d}/cpuacct.stat', f'user {user}\nsystem {user * 3 // 7}\n')
        r.w('/sys/fs/cgroup/cpuset/cpuset.effective_cpus', '0-1\n')
        r.w('/sys/fs/cgroup/cpuset/cpuset.cpus', '0-1\n')
        return
    c = k['cg']
    d = '/sys/fs/cgroup' + cg['path'].rstrip('/')
    if cg['path'] != '/':
        r.w(f'{d}/cgroup.controllers', c['controllers'] + '\n')
    usage, user_share = c['usage']
    stat = f'usage_usec {usage}\nuser_usec {int(usage * user_share)}\nsystem_usec {usage - int(usage * user_share)}\n'
    if ver_ge(m['kver'], 6, 1):
        stat += 'core_sched.force_idle_usec 0\n'
    if c.get('max'):
        r.w(f'{d}/cpu.max', c['max'] + '\n')
        r.w(f'{d}/cpu.max.burst', '0\n')
        r.w(f'{d}/cpu.weight', f'{c["weight"]}\n')
        nice = {100: 0, 20: 7}.get(c['weight'], 0)
        r.w(f'{d}/cpu.weight.nice', f'{nice}\n')
        r.w(f'{d}/cpu.idle', '0\n')
        per, thr, us = c.get('periods', (0, 0, 0))
        stat += f'nr_periods {per}\nnr_throttled {thr}\nthrottled_usec {us}\n'
        if ver_ge(m['kver'], 5, 14):
            stat += 'nr_bursts 0\nburst_usec 0\n'
    if c.get('cpuset'):
        r.w(f'{d}/cpuset.cpus', c['cpuset'] + '\n')
        r.w(f'{d}/cpuset.cpus.effective', c['cpuset'] + '\n')
        r.w(f'{d}/cpuset.mems.effective', '0\n')
    r.w(f'{d}/cpu.stat', stat)
