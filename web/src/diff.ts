export type Lang = "go" | "ts" | "sql" | "sh" | "";
export type TokenKind = "kw" | "str" | "num" | "com" | "text";
export type Token = { kind: TokenKind; text: string };
export type Split = { before: string; changed: string; after: string };

export type DiffAction =
  | "next-line"
  | "prev-line"
  | "next-change"
  | "prev-change"
  | "next-file"
  | "prev-file"
  | "first-file"
  | "last-file"
  | "toggle-viewed"
  | "viewed-and-next"
  | "collapse"
  | "filter"
  | "help";

export const diffKeys: Readonly<Record<string, DiffAction>> = {
  j: "next-line",
  k: "prev-line",
  n: "next-change",
  p: "prev-change",
  "]": "next-file",
  "[": "prev-file",
  "}": "last-file",
  "{": "first-file",
  v: "toggle-viewed",
  V: "viewed-and-next",
  o: "collapse",
  "/": "filter",
  "?": "help",
};

export type CommentAction = "comment" | "comments";

export const commentKeys: Readonly<Record<string, CommentAction>> = {
  c: "comment",
  L: "comments",
};

export type CommentRef = { id: string; path: string; line: number; body: string };

export function countByFile(refs: readonly Pick<CommentRef, "path">[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const r of refs) counts.set(r.path, (counts.get(r.path) ?? 0) + 1);
  return counts;
}

const keywords: Record<Exclude<Lang, "">, Set<string>> = {
  go: new Set(
    "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false".split(
      " ",
    ),
  ),
  ts: new Set(
    "const let var function return if else for while import export from class extends new async await type interface null undefined true false this switch case break default throw try catch of in as".split(
      " ",
    ),
  ),
  sql: new Set(
    "select from where insert into values update set delete create table alter add column index not null default primary key references on conflict do and or begin end commit transaction drop".split(
      " ",
    ),
  ),
  sh: new Set(
    "if then else fi for do done case esac function return export local echo set".split(
      " ",
    ),
  ),
};

const byExtension: Record<string, Lang> = {
  go: "go",
  tmpl: "go",
  ts: "ts",
  tsx: "ts",
  js: "ts",
  mjs: "ts",
  sql: "sql",
  sh: "sh",
  toml: "sh",
  yml: "sh",
  yaml: "sh",
};

const commentMarker: Record<Exclude<Lang, "">, string> = {
  go: "//",
  ts: "//",
  sql: "--",
  sh: "#",
};

export function langOf(path: string): Lang {
  const name = path.slice(path.lastIndexOf("/") + 1);
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : (byExtension[name.slice(dot + 1).toLowerCase()] ?? "");
}

export function tokenize(text: string, lang: Lang): Token[] {
  if (!lang) return text ? [{ kind: "text", text }] : [];
  const re = new RegExp(
    `(${commentMarker[lang]}.*$)|("(?:[^"\\\\]|\\\\.)*"|'(?:[^'\\\\]|\\\\.)*'|\`[^\`]*\`)|(\\b\\d[\\w.]*\\b)|([A-Za-z_]\\w*)|(\\{\\{.*?\\}\\})`,
    "g",
  );
  const out: Token[] = [];
  const push = (kind: TokenKind, s: string) => {
    if (!s) return;
    const prev = out[out.length - 1];
    if (kind === "text" && prev?.kind === "text") prev.text += s;
    else out.push({ kind, text: s });
  };
  let last = 0;
  for (const m of text.matchAll(re)) {
    push("text", text.slice(last, m.index));
    const [t, com, str, num, word, tmpl] = m;
    if (com) push("com", t);
    else if (str) push("str", t);
    else if (num) push("num", t);
    else if (tmpl && lang === "go") push("kw", t);
    else if (word && keywords[lang].has(lang === "sql" ? t.toLowerCase() : t))
      push("kw", t);
    else push("text", t);
    last = m.index + t.length;
  }
  push("text", text.slice(last));
  return out;
}

export function wordDiff(
  deleted: string,
  added: string,
): { deleted: Split; added: Split } {
  let p = 0;
  while (p < deleted.length && p < added.length && deleted[p] === added[p]) p++;
  let s = 0;
  while (
    s < deleted.length - p &&
    s < added.length - p &&
    deleted[deleted.length - 1 - s] === added[added.length - 1 - s]
  )
    s++;
  const split = (x: string): Split => ({
    before: x.slice(0, p),
    changed: x.slice(p, x.length - s),
    after: x.slice(x.length - s),
  });
  return { deleted: split(deleted), added: split(added) };
}

export function treeMatches(
  paths: readonly string[],
  query: string,
): Set<string> {
  const q = query.toLowerCase();
  const out = new Set<string>();
  for (const path of paths) {
    if (!path.toLowerCase().includes(q)) continue;
    out.add(path);
    for (let i = path.indexOf("/"); i >= 0; i = path.indexOf("/", i + 1))
      out.add(path.slice(0, i));
  }
  return out;
}
