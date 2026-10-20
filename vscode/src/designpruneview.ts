import * as vscode from "vscode";
import {
  asked,
  chosen,
  confirm,
  rows,
  told,
  type Group,
  type Prunable,
  type Row,
  type State,
  type WikiState,
} from "./designprunepanel/view";
import { brief } from "./purgeview";
import { WIKI } from "./wiki";

interface Said {
  kind?: string;
  what?: string;
  keys?: string[];
  page?: string;
}

// Archived is what the launcher answers for one document.
export interface Archived {
  file?: string;
  page?: string;
  state?: string;
  commit?: string;
}

// DesignPrunePage is the finished designs of every repository the launcher
// holds. It drives the run itself, one document at a time, and nothing streams.
export class DesignPrunePage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private groups: Group[] = [];
  private wiki?: WikiState;
  private states: Record<string, { state: State; page?: string; why?: string }> = {};
  private busy = false;
  private running = false;
  private stopping = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private repos: () => Promise<{ repos: { repo: string }[]; wiki?: WikiState }>,
    private sweep: (repo: string) => Promise<Prunable>,
    private archive: (repo: string, file: string) => Promise<Archived>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.purgeDesigns",
        "ade · Purge design documents",
        vscode.ViewColumn.Active,
        {
          enableScripts: true,
          retainContextWhenHidden: true,
          localResourceRoots: [this.context.extensionUri],
        },
      );
      this.panel.onDidDispose(() => {
        // Closing stops the run the same way the footer does; it costs you
        // the failure list, which is why the panel stays open on its own.
        this.stopping = true;
        this.panel = undefined;
      });
      this.wire(this.panel.webview);
      this.draw();
    }
    this.panel.reveal(this.panel.viewColumn, false);
    // Nothing is cached and there is no Refresh: a stale list is how a
    // document whose branch just reopened gets archived.
    await this.read();
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind !== "do") return;
      if (said.what === "stop") this.stopping = true;
      if (said.what === "prune") void this.prune(said.keys ?? []);
      if (said.what === "open" && said.page) void openPage(said.page);
    });
  }

  // read lists the repositories, then sweeps them one at a time, drawing each
  // group as it lands. One hanging repository hangs its own row.
  private async read(): Promise<void> {
    this.busy = true;
    this.note = "";
    this.states = {};
    this.groups = [];
    this.hand();
    try {
      const held = await this.repos();
      this.wiki = held.wiki;
      this.groups = held.repos.map((repo) => ({ repo: repo.repo, pending: true }));
    } catch (err) {
      this.note = brief(err);
      this.busy = false;
      this.hand();
      return;
    }
    this.hand();
    for (const group of this.groups) {
      try {
        group.answer = await this.sweep(group.repo);
        if (group.answer.wiki) this.wiki = group.answer.wiki;
      } catch (err) {
        // The call itself only fails if the launcher is unreachable; a
        // repository that cannot be swept answers with its own reason.
        group.answer = { repo: group.repo, error: brief(err) };
      }
      group.pending = false;
      this.hand();
    }
    this.busy = false;
    this.hand();
  }

  // prune asks once, then publishes each document in turn. A failure leaves
  // that file alone and the loop carries on to the next.
  private async prune(keys: string[]): Promise<void> {
    const picked = chosen(rows(this.groups), new Set(keys));
    if (picked.length === 0 || this.running) return;
    const yes = await vscode.window.showWarningMessage(
      asked(picked),
      { modal: true, detail: confirm(picked) },
      "Archive",
    );
    if (yes !== "Archive") return;

    this.running = true;
    this.stopping = false;
    this.note = "";
    for (const row of picked) this.states[row.key] = { state: "queued" };
    this.hand();

    let done = 0;
    let failed = 0;
    for (const row of picked) {
      if (this.stopping) break;
      this.states[row.key] = { state: "distilling" };
      this.hand();
      try {
        const out = await this.archive(row.repo, row.file);
        this.states[row.key] = { state: "published", page: out.page };
        done += 1;
        await closeTab(row);
      } catch (err) {
        this.states[row.key] = { state: "failed", why: brief(err) };
        failed += 1;
      }
      this.hand();
    }

    const stopped = this.stopping && done + failed < picked.length;
    this.running = false;
    this.stopping = false;
    this.note = told(done, failed, stopped);
    this.hand();
    if (done > 0) vscode.window.setStatusBarMessage(`ade: ${this.note}`, 6000);
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      groups: this.groups,
      wiki: this.wiki,
      busy: this.busy,
      running: this.running,
      note: this.note,
      states: this.states,
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
    <link rel="stylesheet" href="${asset("media", "designprune.css")}" />
  </head>
  <body><main id="prune"></main>
    <script nonce="${nonce}" src="${asset("dist", "designprune.js")}"></script>
  </body>
</html>`;
  }
}

// closeTab shuts any editor open on an archived document. A tab on a file
// that no longer exists only gets saved back by accident.
async function closeTab(row: Row): Promise<void> {
  const path = `${row.dir}/${row.file}`;
  const open = vscode.window.tabGroups.all
    .flatMap((group) => group.tabs)
    .filter((tab) => {
      const input = tab.input as { uri?: vscode.Uri } | undefined;
      return input?.uri?.fsPath === path;
    });
  if (open.length > 0) await vscode.window.tabGroups.close(open);
}

// openPage shows the wiki page a document became, through the same read-only
// scheme the wiki search opens.
async function openPage(page: string): Promise<void> {
  const doc = await vscode.workspace.openTextDocument(
    vscode.Uri.from({ scheme: WIKI, path: "/" + page }),
  );
  await vscode.window.showTextDocument(doc, { preview: true });
}
