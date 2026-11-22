export type ViewedRecord = { tip: string; files: Record<string, string> };

export function viewedKey(ticketURL: string): string {
  return `cc:viewed:${ticketURL}`;
}

export function carry(
  prev: ViewedRecord | null,
  tip: string,
  hashes: Readonly<Record<string, string>>,
): ViewedRecord {
  const files: Record<string, string> = {};
  for (const [path, hash] of Object.entries(prev?.files ?? {})) {
    if (hashes[path] === hash) files[path] = hash;
  }
  return { tip, files };
}

export function parseRecord(raw: string | null): ViewedRecord | null {
  try {
    const parsed: unknown = JSON.parse(raw ?? "null");
    if (typeof parsed !== "object" || parsed === null) return null;
    const { tip, files } = parsed as Record<string, unknown>;
    if (typeof tip !== "string" || typeof files !== "object" || files === null || Array.isArray(files)) return null;
    const kept = Object.entries(files).filter((e): e is [string, string] => typeof e[1] === "string");
    return { tip, files: Object.fromEntries(kept) };
  } catch {
    return null;
  }
}

export function viewedLabel(done: number, total: number): string {
  return `${done} of ${total} viewed`;
}

export function viewedCount(done: number, total: number): string {
  return `${done}/${total}`;
}

export function viewedPercent(done: number, total: number): string {
  return total === 0 ? "0%" : `${Math.round((done / total) * 100)}%`;
}

function hashesOf(root: HTMLElement): Record<string, string> {
  const hashes: Record<string, string> = {};
  for (const f of root.querySelectorAll<HTMLElement>("details[data-file]")) {
    hashes[f.dataset.file ?? ""] = f.dataset.diffHash ?? "";
  }
  return hashes;
}

function loadRecord(root: HTMLElement): ViewedRecord {
  const key = viewedKey(root.dataset.ticket ?? "");
  let prev: ViewedRecord | null = null;
  try {
    prev = parseRecord(localStorage.getItem(key));
  } catch {
    prev = null;
  }
  return carry(prev, root.dataset.tip ?? "", hashesOf(root));
}

function saveRecord(root: HTMLElement, record: ViewedRecord): void {
  try {
    localStorage.setItem(viewedKey(root.dataset.ticket ?? ""), JSON.stringify(record));
  } catch {
    return;
  }
}

function render(root: HTMLElement, record: ViewedRecord): void {
  let done = 0;
  for (const file of root.querySelectorAll<HTMLElement>("[data-file]")) {
    const on = Object.hasOwn(record.files, file.dataset.file ?? "");
    const box = file.querySelector<HTMLInputElement>("input[data-viewed]");
    if (box) box.checked = on;
    file.classList.toggle("is-viewed", on);
    if (file instanceof HTMLDetailsElement) {
      file.open = !on;
      if (on) done++;
    }
  }
  for (const dir of root.querySelectorAll<HTMLElement>("[data-dir]")) {
    const count = dir.querySelector<HTMLElement>("[data-dir-count]");
    const checked = dir.querySelectorAll("div[data-file] input[data-viewed]:checked").length;
    if (count) count.textContent = viewedCount(checked, Number(count.dataset.total));
  }
  const progress = root.querySelector("[data-viewed-progress]");
  const total = Number(root.dataset.total);
  if (progress) progress.textContent = viewedLabel(done, total);
  const meter = root.querySelector<HTMLElement>("[data-viewed-meter]");
  if (meter) meter.style.width = viewedPercent(done, total);
}

export function applyViewed(root: HTMLElement): void {
  const record = loadRecord(root);
  saveRecord(root, record);
  render(root, record);
}

export function toggleViewed(root: HTMLElement, path: string, on: boolean): void {
  const record = loadRecord(root);
  if (on) record.files[path] = hashesOf(root)[path] ?? "";
  else delete record.files[path];
  saveRecord(root, record);
  render(root, record);
}
