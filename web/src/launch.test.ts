import { describe, expect, test } from "bun:test";
import { columnsFor, dependentsOf, initialTicked, missingBlockersOf, REFUSED } from "./launch";
import type { Candidate } from "./launch";

function candidate(over: Partial<Candidate> & { url: string }): Candidate {
  return {
    ref: over.url,
    title: "",
    repo: "",
    feature: "",
    label: "now",
    reason: "",
    base: "",
    base_verdict: "",
    prompt_hash: `hash-${over.url}`,
    blocked_by: null,
    ...over,
  };
}

describe("initialTicked", () => {
  test("ticks every candidate except one already refused", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", label: REFUSED })];
    expect(initialTicked(candidates)).toEqual(new Set(["a"]));
  });
});

describe("dependentsOf", () => {
  test("names the ticked candidates blocked by the one being unticked", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", blocked_by: ["a"] }),
      candidate({ url: "c", blocked_by: ["a"] }),
    ];
    const ticked = new Set(["a", "b", "c"]);
    expect(dependentsOf(candidates, ticked, "a").map((c) => c.url)).toEqual(["b", "c"]);
  });

  test("ignores a dependent that has already been unticked", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", blocked_by: ["a"] })];
    const ticked = new Set(["a"]);
    expect(dependentsOf(candidates, ticked, "a")).toEqual([]);
  });

  test("a candidate with no ticked dependents has none", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b" })];
    expect(dependentsOf(candidates, new Set(["a", "b"]), "a")).toEqual([]);
  });

  test("clearing dependents bottom-up eventually frees the blocker in a chain", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", blocked_by: ["a"] }),
      candidate({ url: "c", blocked_by: ["b"] }),
    ];
    let ticked = new Set(["a", "b", "c"]);
    expect(dependentsOf(candidates, ticked, "a").map((c) => c.url)).toEqual(["b"]);

    ticked = new Set(["a", "b"]);
    expect(dependentsOf(candidates, ticked, "b").map((c) => c.url)).toEqual([]);
    expect(dependentsOf(candidates, ticked, "a").map((c) => c.url)).toEqual(["b"]);

    ticked = new Set(["a"]);
    expect(dependentsOf(candidates, ticked, "a")).toEqual([]);
  });
});

describe("missingBlockersOf", () => {
  test("a candidate with every blocker ticked has none missing", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", blocked_by: ["a"] })];
    expect(missingBlockersOf(candidates, new Set(["a", "b"]), "b")).toEqual([]);
  });

  test("names an unticked blocker", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", blocked_by: ["a"] })];
    expect(missingBlockersOf(candidates, new Set(["b"]), "b").map((c) => c.url)).toEqual(["a"]);
  });

  test("re-ticking a dependent whose blocker was left unticked stays refused", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", blocked_by: ["a"] }),
      candidate({ url: "c", blocked_by: ["b"] }),
    ];
    let ticked = new Set(["a", "b", "c"]);
    expect(dependentsOf(candidates, ticked, "c")).toEqual([]);
    ticked = new Set(["a", "b"]);
    expect(dependentsOf(candidates, ticked, "b")).toEqual([]);
    ticked = new Set(["a"]);

    expect(missingBlockersOf(candidates, ticked, "c").map((c) => c.url)).toEqual(["b"]);
  });
});

describe("columnsFor", () => {
  test("a candidate with no blockers inside the set sits in column 0", () => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", blocked_by: ["outside"] })];
    const cols = columnsFor(candidates);
    expect(cols.get("a")).toBe(0);
    expect(cols.get("b")).toBe(0);
  });

  test("a chain lands one column further right per link", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", blocked_by: ["a"] }),
      candidate({ url: "c", blocked_by: ["b"] }),
    ];
    const cols = columnsFor(candidates);
    expect([cols.get("a"), cols.get("b"), cols.get("c")]).toEqual([0, 1, 2]);
  });

  test("a candidate blocked by two chains takes the longer one", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", blocked_by: ["a"] }),
      candidate({ url: "c", blocked_by: ["b", "a"] }),
    ];
    const cols = columnsFor(candidates);
    expect(cols.get("c")).toBe(2);
  });
});
