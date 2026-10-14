import * as vscode from "vscode";
import { isAbsolute, join } from "node:path";
import type { Client } from "./client";
import { type DriftItem, type DriftState, marks, open } from "./driftrow";
import { held } from "./loading";

// Drift is where the change and the design document disagree. The squiggles are
// the ambient half, on screen whether or not anybody opens the checklist.
export class Drift {
  private squiggles = vscode.languages.createDiagnosticCollection("ade drift");
  private client?: Client;
  private root = "";
  private docs = "";
  private fetch = held<DriftState | undefined>(
    async () => this.client?.drift().catch(() => undefined),
    undefined,
  );

  // root is the checkout and docs where its design documents live: a mark
  // names a file relative to one or the other.
  serving(client: Client | undefined, root = "", docs = ""): void {
    this.client = client;
    this.root = root;
    this.docs = docs;
    if (!client) this.squiggles.clear();
    this.fetch.stale();
  }

  // place is where marks point, told on every state: the docs directory is
  // only known once the server says where this checkout keeps them.
  place(root = "", docs = ""): void {
    this.root = root;
    this.docs = docs;
  }

  // Reads now rather than when the picker is next opened: the squiggles are on
  // screen whether or not anybody asks for the list.
  async refresh(): Promise<void> {
    this.fetch.reload();
    this.draw(await this.fetch.children());
  }

  get count(): number {
    return open(this.fetch.now);
  }

  get file(): string | undefined {
    return this.fetch.now?.file;
  }

  state(): Promise<DriftState | undefined> {
    return this.fetch.children();
  }

  dispose(): void {
    this.squiggles.dispose();
  }

  // at opens the document at the requirement, which is what the row is about;
  // the code a contradiction names has its own squiggle.
  at(item: DriftItem): vscode.Command | undefined {
    const file = this.fetch.now?.file;
    if (!file || !this.docs) return undefined;
    const line = Math.max(0, item.line - 1);
    return {
      command: "vscode.open",
      title: "Open",
      arguments: [
        vscode.Uri.file(join(this.docs, file)),
        { selection: new vscode.Range(line, 0, line, 0) },
      ],
    };
  }

  private draw(state: DriftState | undefined): void {
    this.squiggles.clear();
    const byFile = new Map<string, vscode.Diagnostic[]>();
    for (const mark of marks(state)) {
      const base = mark.design ? this.docs : this.root;
      if (!base) continue;
      const line = mark.line - 1;
      const found = new vscode.Diagnostic(
        new vscode.Range(line, 0, line, Number.MAX_SAFE_INTEGER),
        mark.message,
        mark.error
          ? vscode.DiagnosticSeverity.Error
          : vscode.DiagnosticSeverity.Warning,
      );
      found.source = "ade drift";
      const path = isAbsolute(mark.file) ? mark.file : join(base, mark.file);
      byFile.set(path, [...(byFile.get(path) ?? []), found]);
    }
    for (const [path, found] of byFile)
      this.squiggles.set(vscode.Uri.file(path), found);
  }
}
