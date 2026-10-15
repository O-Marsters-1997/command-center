import { customElement, getCurrentElement, noShadowDOM } from "solid-element";
import type { JSX } from "solid-js";

export function defineIsland<P extends Record<string, unknown>>(
  tag: `cc-${string}`,
  defaults: P,
  render: (props: P, element: HTMLElement) => JSX.Element,
): void {
  customElement(tag, defaults, (props: P) => {
    // solid-element appends its render to light DOM; clear the server's fallback before mounting.
    noShadowDOM();
    const element = getCurrentElement();
    element.textContent = "";
    return render(props, element);
  });
}

export function hx(el: Element): void {
  queueMicrotask(() => window.htmx?.process(el));
}
