
export const COL_W = 260;
export const ROW_H = 64;
// Tailwind's static scanner needs a literal class, so each island's node button carries its own
// literal `w-[200px]` rather than an interpolated one, and that literal must be kept in sync with
// NODE_W by hand.
export const NODE_W = 200;
export const NODE_H = 40;
export const MARGIN = 24;

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

export function layoutWaves<Row extends Wave>(rows: Row[]): (Row & { col: number; x: number; y: number })[] {
  const depth = waveDepths(rows);
  return stackByColumn(rows.map((row) => ({ ...row, col: depth.get(row.url) ?? 0 })));
}

export function waveLabel(col: number): string {
  return `Wave ${col + 1}`;
}

export function stackByColumn<T extends { col: number }>(nodes: T[]): (T & { x: number; y: number })[] {
  const yByCol = new Map<number, number>();
  return nodes.map((n) => {
    const y = yByCol.get(n.col) ?? MARGIN;
    yByCol.set(n.col, y + ROW_H);
    return { ...n, x: MARGIN + n.col * COL_W, y };
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

export function edgePath(edge: Edge<{ x: number; y: number }>): string {
  const x1 = edge.from.x + NODE_W;
  const y1 = edge.from.y + NODE_H / 2;
  const x2 = edge.to.x;
  const y2 = edge.to.y + NODE_H / 2;
  const dx = Math.max(40, (x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}

export function bounds(nodes: { col: number; y: number }[]): { width: number; height: number } {
  const maxCol = nodes.reduce((m, n) => Math.max(m, n.col), 0);
  const maxY = nodes.reduce((m, n) => Math.max(m, n.y), 0);
  return { width: MARGIN * 2 + maxCol * COL_W + NODE_W, height: maxY + NODE_H + MARGIN };
}

export function traceChain<T extends { url: string }>(url: string, edges: Edge<T>[]): Set<string> {
  const walk = (next: (e: Edge<T>) => [string, string]): string[] => {
    const seen: string[] = [];
    const queue = [url];
    while (queue.length > 0) {
      const at = queue.pop() as string;
      for (const e of edges) {
        const [near, far] = next(e);
        if (near === at && !seen.includes(far) && far !== url) {
          seen.push(far);
          queue.push(far);
        }
      }
    }
    return seen;
  };
  return new Set([
    url,
    ...walk((e) => [e.to.url, e.from.url]),
    ...walk((e) => [e.from.url, e.to.url]),
  ]);
}
