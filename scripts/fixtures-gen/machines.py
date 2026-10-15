"""The six fake machines of ARCHITECTURE.md section 12. Every value is invented."""
from genlib import Zone
from machine import lsblk_json

LSBLK = 'lsblk --json --bytes --output NAME,TYPE,SIZE,FSTYPE'
X86_ZONES = ['DMA', 'DMA32', 'Normal', 'Movable', 'Device']
GiB = 1 << 30


def x86_block(model, family, model_no, stepping, microcode, mhz, cache_kb, flags, bogomips, addr, vmx=None, bugs=''):
    def block(c):
        lines = [
            ('processor', c['cpu']), ('vendor_id', 'GenuineIntel'), ('cpu family', family), ('model', model_no),
            ('model name', model), ('stepping', stepping), ('microcode', microcode),
            ('cpu MHz', f'{mhz + (c["cpu"] * 37) % 400:.3f}'), ('cache size', f'{cache_kb} KB'),
            ('physical id', c['pkg']), ('siblings', c['siblings']), ('core id', c['core']),
            ('cpu cores', c['cores']), ('apicid', c['apic']), ('initial apicid', c['apic']),
            ('fpu', 'yes'), ('fpu_exception', 'yes'), ('cpuid level', 22 if family == 6 else 13), ('wp', 'yes'),
            ('flags', flags)]
        if vmx:
            lines.append(('vmx flags', vmx))
        lines += [('bugs', bugs), ('bogomips', bogomips), ('clflush size', 64), ('cache_alignment', 64),
                  ('address sizes', addr), ('power management', '')]
        return '\n'.join(f'{k}\t: {v}'.rstrip(' ') if v != '' else f'{k}\t:' for k, v in lines)
    return block


def x86_cpus(sockets, cores, threads, core_ids=None, sibling_stride=None, node_of_pkg=None):
    """Logical CPUs numbered the way Linux enumerates Intel parts: first
    thread of every core on every package, then the second threads."""
    cpus = []
    core_ids = core_ids or list(range(cores))
    n = 0
    for t in range(threads):
        for p in range(sockets):
            for i, cid in enumerate(core_ids):
                cpus.append({'cpu': n, 'pkg': p, 'core': cid, 'siblings': cores * threads, 'cores': cores,
                             'apic': p * 64 + i * 2 + t, 'node': (node_of_pkg or {}).get(p, 0)})
                n += 1
    return cpus


DESKTOP_FLAGS = ('fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr '
                 'sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good '
                 'nopl xtopology nonstop_tsc cpuid aperfmperf pni pclmulqdq dtes64 monitor ds_cpl vmx smx est tm2 '
                 'ssse3 sdbg fma cx16 xtpr pdcm pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave '
                 'avx f16c rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb ssbd ibrs ibpb stibp ibrs_enhanced '
                 'tpr_shadow flexpriority ept vpid ept_ad fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx '
                 'rdseed adx smap clflushopt intel_pt xsaveopt xsavec xgetbv1 xsaves dtherm ida arat pln pts hwp '
                 'hwp_notify hwp_act_window hwp_epp vnmi md_clear flush_l1d arch_capabilities')
ICX_FLAGS = ('fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse '
             'sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl '
             'xtopology nonstop_tsc cpuid aperfmperf pni pclmulqdq dtes64 monitor ds_cpl vmx smx est tm2 ssse3 sdbg '
             'fma cx16 xtpr pdcm pcid dca sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c '
             'rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb cat_l3 invpcid_single intel_ppin ssbd mba ibrs ibpb '
             'stibp ibrs_enhanced tpr_shadow flexpriority ept vpid ept_ad fsgsbase tsc_adjust bmi1 avx2 smep bmi2 '
             'erms invpcid cqm rdt_a avx512f avx512dq rdseed adx smap avx512ifma clflushopt clwb intel_pt avx512cd '
             'sha_ni avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves cqm_llc cqm_occup_llc cqm_mbm_total '
             'cqm_mbm_local split_lock_detect wbnoinvd dtherm ida arat pln pts hwp hwp_act_window hwp_epp hwp_pkg_req '
             'vnmi avx512vbmi umip pku ospke avx512_vbmi2 gfni vaes vpclmulqdq avx512_vnni avx512_bitalg tme '
             'avx512_vpopcntdq la57 rdpid fsrm md_clear pconfig flush_l1d arch_capabilities')
AWS_FLAGS = ('fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht '
             'syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon rep_good nopl xtopology nonstop_tsc cpuid '
             'aperfmperf tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt '
             'tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch invpcid_single ssbd '
             'ibrs ibpb stibp ibrs_enhanced fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid avx512f avx512dq '
             'rdseed adx smap avx512ifma clflushopt clwb avx512cd sha_ni avx512bw avx512vl xsaveopt xsavec xgetbv1 '
             'xsaves wbnoinvd ida arat avx512vbmi pku ospke avx512_vbmi2 gfni vaes vpclmulqdq avx512_vnni '
             'avx512_bitalg tme avx512_vpopcntdq rdpid md_clear flush_l1d arch_capabilities')
QEMU_FLAGS = ('fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss '
              'syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology cpuid tsc_known_freq pni pclmulqdq '
              'ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand '
              'hypervisor lahf_lm abm 3dnowprefetch invpcid_single pti ssbd ibrs ibpb fsgsbase tsc_adjust bmi1 hle '
              'avx2 smep bmi2 erms invpcid rtm mpx avx512f avx512dq rdseed adx smap clwb avx512cd avx512bw avx512vl '
              'xsaveopt xsavec xgetbv1 arat')
N100_FLAGS = ('fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse '
              'sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl '
              'xtopology nonstop_tsc cpuid aperfmperf tsc_known_freq pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 '
              'ssse3 sdbg fma cx16 xtpr pdcm sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c '
              'rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb cat_l2 cdp_l2 ssbd ibrs ibpb stibp ibrs_enhanced '
              'tpr_shadow flexpriority ept vpid ept_ad fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid rdt_a '
              'rdseed adx smap clflushopt clwb intel_pt sha_ni xsaveopt xsavec xgetbv1 xsaves split_lock_detect '
              'avx_vnni dtherm ida arat pln pts hwp hwp_notify hwp_act_window hwp_epp hwp_pkg_req vnmi umip pku '
              'ospke waitpkg gfni vaes vpclmulqdq rdpid movdiri movdir64b fsrm md_clear serialize arch_lbr ibt '
              'flush_l1d arch_capabilities')

STD_PSEUDO = [
    # (mount point, fstype, source, dev, super options)
    ('/sys', 'sysfs', 'sysfs', '0:21', 'rw'),
    ('/proc', 'proc', 'proc', '0:22', 'rw'),
    ('/dev', 'devtmpfs', 'devtmpfs', '0:5', 'rw,size=4096k,nr_inodes=1048576,mode=755,inode64'),
    ('/sys/kernel/security', 'securityfs', 'securityfs', '0:6', 'rw'),
    ('/dev/shm', 'tmpfs', 'tmpfs', '0:23', 'rw,inode64'),
    ('/dev/pts', 'devpts', 'devpts', '0:24', 'rw,gid=5,mode=620,ptmxmode=000'),
    ('/run', 'tmpfs', 'tmpfs', '0:25', 'rw,size=1625072k,nr_inodes=819200,mode=755,inode64'),
    ('/sys/fs/cgroup', 'cgroup2', 'cgroup2', '0:26', 'rw,nsdelegate,memory_recursiveprot'),
    ('/sys/fs/pstore', 'pstore', 'pstore', '0:27', 'rw'),
    ('/sys/fs/bpf', 'bpf', 'bpf', '0:28', 'rw,mode=700'),
    ('/proc/sys/fs/binfmt_misc', 'autofs', 'systemd-1', '0:30', 'rw,fd=32,pgrp=1,timeout=0,minproto=5,maxproto=5,direct'),
    ('/dev/hugepages', 'hugetlbfs', 'hugetlbfs', '0:31', 'rw,pagesize=2M'),
    ('/dev/mqueue', 'mqueue', 'mqueue', '0:20', 'rw'),
    ('/sys/kernel/debug', 'debugfs', 'debugfs', '0:7', 'rw'),
    ('/sys/kernel/tracing', 'tracefs', 'tracefs', '0:12', 'rw'),
    ('/sys/fs/fuse/connections', 'fusectl', 'fusectl', '0:32', 'rw'),
    ('/sys/kernel/config', 'configfs', 'configfs', '0:33', 'rw'),
]


def std_mounts(root, extra, skip=()):
    out = [root]
    for mp, fs, src, dev, sup in STD_PSEUDO:
        if mp in skip:
            continue
        out.append({'mp': mp, 'fs': fs, 'src': src, 'dev': dev, 'super': sup,
                    'opts': 'rw,nosuid,nodev,noexec,relatime' if fs in ('sysfs', 'proc', 'pstore', 'bpf', 'securityfs') else 'rw,nosuid,nodev'})
    return out + extra


def v2_stat(anon, file, shmem, kernel):
    return (f'anon {anon}\nfile {file}\nkernel {kernel}\nkernel_stack {kernel // 20}\npagetables {kernel // 6}\n'
            f'sec_pagetables 0\npercpu 0\nsock 0\nvmalloc 0\nshmem {shmem}\nfile_mapped {file // 4}\n'
            f'file_dirty 0\nfile_writeback 0\nswapcached 0\nanon_thp 0\nfile_thp 0\nshmem_thp 0\n'
            f'inactive_anon {anon // 3}\nactive_anon {anon - anon // 3 + shmem}\ninactive_file {file // 2}\n'
            f'active_file {file - file // 2 - shmem}\nunevictable 0\nslab_reclaimable {kernel // 3}\n'
            f'slab_unreclaimable {kernel // 4}\nslab {kernel // 3 + kernel // 4}\n')


def probe_runs(stack1, mmap1, stack2, mmap2, heap, exe):
    return [{'run': 1, 'stack': stack1, 'mmap_base': mmap1, 'heap': heap, 'exe': exe},
            {'run': 2, 'stack': stack2, 'mmap_base': mmap2, 'heap': heap, 'exe': exe}]


# ======================================================================
# desktop-x86_64

desktop = dict(
    name='desktop-x86_64',
    description='16-thread x86_64 desktop (Arch Linux, kernel 6.12): systemd, swap partition, THP always, MGLRU, cgroup v2, a few OOM kills since boot',
    now='2025-01-20T19:42:10Z', arch='x86_64', release='6.12.10-arch1-1', kver=(6, 12), page_size=4096,
    version='Linux version 6.12.10-arch1-1 (linux@archlinux) (gcc (GCC) 14.2.1 20250110, GNU ld (GNU Binutils) 2.43.1) #1 SMP PREEMPT_DYNAMIC Sat, 18 Jan 2025 02:26:57 +0000',
    hostname='fernwood',
    os_release='NAME="Arch Linux"\nPRETTY_NAME="Arch Linux"\nID=arch\nBUILD_ID=rolling\nANSI_COLOR="38;2;23;147;209"\n'
               'HOME_URL="https://archlinux.org/"\nDOCUMENTATION_URL="https://wiki.archlinux.org/"\n'
               'SUPPORT_URL="https://bbs.archlinux.org/"\nBUG_REPORT_URL="https://gitlab.archlinux.org/groups/archlinux/-/issues"\n'
               'PRIVACY_POLICY_URL="https://terms.archlinux.org/docs/privacy-policy/"\nLOGO=archlinux-logo\n',
    uptime=274512.43, idle_frac=0.91, pid1='systemd', pid1_cgroup='0::/init.scope\n', systemd=True,
    cpus=x86_cpus(1, 8, 2),
    caches=[dict(level=1, type='Data', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0,8'),
            dict(level=1, type='Instruction', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0,8'),
            dict(level=2, type='Unified', size='256K', coherency_line_size=64, ways_of_associativity=4, shared_cpu_list='0,8'),
            dict(level=3, type='Unified', size='16384K', coherency_line_size=64, ways_of_associativity=16, shared_cpu_list='0-15')],
    zone_types=X86_ZONES, nodes=[0], numa_config=True,
    zones=[Zone(0, 'DMA', 1, 4095, 3998, 3840), Zone(0, 'DMA32', 4096, 1044480, 486366, 470112),
           Zone(0, 'Normal', 1048576, 3670016, 3670016, 3601440)],
    use=dict(AnonPages=5904316, Cached=6301840, Shmem=621204, Buffers=410388, SReclaimable=561032, SUnreclaim=240876,
             KernelStack=22816, PageTables=78132, Percpu=13568, Mapped=1251904, Dirty=1840, Writeback=0,
             AnonHugePages=2183168, Unevictable=132044, Mlocked=24, VmallocUsed=98304, Committed_AS=19804112,
             SwapCached=64212),
    swap_total_kb=8388604, swap_used_kb=412308,
    swaps=[dict(name='/dev/sda3', type='partition', size_kb=8388604, used_kb=412308, prio=-2)],
    hugepages={2048: (0, 0), 1048576: (0, 0)}, overcommit_ratio=50, vmalloc_total_kb=34359738367,
    directmap=(612332, 9732096, 6291456), directmap1g=True, min_free_kbytes=67584, watermark_scale_factor=10,
    rates=dict(fault=1840, alloc=2630, majfault=1.4, pgpgin=230, scan=310, swapin=0.7, swapout=1.6, dirtied=160, stall=0.0004),
    oom_kills=3, psi=(0.0, 0.08, 0.21, 48211093, 0.0, 0.05, 0.14, 39842210),
    thp='always', thp_defrag='madvise', lru_gen='0x0007', zswap=False, memory_block_size=0x8000000, frag=0.55,
    cgroup=dict(version='v2', self='0::/user.slice/user-1000.slice/session-2.scope\n',
                path='/user.slice/user-1000.slice/session-2.scope',
                files={'memory.current': 3301638144, 'memory.max': 'max', 'memory.high': 'max', 'memory.low': 0,
                       'memory.min': 0, 'memory.swap.current': 198344704, 'memory.swap.max': 'max',
                       'memory.peak': 5021597696, 'events': {'oom_kill': 1},
                       'stat': v2_stat(2405687296, 812646400, 98566144, 83361792)}),
    dmi=('Micro-Star International Co., Ltd.', 'MS-7C82', 'American Megatrends International, LLC.'),
    block=[dict(name='sda', sectors=1953525168, scheduler='[mq-deadline] kyber bfq none'),
           dict(name='sdb', sectors=3907029168, rotational=1, scheduler='mq-deadline kyber [bfq] none')] +
          [dict(name=f'loop{i}', sectors=0, scheduler='[none]') for i in range(8)],
    cmd_files={'_cmd/lsblk.json': lsblk_json([
        dict(name='sda', type='disk', size=1000204886016, children=[
            dict(name='sda1', type='part', size=1073741824, fstype='vfat'),
            dict(name='sda2', type='part', size=990741299200, fstype='ext4'),
            dict(name='sda3', type='part', size=8589934592, fstype='swap')]),
        dict(name='sdb', type='disk', size=2000398934016, children=[
            dict(name='sdb1', type='part', size=2000397795328, fstype='ext4')])])},
    commands={LSBLK: {'stdout': '_cmd/lsblk.json'}},
    mounts=std_mounts({'mp': '/', 'fs': 'ext4', 'src': '/dev/sda2', 'dev': '8:2', 'opts': 'rw,relatime'}, [
        {'mp': '/sys/firmware/efi/efivars', 'fs': 'efivarfs', 'src': 'efivarfs', 'dev': '0:29', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/tmp', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:40', 'super': 'rw,size=8147680k,nr_inodes=1048576,inode64'},
        {'mp': '/boot', 'fs': 'vfat', 'src': '/dev/sda1', 'dev': '8:1', 'super': 'rw,fmask=0022,dmask=0022,codepage=437,iocharset=ascii,shortname=mixed,utf8,errors=remount-ro'},
        {'mp': '/mnt/media', 'fs': 'ext4', 'src': '/dev/sdb1', 'dev': '8:17'},
        {'mp': '/proc/sys/fs/binfmt_misc', 'fs': 'binfmt_misc', 'src': 'binfmt_misc', 'dev': '0:41'},
        {'mp': '/run/user/1000', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:52', 'super': 'rw,size=1629532k,nr_inodes=407383,mode=700,uid=1000,gid=1000,inode64'},
        {'mp': '/run/user/1000/doc', 'fs': 'fuse.portal', 'src': 'portal', 'dev': '0:55', 'super': 'rw,user_id=1000,group_id=1000'},
        {'mp': '/var/lib/docker/overlay2/9d3b1f0c7a2e/merged', 'fs': 'overlay', 'src': 'overlay', 'dev': '0:61', 'super': 'rw,lowerdir=/var/lib/docker/overlay2/l/A1:/var/lib/docker/overlay2/l/B2,upperdir=/var/lib/docker/overlay2/9d3b1f0c7a2e/diff,workdir=/var/lib/docker/overlay2/9d3b1f0c7a2e/work'},
    ]),
    net=[dict(name='lo', type=772, operstate='unknown', mtu=65536),
         dict(name='enp3s0', type=1, operstate='up', dirs=['device']),
         dict(name='wlp4s0', type=1, operstate='down', dirs=['device', 'wireless']),
         dict(name='docker0', type=1, operstate='down', dirs=['bridge']),
         dict(name='tailscale0', type=65534, operstate='unknown', mtu=1280)],
    env={'XDG_SESSION_TYPE': 'wayland', 'XDG_CURRENT_DESKTOP': 'KDE', 'WAYLAND_DISPLAY': 'wayland-0', 'DISPLAY': ':0'},
    deny=['/proc/slabinfo'],
    probe_layout=dict(exe=0x400000, exe_dev='08:02', exe_inode=2491305, memfd_inode=1027, heap_arena=0xc000000000,
                      mmap_base=0x7f3c8e400000, stack=0x7ffd5c2a1000, vvar=0x7ffd5c3b2000, vmpte=148,
                      faults_before=412, extra_faults=2,
                      layout_runs=probe_runs('0x7ffd5c29fd48', '0x7f3c8e400000', '0x7ffe1a7c3a18', '0x7f81d2c00000',
                                             '0xc000010000', '0x400000')),
)
desktop['cpuinfo_block'] = x86_block('Intel(R) Core(TM) i7-10700K CPU @ 3.80GHz', 6, 165, 5, '0xfc', 3800.0, 16384,
                                     DESKTOP_FLAGS, '7599.80', '39 bits physical, 48 bits virtual',
                                     vmx='vnmi preemption_timer invvpid ept_x_only ept_ad ept_1gb flexpriority tsc_offset vtpr mtf vapic ept vpid unrestricted_guest ple shadow_vmcs pml ept_mode_based_exec',
                                     bugs='spectre_v1 spectre_v2 spec_store_bypass swapgs taa itlb_multihit srbds mmio_stale_data retbleed eibrs_pbrsb gds bhi rfds')
desktop['readme'] = """
# desktop-x86_64

A home desktop: Intel Core i7-10700K (8 cores, 16 threads, one socket), 16 GiB
of RAM, Arch Linux with kernel 6.12.10, KDE Plasma on Wayland, one SATA SSD and
one hard disk.

What it models and the behaviours it exercises:

- **systemd** as PID 1 (`/run/systemd/system` exists), cgroup v2, atlas run from
  a login session (`session-2.scope`). PID 1 sits in `/init.scope`, so
  `virt.pid1_cgroup_root` is false although no container is involved.
- **Swap partition** `/dev/sda3` (8 GiB, about 400 MiB in use); `SwapTotal -
  SwapFree` equals the `Used` column of `/proc/swaps`. zswap is built in but
  disabled.
- **THP `always`**: `AnonHugePages` is large, and the probe's `anon-written`
  region is backed by four 2 MiB pages, so touching 2048 pages costs only a
  handful of minor faults.
- **MGLRU** enabled (`lru_gen/enabled` = `0x0007`), PSI present.
- **Some OOM kills**: `oom_kill 3` in `/proc/vmstat`, one of them in the login
  session (`memory.events`).
- SMT: CPUs n and n+8 are siblings; caches are listed for cpu0 (L1/L2 shared
  with cpu8, L3 shared by all 16).
- Block devices: `sda` (non-rotational), `sdb` (rotational) and eight unused
  loop devices that `block.devices` must skip; `lsblk --json` works.
- Mounts include a user runtime directory, a FUSE document portal, an external
  disk under `/mnt/media` and a Docker overlay: none of these mount points may
  appear verbatim in `mounts.filesystems`.
- Interfaces: loopback, wired Ethernet, Wi-Fi (down), a Docker bridge and a
  WireGuard-style tunnel.

Zone `managed` pages sum exactly to `MemTotal`, buddyinfo sums to each zone's
free pages, and the watermarks follow from `min_free_kbytes = 67584` (the
value khugepaged sets with THP enabled on three populated zones).
"""

# ======================================================================
# rpi-arm64-noswap


def pi_block(c):
    return '\n'.join([f'processor\t: {c["cpu"]}', 'BogoMIPS\t: 108.00',
                      'Features\t: fp asimd evtstrm crc32 cpuid', 'CPU implementer\t: 0x41',
                      'CPU architecture: 8', 'CPU variant\t: 0x0', 'CPU part\t: 0xd08', 'CPU revision\t: 3'])


rpi = dict(
    name='rpi-arm64-noswap',
    description='Raspberry Pi 4 Model B (4 GB) on Raspberry Pi OS 64-bit, kernel 6.6: arm64, 4 KiB pages, 39-bit VA, no swap, no DMI, device tree, THP madvise',
    now='2025-03-08T09:15:00Z', arch='aarch64', release='6.6.51+rpt-rpi-v8', kver=(6, 6), page_size=4096,
    version='Linux version 6.6.51+rpt-rpi-v8 (debian-kernel@lists.debian.org) (gcc-12 (Debian 12.2.0-14) 12.2.0, GNU ld (GNU Binutils for Debian) 2.40) #1 SMP PREEMPT Debian 1:6.6.51-1+rpt3 (2024-10-08)',
    hostname='raspberrypi',
    os_release='PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"\nNAME="Debian GNU/Linux"\nVERSION_ID="12"\n'
               'VERSION="12 (bookworm)"\nVERSION_CODENAME=bookworm\nID=debian\nHOME_URL="https://www.debian.org/"\n'
               'SUPPORT_URL="https://www.debian.org/support"\nBUG_REPORT_URL="https://bugs.debian.org/"\n',
    uptime=3551847.20, idle_frac=0.97, pid1='systemd', pid1_cgroup='0::/init.scope\n', systemd=True,
    cpus=[{'cpu': i, 'pkg': 0, 'core': i, 'node': 0} for i in range(4)],
    cpuinfo_block=pi_block,
    cpuinfo_trailer='Revision\t: c03114\nSerial\t\t: 100000004f2a9c71\nModel\t\t: Raspberry Pi 4 Model B Rev 1.4\n',
    caches=[dict(level=1, type='Data', size='32K', coherency_line_size=64, ways_of_associativity=2, shared_cpu_list='0'),
            dict(level=1, type='Instruction', size='48K', coherency_line_size=64, ways_of_associativity=3, shared_cpu_list='0'),
            dict(level=2, type='Unified', size='1024K', coherency_line_size=64, ways_of_associativity=16, shared_cpu_list='0-3')],
    zone_types=['DMA', 'DMA32', 'Normal', 'Movable'], nodes=[0], numa_config=False, numa_sysfs=False,
    zones=[Zone(0, 'DMA', 0, 262144, 242688, 233472), Zone(0, 'DMA32', 262144, 770048, 755968, 738264)],
    dma_free=51318, cma=65536, cma_free=60112, cma_zone='DMA', zswap_built=True,
    use=dict(AnonPages=611420, Cached=1221880, Shmem=48212, Buffers=92416, SReclaimable=88204, SUnreclaim=52388,
             KernelStack=4208, PageTables=9824, Percpu=1024, Mapped=241336, Dirty=124, Writeback=0, AnonHugePages=0,
             Unevictable=16, Mlocked=16, VmallocUsed=21412, Committed_AS=1482240),
    swap_total_kb=0, swap_used_kb=0, swaps=[],
    hugepages={64: (0, 0), 2048: (0, 0), 32768: (0, 0), 1048576: (0, 0)}, overcommit_ratio=50,
    vmalloc_total_kb=259653632, min_free_kbytes=45056, watermark_scale_factor=10,
    rates=dict(fault=310, alloc=520, majfault=0.05, pgpgin=12, scan=4, dirtied=21),
    psi=(0.0, 0.0, 0.0, 1820433, 0.0, 0.0, 0.0, 1544120),
    thp='madvise', thp_defrag='madvise', lru_gen='0x0007', zswap=False, zswap_compressor='lzo', frag=0.35,
    cgroup=dict(version='v2', self='0::/user.slice/user-1000.slice/session-3.scope\n',
                path='/user.slice/user-1000.slice/session-3.scope',
                files={'memory.current': 31457280, 'memory.max': 'max', 'memory.high': 'max', 'memory.low': 0,
                       'memory.min': 0, 'memory.swap.current': 0, 'memory.swap.max': 'max', 'memory.peak': 88080384,
                       'events': {}, 'stat': v2_stat(18874368, 9437184, 0, 3145728)}),
    dt_model='Raspberry Pi 4 Model B Rev 1.4',
    dt_compatible='raspberrypi,4-model-b\x00brcm,bcm2711\x00',
    block=[dict(name='mmcblk0', sectors=62333952, scheduler='[mq-deadline] kyber bfq none')] +
          [dict(name=f'loop{i}', sectors=0, scheduler='[none]') for i in range(8)],
    cmd_files={'_cmd/lsblk.json': lsblk_json([
        dict(name='mmcblk0', type='disk', size=31914983424, children=[
            dict(name='mmcblk0p1', type='part', size=536870912, fstype='vfat'),
            dict(name='mmcblk0p2', type='part', size=31373918208, fstype='ext4')])])},
    commands={LSBLK: {'stdout': '_cmd/lsblk.json'}},
    mounts=std_mounts({'mp': '/', 'fs': 'ext4', 'src': '/dev/mmcblk0p2', 'dev': '179:2', 'opts': 'rw,noatime'}, [
        {'mp': '/boot/firmware', 'fs': 'vfat', 'src': '/dev/mmcblk0p1', 'dev': '179:1', 'super': 'rw,fmask=0022,dmask=0022,codepage=437,iocharset=ascii,shortname=mixed,errors=remount-ro'},
        {'mp': '/run/user/1000', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:38', 'super': 'rw,size=388640k,nr_inodes=97160,mode=700,uid=1000,gid=1000'},
    ], skip=('/sys/kernel/tracing', '/dev/hugepages', '/sys/fs/pstore')),
    net=[dict(name='eth0', type=1, operstate='up', dirs=['device']),
         dict(name='lo', type=772, operstate='unknown', mtu=65536),
         dict(name='wlan0', type=1, operstate='down', dirs=['device', 'wireless'])],
    env={'XDG_SESSION_TYPE': 'tty'},
    deny=['/proc/slabinfo'],
    probe_layout=dict(exe=0x10000, exe_dev='b3:02', exe_inode=262877, memfd_inode=2051, heap_arena=0x4000000000,
                      mmap_base=0x7f8c400000, stack=0x7fd31c8000, vvar=0x7f9e7ea000, vmpte=60,
                      faults_before=233, extra_faults=2,
                      layout_runs=probe_runs('0x7fd31c7c98', '0x7f8c400000', '0x7fe4a90e18', '0x7f9a2a6000',
                                             '0x4000010000', '0x10000')),
)
rpi['readme'] = """
# rpi-arm64-noswap

A Raspberry Pi 4 Model B with 4 GB of RAM running Raspberry Pi OS 64-bit
(Debian 12 "bookworm"), kernel 6.6.51+rpt-rpi-v8, used headless over ssh.

What it models and the behaviours it exercises:

- **arm64 `/proc/cpuinfo`**: no `model name`, no `vendor_id`, no `address
  sizes`, no `flags`. The CPU is identified only by `CPU implementer 0x41`
  (Arm) and `CPU part 0xd08` (Cortex-A72). Expect `cpu.model_name` and both
  `cpu.address_bits.*` to be `absent` with a note, and the four x86
  `cpu.flags.*` to be `unsupported`. The board trailer (`Revision`, `Serial`,
  `Model`) is present, as the Raspberry Pi kernel prints it; the serial is
  invented and atlas never reports it.
- **4 KiB pages, 39-bit virtual addresses** (the Raspberry Pi kernel's
  `CONFIG_ARM64_VA_BITS_39`): every probe address is below `0x8000000000`;
  `VmallocTotal` is correspondingly small.
- **No swap**: `/proc/swaps` has only its header, `SwapTotal` is 0; zswap is
  built in and off.
- **No DMI**: `/sys/class/dmi` does not exist; the device tree names the board
  (`/proc/device-tree/model`, NUL-terminated).
- **No NUMA and no memory hotplug** in this kernel configuration:
  `/sys/devices/system/node` and `/sys/devices/system/memory` are absent.
- Two populated zones, DMA (the first GiB, holding the 64 MiB CMA area) and
  DMA32; `Normal` exists but is empty because all 4 GB lie below 4 GiB.
- **THP `madvise`**: the probe does not madvise, so `anon-written` is mapped
  with 4 KiB pages and costs about one fault per page.
- An SD card (`mmcblk0`) and eight unused loop devices; `lsblk --json` works.
"""

# ======================================================================
# alpine-container

aws_cpus = [{'cpu': 0, 'pkg': 0, 'core': 0}, {'cpu': 1, 'pkg': 0, 'core': 1},
            {'cpu': 2, 'pkg': 0, 'core': 0}, {'cpu': 3, 'pkg': 0, 'core': 1}]
for i, c in enumerate(aws_cpus):
    c.update(siblings=4, cores=2, apic=[0, 2, 1, 3][i], node=0)

container = dict(
    name='alpine-container',
    description='Alpine 3.20 in a Docker container (512 MiB memory limit) on an EC2 host with kernel 6.8: PID 1 is sh, no systemd, host /proc/meminfo, restricted /proc and /sys',
    now='2025-06-02T14:03:27Z', arch='x86_64', release='6.8.0-1015-aws', kver=(6, 8), page_size=4096,
    version='Linux version 6.8.0-1015-aws (buildd@lcy02-amd64-031) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.2.0-23ubuntu4) 13.2.0, GNU ld (GNU Binutils for Ubuntu) 2.42) #16-Ubuntu SMP Mon Aug 19 19:38:17 UTC 2024',
    hostname='4f2c9a1b7e3d',
    os_release='NAME="Alpine Linux"\nID=alpine\nVERSION_ID=3.20.3\nPRETTY_NAME="Alpine Linux v3.20"\n'
               'HOME_URL="https://alpinelinux.org/"\nBUG_REPORT_URL="https://gitlab.alpinelinux.org/alpine/aports/-/issues"\n',
    os_release_paths=['/usr/lib/os-release'],
    uptime=1047210.66, idle_frac=0.78, pid1='sh', pid1_cgroup='0::/\n', systemd=False,
    cpus=aws_cpus,
    caches=[dict(level=1, type='Data', size='48K', coherency_line_size=64, ways_of_associativity=12, shared_cpu_list='0,2'),
            dict(level=1, type='Instruction', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0,2'),
            dict(level=2, type='Unified', size='1280K', coherency_line_size=64, ways_of_associativity=20, shared_cpu_list='0,2'),
            dict(level=3, type='Unified', size='55296K', coherency_line_size=64, ways_of_associativity=12, shared_cpu_list='0-3')],
    zone_types=X86_ZONES, nodes=[0], numa_config=True, pku=True,
    zones=[Zone(0, 'DMA', 1, 4095, 3998, 3840), Zone(0, 'DMA32', 4096, 1044480, 765946, 750512),
           Zone(0, 'Normal', 1048576, 3407872, 3407872, 3339744)],
    use=dict(AnonPages=3901228, Cached=7812604, Shmem=24412, Buffers=310228, SReclaimable=621040, SUnreclaim=181532,
             KernelStack=11984, PageTables=41208, Percpu=4352, Mapped=702144, Dirty=388, Writeback=0,
             AnonHugePages=0, Unevictable=27648, Mlocked=27648, VmallocUsed=37120, Committed_AS=6104212),
    unaccounted_frac=0.01, swap_total_kb=0, swap_used_kb=0, swaps=[],
    hugepages={2048: (0, 0), 1048576: (0, 0)}, overcommit_ratio=50, vmalloc_total_kb=34359738367,
    directmap=(239616, 7100416, 9437184), directmap1g=True, min_free_kbytes=67584, watermark_scale_factor=10,
    rates=dict(fault=2100, alloc=3900, majfault=0.3, pgpgin=640, scan=180, dirtied=410),
    psi=(0.0, 0.0, 0.0, 2211843, 0.0, 0.0, 0.0, 1997231),
    thp='madvise', thp_defrag='madvise', lru_gen='0x0007', zswap=False, zswap_compressor='lzo', frag=0.45,
    memory_block_size=0x8000000,
    cgroup=dict(version='v2', self='0::/\n', path='/', root_controllers='cpuset cpu io memory hugetlb pids rdma misc',
                files={'memory.current': 46137344, 'memory.max': 536870912, 'memory.high': 'max', 'memory.low': 0,
                       'memory.min': 0, 'memory.swap.current': 0, 'memory.swap.max': 536870912,
                       'memory.peak': 98566144, 'events': {'max': 37},
                       'stat': v2_stat(29360128, 12582912, 0, 4194304)}),
    dmi=('Amazon EC2', 'm6i.xlarge', 'Amazon EC2'),
    dockerenv=True,
    block=[dict(name='nvme0n1', sectors=62914560), dict(name='loop0', sectors=155616, ro=1, scheduler='[none]'),
           dict(name='loop1', sectors=79400, ro=1, scheduler='[none]')] +
          [dict(name=f'loop{i}', sectors=0, scheduler='[none]') for i in range(2, 8)],
    commands={LSBLK: {'missing': True}},
    shared_mounts=False,
    mounts=[
        {'mp': '/', 'fs': 'overlay', 'src': 'overlay', 'dev': '0:68', 'super': 'rw,lowerdir=/var/lib/docker/overlay2/l/QX3:/var/lib/docker/overlay2/l/ZK7,upperdir=/var/lib/docker/overlay2/5be1c0d2/diff,workdir=/var/lib/docker/overlay2/5be1c0d2/work'},
        {'mp': '/proc', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/dev', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:72', 'opts': 'rw,nosuid', 'super': 'rw,size=65536k,mode=755,inode64'},
        {'mp': '/dev/pts', 'fs': 'devpts', 'src': 'devpts', 'dev': '0:73', 'opts': 'rw,nosuid,noexec,relatime', 'super': 'rw,gid=5,mode=620,ptmxmode=666'},
        {'mp': '/sys', 'fs': 'sysfs', 'src': 'sysfs', 'dev': '0:74', 'opts': 'ro,nosuid,nodev,noexec,relatime', 'super': 'ro'},
        {'mp': '/sys/fs/cgroup', 'fs': 'cgroup2', 'src': 'cgroup', 'dev': '0:29', 'opts': 'ro,nosuid,nodev,noexec,relatime', 'super': 'rw,nsdelegate,memory_recursiveprot'},
        {'mp': '/dev/mqueue', 'fs': 'mqueue', 'src': 'mqueue', 'dev': '0:70', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/dev/shm', 'fs': 'tmpfs', 'src': 'shm', 'dev': '0:75', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,size=65536k,inode64'},
        {'mp': '/etc/resolv.conf', 'fs': 'ext4', 'src': '/dev/root', 'dev': '259:1', 'root': '/var/lib/docker/containers/4f2c9a1b7e3d/resolv.conf', 'super': 'rw,discard,errors=remount-ro'},
        {'mp': '/etc/hostname', 'fs': 'ext4', 'src': '/dev/root', 'dev': '259:1', 'root': '/var/lib/docker/containers/4f2c9a1b7e3d/hostname', 'super': 'rw,discard,errors=remount-ro'},
        {'mp': '/etc/hosts', 'fs': 'ext4', 'src': '/dev/root', 'dev': '259:1', 'root': '/var/lib/docker/containers/4f2c9a1b7e3d/hosts', 'super': 'rw,discard,errors=remount-ro'},
        {'mp': '/proc/bus', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'root': '/bus', 'opts': 'ro,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc/fs', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'root': '/fs', 'opts': 'ro,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc/irq', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'root': '/irq', 'opts': 'ro,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc/sys', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'root': '/sys', 'opts': 'ro,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc/sysrq-trigger', 'fs': 'proc', 'src': 'proc', 'dev': '0:71', 'root': '/sysrq-trigger', 'opts': 'ro,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc/interrupts', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:76', 'root': '/', 'opts': 'ro,relatime', 'super': 'ro,inode64'},
        {'mp': '/proc/kcore', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:72', 'root': '/null', 'opts': 'rw,nosuid', 'super': 'rw,size=65536k,mode=755,inode64'},
        {'mp': '/proc/keys', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:72', 'root': '/null', 'opts': 'rw,nosuid', 'super': 'rw,size=65536k,mode=755,inode64'},
        {'mp': '/proc/timer_list', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:72', 'root': '/null', 'opts': 'rw,nosuid', 'super': 'rw,size=65536k,mode=755,inode64'},
        {'mp': '/proc/scsi', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:77', 'opts': 'ro,relatime', 'super': 'ro,inode64'},
        {'mp': '/sys/firmware', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:78', 'opts': 'ro,relatime', 'super': 'ro,inode64'},
        {'mp': '/sys/devices/virtual/powercap', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:79', 'opts': 'ro,relatime', 'super': 'ro,inode64'},
    ],
    net=[dict(name='eth0', type=1, operstate='up'), dict(name='lo', type=772, operstate='unknown', mtu=65536)],
    env={},
    deny=['/proc/slabinfo', '/sys/kernel/security'],
    probe_layout=dict(exe=0x400000, exe_dev='00:44', exe_inode=1838263, memfd_inode=5, heap_arena=0xc000000000,
                      mmap_base=0x7f19b0c00000, stack=0x7ffc3b9e6000, vvar=0x7ffc3bbf1000, vmpte=136,
                      faults_before=388, extra_faults=3,
                      layout_runs=probe_runs('0x7ffc3b9e4f08', '0x7f19b0c00000', '0x7ffe71d2b9b8', '0x7fa44e400000',
                                             '0xc000010000', '0x400000')),
)
container['cpuinfo_block'] = x86_block('Intel(R) Xeon(R) Platinum 8375C CPU @ 2.90GHz', 6, 106, 6, '0xd0003e7', 2899.96,
                                       55296, AWS_FLAGS, '5799.92', '46 bits physical, 48 bits virtual',
                                       bugs='spectre_v1 spectre_v2 spec_store_bypass swapgs mmio_stale_data eibrs_pbrsb gds bhi')
container['readme'] = """
# alpine-container

Alpine Linux 3.20 running in a Docker container started with
`docker run -m 512m --cpus 1.5 --cpuset-cpus 0-1 -it alpine sh`, on an Amazon
EC2 m6i.xlarge host (4 vCPUs, 16 GiB) running Ubuntu's 6.8.0-1015-aws kernel.
Atlas runs from that shell.

What it models and the behaviours it exercises:

- **PID 1 is `sh`**, not an init system; `/run/systemd/system` does not exist,
  so `init.systemd_booted` is false.
- **Container evidence**: `/.dockerenv` exists; with a private cgroup
  namespace PID 1 sits at the cgroup root (`0::/`), which on its own proves
  nothing. DMI is readable through the read-only sysfs and names the *host*
  (Amazon EC2), and the CPU reports the `hypervisor` flag: a container on a VM.
- **Host kernel fields are visible**: `/proc/meminfo`, `zoneinfo`, `vmstat`
  and PSI describe the 16 GiB host (no lxcfs), while the container's own
  limit is `memory.max = 536870912` at the root of its cgroup namespace.
  `memory.events` shows the limit was hit 37 times (`max`) without an OOM kill.
- **Restricted `/proc` and `/sys`**: Docker's masked paths (`/proc/kcore`,
  `/proc/keys`, `/proc/timer_list`, `/sys/firmware`, ...) appear in
  `mountinfo` as tmpfs or `/dev/null` binds; `/sys/firmware` is empty, so there
  is no device tree; `/proc/slabinfo` and `/sys/kernel/security` are denied.
- **No `lsblk`** in the image (busybox has none): `block.lsblk` is `absent`,
  while `block.devices` still lists the host's EBS volume and the two snap loop
  devices that are in use (the six unused ones are skipped).
- `/etc/os-release` is a relative symbolic link to `../usr/lib/os-release`, as
  in the Alpine image.
- SMT siblings are not adjacent: cpu0 and cpu2 share a core.
- The only interfaces are `lo` and the container end of a veth pair (`eth0`,
  no backing device, so `virtual`).
- No environment variables from a desktop session: every `session.*` string
  is `absent` and both display flags are false.
"""

# ======================================================================
# numa-server-la57

icx = x86_cpus(2, 16, 2, core_ids=[0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], node_of_pkg={0: 0, 1: 1})
numa = dict(
    name='numa-server-la57',
    description='Two-socket Ice Lake server (Ubuntu 24.04, kernel 6.8): two NUMA nodes, 5-level paging, 1 GiB huge pages, zswap, swap file, THP never',
    now='2025-04-17T03:30:00Z', arch='x86_64', release='6.8.0-49-generic', kver=(6, 8), page_size=4096,
    version='Linux version 6.8.0-49-generic (buildd@lcy02-amd64-103) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.2.0-23ubuntu4) 13.2.0, GNU ld (GNU Binutils for Ubuntu) 2.42) #49-Ubuntu SMP PREEMPT_DYNAMIC Mon Nov  4 02:06:24 UTC 2024',
    hostname='db-node-07',
    os_release='PRETTY_NAME="Ubuntu 24.04.1 LTS"\nNAME="Ubuntu"\nVERSION_ID="24.04"\nVERSION="24.04.1 LTS (Noble Numbat)"\n'
               'VERSION_CODENAME=noble\nID=ubuntu\nID_LIKE=debian\nHOME_URL="https://www.ubuntu.com/"\n'
               'SUPPORT_URL="https://help.ubuntu.com/"\nBUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"\n'
               'PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"\n'
               'UBUNTU_CODENAME=noble\nLOGO=ubuntu-logo\n',
    uptime=7519330.18, idle_frac=0.62, pid1='systemd', pid1_cgroup='0::/init.scope\n', systemd=True,
    cpus=icx,
    caches=[dict(level=1, type='Data', size='48K', coherency_line_size=64, ways_of_associativity=12, shared_cpu_list='0,32'),
            dict(level=1, type='Instruction', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0,32'),
            dict(level=2, type='Unified', size='1280K', coherency_line_size=64, ways_of_associativity=20, shared_cpu_list='0,32'),
            dict(level=3, type='Unified', size='24576K', coherency_line_size=64, ways_of_associativity=12, shared_cpu_list='0-15,32-47')],
    zone_types=X86_ZONES, nodes=[0, 1], numa_config=True, numa_balancing=1, pku=True,
    zones=[Zone(0, 'DMA', 1, 4095, 3998, 3840), Zone(0, 'DMA32', 4096, 1044480, 486310, 470016),
           Zone(0, 'Normal', 1048576, 33030144, 33030144, 32560128),
           Zone(1, 'Normal', 34078720, 33554432, 33554432, 33030144)],
    use=dict(AnonPages=96012440, Cached=120004312, Shmem=34012216, Buffers=2101884, SReclaimable=6802116,
             SUnreclaim=2401332, KernelStack=52032, PageTables=1900412, Percpu=120320, Mapped=35504220, Dirty=182004,
             Writeback=0, AnonHugePages=0, Unevictable=0, Mlocked=0, VmallocUsed=610248, Committed_AS=182004132,
             SwapCached=410224, Zswap=1320104, Zswapped=5100412),
    unaccounted_frac=0.01, swap_total_kb=8388604, swap_used_kb=6050212,
    swaps=[dict(name='/swap.img', type='file\t', size_kb=8388604, used_kb=6050212, prio=-2)],
    hugepages={2048: (0, 0), 1048576: (16, 16)}, overcommit_ratio=50, vmalloc_total_kb=13743895347199,
    directmap=(1250304, 52000768, 216006656), directmap1g=True, min_free_kbytes=65023, watermark_scale_factor=10,
    swappiness=10, max_map_count=262144,
    rates=dict(fault=21000, alloc=48000, majfault=3.1, pgpgin=9100, scan=2600, swapin=18, swapout=22, dirtied=3100, stall=0.002),
    psi=(0.12, 0.31, 0.27, 961204883, 0.02, 0.09, 0.08, 702114906),
    thp='never', thp_defrag='never', lru_gen='0x0007', zswap=True, zswap_compressor='zstd', frag=0.7,
    memory_block_size=0x80000000,
    cgroup=dict(version='v2', self='0::/user.slice/user-1001.slice/session-14.scope\n',
                path='/user.slice/user-1001.slice/session-14.scope',
                files={'memory.current': 12582912, 'memory.max': 'max', 'memory.high': 'max', 'memory.low': 0,
                       'memory.min': 0, 'memory.swap.current': 0, 'memory.swap.max': 'max', 'memory.peak': 41943040,
                       'events': {}, 'stat': v2_stat(6291456, 5242880, 0, 1048576)}),
    dmi=('Supermicro', 'Super Server', 'American Megatrends International, LLC.'),
    block=[dict(name='sda', sectors=937703088, scheduler='[mq-deadline] kyber bfq none'), dict(name='nvme0n1', sectors=7501476528),
           dict(name='nvme1n1', sectors=7501476528)],
    cmd_files={'_cmd/lsblk.json': lsblk_json([
        dict(name='sda', type='disk', size=480103981056, children=[
            dict(name='sda1', type='part', size=1127219200, fstype='vfat'),
            dict(name='sda2', type='part', size=2147483648, fstype='ext4'),
            dict(name='sda3', type='part', size=476828041216, fstype='ext4')]),
        dict(name='nvme0n1', type='disk', size=3840755982336, children=[
            dict(name='nvme0n1p1', type='part', size=3840753885184, fstype='xfs')]),
        dict(name='nvme1n1', type='disk', size=3840755982336, children=[
            dict(name='nvme1n1p1', type='part', size=3840753885184, fstype='xfs')])])},
    commands={LSBLK: {'stdout': '_cmd/lsblk.json'}},
    mounts=std_mounts({'mp': '/', 'fs': 'ext4', 'src': '/dev/sda3', 'dev': '8:3'}, [
        {'mp': '/boot', 'fs': 'ext4', 'src': '/dev/sda2', 'dev': '8:2'},
        {'mp': '/boot/efi', 'fs': 'vfat', 'src': '/dev/sda1', 'dev': '8:1', 'super': 'rw,fmask=0077,dmask=0077,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro'},
        {'mp': '/var/lib/postgresql', 'fs': 'xfs', 'src': '/dev/nvme0n1p1', 'dev': '259:1', 'opts': 'rw,noatime', 'super': 'rw,attr2,inode64,logbufs=8,logbsize=32k,noquota'},
        {'mp': '/srv/pgwal', 'fs': 'xfs', 'src': '/dev/nvme1n1p1', 'dev': '259:3', 'opts': 'rw,noatime', 'super': 'rw,attr2,inode64,logbufs=8,logbsize=32k,noquota'},
        {'mp': '/dev/hugepages1G', 'fs': 'hugetlbfs', 'src': 'hugetlbfs', 'dev': '0:45', 'super': 'rw,pagesize=1024M'},
        {'mp': '/run/user/1001', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:50', 'super': 'rw,size=26425536k,nr_inodes=6606384,mode=700,uid=1001,gid=1001,inode64'},
    ]),
    net=[dict(name='eno1', type=1, operstate='up', dirs=['device']),
         dict(name='eno2', type=1, operstate='down', dirs=['device']),
         dict(name='lo', type=772, operstate='unknown', mtu=65536)],
    bonding_masters=True,
    env={'XDG_SESSION_TYPE': 'tty'},
    deny=['/proc/slabinfo'],
    probe_layout=dict(exe=0x400000, exe_dev='08:03', exe_inode=3934211, memfd_inode=4113, heap_arena=0xc000000000,
                      mmap_base=0x7f6a2b200000, stack=0x7ffe9c3e1000, vvar=0x7ffe9c5f6000, vmpte=164,
                      faults_before=455, extra_faults=3,
                      layout_runs=probe_runs('0x7ffe9c3dfd28', '0x7f6a2b200000', '0x7ffcd04a1ac8', '0x7f0e57a00000',
                                             '0xc000010000', '0x400000')),
)
numa['cpuinfo_block'] = x86_block('Intel(R) Xeon(R) Silver 4314 CPU @ 2.40GHz', 6, 106, 6, '0xd0003e7', 2400.0, 24576,
                                  ICX_FLAGS, '4800.00', '46 bits physical, 57 bits virtual',
                                  vmx='vnmi preemption_timer posted_intr invvpid ept_x_only ept_ad ept_1gb flexpriority apicv tsc_offset vtpr mtf vapic ept vpid unrestricted_guest vapic_reg vid ple shadow_vmcs pml ept_mode_based_exec tsc_scaling',
                                  bugs='spectre_v1 spectre_v2 spec_store_bypass swapgs mmio_stale_data eibrs_pbrsb gds bhi')
numa['readme'] = """
# numa-server-la57

A two-socket database server: 2 x Intel Xeon Silver 4314 (Ice Lake-SP,
16 cores and 32 threads each, 64 logical CPUs), 256 GiB of RAM split evenly
over two NUMA nodes, Ubuntu 24.04 with kernel 6.8.0-49-generic, used over ssh.

What it models and the behaviours it exercises:

- **Two NUMA nodes**: node 0 holds DMA, DMA32 and Normal, node 1 only Normal;
  per-node `meminfo` files sum to `/proc/meminfo`; CPUs 0-15 and 32-47 are on
  node 0. `numa_miss`/`numa_foreign` are non-zero and NUMA balancing is on.
- **5-level paging**: the CPU lists `la57`, `address sizes` reports 57 virtual
  bits and `VmallocTotal` is the 5-level value. User mappings still sit below
  2^47 (`0x7f...`), because the kernel only hands out higher addresses to
  programs that ask for them.
- **1 GiB huge pages**: sixteen are reserved at boot (eight per node,
  `Hugetlb: 16777216 kB`), mounted at `/dev/hugepages1G`; the default 2 MiB
  pool is empty, so `HugePages_Total` is 0.
- **zswap enabled** in front of a **swap file** (`/swap.img`, shown with type
  `file` in `/proc/swaps`); swap use includes the pages held compressed in
  zswap (`Zswapped`).
- **THP `never`** (set on the kernel command line, so `min_free_kbytes` keeps
  its square-root default): `AnonHugePages` is 0 and the probe's
  `anon-written` region costs about one minor fault per 4 KiB page.
- Large shared memory (`Shmem` about 32 GiB, a database buffer pool),
  `swappiness = 10`, `max_map_count = 262144`.
- Core ids repeat across the two packages, so cores must be counted per
  package (the sibling lists do this).
- Mount points for the data volumes (`/var/lib/postgresql`, `/srv/pgwal`) are
  not standard paths and must be replaced by `<path>`.
- `/sys/class/net/bonding_masters` is a plain file next to the interfaces and
  must not be reported as one.
"""

# ======================================================================
# old-vm-cgroupv1

oldvm_cpus = [{'cpu': 0, 'pkg': 0, 'core': 0, 'siblings': 1, 'cores': 1, 'apic': 0, 'node': 0},
              {'cpu': 1, 'pkg': 1, 'core': 0, 'siblings': 1, 'cores': 1, 'apic': 1, 'node': 0}]
V1_STAT = ('cache 498241536\nrss 702906368\nrss_huge 190840832\nshmem 9216000\nmapped_file 94208000\ndirty 90112\n'
           'writeback 0\nswap 274432000\npgpgin 81233410\npgpgout 80939022\npgfault 214533102\npgmajfault 210233\n'
           'inactive_anon 260231168\nactive_anon 451891200\ninactive_file 301989888\nactive_file 196251648\n'
           'unevictable 0\nhierarchical_memory_limit 9223372036854771712\nhierarchical_memsw_limit 9223372036854771712\n'
           'total_cache 498241536\ntotal_rss 702906368\ntotal_rss_huge 190840832\ntotal_shmem 9216000\n'
           'total_mapped_file 94208000\ntotal_dirty 90112\ntotal_writeback 0\ntotal_swap 274432000\n'
           'total_pgpgin 81233410\ntotal_pgpgout 80939022\ntotal_pgfault 214533102\ntotal_pgmajfault 210233\n'
           'total_inactive_anon 260231168\ntotal_active_anon 451891200\ntotal_inactive_file 301989888\n'
           'total_active_file 196251648\ntotal_unevictable 0\n')
oldvm = dict(
    name='old-vm-cgroupv1',
    description='KVM guest running CentOS 7 with a 4.19 kernel: no PSI, no MGLRU, cgroup v1, a nearly empty DMA zone, lsblk too old for --json',
    now='2024-11-05T11:20:44Z', arch='x86_64', release='4.19.113-300.el7.x86_64', kver=(4, 19), page_size=4096,
    version='Linux version 4.19.113-300.el7.x86_64 (mockbuild@x86-01.bsys.centos.org) (gcc version 4.8.5 20150623 (Red Hat 4.8.5-39) (GCC)) #1 SMP Mon Apr 6 15:15:39 UTC 2020',
    hostname='legacy-vm01',
    os_release='NAME="CentOS Linux"\nVERSION="7 (Core)"\nID="centos"\nID_LIKE="rhel fedora"\nVERSION_ID="7"\n'
               'PRETTY_NAME="CentOS Linux 7 (Core)"\nANSI_COLOR="0;31"\nCPE_NAME="cpe:/o:centos:centos:7"\n'
               'HOME_URL="https://www.centos.org/"\nBUG_REPORT_URL="https://bugs.centos.org/"\n\n'
               'CENTOS_MANTISBT_PROJECT="CentOS-7"\nCENTOS_MANTISBT_PROJECT_VERSION="7"\n'
               'REDHAT_SUPPORT_PRODUCT="centos"\nREDHAT_SUPPORT_PRODUCT_VERSION="7"\n\n',
    uptime=34741823.51, idle_frac=0.88, pid1='systemd',
    pid1_cgroup=('11:cpuset:/\n10:hugetlb:/\n9:memory:/\n8:pids:/\n7:blkio:/\n6:devices:/\n5:net_cls,net_prio:/\n'
                 '4:freezer:/\n3:cpu,cpuacct:/\n2:perf_event:/\n1:name=systemd:/\n'),
    systemd=True, cpus=oldvm_cpus, possible=[0, 1], kernel_max=5119,
    caches=[dict(level=1, type='Data', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0'),
            dict(level=1, type='Instruction', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0'),
            dict(level=2, type='Unified', size='4096K', coherency_line_size=64, ways_of_associativity=16, shared_cpu_list='0'),
            dict(level=3, type='Unified', size='16384K', coherency_line_size=64, ways_of_associativity=16, shared_cpu_list='0')],
    zone_types=['DMA', 'DMA32', 'Normal', 'Movable'], nodes=[0], numa_config=True,
    zones=[Zone(0, 'DMA', 1, 4095, 3998, 3977), Zone(0, 'DMA32', 4096, 520192, 520160, 498498)],
    dma_free=4,
    use=dict(AnonPages=689412, Cached=521004, Shmem=9000, Buffers=41212, SReclaimable=58044, SUnreclaim=39116,
             KernelStack=3904, PageTables=14812, Percpu=1840, Mapped=92004, Dirty=88, Writeback=0,
             AnonHugePages=186368, Unevictable=0, Mlocked=0, VmallocUsed=0, Committed_AS=2450212, SwapCached=22104),
    unaccounted_frac=0.015, swap_total_kb=1048572, swap_used_kb=268004,
    swaps=[dict(name='/dev/dm-1', type='partition', size_kb=1048572, used_kb=268004, prio=-2)],
    hugepages={2048: (0, 0), 1048576: (0, 0)}, overcommit_ratio=50, vmalloc_total_kb=34359738367,
    directmap=(116604, 2080768, 0), directmap1g=True,
    min_free_kbytes=45056, watermark_scale_factor=10,
    rates=dict(fault=310, alloc=620, majfault=0.006, pgpgin=9.4, scan=21, swapin=0.02, swapout=0.03, dirtied=11, stall=0.00001),
    thp='always', thp_defrag='madvise', zswap=False, zswap_compressor='lzo', memory_block_size=0x8000000, frag=0.8,
    cgroup=dict(version='v1',
                self=('11:cpuset:/\n10:hugetlb:/\n9:memory:/user.slice\n8:pids:/user.slice\n7:blkio:/user.slice\n'
                      '6:devices:/user.slice\n5:net_cls,net_prio:/\n4:freezer:/\n3:cpu,cpuacct:/user.slice\n'
                      '2:perf_event:/\n1:name=systemd:/user.slice/user-1000.slice/session-1.scope\n'),
                path='/user.slice',
                files={'memory.limit_in_bytes': '9223372036854771712', 'memory.soft_limit_in_bytes': '9223372036854771712',
                       'memory.usage_in_bytes': '1201147904', 'memory.max_usage_in_bytes': '1873190912',
                       'memory.failcnt': '0', 'memory.oom_control': 'oom_kill_disable 0\nunder_oom 0\noom_kill 0\n',
                       'memory.swappiness': '60', 'memory.use_hierarchy': '1', 'memory.stat': V1_STAT}),
    dmi=('QEMU', 'Standard PC (i440FX + PIIX, 1996)', 'SeaBIOS'),
    block=[dict(name='dm-0', sectors=35643392, rotational=1, scheduler='none'),
           dict(name='dm-1', sectors=2097152, rotational=1, scheduler='none'),
           dict(name='sr0', sectors=0, removable=1, rotational=1, scheduler='[mq-deadline] kyber bfq none'),
           dict(name='vda', sectors=41943040, rotational=1, scheduler='[mq-deadline] kyber bfq none')],
    commands={LSBLK: {'exit': 1, 'stderr': "lsblk: unrecognized option '--json'\nTry 'lsblk --help' for more information.\n"}},
    mounts=[
        {'mp': '/', 'fs': 'xfs', 'src': '/dev/mapper/centos-root', 'dev': '253:0', 'super': 'rw,attr2,inode64,noquota'},
        {'mp': '/sys', 'fs': 'sysfs', 'src': 'sysfs', 'dev': '0:18', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/proc', 'fs': 'proc', 'src': 'proc', 'dev': '0:4', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/dev', 'fs': 'devtmpfs', 'src': 'devtmpfs', 'dev': '0:6', 'opts': 'rw,nosuid', 'super': 'rw,size=995392k,nr_inodes=248848,mode=755'},
        {'mp': '/sys/kernel/security', 'fs': 'securityfs', 'src': 'securityfs', 'dev': '0:7', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/dev/shm', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:19', 'opts': 'rw,nosuid,nodev'},
        {'mp': '/dev/pts', 'fs': 'devpts', 'src': 'devpts', 'dev': '0:12', 'opts': 'rw,nosuid,noexec,relatime', 'super': 'rw,gid=5,mode=620,ptmxmode=000'},
        {'mp': '/run', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:20', 'opts': 'rw,nosuid,nodev', 'super': 'rw,mode=755'},
        {'mp': '/sys/fs/cgroup', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:21', 'opts': 'ro,nosuid,nodev,noexec', 'super': 'ro,mode=755'},
        {'mp': '/sys/fs/cgroup/systemd', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:22', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,xattr,release_agent=/usr/lib/systemd/systemd-cgroups-agent,name=systemd'},
        {'mp': '/sys/fs/pstore', 'fs': 'pstore', 'src': 'pstore', 'dev': '0:23', 'opts': 'rw,nosuid,nodev,noexec,relatime'},
        {'mp': '/sys/fs/cgroup/perf_event', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:24', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,perf_event'},
        {'mp': '/sys/fs/cgroup/cpu,cpuacct', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:25', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,cpu,cpuacct'},
        {'mp': '/sys/fs/cgroup/freezer', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:26', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,freezer'},
        {'mp': '/sys/fs/cgroup/net_cls,net_prio', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:27', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,net_cls,net_prio'},
        {'mp': '/sys/fs/cgroup/devices', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:28', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,devices'},
        {'mp': '/sys/fs/cgroup/blkio', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:29', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,blkio'},
        {'mp': '/sys/fs/cgroup/pids', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:30', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,pids'},
        {'mp': '/sys/fs/cgroup/memory', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:31', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,memory'},
        {'mp': '/sys/fs/cgroup/hugetlb', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:32', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,hugetlb'},
        {'mp': '/sys/fs/cgroup/cpuset', 'fs': 'cgroup', 'src': 'cgroup', 'dev': '0:33', 'opts': 'rw,nosuid,nodev,noexec,relatime', 'super': 'rw,cpuset'},
        {'mp': '/sys/kernel/config', 'fs': 'configfs', 'src': 'configfs', 'dev': '0:34', 'opts': 'rw,relatime'},
        {'mp': '/proc/sys/fs/binfmt_misc', 'fs': 'autofs', 'src': 'systemd-1', 'dev': '0:35', 'super': 'rw,fd=31,pgrp=1,timeout=0,minproto=5,maxproto=5,direct'},
        {'mp': '/dev/mqueue', 'fs': 'mqueue', 'src': 'mqueue', 'dev': '0:17', 'opts': 'rw,relatime'},
        {'mp': '/sys/kernel/debug', 'fs': 'debugfs', 'src': 'debugfs', 'dev': '0:8', 'opts': 'rw,relatime'},
        {'mp': '/dev/hugepages', 'fs': 'hugetlbfs', 'src': 'hugetlbfs', 'dev': '0:36', 'opts': 'rw,relatime', 'super': 'rw,pagesize=2M'},
        {'mp': '/boot', 'fs': 'xfs', 'src': '/dev/vda1', 'dev': '252:1', 'super': 'rw,attr2,inode64,noquota'},
        {'mp': '/run/user/1000', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:40', 'opts': 'rw,nosuid,nodev,relatime', 'super': 'rw,size=200992k,mode=700,uid=1000,gid=1000'},
    ],
    net=[dict(name='eth0', type=1, operstate='up', dirs=['device']), dict(name='lo', type=772, operstate='unknown', mtu=65536)],
    env={},
    deny=['/proc/slabinfo'],
    probe_layout=dict(exe=0x400000, exe_dev='fd:00', exe_inode=34218391, memfd_inode=24877, heap_arena=0xc000000000,
                      mmap_base=0x7f2e94c00000, stack=0x7ffdb6fb1000, vvar=0x7ffdb6ff5000, vmpte=96,
                      faults_before=301, extra_faults=2,
                      layout_runs=probe_runs('0x7ffdb6faf9e8', '0x7f2e94c00000', '0x7ffc0a1e7e48', '0x7f6c11400000',
                                             '0xc000010000', '0x400000')),
)
oldvm['cpuinfo_block'] = x86_block('Intel Xeon Processor (Skylake, IBRS)', 6, 85, 4, '0x1', 2194.842, 16384,
                                   QEMU_FLAGS, '4389.68', '40 bits physical, 48 bits virtual',
                                   bugs='cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs taa')
oldvm['readme'] = """
# old-vm-cgroupv1

A long-lived KVM guest (up for 402 days): 2 vCPUs, 2 GiB of RAM, CentOS 7
with the 4.19.113 kernel from the CentOS virtualization SIG, an LVM root and
an LVM swap volume on a virtio disk. Atlas runs from an ssh session.

What it models and the behaviours it exercises:

- **Kernel 4.19 interface formats**: `/proc/meminfo` has `MemAvailable` but no
  `KReclaimable`, `Zswap`, `FileHugePages` or `SecPageTables`, and prints
  `VmallocUsed: 0 kB` (as 4.4 to 5.2 did); `zoneinfo` has no `boost`, `promo`
  or `cma` lines and keeps `nr_kernel_stack` and `nr_page_table_pages` among
  the zone counters; `vmstat` has `workingset_refault` (not split into anon
  and file) and `nr_unstable` among the node counters; `smaps` has no
  `Pss_Dirty`, `FilePmdMapped` or `THPeligible`; `[vsyscall]` is `r-xp`.
- **No PSI** (added in 4.20): `/proc/pressure` does not exist.
- **No MGLRU** (added in 6.1): `/sys/kernel/mm/lru_gen` does not exist.
- **cgroup v1**: `/proc/self/cgroup` lists one line per hierarchy, the memory
  controller is mounted at `/sys/fs/cgroup/memory`, and the v2 files
  (`memory.max`, `memory.events`, ...) do not exist; the v1 limit is the
  "unlimited" value 9223372036854771712.
- **The DMA zone is nearly empty**: 4 free pages (16 KiB), below its own `min`
  watermark, while the machine is healthy. Only allocations that require
  ZONE_DMA can use those pages, and `lowmem_reserve` keeps everything else
  out, so kswapd is not woken for it. Free memory per zone is not a health
  measure on its own.
- **KVM evidence**: DMI says `QEMU` / `Standard PC (i440FX + PIIX, 1996)`, the
  CPU reports `hypervisor`, and QEMU's default topology presents the two vCPUs
  as **two sockets** of one core each. The virtio disk claims to rotate.
- **`lsblk` without `--json`** (util-linux 2.23): it exits 1 with
  "unrecognized option '--json'", so `block.lsblk` is `unsupported`.
- THP `always` (the RHEL 7 default), so the probe sees huge pages.
- Device-mapper volumes appear in `/sys/block` as `dm-0` and `dm-1`; the
  QEMU CD-ROM `sr0` has no medium (size 0) and is listed, since only loop and
  ram devices are skipped when empty.
- No `XDG_*` variables: systemd 219's pam module does not set
  `XDG_SESSION_TYPE`.
"""

# ======================================================================
# restricted

n100 = x86_cpus(1, 4, 1)
restricted = dict(
    name='restricted',
    description='Hardened Debian 12 mini PC (kernel 6.1): sysfs restricted to root, /proc mounted with hidepid, atlas run as a service, permission failures everywhere',
    now='2025-02-11T22:05:51Z', arch='x86_64', release='6.1.0-28-amd64', kver=(6, 1), page_size=4096,
    version='Linux version 6.1.0-28-amd64 (debian-kernel@lists.debian.org) (gcc-12 (Debian 12.2.0-14) 12.2.0, GNU ld (GNU Binutils for Debian) 2.40) #1 SMP PREEMPT_DYNAMIC Debian 6.1.119-1 (2024-11-22)',
    hostname='hardened-box',
    os_release='PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"\nNAME="Debian GNU/Linux"\nVERSION_ID="12"\n'
               'VERSION="12 (bookworm)"\nVERSION_CODENAME=bookworm\nID=debian\nHOME_URL="https://www.debian.org/"\n'
               'SUPPORT_URL="https://www.debian.org/support"\nBUG_REPORT_URL="https://bugs.debian.org/"\n',
    uptime=528641.09, idle_frac=0.95, pid1='systemd', pid1_cgroup='0::/init.scope\n', systemd=True,
    cpus=n100,
    caches=[dict(level=1, type='Data', size='32K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0'),
            dict(level=1, type='Instruction', size='64K', coherency_line_size=64, ways_of_associativity=8, shared_cpu_list='0'),
            dict(level=2, type='Unified', size='2048K', coherency_line_size=64, ways_of_associativity=16, shared_cpu_list='0-3'),
            dict(level=3, type='Unified', size='6144K', coherency_line_size=64, ways_of_associativity=12, shared_cpu_list='0-3')],
    zone_types=X86_ZONES, nodes=[0], numa_config=True,
    zones=[Zone(0, 'DMA', 1, 4095, 3999, 3840), Zone(0, 'DMA32', 4096, 1044480, 531190, 520014),
           Zone(0, 'Normal', 1048576, 1572864, 1572864, 1527986)],
    use=dict(AnonPages=1210412, Cached=2904312, Shmem=31208, Buffers=181004, SReclaimable=210332, SUnreclaim=88116,
             KernelStack=6208, PageTables=19412, Percpu=2432, Mapped=301204, Dirty=212, Writeback=0,
             AnonHugePages=411648, Unevictable=0, Mlocked=0, VmallocUsed=29812, Committed_AS=2804116),
    swap_total_kb=2097148, swap_used_kb=0,
    swaps=[dict(name='/swapfile', type='file\t', size_kb=2097148, used_kb=0, prio=-2)],
    hugepages={2048: (0, 0), 1048576: (0, 0)}, overcommit_ratio=50, vmalloc_total_kb=34359738367,
    directmap=(210400, 6031360, 2097152), directmap1g=True, min_free_kbytes=67584, watermark_scale_factor=10,
    rates=dict(fault=520, alloc=1100, majfault=0.02, pgpgin=40, scan=12, dirtied=44),
    psi=(0.0, 0.0, 0.0, 412093, 0.0, 0.0, 0.0, 330217),
    thp='always', thp_defrag='madvise', lru_gen='0x0000', zswap=False, zswap_compressor='lzo',
    memory_block_size=0x8000000, frag=0.3,
    cgroup=dict(version='v2', self='0::/system.slice/atlas-snapshot.service\n',
                path='/system.slice/atlas-snapshot.service',
                files={'memory.current': 22020096, 'memory.max': 1073741824, 'memory.high': 'max', 'memory.low': 0,
                       'memory.min': 0, 'memory.swap.current': 0, 'memory.swap.max': 0, 'memory.peak': 25165824,
                       'events': {}, 'stat': v2_stat(14680064, 5242880, 0, 2097152)}),
    dmi=('Default string', 'Default string', 'American Megatrends International, LLC.'),
    block=[dict(name='sda', sectors=500118192)],
    commands={LSBLK: {'exit': 32, 'stderr': 'lsblk: failed to access sysfs directory: /sys/dev/block: Permission denied\n'}},
    mounts=std_mounts({'mp': '/', 'fs': 'ext4', 'src': '/dev/sda2', 'dev': '8:2', 'super': 'rw,errors=remount-ro'}, [
        {'mp': '/boot/efi', 'fs': 'vfat', 'src': '/dev/sda1', 'dev': '8:1', 'super': 'rw,fmask=0077,dmask=0077,codepage=437,iocharset=ascii,shortname=mixed,utf8,errors=remount-ro'},
        {'mp': '/tmp', 'fs': 'tmpfs', 'src': 'tmpfs', 'dev': '0:44', 'opts': 'rw,nosuid,nodev,noexec', 'super': 'rw,size=4103680k,nr_inodes=1048576,inode64'},
    ]),
    net=[dict(name='enp1s0', type=1, operstate='up', dirs=['device']), dict(name='lo', type=772, operstate='unknown', mtu=65536)],
    env={},
    deny=['/proc/1', '/proc/slabinfo', '/proc/zoneinfo', '/proc/buddyinfo', '/proc/pressure', '/proc/schedstat',
          '/sys/block', '/sys/class', '/sys/devices/system/cpu', '/sys/devices/system/memory',
          '/sys/devices/system/node', '/sys/firmware', '/sys/kernel', '/sys/module'],
    probe_layout=dict(exe=0x400000, exe_dev='08:02', exe_inode=1311892, memfd_inode=2219, heap_arena=0xc000000000,
                      mmap_base=0x7f5d27e00000, stack=0x7ffef2b4d000, vvar=0x7ffef2bd0000, vmpte=140,
                      faults_before=377, extra_faults=2,
                      layout_runs=probe_runs('0x7ffef2b4bd38', '0x7f5d27e00000', '0x7fff3a6c19a8', '0x7fd8c3200000',
                                             '0xc000010000', '0x400000')),
)
restricted['cpuinfo_block'] = x86_block('Intel(R) N100', 6, 190, 0, '0x1a', 806.0, 6144, N100_FLAGS, '1612.80',
                                        '39 bits physical, 48 bits virtual',
                                        bugs='spectre_v1 spectre_v2 spec_store_bypass swapgs eibrs_pbrsb rfds bhi')
restricted['readme'] = """
# restricted

A small Intel N100 mini PC (4 cores, 8 GiB) running Debian 12 with kernel
6.1, hardened in the style of security-focused derivatives: sysfs is readable
only by root, `/proc` is mounted with `hidepid=noaccess`, and a few procfs
files that leak allocator state are made root-only. Atlas runs as a systemd
service (`atlas-snapshot.service`, `MemoryMax=1G`) under an unprivileged
user.

What it models and the behaviours it exercises: `_meta.json` denies, with
EACCES, every path below

- `/proc/1` (hidepid): `init.pid1_comm` and `virt.pid1_cgroup_root` are
  `permission_denied`;
- `/proc/slabinfo`, `/proc/zoneinfo`, `/proc/buddyinfo`, `/proc/pressure`;
- `/sys/devices/system/cpu`: the CPU counts fall back to `/proc/cpuinfo`
  (`physical id`/`core id`), and `cpu.caches` and `cpu.online` are denied;
- `/sys/devices/system/node`, `/sys/devices/system/memory`, `/sys/kernel`
  (THP, MGLRU, huge pages), `/sys/module` (zswap), `/sys/firmware`;
- `/sys/class` (DMI and network interfaces) and `/sys/block`; `lsblk` fails
  the same way and says so ("Permission denied", exit 32), which atlas
  reports as `permission_denied`.

The files behind the denied paths are present anyway, written as this kernel
would print them, so the root also shows what root would have seen. Still
readable: `/proc/meminfo`, `/proc/vmstat`, `/proc/swaps` (a swap file, unused),
`/proc/sys/vm/*`, `/proc/self/*`, the service's own cgroup (with
`memory.max = 1073741824` and `memory.swap.max = 0`), and the probe data,
since a process may always read its own `/proc/self` files. No session
variables are set for a service.
"""

MACHINES = [desktop, rpi, container, numa, oldvm, restricted]
