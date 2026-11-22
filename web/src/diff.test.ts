import { describe, expect, test } from "bun:test";
import { diffKeys, langOf, tokenize, treeMatches, wordDiff, type Lang } from "./diff";

const join = (s: string, l: Lang) =>
  tokenize(s, l)
    .map((t) => t.text)
    .join("");

describe("langOf", () => {
  test.each([
    ["a/b.go", "go"],
    ["x.tmpl", "go"],
    ["x.TSX", "ts"],
    ["m.sql", "sql"],
    ["Cargo.toml", "sh"],
    ["a.yaml", "sh"],
    ["README", ""],
    ["a.dir/file", ""],
    ["x.css", ""],
  ] as const)("%s", (path, want) => expect(langOf(path)).toBe(want));
});

describe("tokenize", () => {
  const cases: [Lang, string, string, string][] = [
    ["go", `func f() { return "a" + 12 } // done`, "//", "func"],
    ["ts", `const x = 'a'; // c`, "//", "const"],
    ["sql", `SELECT 1 -- c`, "--", "SELECT"],
    ["sh", `if true; then echo "x" # c`, "#", "if"],
    ["", `anything // here`, "", ""],
  ];
  test.each(cases)("%s round-trips and finds comments and keywords", (lang, line, marker, kw) => {
    expect(join(line, lang)).toBe(line);
    const toks = tokenize(line, lang);
    const com = toks.find((t) => t.kind === "com");
    if (marker) expect(com?.text.startsWith(marker)).toBe(true);
    else expect(com).toBeUndefined();
    if (kw) expect(toks.find((t) => t.kind === "kw")?.text).toBe(kw);
  });

  test("a comment marker of another language is plain text", () => {
    expect(tokenize("x -- y", "go").some((t) => t.kind === "com")).toBe(false);
    expect(tokenize("x // y", "sql").some((t) => t.kind === "com")).toBe(false);
  });

  test("sql keywords are case-insensitive, go keywords are not", () => {
    expect(tokenize("select", "sql")[0].kind).toBe("kw");
    expect(tokenize("Func", "go")[0].kind).toBe("text");
  });

  test("strings, numbers and template actions", () => {
    expect(tokenize(`"a // b"`, "go")).toEqual([{ kind: "str", text: `"a // b"` }]);
    expect(tokenize("42", "ts")).toEqual([{ kind: "num", text: "42" }]);
    expect(tokenize("{{.X}}", "go")[0].kind).toBe("kw");
    expect(join("{{.X}} y", "go")).toBe("{{.X}} y");
  });

  test("empty input", () => {
    expect(tokenize("", "go")).toEqual([]);
  });
});

describe("wordDiff", () => {
  const cases: [string, string][] = [
    ["same", "same"],
    ["ab", "abXY"],
    ["abXY", "ab"],
    ["aXb", "aYYb"],
    ["Xab", "Yab"],
    ["abX", "abY"],
    ["", "new"],
    ["aaa", "aa"],
  ];
  test.each(cases)("%j -> %j rejoins", (d, a) => {
    const r = wordDiff(d, a);
    for (const [s, x] of [[r.deleted, d], [r.added, a]] as const)
      expect(s.before + s.changed + s.after).toBe(x);
  });

  test("identical lines have no change", () => {
    const r = wordDiff("x", "x");
    expect(r.deleted.changed).toBe("");
    expect(r.added.changed).toBe("");
  });

  test("pure insertion", () => {
    const r = wordDiff("ab", "aXb");
    expect(r.deleted.changed).toBe("");
    expect(r.added.changed).toBe("X");
  });

  test("middle, start and end changes", () => {
    expect(wordDiff("aXb", "aYb").added).toEqual({ before: "a", changed: "Y", after: "b" });
    expect(wordDiff("Xb", "Yb").added).toEqual({ before: "", changed: "Y", after: "b" });
    expect(wordDiff("aX", "aY").added).toEqual({ before: "a", changed: "Y", after: "" });
  });
});

describe("treeMatches", () => {
  const paths = ["internal/web/a.go", "internal/store/b.go", "web/src/Diff.ts"];

  test("keeps ancestors of a match", () => {
    expect(treeMatches(paths, "a.go")).toEqual(
      new Set(["internal/web/a.go", "internal/web", "internal"]),
    );
  });
  test("is case-insensitive", () => {
    expect(treeMatches(paths, "DIFF")).toEqual(new Set(["web/src/Diff.ts", "web/src", "web"]));
  });
  test("empty query returns every path", () => {
    for (const p of paths) expect(treeMatches(paths, "").has(p)).toBe(true);
  });
  test("no match is empty", () => {
    expect(treeMatches(paths, "zzz").size).toBe(0);
  });
});

describe("diffKeys", () => {
  test("maps the documented keys", () => {
    expect(diffKeys.j).toBe("next-line");
    expect(diffKeys["{"]).toBe("first-file");
    expect(diffKeys["}"]).toBe("last-file");
    expect(diffKeys.v).toBe("toggle-viewed");
    expect(diffKeys.V).toBe("viewed-and-next");
    expect(diffKeys["?"]).toBe("help");
  });
});
