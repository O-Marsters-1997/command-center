import { describe, expect, test } from "bun:test";
import { bounds, edgePath, edgesFor, layoutGroups, stackByColumn } from "./layout";

interface Row {
  url: string;
}

describe("layoutGroups", () => {
  test("places a root at col 0 and its children at col 1, stacked below it", () => {
    const placed = layoutGroups<Row>([{ root: { url: "root" }, children: [{ url: "a" }, { url: "b" }] }]);
    expect(placed.map((p) => [p.row.url, p.col, p.x, p.y])).toEqual([
      ["root", 0, 24, 24],
      ["a", 1, 284, 88],
      ["b", 1, 284, 152],
    ]);
  });

  test("starts the next group below every row the previous group used, not just its root", () => {
    const placed = layoutGroups<Row>([
      { root: { url: "root1" }, children: [{ url: "a" }, { url: "b" }] },
      { root: { url: "root2" }, children: [] },
    ]);
    expect(placed.map((p) => [p.row.url, p.y])).toEqual([
      ["root1", 24],
      ["a", 88],
      ["b", 152],
      ["root2", 216],
    ]);
  });

  test("stacks a rootless group's children in col 0", () => {
    const placed = layoutGroups<Row>([{ root: null, children: [{ url: "a" }, { url: "b" }] }]);
    expect(placed.map((p) => [p.row.url, p.col, p.x, p.y])).toEqual([
      ["a", 0, 24, 24],
      ["b", 0, 24, 88],
    ]);
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
