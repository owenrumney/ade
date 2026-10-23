import * as vscode from "vscode";
import {
  type Active,
  type Branch,
  detail,
  icon,
  type Node,
  same,
  tree,
  waiting,
} from "./agents";
import { ask, find } from "./launcher";

// Agents is every agent at work on every machine, as a tree: a session's own
// agent, and under it the sub-agents and named side runs it called.
export class Agents implements vscode.TreeDataProvider<Node> {
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private branches: Branch[] = [];
  private wanted = 0;

  // How many agents want an answer. The badge says it; the context key
  // turns the activity bar icon red, which is what you see without looking.
  get count(): number {
    return this.wanted;
  }

  show(all: Active[]): void {
    const next = tree(all);
    this.wanted = waiting(all);
    void vscode.commands.executeCommand(
      "setContext",
      "ade.agentsWaiting",
      this.wanted > 0,
    );
    if (same(this.branches, next)) return;
    this.branches = next;
    this.changed.fire();
  }

  async refresh(): Promise<void> {
    const at = await find();
    if (!at) return this.show([]);
    const got = await ask<Active[]>(at, "/api/serving").catch(() => undefined);
    if (got) this.show(got);
  }

  getChildren(node?: Node): Node[] {
    if (!node) return this.branches.map((b) => b.row);
    return this.branches.find((b) => b.row.id === node.id)?.children ?? [];
  }

  getTreeItem(node: Node): vscode.TreeItem {
    const kids = this.getChildren(node).length;
    const item = new vscode.TreeItem(
      node.name,
      kids > 0
        ? vscode.TreeItemCollapsibleState.Expanded
        : vscode.TreeItemCollapsibleState.None,
    );
    item.id = node.id;
    item.description = detail(node);
    item.tooltip = [node.name, detail(node), node.machine]
      .filter(Boolean)
      .join("\n");
    item.iconPath = new vscode.ThemeIcon(icon(node));
    // A row opens the conversation it belongs to, a sub-agent's included.
    item.command = {
      command: "ade.openSession",
      title: "Open the conversation",
      arguments: [{ id: node.session, dir: node.dir }, node.machine],
    };
    item.contextValue =
      node.kind === "session" ? "adeAgentSession" : "adeAgentChild";
    return item;
  }
}
