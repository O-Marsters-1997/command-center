import { describe, expect, test } from "bun:test";
import { cardTag, subline, waveSubtitle } from "./graph-card";

const card = {
  glyph: "ready" as const,
  label: "Ready to launch",
  detail: "",
  after: null,
  pr_number: 0,
  checks_passed: 0,
  checks_total: 0,
};

describe("subline", () => {
  test("a blocked ticket names what it waits on", () => {
    expect(subline({ ...card, glyph: "blocked", after: ["#4", "#5"] })).toBe("after #4, #5");
  });

  test("a ticket with a PR shows it with its checks", () => {
    expect(subline({ ...card, glyph: "checking", pr_number: 7, checks_passed: 2, checks_total: 3 })).toBe(
      "PR #7 · 2/3 checks",
    );
  });

  test("a PR with no checks omits them", () => {
    expect(subline({ ...card, pr_number: 7 })).toBe("PR #7");
  });

  test("a ticket with no PR shows its detail, else its label", () => {
    expect(subline({ ...card, detail: "Every required check passed" })).toBe("Every required check passed");
    expect(subline(card)).toBe("Ready to launch");
  });
});

describe("cardTag", () => {
  test("only Needs you states carry a label", () => {
    expect(cardTag({ glyph: "failed", label: "CI failed" })).toBe("CI failed");
    expect(cardTag({ glyph: "attention", label: "Review me" })).toBe("Review me");
    expect(cardTag({ glyph: "running", label: "Running" })).toBe("");
  });
});

describe("waveSubtitle", () => {
  test("counts what is in progress", () => {
    expect(waveSubtitle(0, [{ glyph: "running" }, { glyph: "checking" }, { glyph: "ready" }])).toBe("2 in progress");
  });

  test("a later wave of only blocked tickets waits on the one before", () => {
    expect(waveSubtitle(1, [{ glyph: "blocked" }])).toBe("unlocks as wave 1 merges");
    expect(waveSubtitle(0, [{ glyph: "blocked" }])).toBe("0 in progress");
  });
});
