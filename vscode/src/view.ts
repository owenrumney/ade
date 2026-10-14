import * as vscode from "vscode";
import type { Server } from "./discover";

// Every loopback port, not just ade's: the page nests a frame to the launcher on
// another port, and naming one origin leaves the view silently blank.
const FRAMES = "http://127.0.0.1:* http://localhost:*";

const options: vscode.WebviewOptions = {
  enableScripts: true,
  localResourceRoots: [],
};

// embed=1 asks ade's page to drop its own title bar, rail and status bar. The
// editor draws all three, and without it the whole application is in the pane.
export function page(server: Server, hash: string): string {
  return `${server.base}/?embed=1&t=${encodeURIComponent(server.token)}${hash}`;
}

// Panels are ade's pages in the editor area, one tab each. Keyed, because Chat
// and Diff open together and a single reused panel would swap one for the other.
export class Panels implements vscode.Disposable {
  private held = new Map<string, vscode.WebviewPanel>();

  dispose(): void {
    for (const panel of this.held.values()) panel.dispose();
    this.held.clear();
  }

  show(key: string, title: string, url: string | undefined): void {
    let panel = this.held.get(key);
    if (!panel) {
      panel = vscode.window.createWebviewPanel(
        `ade.${key}`,
        title,
        vscode.ViewColumn.Active,
        {
          ...options,
          retainContextWhenHidden: true,
        },
      );
      panel.onDidDispose(() => this.held.delete(key));
      this.held.set(key, panel);
    }
    panel.title = title;
    panel.webview.html = url ? frame(url) : waiting();
    panel.reveal(panel.viewColumn, false);
  }

  // closed reports whether nothing is open, so a reconnect does not reopen tabs
  // somebody closed on purpose.
  closed(): boolean {
    return this.held.size === 0;
  }

  // serving repoints whatever is open at a server that has just come or gone.
  serving(url: (key: string) => string | undefined): void {
    for (const [key, panel] of this.held) {
      const at = url(key);
      panel.webview.html = at ? frame(at) : waiting();
    }
  }
}

// The colours ade is asked to wear, read off the webview itself. An extension
// cannot enumerate a theme, but the webview has every --vscode-* var computed.
const PAINT: Record<string, string> = {
  bg: "--vscode-editor-background",
  panel: "--vscode-sideBar-background",
  text: "--vscode-editor-foreground",
  muted: "--vscode-descriptionForeground",
  accent: "--vscode-textLink-foreground",
  red: "--vscode-errorForeground",
  green: "--vscode-gitDecoration-addedResourceForeground",
  amber: "--vscode-editorWarning-foreground",
};

function frame(url: string): string {
  return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy"
          content="default-src 'none'; frame-src ${FRAMES}; style-src 'unsafe-inline'; script-src 'unsafe-inline';" />
    <style>
      html, body { height: 100%; margin: 0; padding: 0; background: var(--vscode-editor-background); }
      iframe { border: 0; width: 100%; height: 100%; display: block; }
    </style>
  </head>
  <body>
    <iframe id="ade" allow="clipboard-read; clipboard-write"></iframe>
    <script>
      const PAINT = ${JSON.stringify(PAINT)};
      const style = getComputedStyle(document.body);
      // ade takes #rrggbb and nothing else, and a theme reports rgb().
      const hex = (raw) => {
        const value = (raw || "").trim();
        if (/^#[0-9a-f]{6}$/i.test(value)) return value;
        if (/^#[0-9a-f]{3}$/i.test(value)) return "#" + value.slice(1).split("").map((c) => c + c).join("");
        const m = value.match(/rgba?\(([^)]+)\)/);
        if (!m) return "";
        const [r, g, b] = m[1].split(",").map((n) => Math.max(0, Math.min(255, Math.round(parseFloat(n)))));
        return "#" + [r, g, b].map((n) => n.toString(16).padStart(2, "0")).join("");
      };
      const colors = {};
      for (const [name, wanted] of Object.entries(PAINT)) {
        const got = hex(style.getPropertyValue(wanted));
        if (got) colors[name] = got;
      }
      // The src is set here rather than in the markup so the page never paints
      // in its own colours first and then change under the reader.
      const at = new URL(${JSON.stringify(url)});
      if (Object.keys(colors).length) at.searchParams.set("colors", JSON.stringify(colors));
      document.getElementById("ade").src = at.toString();
    </script>
  </body>
</html>`;
}

function waiting(): string {
  return `<!doctype html>
<html>
  <head><meta charset="utf-8" /></head>
  <body style="font: 13px var(--vscode-font-family); color: var(--vscode-descriptionForeground); padding: 12px">
    ade is not running for this folder.
  </body>
</html>`;
}

// The permissions page is ade's own, served whole, so it needs no tab in the
// app and works the same in a browser.
export function rulesPage(server: Server): string {
  return `${server.base}/permissions?t=${encodeURIComponent(server.token)}`;
}
