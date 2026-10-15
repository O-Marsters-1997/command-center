import { describe, expect, test } from "bun:test";
import {
  cancelHref,
  columnsFor,
  dependentsOf,
  initialTicked,
  missingBlockersOf,
  REFUSED,
  sectionsFor,
  toggleResult,
} from "./launch";
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
  test.each([
    ["two ticked dependents", ["a", "a"], ["a", "b", "c"], ["b", "c"]],
    ["unticked dependent", ["a"], ["a"], []],
    ["unblocked candidate", [null], ["a", "b"], []],
  ] as const)("%s", (_, blockers, selected, expected) => {
    const candidates = [candidate({ url: "a" }), ...blockers.map((blocker, i) => candidate({
      url: String.fromCharCode(98 + i), blocked_by: blocker ? [blocker] : null,
    }))];
    expect(dependentsOf(candidates, new Set(selected), "a").map((c) => c.url)).toEqual(expected);
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
  test.each([
    ["ticked blocker", ["a", "b"], []],
    ["unticked blocker", ["b"], ["a"]],
  ] as const)("%s", (_, selected, expected) => {
    const candidates = [candidate({ url: "a" }), candidate({ url: "b", blocked_by: ["a"] })];
    expect(missingBlockersOf(candidates, new Set(selected), "b").map((c) => c.url)).toEqual(expected);
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

  test("a cycle terminates with finite columns", () => {
    const cols = columnsFor([
      candidate({ url: "a", blocked_by: ["b"] }),
      candidate({ url: "b", blocked_by: ["a"] }),
    ]);
    expect([...cols.values()].every(Number.isFinite)).toBe(true);
  });
});

describe("sectionsFor", () => {
  test("splits by preview label, keeping order within a section", () => {
    const candidates = [
      candidate({ url: "a" }),
      candidate({ url: "b", label: "on unlock", blocked_by: ["a"] }),
      candidate({ url: "c", label: REFUSED }),
      candidate({ url: "d" }),
    ];
    const sections = sectionsFor(candidates);
    expect(sections.now.map((c) => c.url)).toEqual(["a", "d"]);
    expect(sections.onUnlock.map((c) => c.url)).toEqual(["b"]);
    expect(sections.refused.map((c) => c.url)).toEqual(["c"]);
  });
});

describe("cancelHref", () => {
  test.each([
    ["feature name escaped", "project:x/y", "/f/project%3Ax%2Fy"],
    ["ticket slice goes home", "", "/"],
  ])("%s", (_, feature, expected) => {
    expect(cancelHref(feature)).toBe(expected);
  });
});

describe("toggleResult", () => {
  const candidates = [
    candidate({ url: "x/1", ref: "#1", title: "first" }),
    candidate({ url: "x/2", ref: "#2", title: "second", blocked_by: ["x/1"] }),
  ];

  test.each([
    ["refused tick", [], "x/2", "#2 is blocked by #1 first -- tick it first"],
    ["refused clear", ["x/1", "x/2"], "x/1", "#2 second depends on #1 -- clear it first"],
  ] as const)("%s preserves selection", (_, selected, url, refusal) => {
    const ticked = new Set<string>(selected);
    expect(toggleResult(candidates, ticked, url)).toEqual({ refusal });
    expect(ticked).toEqual(new Set(selected));
  });

  test("refusals use supplied refs rather than parsing URLs", () => {
    const named = [candidate({ url: "opaque-a", ref: "ACME-1", title: "first" }), candidate({ url: "opaque-b", ref: "ACME-2", title: "second", blocked_by: ["opaque-a"] })];
    expect(toggleResult(named, new Set<string>(), "opaque-b")).toEqual({ refusal: "ACME-2 is blocked by ACME-1 first -- tick it first" });
  });

  test.each([
    ["allowed tick", ["x/1"], ["x/1", "x/2"]],
    ["allowed clear", ["x/1", "x/2"], ["x/1"]],
  ] as const)("%s returns a new set", (_, selected, expected) => {
    const ticked = new Set<string>(selected);
    expect(toggleResult(candidates, ticked, "x/2")).toEqual({ ticked: new Set(expected) });
    expect(ticked).toEqual(new Set(selected));
  });
});
