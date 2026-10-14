import * as vscode from "vscode";
import type { Client, State } from "./client";
import { split } from "./commit";

// Said is what the page asks for: verbs the source control view already had.
interface Said {
  kind?: string;
  what?: string;
  paths?: string[];
  message?: string;
  name?: string;
  push?: boolean;
}

// CommitView is staging, the message and what follows, in one panel.
export class CommitView implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private client?: Client;
  private state?: State;
  private busy = "";
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private after: (what: string) => Promise<void>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  serving(client: Client | undefined): void {
    this.client = client;
    if (!client) this.state = undefined;
    this.hand();
  }

  // apply keeps it in step without redrawing: a half-typed message survives.
  apply(state: State): void {
    this.state = state;
    this.hand();
  }

  show(): void {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.commit",
        "ade · Commit",
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
    this.hand();
    void this.open();
  }

  // open is what the panel does for you on arrival: everything in, and a
  // message for it. A draft already written for that set is left alone.
  private async open(): Promise<void> {
    const c = this.client;
    const tree = this.state?.worktree;
    if (!c) return;
    const waiting = (tree?.unstaged ?? []).map((one) => one.path);
    try {
      if (waiting.length > 0) await c.stage(waiting);
    } catch (err) {
      this.failed(String(err));
      return;
    }
    const was = [...(this.state?.draft?.staged ?? [])].sort().join("\n");
    const now = [...(tree?.staged ?? []), ...waiting].sort().join("\n");
    if (was !== "" && was === now) {
      await this.after("open");
      return;
    }
    await this.run({ kind: "do", what: "redraft" });
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
    this.hand();
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind === "do") void this.run(said);
    });
  }

  private async run(said: Said): Promise<void> {
    const c = this.client;
    if (!c) return;
    this.note = "";
    try {
      switch (said.what) {
        case "stage":
          await c.stage(said.paths ?? []);
          break;
        case "unstage":
          await c.unstage(said.paths ?? []);
          break;
        case "branch":
          await this.branch(c, said.name ?? "");
          return;
        case "redraft":
          this.working("redraft");
          await c.writeCommitMessage();
          break;
        case "commit":
          await this.commit(c, said);
          return;
        case "createPR":
          this.working("createPR");
          await c.createPR();
          break;
        case "showPR":
          await vscode.commands.executeCommand("ade.showPR");
          return;
        case "rebase":
          await this.rebase(c);
          return;
        default:
          return;
      }
    } catch (err) {
      this.failed(String(err));
      return;
    }
    await this.after(said.what ?? "");
  }

  // branch moves this checkout onto a new one, uncommitted work and all, so
  // the commit lands somewhere the base branch can merge from.
  private async branch(c: Client, name: string): Promise<void> {
    if (!name.trim()) {
      this.failed("name the branch");
      return;
    }
    this.working("branch");
    try {
      await c.newBranch(name.trim());
    } catch (err) {
      this.failed(String(err));
      return;
    }
    this.busy = "";
    this.note = `On ${name.trim()}.`;
    await this.after("branch");
  }

  // rebase replays the branch on a freshly fetched base. The server refuses
  // over uncommitted work and undoes a conflict, so both are a note here.
  private async rebase(c: Client): Promise<void> {
    this.working("rebase");
    let done: { base: string; behind: number; replayed: number };
    try {
      done = await c.rebaseBase();
    } catch (err) {
      this.failed(String(err));
      return;
    }
    this.busy = "";
    this.note = done.behind
      ? `Replayed ${done.replayed} commit(s) on ${done.base}.`
      : `Already on top of ${done.base}.`;
    await this.after("rebase");
  }

  private async commit(c: Client, said: Said): Promise<void> {
    const { title, body } = split(said.message ?? "");
    if (!title) {
      this.failed("the message needs a title");
      return;
    }
    this.working("commit");
    try {
      await c.commit(title, body, false);
      if (said.push) await c.push(false);
    } catch (err) {
      this.failed(String(err));
      return;
    }
    this.busy = "";
    this.note = said.push ? "Committed and pushed." : "Committed.";
    await this.after("commit");
  }

  private working(what: string): void {
    this.busy = what;
    this.hand();
  }

  private failed(why: string): void {
    this.busy = "";
    this.note = why;
    this.hand();
  }

  private hand(): void {
    const state = this.state;
    void this.panel?.webview.postMessage({
      kind: "state",
      tree: state?.worktree ?? undefined,
      draft: state?.draft ?? undefined,
      hasPR: Boolean(state?.branchPr),
      ahead: state?.worktree?.ahead ?? 0,
      behind: state?.worktree?.behind ?? 0,
      base: state?.meta?.base ?? "",
      branch: state?.meta?.branch ?? "",
      busy: this.busy,
      note: this.note,
    });
    if (this.busy === "redraft" && state?.draft) this.busy = "";
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
    <link rel="stylesheet" href="${asset("media", "commit.css")}" />
  </head>
  <body><main id="commit"></main>
    <script nonce="${nonce}" src="${asset("dist", "commit.js")}"></script>
  </body>
</html>`;
  }
}
