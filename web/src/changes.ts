import { diffKeys, langOf, tokenize, treeMatches, wordDiff, type DiffAction, type Lang, type TokenKind } from "./diff";

export type Segment = { kind: TokenKind; text: string; changed: boolean };

const wsKey = "cc:changes:ws";
const wrapKey = "cc:changes:wrap";

export function pairRuns(kinds: readonly string[]): [number, number][] {
  const pairs: [number, number][] = [];
  let i = 0;
  while (i < kinds.length) {
    const del = i;
    while (kinds[i] === "deleted") i++;
    const add = i;
    while (kinds[i] === "added") i++;
    const n = Math.min(add - del, i - add);
    for (let k = 0; k < n; k++) pairs.push([del + k, add + k]);
    if (i === del) i++;
  }
  return pairs;
}

export function segments(text: string, lang: Lang, range?: { start: number; end: number }): Segment[] {
  const out: Segment[] = [];
  let at = 0;
  for (const t of tokenize(text, lang)) {
    const end = at + t.text.length;
    const cuts = range ? [range.start, range.end].filter((c) => c > at && c < end) : [];
    let from = at;
    for (const to of [...cuts, end]) {
      const changed = range !== undefined && from >= range.start && to <= range.end;
      out.push({ kind: t.kind, text: t.text.slice(from - at, to - at), changed });
      from = to;
    }
    at = end;
  }
  return out;
}

export function nextChange(changed: readonly boolean[], cur: number, step: 1 | -1): number {
  let i = cur;
  if (i >= 0) while (changed[i]) i += step;
  else i = step === 1 ? 0 : changed.length - 1;
  while (i >= 0 && i < changed.length && !changed[i]) i += step;
  if (i < 0 || i >= changed.length) return -1;
  if (step === -1) while (i > 0 && changed[i - 1]) i--;
  return i;
}

export function nextUnviewed(viewed: readonly boolean[], cur: number): number {
  for (let k = 1; k < viewed.length; k++) {
    const i = (cur + k) % viewed.length;
    if (!viewed[i]) return i;
  }
  return -1;
}

function stripSpace(s: string): string {
  return s.replace(/\s+/g, "");
}

export function actionFor(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "isComposing">): DiffAction | undefined {
  if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return undefined;
  return diffKeys[e.key];
}

function stored(key: string): boolean {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

function store(key: string, on: boolean): void {
  try {
    localStorage.setItem(key, on ? "1" : "0");
  } catch {
    return;
  }
}

const lineSel = "details.diff-file[open] tr.diff-line:not([hidden])";

function fileOf(el: Element | null): HTMLDetailsElement | null {
  return el?.closest("details.diff-file") ?? null;
}

function lineText(tr: HTMLElement): string {
  return tr.querySelector(".diff-text pre")?.textContent?.slice(1) ?? "";
}

function paint(tr: HTMLElement, lang: Lang, range?: { start: number; end: number }): void {
  const pre = tr.querySelector(".diff-text pre");
  if (!pre) return;
  const text = lineText(tr);
  const sign = pre.firstElementChild;
  pre.replaceChildren(...(sign ? [sign] : []));
  for (const s of segments(text, lang, range)) {
    const span = document.createElement(s.changed ? "mark" : "span");
    if (s.kind !== "text") span.className = `tk-${s.kind}`;
    if (s.changed) span.className += " diff-word";
    span.textContent = s.text;
    pre.append(span);
  }
}

function highlight(root: HTMLElement): void {
  for (const file of root.querySelectorAll<HTMLDetailsElement>("details.diff-file:not([data-hl])")) {
    file.dataset.hl = "";
    const lang = langOf(file.dataset.file ?? "");
    for (const body of file.querySelectorAll("tbody")) {
      const rows = [...body.querySelectorAll<HTMLElement>("tr.diff-line")];
      const done = new Set<number>();
      for (const [d, a] of pairRuns(rows.map((r) => r.dataset.kind ?? ""))) {
        const del = rows[d];
        const add = rows[a];
        if (!del || !add) continue;
        const split = wordDiff(lineText(del), lineText(add));
        const range = (s: typeof split.deleted) => ({ start: s.before.length, end: s.before.length + s.changed.length });
        paint(del, lang, range(split.deleted));
        paint(add, lang, range(split.added));
        done.add(d).add(a);
      }
      rows.forEach((r, i) => done.has(i) || paint(r, lang));
    }
  }
}

function applyWhitespace(root: HTMLElement, on: boolean): void {
  for (const body of root.querySelectorAll("tbody")) {
    const rows = [...body.querySelectorAll<HTMLElement>("tr.diff-line")];
    for (const r of rows) {
      r.hidden = false;
      r.classList.remove("diff-ws");
    }
    if (!on) continue;
    for (const [d, a] of pairRuns(rows.map((r) => r.dataset.kind ?? ""))) {
      const del = rows[d];
      const add = rows[a];
      if (!del || !add || stripSpace(lineText(del)) !== stripSpace(lineText(add))) continue;
      del.hidden = true;
      add.classList.add("diff-ws");
    }
  }
}

function lines(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>(lineSel)];
}

function focused(): HTMLElement | null {
  return document.querySelector<HTMLElement>("tr.diff-line.is-focus");
}

function setFocus(tr: HTMLElement | null): void {
  focused()?.classList.remove("is-focus");
  if (!tr) return;
  tr.classList.add("is-focus");
  tr.scrollIntoView({ block: "center" });
  markActive();
}

function files(): HTMLDetailsElement[] {
  return [...document.querySelectorAll<HTMLDetailsElement>("details.diff-file")];
}

function fileInView(): HTMLDetailsElement | null {
  const all = files();
  const above = all.filter((f) => f.getBoundingClientRect().top < 160);
  return above[above.length - 1] ?? all[0] ?? null;
}

function currentFile(): HTMLDetailsElement | null {
  return fileOf(focused()) ?? fileInView();
}

function markActive(byScroll = false): void {
  const path = (byScroll ? fileInView() : currentFile())?.dataset.file;
  for (const row of document.querySelectorAll<HTMLElement>(".tree-file")) {
    const on = row.dataset.file === path;
    row.classList.toggle("is-active", on);
    if (on) row.scrollIntoView({ block: "nearest" });
  }
}

function goFile(file: HTMLDetailsElement | undefined): void {
  if (!file) return;
  file.scrollIntoView({ block: "start" });
  setFocus(file.open ? (file.querySelector<HTMLElement>("tr.diff-line:not([hidden])") ?? null) : null);
  markActive();
}

function viewedBox(file: HTMLDetailsElement): HTMLInputElement | null {
  return file.querySelector<HTMLInputElement>("summary input[data-viewed]");
}

function act(action: DiffAction): void {
  const all = files();
  const here = currentFile();
  const at = here ? all.indexOf(here) : -1;
  switch (action) {
    case "next-line":
    case "prev-line": {
      const rows = lines();
      const i = rows.indexOf(focused() as HTMLElement);
      const step = action === "next-line" ? 1 : -1;
      setFocus(rows[i < 0 ? (step === 1 ? 0 : rows.length - 1) : Math.min(rows.length - 1, Math.max(0, i + step))] ?? null);
      return;
    }
    case "next-change":
    case "prev-change": {
      const rows = lines();
      const i = nextChange(
        rows.map((r) => r.dataset.kind !== "context" && !r.classList.contains("diff-ws")),
        rows.indexOf(focused() as HTMLElement),
        action === "next-change" ? 1 : -1,
      );
      if (i >= 0) setFocus(rows[i] ?? null);
      return;
    }
    case "next-file":
      return goFile(all[Math.min(all.length - 1, at + 1)]);
    case "prev-file":
      return goFile(all[Math.max(0, at - 1)]);
    case "first-file":
      return goFile(all[0]);
    case "last-file":
      return goFile(all[all.length - 1]);
    case "toggle-viewed":
      return viewedBox(here as HTMLDetailsElement)?.click();
    case "viewed-and-next": {
      const box = here && viewedBox(here);
      if (box && !box.checked) box.click();
      goFile(all[nextUnviewed(all.map((f) => viewedBox(f)?.checked ?? true), at)]);
      return;
    }
    case "collapse":
      if (here) {
        here.open = !here.open;
        if (!here.open) setFocus(null);
      }
      return;
    case "filter":
      document.querySelector<HTMLInputElement>("#changes-filter")?.focus();
      return;
    case "help":
      (document.getElementById("key-sheet") as HTMLDialogElement | null)?.showModal();
  }
}

function filterTree(query: string): void {
  const rows = [...document.querySelectorAll<HTMLElement>(".tree-file")];
  const keep = treeMatches(rows.map((r) => r.dataset.file ?? ""), query);
  for (const r of rows) r.hidden = !keep.has(r.dataset.file ?? "");
  const dirs = [...document.querySelectorAll<HTMLElement>("details[data-dir]")].reverse();
  for (const d of dirs) d.hidden = d.querySelector(".tree-file:not([hidden])") === null;
}

function enhance(): void {
  const root = document.querySelector<HTMLElement>("[data-changes]");
  if (!root) return;
  highlight(root);
  if (root.dataset.ready === undefined) {
    root.dataset.ready = "";
    const ws = root.querySelector<HTMLInputElement>("#changes-ws");
    const wrap = root.querySelector<HTMLInputElement>("#changes-wrap");
    if (ws) {
      ws.checked = stored(wsKey);
      applyWhitespace(root, ws.checked);
    }
    if (wrap) {
      wrap.checked = stored(wrapKey);
      root.classList.toggle("is-wrap", wrap.checked);
    }
  }
  markActive();
}

function isField(el: Element | null): boolean {
  return el instanceof HTMLElement && (el.matches("input, textarea, select") || el.isContentEditable);
}

function init(): void {
  document.addEventListener(
    "keydown",
    (e) => {
      if (!document.querySelector("[data-changes]") || document.querySelector("dialog[open]")) return;
      if (isField(document.activeElement)) return;
      if (e.key === "Escape") setFocus(null);
      const action = actionFor(e);
      if (!action) return;
      e.preventDefault();
      e.stopPropagation();
      act(action);
    },
    { capture: true },
  );
  document.addEventListener("input", (e) => {
    if (e.target instanceof HTMLInputElement && e.target.id === "changes-filter") filterTree(e.target.value.trim());
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || !(e.target instanceof HTMLInputElement) || e.target.id !== "changes-filter") return;
    e.target.value = "";
    filterTree("");
    e.target.blur();
  });
  document.addEventListener("change", (e) => {
    const box = e.target;
    const root = document.querySelector<HTMLElement>("[data-changes]");
    if (!(box instanceof HTMLInputElement) || !root) return;
    if (box.id === "changes-ws") {
      store(wsKey, box.checked);
      applyWhitespace(root, box.checked);
    } else if (box.id === "changes-wrap") {
      store(wrapKey, box.checked);
      root.classList.toggle("is-wrap", box.checked);
    }
  });
  let frame = 0;
  document.addEventListener(
    "scroll",
    () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => markActive(true));
    },
    { passive: true },
  );
  document.addEventListener("htmx:afterSettle", enhance);
  document.addEventListener("htmx:historyRestore", enhance);
  enhance();
}

if (typeof document !== "undefined") init();
