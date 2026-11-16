import {
  launchHref,
  type PickedRow,
  rangeBetween,
  stepCursor,
  summarize,
  toggled,
  withAll,
  without,
} from "./select";

const listSelector = "#main #board [data-row]";

export interface Selection {
  hasList(): boolean;
  hasSelection(): boolean;
  moveCursor(step: 1 | -1): void;
  toggleCursor(): void;
  openCursor(): boolean;
  launch(): boolean;
  clear(): void;
}

function boxes(doc: Document): HTMLInputElement[] {
  return [...doc.querySelectorAll<HTMLInputElement>("#board input[data-select]")];
}

function rows(doc: Document): HTMLElement[] {
  return [...doc.querySelectorAll<HTMLElement>("#board [data-row]")];
}

export function initSelection(doc: Document, signal: AbortSignal): Selection {
  let selected: string[] = [];
  let anchor: string | null = null;
  let cursor: string | null = null;

  const rowUrl = (row: HTMLElement): string | null =>
    row.querySelector<HTMLInputElement>("input[data-select]")?.value ??
    row.querySelector("a")?.getAttribute("href") ??
    null;

  function render(): void {
    const present = boxes(doc);
    selected = selected.filter((url) => present.some((b) => b.value === url));
    const picks: PickedRow[] = [];
    for (const box of present) {
      const on = selected.includes(box.value);
      box.checked = on;
      box.closest<HTMLElement>("[data-row]")?.toggleAttribute("data-selected", on);
      if (on)
        picks.push({
          url: box.value,
          feature: box.dataset.feature ?? "",
          armed: box.dataset.armed !== undefined,
        });
    }
    for (const row of rows(doc)) row.toggleAttribute("data-cursor", rowUrl(row) === cursor);
    const bar = doc.getElementById("select-bar");
    if (!bar) return;
    bar.hidden = picks.length === 0;
    const sum = summarize(picks);
    const text = bar.querySelector("[data-select-summary]");
    if (text) text.textContent = `${sum.count} selected · ${sum.scope}`;
    const link = bar.querySelector<HTMLAnchorElement>("[data-select-launch]");
    if (link) {
      link.textContent = `Launch ${sum.now} now · arm ${sum.arm}`;
      link.href = launchHref(selected);
    }
  }

  function groupUrls(el: Element): string[] {
    return [...(el.closest("section")?.querySelectorAll<HTMLInputElement>("input[data-select]") ?? [])].map(
      (b) => b.value,
    );
  }

  doc.addEventListener(
    "click",
    (e) => {
      const target = e.target;
      if (!(target instanceof Element)) return;
      if (target instanceof HTMLInputElement && target.matches("input[data-select]")) {
        const url = target.value;
        const order = boxes(doc).map((b) => b.value);
        selected =
          e.shiftKey && anchor !== null
            ? withAll(selected, rangeBetween(order, anchor, url))
            : target.checked
              ? withAll(selected, [url])
              : without(selected, [url]);
        anchor = cursor = url;
        render();
        return;
      }
      const all = target.closest("[data-select-all]");
      if (all) {
        selected = withAll(selected, groupUrls(all));
        render();
        return;
      }
      const clearGroup = target.closest("[data-select-clear]");
      if (clearGroup) {
        selected = without(selected, groupUrls(clearGroup));
        render();
        return;
      }
      if (target.closest("[data-select-reset]")) {
        selected = [];
        render();
      }
    },
    { signal },
  );

  doc.addEventListener(
    "htmx:afterSwap",
    () => {
      if (!doc.querySelector("#board")) selected = [];
      render();
    },
    { signal },
  );

  function cursorRow(): HTMLElement | undefined {
    return rows(doc).find((r) => rowUrl(r) === cursor);
  }

  return {
    hasList: () => doc.querySelector(listSelector) !== null,
    hasSelection: () => selected.length > 0,
    moveCursor(step) {
      const all = rows(doc);
      const next = stepCursor(
        all.length,
        all.findIndex((r) => rowUrl(r) === cursor),
        step,
      );
      const row = all[next];
      cursor = row ? rowUrl(row) : null;
      render();
      row?.scrollIntoView({ block: "nearest" });
    },
    toggleCursor() {
      const box = cursorRow()?.querySelector<HTMLInputElement>("input[data-select]");
      if (!box) return;
      selected = toggled(selected, box.value);
      anchor = box.value;
      render();
    },
    openCursor() {
      const link = cursorRow()?.querySelector("a");
      link?.click();
      return link !== undefined && link !== null;
    },
    launch() {
      const link = doc.querySelector<HTMLAnchorElement>("[data-select-launch]");
      if (selected.length === 0 || !link) return false;
      link.click();
      return true;
    },
    clear() {
      selected = [];
      render();
    },
  };
}
