// Shared DAG layout math for both islands (graph.tsx's ticket board and launch-modal.tsx's
// candidate DAG). Neither island imports the other -- they each import this instead.

export const COL_W = 260;
export const ROW_H = 64;
// Tailwind's static scanner needs a literal class, so each island's node button carries its own
// literal `w-[200px]` rather than an interpolated one, and that literal must be kept in sync with
// NODE_W by hand.
export const NODE_W = 200;
export const NODE_H = 40;
export const MARGIN = 24;

export interface LayoutGroup<Row> {
  root: Row | null;
  children: Row[];
}

export interface Placed<Row> {
  row: Row;
  col: number;
  x: number;
  y: number;
}

// layoutGroups places one column-0 node per group's root and one column-1 node per waiting
// child, stacking each group's own rows together so the next group starts below all of them
// (CONTEXT.md's "group": one blocker ticket and zero or more waiting tickets).
export function layoutGroups<Row>(groups: LayoutGroup<Row>[]): Placed<Row>[] {
  const placed: Placed<Row>[] = [];
  let y = MARGIN;
  for (const g of groups) {
    if (g.root) {
      placed.push({ row: g.root, col: 0, x: MARGIN, y });
      let cy = y;
      for (const child of g.children) {
        cy += ROW_H;
        placed.push({ row: child, col: 1, x: MARGIN + COL_W, y: cy });
      }
      y = cy + ROW_H;
    } else {
      for (const child of g.children) {
        placed.push({ row: child, col: 0, x: MARGIN, y });
        y += ROW_H;
      }
    }
  }
  return placed;
}

// stackByColumn assigns x from col and y from each column's own independent row count, in input
// order -- for a DAG with no group structure to keep contiguous, unlike layoutGroups above.
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

// edgesFor draws one edge per (blocker, node) pair, reading each node's own blocked-by list
// through the caller's accessor -- graph.tsx's Row names it "blocking", a candidate names it
// "blocked_by", and both mean the same thing: the URLs that must land before this node does.
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
