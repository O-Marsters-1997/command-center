import { expect, test } from "bun:test";
import {
  ago,
  elapsed,
  failureText,
  isCurrentPath,
  maxLiveLines,
  modifierLabel,
  railOpenCookie,
  scopeOf,
  shouldHop,
} from "./glue";

test("a failed response names its status", () => {
  expect(failureText("responseError", 500)).toContain("500");
});

test("a request that never arrived says so", () => {
  expect(failureText("sendError")).toBe("Could not reach the server.");
});

test("ago counts seconds, minutes and hours", () => {
  expect(ago(42_000)).toBe("42s ago");
  expect(ago(-5000)).toBe("0s ago");
  expect(ago(185_000)).toBe("3m5s ago");
  expect(ago(180_000)).toBe("3m ago");
  expect(ago(3_720_000)).toBe("1h2m ago");
  expect(ago(3_600_000)).toBe("1h ago");
});

test("elapsed is the rail timer", () => {
  expect(elapsed(185_000)).toBe("3m5s");
  expect(elapsed(3_720_000)).toBe("1h2m");
});

test("railOpenCookie adds and removes a key", () => {
  const base = "a=b; rail-open=x%7Cy";
  expect(railOpenCookie(base, "z", true)).toStartWith("rail-open=x%7Cy%7Cz; path=/");
  expect(railOpenCookie(base, "x", false)).toStartWith("rail-open=y; path=/");
  expect(railOpenCookie(base, "y", true)).toStartWith("rail-open=x%7Cy;");
  expect(railOpenCookie("", "k", true)).toStartWith("rail-open=k;");
});

test("isCurrentPath matches home exactly and others by prefix", () => {
  expect(isCurrentPath("/", "/")).toBe(true);
  expect(isCurrentPath("/", "/tickets")).toBe(false);
  expect(isCurrentPath("/tickets", "/tickets/x")).toBe(true);
  expect(isCurrentPath("/insights", "/tickets")).toBe(false);
});

test("scopeOf prefers project over repo", () => {
  const o = "http://x";
  expect(scopeOf("/tickets?project=p&repo=r", o)).toBe("project=p");
  expect(scopeOf("/tickets?repo=r", o)).toBe("repo=r");
  expect(scopeOf("/tickets", o)).toBe("");
});

test("modifierLabel rewrites the command glyph off Apple platforms", () => {
  expect(modifierLabel("⌘K", false)).toBe("Ctrl K");
  expect(modifierLabel("⌘K", true)).toBe("⌘K");
});

test("shouldHop needs a target and a wide viewport", () => {
  expect(shouldHop("/tickets", true)).toBe(true);
  expect(shouldHop("/tickets", false)).toBe(false);
  expect(shouldHop(undefined, true)).toBe(false);
  expect(shouldHop("", true)).toBe(false);
});

test("the live tail keeps a thousand lines", () => {
  expect(maxLiveLines).toBe(1000);
});
