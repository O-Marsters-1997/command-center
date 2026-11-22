import { expect, test } from "bun:test";
import { carry, parseRecord, viewedCount, viewedKey, viewedLabel, viewedPercent } from "./viewed";

const prev = { tip: "t1", files: { a: "h1", b: "h2", c: "h3" } };

test("viewedKey is per ticket", () => {
  expect(viewedKey("https://x/1")).toBe("cc:viewed:https://x/1");
});

test("carry keeps every mark on the same tip", () => {
  expect(carry(prev, "t1", { a: "h1", b: "h2", c: "h3" })).toEqual(prev);
});

test("carry keeps only marks whose diff hash is unchanged on a new tip", () => {
  expect(carry(prev, "t2", { a: "h1", b: "new", c: "h3" })).toEqual({ tip: "t2", files: { a: "h1", c: "h3" } });
});

test("carry drops a file removed from the diff", () => {
  expect(carry(prev, "t2", { a: "h1", b: "h2" })).toEqual({ tip: "t2", files: { a: "h1", b: "h2" } });
});

test("carry starts empty without a previous record", () => {
  expect(carry(null, "t2", { a: "h1" })).toEqual({ tip: "t2", files: {} });
});

test("parseRecord rejects a corrupt record", () => {
  expect(parseRecord(JSON.stringify(prev))).toEqual(prev);
  expect(parseRecord(null)).toBeNull();
  expect(parseRecord("{")).toBeNull();
  expect(parseRecord('["a"]')).toBeNull();
  expect(parseRecord('{"tip":1,"files":{}}')).toBeNull();
  expect(parseRecord('{"tip":"t","files":{"a":1,"b":"h"}}')).toEqual({ tip: "t", files: { b: "h" } });
});

test("viewedCount, viewedPercent and viewedLabel", () => {
  expect(viewedCount(2, 5)).toBe("2/5");
  expect(viewedPercent(1, 4)).toBe("25%");
  expect(viewedPercent(0, 0)).toBe("0%");
  expect(viewedLabel(4, 4)).toBe("4 of 4 viewed");
});
