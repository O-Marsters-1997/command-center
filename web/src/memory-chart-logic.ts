export type BarSpan = { from: number; to: number };
export type Move = { kind: "select"; index: number } | { kind: "pan"; dir: -1 | 1 };

export const settleMs = 150;

export function brushSpan(bars: BarSpan[], start: number, end: number): BarSpan {
  const clamp = (i: number) => Math.min(Math.max(i, 0), bars.length - 1);
  const first = bars[clamp(Math.min(start, end))];
  const last = bars[clamp(Math.max(start, end))];
  if (!first || !last) {
    throw new RangeError("brushSpan needs at least one bar");
  }
  return { from: first.from, to: last.to };
}

export function move(index: number, delta: -1 | 1, count: number): Move {
  const next = index + delta;
  if (next < 0 || next >= count) {
    return { kind: "pan", dir: delta };
  }
  return { kind: "select", index: next };
}
