import * as vscode from "vscode";
import { join, relative, sep } from "node:path";
import type { Thread } from "./client";

export interface Mark {
  badge: string;
  tooltip: string;
  colour: string;
  propagate: boolean;
}

// openThreads counts, per file, the threads that still want something.
export function openThreads(threads: Thread[]): Map<string, number> {
  const open = new Map<string, number>();
  for (const t of threads) {
    if (t.resolved || t.withdrawn || t.orphaned) continue;
    open.set(t.file, (open.get(t.file) ?? 0) + 1);
  }
  return open;
}

export function markFor(
  touched: boolean,
  open: number,
  running: boolean,
): Mark | undefined {
  if (!touched && open === 0) return undefined;
  const badge = `${touched ? "✎" : ""}${open ? Math.min(open, 9) : ""}`;
  const tooltip = [
    touched ? (running ? "ade is editing this" : "ade edited this turn") : "",
    open ? `${open} open review comment${open === 1 ? "" : "s"}` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return {
    badge,
    tooltip,
    colour: open ? "charts.orange" : "charts.blue",
    propagate: touched,
  };
}

export function marksFor(
  touched: string[],
  threads: Thread[],
  running: boolean,
): Map<string, Mark> {
  const open = openThreads(threads);
  const marks = new Map<string, Mark>();
  for (const file of new Set([...touched, ...open.keys()])) {
    const mark = markFor(touched.includes(file), open.get(file) ?? 0, running);
    if (mark) marks.set(file, mark);
  }
  return marks;
}

// Marks badges the explorer with what the agent is editing and what still has
// an open review comment, so the tree says where the turn is without a diff.
export class Marks implements vscode.FileDecorationProvider {
  private changed = new vscode.EventEmitter<vscode.Uri[]>();
  readonly onDidChangeFileDecorations = this.changed.event;
  private root = "";
  private marks = new Map<string, Mark>();

  apply(
    root: string | undefined,
    touched: string[],
    threads: Thread[],
    running: boolean,
  ): void {
    const next = root ? marksFor(touched, threads, running) : new Map<string, Mark>();
    const was = this.marks;
    const at = root ?? this.root;
    const moved = [...new Set([...was.keys(), ...next.keys()])].filter(
      (file) => was.get(file)?.badge !== next.get(file)?.badge
        || was.get(file)?.tooltip !== next.get(file)?.tooltip,
    );
    this.root = root ?? "";
    this.marks = next;
    if (moved.length && at)
      this.changed.fire(moved.map((file) => vscode.Uri.file(join(at, file))));
  }

  provideFileDecoration(uri: vscode.Uri): vscode.FileDecoration | undefined {
    if (uri.scheme !== "file" || !this.root) return undefined;
    const rel = relative(this.root, uri.fsPath);
    if (!rel || rel.startsWith("..")) return undefined;
    const mark = this.marks.get(rel.split(sep).join("/"));
    if (!mark) return undefined;
    return {
      badge: mark.badge,
      tooltip: mark.tooltip,
      color: new vscode.ThemeColor(mark.colour),
      propagate: mark.propagate,
    };
  }
}
