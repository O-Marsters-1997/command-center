export function failureText(event: "responseError" | "sendError", status?: number): string {
  if (event === "sendError") return "Could not reach the server.";
  return `The server failed (${status ?? "no status"}). The cause is in its log.`;
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

if (typeof document !== "undefined") {
  document.addEventListener("htmx:responseError", (e) => {
    const detail = (e as CustomEvent<{ xhr?: { status?: number } }>).detail;
    show(failureText("responseError", detail?.xhr?.status));
  });
  document.addEventListener("htmx:sendError", () => show(failureText("sendError")));
}
