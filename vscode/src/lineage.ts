import * as vscode from "vscode";
import { relative } from "node:path";
import type { Client, Marks } from "./client";
import { spans, summarise } from "./ranges";

// Lineage marks the lines a turn of the conversation wrote, and answers "why is
// this here?" on hover from the same store `ade why` reads.
export class Lineage implements vscode.Disposable {
  private mark: vscode.TextEditorDecorationType;
  private marks: Marks = {};
  private disposables: vscode.Disposable[] = [];

  constructor(private client: Client) {
    this.mark = vscode.window.createTextEditorDecorationType({
      // A ruler tick and a border, not a background: the line is ordinary code
      // and shading every agent-written line would shade most of the file.
      overviewRulerColor: new vscode.ThemeColor("editorInfo.foreground"),
      overviewRulerLane: vscode.OverviewRulerLane.Left,
      borderWidth: "0 0 0 2px",
      borderStyle: "solid",
      borderColor: new vscode.ThemeColor("editorInfo.foreground"),
      isWholeLine: true,
      rangeBehavior: vscode.DecorationRangeBehavior.ClosedClosed,
    });

    this.disposables.push(
      vscode.window.onDidChangeVisibleTextEditors(() => this.draw()),
      vscode.languages.registerHoverProvider(
        { scheme: "file" },
        { provideHover: (doc, at) => this.hover(doc, at) },
      ),
    );
  }

  dispose(): void {
    for (const d of this.disposables) d.dispose();
    this.mark.dispose();
  }

  async refresh(): Promise<void> {
    if (
      !vscode.workspace
        .getConfiguration("ade")
        .get<boolean>("lineageGutter", true)
    ) {
      this.marks = {};
      this.draw();
      return;
    }
    try {
      this.marks = await this.client.marks();
    } catch {
      this.marks = {};
    }
    this.draw();
  }

  private draw(): void {
    for (const editor of vscode.window.visibleTextEditors) {
      const file = this.rel(editor.document.uri);
      const lines = file ? (this.marks[file] ?? []) : [];
      editor.setDecorations(
        this.mark,
        spans(lines).map((s) => new vscode.Range(s.from - 1, 0, s.to - 1, 0)),
      );
    }
  }

  private async hover(
    doc: vscode.TextDocument,
    at: vscode.Position,
  ): Promise<vscode.Hover | undefined> {
    const file = this.rel(doc.uri);
    const line = at.line + 1;
    if (!file || !this.marks[file]?.includes(line)) return undefined;

    let text: string;
    try {
      text = summarise(await this.client.lineage(file, line));
    } catch {
      return undefined;
    }
    if (!text) return undefined;
    return new vscode.Hover(new vscode.MarkdownString(text));
  }

  // rel is the path as ade names it: relative to the root the diff is of, which
  // is not always the workspace folder.
  private rel(uri: vscode.Uri): string | undefined {
    if (uri.scheme !== "file") return undefined;
    const path = relative(this.client.root, uri.fsPath);
    return path && !path.startsWith("..")
      ? path.split("\\").join("/")
      : undefined;
  }
}
