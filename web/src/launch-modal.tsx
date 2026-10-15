import { For, Match, Show, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { JSX } from "solid-js";
import { defineIsland, hx } from "./island";
import { COMPACT, bounds, edgePath, edgesFor, stackByColumn } from "./layout";
import {
  type Candidate,
  REFUSED,
  cancelHref,
  columnsFor,
  initialTicked,
  sectionsFor,
  toggleResult,
} from "./launch";

type Status = "loading" | "error" | "ready";

function Section(props: {
  title: string;
  rows: Candidate[];
  ticked: Set<string>;
  toggle: (c: Candidate, box: HTMLInputElement) => void;
}) {
  return (
    <Show when={props.rows.length > 0}>
      <section class="launch-section">
        <h3 class="launch-section-title">
          {props.title} <span class="text-muted">{props.rows.length}</span>
        </h3>
        <ul class="launch-rows">
          <For each={props.rows}>
            {(c) => (
              <li class="launch-row max-[759px]:flex-col max-[759px]:items-stretch">
                <label class="launch-row-main">
                  <input
                    type="checkbox"
                    checked={props.ticked.has(c.url)}
                    disabled={c.label === REFUSED}
                    onChange={(e) => props.toggle(c, e.currentTarget)}
                  />
                  <Show when={props.ticked.has(c.url) && c.label !== REFUSED}>
                    <input type="hidden" name="ticket" value={c.url} />
                    <input type="hidden" name="hash" value={`${c.url} ${c.prompt_hash}`} />
                  </Show>
                  <span class="launch-row-ref">{c.ref}</span>
                  <span>{c.title}</span>
                </label>
                <span class="launch-row-meta min-w-0 wrap-anywhere text-muted max-[759px]:ml-0 max-[759px]:pl-[25px]">
                  {c.base} &middot; {c.base_verdict}
                  {c.reason && <> &middot; {c.reason}</>}
                </span>
              </li>
            )}
          </For>
        </ul>
      </section>
    </Show>
  );
}

function Banner(props: { children: JSX.Element }) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: <output> would break that byte-for-byte match.
    <p class="banner ribbon mb-2" data-glyph="failed" role="status">
      {props.children}
    </p>
  );
}

defineIsland(
  "cc-launch-modal",
  { feature: "", tickets: "" },
  (props, element) => {
    const [candidates, setCandidates] = createSignal<Candidate[]>([]);
    const [ticked, setTicked] = createSignal<Set<string>>(new Set());
    const [refusal, setRefusal] = createSignal<string | null>(null);
    const [status, setStatus] = createSignal<Status>("loading");

    onMount(async () => {
      hx(element);
      const dialog = element.closest("dialog");
      if (dialog) {
        const onClose = () => {
          dialog.remove();
          if (window.location.pathname.endsWith("/launch")) {
            window.history.replaceState(null, "", cancelHref(props.feature));
          }
        };
        dialog.addEventListener("close", onClose);
        onCleanup(() => dialog.removeEventListener("close", onClose));
        dialog.removeAttribute("open");
        dialog.showModal();
      }

      const controller = new AbortController();
      onCleanup(() => controller.abort());
      try {
        const query = props.tickets || `feature=${encodeURIComponent(props.feature)}`;
        const res = await fetch(`/launch/candidates.json?${query}`, { signal: controller.signal });
        if (!res.ok) throw new Error(String(res.status));
        const data: Candidate[] = await res.json();
        setCandidates(data);
        setTicked(initialTicked(data));
        setStatus("ready");
      } catch (err) {
        if (controller.signal.aborted) return;
        console.error("launch candidates", err);
        setStatus("error");
      }
    });

    function toggle(c: Candidate, box: HTMLInputElement) {
      const result = toggleResult(candidates(), ticked(), c.url);
      if ("refusal" in result) {
        setRefusal(result.refusal);
      } else {
        setRefusal(null);
        setTicked(result.ticked);
      }
      box.checked = ticked().has(c.url);
    }

    const tickedCandidates = createMemo(() => candidates().filter((c) => ticked().has(c.url)));

    const dagNodes = createMemo(() => {
      const list = tickedCandidates();
      const cols = columnsFor(list);
      return stackByColumn(list.map((c) => ({ ...c, col: cols.get(c.url) ?? 0 })));
    });
    const dagEdges = createMemo(() => edgesFor(dagNodes(), (n) => n.blocked_by));
    const dagBounds = createMemo(() => bounds(dagNodes()));

    const sections = createMemo(() => sectionsFor(candidates()));

    return (
      <div>
        <Switch>
          <Match when={status() === "loading"}>
            <p class="text-muted">loading&hellip;</p>
          </Match>
          <Match when={status() === "error"}>
            <Banner>could not load candidates{props.feature && ` for ${props.feature}`}.</Banner>
            <a class="btn btn-ghost" href={cancelHref(props.feature)}>
              Cancel
            </a>
          </Match>
          <Match when={status() === "ready"}>
            <Show when={refusal()}>{(message) => <Banner>{message()}</Banner>}</Show>

            <div
              class="mb-3 max-w-full overflow-x-auto rounded border border-border"
              style={{ width: `${dagBounds().width}px` }}
            >
              <div class="relative" style={{ width: `${dagBounds().width}px`, height: `${dagBounds().height}px` }}>
                <svg width={dagBounds().width} height={dagBounds().height} aria-hidden="true">
                  <For each={dagEdges()}>
                    {(edge) => <path class="fill-none stroke-border stroke-[1.5]" d={edgePath(edge)} />}
                  </For>
                </svg>
                <For each={dagNodes()}>
                  {(node) => (
                    <div
                      class="absolute flex items-center overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-surface px-2 py-[0.3rem] text-[0.85em]"
                      style={{ left: `${node.x}px`, top: `${node.y}px`, width: `${COMPACT.nodeW}px` }}
                    >
                      {node.ref} {node.title || "untitled"}
                    </div>
                  )}
                </For>
              </div>
            </div>

            <form method="post" action="/launch">
              <Section title="Ready now" rows={sections().now} ticked={ticked()} toggle={toggle} />
              <Section title="On unlock" rows={sections().onUnlock} ticked={ticked()} toggle={toggle} />
              <Section title="Refused" rows={sections().refused} ticked={ticked()} toggle={toggle} />
              <div class="launch-actions">
                <a class="btn btn-ghost" href={cancelHref(props.feature)}>
                  Cancel
                </a>
                <button type="submit" class="btn btn-primary" disabled={tickedCandidates().length === 0}>
                  Confirm launch
                </button>
              </div>
            </form>
          </Match>
        </Switch>
      </div>
    );
  },
);
