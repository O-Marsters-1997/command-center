import { expect, test } from "bun:test";
import {
  ago,
  elapsed,
  failureText,
  maxLiveLines,
  modifierLabel,
  nextTab,
  railClosedCookie,
  scopeOf,
  shouldHop,
  whyOf,
} from "./glue";

test("a disabled verb's reason is its data-why", () => {
  const seen: string[] = [];
  const target = {
    closest(sel: string) {
      seen.push(sel);
      return { dataset: { why: "PR merged" } };
    },
  };
  expect(whyOf(target)).toBe("PR merged");
  expect(seen).toEqual(['[aria-disabled="true"][data-why]']);
  expect(whyOf({ closest: () => null })).toBeUndefined();
});

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

test("railClosedCookie adds a closed key and drops a reopened one", () => {
  const base = "a=b; rail-closed=x%7Cy";
  expect(railClosedCookie(base, "z", false)).toStartWith("rail-closed=x%7Cy%7Cz; path=/");
  expect(railClosedCookie(base, "x", true)).toStartWith("rail-closed=y; path=/");
  expect(railClosedCookie(base, "y", false)).toStartWith("rail-closed=x%7Cy;");
  expect(railClosedCookie("", "k", false)).toStartWith("rail-closed=k;");
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

test("nextTab wraps with the arrow keys and ignores other keys", () => {
  expect(nextTab(0, 2, "ArrowRight")).toBe(1);
  expect(nextTab(1, 2, "ArrowRight")).toBe(0);
  expect(nextTab(0, 2, "ArrowLeft")).toBe(1);
  expect(nextTab(1, 2, "Home")).toBe(0);
  expect(nextTab(0, 2, "End")).toBe(1);
  expect(nextTab(0, 2, "a")).toBeNull();
});
