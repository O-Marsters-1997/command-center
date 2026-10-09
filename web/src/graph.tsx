import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { COL_W, MARGIN, bounds as layoutBounds, edgePath, edgesFor, layoutWaves, traceChain, waveLabel } from "./layout";
import type { Group, Row } from "./types";

const POLL_MS = 5000;
const PHONE = "(max-width: 759px)";

declare global {
  interface Window {
    htmx?: { ajax: (verb: string, path: string, options: Record<string, unknown>) => void };
  }
}

interface GraphNode extends Row {
  col: number;
  x: number;
  y: number;
}

type Edge = ReturnType<typeof edgesFor<GraphNode>>[number];

// <For> keys children by object identity, so nodes are mutated in place to keep a node button's
// DOM and keyboard focus stable across a poll.
const nodeCache = new Map<string, GraphNode>();

function toNode(row: Row, col: number, x: number, y: number): GraphNode {
  const node = nodeCache.get(row.url) ?? ({} as GraphNode);
  Object.assign(node, row, { col, x, y });
  nodeCache.set(row.url, node);
  return node;
}

function ticketRef(url: string): string {
  const parts = url.split("/");
  return `#${parts[parts.length - 1]}`;
}

customElement("cc-graph", {}, () => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  const element = getCurrentElement();
  const src = element.dataset.src ?? `/graph.json${window.location.search}`;
  element.textContent = "";

  const [groups, setGroups] = createSignal<Group[]>([]);
  const [selected, setSelected] = createSignal<Set<string>>(new Set());
  const [hovered, setHovered] = createSignal<string | null>(null);
  const [pinned, setPinned] = createSignal<string | null>(null);

  const nodeRefs = new Map<string, HTMLButtonElement>();

  async function load() {
    try {
      const res = await fetch(src);
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

  const nodes = createMemo(() => {
    const rows = groups().flatMap((g) => (g.root ? [g.root, ...g.children] : g.children));
    return layoutWaves(rows).map((p) => toNode(p, p.col, p.x, p.y));
  });
  const edges = createMemo(() => edgesFor(nodes(), (n) => n.blocking));
  const bounds = createMemo(() => layoutBounds(nodes()));

  const waveCount = createMemo(() => nodes().reduce((m, n) => Math.max(m, n.col + 1), 0));

  const traced = createMemo(() => {
    const from = hovered() ?? pinned();
    return from ? traceChain(from, edges()) : new Set<string>();
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
    window.htmx?.ajax("POST", "/launch/open", {
      target: "#launch-modal",
      swap: "innerHTML",
      values: { ticket: [...selected()] },
    });
  }

  function onNodeClick(url: string) {
    if (window.matchMedia(PHONE).matches) setPinned((prev) => (prev === url ? null : url));
    else toggle(url);
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
    <div>
      <div class="mb-[0.4rem] flex items-center gap-[0.6rem] text-[0.85em] text-muted">
        <span>{selected().size} selected</span>
        <button type="button" class="ml-auto" disabled={selected().size === 0} onClick={submit}>
          launch selected
        </button>
      </div>
      <div class="relative h-[70vh] overflow-auto rounded border border-border" data-graph-panel>
        <div class="relative" style={{ width: `${bounds().width}px`, height: `${bounds().height}px` }}>
          <For each={Array.from({ length: waveCount() }, (_, i) => i)}>
            {(col) => (
              <span
                class="absolute top-1 text-[0.75em] uppercase tracking-wide text-muted"
                style={{ left: `${MARGIN + col * COL_W}px` }}
              >
                {waveLabel(col)}
              </span>
            )}
          </For>
          <svg width={bounds().width} height={bounds().height} aria-hidden="true" role="presentation">
            <For each={edges()}>
              {(edge) => {
                const lit = () => traced().has(edge.from.url) && traced().has(edge.to.url);
                return (
                  <path
                    class="fill-none"
                    classList={{
                      "stroke-border stroke-[1.5]": !lit(),
                      "stroke-s-live stroke-[2.5]": lit(),
                    }}
                    d={edgePath(edge)}
                  />
                );
              }}
            </For>
          </svg>
          <For each={nodes()}>
            {(node) => {
              const isSelected = () => selected().has(node.url);
              const isLit = () => isSelected() || traced().has(node.url);
              return (
                <button
                  type="button"
                  data-node
                  class="absolute flex w-[200px] cursor-pointer items-center gap-[0.4rem] rounded border bg-bg px-2 py-[0.3rem] text-left [font:inherit] text-inherit"
                  classList={{
                    "border-border": !isLit(),
                    "border-s-live": isLit(),
                    "shadow-[0_0_0_1px_var(--color-s-live)]": isSelected(),
                  }}
                  style={{ left: `${node.x}px`, top: `${node.y}px` }}
                  ref={(el) => nodeRefs.set(node.url, el)}
                  aria-pressed={isSelected()}
                  onClick={() => onNodeClick(node.url)}
                  onPointerEnter={(e) => e.pointerType === "mouse" && setHovered(node.url)}
                  onPointerLeave={() => setHovered(null)}
                  onFocus={(e) => e.currentTarget.matches(":focus-visible") && setHovered(node.url)}
                  onBlur={() => setHovered(null)}
                  onKeyDown={(e) => onNodeKeyDown(e, node)}
                >
                  <span
                    class={`glyph glyph-${node.glyph}${node.alive ? " glyph-pulse" : ""}`}
                  >
                    {node.state}
                  </span>
                  <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[0.85em]">
                    {ticketRef(node.url)} {node.title || "untitled"}
                  </span>
                </button>
              );
            }}
          </For>
        </div>
      </div>
    </div>
  );
});
