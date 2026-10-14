import * as vscode from "vscode";
import type { Server } from "./discover";
import { describe, done, icon, list, overdue, waiting, type Todo } from "./todos";

// Desk holds the todo list between openings: the machine's, and this window's
// checkout too. A quick pick draws from it — a keystroke, not a pane.
export class Desk {
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChange = this.changed.event;
  private items: Todo[] = [];
  private dir?: string;

  // The launcher is read when the list is, not held: the desk exists before
  // one has been found, and a held copy would stay empty for the window's life.
  constructor(private at: () => Promise<Server | undefined>) {}

  // scope is the checkout this window attached to, whose todos join the list.
  scope(dir: string | undefined): void {
    if (this.dir === dir) return;
    this.dir = dir;
    void this.refresh();
  }

  async refresh(): Promise<void> {
    const server = await this.at();
    if (!server) return;
    const items = await list(server, this.dir).catch(() => undefined);
    if (!items) return;
    this.items = items;
    this.changed.fire();
  }

  get list(): Todo[] {
    return this.items;
  }

  get count(): number {
    return waiting(this.items);
  }

  get late(): number {
    const now = new Date();
    return this.items.filter((item) => overdue(item, now)).length;
  }

  // where is the store a row lives in, which every edit has to name.
  where(item: Todo): string | undefined {
    return item.repo ? this.dir : undefined;
  }
}

// Row is one line of the quick pick: the thing itself, dressed.
export interface Row extends vscode.QuickPickItem {
  todo?: Todo;
}

// The buttons a row carries, and the one the list carries. Named here with the
// rows they sit on, so the picker compares identities rather than tooltips.
export const ADD: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("add"),
  tooltip: "Add a todo",
};
export const DONE: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("check"),
  tooltip: "Done",
};
export const REOPEN: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("issue-reopened"),
  tooltip: "Not done after all",
};
export const DUE: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("calendar"),
  tooltip: "Set a due date",
};
export const DROP: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("trash"),
  tooltip: "Delete",
};

// groupOf is the first #word in the text. A group is a tag, not a form: it
// is typed with the todo, filters the list, and costs the store nothing.
export function groupOf(item: Todo): string | undefined {
  return /(^|\s)#([\w-]+)/.exec(item.text)?.[2]?.toLowerCase();
}

// rows draws the list for the picker: loose items first, then each group
// under its tag in the order the sort first reached it, then what is done.
export function rows(items: Todo[], now: Date): Row[] {
  const open = items.filter((item) => !done(item));
  const closed = items.filter(done);
  const groups = new Map<string, Todo[]>();
  const loose: Todo[] = [];
  for (const item of open) {
    const group = groupOf(item);
    if (!group) loose.push(item);
    else groups.set(group, [...(groups.get(group) ?? []), item]);
  }
  const out: Row[] = loose.map((item) => row(item, now));
  for (const [name, members] of groups) {
    out.push({ label: `#${name}`, kind: vscode.QuickPickItemKind.Separator });
    out.push(...members.map((item) => row(item, now)));
  }
  if (closed.length > 0 && open.length > 0) {
    out.push({ label: "done", kind: vscode.QuickPickItemKind.Separator });
  }
  out.push(...closed.map((item) => row(item, now)));
  return out;
}

function row(item: Todo, now: Date): Row {
  return {
    label: `$(${icon(item, now)}) ${item.text}`,
    description: describe(item, now),
    detail: item.note || undefined,
    buttons: done(item) ? [REOPEN, DROP] : [DONE, DUE, DROP],
    todo: item,
  };
}
