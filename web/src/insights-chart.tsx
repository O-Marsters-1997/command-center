import { Chart } from "@tanstack/charts/solid";
import { Match, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { defineIsland } from "./island";
import { type ChartWire, parseWire, ticketChart, weekChart } from "./insights-chart.ts";

type Status = { kind: "loading" } | { kind: "ok"; wire: ChartWire } | { kind: "error"; message: string };

const accent = () =>
  getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim() || "gray";

defineIsland("cc-insights-chart", { src: "" }, (props, element) => {
  const src = props.src || element.getAttribute("src") || "/insights/chart";
  const [status, setStatus] = createSignal<Status>({ kind: "loading" });

  const ctl = new AbortController();
  onMount(async () => {
    try {
      const res = await fetch(src, { signal: ctl.signal });
      if (!res.ok) throw new Error(`chart request failed: ${res.status}`);
      setStatus({ kind: "ok", wire: parseWire(await res.json()) });
    } catch (err) {
      if (ctl.signal.aborted) return;
      setStatus({ kind: "error", message: err instanceof Error ? err.message : String(err) });
    }
  });
  onCleanup(() => ctl.abort());

  const wire = () => {
    const s = status();
    return s.kind === "ok" ? s.wire : undefined;
  };
  const definition = createMemo(() => {
    const w = wire();
    if (!w) return undefined;
    return w.view === "week" ? weekChart(w, accent()) : ticketChart(w);
  });

  const open = (point: { datum: unknown } | null) => {
    const href = (point?.datum as { href?: string } | undefined)?.href;
    if (href) window.location.assign(href);
  };

  return (
    <Switch>
      <Match when={status().kind === "loading"}>
        <p class="text-muted">Loading chart…</p>
      </Match>
      <Match when={status().kind === "error"}>
        <p class="banner" role="alert">
          Chart out of date: {(status() as { message: string }).message}
        </p>
      </Match>
      <Match when={definition()}>
        {(def) => <Chart definition={def()} ariaLabel="Merged ticket cost" aspectRatio={3} onSelect={open} />}
      </Match>
    </Switch>
  );
});
