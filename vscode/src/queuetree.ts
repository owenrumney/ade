import * as vscode from "vscode";
import type { Client, Waiting } from "./client";
import { same, treeIcon, treeLabel, treeRows, type TreeRow } from "./queue";

// Queue is what is waiting for the agent. A button pressed during a run queues
// rather than refusing, so what is waiting has to be somewhere you can see it.
export class Queue implements vscode.TreeDataProvider<TreeRow> {
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private client?: Client;
  private rows: TreeRow[] = [];

  show(waiting: Waiting[] | null | undefined): void {
    const next = treeRows(waiting);
    if (same(this.rows, next)) return;
    this.rows = next;
    this.changed.fire();
  }

  serving(client: Client | undefined): void {
    this.client = client;
    if (!client) this.rows = [];
    this.changed.fire();
  }

  async refresh(): Promise<void> {
    if (!this.client) return;
    const state = await this.client.state().catch(() => undefined);
    this.show(state?.agent?.waiting ?? []);
  }

  get count(): number {
    return this.rows.length;
  }

  getChildren(row?: TreeRow): TreeRow[] {
    return row ? [] : this.rows;
  }

  getTreeItem(row: TreeRow): vscode.TreeItem {
    const item = new vscode.TreeItem(
      treeLabel(row),
      vscode.TreeItemCollapsibleState.None,
    );
    item.id = row.id;
    item.description = row.what;
    item.tooltip = row.what || row.kind;
    item.iconPath = new vscode.ThemeIcon(treeIcon(row));
    // The one at the front is already next, so it has nothing to promote to.
    item.contextValue = row.next ? "adeQueuedNext" : "adeQueued";
    return item;
  }
}
