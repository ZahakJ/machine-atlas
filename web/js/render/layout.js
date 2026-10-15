// Layered graph layout for the prerequisite graph and the component map.
// Pure: it assigns each node a column and a row; the SVG renderers turn
// those into coordinates.
//
// Columns are longest-path layers, so every edge points rightwards: a node
// sits one column after its furthest prerequisite. Within a column, nodes are
// ordered by the mean row of their predecessors (one barycentre sweep), which
// removes most crossings in graphs this size without the cost of a full
// Sugiyama layout. Ties keep the input order, so the result is deterministic.

/**
 * nodes: [{id}] in preferred order. edges: [{from, to}] meaning "from comes
 * before to". Edges naming unknown nodes are ignored. A cycle (the
 * curriculum lint forbids them, but data is data) cannot loop forever: a
 * node's column is capped at the node count.
 *
 * Returns {columns: [[id]], place: Map id → {col, row}}.
 */
export function layered(nodes, edges) {
  const ids = nodes.map((n) => n.id);
  const known = new Set(ids);
  const preds = new Map(ids.map((id) => [id, []]));
  for (const e of edges) {
    if (known.has(e.from) && known.has(e.to) && e.from !== e.to) preds.get(e.to).push(e.from);
  }

  const col = new Map();
  const visiting = new Set();
  const depth = (id) => {
    if (col.has(id)) return col.get(id);
    if (visiting.has(id)) return 0; // a cycle: break it here
    visiting.add(id);
    let d = 0;
    for (const p of preds.get(id)) d = Math.max(d, depth(p) + 1);
    visiting.delete(id);
    d = Math.min(d, ids.length);
    col.set(id, d);
    return d;
  };
  ids.forEach(depth);

  // A root that only leads somewhere sits just before its first
  // successor rather than at the far left, so a concept with a late
  // prerequisite that is not drawn does not pull a long edge across.
  const succs = new Map(ids.map((id) => [id, []]));
  for (const [to, ps] of preds) for (const p of ps) succs.get(p).push(to);
  for (const id of [...ids].sort((a, b) => col.get(b) - col.get(a))) {
    if (preds.get(id).length === 0 && succs.get(id).length > 0) {
      col.set(id, Math.max(0, Math.min(...succs.get(id).map((s) => col.get(s))) - 1));
    }
  }

  const columns = [];
  for (const id of ids) {
    const c = col.get(id);
    while (columns.length <= c) columns.push([]);
    columns[c].push(id);
  }

  const row = new Map();
  columns[0]?.forEach((id, i) => row.set(id, i));
  for (let c = 1; c < columns.length; c++) {
    const order = new Map(columns[c].map((id, i) => [id, i]));
    const key = (id) => {
      const ps = preds.get(id).filter((p) => row.has(p));
      return ps.length ? ps.reduce((s, p) => s + row.get(p), 0) / ps.length : order.get(id);
    };
    columns[c].sort((a, b) => key(a) - key(b) || order.get(a) - order.get(b));
    columns[c].forEach((id, i) => row.set(id, i));
  }

  const place = new Map(ids.map((id) => [id, { col: col.get(id), row: row.get(id) }]));
  return { columns, place };
}

/**
 * A smooth left-to-right connector between two points, as an SVG path:
 * a cubic curve whose control points pull horizontally, so edges leave and
 * enter nodes level.
 */
export function curve(x1, y1, x2, y2) {
  const dx = Math.max(24, (x2 - x1) / 2);
  const r = (n) => Math.round(n * 10) / 10;
  return `M${r(x1)},${r(y1)} C${r(x1 + dx)},${r(y1)} ${r(x2 - dx)},${r(y2)} ${r(x2)},${r(y2)}`;
}
