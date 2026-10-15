"""Regenerate fixtures/roots from machines.py. Usage: python3 -I gen.py <repo>/fixtures/roots"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from machine import generate  # noqa: E402
from machines import MACHINES  # noqa: E402

base = sys.argv[1]
os.makedirs(base, exist_ok=True)
for m in MACHINES:
    r = generate(base, m)
    if m['name'] == 'alpine-container':
        os.makedirs(os.path.join(r.dir, 'etc'), exist_ok=True)
        os.symlink('../usr/lib/os-release', os.path.join(r.dir, 'etc/os-release'))
    print(m['name'], m['_mi']['MemTotal'], m['_mi']['MemFree'], m['_mi']['MemAvailable'])
