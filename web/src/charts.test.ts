import { describe, expect, test } from "bun:test";
import { histogramBars, niceTicks, sparkPoints } from "./charts";

describe("niceTicks", () => {
  test("falls back to a single zero tick for a non-positive max", () => {
    expect(niceTicks(0)).toEqual({ values: [0], step: 1 });
  });

  test("picks a 1/2/5-ladder step and rounds the top up to a multiple of it", () => {
    expect(niceTicks(9)).toEqual({ values: [0, 5, 10], step: 5 });
    expect(niceTicks(100)).toEqual({ values: [0, 50, 100], step: 50 });
  });
});

describe("sparkPoints", () => {
  test("returns nothing for an empty series", () => {
    expect(sparkPoints([], 100, 20)).toEqual([]);
  });

  test("centers a single point in the plot area", () => {
    expect(sparkPoints([5], 100, 20)).toEqual([{ x: 50, y: 10 }]);
  });

  test("spans the width and maps value range to the full height, inverted for SVG y", () => {
    expect(sparkPoints([0, 10], 100, 20)).toEqual([
      { x: 0, y: 20 },
      { x: 100, y: 0 },
    ]);
  });
});

describe("histogramBars", () => {
  test("returns nothing for an empty series", () => {
    expect(histogramBars([])).toEqual([]);
  });

  test("collapses a single repeated value into one bin", () => {
    expect(histogramBars([5, 5, 5])).toEqual([{ label: "5", value: 3 }]);
  });

  test("buckets a range evenly across the requested bin count", () => {
    expect(histogramBars([0, 10], 2)).toEqual([
      { label: "0", value: 1 },
      { label: "5", value: 1 },
    ]);
  });
});
