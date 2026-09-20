import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Show, createMemo, createSignal, onMount } from "solid-js";
import { niceTicks } from "./charts";

interface Bucket {
  day: string;
  tokens_in: number;
  tokens_out: number;
  runs: number;
  unsettled: number;
}

interface InsightsResponse {
  buckets: Bucket[];
}

const WIDTH = 720;
const HEIGHT = 220;
const PAD_LEFT = 52;
const PAD_TOP = 8;
const PAD_BOTTOM = 20;
const PLOT_WIDTH = WIDTH - PAD_LEFT;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;

customElement("cc-insights", {}, () => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  getCurrentElement().textContent = "";

  const [buckets, setBuckets] = createSignal<Bucket[]>([]);

  onMount(async () => {
    try {
      const res = await fetch(`/insights.json${window.location.search}`);
      if (!res.ok) return;
      const body: InsightsResponse = await res.json();
      setBuckets(body.buckets);
    } catch {}
  });

  const ticks = createMemo(() => niceTicks(Math.max(0, ...buckets().map((b) => b.tokens_in + b.tokens_out))));
  const top = () => ticks().values[ticks().values.length - 1] || 1;
  const scaleY = (v: number) => PAD_TOP + PLOT_HEIGHT - (v / top()) * PLOT_HEIGHT;
  const barWidth = () => (buckets().length === 0 ? 0 : PLOT_WIDTH / buckets().length);

  return (
    <div>
      <h2 class="mb-2 text-[0.9em] text-muted">tokens per day</h2>
      <Show when={buckets().length > 0} fallback={<p class="text-muted">no runs in range.</p>}>
        <svg width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img">
          <title>input and output tokens per day, {buckets().length} days</title>
          <For each={ticks().values}>
            {(t) => (
              <g>
                <line class="chart-grid" x1={PAD_LEFT} x2={WIDTH} y1={scaleY(t)} y2={scaleY(t)} />
                <text class="chart-tick" x={PAD_LEFT - 4} y={scaleY(t)} dy="0.32em" text-anchor="end">
                  {t}
                </text>
              </g>
            )}
          </For>
          <For each={buckets()}>
            {(bucket, i) => {
              const x = PAD_LEFT + i() * barWidth() + 1;
              const w = Math.max(0, barWidth() - 2);
              const inTop = scaleY(bucket.tokens_in);
              const outTop = scaleY(bucket.tokens_in + bucket.tokens_out);
              return (
                <g>
                  <rect class="chart-bar" x={x} y={inTop} width={w} height={PAD_TOP + PLOT_HEIGHT - inTop}>
                    <title>{`${bucket.day}: ${bucket.tokens_in} in`}</title>
                  </rect>
                  <rect class="chart-bar-out" x={x} y={outTop} width={w} height={inTop - outTop}>
                    <title>{`${bucket.day}: ${bucket.tokens_out} out`}</title>
                  </rect>
                  <Show when={bucket.unsettled > 0}>
                    <circle class="chart-unsettled" cx={x + w / 2} cy={outTop - 5} r={2.5}>
                      <title>{`${bucket.day}: ${bucket.unsettled} unsettled run(s)`}</title>
                    </circle>
                  </Show>
                </g>
              );
            }}
          </For>
        </svg>
        <div class="mt-1 flex items-center gap-3 text-[0.8em] text-muted">
          <span class="chart-legend"><span class="chart-legend-swatch chart-bar" /> input</span>
          <span class="chart-legend"><span class="chart-legend-swatch chart-bar-out" /> output</span>
          <span class="chart-legend"><span class="chart-legend-swatch chart-unsettled" /> unsettled</span>
        </div>
      </Show>
    </div>
  );
});
