import * as vscode from "vscode";

// PAGE is the scheme a generated page opens under: a reading, not a file, so
// there is nothing to save and no unsaved tab left behind when it closes.
export const PAGE = "ade-page";

// Pages holds what each generated page says, by name. Showing one again
// replaces its words and tells the editor, so an open copy is not stale.
export class Pages implements vscode.TextDocumentContentProvider {
  private held = new Map<string, string>();
  private changed = new vscode.EventEmitter<vscode.Uri>();
  readonly onDidChange = this.changed.event;

  provideTextDocumentContent(uri: vscode.Uri): string {
    return this.held.get(uri.path) ?? "";
  }

  // show opens a page rendered. name ends in .md: the preview is chosen by it.
  async show(name: string, content: string): Promise<void> {
    const uri = vscode.Uri.from({ scheme: PAGE, path: `/${name}` });
    this.held.set(uri.path, content);
    this.changed.fire(uri);
    await vscode.commands.executeCommand("markdown.showPreview", uri);
  }
}

// EDIT is the scheme a generated page you can write back opens under. Saving
// it is the save: there is no second form to keep in step.
export const EDIT = "ade-edit";

interface Held {
  content: string;
  save: (text: string) => Promise<void>;
}

// Edits is a page the editor writes back through a callback. Like Pages, but
// the editor's save is what ships it.
export class Edits implements vscode.FileSystemProvider {
  private held = new Map<string, Held>();
  private changed = new vscode.EventEmitter<vscode.FileChangeEvent[]>();
  readonly onDidChangeFile = this.changed.event;

  async show(
    name: string,
    content: string,
    save: (text: string) => Promise<void>,
  ): Promise<void> {
    const uri = vscode.Uri.from({ scheme: EDIT, path: `/${name}` });
    this.held.set(uri.path, { content, save });
    this.changed.fire([{ type: vscode.FileChangeType.Changed, uri }]);
    await vscode.window.showTextDocument(uri);
  }

  watch(): vscode.Disposable {
    return new vscode.Disposable(() => {});
  }

  stat(uri: vscode.Uri): vscode.FileStat {
    const held = this.held.get(uri.path);
    if (!held) throw vscode.FileSystemError.FileNotFound(uri);
    return {
      type: vscode.FileType.File,
      ctime: 0,
      mtime: Date.now(),
      size: held.content.length,
    };
  }

  readFile(uri: vscode.Uri): Uint8Array {
    const held = this.held.get(uri.path);
    if (!held) throw vscode.FileSystemError.FileNotFound(uri);
    return new TextEncoder().encode(held.content);
  }

  async writeFile(uri: vscode.Uri, content: Uint8Array): Promise<void> {
    const held = this.held.get(uri.path);
    if (!held) throw vscode.FileSystemError.FileNotFound(uri);
    const text = new TextDecoder().decode(content);
    await held.save(text);
    held.content = text;
  }

  readDirectory(): [string, vscode.FileType][] {
    return [];
  }

  createDirectory(): void {}

  delete(): void {}

  rename(): void {}
}
