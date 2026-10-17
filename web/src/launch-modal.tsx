import { For, Match, Show, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { JSX } from "solid-js";
import { defineIsland, hx } from "./island";
import {
  type Candidate,
  REFUSED,
  cancelHref,
  initialTicked,
  launchLabel,
  sectionsFor,
  toggleResult,
  whenLabel,
} from "./launch";

type Status = "loading" | "error" | "ready";

function Section(props: {
  title: string;
  hint: string;
  glyph: string;
  rows: Candidate[];
  all: Candidate[];
  ticked: Set<string>;
  toggle: (c: Candidate, box: HTMLInputElement) => void;
}) {
  return (
    <Show when={props.rows.length > 0}>
      <section class="launch-section">
        <h3 class="launch-section-title">
          {props.title} <span class="text-muted">{props.hint}</span>
        </h3>
        <ul class="launch-rows">
          <For each={props.rows}>
            {(c) => (
              <li class="launch-row">
                <label class="launch-row-main">
                  <input
                    type="checkbox"
                    checked={props.ticked.has(c.url)}
                    onChange={(e) => props.toggle(c, e.currentTarget)}
                  />
                  <Show when={props.ticked.has(c.url)}>
                    <input type="hidden" name="ticket" value={c.url} />
                    <input type="hidden" name="hash" value={`${c.url} ${c.prompt_hash}`} />
                  </Show>
                  <span class="glyph" data-glyph={props.glyph} aria-hidden="true" />
                  <span class="launch-row-ref">{c.ref}</span>
                  <span class="launch-row-title">{c.title}</span>
                </label>
                <span class="launch-row-meta">{whenLabel(c, props.all)}</span>
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

const split = (list: string) => (list ? list.split(",") : []);

defineIsland("cc-launch-modal", { feature: "", tickets: "", models: "", model: "", efforts: "" }, (props, element) => {
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

  const sections = createMemo(() => sectionsFor(candidates()));
  const label = createMemo(() => launchLabel(candidates(), ticked()));

  function submitOnChord(e: KeyboardEvent & { currentTarget: HTMLFormElement }) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && ticked().size > 0) {
      e.preventDefault();
      e.currentTarget.requestSubmit();
    }
  }

  return (
    <div class="launch-island">
      <Switch>
        <Match when={status() === "loading"}>
          <div class="launch-body">
            <p class="text-muted">loading&hellip;</p>
          </div>
        </Match>
        <Match when={status() === "error"}>
          <div class="launch-body">
            <Banner>could not load candidates{props.feature && ` for ${props.feature}`}.</Banner>
          </div>
          <div class="launch-foot">
            <a class="btn btn-ghost" href={cancelHref(props.feature)}>
              Cancel
            </a>
          </div>
        </Match>
        <Match when={status() === "ready"}>
          <form class="launch-form" method="post" action="/launch" onKeyDown={submitOnChord}>
            <div class="launch-body">
              <Show when={refusal()}>{(message) => <Banner>{message()}</Banner>}</Show>
              <Section
                title="Ready now"
                hint="no blockers, launch in fresh worktrees"
                glyph="ready"
                rows={sections().now}
                all={candidates()}
                ticked={ticked()}
                toggle={toggle}
              />
              <Section
                title="On unlock"
                hint="armed, each launches the moment its blockers merge"
                glyph="blocked"
                rows={sections().onUnlock}
                all={candidates()}
                ticked={ticked()}
                toggle={toggle}
              />
              <Show when={sections().refused.length > 0}>
                <details class="launch-flight">
                  <summary>{sections().refused.length} already in flight or refused</summary>
                  <ul>
                    <For each={sections().refused}>
                      {(c) => (
                        <li>
                          <span class="glyph" data-glyph="pending" aria-hidden="true" />
                          <span class="launch-row-ref">{c.ref}</span>
                          <span class="launch-row-title">{c.title}</span>
                          <span class="launch-row-meta">{c.label === REFUSED ? c.reason : c.label}</span>
                        </li>
                      )}
                    </For>
                  </ul>
                </details>
              </Show>
            </div>
            <div class="launch-foot">
              <div class="composer">
                <label class="sr-only" for="launch-context">
                  Extra context for every agent
                </label>
                <textarea
                  id="launch-context"
                  name="context"
                  rows="2"
                  placeholder="Extra context for every agent in this launch (optional)"
                />
                <div class="composer-bar">
                  <select class="select" name="model" aria-label="Model">
                    <For each={split(props.models)}>
                      {(m) => (
                        <option value={m} selected={m === props.model}>
                          {m}
                        </option>
                      )}
                    </For>
                  </select>
                  <select class="select" name="effort" aria-label="Effort">
                    <option value="">default effort</option>
                    <For each={split(props.efforts)}>{(e) => <option value={e}>{e}</option>}</For>
                  </select>
                  <span class="composer-chip">base origin/main</span>
                </div>
              </div>
              <div class="launch-actions">
                <a class="btn btn-ghost" href={cancelHref(props.feature)}>
                  Cancel
                </a>
                <button type="submit" class="btn btn-primary" disabled={ticked().size === 0}>
                  {label()} <kbd>⌘↵</kbd>
                </button>
              </div>
            </div>
          </form>
        </Match>
      </Switch>
    </div>
  );
});
