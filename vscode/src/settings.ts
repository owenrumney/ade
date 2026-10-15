import * as vscode from "vscode";
import type { Server } from "./discover";

// Said is what the page sends: it is ready, or it wants one settings route.
interface Said {
  kind?: string;
  id?: string;
  path?: string;
  method?: string;
  body?: unknown;
}

// Settings is a native editor panel. The server remains the settings store.
export class Settings implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private server?: Server;

  constructor(private context: vscode.ExtensionContext) {}

  dispose(): void {
    this.panel?.dispose();
  }

  // revive takes back a panel VS Code restored across a reload, which is
  // otherwise a tab nothing ever draws into.
  revive(panel: vscode.WebviewPanel, server: Server | undefined): void {
    this.panel?.dispose();
    this.panel = panel;
    panel.webview.options = {
      enableScripts: true,
      localResourceRoots: [this.context.extensionUri],
    };
    panel.onDidDispose(() => (this.panel = undefined));
    this.wire(panel.webview);
    if (!server) {
      panel.webview.html = offline();
      return;
    }
    void this.show(server);
  }

  async show(server: Server): Promise<void> {
    this.server = server;
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.settings",
        "ade · Settings",
        vscode.ViewColumn.Active,
        {
          enableScripts: true,
          retainContextWhenHidden: true,
          localResourceRoots: [this.context.extensionUri],
        },
      );
      this.panel.onDidDispose(() => (this.panel = undefined));
      this.wire(this.panel.webview);
    }
    this.draw();
    this.hand();
    this.panel.reveal(this.panel.viewColumn, false);
  }

  serving(server: Server | undefined): void {
    this.server = server;
    if (!this.panel) return;
    if (!server) {
      this.panel.webview.html = offline();
      return;
    }
    this.draw();
    this.hand();
  }

  // wire is what the page may ask of the editor: a reply when it is ready,
  // and ade's own API, which only this process is sure of reaching.
  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind === "api" && said.id) void this.relay(webview, said);
    });
  }

  private async relay(webview: vscode.Webview, said: Said): Promise<void> {
    const server = this.server;
    const back = (answer: Record<string, unknown>) =>
      void webview.postMessage({ kind: "api.result", id: said.id, ...answer });
    if (!server || !said.path?.startsWith("/api/settings")) {
      back({ error: "ade is not running for this folder." });
      return;
    }
    try {
      const res = await fetch(`${server.base}${said.path}`, {
        method: said.method ?? "GET",
        headers: {
          "X-Ade-Token": server.token,
          "Content-Type": "application/json",
        },
        body: said.body === undefined ? undefined : JSON.stringify(said.body),
      });
      const body = (await res.json().catch(() => ({}))) as {
        error?: string;
      };
      if (!res.ok) {
        back({ error: body.error || `ade answered ${res.status}` });
        return;
      }
      back({ view: body });
    } catch (err) {
      back({ error: String(err) });
    }
  }

  private draw(): void {
    if (!this.panel || !this.server) return;
    const asset = (...bits: string[]) =>
      this.panel!.webview.asWebviewUri(
        vscode.Uri.joinPath(this.context.extensionUri, ...bits),
      );
    const nonce = Math.random().toString(36).slice(2);
    this.panel.webview.html = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${this.panel.webview.cspSource}; script-src 'nonce-${nonce}';" />
    <link rel="stylesheet" href="${asset("media", "settings.css")}" />
  </head>
  <body>
    <main>
      <h1>Settings</h1>
      <p id="path"></p>
      <div id="status" role="status"></div>
      <form id="settings">
        <section><h2>Repositories</h2><label>Scan directories<textarea id="scan-dirs" rows="4"></textarea></label><label>Ignored directories<textarea id="ignored-dirs" rows="3"></textarea></label></section>
        <section><h2>Instructions</h2><label>Every new agent<textarea id="instructions" rows="7"></textarea></label><p id="instruction-limit"></p></section>
        <section><h2>Dashboard</h2><label>URL<input id="dashboard-url" type="url" placeholder="http://pi.local:8080" /></label><label>Machine name<input id="dashboard-host" /></label><label>Claude account<input id="dashboard-claude" /></label><label>Codex account<input id="dashboard-codex" /></label><div class="secret"><label>Token <span id="dashboard-token-state"></span><input id="dashboard-token" type="password" autocomplete="off" /></label><button type="button" data-secret="dashboard-token">Save token</button><button type="button" data-forget="dashboard-token">Forget</button></div></section>
        <section><h2>Integrations</h2><div class="secret"><label>Linear API key <span id="linear-key-state"></span><input id="linear-key" type="password" autocomplete="off" /></label><button type="button" data-secret="linear-key">Save key</button><button type="button" data-forget="linear-key">Forget</button></div><label>Home Assistant URL<input id="home-assistant-url" type="url" /></label><label>Desk light<input id="home-assistant-desk" placeholder="light.desk" /></label><label>Notify service<input id="home-assistant-notify" placeholder="notify.mobile_app" /></label><div class="secret"><label>Home Assistant token <span id="home-assistant-token-state"></span><input id="home-assistant-token" type="password" autocomplete="off" /></label><button type="button" data-secret="home-assistant-token">Save token</button><button type="button" data-forget="home-assistant-token">Forget</button></div><div class="secret"><label>GitHub Packages token <span id="github-packages-token-state"></span><input id="github-packages-token" type="password" autocomplete="off" /></label><button type="button" data-secret="github-packages-token">Save token</button><button type="button" data-forget="github-packages-token">Forget</button></div></section>
        <section><h2>Preview</h2><label>App URL<input id="preview-url" type="url" placeholder="http://localhost:3000" /></label><label class="check"><input id="preview-off" type="checkbox" /> Turn off capture</label></section><section><h2>Prompts</h2><label class="check"><input id="sharpen-off" type="checkbox" /> Do not ask about a vague prompt before it is sent</label><label class="check"><input id="cleanse-off" type="checkbox" /> Do not rewrite the agent's replies short</label><label class="check"><input id="merge-queued-off" type="checkbox" /> Do not send prompts queued during one run as one turn</label></section>
        <button id="save" type="submit">Save settings</button>
      </form>
    </main>
    <script nonce="${nonce}" src="${asset("dist", "settings.js")}"></script>
  </body>
</html>`;
  }

  // The page is told there is something to read, not how to read it: the
  // token stays in this process.
  private hand(): void {
    if (!this.panel || !this.server) return;
    void this.panel.webview.postMessage({ kind: "serving" });
  }
}

function offline(): string {
  return "<!doctype html><body>ade must be running to edit settings.</body>";
}
