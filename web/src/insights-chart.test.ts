import { describe, expect, test } from "bun:test";
import {
  bucketsOf,
  dotsOf,
  formatValue,
  gradientStops,
  parseReworkWire,
  parseWire,
  reworkBars,
} from "./insights-chart.ts";

const wire = {
  unit: "tokens",
  view: "ticket",
  dots: [
    {
      ticket: "#7",
      title: "t",
      href: "/s/a/b/7?log=insights",
      merged_at: "2026-09-20T10:00:00Z",
      value: 1500,
      rolling: 1200,
    },
  ],
  buckets: [{ start: "2026-09-14T00:00:00Z", median: 900, n: 3 }],
  range_median: 1000,
};

describe("parseWire", () => {
  test("accepts a well-formed response", () => {
    expect(parseWire(wire).unit).toBe("tokens");
  });
  test("rejects a malformed response", () => {
    expect(() => parseWire({ unit: "tokens" })).toThrow();
    expect(() => parseWire({ ...wire, view: "month" })).toThrow();
    expect(() => parseWire(null)).toThrow();
  });
});

describe("series", () => {
  test("dots carry parsed dates", () => {
    expect(dotsOf(parseWire(wire))[0]?.merged.toISOString()).toBe("2026-09-20T10:00:00.000Z");
  });
  test("buckets rename median to value", () => {
    expect(bucketsOf(parseWire(wire))[0]?.value).toBe(900);
  });
});

describe("formatValue", () => {
  test("formats each unit", () => {
    expect(formatValue("tokens", 1500)).toBe("1.5k");
    expect(formatValue("pct", 4.25)).toBe("4.3%");
    expect(formatValue("seconds", 7200)).toBe("2h 00m");
    expect(formatValue("seconds", 90)).toBe("1m");
    expect(formatValue("calls", 7.4)).toBe("7");
  });
});

const reworkWire = {
  metric: "green",
  weeks: [{ start: "2026-09-14T00:00:00Z", hit: 3, miss: 1 }],
  histogram: [{ start: "2026-09-14T00:00:00Z", median: 25, n: 4 }],
};

describe("rework", () => {
  test("rejects a malformed response", () => {
    expect(() => parseReworkWire({ ...reworkWire, metric: "vibes" })).toThrow();
    expect(() => parseReworkWire({ metric: "green" })).toThrow();
  });
  test("weekly bars total hits and misses", () => {
    const [bar] = reworkBars(parseReworkWire(reworkWire));
    expect([bar?.total, bar?.hit]).toEqual([4, 3]);
    expect(bar?.tip).toContain("3 of 4 tickets green on the first push");
  });
  test("the pushes metric draws the histogram in percent", () => {
    const [bar] = reworkBars(parseReworkWire({ ...reworkWire, metric: "pushes" }));
    expect([bar?.total, bar?.hit]).toEqual([25, 25]);
    expect(bar?.tip).toContain("25.0% needed three or more pushes");
  });
});

describe("gradientStops", () => {
  test("fades from transparent to the top opacity", () => {
    expect(gradientStops("red", 0.3)).toEqual([
      { offset: 0, color: "red", opacity: 0 },
      { offset: 1, color: "red", opacity: 0.3 },
    ]);
  });
});
