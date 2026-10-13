import { reconcile, type SetStoreFunction } from "solid-js/store";
import { CARD, layoutWaves } from "./layout";
import type { Group, Row } from "./types";

export interface GraphNode extends Row {
  col: number;
  x: number;
  y: number;
}

export function placeNodes(groups: Group[]): GraphNode[] {
  const rows = groups.flatMap((g) => (g.root ? [g.root, ...g.children] : g.children));
  return layoutWaves(rows, CARD).map((p) => ({ ...p }));
}

export function applyPoll(setNodes: SetStoreFunction<GraphNode[]>, groups: Group[]): void {
  setNodes(reconcile(placeNodes(groups), { key: "url" }));
}
