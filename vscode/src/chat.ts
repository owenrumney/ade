import * as vscode from "vscode";
import type { Server } from "./discover";
import { openable } from "./chat/links";
import { pick } from "./pick";
import {
  MAX_BYTES,
  MAX_FILES,
  mediaOf,
  type Incoming,
} from "./chat/files";
import { watch } from "./launcher";
import { machineWide } from "./remote";
import { body } from "./chat/body";

// Said is what the page sends: one of ade's routes, or a plain intent.
interface Said {
  kind?: string;
  id?: string;
  path?: string;
  method?: string;
  body?: string;
  href?: string;
  text?: string;
  paths?: string[];
  devices?: { id: string; label: string }[];
  chosen?: string;
}

// streamRetry is how long a dropped event stream waits before reopening.
const streamRetry = 2000;

const MIC_KEY = "ade.chat.mic";

// Chat is ade's conversation as a webview of its own, not a frame around ade's
// page. A nested cross-origin frame never receives a pasted image.
export class Chat implements vscode.Disposable, vscode.WebviewViewProvider {
  private held?: vscode.WebviewPanel;
  // The conversation in its own container, which you can drag to the secondary
  // sidebar: full height, and not an editor group anything can open into.
  private side?: vscode.WebviewView;
  private server?: Server;
  private closed = false;
  private watching?: AbortController;
  // The server the open stream was started for.
  private listening?: Server;
  // Where the page should fetch from, and which base it was resolved for.
  private reach?: string;
  private reachOf?: string;
  // A focus asked for before the page could hear it.
  private wanted = false;

  // desk makes this the concierge rather than the session.s chat: another
  // stylesheet, a header that names it, and no talk of this repository.
  constructor(
    private context: vscode.ExtensionContext,
    private desk = false,
  ) {}

  dispose(): void {
    this.watching?.abort();
    this.held?.dispose();
  }

  // onShown fires when the view is drawn and whenever it comes back into
  // sight. Opening a container is not a command: it is what starts the desk.
  onShown?: () => void;

  // VS Code owns the view's lifetime: never created, never disposed here.
  resolveWebviewView(view: vscode.WebviewView): void {
    this.side = view;
    view.webview.options = {
      enableScripts: true,
      localResourceRoots: [this.context.extensionUri],
    };
    view.onDidDispose(() => (this.side = undefined));
    view.onDidChangeVisibility(() => {
      if (view.visible) this.onShown?.();
    });
    this.wire(view.webview);
    this.draw(view.webview);
    this.onShown?.();
  }

  // revive takes back a panel VS Code restored across a reload. Without this
  // the tab comes back and nothing ever writes its html, so it stays blank.
  revive(panel: vscode.WebviewPanel): void {
    this.held?.dispose();
    this.held = panel;
    panel.webview.options = {
      enableScripts: true,
      localResourceRoots: [this.context.extensionUri],
    };
    panel.onDidDispose(() => (this.held = undefined));
    this.wire(panel.webview);
    this.draw(panel.webview);
  }

  // The conversation lives in its own container and nowhere else. Focusing it
  // resolves the view, so a window that has never opened it needs no tab.
  async reveal(server: Server | undefined): Promise<void> {
    this.server = server;
    if (this.desk) {
      this.show(server);
      return;
    }
    try {
      await vscode.commands.executeCommand("ade.chat.focus");
    } catch {
      await vscode.commands.executeCommand(
        "workbench.view.extension.ade-chat",
      );
    }
  }

  // focus is reveal with the caret in the composer. A webview's contents are
  // its own, so the page does the focusing and this only asks.
  async focus(server: Server | undefined): Promise<void> {
    const already = this.side?.webview ?? this.held?.webview;
    await this.reveal(server);
    if (already) void already.postMessage({ kind: "focus" });
    // Nothing was drawn yet, so the page is not listening; ready flushes it.
    else this.wanted = true;
  }

  // dictate is the chord. Every open page hears it; the one with focus acts.
  dictate(): void {
    for (const page of [this.side?.webview, this.held?.webview]) {
      void page?.postMessage({ kind: "dictate" });
    }
  }

  show(server: Server | undefined): void {
    if (!this.held) {
      this.held = vscode.window.createWebviewPanel(
        this.desk ? "ade.concierge" : "ade.chat",
        this.desk ? "Concierge" : "ade · Chat",
        // Not Active: that opens over the document you wanted to discuss.
        vscode.ViewColumn.Beside,
        {
          enableScripts: true,
          retainContextWhenHidden: true,
          localResourceRoots: [this.context.extensionUri],
        },
      );
      this.held.onDidDispose(() => (this.held = undefined));
      void this.lock();
      this.wire(this.held.webview);
    }
    this.server = server;
    this.draw(this.held.webview);
    this.held.reveal(this.held.viewColumn, false);
  }

  // wire is what the page may ask of the editor. The token is posted rather
  // than written into the html, which any window's devtools can read.
  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage(
      (said: Said) => {
        if (said?.kind === "ready") {
          this.hand();
          this.listen(webview);
          if (this.wanted) {
            this.wanted = false;
            void webview.postMessage({ kind: "focus" });
          }
        }
        if (said?.kind === "api" && said.id) void this.relay(webview, said);
        if (said?.kind === "drop" && said.id) void this.haul(webview, said);
        // Gated here as well as in the page: the renderer sets innerHTML from
        // the agent's markdown, so it is the wrong place for the only check.
        if (said?.kind === "link" && said.href && openable(said.href)) {
          void vscode.env.openExternal(vscode.Uri.parse(said.href));
        }
        if (said?.kind === "mics") void this.chooseMic(said);
        if (said?.kind === "copy" && said.text) {
          void vscode.env.clipboard.writeText(said.text);
        }
        if (said?.kind === "open" && said.path) {
          void vscode.commands.executeCommand(
            "vscode.open",
            vscode.Uri.file(said.path),
          );
        }
      },
    );
  }

  // haul reads what was dropped. A drag out of the editor's tree carries paths
  // and no bytes, and the page cannot reach the disk.
  private async haul(webview: vscode.Webview, said: Said): Promise<void> {
    const back = (answer: Record<string, unknown>) =>
      void webview.postMessage({ kind: "api.result", id: said.id, ...answer });
    const files: Incoming[] = [];
    try {
      for (const path of (said.paths || []).slice(0, MAX_FILES)) {
        const at = vscode.Uri.parse(path);
        const name = at.path.split("/").pop() || "attachment";
        const stat = await vscode.workspace.fs.stat(at);
        if (stat.type === vscode.FileType.Directory) continue;
        if (stat.size > MAX_BYTES) {
          back({ ok: false, error: `${name} is over ${MAX_BYTES >> 20}MB` });
          return;
        }
        const bytes = await vscode.workspace.fs.readFile(at);
        files.push({
          name,
          mediaType: mediaOf(name),
          data: Buffer.from(bytes).toString("base64"),
        });
      }
    } catch (err) {
      back({ ok: false, error: String(err) });
      return;
    }
    back({ ok: true, text: JSON.stringify({ files }) });
  }

  // relay makes the page's request from here. A webview renders on the machine
  // you are sitting at, which is not where ade is in a remote window.
  private async relay(webview: vscode.Webview, said: Said): Promise<void> {
    const server = this.server;
    const back = (answer: Record<string, unknown>) =>
      void webview.postMessage({ kind: "api.result", id: said.id, ...answer });
    if (this.closed) {
      back({ status: 0, error: "the session is closed." });
      return;
    }
    if (!server || !said.path?.startsWith("/api/")) {
      back({ status: 0, error: "ade is not running for this folder." });
      return;
    }
    try {
      const res = await fetch(`${server.base}${said.path}`, {
        method: said.method ?? "GET",
        headers: {
          "X-Ade-Token": server.token,
          "Content-Type": "application/json",
        },
        body: said.body,
      });
      const text = await res.text();
      back({ status: res.status, ok: res.ok, text });
    } catch (err) {
      back({ status: 0, error: String(err) });
    }
  }

  // One stream per server, not per page: the view and the panel are open at
  // once, and a per-page stream left whichever said ready first stale.
  private listen(joined?: vscode.Webview): void {
    const server = this.server;
    if (!server) return;
    if (this.watching && !this.watching.signal.aborted && this.listening === server) {
      void joined?.postMessage({ kind: "stream", open: true });
      return;
    }
    this.watching?.abort();
    const stop = new AbortController();
    this.watching = stop;
    this.listening = server;
    void (async () => {
      for (;;) {
        try {
          this.tell({ kind: "stream", open: true });
          await watch(server, (name) => {
            this.tell({ kind: "event", name });
          }, stop.signal);
        } catch {
          // Dropped or refused: the wait below is the retry.
        }
        if (stop.signal.aborted || this.server !== server) return;
        this.tell({ kind: "stream", open: false });
        await new Promise((done) => setTimeout(done, streamRetry));
        if (stop.signal.aborted || this.server !== server) return;
      }
    })();
  }

  private tell(message: unknown): void {
    for (const webview of this.webviews()) void webview.postMessage(message);
  }

  // lock stops anything else opening into the conversation's group.
  private async lock(): Promise<void> {
    if (!this.held) return;
    this.held.reveal(this.held.viewColumn, false);
    await vscode.commands.executeCommand("workbench.action.lockEditorGroup");
  }

  // closing is a session exited on purpose, so a failed poll is expected.
  closing(): void {
    this.closed = true;
    this.watching?.abort();
    this.listening = undefined;
    for (const webview of this.webviews())
      void webview.postMessage({ kind: "closed" });
  }

  // Rewriting the html throws away a typed draft and any held images, so it
  // happens only when the page is on the wrong server, or on none.
  get served(): Server | undefined {
    return this.server;
  }

  serving(server: Server | undefined): void {
    const was = this.server;
    this.server = server;
    if (server) this.closed = false;
    if (was?.base === server?.base) {
      this.hand();
      this.listen();
      return;
    }
    for (const webview of this.webviews()) this.draw(webview);
  }

  // draw writes the page once its address is known. Every entry point goes
  // through here: the address is resolved and the html cannot be built first.
  private draw(webview: vscode.Webview): void {
    const server = this.server;
    this.write(
      webview,
      server,
      this.reachOf === server?.base ? this.reach : undefined,
    );
    if (!server || this.reachOf === server.base) return;
    // Forwarding is resolved after the first paint, never before it: on a
    // remote window asExternalUri can take a while, or never answer.
    void this.where().then((reach) => {
      if (this.server === server && reach !== server.base)
        this.write(webview, server, reach);
    });
  }

  private write(
    webview: vscode.Webview,
    server: Server | undefined,
    reach: string | undefined,
  ): void {
    try {
      webview.html = this.page(webview, server, reach);
    } catch (err) {
      // A blank pane says nothing at all, which is the one state that cannot
      // be diagnosed from the outside. Always leave something readable.
      webview.html = broken(String(err));
    }
  }

  // where is what the page can reach. A webview renders locally even when the
  // editor is on another machine, so ade's own address may resolve nowhere.
  private async where(): Promise<string | undefined> {
    const server = this.server;
    if (!server) return undefined;
    if (this.reachOf === server.base) return this.reach;
    let reach = server.base;
    try {
      const at = await vscode.env.asExternalUri(vscode.Uri.parse(server.base));
      reach = at.toString().replace(/\/$/, "");
    } catch {
      // No forwarding to be had: the page is no worse off than before.
    }
    this.reachOf = server.base;
    this.reach = reach;
    return reach;
  }

  private webviews(): vscode.Webview[] {
    return [this.held?.webview, this.side?.webview].filter(
      (w): w is vscode.Webview => Boolean(w),
    );
  }

  // hand gives the page the credential it asked for. Nothing to send when
  // no ade is serving: the page is the offline one and fetches nothing.
  private hand(): void {
    if (!this.server) return;
    for (const webview of this.webviews()) {
      void webview.postMessage({ kind: "mic", id: this.mic() });
      void webview.postMessage({ kind: "token", token: this.server.token });
    }
  }

  private mic(): string {
    return this.context.globalState.get<string>(MIC_KEY) ?? "";
  }

  // The page lists the devices because only it has the media API; the pick is
  // the editor's, because the omnibox is where anything is put to a person.
  private async chooseMic(said: Said): Promise<void> {
    const devices = said.devices ?? [];
    if (devices.length === 0) {
      void vscode.window.showWarningMessage("ade: no microphone to choose from");
      return;
    }
    const rows = [
      { label: "System default", id: "" },
      ...devices.map((d) => ({ label: d.label, id: d.id })),
    ].map((row) => ({
      ...row,
      description: row.id === (said.chosen ?? "") ? "in use" : undefined,
    }));
    const got = (await pick(rows, { title: "Dictate from" })) as
      | { id: string }
      | undefined;
    if (!got) return;
    await this.context.globalState.update(MIC_KEY, got.id);
    this.tell({ kind: "mic", id: got.id });
  }

  private page(
    webview: vscode.Webview,
    server: Server | undefined,
    reach: string | undefined,
  ): string {
    if (!server) return this.desk ? waking() : offline();
    const asset = (...bits: string[]) =>
      webview.asWebviewUri(
        vscode.Uri.joinPath(this.context.extensionUri, ...bits),
      );
    const nonce = Math.random().toString(36).slice(2);
    const base = reach ?? server.base;
    // A forwarded address may be on any host, so the policy has to name it.
    const origin = new URL(base).origin;

    // connect-src is what a first-party webview needs and an iframe could not
    // have: this page fetches and streams from ade itself.
    return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy" content="default-src 'none';
      style-src ${webview.cspSource};
      script-src 'nonce-${nonce}' ${webview.cspSource};
      img-src ${webview.cspSource} ${origin} http://127.0.0.1:* http://localhost:* data:;
      connect-src ${origin} http://127.0.0.1:* http://localhost:*;" />
    <link rel="stylesheet" href="${asset("media", "chat.css")}" />
    ${this.desk ? `<link rel="stylesheet" href="${asset("media", "desk.css")}" />` : ""}
  </head>
  <body>
    ${body({ base, mic: String(asset("media", "mic.js")), desk: this.desk })}
    <script nonce="${nonce}" src="${asset("dist", "chat.js")}"></script>
  </body>
</html>`;
  }
}

function broken(why: string): string {
  return shell(
    `ade could not draw the conversation.<br /><br /><code>${escape(why)}</code>`,
  );
}

function offline(): string {
  // A window on another machine does not start ade, so "not running" alone
  // reads as something you could fix from here.
  return shell(
    machineWide(vscode.env.remoteName)
      ? "ade is not running for this folder."
      : "Waiting for ade on this machine. It is started over there, not from this window.",
  );
}

// waking is the desk before its session answers. It is started by opening the
// view, so this is a wait and not a fault.
function waking(): string {
  return shell("Opening the front desk…");
}

// shell is the plain page both the empty states use.
function shell(body: string): string {
  return `<!doctype html>
<html>
  <head><meta charset="utf-8" /></head>
  <body style="font: 13px var(--vscode-font-family); color: var(--vscode-descriptionForeground); padding: 12px">
    ${body}
  </body>
</html>`;
}

function escape(text: string): string {
  return text.replace(/[&<>]/g, (c) => `&#${c.charCodeAt(0)};`);
}
