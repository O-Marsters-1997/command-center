import { describe, expect, test } from "bun:test";
import { actionFor, nextChange, nextUnviewed, pairRuns, segments } from "./changes";

describe("pairRuns", () => {
  test("pairs a deleted run with the added run after it, by position", () => {
    expect(pairRuns(["context", "deleted", "deleted", "added", "added", "added", "context"])).toEqual([
      [1, 3],
      [2, 4],
    ]);
  });

  test("leaves unmatched runs alone", () => {
    expect(pairRuns(["added", "deleted", "context", "deleted"])).toEqual([]);
  });
});

describe("segments", () => {
  test("splits tokens at the changed range", () => {
    const got = segments('x := "ab"', "go", { start: 6, end: 8 });
    expect(got.map((s) => s.text).join("")).toBe('x := "ab"');
    expect(got.filter((s) => s.changed).map((s) => s.text).join("")).toBe("ab");
    expect(got.find((s) => s.changed)?.kind).toBe("str");
  });

  test("marks nothing without a range", () => {
    expect(segments("return nil", "go").some((s) => s.changed)).toBe(false);
  });
});

describe("nextChange", () => {
  const rows = [0, 1, 1, 0, 1];

  test.each([
    [-1, 1, 1],
    [1, 1, 4],
    [4, 1, -1],
    [4, -1, 1],
    [2, -1, -1],
    [-1, -1, 4],
  ] as const)("from %i step %i lands on %i", (cur, step, want) => {
    expect(nextChange(rows, cur, step)).toBe(want);
  });
});

describe("nextChange across files", () => {
  const rows = [1, 1, 2, 2];

  test("treats adjacent runs from different files as separate", () => {
    expect(nextChange(rows, 0, 1)).toBe(2);
    expect(nextChange(rows, 3, -1)).toBe(0);
    expect(nextChange([1, 1, 2, 2, 2], 4, -1)).toBe(0);
    expect(nextChange([1, 2, 2], 0, 1)).toBe(1);
  });
});

describe("nextUnviewed", () => {
  test("skips viewed files and wraps", () => {
    expect(nextUnviewed([false, true, true, false], 0)).toBe(3);
    expect(nextUnviewed([false, true, true, false], 3)).toBe(0);
  });

  test("is -1 when every other file is viewed", () => {
    expect(nextUnviewed([true, true], 0)).toBe(-1);
  });
});

describe("actionFor", () => {
  const key = (k: string, extra = {}) => ({ key: k, ctrlKey: false, metaKey: false, altKey: false, isComposing: false, ...extra });

  test("maps diffKeys and ignores modified presses", () => {
    expect(actionFor(key("V"))).toBe("viewed-and-next");
    expect(actionFor(key("j", { metaKey: true }))).toBeUndefined();
    expect(actionFor(key("c"))).toBe("comment");
    expect(actionFor(key("L"))).toBe("comments");
    expect(actionFor(key("x"))).toBeUndefined();
  });
});
