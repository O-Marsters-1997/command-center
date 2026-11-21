export const sidebar = { min: 220, max: 440, initial: 300, strip: 52 } as const;

export type SidebarState = { width: number; collapsed: boolean };

const cookieAttrs = "path=/; max-age=31536000; samesite=lax";
const keyStep = 16;

export function clampWidth(px: number): number {
  if (!Number.isFinite(px)) return sidebar.initial;
  return Math.min(sidebar.max, Math.max(sidebar.min, Math.round(px)));
}

export function sidebarCookies(state: SidebarState): string[] {
  return [
    `rail-width=${clampWidth(state.width)}; ${cookieAttrs}`,
    `rail-collapsed=${state.collapsed ? 1 : 0}; ${cookieAttrs}`,
  ];
}

export function keyedWidth(width: number, key: string): number | null {
  if (key === "ArrowLeft") return clampWidth(width - keyStep);
  if (key === "ArrowRight") return clampWidth(width + keyStep);
  return null;
}

export function initSidebar(doc: Document): void {
  const app = () => doc.querySelector<HTMLElement>(".app");

  function current(): SidebarState | null {
    const el = app();
    if (!el) return null;
    const width = clampWidth(Number.parseFloat(el.style.getPropertyValue("--sidebar-w")));
    return { width, collapsed: el.hasAttribute("data-collapsed") };
  }

  function apply(state: SidebarState, persist: boolean): void {
    const el = app();
    if (!el) return;
    el.style.setProperty("--sidebar-w", `${state.width}px`);
    el.toggleAttribute("data-collapsed", state.collapsed);
    doc.querySelector(".sidebar-resizer")?.setAttribute("aria-valuenow", String(state.width));
    for (const button of doc.querySelectorAll("[data-sidebar-toggle]")) {
      button.setAttribute("aria-expanded", String(!state.collapsed));
    }
    if (persist) for (const cookie of sidebarCookies(state)) doc.cookie = cookie;
  }

  const target = (e: Event) => (e.target instanceof Element ? e.target : null);

  doc.addEventListener("click", (e) => {
    const state = current();
    if (state && target(e)?.closest("[data-sidebar-toggle]"))
      apply({ ...state, collapsed: !state.collapsed }, true);
  });

  doc.addEventListener("focusin", (e) => {
    const state = current();
    if (state?.collapsed && target(e)?.closest("#rail-search")) apply({ ...state, collapsed: false }, true);
  });

  doc.addEventListener("dblclick", (e) => {
    const state = current();
    if (state && target(e)?.closest(".sidebar-resizer")) apply({ ...state, width: sidebar.initial }, true);
  });

  doc.addEventListener("keydown", (e) => {
    const state = current();
    if (!state || !target(e)?.closest(".sidebar-resizer")) return;
    const width = keyedWidth(state.width, e.key);
    if (width === null) return;
    e.preventDefault();
    apply({ ...state, width }, true);
  });

  doc.addEventListener("pointerdown", (e) => {
    const handle = target(e)?.closest<HTMLElement>(".sidebar-resizer");
    const start = current();
    if (!handle || !start || e.button !== 0) return;
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    doc.documentElement.setAttribute("data-sidebar-resizing", "");
    let state = start;
    const drag = new AbortController();
    const end = () => {
      drag.abort();
      doc.documentElement.removeAttribute("data-sidebar-resizing");
      apply(state, true);
    };
    handle.addEventListener(
      "pointermove",
      (ev) => {
        state = { ...start, width: clampWidth(start.width + ev.clientX - e.clientX) };
        apply(state, false);
      },
      { signal: drag.signal },
    );
    handle.addEventListener("pointerup", end, { signal: drag.signal });
    handle.addEventListener("pointercancel", end, { signal: drag.signal });
  });
}
