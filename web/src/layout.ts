export const MARGIN = 24;

export interface Metrics {
  colW: number;
  rowH: number;
  nodeW: number;
  nodeH: number;
}

export const COMPACT: Metrics = { colW: 260, rowH: 64, nodeW: 200, nodeH: 40 };
export const CARD: Metrics = { colW: 288, rowH: 120, nodeW: 240, nodeH: 104 };

export interface Wave {
  url: string;
  blocking: string[] | null;
}

export function waveDepths(rows: Wave[]): Map<string, number> {
  const byURL = new Map(rows.map((r) => [r.url, r]));
  const depth = new Map<string, number>();
  const visiting = new Set<string>();
  const depthOf = (url: string): number => {
    const known = depth.get(url);
    if (known !== undefined) return known;
    if (visiting.has(url)) return 0;
    visiting.add(url);
    let d = 0;
    for (const blocker of byURL.get(url)?.blocking ?? []) {
      if (byURL.has(blocker)) d = Math.max(d, depthOf(blocker) + 1);
    }
    visiting.delete(url);
    depth.set(url, d);
    return d;
  };
  for (const row of rows) depthOf(row.url);
  return depth;
}

export function layoutWaves<Row extends Wave>(
  rows: Row[],
  m: Metrics = COMPACT,
): (Row & { col: number; x: number; y: number })[] {
  const depth = waveDepths(rows);
  return stackByColumn(
    rows.map((row) => ({ ...row, col: depth.get(row.url) ?? 0 })),
    m,
  );
}

export function waveLabel(col: number): string {
  return `Wave ${col + 1}`;
}

export function stackByColumn<T extends { col: number }>(
  nodes: T[],
  m: Metrics = COMPACT,
): (T & { x: number; y: number })[] {
  const yByCol = new Map<number, number>();
  return nodes.map((n) => {
    const y = yByCol.get(n.col) ?? MARGIN;
    yByCol.set(n.col, y + m.rowH);
    return { ...n, x: MARGIN + n.col * m.colW, y };
  });
}

export interface Edge<T> {
  from: T;
  to: T;
}

export function edgesFor<T extends { url: string }>(
  nodes: T[],
  blockedBy: (node: T) => string[] | null | undefined,
): Edge<T>[] {
  const byURL = new Map(nodes.map((n) => [n.url, n]));
  const edges: Edge<T>[] = [];
  for (const node of nodes) {
    for (const blockerURL of blockedBy(node) || []) {
      const from = byURL.get(blockerURL);
      if (from) edges.push({ from, to: node });
    }
  }
  return edges;
}

export function edgePath(edge: Edge<{ x: number; y: number }>, m: Metrics = COMPACT): string {
  const x1 = edge.from.x + m.nodeW;
  const y1 = edge.from.y + m.nodeH / 2;
  const x2 = edge.to.x;
  const y2 = edge.to.y + m.nodeH / 2;
  const dx = Math.max(40, (x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}

export function bounds(
  nodes: { col: number; y: number }[],
  m: Metrics = COMPACT,
): { width: number; height: number } {
  const maxCol = nodes.reduce((acc, n) => Math.max(acc, n.col), 0);
  const maxY = nodes.reduce((acc, n) => Math.max(acc, n.y), 0);
  return { width: MARGIN * 2 + maxCol * m.colW + m.nodeW, height: maxY + m.nodeH + MARGIN };
}

export function traceChain<T extends { url: string }>(url: string, edges: Edge<T>[]): Set<string> {
  const upstream = new Map<string, string[]>();
  const downstream = new Map<string, string[]>();
  for (const e of edges) {
    upstream.set(e.to.url, [...(upstream.get(e.to.url) ?? []), e.from.url]);
    downstream.set(e.from.url, [...(downstream.get(e.from.url) ?? []), e.to.url]);
  }
  const traced = new Set([url]);
  for (const adjacent of [upstream, downstream]) {
    const queue = [url];
    const seen = new Set([url]);
    while (queue.length > 0) {
      for (const next of adjacent.get(queue.pop() as string) ?? []) {
        if (seen.has(next)) continue;
        seen.add(next);
        traced.add(next);
        queue.push(next);
      }
    }
  }
  return traced;
}
