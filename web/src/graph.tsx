import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { Group, Row } from "./types";

const POLL_MS = 5000;
const COL_W = 260;
const ROW_H = 64;
const NODE_W = 200;
const NODE_H = 40;
const MARGIN = 24;
const MIN_SCALE = 0.25;
const MAX_SCALE = 3;

interface GraphNode extends Row {
  col: number;
  x: number;
  y: number;
}

interface Edge {
  from: GraphNode;
  to: GraphNode;
}

// nodeCache keeps one object per URL across polls: <For> keys its children by object identity,
// so mutating a cached node in place (rather than spreading a fresh one every layoutGroups call)
// is what keeps a node button's own DOM -- and its keyboard focus -- stable across a 5s poll.
const nodeCache = new Map<string, GraphNode>();

function layoutGroups(groups: Group[]): GraphNode[] {
  const nodes: GraphNode[] = [];
  let y = MARGIN;
  for (const g of groups) {
    if (g.root) {
      nodes.push(toNode(g.root, 0, y));
      let cy = y;
      for (const child of g.children) {
        cy += ROW_H;
        nodes.push(toNode(child, 1, cy));
      }
      y = cy + ROW_H;
    } else {
      for (const child of g.children) {
        nodes.push(toNode(child, 0, y));
        y += ROW_H;
      }
    }
  }
  return nodes;
}

function toNode(row: Row, col: number, y: number): GraphNode {
  const node = nodeCache.get(row.url) ?? ({} as GraphNode);
  Object.assign(node, row, { col, x: MARGIN + col * COL_W, y });
  nodeCache.set(row.url, node);
  return node;
}

function ticketRef(url: string): string {
  const parts = url.split("/");
  return `#${parts[parts.length - 1]}`;
}

function edgesFor(nodes: GraphNode[]): Edge[] {
  const byURL = new Map(nodes.map((n) => [n.url, n]));
  const edges: Edge[] = [];
  for (const node of nodes) {
    for (const blockerURL of node.blocking || []) {
      const from = byURL.get(blockerURL);
      if (from) edges.push({ from, to: node });
    }
  }
  return edges;
}

function edgePath(edge: Edge): string {
  const x1 = edge.from.x + NODE_W;
  const y1 = edge.from.y + NODE_H / 2;
  const x2 = edge.to.x;
  const y2 = edge.to.y + NODE_H / 2;
  const dx = Math.max(40, (x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}

customElement("cc-graph", {}, () => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  getCurrentElement().textContent = "";

  const [groups, setGroups] = createSignal<Group[]>([]);
  const [selected, setSelected] = createSignal<Set<string>>(new Set());
  const [pan, setPan] = createSignal({ x: 0, y: 0 });
  const [scale, setScale] = createSignal(1);

  let viewport: HTMLDivElement | undefined;
  const nodeRefs = new Map<string, HTMLButtonElement>();

  async function load() {
    try {
      const res = await fetch("/graph.json");
      if (!res.ok) return;
      setGroups(await res.json());
    } catch {}
  }

  let timer: ReturnType<typeof setInterval>;
  onMount(() => {
    load();
    timer = setInterval(load, POLL_MS);
  });
  onCleanup(() => clearInterval(timer));

  const nodes = createMemo(() => layoutGroups(groups()));
  const edges = createMemo(() => edgesFor(nodes()));
  const bounds = createMemo(() => {
    const ns = nodes();
    const maxCol = ns.reduce((m, n) => Math.max(m, n.col), 0);
    const maxY = ns.reduce((m, n) => Math.max(m, n.y), 0);
    return {
      width: MARGIN * 2 + maxCol * COL_W + NODE_W,
      height: maxY + NODE_H + MARGIN,
    };
  });

  const litURLs = createMemo(() => {
    const sel = selected();
    const lit = new Set(sel);
    for (const edge of edges()) {
      if (sel.has(edge.from.url)) lit.add(edge.to.url);
      if (sel.has(edge.to.url)) lit.add(edge.from.url);
    }
    return lit;
  });

  function toggle(url: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(url)) next.delete(url);
      else next.add(url);
      return next;
    });
  }

  function submit() {
    const params = [...selected()].map((u) => `task=${encodeURIComponent(u)}`).join("&");
    window.location.href = params ? `/preview?${params}` : "/preview";
  }

  function resetView() {
    setPan({ x: 0, y: 0 });
    setScale(1);
  }

  // Pan and zoom are local signals only -- neither handler below ever calls fetch, which is the
  // property this island exists to prove (docs/prds/prd-fleet-view.md § The graph).
  let dragging: { x: number; y: number; pan: { x: number; y: number } } | null = null;
  function onPointerDown(e: PointerEvent) {
    if ((e.target as HTMLElement).closest(".graph-node")) return;
    dragging = { x: e.clientX, y: e.clientY, pan: pan() };
    viewport?.setPointerCapture(e.pointerId);
    viewport?.classList.add("panning");
  }
  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    setPan({ x: dragging.pan.x + (e.clientX - dragging.x), y: dragging.pan.y + (e.clientY - dragging.y) });
  }
  function endDrag() {
    dragging = null;
    viewport?.classList.remove("panning");
  }
  // Zoom to cursor (nice to have): keep the content point under the pointer fixed while scale
  // changes, by solving pan from the point's own before/after content-space coordinates.
  function onWheel(e: WheelEvent) {
    e.preventDefault();
    if (!viewport) return;
    const rect = viewport.getBoundingClientRect();
    const cursorX = e.clientX - rect.left;
    const cursorY = e.clientY - rect.top;
    const p = pan();
    const s = scale();
    const contentX = (cursorX - p.x) / s;
    const contentY = (cursorY - p.y) / s;
    const nextScale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, s * (1 - e.deltaY * 0.001)));
    setScale(nextScale);
    setPan({ x: cursorX - contentX * nextScale, y: cursorY - contentY * nextScale });
  }

  function onNodeKeyDown(e: KeyboardEvent, node: GraphNode) {
    const dir = ({ ArrowUp: [0, -1], ArrowDown: [0, 1], ArrowLeft: [-1, 0], ArrowRight: [1, 0] } as const)[
      e.key as "ArrowUp" | "ArrowDown" | "ArrowLeft" | "ArrowRight"
    ];
    if (!dir) return;
    e.preventDefault();
    const [dCol] = dir;
    const inDirection =
      dCol === 0
        ? nodes().filter((n) => n.col === node.col && n.url !== node.url)
        : nodes().filter((n) => Math.sign(n.col - node.col) === dCol);
    if (inDirection.length === 0) return;
    inDirection.sort((a, b) => Math.abs(a.y - node.y) - Math.abs(b.y - node.y));
    nodeRefs.get(inDirection[0].url)?.focus();
  }

  return (
    <div class="graph">
      <div class="graph-toolbar">
        <span>{selected().size} selected</span>
        <button type="button" onClick={resetView}>
          reset view
        </button>
        <button type="button" disabled={selected().size === 0} onClick={submit}>
          preview selection
        </button>
      </div>
      <div
        class="graph-viewport"
        ref={viewport}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onWheel={onWheel}
      >
        <div
          class="graph-surface"
          style={{
            transform: `translate(${pan().x}px, ${pan().y}px) scale(${scale()})`,
            width: `${bounds().width}px`,
            height: `${bounds().height}px`,
          }}
        >
          <svg
            class="graph-edges"
            width={bounds().width}
            height={bounds().height}
            aria-hidden="true"
            role="presentation"
          >
            <For each={edges()}>
              {(edge) => (
                <path
                  class="graph-edge"
                  classList={{
                    "graph-edge-lit": selected().has(edge.from.url) || selected().has(edge.to.url),
                  }}
                  d={edgePath(edge)}
                />
              )}
            </For>
          </svg>
          <For each={nodes()}>
            {(node) => (
              <button
                type="button"
                class="graph-node"
                classList={{
                  "graph-node-selected": selected().has(node.url),
                  "graph-node-lit": litURLs().has(node.url) && !selected().has(node.url),
                }}
                style={{ left: `${node.x}px`, top: `${node.y}px`, "--graph-node-w": `${NODE_W}px` }}
                ref={(el) => nodeRefs.set(node.url, el)}
                aria-pressed={selected().has(node.url)}
                onClick={() => toggle(node.url)}
                onKeyDown={(e) => onNodeKeyDown(e, node)}
              >
                <span
                  class={`pill pill-${node.tone}${node.unattended ? " pill-disc" : " pill-ring"}${node.alive ? " pill-pulse" : ""}`}
                >
                  {node.state}
                </span>
                <span class="title">
                  {ticketRef(node.url)} {node.title || "untitled"}
                </span>
              </button>
            )}
          </For>
        </div>
      </div>
    </div>
  );
});
