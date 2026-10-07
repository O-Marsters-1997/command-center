import { describe, expect, test } from "bun:test";
import { niceTicks, rollingMedian } from "./charts";

describe("niceTicks", () => {
  test("falls back to a single zero tick for a non-positive max", () => {
    expect(niceTicks(0)).toEqual({ values: [0], step: 1 });
  });

  test("picks a 1/2/5-ladder step and rounds the top up to a multiple of it", () => {
    expect(niceTicks(9)).toEqual({ values: [0, 5, 10], step: 5 });
    expect(niceTicks(100)).toEqual({ values: [0, 50, 100], step: 50 });
  });
});

describe("rollingMedian", () => {
  test("returns nothing for an empty series", () => {
    expect(rollingMedian([])).toEqual([]);
  });

  test("is the running median when fewer than the window's worth of points exist", () => {
    expect(rollingMedian([1, 2, 3])).toEqual([1, 1.5, 2]);
  });

  test("slides a trailing window of the given size once enough points exist", () => {
    expect(rollingMedian([1, 2, 3, 4, 5], 3)).toEqual([1, 1.5, 2, 3, 4]);
  });

  test("averages the two middle values for an even-sized window", () => {
    expect(rollingMedian([1, 5, 2, 8], 4)).toEqual([1, 3, 2, 3.5]);
  });
});
