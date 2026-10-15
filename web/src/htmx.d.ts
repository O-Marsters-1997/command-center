declare global {
  interface Window {
    htmx?: { process: (el: Element) => void };
  }
}
export {};
