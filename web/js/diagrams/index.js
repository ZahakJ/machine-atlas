// Diagram registry (ARCHITECTURE.md §9). Each entry is { title, mount(container,
// ctx) -> unmount }, where ctx = { obs(id), derived(id), navigate(hash) } and
// optionally inspect(id, opener) to open the side inspector in place of
// following a citation link.
//
// Data-bound diagrams draw only what the snapshot says and cite the ids they
// draw; authored diagrams (GENERAL) explain mechanisms true of Linux in general.

import * as addressSpaceMap from './address-space-map.js';
import * as meminfoBreakdown from './meminfo-breakdown.js';
import * as zoneWatermarks from './zone-watermarks.js';
import * as probeRegions from './probe-regions.js';
import * as faultFlow from './fault-flow.js';
import * as lruLists from './lru-lists.js';
import * as memoryLayers from './memory-layers.js';
import * as packetPath from './packet-path.js';
import * as cpuTopology from './cpu-topology.js';
import * as ioStack from './io-stack.js';

const entry = (m) => ({ title: m.title, mount: m.mount });

export const diagrams = {
  'address-space-map': entry(addressSpaceMap),
  'meminfo-breakdown': entry(meminfoBreakdown),
  'zone-watermarks': entry(zoneWatermarks),
  'probe-regions': entry(probeRegions),
  'fault-flow': entry(faultFlow),
  'lru-lists': entry(lruLists),
  'memory-layers': entry(memoryLayers),
  'packet-path': entry(packetPath),
  'cpu-topology': entry(cpuTopology),
  'io-stack': entry(ioStack),
};
