import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { JSX } from "solid-js";
import { bounds, edgePath, edgesFor, stackByColumn } from "./layout";
import {
  type Candidate,
  REFUSED,
  cancelHref,
  columnsFor,
  dependentsOf,
  initialTicked,
  missingBlockersOf,
  sectionsFor,
} from "./launch";

function ticketRef(url: string): string {
  const parts = url.split("/");
  return `#${parts[parts.length - 1]}`;
}

function Banner(props: { children: JSX.Element }) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: <output> would break that byte-for-byte match.
    <p class="banner ribbon mb-2" data-glyph="failed" role="status">
      {props.children}
    </p>
  );
}

customElement("cc-launch-modal", { feature: "", tickets: "" }, (props: { feature: string; tickets: string }) => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  getCurrentElement().textContent = "";

  const [candidates, setCandidates] = createSignal<Candidate[]>([]);
  const [ticked, setTicked] = createSignal<Set<string>>(new Set());
  const [refusal, setRefusal] = createSignal<string | null>(null);
  const [loadError, setLoadError] = createSignal(false);
  const [loaded, setLoaded] = createSignal(false);

  onMount(async () => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") window.location.assign(cancelHref(props.feature));
    };
    document.addEventListener("keydown", onKey);
    onCleanup(() => document.removeEventListener("keydown", onKey));
    try {
      const query = props.tickets || `feature=${encodeURIComponent(props.feature)}`;
      const res = await fetch(`/launch/candidates?${query}`);
      if (!res.ok) throw new Error(String(res.status));
      const data: Candidate[] = await res.json();
      setCandidates(data);
      setTicked(initialTicked(data));
    } catch {
      setLoadError(true);
    } finally {
      setLoaded(true);
    }
  });

  function names(candidates: Candidate[]): string {
    return candidates.map((c) => `${ticketRef(c.url)} ${c.title}`).join(", ");
  }

  function toggle(c: Candidate) {
    if (c.label === REFUSED) return;
    if (!ticked().has(c.url)) {
      const missing = missingBlockersOf(candidates(), ticked(), c.url);
      if (missing.length > 0) {
        setRefusal(`${ticketRef(c.url)} is blocked by ${names(missing)} -- tick it first`);
        return;
      }
      setRefusal(null);
      setTicked((prev) => new Set(prev).add(c.url));
      return;
    }
    const dependents = dependentsOf(candidates(), ticked(), c.url);
    if (dependents.length > 0) {
      setRefusal(`${names(dependents)} depends on ${ticketRef(c.url)} -- clear it first`);
      return;
    }
    setRefusal(null);
    setTicked((prev) => {
      const next = new Set(prev);
      next.delete(c.url);
      return next;
    });
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
                      onChange={() => toggle(c)}
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
      <Show when={loaded()} fallback={<p class="text-muted">loading&hellip;</p>}>
        <Show when={!loadError()} fallback={<Banner>could not load candidates{props.feature && ` for ${props.feature}`}.</Banner>}>
          <Show when={refusal()}>{(message) => <Banner>{message()}</Banner>}</Show>

          <div
            class="relative mb-3 overflow-hidden rounded border border-border"
            style={{ width: `${dagBounds().width}px`, height: `${dagBounds().height}px` }}
          >
            <svg width={dagBounds().width} height={dagBounds().height} aria-hidden="true" role="presentation">
              <For each={dagEdges()}>
                {(edge) => <path class="fill-none stroke-border stroke-[1.5]" d={edgePath(edge)} />}
              </For>
            </svg>
            <For each={dagNodes()}>
              {(node) => (
                <div
                  class="absolute flex w-[200px] items-center overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-bg px-2 py-[0.3rem] text-[0.85em]"
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
              <a class="button" href={cancelHref(props.feature)}>
                Cancel
              </a>
              <button type="submit" class="button launch-confirm" disabled={tickedCandidates().length === 0}>
                Confirm launch
              </button>
            </div>
          </form>
        </Show>
      </Show>
    </div>
  );
});
