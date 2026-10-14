import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Match, Show, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { JSX } from "solid-js";
import { bounds, edgePath, edgesFor, stackByColumn } from "./layout";
import {
  type Candidate,
  REFUSED,
  cancelHref,
  columnsFor,
  initialTicked,
  sectionsFor,
  ticketRef,
  toggleResult,
} from "./launch";

type Status = "loading" | "error" | "ready";

function Banner(props: { children: JSX.Element }) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: <output> would break that byte-for-byte match.
    <p class="banner ribbon mb-2" data-glyph="failed" role="status">
      {props.children}
    </p>
  );
}

customElement(
  "cc-launch-modal",
  { feature: "", tickets: "" },
  (props: { feature: string; tickets: string }) => {
    noShadowDOM();
    // solid-element inserts into the element rather than clearing it first, so the light-DOM
    // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
    getCurrentElement().textContent = "";

    const [candidates, setCandidates] = createSignal<Candidate[]>([]);
    const [ticked, setTicked] = createSignal<Set<string>>(new Set());
    const [refusal, setRefusal] = createSignal<string | null>(null);
    const [status, setStatus] = createSignal<Status>("loading");

    onMount(async () => {
      const dialog = getCurrentElement().closest("dialog");
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

    function Section(sec: { title: string; rows: Candidate[] }) {
      return (
        <Show when={sec.rows.length > 0}>
          <section class="launch-section" aria-label={sec.title}>
            <h3 class="launch-section-title">
              {sec.title} <span class="text-muted">{sec.rows.length}</span>
            </h3>
            <ul class="launch-rows">
              <For each={sec.rows}>
                {(c) => (
                  <li class="launch-row">
                    <label class="launch-row-main">
                      <input
                        type="checkbox"
                        checked={ticked().has(c.url)}
                        disabled={c.label === REFUSED}
                        onChange={(e) => toggle(c, e.currentTarget)}
                      />
                      <Show when={ticked().has(c.url) && c.label !== REFUSED}>
                        <input type="hidden" name="ticket" value={c.url} />
                        <input type="hidden" name="hash" value={`${c.url} ${c.prompt_hash}`} />
                      </Show>
                      <span class="launch-row-ref">{c.ref}</span>
                      <span>{c.title}</span>
                    </label>
                    <span class="launch-row-meta text-muted">
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
              class="relative mb-3 overflow-hidden rounded border border-border"
              style={{ width: `${dagBounds().width}px`, height: `${dagBounds().height}px` }}
            >
              <svg
                width={dagBounds().width}
                height={dagBounds().height}
                aria-hidden="true"
                role="presentation"
              >
                <For each={dagEdges()}>
                  {(edge) => <path class="fill-none stroke-border stroke-[1.5]" d={edgePath(edge)} />}
                </For>
              </svg>
              <For each={dagNodes()}>
                {(node) => (
                  <div
                    class="absolute flex w-[200px] items-center overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-surface px-2 py-[0.3rem] text-[0.85em]"
                    style={{ left: `${node.x}px`, top: `${node.y}px` }}
                  >
                    {ticketRef(node.url)} {node.title || "untitled"}
                  </div>
                )}
              </For>
            </div>

            <form method="post" action="/launch">
              <Section title="Ready now" rows={sections().now} />
              <Section title="On unlock" rows={sections().onUnlock} />
              <Section title="Refused" rows={sections().refused} />
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
