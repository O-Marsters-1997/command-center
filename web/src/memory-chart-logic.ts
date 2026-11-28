export type BarSpan = { from: number; to: number };
export type Move = { kind: "select"; index: number } | { kind: "pan"; dir: -1 | 1 };

export const settleMs = 150;

export function brushSpan(bars: BarSpan[], start: number, end: number): BarSpan {
  const last = bars.length - 1;
  const lo = Math.min(Math.max(Math.min(start, end), 0), last);
  const hi = Math.min(Math.max(Math.max(start, end), 0), last);
  return { from: bars[lo].from, to: bars[hi].to };
}

export function move(index: number, delta: -1 | 1, count: number): Move {
  const next = index + delta;
  if (next < 0 || next >= count) {
    return { kind: "pan", dir: delta };
  }
  return { kind: "select", index: next };
}
