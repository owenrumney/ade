import * as vscode from "vscode";
import { byRepo, chosen, confirm, rows, told, type Sweep } from "./purgepanel/view";

interface Said {
  kind?: string;
  what?: string;
  paths?: string[];
  force?: boolean;
}

export interface Pruned {
  removed?: string[];
  kept?: { path: string; why: string }[];
}

// brief is an error as one line of a note. A dead port answers with a whole
// HTML page, and the panel shows what it is given.
export function brief(err: unknown): string {
  const first = String(err).split("\n")[0].trim();
  return first.length > 200 ? `${first.slice(0, 200)}…` : first;
}

// PurgePage is every worktree on this machine, grouped by repository, with
// what stands in the way of each. The launcher sweeps; this draws and asks.
export class PurgePage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private sweep?: Sweep;
  private busy = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private feed: (fresh: boolean) => Promise<Sweep>,
    private prune: (repo: string, paths: string[], force: boolean) => Promise<Pruned>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.purge",
        "ade · Purge worktrees",
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
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind === "do" && said.what === "refresh") void this.read(true);
      if (said.kind === "do" && said.what === "remove")
        void this.remove(said.paths ?? [], Boolean(said.force));
    });
  }

  private async read(fresh = false): Promise<void> {
    this.busy = true;
    this.hand();
    try {
      this.sweep = await this.feed(fresh);
      this.note = "";
    } catch (err) {
      this.note = brief(err);
    } finally {
      this.busy = false;
      this.hand();
    }
  }

  // remove asks first, then prunes one repository at a time, so a failure in
  // one leaves the rest done. The server rescans, so a stale tick is refused.
  private async remove(paths: string[], force: boolean): Promise<void> {
    const picked = chosen(rows(this.sweep, force), new Set(paths));
    if (picked.length === 0) return;
    const yes = await vscode.window.showWarningMessage(
      `Remove ${picked.length} worktree${picked.length === 1 ? "" : "s"}?`,
      { modal: true, detail: confirm(picked) },
      "Remove",
    );
    if (yes !== "Remove") return;

    this.busy = true;
    this.note = "";
    this.hand();

    const removed: string[] = [];
    const kept: { path: string; why: string }[] = [];
    for (const group of byRepo(picked)) {
      try {
        const out = await this.prune(group.repo, group.paths, force);
        removed.push(...(out.removed ?? []));
        kept.push(...(out.kept ?? []));
      } catch (err) {
        for (const path of group.paths) kept.push({ path, why: brief(err) });
      }
    }

    const said = told(removed, kept);
    // After the re-read, which clears the note. A re-read that failed keeps
    // its own: the table under this message is then the one from before.
    await this.read(true);
    this.note = this.note ? `${said} ${this.note}` : said;
    this.hand();
    if (said) vscode.window.setStatusBarMessage(`ade: ${said}`, 6000);
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      sweep: this.sweep,
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
    <link rel="stylesheet" href="${asset("media", "purge.css")}" />
  </head>
  <body><main id="purge"></main>
    <script nonce="${nonce}" src="${asset("dist", "purge.js")}"></script>
  </body>
</html>`;
  }
}
