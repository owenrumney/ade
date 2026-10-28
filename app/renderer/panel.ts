import { el } from "./omnibox";
import { THEME } from "./vstheme";
import "./bridge";

// Hosting the pages under app/pages: one iframe each, as VS Code does.

export interface Panel {
  id: string;
  title: string;
  root: string;
  bundle: string;
  styles: string[];
  // onMessage answers what the panel sends. Returning a value posts it
  // back, which is how these webviews are fed.
  onMessage(said: Record<string, unknown>): Promise<unknown>;
  // A page that never reaches a session server draws before one is picked.
  launcher?: boolean;
  // stirs is whether an ade event hands this panel a fresh state. Opt
  // in: the designs sweep is a `gh pr list` per repository.
  stirs?: boolean;
  // data is written onto the root element, for a panel that fetches ade
  // itself rather than asking its host. The chat is the one that does.
  data?: Record<string, string>;
  // body replaces the bare root element, for a panel whose markup is
  // fixed rather than drawn. Shared with the extension, never copied.
  body?: string;
  // before are bundles the page needs loaded ahead of its own. The
  // design editor wants mermaid on the window before it starts.
  before?: string[];
  // css is a rule or two the page needs that are nobody's stylesheet,
  css?: string;
}

// The pages' built assets, on the webview scheme. Not file://:
// the session server allows CORS to a webview scheme and nothing else.
export const UNDER = "ade-webview://asset";

// skeleton is the host half of a webview, as `*view.ts` writes it, down
// to the shim that makes `acquireVsCodeApi()` reach this window.
export function skeleton(panel: Panel): string {
  // The shim is inline, so the policy has to name it, or the page draws
  // and `acquireVsCodeApi` is never defined.
  const nonce = Math.random().toString(36).slice(2);
  const styles = panel.styles
    .map((s) => `<link rel="stylesheet" href="${UNDER}/media/${s}" />`)
    .join("\n    ");
  const attrs = Object.entries(panel.data ?? {})
    .map(([k, v]) => ` data-${k}="${v.replace(/"/g, "&quot;")}"`)
    .join("");
  return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <!-- The panel's own policy: its bundle and styles come from this
         scheme, and it reaches ade over loopback. Same shape as the
         one a webview host writes, with this scheme in
         place of a webview's own cspSource. -->
    <meta http-equiv="Content-Security-Policy"
          content="default-src 'none'; style-src ade-webview: 'unsafe-inline'; script-src 'nonce-${nonce}' ade-webview:; img-src ade-webview: data: http://127.0.0.1:* http://localhost:*; font-src ade-webview:; connect-src http://127.0.0.1:* http://localhost:*;" />
    <!-- The theme first: a panel is a VS Code webview and reads the
         theme off the document as --vscode-* variables. Before the
         stylesheets, so a panel may override one. -->
    <style>
      :root {${THEME}}
      html, body { height: 100%; margin: 0; background: var(--vscode-editor-background); }
      body {
        color: var(--vscode-foreground);
        font: var(--vscode-font-size)/1.45 var(--vscode-font-family);
      }
      /* Chromium's default scrollbar is a white stripe on this
         background. VS Code themes its own; a page of ours has to. */
      * { scrollbar-color: var(--vscode-widget-border) transparent; scrollbar-width: thin; }
      ::-webkit-scrollbar { width: 10px; height: 10px; }
      ::-webkit-scrollbar-track { background: transparent; }
      ::-webkit-scrollbar-thumb {
        background: var(--vscode-widget-border);
        border: 2px solid transparent;
        background-clip: content-box;
        border-radius: 6px;
      }
      ::-webkit-scrollbar-thumb:hover {
        background: var(--vscode-descriptionForeground);
        background-clip: content-box;
      }
    </style>
    ${styles}
    <style>${panel.css ?? ""}</style>
  </head>
  <body>
    ${panel.body ?? `<main id="${panel.root}"${attrs}></main>`}
    ${(panel.before ?? [])
      .map((b) => `<script src="${UNDER}/dist/${b}"></script>`)
      .join("\n    ")}
    <script nonce="${nonce}">
      // The shim. A webview's whole contract with its host is this
      // object, so the panels need no change to run here.
      const sent = [];
      window.acquireVsCodeApi = () => ({
        postMessage: (m) => parent.postMessage({ from: ${JSON.stringify(panel.id)}, said: m }, "*"),
        getState: () => undefined,
        setState: () => undefined,
      });
    </script>
    <script src="${UNDER}/dist/${panel.bundle}"></script>
  </body>
</html>`;
}

// open draws the panel and carries its messages. The iframe comes
// back, for the caller to put where it keeps panels.
export function open(panel: Panel): { frame: HTMLIFrameElement; stop: () => void } {
  const frame = document.createElement("iframe");
  frame.className = "panel";
  // Fetched from the webview scheme, so it has an origin: srcdoc leaves
  // it opaque, and the session server refuses that.
  void window.ade?.panel(panel.id, skeleton(panel)).then((url) => {
    frame.src = url;
  });

  const listen = async (e: MessageEvent) => {
    const message = e.data as { from?: string; said?: Record<string, unknown> };
    if (message?.from !== panel.id || !message.said) return;
    const answer = await panel.onMessage(message.said).catch((err: Error) => ({
      kind: "error",
      error: err.message,
    }));
    // An answer may be several: the design editor gets style then text.
    if (answer === undefined) return;
    for (const one of Array.isArray(answer) ? answer : [answer]) {
      frame.contentWindow?.postMessage(one, "*");
    }
  };
  window.addEventListener("message", listen);
  return { frame, stop: () => window.removeEventListener("message", listen) };
}

// A panel, kept. Every read is a redraw, so a rebuilt iframe would
// reload the chat every few seconds. Rebuilt only when `key` moves.
const held = new Map<
  string,
  { key: string; node: HTMLElement; stop: () => void; panel: Panel }
>();

// stirred asks every panel that takes a push what it is now, and posts
// it. Work that finishes elsewhere reaches the page no other way.
// Quiet: nobody pressed, so the page is not told it is busy once a second.
export async function stirred(): Promise<void> {
  for (const { node, panel } of held.values()) {
    if (!panel.stirs) continue;
    const said = await panel
      .onMessage({ kind: "do", what: "refresh", quiet: true })
      .catch(() => undefined);
    if (said) node.querySelector("iframe")?.contentWindow?.postMessage(said, "*");
  }
}

// pushTo posts to one panel unasked. A host that only answers cannot say
// "I have started", so its buttons stay live under a mashing finger.
export function pushTo(id: string, said: unknown): void {
  held.get(id)?.node.querySelector("iframe")?.contentWindow?.postMessage(said, "*");
}

// tell posts to every panel that is up: one that does not hear the
// session's events shows what was true when it last fetched.
export function tell(message: unknown): void {
  for (const { node } of held.values()) {
    node.querySelector("iframe")?.contentWindow?.postMessage(message, "*");
  }
}

export function frame(panel: Panel, key: string): HTMLElement {
  const have = held.get(panel.id);
  if (have && have.key === key) return have.node;
  have?.stop();
  const wrap = el("div", "panel-wrap");
  const { frame: node, stop } = open(panel);
  wrap.append(node);
  held.set(panel.id, { key, node: wrap, stop, panel });
  return wrap;
}
