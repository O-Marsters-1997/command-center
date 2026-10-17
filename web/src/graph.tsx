import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { createStore } from "solid-js/store";
import { cardTag, subline, waveSubtitle } from "./graph-card";
import { type GraphNode, applyPoll } from "./graph-state";
import { defineIsland, hx } from "./island";
import {
  CARD,
  MARGIN,
  bounds as layoutBounds,
  edgePath,
  edgesFor,
  traceChain,
  waveLabel,
} from "./layout";
import type { Group } from "./types";

const POLL_MS = 5000;
const PHONE = "(max-width: 759px)";
const DIRECTIONS: Record<string, [number, number]> = {
  ArrowUp: [0, -1], ArrowDown: [0, 1], ArrowLeft: [-1, 0], ArrowRight: [1, 0],
};

type Status = { kind: "loading" } | { kind: "ok" } | { kind: "error"; message: string };

defineIsland("cc-graph", { src: "" }, (props, element) => {
  const src = props.src || element.dataset.src || "/graph.json";

  const [nodes, setNodes] = createStore<GraphNode[]>([]);
  const [status, setStatus] = createSignal<Status>({ kind: "loading" });
  const [focused, setFocused] = createSignal<string | null>(null);
  const [hovered, setHovered] = createSignal<string | null>(null);

  const nodeRefs = new Map<string, HTMLAnchorElement>();

  let inflight: AbortController | undefined;
  async function load() {
    if (document.hidden || inflight) return;
    const ctl = new AbortController();
    inflight = ctl;
    try {
      const res = await fetch(src, { signal: ctl.signal });
      if (!res.ok) throw new Error(`graph request failed: ${res.status}`);
      const groups: Group[] = await res.json();
      if (ctl.signal.aborted) return;
      applyPoll(setNodes, groups);
      setStatus({ kind: "ok" });
    } catch (err) {
      if (ctl.signal.aborted) return;
      setStatus({ kind: "error", message: err instanceof Error ? err.message : String(err) });
    } finally {
      if (inflight === ctl) inflight = undefined;
    }
  }

  let timer: ReturnType<typeof setInterval>;
  const onVisible = () => load();
  onMount(() => {
    load();
    timer = setInterval(load, POLL_MS);
    document.addEventListener("visibilitychange", onVisible);
  });
  onCleanup(() => {
    clearInterval(timer);
    document.removeEventListener("visibilitychange", onVisible);
    inflight?.abort();
  });

  const message = () => {
    const s = status();
    return s.kind === "error" ? `Graph out of date: ${s.message}` : "Loading graph";
  };

  const edges = createMemo(() => edgesFor(nodes, (n) => n.blocking));
  const bounds = createMemo(() => layoutBounds(nodes, CARD));

  const waveCount = createMemo(() => nodes.reduce((m, n) => Math.max(m, n.col + 1), 0));
  const cols = createMemo(() => Array.from({ length: waveCount() }, (_, col) => col));

  const phoneQuery = window.matchMedia(PHONE);
  const [phone, setPhone] = createSignal(phoneQuery.matches);
  const onPhoneChange = (e: MediaQueryListEvent) => {
    setHovered(null);
    setFocused(null);
    setPhone(e.matches);
  };
  phoneQuery.addEventListener("change", onPhoneChange);
  onCleanup(() => phoneQuery.removeEventListener("change", onPhoneChange));

  const traced = createMemo(() => {
    const from = hovered() ?? focused();
    return from ? traceChain(from, edges()) : new Set<string>();
  });

  function onNodeKeyDown(e: KeyboardEvent, node: GraphNode) {
    const dir = DIRECTIONS[e.key];
    if (!dir) return;
    e.preventDefault();
    const [dCol] = dir;
    const inDirection =
      dCol === 0
        ? nodes.filter((n) => n.col === node.col && n.url !== node.url)
        : nodes.filter((n) => Math.sign(n.col - node.col) === dCol);
    if (inDirection.length === 0) return;
    inDirection.sort((a, b) => Math.abs(a.y - node.y) - Math.abs(b.y - node.y));
    const nearest = inDirection[0];
    if (nearest) nodeRefs.get(nearest.url)?.focus();
  }

  function nodeLink(node: GraphNode, stacked: boolean) {
    const isLit = () => traced().has(node.url);
    const href = () => node.session_path;
    return (
      <a
        data-node
        class="grid cursor-pointer content-start gap-1 rounded-lg border px-[10px] py-[9px] text-left no-underline [font:inherit] text-inherit"
        classList={{
          absolute: !stacked,
          "w-full": stacked,
          "bg-surface": node.glyph !== "blocked",
          "bg-canvas border-dashed": node.glyph === "blocked",
          "border-border": !isLit() && node.glyph !== "done",
          "border-border-soft": !isLit() && node.glyph === "done",
          "border-s-attention": isLit(),
        }}
        style={stacked
          ? { height: `${CARD.nodeH}px` }
          : { left: `${node.x}px`, top: `${node.y}px`, width: `${CARD.nodeW}px`, height: `${CARD.nodeH}px` }}
        href={href() || undefined}
        hx-get={href() || undefined}
        hx-select="#main"
        hx-target="#main"
        hx-swap="outerHTML"
        hx-push-url="true"
        ref={(el) => {
          nodeRefs.set(node.url, el);
          onCleanup(() => nodeRefs.get(node.url) === el && nodeRefs.delete(node.url));
          hx(el);
        }}
        onPointerEnter={(e) => e.pointerType === "mouse" && setHovered(node.url)}
        onPointerLeave={() => setHovered(null)}
        onFocus={(e) => e.currentTarget.matches(":focus-visible") && setFocused(node.url)}
        onBlur={() => setFocused(null)}
        onKeyDown={(e) => onNodeKeyDown(e, node)}
      >
        <span class="flex items-center gap-[7px]">
          <span class={`glyph glyph-${node.glyph}${node.alive ? " glyph-pulse" : ""}`} title={node.label} />
          <span class="font-mono text-xs text-meta">{node.ref}</span>
          <span class="ml-auto truncate text-2xs text-muted">{cardTag(node)}</span>
        </span>
        <span
          class="line-clamp-2 overflow-hidden text-sm font-medium leading-[1.35]"
        >
          {node.title || "untitled"}
        </span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted">
          {subline(node)}
        </span>
      </a>
    );
  }

  return (
    <div>
      <Show when={status().kind !== "ok"}>
        <output class="banner ribbon mb-3 block" data-glyph={status().kind === "error" ? "failed" : "pending"}>
          {message()}
        </output>
      </Show>
      <Show
        when={!phone()}
        fallback={
          <div class="grid gap-4">
            <For each={cols()}>
              {(col) => (
                <section class="grid gap-2">
                  <h3 class="text-xs text-muted">
                    <b class="font-medium text-fg-2">{waveLabel(col)}</b> {waveSubtitle(col, nodes.filter((n) => n.col === col))}
                  </h3>
                  <For each={nodes.filter((n) => n.col === col)}>{(node) => nodeLink(node, true)}</For>
                </section>
              )}
            </For>
          </div>
        }
      >
        <div class="relative h-[70vh] overflow-auto rounded border border-border" data-graph-panel>
          <div class="relative" style={{ width: `${bounds().width}px`, height: `${bounds().height}px` }}>
            <For each={cols()}>
              {(col) => (
                <span
                  class="absolute top-1 w-60 truncate text-xs text-muted"
                  style={{ left: `${MARGIN + col * CARD.colW}px` }}
                >
                  <b class="font-medium text-fg-2">{waveLabel(col)}</b> {waveSubtitle(col, nodes.filter((n) => n.col === col))}
                </span>
              )}
            </For>
            <svg width={bounds().width} height={bounds().height} aria-hidden="true">
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
            <For each={nodes}>{(node) => nodeLink(node, false)}</For>
          </div>
        </div>
      </Show>
    </div>
  );
});
