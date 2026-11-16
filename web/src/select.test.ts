import { describe, expect, test } from "bun:test";
import { launchHref, rangeBetween, stepCursor, summarize, toggled, withAll, without } from "./select";

describe("selection edits", () => {
  test("toggled adds then removes", () => {
    expect(toggled(["a"], "b")).toEqual(["a", "b"]);
    expect(toggled(["a", "b"], "a")).toEqual(["b"]);
  });

  test("withAll skips what is already selected and without drops only the named", () => {
    expect(withAll(["a"], ["a", "b", "c"])).toEqual(["a", "b", "c"]);
    expect(without(["a", "b", "c"], ["b", "x"])).toEqual(["a", "c"]);
  });
});

describe("rangeBetween", () => {
  const order = ["a", "b", "c", "d"];

  test("covers both ends in either direction", () => {
    expect(rangeBetween(order, "b", "d")).toEqual(["b", "c", "d"]);
    expect(rangeBetween(order, "d", "b")).toEqual(["b", "c", "d"]);
  });

  test("an unknown anchor selects only the target and an unknown target nothing", () => {
    expect(rangeBetween(order, "zzz", "c")).toEqual(["c"]);
    expect(rangeBetween(order, "a", "zzz")).toEqual([]);
  });
});

describe("stepCursor", () => {
  test("starts at an end and clamps at the other", () => {
    expect(stepCursor(3, -1, 1)).toBe(0);
    expect(stepCursor(3, -1, -1)).toBe(2);
    expect(stepCursor(3, 2, 1)).toBe(2);
    expect(stepCursor(3, 0, -1)).toBe(0);
    expect(stepCursor(0, -1, 1)).toBe(-1);
  });
});

describe("summarize", () => {
  test("names the feature when there is one and counts across features otherwise", () => {
    const one = summarize([
      { url: "1", feature: "f", armed: false },
      { url: "2", feature: "f", armed: true },
    ]);
    expect(one).toEqual({ count: 2, now: 1, arm: 1, scope: "f" });
    const many = summarize([
      { url: "1", feature: "f", armed: false },
      { url: "2", feature: "g", armed: false },
    ]);
    expect(many.scope).toBe("across 2 features");
  });
});

describe("launchHref", () => {
  test("repeats ticket and encodes the urls", () => {
    expect(launchHref(["https://x/1", "https://x/2"])).toBe(
      "/launch?ticket=https%3A%2F%2Fx%2F1&ticket=https%3A%2F%2Fx%2F2",
    );
  });
});
