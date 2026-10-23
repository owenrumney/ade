import * as vscode from "vscode";
import type { Do, Line, Section } from "./sections";

// Node is a heading or a row. A section without groups is rows only.
export type Node =
  | { kind: "group"; name: string; lines: Line[] }
  | { kind: "line"; line: Line; at: string };

// nodesOf is what a section draws at its root: its headings, or its rows.
export function nodesOf(section: Section | undefined): Node[] {
  if (!section) return [];
  const groups = section.groups?.filter((g) => g.lines.length > 0) ?? [];
  if (groups.length > 0)
    return groups.map((g) => ({ kind: "group", name: g.name, lines: g.lines }));
  return section.lines.map((line, i) => ({ kind: "line", line, at: `${i}` }));
}

// SectionTree draws one section of the desk. One view per section, so each
// keeps its own badge, its own collapse and its own keyboard focus.
export class SectionTree implements vscode.TreeDataProvider<Node> {
  private changed = new vscode.EventEmitter<Node | undefined>();
  readonly onDidChangeTreeData = this.changed.event;
  private held?: Section;

  constructor(
    private id: string,
    private act: (what: Do) => Promise<string | void>,
  ) {}

  show(all: Section[]): void {
    this.held = all.find((s) => s.id === this.id);
    this.changed.fire(undefined);
  }

  get count(): number {
    return this.held?.lines.length ?? 0;
  }

  // run is what a row's command calls, so a click is the same action the
  // dashboard's line was.
  async run(node: Node): Promise<void> {
    if (node.kind !== "line") return;
    const said = await this.act(node.line.do);
    if (said) vscode.window.setStatusBarMessage(`ade: ${said}`, 3000);
  }

  getChildren(node?: Node): Node[] {
    if (!node) return nodesOf(this.held);
    if (node.kind !== "group") return [];
    return node.lines.map((line, i) => ({
      kind: "line",
      line,
      at: `${node.name}/${i}`,
    }));
  }

  getTreeItem(node: Node): vscode.TreeItem {
    if (node.kind === "group") {
      const head = new vscode.TreeItem(
        node.name,
        vscode.TreeItemCollapsibleState.Expanded,
      );
      head.id = `${this.id}/${node.name}`;
      head.description = `${node.lines.length}`;
      head.contextValue = "adeDeskGroup";
      return head;
    }
    const item = new vscode.TreeItem(
      node.line.label,
      vscode.TreeItemCollapsibleState.None,
    );
    item.id = `${this.id}/${node.at}`;
    item.description = node.line.meta;
    item.tooltip = [node.line.label, node.line.meta].filter(Boolean).join("\n");
    item.iconPath = new vscode.ThemeIcon(node.line.icon);
    item.contextValue = "adeDeskRow";
    item.command = {
      command: "ade.desk.run",
      title: node.line.label,
      arguments: [this.id, node],
    };
    return item;
  }
}
