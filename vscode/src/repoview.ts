import * as vscode from "vscode";
import type { Repo } from "./repo/view";

// RepoPage is what this checkout is and what ade has done in it. One reading
// per open; the launcher counts a million lines, so it is not watched.
export class RepoPage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private repo?: Repo;
  private busy = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private feed: () => Promise<Repo>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.repo",
        "Repository",
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

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: { kind?: string; what?: string }) => {
      if (said.kind === "ready") this.hand();
      if (said.kind === "do" && said.what === "refresh") void this.read();
    });
  }

  private async read(): Promise<void> {
    this.busy = true;
    this.hand();
    try {
      this.repo = await this.feed();
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
      repo: this.repo,
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
    <link rel="stylesheet" href="${asset("media", "repo.css")}" />
  </head>
  <body><main id="repo"></main>
    <script nonce="${nonce}" src="${asset("dist", "repo.js")}"></script>
  </body>
</html>`;
  }
}
