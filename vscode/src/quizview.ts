import * as vscode from "vscode";
import type { Client, State } from "./client";
import type { Quiz } from "./quizpanel/view";

// Said is what the page asks for. Nothing about how you did comes back: the
// score stays in the webview and is never posted, stored or shown elsewhere.
interface Said {
  kind?: string;
  what?: string;
  file?: string;
  line?: number;
}

// QuizPage asks you up to five questions about the branch, marks them, and
// shows you the hunk behind each one. A confidence check, not a gate.
export class QuizPage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private client?: Client;
  private quiz?: Quiz;
  private busy = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private root: () => string | undefined,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  serving(client: Client | undefined): void {
    this.client = client;
    if (!client) this.quiz = undefined;
    this.hand();
  }

  // apply keeps it in step with the run: the questions arrive on a later
  // /api/state, the way the commit draft does.
  apply(state: State): void {
    if (state.quiz) {
      this.quiz = state.quiz;
      this.busy = false;
    }
    this.hand();
  }

  async show(): Promise<void> {
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.quiz",
        "ade · Quiz me",
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
    await this.ask(false);
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage((said: Said) => {
      if (said.kind === "ready") this.hand();
      if (said.kind !== "do") return;
      if (said.what === "regenerate") void this.ask(true);
      if (said.what === "cancel") void this.cancel();
      if (said.what === "open") void this.open(said.file ?? "", said.line ?? 1);
    });
  }

  // ask wants the questions. A quiz already written for this diff comes back
  // in the answer; otherwise it is accepted and arrives on a later state.
  private async ask(regenerate: boolean): Promise<void> {
    const c = this.client;
    if (!c) {
      this.failed("nothing is running for this folder yet");
      return;
    }
    this.note = "";
    this.busy = true;
    if (regenerate) this.quiz = undefined;
    this.hand();
    try {
      const said = await c.quiz(regenerate);
      if (said.quiz) {
        this.quiz = said.quiz;
        this.busy = false;
      }
    } catch (err) {
      this.failed(String(err));
      return;
    }
    this.hand();
  }

  // Cancel stops the run and closes the panel: an empty panel is not what you
  // opened it for.
  private async cancel(): Promise<void> {
    try {
      await this.client?.cancel();
    } catch (err) {
      this.failed(String(err));
      return;
    }
    this.panel?.dispose();
  }

  private async open(file: string, line: number): Promise<void> {
    const root = this.root();
    if (!file || !root) return;
    try {
      const doc = await vscode.workspace.openTextDocument(
        vscode.Uri.joinPath(vscode.Uri.file(root), file),
      );
      const editor = await vscode.window.showTextDocument(doc, {
        preview: true,
        viewColumn: vscode.ViewColumn.Beside,
      });
      const at = new vscode.Position(Math.max(0, line - 1), 0);
      editor.revealRange(
        new vscode.Range(at, at),
        vscode.TextEditorRevealType.InCenter,
      );
      editor.selection = new vscode.Selection(at, at);
    } catch (err) {
      this.failed(String(err));
    }
  }

  private failed(why: string): void {
    this.busy = false;
    this.note = why;
    this.hand();
  }

  private hand(): void {
    void this.panel?.webview.postMessage({
      kind: "state",
      quiz: this.quiz,
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
    <link rel="stylesheet" href="${asset("media", "quiz.css")}" />
  </head>
  <body><main id="quiz"></main>
    <script nonce="${nonce}" src="${asset("dist", "quiz.js")}"></script>
  </body>
</html>`;
  }
}
