import { expect, test } from "bun:test";
import { clampWidth, keyedWidth, sidebar, sidebarCookies } from "./sidebar";

test("keyedWidth steps the width with the arrow keys and ignores other keys", () => {
  expect(keyedWidth(300, "ArrowLeft")).toBe(284);
  expect(keyedWidth(300, "ArrowRight")).toBe(316);
  expect(keyedWidth(sidebar.max, "ArrowRight")).toBe(sidebar.max);
  expect(keyedWidth(sidebar.min, "ArrowLeft")).toBe(sidebar.min);
  expect(keyedWidth(300, "Enter")).toBeNull();
});

test("clampWidth holds the width between the bounds and rounds it", () => {
  expect(clampWidth(100)).toBe(sidebar.min);
  expect(clampWidth(1000)).toBe(sidebar.max);
  expect(clampWidth(312.6)).toBe(313);
  expect(clampWidth(Number.NaN)).toBe(sidebar.initial);
});

test("sidebarCookies writes the clamped width and the collapsed flag", () => {
  expect(sidebarCookies({ width: 9000, collapsed: true })).toEqual([
    "rail-width=440; path=/; max-age=31536000; samesite=lax",
    "rail-collapsed=1; path=/; max-age=31536000; samesite=lax",
  ]);
  expect(sidebarCookies({ width: 260, collapsed: false })).toEqual([
    "rail-width=260; path=/; max-age=31536000; samesite=lax",
    "rail-collapsed=0; path=/; max-age=31536000; samesite=lax",
  ]);
});
