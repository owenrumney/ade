// What a review has read, as the server counted it. The ticks live in the
// ledger, so they survive the browser.

import type { Span } from "./dwell";

export interface LedgerFile {
  path: string;
  agent: number;
  read: number;
  unread: number;
  // viewed is every hunk of the file ticked off.
  viewed: boolean;
  ticked: Span[];
}

export interface Ledger {
  files: LedgerFile[];
  agent: number;
  read: number;
  unread: number;
}

export const NO_LEDGER: Ledger = { files: [], agent: 0, read: 0, unread: 0 };

export function ledgerIn(got: unknown): Ledger {
  const ledger = got as Partial<Ledger> | undefined;
  if (!ledger || !Array.isArray(ledger.files)) return NO_LEDGER;
  return {
    files: ledger.files.filter((f) => Boolean(f?.path)),
    agent: ledger.agent ?? 0,
    read: ledger.read ?? 0,
    unread: ledger.unread ?? 0,
  };
}

export function fileIn(ledger: Ledger, path: string): LedgerFile | undefined {
  return ledger.files.find((f) => f.path === path);
}

// viewedIn is the paths ticked off, which is what dims a file in the tree.
export function viewedIn(ledger: Ledger): Set<string> {
  return new Set(ledger.files.filter((f) => f.viewed).map((f) => f.path));
}

// ticked is whether a hunk's own lines are all claimed. A tick on part of it
// is not a tick on it: the box says what it means or it says nothing.
export function ticked(file: LedgerFile | undefined, span: Span): boolean {
  if (!file || span.lines < 1) return false;
  for (let line = span.start; line < span.start + span.lines; line++) {
    if (!file.ticked.some((run) => line >= run.start && line < run.start + run.lines)) return false;
  }
  return true;
}

export function unreadNote(file: LedgerFile | undefined): string {
  if (!file || file.agent === 0) return "";
  if (file.unread === 0) return `${file.agent} agent lines, all read`;
  return `${file.unread} of ${file.agent} agent lines unread`;
}
