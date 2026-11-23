import { areaY, defineChart, dot, lineY, ruleY } from "@tanstack/charts";
import { scaleLinear } from "@tanstack/charts/scales/linear";
import { scaleUtc } from "@tanstack/charts/scales/time";
import { tooltip } from "@tanstack/charts/tooltip";

export type ChartWire = {
  unit: string;
  view: "ticket" | "week";
  dots: { ticket: string; title: string; href: string; merged_at: string; value: number; rolling: number }[];
  buckets: { start: string; median: number; n: number }[];
  range_median: number;
};

export type Dot = { ticket: string; title: string; href: string; merged: Date; value: number; rolling: number };
export type Bucket = { week: Date; value: number; n: number };

export const AREA_ID = "insights-area";

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

export function parseWire(raw: unknown): ChartWire {
  if (!isRecord(raw) || !Array.isArray(raw.dots) || !Array.isArray(raw.buckets)) {
    throw new Error("chart response is malformed");
  }
  if ((raw.view !== "ticket" && raw.view !== "week") || typeof raw.unit !== "string" || typeof raw.range_median !== "number") {
    throw new Error("chart response is malformed");
  }
  return raw as ChartWire;
}

export const dotsOf = (wire: ChartWire): Dot[] =>
  wire.dots.map((d) => ({ ...d, merged: new Date(d.merged_at) }));

export const bucketsOf = (wire: ChartWire): Bucket[] =>
  wire.buckets.map((b) => ({ week: new Date(b.start), value: b.median, n: b.n }));

export function formatValue(unit: string, v: number): string {
  switch (unit) {
    case "tokens":
      return v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : v >= 1e3 ? `${(v / 1e3).toFixed(1)}k` : v.toFixed(0);
    case "pct":
      return `${v.toFixed(1)}%`;
    case "seconds": {
      const s = Math.round(v);
      if (s >= 3600) return `${Math.floor(s / 3600)}h ${String(Math.floor((s % 3600) / 60)).padStart(2, "0")}m`;
      return s >= 60 ? `${Math.floor(s / 60)}m` : `${s}s`;
    }
    default:
      return v.toFixed(0);
  }
}

export const formatDay = (d: Date): string =>
  d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });

export function gradientStops(color: string, top: number) {
  return [
    { offset: 0, color, opacity: 0 },
    { offset: 1, color, opacity: top },
  ];
}

const axisText = { tickLabels: { fontSize: 11 } };

function scales(unit: string) {
  return {
    x: { scale: scaleUtc, axis: { ...axisText, ticks: { count: 6, format: (v: Date) => formatDay(v) } } },
    y: {
      scale: scaleLinear,
      nice: true,
      grid: true,
      axis: { ...axisText, ticks: { count: 4, format: (v: number) => formatValue(unit, v) } },
    },
  };
}

const token = (name: string) => `var(--color-${name})`;

export function ticketChart(wire: ChartWire) {
  const dots = dotsOf(wire);
  return defineChart({
    marks: [
      lineY(dots, {
        x: "merged",
        y: "rolling",
        stroke: token("accent"),
        strokeWidth: 2,
        strokeDasharray: "5 4",
      }),
      dot(dots, { x: "merged", y: "value", r: 4.5, fill: token("accent"), fillOpacity: 0.8 }),
    ],
    scales: scales(wire.unit),
    focus: "nearest",
    maxFocusDistance: 28,
    tooltip: {
      use: tooltip,
      sticky: false,
      format: (p) => {
        const d = p.datum as Dot;
        return `${d.ticket} ${d.title}\nMerged ${formatDay(d.merged)}\n${formatValue(wire.unit, d.value)}${d.href ? "\nClick to open the session" : ""}`;
      },
    },
  });
}

export function weekChart(wire: ChartWire, color: string) {
  const rows = bucketsOf(wire);
  return defineChart({
    marks: [
      areaY(rows, { x: "week", y1: 0, y2: "value", fill: `url(#${AREA_ID})` }),
      ruleY([wire.range_median], { stroke: token("meta"), strokeDasharray: "4 4" }),
      lineY(rows, { x: "week", y: "value", stroke: token("accent"), strokeWidth: 2.25 }),
    ],
    gradients: [{ id: AREA_ID, x1: 0, y1: 1, x2: 0, y2: 0, stops: gradientStops(color, 0.3) }],
    scales: scales(wire.unit),
    focus: "nearest-x",
    tooltip: {
      use: tooltip,
      sticky: false,
      format: (p) => {
        const d = p.datum as Bucket;
        return `Week of ${formatDay(d.week)}\nMedian ${formatValue(wire.unit, d.value)} · ${d.n} tickets`;
      },
    },
  });
}
