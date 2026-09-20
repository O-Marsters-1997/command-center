import { For, Show } from "solid-js";

export interface Point {
  x: number;
  y: number;
}

export interface Bar {
  label: string;
  value: number;
}

export interface Ticks {
  values: number[];
  step: number;
}

export interface ChartDims {
  width?: number;
  height?: number;
}

// (max / step) * step can leave floating-point dust, e.g. 4.999999999999999.
function round(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

// niceTicks picks a step from the 1/2/5 ladder, times a power of ten.
export function niceTicks(max: number, targetCount = 4): Ticks {
  if (!(max > 0)) return { values: [0], step: 1 };
  const roughStep = max / targetCount;
  const magnitude = 10 ** Math.floor(Math.log10(roughStep));
  const residual = roughStep / magnitude;
  const rung = residual <= 1 ? 1 : residual <= 2 ? 2 : residual <= 5 ? 5 : 10;
  const step = rung * magnitude;
  const top = Math.ceil(max / step) * step;
  const values: number[] = [];
  for (let v = 0; v <= top + step / 2; v += step) values.push(round(v));
  return { values, step };
}

export function sparkPoints(values: number[], width: number, height: number): Point[] {
  if (values.length === 0) return [];
  if (values.length === 1) return [{ x: width / 2, y: height / 2 }];
  const min = Math.min(...values);
  const range = Math.max(...values) - min || 1;
  const step = width / (values.length - 1);
  return values.map((v, i) => ({ x: i * step, y: height - ((v - min) / range) * height }));
}

function linePath(points: Point[]): string {
  return points.map((p, i) => `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`).join(" ");
}

function areaPath(points: Point[], height: number): string {
  const first = points[0];
  const last = points[points.length - 1];
  return `${linePath(points)} L ${last?.x} ${height} L ${first?.x} ${height} Z`;
}

export function histogramBars(values: number[], binCount = 8): Bar[] {
  if (values.length === 0) return [];
  const min = Math.min(...values);
  const max = Math.max(...values);
  if (min === max) return [{ label: String(min), value: values.length }];
  const width = (max - min) / binCount;
  const counts = new Array(binCount).fill(0);
  for (const v of values) counts[Math.min(binCount - 1, Math.floor((v - min) / width))]++;
  return counts.map((value, i) => ({ label: String(round(min + i * width)), value }));
}

export interface SparklineProps extends ChartDims {
  values: number[];
}

export function Sparkline(props: SparklineProps) {
  const width = () => props.width ?? 120;
  const height = () => props.height ?? 32;
  const points = () => sparkPoints(props.values, width(), height());

  return (
    <svg width={width()} height={height()} viewBox={`0 0 ${width()} ${height()}`} role="img">
      <title>{props.values.join(", ") || "no data"}</title>
      <Show when={points().length > 1}>
        <path class="spark-area" d={areaPath(points(), height())} />
        <path class="spark-line" d={linePath(points())} />
      </Show>
      <Show when={points().length === 1}>
        <circle class="spark-point" cx={points()[0]?.x} cy={points()[0]?.y} r={2} />
      </Show>
    </svg>
  );
}

const PAD_LEFT = 28;
const PAD_TOP = 8;
const PAD_BOTTOM = 16;

export interface BarChartProps extends ChartDims {
  bars: Bar[];
}

export function BarChart(props: BarChartProps) {
  const width = () => props.width ?? 240;
  const height = () => props.height ?? 120;
  const plotWidth = () => width() - PAD_LEFT;
  const plotHeight = () => height() - PAD_TOP - PAD_BOTTOM;
  const ticks = () => niceTicks(Math.max(0, ...props.bars.map((b) => b.value)));
  const top = () => ticks().values[ticks().values.length - 1] || 1;
  const scaleY = (v: number) => PAD_TOP + plotHeight() - (v / top()) * plotHeight();
  const barWidth = () => (props.bars.length === 0 ? 0 : plotWidth() / props.bars.length);

  return (
    <svg width={width()} height={height()} viewBox={`0 0 ${width()} ${height()}`} role="img">
      <title>{props.bars.map((b) => `${b.label}: ${b.value}`).join(", ") || "no data"}</title>
      <For each={ticks().values}>
        {(t) => (
          <g>
            <line class="chart-grid" x1={PAD_LEFT} x2={width()} y1={scaleY(t)} y2={scaleY(t)} />
            <text class="chart-tick" x={PAD_LEFT - 4} y={scaleY(t)} dy="0.32em" text-anchor="end">
              {t}
            </text>
          </g>
        )}
      </For>
      <For each={props.bars}>
        {(bar, i) => (
          <g>
            <rect
              class="chart-bar"
              x={PAD_LEFT + i() * barWidth() + 1}
              y={scaleY(bar.value)}
              width={Math.max(0, barWidth() - 2)}
              height={PAD_TOP + plotHeight() - scaleY(bar.value)}
            >
              <title>{`${bar.label}: ${bar.value}`}</title>
            </rect>
            <text
              class="chart-tick"
              x={PAD_LEFT + i() * barWidth() + barWidth() / 2}
              y={height()}
              dy="-0.2em"
              text-anchor="middle"
            >
              {bar.label}
            </text>
          </g>
        )}
      </For>
    </svg>
  );
}

export interface HistogramProps extends ChartDims {
  values: number[];
  bins?: number;
}

// Histogram is a binner in front of BarChart, not a second chart primitive.
export function Histogram(props: HistogramProps) {
  return <BarChart bars={histogramBars(props.values, props.bins ?? 8)} width={props.width} height={props.height} />;
}
