import * as vscode from "vscode";
import type { Client, Diff, DiffFile } from "./client";
import {
  BLOB,
  atOf,
  colour,
  letter,
  name,
  nest,
  type Node,
  rank,
  sides,
  staleSides,
  summary,
  tally,
  title,
  REV,
  revUri,
} from "./change";
import { fetched, VIEWS } from "./loading";

// Two schemes, because they are two jobs. A row is identified by CHANGE so a
// decoration lands on the tree and nowhere else; a side is read through BLOB.
export const CHANGE = "ade-change";
export { BLOB, REV, revUri };

export function changeUri(path: string): vscode.Uri {
  return vscode.Uri.from({ scheme: CHANGE, path: `/${path}` });
}

export function blobUri(path: string, side: "left" | "right"): vscode.Uri {
  return vscode.Uri.from({ scheme: BLOB, path: `/${path}`, query: side });
}

export function pathOf(uri: vscode.Uri): string {
  return uri.path.replace(/^\//, "");
}

// Changes is the diff as a tree: the view this extension exists to replace a
// frame with, because the editor's own diff is better than any page's.
export class Changes
  implements vscode.TreeDataProvider<Node>, vscode.FileDecorationProvider
{
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private decorated = new vscode.EventEmitter<vscode.Uri[]>();
  readonly onDidChangeFileDecorations = this.decorated.event;

  private client?: Client;
  private diff?: Diff;
  private version = "";
  private fetch = fetched<DiffFile[]>(VIEWS.diff, () => this.load(), []);
  private moved?: () => void;

  constructor(private view?: vscode.TreeView<Node>) {}

  // Told when the diff is read again: a scope change or a commit moves what
  // a side is without moving its uri.
  onMoved(tell: () => void): void {
    this.moved = tell;
  }

  watching(view: vscode.TreeView<Node>): void {
    this.view = view;
    this.describe();
  }

  serving(client: Client | undefined): void {
    this.client = client;
    this.version = "";
    if (client) {
      this.fetch.stale();
    } else {
      this.diff = undefined;
      this.fetch.clear();
    }
    this.changed.fire();
  }

  private async load(): Promise<DiffFile[]> {
    if (!this.client) {
      this.diff = undefined;
      return [];
    }
    this.diff = await this.client.diff().catch(() => undefined);
    return [...(this.diff?.files ?? [])].sort(rank);
  }

  // at refetches only when the server says the diff moved. A state event
  // arrives every few seconds during a run and the diff is the big one.
  async at(version: string): Promise<void> {
    if (version === this.version) return;
    this.version = version;
    await this.refresh();
  }

  async refresh(): Promise<void> {
    this.fetch.reload();
    this.changed.fire();
    const files = await this.fetch.children();
    this.changed.fire();
    this.decorated.fire(files.map((f) => changeUri(f.path)));
    this.describe();
    this.moved?.();
  }

  private describe(): void {
    if (this.view) this.view.description = summary(this.diff);
  }

  async getChildren(node?: Node): Promise<Node[]> {
    if (!node) return nest(await this.fetch.children());
    return node.kind === "dir" ? node.kids : [];
  }

  getTreeItem(node: Node): vscode.TreeItem {
    return node.kind === "dir" ? dirItem(node) : this.fileItem(node.file);
  }

  private fileItem(file: DiffFile): vscode.TreeItem {
    const item = new vscode.TreeItem(
      name(file.path),
      vscode.TreeItemCollapsibleState.None,
    );
    item.description = tally(file);
    item.resourceUri = changeUri(file.path);
    item.tooltip = file.oldPath ? `${file.oldPath} → ${file.path}` : file.path;
    item.contextValue = "ade.change";
    item.command = {
      command: "ade.openChange",
      title: "Open the change",
      arguments: [file],
    };
    return item;
  }

  // The green and the red. A tree row takes its colour from here and from
  // nowhere else: the SCM view gets this for free and a TreeView does not.
  provideFileDecoration(uri: vscode.Uri): vscode.FileDecoration | undefined {
    if (uri.scheme !== CHANGE) return undefined;
    const file = this.diff?.files?.find((f) => f.path === pathOf(uri));
    if (!file) return undefined;
    return {
      badge: letter(file),
      color: new vscode.ThemeColor(colour(file)),
      tooltip: tally(file),
    };
  }

  // The right side is the file on disk wherever the scope ends at the working
  // tree, so the diff is editable rather than a picture of one.
  async open(file: DiffFile, root: string): Promise<void> {
    const has = sides(file);
    const left = has.left
      ? blobUri(file.oldPath || file.path, "left")
      : blobUri(file.path, "left");
    const right =
      has.right && this.diff?.mode !== "committed"
        ? vscode.Uri.file(`${root}/${file.path}`)
        : blobUri(file.path, "right");
    await vscode.commands.executeCommand(
      "vscode.diff",
      left,
      right,
      title(file, this.diff),
    );
  }
}

// A directory is expanded by default: a diff you have to click into to read
// is a diff you will skim instead.
function dirItem(node: Node & { kind: "dir" }): vscode.TreeItem {
  const item = new vscode.TreeItem(
    node.label,
    vscode.TreeItemCollapsibleState.Expanded,
  );
  item.resourceUri = vscode.Uri.from({ scheme: CHANGE, path: `/${node.path}` });
  item.iconPath = vscode.ThemeIcon.Folder;
  item.contextValue = "ade.change.dir";
  return item;
}

// Blobs serves either side as a read-only document. Read-only because it is a
// commit: the working tree is opened as itself and stays editable.
export class Blobs implements vscode.TextDocumentContentProvider {
  private changed = new vscode.EventEmitter<vscode.Uri>();
  readonly onDidChange = this.changed.event;

  constructor(private client: () => Client | undefined) {}

  // The editor keeps a document it has read and never asks again, so the
  // left of one scope would be shown as the left of the next.
  stale(): void {
    for (const uri of staleSides(
      vscode.workspace.textDocuments.map((d) => d.uri),
    ))
      this.changed.fire(uri);
  }
  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const client = this.client();
    if (!client) return "";
    const side = uri.query === "right" ? "right" : "left";
    return client.blob(pathOf(uri), side).catch(() => "");
  }
}

// Revs serves a file as one commit left it. A commit never changes, so unlike
// Blobs this never goes stale and fires nothing.
export class Revs implements vscode.TextDocumentContentProvider {
  constructor(private client: () => Client | undefined) {}

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const client = this.client();
    const { sha, path } = atOf(uri);
    if (!client || !sha) return "";
    return client.fileAt(sha, path).catch(() => "");
  }
}
