import { describe, expect, test } from "bun:test";
import { bounds, edgePath, edgesFor, layoutWaves, stackByColumn, traceChain, waveDepths, waveLabel } from "./layout";

interface Row {
  url: string;
}

describe("layoutWaves", () => {
  const rows = [
    { url: "9", blocking: ["7"] },
    { url: "2", blocking: null },
    { url: "7", blocking: ["2"] },
    { url: "3", blocking: ["2", "7"] },
  ];

  test("places a ticket one wave after its deepest blocker", () => {
    const placed = layoutWaves(rows);
    const col = Object.fromEntries(placed.map((p) => [p.url, p.col]));
    expect(col).toEqual({ "2": 0, "7": 1, "9": 2, "3": 2 });
  });

  test("stacks each wave in input order at that wave's x", () => {
    expect(layoutWaves(rows).map((p) => [p.url, p.x, p.y])).toEqual([
      ["9", 24 + 2 * 260, 24],
      ["2", 24, 24],
      ["7", 24 + 260, 24],
      ["3", 24 + 2 * 260, 88],
    ]);
  });

  test("ignores a blocker outside the set and survives a cycle", () => {
    const depth = waveDepths([
      { url: "a", blocking: ["gone"] },
      { url: "b", blocking: ["c"] },
      { url: "c", blocking: ["b"] },
    ]);
    expect(depth.get("a")).toBe(0);
    expect([...depth.values()].every((d) => Number.isFinite(d))).toBe(true);
  });

  test("labels waves from one", () => {
    expect(waveLabel(0)).toBe("Wave 1");
  });
});

describe("traceChain", () => {
  const n = (url: string) => ({ url });
  const nodes = ["2", "7", "9", "4"].map(n);
  const edges = edgesFor(
    nodes.map((x) => ({ ...x, blocking: x.url === "7" ? ["2"] : x.url === "9" ? ["7"] : null })),
    (x) => x.blocking,
  );

  test("lights the full upstream chain of a node", () => {
    expect([...traceChain("9", edges)].sort()).toEqual(["2", "7", "9"]);
  });

  test("lights the full downstream chain of a node", () => {
    expect([...traceChain("2", edges)].sort()).toEqual(["2", "7", "9"]);
  });

  test("leaves an unconnected node alone", () => {
    expect([...traceChain("4", edges)]).toEqual(["4"]);
  });
});

describe("stackByColumn", () => {
  test("assigns y independently per column, unlike layoutGroups", () => {
    const placed = stackByColumn([
      { url: "a", col: 0 },
      { url: "b", col: 1 },
      { url: "c", col: 0 },
    ]);
    expect(placed.map((p) => [p.url, p.x, p.y])).toEqual([
      ["a", 24, 24],
      ["b", 284, 24],
      ["c", 24, 88],
    ]);
  });
});

describe("edgesFor", () => {
  test("draws one edge per blocker naming a node in the set", () => {
    const nodes = [
      { url: "a", blockedBy: [] as string[] },
      { url: "b", blockedBy: ["a"] },
      { url: "c", blockedBy: ["a", "missing"] },
    ];
    const edges = edgesFor(nodes, (n) => n.blockedBy);
    expect(edges.map((e) => [e.from.url, e.to.url])).toEqual([
      ["a", "b"],
      ["a", "c"],
    ]);
  });

  test("reads whatever field the caller's accessor points at", () => {
    const nodes = [
      { url: "a", blocking: ["b"] },
      { url: "b", blocking: null },
    ];
    const edges = edgesFor(nodes, (n) => n.blocking);
    expect(edges.map((e) => [e.from.url, e.to.url])).toEqual([["b", "a"]]);
  });
});

test("edgePath curves from the blocker's right edge to the dependent's left edge", () => {
  const path = edgePath({ from: { x: 0, y: 0 }, to: { x: 260, y: 64 } });
  expect(path.startsWith("M 200 20 C")).toBe(true);
});

test("bounds grows with the widest column and the tallest row", () => {
  expect(
    bounds([
      { col: 0, y: 24 },
      { col: 1, y: 88 },
    ]),
  ).toEqual({ width: 24 * 2 + 260 + 200, height: 88 + 40 + 24 });
});
