import { describe, expect, test } from "bun:test";
import {
  bucketsOf,
  dotsOf,
  formatValue,
  gradientStops,
  parseTicketWire,
  parseWire,
  stepsOf,
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
    expect(dotsOf(parseWire(wire))[0]?.merged.toISOString()).toBe(
      "2026-09-20T10:00:00.000Z",
    );
  });
  test("buckets rename median to value", () => {
    expect(bucketsOf(parseWire(wire))[0]?.value).toBe(900);
  });
});

describe("parseTicketWire", () => {
  test("accepts steps and a null list", () => {
    expect(
      stepsOf(
        parseTicketWire({
          steps: [{ at: "2026-09-20T10:00:00Z", tokens: 40 }],
        }),
      )[0]?.at.toISOString(),
    ).toBe("2026-09-20T10:00:00.000Z");
    expect(stepsOf(parseTicketWire({ steps: null }))).toEqual([]);
  });
  test("rejects a malformed response", () => {
    expect(() => parseTicketWire({ steps: "x" })).toThrow();
    expect(() => parseTicketWire(null)).toThrow();
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

describe("gradientStops", () => {
  test("fades from transparent to the top opacity", () => {
    expect(gradientStops("red", 0.3)).toEqual([
      { offset: 0, color: "red", opacity: 0 },
      { offset: 1, color: "red", opacity: 0.3 },
    ]);
  });
});
