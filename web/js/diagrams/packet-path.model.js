// The rows of the packet-path diagram: each layer of the stack with what it
// does to a packet on the way out and on the way in, and, where this
// machine's snapshot has it, the value that layer uses here. Pure; tested
// in web/test/diagram-packet-path.test.mjs.

import { formatCount } from '../lib/format.js';

/** Every id the diagram reads; all are optional. */
export const IDS = [
  'net.sysctl.ipv4.tcp_congestion_control',
  'derived.net.default_route_interface',
  'net.conntrack.count',
  'derived.net.default_route_qdisc',
  'derived.net.default_route_tx_queues',
  'net.sysctl.core.default_qdisc',
  'derived.net.default_route_mtu',
  'net.sysctl.core.netdev_budget',
  'net.sysctl.core.netdev_max_backlog',
  'derived.net.softnet.dropped',
];

const LAYERS = [
  {
    layer: 'Socket',
    send: { title: 'send()', body: 'Copies the data into the socket’s send buffer and returns; nothing has been transmitted yet.' },
    receive: { title: 'recv()', body: 'Copies data out of the socket’s receive queue; the reading program was woken when it arrived.' },
  },
  {
    layer: 'Protocol',
    send: { title: 'TCP', body: 'Window and congestion window decide how much may go now; builds segments up to 64 KiB.' },
    receive: { title: 'TCP', body: 'Matches the segment to its connection, acknowledges it, appends the data in order.' },
  },
  {
    layer: 'IP',
    send: { title: 'Routing and netfilter', body: 'Longest matching prefix picks interface and gateway (cached in the socket); output and postrouting hooks run.' },
    receive: { title: 'Routing and netfilter', body: 'Local address or forward? Prerouting and input (or forward) hooks run around the decision.' },
  },
  {
    layer: 'Device',
    send: { title: 'Neighbour and qdisc', body: 'ARP or neighbour discovery finds the next hop’s MAC; the queueing discipline orders waiting packets.' },
    receive: { title: 'GRO and backlog', body: 'Segments of one flow are merged; packets steered to another CPU wait in its backlog.' },
  },
  {
    layer: 'Driver',
    send: { title: 'Transmit ring', body: 'The driver posts descriptors; byte queue limits cap how much waits there.' },
    receive: { title: 'NAPI poll', body: 'The receive softirq polls the ring in batches, interrupts masked while work remains.' },
  },
  {
    layer: 'NIC',
    send: { title: 'NIC transmits', body: 'Reads the data by DMA, cuts large segments into frames (TSO), adds checksums.' },
    receive: { title: 'NIC receives', body: 'Writes frames into the receive ring by DMA, hashes flows to queues (RSS), raises an interrupt.' },
  },
];

/**
 * Builds the rows from `get(id)`, which returns a record from
 * inputs.read(): {ok, value, status, ...}. Each row is
 * {layer, send: {title, body, here}, receive: {title, body, here}}, where
 * `here` lists sentences about this machine (empty when nothing applies).
 * `missing` lists the records that could not be read, `observed` whether
 * any value of this machine was drawn.
 */
export function pathModel(get) {
  const rec = (id) => get(id) ?? { id, ok: false, status: 'missing' };
  const ok = (id) => rec(id).ok;
  const val = (id) => rec(id).value;
  const count = (id) => formatCount(val(id));
  const used = new Set();
  const use = (...ids) => ids.forEach((id) => used.add(id));

  const here = { send: LAYERS.map(() => []), receive: LAYERS.map(() => []) };
  const iface = ok('derived.net.default_route_interface') ? val('derived.net.default_route_interface') : null;

  if (ok('net.sysctl.ipv4.tcp_congestion_control')) {
    here.send[1].push(`Congestion control: ${val('net.sysctl.ipv4.tcp_congestion_control')}`);
    use('net.sysctl.ipv4.tcp_congestion_control');
  }
  if (iface) {
    here.send[2].push(`Default route: ${iface}`);
    use('derived.net.default_route_interface');
  }
  const ct = rec('net.conntrack.count');
  if (ct.ok) {
    here.send[2].push(`Connection tracking: ${count('net.conntrack.count')} flows`);
    here.receive[2].push(`Connection tracking: ${count('net.conntrack.count')} flows`);
    use('net.conntrack.count');
  } else if (ct.status === 'absent') {
    here.send[2].push('Connection tracking: not loaded');
    here.receive[2].push('Connection tracking: not loaded');
    use('net.conntrack.count');
  }
  if (ok('derived.net.default_route_qdisc')) {
    let q = `Qdisc: ${val('derived.net.default_route_qdisc')}`;
    if (iface) q += ` on ${iface}`;
    if (ok('derived.net.default_route_tx_queues') && val('derived.net.default_route_tx_queues') > 1) {
      q += `, ${count('derived.net.default_route_tx_queues')} transmit queues`;
      use('derived.net.default_route_tx_queues');
    }
    here.send[3].push(q);
    use('derived.net.default_route_qdisc');
  } else if (ok('net.sysctl.core.default_qdisc')) {
    here.send[3].push(`Default qdisc for new interfaces: ${val('net.sysctl.core.default_qdisc')}`);
    use('net.sysctl.core.default_qdisc');
  }
  if (ok('net.sysctl.core.netdev_max_backlog')) {
    let b = `Backlog limit: ${count('net.sysctl.core.netdev_max_backlog')} packets per CPU`;
    if (ok('derived.net.softnet.dropped')) {
      b += `; ${count('derived.net.softnet.dropped')} dropped since boot`;
      use('derived.net.softnet.dropped');
    }
    here.receive[3].push(b);
    use('net.sysctl.core.netdev_max_backlog');
  }
  if (ok('net.sysctl.core.netdev_budget')) {
    here.receive[4].push(`Budget: ${count('net.sysctl.core.netdev_budget')} packets per softirq run`);
    use('net.sysctl.core.netdev_budget');
  }
  if (ok('derived.net.default_route_mtu')) {
    here.send[5].push(`Frames of at most ${count('derived.net.default_route_mtu')} bytes (MTU)`);
    use('derived.net.default_route_mtu');
  }

  const rows = LAYERS.map((l, i) => ({
    layer: l.layer,
    send: { ...l.send, here: here.send[i] },
    receive: { ...l.receive, here: here.receive[i] },
  }));
  // An absent conntrack count is an answer (not loaded), not a gap.
  const missing = IDS.map(rec).filter((r) => !r.ok && !(r.id === 'net.conntrack.count' && r.status === 'absent'));
  return { rows, missing, observed: used.size > 0, used: IDS.filter((id) => used.has(id)) };
}
