export const maxLiveLines = 1000;

const railCookieName = "rail-open";
const wideViewport = "(min-width: 760px)";

export function failureText(event: "responseError" | "sendError", status?: number): string {
  if (event === "sendError") return "Could not reach the server.";
  return `The server failed (${status ?? "no status"}). The cause is in its log.`;
}

export function ago(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${s % 60 ? `${s % 60}s` : ""} ago`;
  const h = Math.floor(m / 60);
  return `${h}h${m % 60 ? `${m % 60}m` : ""} ago`;
}

export function elapsed(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const m = Math.floor(s / 60);
  return m < 60 ? `${m}m${s % 60}s` : `${Math.floor(m / 60)}h${m % 60}m`;
}

export function railOpenCookie(cookie: string, key: string, open: boolean): string {
  const current = cookie
    .split("; ")
    .filter((c) => c.startsWith(`${railCookieName}=`))
    .flatMap((c) => decodeURIComponent(c.slice(railCookieName.length + 1)).split("|"))
    .filter((k) => k !== "" && k !== key);
  if (open) current.push(key);
  return `${railCookieName}=${encodeURIComponent(current.join("|"))}; path=/; max-age=31536000; samesite=lax`;
}

export function isCurrentPath(linkPath: string, here: string): boolean {
  return linkPath === "/" ? here === "/" : here.startsWith(linkPath);
}

export function scopeOf(href: string, origin: string): string {
  const q = new URL(href, origin).searchParams;
  const project = q.get("project");
  if (project) return `project=${project}`;
  const repo = q.get("repo");
  return repo ? `repo=${repo}` : "";
}

export function modifierLabel(text: string, apple: boolean): string {
  return apple ? text : text.replace("⌘", "Ctrl ");
}

export function shouldHop(target: string | undefined, wide: boolean): boolean {
  return wide && target !== undefined && target !== "";
}

function show(text: string): void {
  const flash = document.getElementById("flash");
  if (!flash) return;
  const message = document.createElement("p");
  message.className = "banner ribbon";
  message.dataset.glyph = "failed";
  message.textContent = text;
  flash.replaceChildren(message);
}

function tickTimes(): void {
  const now = Date.now();
  for (const el of document.querySelectorAll<HTMLTimeElement>("time[datetime]")) {
    const at = Date.parse(el.dateTime);
    if (!Number.isNaN(at)) el.textContent = ago(now - at);
  }
}

function tickStarted(): void {
  const now = Date.now();
  for (const el of document.querySelectorAll<HTMLElement>("[data-started]")) {
    const at = Date.parse(el.dataset.started ?? "");
    if (!Number.isNaN(at)) el.textContent = elapsed(now - at);
  }
}

function markCurrent(): void {
  for (const a of document.querySelectorAll<HTMLAnchorElement>(".sidebar-item")) {
    if (isCurrentPath(a.pathname, location.pathname)) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  }
}

function closeScopeMenu(): void {
  const here = scopeOf(location.href, location.origin);
  for (const d of document.querySelectorAll<HTMLDetailsElement>("details[data-scope-menu]")) {
    d.open = false;
    for (const a of d.querySelectorAll("a")) {
      const current = scopeOf(a.getAttribute("href") ?? "", location.origin) === here;
      if (current) {
        const summary = d.querySelector("summary");
        if (summary) summary.innerHTML = a.innerHTML;
        a.setAttribute("aria-current", "true");
      } else {
        a.removeAttribute("aria-current");
      }
    }
  }
}

function hop(): void {
  const target = document.getElementById("main")?.dataset.desktopTarget;
  if (target && shouldHop(target, matchMedia(wideViewport).matches)) location.replace(target);
}

function relabelModifiers(): void {
  const apple = /Mac|iPhone|iPad/.test(navigator.platform);
  if (apple) return;
  for (const k of document.querySelectorAll("#key-sheet kbd, .rail-search-wrap kbd")) {
    k.textContent = modifierLabel(k.textContent ?? "", apple);
  }
}

function eventElement(e: Event): Element | null {
  return e.target instanceof Element ? e.target : null;
}

function initGlue(): void {
  document.addEventListener("htmx:responseError", (e) => {
    const detail = (e as CustomEvent<{ xhr?: { status?: number } }>).detail;
    show(failureText("responseError", detail?.xhr?.status));
  });
  document.addEventListener("htmx:sendError", () => show(failureText("sendError")));

  document.addEventListener("htmx:beforeRequest", (e) => eventElement(e)?.setAttribute("aria-busy", "true"));
  document.addEventListener("htmx:afterRequest", (e) => {
    const el = eventElement(e);
    el?.removeAttribute("aria-busy");
    const detail = (e as CustomEvent<{ successful?: boolean }>).detail;
    if (detail?.successful && el instanceof HTMLFormElement && el.hasAttribute("data-reset-on-success")) el.reset();
  });

  document.addEventListener("click", (e) => {
    const trigger = eventElement(e)?.closest("[data-show-modal]");
    const dialog = trigger && document.getElementById(trigger.getAttribute("data-show-modal") ?? "");
    if (dialog instanceof HTMLDialogElement && !dialog.open) dialog.showModal();
  });

  document.addEventListener(
    "toggle",
    (e) => {
      const el = eventElement(e);
      if (!(el instanceof HTMLDetailsElement) || !el.dataset.railKey || el.hasAttribute("data-rail-forced")) return;
      document.cookie = railOpenCookie(document.cookie, el.dataset.railKey, el.open);
    },
    true,
  );

  document.addEventListener("htmx:sseMessage", (e) => {
    const pre = eventElement(e);
    while (pre && pre.childElementCount > maxLiveLines) pre.firstElementChild?.remove();
  });

  for (const name of ["htmx:pushedIntoHistory", "htmx:historyRestore"]) {
    document.addEventListener(name, closeScopeMenu);
    document.addEventListener(name, markCurrent);
  }
  document.addEventListener("htmx:afterSwap", tickStarted);
  document.addEventListener("htmx:afterSettle", hop);

  setInterval(tickTimes, 1000);
  setInterval(tickStarted, 1000);
  relabelModifiers();
  hop();
}

if (typeof document !== "undefined") initGlue();
