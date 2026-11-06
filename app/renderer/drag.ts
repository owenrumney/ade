// WKWebView has no -webkit-app-region, so the titlebar asks the window
// host to move itself and the page decides where a drag may start.

export const LIVE = ["button", "input", "textarea", "select", "a", ".box"];

type Hit = { closest(selector: string): unknown } | null | undefined;

export function dragsWindow(target: Hit): boolean {
  if (!target || typeof target.closest !== "function") return false;
  return !LIVE.some((selector) => target.closest(selector));
}

declare global {
  interface Window {
    webkit?: { messageHandlers?: Record<string, { postMessage(body: unknown): void }> };
  }
}

export function dragsBy(bar: HTMLElement): void {
  bar.addEventListener("mousedown", (e) => {
    if (e.button !== 0 || !dragsWindow(e.target as Hit)) return;
    const want = e.detail === 1 ? "move" : e.detail === 2 ? "zoom" : "";
    if (want) window.webkit?.messageHandlers?.adeWindow?.postMessage(want);
  });
}
