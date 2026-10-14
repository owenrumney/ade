import * as vscode from "vscode";
import type { Client, Overlap } from "./client";
import { report } from "./tree";
import { blocker, clearable, lanes } from "./worktreepanel/view";

interface Said {
  kind?: string;
  what?: string;
  paths?: string[];
  path?: string;
  into?: string;
}

// WorktreeGraph draws the worktrees by what they hold: a lane each, an arc
// per relation, commits behind a lane you open. The tree is the glance.
export class WorktreeGraph implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private client?: Client;
  private graph?: Overlap;
  private busy = false;
  private note = "";

  constructor(private context: vscode.ExtensionContext) {}

  dispose(): void {
    this.panel?.dispose();
  }

  serving(client: Client | undefined): void {
    this.client = client;
    if (!client) this.graph = undefined;
    this.hand();
  }

  // stale marks the drawing old without reading: the panel is only worth a
  // scan of every worktree while someone is looking at it.
  stale(): void {
    if (this.panel?.visible) void this.read();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.worktreeGraph",
        "ade · Worktrees",
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
      if (said.kind === "do" && said.what === "refresh") void this.read();
      if (said.kind === "do" && said.what === "consolidate")
        void this.consolidate(said.paths ?? []);
      if (said.kind === "do" && said.what === "merge")
        void this.merge(said.path ?? "", said.into ?? "");
    });
  }

  private async read(): Promise<void> {
    const c = this.client;
    if (!c) {
      this.note = "Nothing is running for this folder yet.";
      this.hand();
      return;
    }
    this.busy = true;
    this.note = "";
    this.hand();
    try {
      this.graph = await c.overlap();
    } catch (err) {
      this.note = String(err);
    } finally {
      this.busy = false;
      this.hand();
    }
  }

  // consolidate clears each blocker, then removes them all in one prune.
  // Serially, and a failed blocker drops only its own row.
  private async consolidate(paths: string[]): Promise<void> {
    const c = this.client;
    if (!c || paths.length === 0) return;
    const rows = lanes(this.graph).filter(
      (row) => paths.includes(row.node.path) && clearable(row),
    );
    if (rows.length === 0) return;

    this.busy = true;
    this.note = "";
    this.hand();

    const going: string[] = [];
    const left: string[] = [];
    for (const row of rows) {
      const clear = blocker(row)?.clear;
      if (!clear) {
        going.push(row.node.path);
        continue;
      }
      try {
        if (clear === "push") await c.pushWorktree(row.node.path);
        else await c.closeWorktree(row.node.path);
        going.push(row.node.path);
      } catch (err) {
        left.push(`${row.label}: ${String(err)}`);
      }
    }

    let said = "";
    if (going.length) {
      try {
        const out = await c.prune(going, false);
        said = report(out.removed, out.kept);
      } catch (err) {
        left.push(String(err));
      }
    }
    const told = [said, ...left].filter(Boolean).join(" ");
    this.busy = false;
    // The re-read clears the note, so the report is set after it rather than
    // before: what just happened is the one thing worth keeping on screen.
    await this.read();
    this.note = told;
    this.hand();
    if (told) vscode.window.setStatusBarMessage(`ade: ${told}`, 6000);
  }

  // merge lands a branch on the rail it left, then drops the checkout, which
  // is only spare once the merge has landed. A refusal removes nothing.
  private async merge(path: string, into: string): Promise<void> {
    const c = this.client;
    if (!c || !path || !into) return;
    this.busy = true;
    this.note = "";
    this.hand();

    let told = "";
    try {
      const out = await c.mergeWorktree(path, into);
      const gone = await c.prune([path], false);
      // Named, and never only in a status message that fades: a merge a model
      // resolved is one somebody has to read before it is pushed.
      const how = out.resolved?.length
        ? ` ${out.by ?? "a model"} resolved conflicts in ${out.resolved.join(", ")} — read the merge before pushing.`
        : "";
      told = gone.removed.length
        ? `Merged ${out.merged} into ${out.into} and removed it.${how}`
        : `Merged ${out.merged} into ${out.into}. ${report(gone.removed, gone.kept)}${how}`;
    } catch (err) {
      told = String(err);
    }
    this.busy = false;
    await this.read();
    this.note = told;
    this.hand();
    vscode.window.setStatusBarMessage(`ade: ${told}`, 6000);
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      overlap: this.graph,
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
    <link rel="stylesheet" href="${asset("media", "worktrees.css")}" />
  </head>
  <body><main id="worktrees"></main>
    <script nonce="${nonce}" src="${asset("dist", "worktrees.js")}"></script>
  </body>
</html>`;
  }
}
