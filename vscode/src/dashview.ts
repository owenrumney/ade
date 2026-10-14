import * as vscode from "vscode";
import type { Board, Do } from "./dashboard/view";

interface Said {
  kind?: string;
  what?: string;
  [key: string]: unknown;
}

// Dashboard is the home tab: what is waiting on you, your reviews and pull
// requests, the issues in flight, and the sessions, each line a thing to do.
export class Dashboard implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private board?: Board;
  private busy = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private feed: () => Promise<Board>,
    private act: (what: Do) => Promise<string | void>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  stale(): void {
    if (this.panel?.visible) void this.read();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.dashboard",
        "ADE",
        vscode.ViewColumn.Active,
        {
          enableScripts: true,
          retainContextWhenHidden: true,
          localResourceRoots: [this.context.extensionUri],
        },
      );
      this.panel.onDidDispose(() => (this.panel = undefined));
      this.wire(this.panel.webview);
      this.draw();
    }
    this.panel.reveal(this.panel.viewColumn, false);
    await this.read();
  }

  revive(panel: vscode.WebviewPanel): void {
    this.panel?.dispose();
    this.panel = panel;
    panel.webview.options = {
      enableScripts: true,
      localResourceRoots: [this.context.extensionUri],
    };
    panel.onDidDispose(() => (this.panel = undefined));
    this.wire(panel.webview);
    this.draw();
    void this.read();
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind !== "do" || typeof said.what !== "string") return;
      if (said.what === "refresh") void this.read(true);
      else void this.run(said as Do);
    });
  }

  private async run(what: Do): Promise<void> {
    try {
      const told = await this.act(what);
      if (told) {
        this.note = told;
        this.hand();
      }
    } catch (err) {
      this.note = String(err);
      this.hand();
      return;
    }
    await this.read();
  }

  private async read(hard = false): Promise<void> {
    this.busy = true;
    this.note = hard ? "Asking GitHub and Linear again…" : "";
    this.hand();
    try {
      if (hard) await this.act({ what: "refresh" });
      this.board = await this.feed();
      this.note = "";
    } catch (err) {
      this.note = String(err);
    } finally {
      this.busy = false;
      this.hand();
    }
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      board: this.board,
      busy: this.busy,
      note: this.note,
    });
  }

  private draw(): void {
    if (!this.panel) return;
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
    <link rel="stylesheet" href="${asset("media", "dashboard.css")}" />
  </head>
  <body><main id="dashboard"></main>
    <script nonce="${nonce}" src="${asset("dist", "dashboard.js")}"></script>
  </body>
</html>`;
  }
}
