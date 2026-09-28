import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import { For, Show, createMemo, createSignal, onMount } from "solid-js";
import { niceTicks, rollingMedian } from "./charts";

interface Point {
  ticket: string;
  title: string;
  merged_at: string;
  pct_week: number;
  agent_pct_week: number;
  resolve_pct_week: number;
  follow_up_pct_week: number;
}

interface InsightsResponse {
  points: Point[];
  waste_pct_week: number;
}

const WIDTH = 720;
const HEIGHT = 220;
const PAD_LEFT = 52;
const PAD_TOP = 8;
const PAD_BOTTOM = 20;
const PLOT_WIDTH = WIDTH - PAD_LEFT;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const MEDIAN_WINDOW = 10;

function pointTitle(p: Point): string {
  return (
    `${p.title || p.ticket}: ${p.pct_week.toFixed(2)}% week ` +
    `(agent ${p.agent_pct_week.toFixed(2)}, resolve ${p.resolve_pct_week.toFixed(2)}, ` +
    `follow-up ${p.follow_up_pct_week.toFixed(2)}), merged ${p.merged_at}`
  );
}

customElement("cc-insights", {}, () => {
  noShadowDOM();
  // solid-element inserts into the element rather than clearing it first, so the light-DOM
  // fallback content (the Go-only-build message) survives an upgrade unless it goes here.
  getCurrentElement().textContent = "";

  const [points, setPoints] = createSignal<Point[]>([]);
  const [wastePctWeek, setWastePctWeek] = createSignal(0);

  onMount(async () => {
    try {
      const res = await fetch(`/insights.json${window.location.search}`);
      if (!res.ok) return;
      const body: InsightsResponse = await res.json();
      setPoints(body.points);
      setWastePctWeek(body.waste_pct_week);
    } catch {}
  });

  const pctWeeks = createMemo(() => points().map((p) => p.pct_week));
  const median = createMemo(() => rollingMedian(pctWeeks(), MEDIAN_WINDOW));
  const ticks = createMemo(() => niceTicks(Math.max(0, wastePctWeek(), ...pctWeeks())));
  const top = () => ticks().values[ticks().values.length - 1] || 1;
  const scaleY = (v: number) => PAD_TOP + PLOT_HEIGHT - (v / top()) * PLOT_HEIGHT;
  const slotWidth = () => (points().length === 0 ? 0 : PLOT_WIDTH / points().length);
  const scaleX = (i: number) => PAD_LEFT + (i + 0.5) * slotWidth();
  const medianPath = () =>
    median()
      .map((v, i) => `${i === 0 ? "M" : "L"} ${scaleX(i)} ${scaleY(v)}`)
      .join(" ");

  return (
    <div>
      <h2 class="mb-2 text-[0.9em] text-muted">spend per merged ticket</h2>
      <Show when={points().length > 0} fallback={<p class="text-muted">no merged tickets in range.</p>}>
        <svg width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img">
          <title>spend as a percentage of the week, {points().length} merged tickets</title>
          <For each={ticks().values}>
            {(t) => (
              <g>
                <line class="chart-grid" x1={PAD_LEFT} x2={WIDTH} y1={scaleY(t)} y2={scaleY(t)} />
                <text class="chart-tick" x={PAD_LEFT - 4} y={scaleY(t)} dy="0.32em" text-anchor="end">
                  {t}%
                </text>
              </g>
            )}
          </For>
          <Show when={wastePctWeek() > 0}>
            <line
              class="chart-line chart-series-3"
              stroke-dasharray="4 3"
              x1={PAD_LEFT}
              x2={WIDTH}
              y1={scaleY(wastePctWeek())}
              y2={scaleY(wastePctWeek())}
            >
              <title>{`waste (withdrawn tickets): ${wastePctWeek().toFixed(2)}% week`}</title>
            </line>
          </Show>
          <path class="chart-line chart-series-1" d={medianPath()} />
          <For each={points()}>
            {(p, i) => (
              <circle class="chart-dot" cx={scaleX(i())} cy={scaleY(p.pct_week)} r={3}>
                <title>{pointTitle(p)}</title>
              </circle>
            )}
          </For>
        </svg>
        <div class="mt-1 flex items-center gap-3 text-[0.8em] text-muted">
          <span class="chart-legend">
            <span class="chart-legend-swatch chart-dot" /> ticket
          </span>
          <span class="chart-legend">
            <span class="chart-legend-swatch chart-series-1" /> rolling median ({MEDIAN_WINDOW})
          </span>
          <Show when={wastePctWeek() > 0}>
            <span class="chart-legend">
              <span class="chart-legend-swatch chart-series-3" /> waste: {wastePctWeek().toFixed(2)}% week
            </span>
          </Show>
        </div>
      </Show>
    </div>
  );
});
