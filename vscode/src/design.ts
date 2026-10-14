import * as vscode from "vscode";
import { readdir } from "node:fs/promises";
import { join } from "node:path";
import { held } from "./loading";

// Design lists the design directory as files. They are Markdown on disk, so the
// editor already edits, searches and diffs them better than a frame could.
// The directory is the server's answer, never one worked out here.
export class Design {
  private watcher?: vscode.FileSystemWatcher;
  private sweeping = false;
  private fetch = held<string[]>(() => this.read(), []);

  constructor(private dir: string | undefined) {
    this.watch();
  }

  // Called on every state event. An unchanged directory keeps its watcher —
  // rewatching would drop the one the agent's writes come through — but is
  // still swept, because that watcher does not fire. See sweep.
  serving(dir: string | undefined): void {
    if ((dir ?? undefined) !== this.dir) {
      this.dir = dir ?? undefined;
      this.watch();
      this.publish([]);
      this.reload();
      return;
    }
    void this.sweep();
  }

  // refresh is the picker opening, for when you want to be sure rather than
  // wait for the next sweep.
  async refresh(): Promise<void> {
    await this.fetch.again();
  }

  // dir is where a new document goes, so the command writes beside the ones
  // already listed rather than working the path out a second way.
  get docs(): string | undefined {
    return this.dir;
  }

  dispose(): void {
    this.watcher?.dispose();
  }

  // files is every document, flat. There is no level to answer any more: the
  // picker filters on the path, which a folder row would only get in the way of.
  get files(): readonly string[] {
    return this.fetch.now;
  }

  private async read(): Promise<string[]> {
    if (!this.dir) return this.publish([]);
    try {
      const found = await walk(this.dir, "");
      return this.publish(found.sort((a, b) => a.localeCompare(b)));
    } catch {
      return this.publish([]);
    }
  }

  // publish is the context key the document's title buttons read: every
  // document, as the path the editor sees it under.
  private publish(files: string[]): string[] {
    const dir = this.dir;
    void vscode.commands.executeCommand(
      "setContext",
      "ade.designDocs",
      dir ? files.map((f) => join(dir, f)) : [],
    );
    return files;
  }

  // sweep re-reads and redraws only when the list actually changed. The
  // documents live under .git, where VS Code's watcher does not reach, so
  // this and not the watcher is what shows a document the agent just wrote.
  private async sweep(): Promise<void> {
    if (!this.dir || this.sweeping) return;
    this.sweeping = true;
    try {
      const found = await this.read();
      if (same(found, this.fetch.now)) return;
      this.fetch.put(found);
    } finally {
      this.sweeping = false;
    }
  }

  private reload(): void {
    this.fetch.reload();
    void this.fetch.children();
  }

  // watch redraws when a document is written. The agent writes these while you
  // are looking at the list, which is the whole reason the list exists.
  private watch(): void {
    this.watcher?.dispose();
    this.watcher = undefined;
    if (!this.dir) return;
    this.watcher = vscode.workspace.createFileSystemWatcher(
      new vscode.RelativePattern(vscode.Uri.file(this.dir), "**/*.md"),
    );
    this.watcher.onDidCreate(() => this.reload());
    this.watcher.onDidDelete(() => this.reload());
  }
}

// same compares two sorted listings, so an unchanged directory costs one
// readdir and no redraw.
export function same(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((name, i) => name === b[i]);
}

export function base(path: string): string {
  return path.split("/").pop() ?? path;
}

async function walk(dir: string, at: string): Promise<string[]> {
  const found = await readdir(join(dir, at), { withFileTypes: true });
  const out: string[] = [];
  for (const entry of found) {
    const path = at ? `${at}/${entry.name}` : entry.name;
    if (entry.isDirectory()) out.push(...(await walk(dir, path)));
    else if (entry.name.endsWith(".md")) out.push(path);
  }
  return out;
}
