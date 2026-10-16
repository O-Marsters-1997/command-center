export interface KeyInfo {
  readonly key: string;
  readonly altKey: boolean;
  readonly ctrlKey: boolean;
  readonly metaKey: boolean;
  readonly shiftKey: boolean;
  readonly isComposing: boolean;
  readonly repeat: boolean;
}

export interface Focused {
  matches(selector: string): boolean;
  readonly isContentEditable?: boolean;
}

export interface KeyTarget extends Focused {
  readonly dataset: { readonly key?: string };
  readonly disabled?: boolean;
  getClientRects(): ArrayLike<unknown>;
  closest(selector: string): { open?: boolean } | null;
  focus(): void;
  select?(): void;
  click(): void;
}

interface Timers {
  set(fn: () => void, ms: number): () => void;
}

interface PlatformSource {
  readonly userAgentData?: { readonly platform?: string };
  readonly platform?: string;
}

const chordWindowMs = 1500;
const applePlatform = /^(mac|iphone|ipad|ios)/i;
const fieldSelector = "input, textarea, select";
const activatableSelector = "a, button, summary";
const scopeMenuSelector = "details[data-scope-menu]";

export function isApplePlatform(source: PlatformSource): boolean {
  return applePlatform.test(source.userAgentData?.platform ?? source.platform ?? "");
}

function isField(el: Focused | null): boolean {
  return el !== null && (el.matches(fieldSelector) || el.isContentEditable === true);
}

export function bindingFor(e: KeyInfo, apple: boolean): string | null {
  if (e.altKey || e.isComposing || e.repeat) return null;
  const name = e.key.toLowerCase();
  if (e.shiftKey && /^[a-z]$/.test(name)) return null;
  if (apple ? e.ctrlKey : e.metaKey) return null;
  const mod = apple ? e.metaKey : e.ctrlKey;
  if (!mod) return name;
  if (name === "k") return "mod+k";
  return /^[0-9]$/.test(name) ? `g ${name}` : null;
}

function createChord(timers: Timers) {
  let cancel: (() => void) | null = null;

  function clear(): void {
    cancel?.();
    cancel = null;
  }

  function next(binding: string): string | null {
    if (cancel) {
      clear();
      return `g ${binding}`;
    }
    if (binding === "g") {
      cancel = timers.set(clear, chordWindowMs);
      return null;
    }
    return binding;
  }

  return { next, clear };
}

export function createKeyState(apple: boolean, timers: Timers) {
  const chord = createChord(timers);

  function lookup(e: KeyInfo, focused: Focused | null): string | null {
    const binding = bindingFor(e, apple);
    if (binding === null) return null;
    if (isField(focused) && binding !== "mod+k" && binding !== "escape") return null;
    if (binding === "enter" && focused?.matches(activatableSelector)) return null;
    return chord.next(binding);
  }

  return { lookup, clear: chord.clear };
}

function isUsable(el: KeyTarget): boolean {
  return el.disabled !== true && (el.getClientRects().length > 0 || el.closest(scopeMenuSelector) !== null);
}

export function resolveTarget<T extends KeyTarget>(binding: string, candidates: Iterable<T>): T | null {
  let target: T | null = null;
  for (const el of candidates) {
    if (el.dataset.key !== binding || !isUsable(el)) continue;
    if (!target || (el.closest("#main") && !target.closest("#main"))) target = el;
  }
  return target;
}

export function activate(target: KeyTarget): void {
  if (target.matches(fieldSelector)) {
    target.focus();
    target.select?.();
    return;
  }
  const menu = target.closest(scopeMenuSelector);
  if (menu) menu.open = true;
  target.click();
}

export interface SearchBox {
  value: string;
  blur(): void;
  dispatchEvent(event: Event): boolean;
}

export function searchKey(key: string, box: SearchBox, links: readonly { click(): void }[]): boolean {
  if (key === "Enter") {
    const first = links[0];
    if (first) {
      first.click();
      box.blur();
    }
    return true;
  }
  if (key === "Escape") {
    if (box.value !== "") {
      box.value = "";
      box.dispatchEvent(new Event("search"));
    }
    box.blur();
    return true;
  }
  return false;
}

export interface RailLink {
  getAttribute(name: string): string | null;
  closest(selector: string): { open: boolean } | null;
  click(): void;
}

export function walkRail(links: readonly RailLink[], step: 1 | -1): RailLink | null {
  const current = links.findIndex((l) => l.getAttribute("aria-current") === "page");
  const index = current === -1 ? (step === 1 ? 0 : links.length - 1) : current + step;
  const target = links[index] ?? null;
  if (target) {
    const fold = target.closest("details");
    if (fold) fold.open = true;
    target.click();
  }
  return target;
}

function isHTMLElement(node: Node): node is HTMLElement {
  return node instanceof HTMLElement;
}

export function initKeys(doc: Document = document, nav: PlatformSource = navigator): () => void {
  const controller = new AbortController();
  const { signal } = controller;
  const state = createKeyState(isApplePlatform(nav), {
    set: (fn, ms) => {
      const handle = setTimeout(fn, ms);
      return () => clearTimeout(handle);
    },
  });

  doc.addEventListener(
    "keydown",
    (e) => {
      const active = doc.activeElement;
      const plain = !(e.isComposing || e.repeat || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey);
      if (active instanceof HTMLInputElement && active.id === "rail-search" && plain) {
        const matches = [...doc.querySelectorAll("#rail-body .rail-link")].filter(isHTMLElement);
        if (searchKey(e.key, active, matches)) {
          e.preventDefault();
          return;
        }
      }
      const binding = state.lookup(e, doc.activeElement);
      if (binding === null) return;
      if (binding === "j" || binding === "k") {
        const links = [...doc.querySelectorAll("#rail-body .rail-link")].filter(isHTMLElement);
        if (links.length > 0) e.preventDefault();
        walkRail(links, binding === "j" ? 1 : -1);
        return;
      }
      const target = resolveTarget(binding, [...doc.querySelectorAll("[data-key]")].filter(isHTMLElement));
      if (!target) return;
      e.preventDefault();
      activate(target);
    },
    { signal },
  );

  doc.addEventListener(
    "focusin",
    (e) => {
      if (e.target instanceof Element && isField(e.target)) state.clear();
    },
    { signal },
  );

  return () => {
    controller.abort();
    state.clear();
  };
}

if (typeof document !== "undefined") initKeys();
