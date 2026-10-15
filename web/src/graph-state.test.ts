import { describe, expect, test } from "bun:test";
import { createStore } from "solid-js/store";
import { applyPoll, type GraphNode } from "./graph-state";
import type { Group, Row } from "./types";

function row(url: string, over: Partial<Row> = {}): Row {
  return {
    url,
    ref: "#1",
    title: "t",
    state: "ready",
    glyph: "ready",
    alive: false,
    blocking: null,
    after: null,
    reason: "",
    elapsed: "",
    pr_number: 0,
    session_path: "/s/o/r/1",
    ...over,
  };
}

const poll = (...rows: Row[]): Group[] => [{ root: null, children: rows }];

describe("applyPoll", () => {
  test("a changed glyph, state and title reach the node with the same url", () => {
    const [nodes, setNodes] = createStore<GraphNode[]>([]);
    applyPoll(setNodes, poll(row("o/r/1")));

    applyPoll(setNodes, poll(row("o/r/1", { glyph: "running", state: "running", title: "new" })));

    expect(nodes.length).toBe(1);
    expect(nodes[0]?.url).toBe("o/r/1");
    expect(nodes[0]?.glyph).toBe("running");
    expect(nodes[0]?.state).toBe("running");
    expect(nodes[0]?.title).toBe("new");
  });

  test("a ticket that leaves the poll leaves the store", () => {
    const [nodes, setNodes] = createStore<GraphNode[]>([]);
    applyPoll(setNodes, poll(row("o/r/1"), row("o/r/2")));
    applyPoll(setNodes, poll(row("o/r/2")));
    expect(nodes.map((n) => n.url)).toEqual(["o/r/2"]);
  });
});
