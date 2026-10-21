import * as vscode from "vscode";
import { brief } from "./purgeview";
import type { Body, SandboxFile } from "./sandboxfile";

interface Said {
  kind?: string;
  body?: Body;
}

// SandboxPage is the repository's sandbox as a panel: every mount and every
// variable at once, rather than one pick on top of another.
export class SandboxPage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private file?: SandboxFile;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private read: () => Promise<SandboxFile>,
    private write: (body: Body) => Promise<SandboxFile>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.sandbox",
        "ade · Sandbox",
        vscode.ViewColumn.Active,
        {
          enableScripts: true,
          retainContextWhenHidden: true,
          localResourceRoots: [this.context.extensionUri],
        },
      );
      this.panel.onDidDispose(() => {
        this.panel = undefined;
      });
      // The quick pick writes the same file, so a panel coming back to the
      // front re-reads rather than showing what it had when it was hidden.
      this.panel.onDidChangeViewState((event) => {
        if (event.webviewPanel.visible) void this.refresh();
      });
      this.wire(this.panel.webview);
      this.draw();
    }
    this.panel.reveal(this.panel.viewColumn, false);
    await this.refresh();
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind === "save" && said.body) void this.save(said.body);
      if (said.kind === "browse") void this.browse();
    });
  }

  private async refresh(): Promise<void> {
    try {
      this.file = await this.read();
      this.note = "";
    } catch (err) {
      this.note = brief(err);
    }
    this.hand();
  }

  private async save(body: Body): Promise<void> {
    try {
      this.file = await this.write(body);
      this.note = "";
    } catch (err) {
      // The file is unchanged, so the list the panel shows is still the
      // file: re-read rather than leave the refused edit on screen.
      this.note = brief(err);
      await this.refresh();
      return;
    }
    this.hand();
  }

  private async browse(): Promise<void> {
    const [folder] =
      (await vscode.window.showOpenDialog({
        canSelectFiles: true,
        canSelectFolders: true,
        canSelectMany: false,
        openLabel: "Mount",
      })) ?? [];
    if (!folder) return;
    void this.panel?.webview.postMessage({ kind: "mounted", path: folder.fsPath });
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      file: this.file,
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
    <link rel="stylesheet" href="${asset("media", "sandbox.css")}" />
    <link rel="stylesheet" href="${asset("media", "mounts.css")}" />
  </head>
  <body><main id="sandbox"></main>
    <script nonce="${nonce}" src="${asset("dist", "sandbox.js")}"></script>
  </body>
</html>`;
  }
}
