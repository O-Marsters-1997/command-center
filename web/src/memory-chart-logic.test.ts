import { describe, expect, test } from "bun:test";
import { brushSpan, move } from "./memory-chart-logic";

const bars = [
  { from: 0, to: 10 },
  { from: 10, to: 20 },
  { from: 20, to: 30 },
  { from: 30, to: 40 },
];

describe("brushSpan", () => {
  test.each([
    { name: "forward", start: 1, end: 2, want: { from: 10, to: 30 } },
    { name: "backward", start: 2, end: 1, want: { from: 10, to: 30 } },
    { name: "single bar", start: 3, end: 3, want: { from: 30, to: 40 } },
    { name: "clamped past both edges", start: -5, end: 9, want: { from: 0, to: 40 } },
  ])("$name", ({ start, end, want }) => {
    expect(brushSpan(bars, start, end)).toEqual(want);
  });
});

test("brushSpan rejects an empty chart", () => {
  expect(() => brushSpan([], 0, 1)).toThrow(RangeError);
});

describe("move", () => {
  test.each([
    { name: "next inside", index: 1, delta: 1 as const, want: { kind: "select", index: 2 } },
    { name: "previous inside", index: 1, delta: -1 as const, want: { kind: "select", index: 0 } },
    { name: "pan past start", index: 0, delta: -1 as const, want: { kind: "pan", dir: -1 } },
    { name: "pan past end", index: 3, delta: 1 as const, want: { kind: "pan", dir: 1 } },
  ])("$name", ({ index, delta, want }) => {
    expect(move(index, delta, 4)).toEqual(want);
  });
});
