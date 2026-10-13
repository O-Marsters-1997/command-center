import { expect, test } from "bun:test";
import { failureText } from "./glue";

test("a failed response names its status", () => {
  expect(failureText("responseError", 500)).toContain("500");
});

test("a request that never arrived says so", () => {
  expect(failureText("sendError")).toBe("Could not reach the server.");
});
