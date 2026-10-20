import * as vscode from "vscode";
import { plan } from "./newsession/plan";
import { fresh, held, type Chosen, type Doc } from "./newsession/view";

// Offer is everything the screen can pick from. Read once per open: a
// repository list is a scan, and the screen is not a watcher.
export interface Offer {
  machines: { name: string; label: string }[];
  repos: { path: string; name: string; owner?: string; branch?: string }[];
  agents: { id: string; label: string }[];
  models: { id: string; label?: string }[];
  roles: string[];
  // designs are home's documents, re-read when home changes. designNote is
  // why the list is empty when the read failed rather than found nothing.
  designs: Doc[];
  designNote?: string;
  recent: string[];
}

export const nothing: Offer = {
  machines: [],
  repos: [],
  agents: [],
  models: [],
  roles: [],
  designs: [],
  recent: [],
};

// NewSessionPage is the six ways into a session on one surface, with what
// each would create listed before you commit to it.
export class NewSessionPage implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private offer: Offer = nothing;
  private chosen: Chosen = { ...fresh };
  private busy = false;
  private note = "";

  constructor(
    private context: vscode.ExtensionContext,
    private feed: (machine: string) => Promise<Offer>,
    private documents: (machine: string, dir: string) => Promise<Doc[]>,
    private start: (chosen: Chosen) => Promise<void>,
  ) {}

  dispose(): void {
    this.panel?.dispose();
  }

  async show(seed?: Partial<Chosen>): Promise<void> {
    this.chosen = { ...fresh, ...seed };
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "ade.newsession",
        "New session",
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
    await this.read(this.chosen.machine);
  }

  // listDesigns re-reads home's documents. A document picked in the repository
  // that is no longer home is dropped: it was never this session's to carry on.
  private async listDesigns(): Promise<void> {
    this.offer = { ...this.offer, ...(await this.documentsIn()) };
    this.holdDesign();
    this.hand();
  }

  // A document the list does not have is dropped, so the box never shows one
  // choice while another is held. A list that could not be read drops nothing.
  private holdDesign(): void {
    const { designs, designNote } = this.offer;
    if (designNote || !this.chosen.design) return;
    if (!held(designs, this.chosen))
      this.chosen = { ...this.chosen, design: "", ship: false };
  }

  // A read that failed is said, not counted as none. An old launcher with no
  // such route answers 404, and "no documents yet" is a lie that costs an hour.
  private async documentsIn(): Promise<Pick<Offer, "designs" | "designNote">> {
    const dir = this.chosen.repos[0];
    if (!dir) return { designs: [], designNote: undefined };
    try {
      return { designs: await this.documents(this.chosen.machine, dir) };
    } catch (err) {
      return { designs: [], designNote: `Could not list them: ${String(err)}` };
    }
  }

  private wire(webview: vscode.Webview): void {
    webview.onDidReceiveMessage(
      (said: { kind?: string; chosen?: Chosen }) => {
        if (said.kind === "ready") this.hand();
        if (said.kind === "change" && said.chosen) {
          const was = this.chosen;
          this.chosen = said.chosen;
          if (this.chosen.machine !== was.machine) {
            void this.read(this.chosen.machine);
            return;
          }
          // Home names which repository's documents the Design row lists.
          if (this.chosen.repos[0] !== was.repos[0]) {
            void this.listDesigns();
            return;
          }
          void this.panel?.webview.postMessage({
            kind: "plan",
            plan: plan(this.chosen),
          });
        }
        if (said.kind === "start" && said.chosen) {
          this.chosen = said.chosen;
          void this.run();
        }
      },
    );
  }

  private async run(): Promise<void> {
    this.busy = true;
    this.hand();
    try {
      await this.start(this.chosen);
      this.panel?.dispose();
    } catch (err) {
      this.note = String(err);
    } finally {
      this.busy = false;
      this.hand();
    }
  }

  private async read(machine: string): Promise<void> {
    this.busy = true;
    this.hand();
    try {
      this.offer = { ...(await this.feed(machine)), ...(await this.documentsIn()) };
      this.holdDesign();
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
      chosen: this.chosen,
      plan: plan(this.chosen),
      offer: this.offer,
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
    <link rel="stylesheet" href="${asset("media", "newsession.css")}" />
  </head>
  <body><main id="newsession"></main>
    <script nonce="${nonce}" src="${asset("dist", "newsession.js")}"></script>
  </body>
</html>`;
  }
}
