import { describe, expect, test } from "bun:test";
import {
  cancelHref,
  dependentsOf,
  initialTicked,
  launchLabel,
  missingBlockersOf,
  REFUSED,
  sectionsFor,
  toggleResult,
  whenLabel,
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

describe("launchLabel", () => {
  const candidates = [
    candidate({ url: "a" }),
    candidate({ url: "b" }),
    candidate({ url: "c", label: "on unlock", blocked_by: ["a"] }),
  ];

  test("counts ticked tickets by section", () => {
    expect(launchLabel(candidates, new Set(["a", "b", "c"]))).toBe("Launch 2 now · arm 1");
  });

  test("a selection with nothing ready leads with Arm", () => {
    expect(launchLabel(candidates, new Set(["c"]))).toBe("Arm 1");
  });

  test("an empty selection asks for one", () => {
    expect(launchLabel(candidates, new Set())).toBe("Select tickets to launch");
  });
});

describe("whenLabel", () => {
  test("a ready ticket shows its repo", () => {
    const c = candidate({ url: "a", repo: "olly/x" });
    expect(whenLabel(c, [c])).toBe("olly/x");
  });

  test("an on-unlock ticket names its blockers by their supplied refs", () => {
    const blocker = candidate({ url: "linear://ACME-1", ref: "ACME-1" });
    const c = candidate({ url: "z", label: "on unlock", blocked_by: ["linear://ACME-1", "gh://o/r/7"] });
    expect(whenLabel(c, [blocker, c])).toBe("after ACME-1, #7");
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
  test("returns to the feature page, escaping its name", () => {
    expect(cancelHref("project:x/y")).toBe("/f/project%3Ax%2Fy");
  });

  test("returns home for a ticket slice", () => {
    expect(cancelHref("")).toBe("/");
  });
});

describe("toggleResult", () => {
  const candidates = [
    candidate({ url: "x/1", ref: "#1", title: "first" }),
    candidate({ url: "x/2", ref: "#2", title: "second", blocked_by: ["x/1"] }),
  ];

  test("a refused tick leaves the ticked set unchanged and names the blocker", () => {
    const ticked = new Set<string>();
    const result = toggleResult(candidates, ticked, "x/2");
    expect(result).toEqual({ refusal: "#2 is blocked by #1 first -- tick it first" });
    expect(ticked).toEqual(new Set());
  });

  test("a refused clear keeps the blocker ticked", () => {
    const ticked = new Set(["x/1", "x/2"]);
    const result = toggleResult(candidates, ticked, "x/1");
    expect(result).toEqual({ refusal: "#2 second depends on #1 -- clear it first" });
    expect(ticked).toEqual(new Set(["x/1", "x/2"]));
  });

  test("refusals use supplied refs rather than parsing URLs", () => {
    const named = [
      candidate({ url: "opaque-a", ref: "ACME-1", title: "first" }),
      candidate({ url: "opaque-b", ref: "ACME-2", title: "second", blocked_by: ["opaque-a"] }),
    ];
    expect(toggleResult(named, new Set<string>(), "opaque-b")).toEqual({
      refusal: "ACME-2 is blocked by ACME-1 first -- tick it first",
    });
  });

  test("an allowed tick and clear return a new set", () => {
    expect(toggleResult(candidates, new Set(["x/1"]), "x/2")).toEqual({ ticked: new Set(["x/1", "x/2"]) });
    expect(toggleResult(candidates, new Set(["x/1", "x/2"]), "x/2")).toEqual({ ticked: new Set(["x/1"]) });
  });
});
