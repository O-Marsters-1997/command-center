import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Show, createMemo, createSignal, onMount } from "solid-js";
import type { JSX } from "solid-js";
import { bounds, edgePath, edgesFor, stackByColumn } from "./layout";
import {
  type Candidate,
  REFUSED,
  columnsFor,
  dependentsOf,
  initialTicked,
  missingBlockersOf,
} from "./launch";

function ticketRef(url: string): string {
  const parts = url.split("/");
  return `#${parts[parts.length - 1]}`;
}

// Banner is the same markup launch_modal.tmpl's Pending/Refused/Empty branches and features.tmpl's
// import-error banner render, so a refusal reads identically whether Go or Solid drew it (ADR 2).
function Banner(props: { children: JSX.Element }) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: <output> would break that byte-for-byte match.
    <p class="banner ribbon mb-2" data-tone="stop" role="status">
      {props.children}
    </p>
  );
}

customElement("cc-launch-modal", { feature: "" }, (props: { feature: string }) => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  getCurrentElement().textContent = "";

  const [candidates, setCandidates] = createSignal<Candidate[]>([]);
  const [ticked, setTicked] = createSignal<Set<string>>(new Set());
  const [refusal, setRefusal] = createSignal<string | null>(null);
  const [loadError, setLoadError] = createSignal(false);
  const [loaded, setLoaded] = createSignal(false);

  // One fetch for the whole modal lifetime (CONTEXT.md § Launch modal): every toggle after this
  // reshapes the DAG from data already in hand, and only confirm talks to the server again.
  onMount(async () => {
    try {
      const res = await fetch(`/launch/candidates?feature=${encodeURIComponent(props.feature)}`);
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

  return (
    <div>
      <Show when={loaded()} fallback={<p class="text-muted">loading&hellip;</p>}>
        <Show when={!loadError()} fallback={<Banner>could not load candidates for {props.feature}.</Banner>}>
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
            <table class="mb-3 w-full max-w-4xl">
              <tbody>
                <tr>
                  <th />
                  <th>label</th>
                  <th>ticket</th>
                  <th>base</th>
                  <th>base verdict</th>
                  <th>why</th>
                </tr>
                <For each={candidates()}>
                  {(c) => (
                    <tr>
                      <td>
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
                      </td>
                      <td>{c.label}</td>
                      <td>
                        {c.ref} {c.title}
                      </td>
                      <td>{c.base}</td>
                      <td>{c.base_verdict}</td>
                      <td class="text-muted">{c.reason}</td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
            <button type="submit" disabled={tickedCandidates().length === 0}>
              [ confirm ]
            </button>
          </form>
        </Show>
      </Show>
    </div>
  );
});
