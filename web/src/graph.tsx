import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { bounds as layoutBounds, edgePath, edgesFor, layoutGroups } from "./layout";
import type { Group, Row } from "./types";

const POLL_MS = 5000;
const MIN_SCALE = 0.25;
const MAX_SCALE = 3;

interface GraphNode extends Row {
  col: number;
  x: number;
  y: number;
}

type Edge = ReturnType<typeof edgesFor<GraphNode>>[number];

// nodeCache keeps one object per URL across polls: <For> keys its children by object identity,
// so mutating a cached node in place (rather than spreading a fresh one every layoutGroups call)
// is what keeps a node button's own DOM -- and its keyboard focus -- stable across a 5s poll.
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
  getCurrentElement().textContent = "";

  const [groups, setGroups] = createSignal<Group[]>([]);
  const [selected, setSelected] = createSignal<Set<string>>(new Set());
  const [pan, setPan] = createSignal({ x: 0, y: 0 });
  const [scale, setScale] = createSignal(1);

  let viewport: HTMLDivElement | undefined;
  const nodeRefs = new Map<string, HTMLButtonElement>();

  async function load() {
    try {
      // Carries the page's own scope (e.g. ?repo=X) so /graph.json answers the same groups the
      // board renders (CONTEXT.md § Scope).
      const res = await fetch(`/graph.json${window.location.search}`);
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

  const nodes = createMemo(() => layoutGroups(groups()).map((p) => toNode(p.row, p.col, p.x, p.y)));
  const edges = createMemo(() => edgesFor(nodes(), (n) => n.blocking));
  const bounds = createMemo(() => layoutBounds(nodes()));

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
    const params = [...selected()].map((u) => `ticket=${encodeURIComponent(u)}`).join("&");
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
    if ((e.target as HTMLElement).closest("[data-node]")) return;
    dragging = { x: e.clientX, y: e.clientY, pan: pan() };
    viewport?.setPointerCapture(e.pointerId);
    viewport?.classList.replace("cursor-grab", "cursor-grabbing");
  }
  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    setPan({ x: dragging.pan.x + (e.clientX - dragging.x), y: dragging.pan.y + (e.clientY - dragging.y) });
  }
  function endDrag() {
    dragging = null;
    viewport?.classList.replace("cursor-grabbing", "cursor-grab");
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
    <div>
      <div class="mb-[0.4rem] flex items-center gap-[0.6rem] text-[0.85em] text-muted">
        <span>{selected().size} selected</span>
        <button type="button" class="ml-auto" onClick={resetView}>
          reset view
        </button>
        <button type="button" class="ml-auto" disabled={selected().size === 0} onClick={submit}>
          preview selection
        </button>
      </div>
      <div
        class="relative h-[70vh] cursor-grab touch-none select-none overflow-hidden rounded border border-border"
        ref={viewport}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onWheel={onWheel}
      >
        <div
          class="absolute left-0 top-0 origin-top-left"
          style={{
            transform: `translate(${pan().x}px, ${pan().y}px) scale(${scale()})`,
            width: `${bounds().width}px`,
            height: `${bounds().height}px`,
          }}
        >
          <svg width={bounds().width} height={bounds().height} aria-hidden="true" role="presentation">
            <For each={edges()}>
              {(edge) => {
                const lit = () => selected().has(edge.from.url) || selected().has(edge.to.url);
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
              const isLit = () => isSelected() || litURLs().has(node.url);
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
                  onClick={() => toggle(node.url)}
                  onKeyDown={(e) => onNodeKeyDown(e, node)}
                >
                  <span
                    class={`pill pill-${node.tone}${node.unattended ? " pill-disc" : " pill-ring"}${node.alive ? " pill-pulse" : ""}`}
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
