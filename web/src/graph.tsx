import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { CARD, MARGIN, bounds as layoutBounds, edgePath, edgesFor, layoutWaves, traceChain, waveLabel } from "./layout";
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

function nodeTag(node: GraphNode): string {
  if (node.glyph === "failed") return node.reason;
  if (node.glyph === "attention") return "Review";
  return node.glyph === "running" ? node.elapsed : "";
}

function nodeFoot(node: GraphNode): string {
  switch (node.glyph) {
    case "blocked":
      return node.after?.length ? `after ${node.after.join(", ")}` : "Blocked";
    case "ready":
      return "Ready to launch";
    case "done":
      return node.pr_number ? `Merged #${node.pr_number}` : "Merged";
    default:
      return node.reason || (node.pr_number ? `PR #${node.pr_number}` : node.state);
  }
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
  const [focused, setFocused] = createSignal<string | null>(null);
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
    return layoutWaves(rows, CARD).map((p) => toNode(p, p.col, p.x, p.y));
  });
  const edges = createMemo(() => edgesFor(nodes(), (n) => n.blocking));
  const bounds = createMemo(() => layoutBounds(nodes(), CARD));

  const waveCount = createMemo(() => nodes().reduce((m, n) => Math.max(m, n.col + 1), 0));
  const waves = createMemo(() =>
    Array.from({ length: waveCount() }, (_, col) => ({ col, nodes: nodes().filter((n) => n.col === col) })),
  );

  const phoneQuery = window.matchMedia(PHONE);
  const [phone, setPhone] = createSignal(phoneQuery.matches);
  const onPhoneChange = (e: MediaQueryListEvent) => setPhone(e.matches);
  phoneQuery.addEventListener("change", onPhoneChange);
  onCleanup(() => phoneQuery.removeEventListener("change", onPhoneChange));

  const traced = createMemo(() => {
    const from = hovered() ?? focused() ?? pinned();
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
    toggle(url);
    if (window.matchMedia(PHONE).matches) setPinned((prev) => (prev === url ? null : url));
    else setPinned(null);
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

  function nodeButton(node: GraphNode, stacked: boolean) {
    const isSelected = () => selected().has(node.url);
    const isLit = () => isSelected() || traced().has(node.url);
    return (
      <button
        type="button"
        data-node
        class="grid h-[104px] cursor-pointer content-start gap-1 rounded-lg border px-[10px] py-[9px] text-left [font:inherit] text-inherit"
        classList={{
          "absolute w-[240px]": !stacked,
          "w-full": stacked,
          "bg-surface": node.glyph !== "blocked",
          "bg-canvas border-dashed": node.glyph === "blocked",
          "border-border": !isLit() && node.glyph !== "done",
          "border-border-soft": !isLit() && node.glyph === "done",
          "border-s-attention": isLit(),
          "shadow-[0_0_0_1px_var(--color-s-attention)]": isSelected(),
        }}
        style={stacked ? undefined : { left: `${node.x}px`, top: `${node.y}px` }}
        ref={(el) => nodeRefs.set(node.url, el)}
        aria-pressed={isSelected()}
        onClick={() => onNodeClick(node.url)}
        onPointerEnter={(e) => e.pointerType === "mouse" && setHovered(node.url)}
        onPointerLeave={() => setHovered(null)}
        onFocus={(e) => e.currentTarget.matches(":focus-visible") && setFocused(node.url)}
        onBlur={() => setFocused(null)}
        onKeyDown={(e) => onNodeKeyDown(e, node)}
      >
        <span class="flex items-center gap-[7px]">
          <span class={`glyph glyph-${node.glyph}${node.alive ? " glyph-pulse" : ""}`}>{node.state}</span>
          <span class="text-xs text-meta">{ticketRef(node.url)}</span>
          <span class="ml-auto text-2xs text-muted">{nodeTag(node)}</span>
        </span>
        <span
          class="overflow-hidden text-sm font-medium leading-[1.35]"
          style={{ display: "-webkit-box", "-webkit-line-clamp": 2, "-webkit-box-orient": "vertical" }}
        >
          {node.title || "untitled"}
        </span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted">{nodeFoot(node)}</span>
      </button>
    );
  }

  return (
    <div>
      <div class="mb-[0.4rem] flex items-center gap-[0.6rem] text-[0.85em] text-muted">
        <span>{selected().size} selected</span>
        <button type="button" class="btn ml-auto" disabled={selected().size === 0} onClick={submit}>
          launch selected
        </button>
      </div>
      <Show
        when={!phone()}
        fallback={
          <div class="grid gap-4">
            <For each={waves()}>
              {(wave) => (
                <section class="grid gap-2" aria-label={waveLabel(wave.col)}>
                  <h3 class="text-xs font-medium text-fg-2">{waveLabel(wave.col)}</h3>
                  <For each={wave.nodes}>{(node) => nodeButton(node, true)}</For>
                </section>
              )}
            </For>
          </div>
        }
      >
        <div class="relative h-[70vh] overflow-auto rounded border border-border" data-graph-panel>
          <div class="relative" style={{ width: `${bounds().width}px`, height: `${bounds().height}px` }}>
            <For each={Array.from({ length: waveCount() }, (_, i) => i)}>
              {(col) => (
                <span
                  class="absolute top-1 text-xs font-medium text-fg-2"
                  style={{ left: `${MARGIN + col * CARD.colW}px` }}
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
                        "stroke-s-attention stroke-[2.5]": lit(),
                      }}
                      d={edgePath(edge, CARD)}
                    />
                  );
                }}
              </For>
            </svg>
            <For each={nodes()}>{(node) => nodeButton(node, false)}</For>
          </div>
        </div>
      </Show>
    </div>
  );
});
