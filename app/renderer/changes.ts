// The diff as the session server answers with it: side-by-side rows,
// drawn unified here, which is one line per cell rather than per row.

// c is the highlight class, t the text, e a run that differs from the
// line this one replaced.
export interface Token {
  c?: string;
  t: string;
  e?: boolean;
}

export interface Cell {
  line: number;
  kind: string;
  content: string;
  tokens?: Token[] | null;
}

export interface Row {
  left: Cell | null;
  right: Cell | null;
}

export interface Hunk {
  header: string;
  heading?: string;
  rows: Row[] | null;
}

export interface Change {
  path: string;
  oldPath?: string;
  status: string;
  binary: boolean;
  additions: number;
  deletions: number;
  hunks?: Hunk[] | null;
}

export interface Diff {
  base?: string;
  head?: string;
  mode?: string;
  commits?: number;
  files?: Change[] | null;
}

// Which revision a thread is anchored to, in the server's own words
// (internal/comments/anchor.go). Diff rows call the same sides del and add.
export type Side = "left" | "right";

// A comment thread, as the session server keeps it: anchored to a file
// and a line of one side, with every comment on it.
// The editor's, not a second declaration of it. A narrower copy here is what
// left the window with no notion of a draft at all.
import type { Thread } from "../../vscode/src/client";
export type { Comment as Said, Thread } from "../../vscode/src/client";

// One line of the drawn diff: a hunk header, a cell with the number it
// has on whichever side it belongs to, or a thread anchored under one.
export type Line =
  | { at: "hunk"; text: string }
  | { at: "line"; kind: string; no: number; cell: Cell }
  | { at: "thread"; thread: Thread };

export function changesIn(got: unknown): Change[] {
  const diff = (got as { diff?: Diff })?.diff;
  return (diff?.files ?? []).filter((f) => Boolean(f?.path));
}

// linesOf is the file drawn unified: deletions from the left, the rest
// from the right. A thread follows the line it is anchored to.
export function linesOf(file: Change | undefined, threads: Thread[] = []): Line[] {
  const out: Line[] = [];
  const mine = threads.filter((t) => t.file === file?.path);
  const under = (side: Side, no: number) => {
    // Not an orphan: it keeps the line it was pinned to, and the tail
    // below draws it. Both would draw it twice.
    for (const thread of mine.filter((t) => !t.orphaned && t.side === side && t.line === no)) {
      out.push({ at: "thread", thread });
    }
  };
  for (const hunk of file?.hunks ?? []) {
    out.push({ at: "hunk", text: [hunk.header, hunk.heading].filter(Boolean).join(" ") });
    for (const row of hunk.rows ?? []) {
      // A paired replacement is two lines, the old one first, which is
      // the order a unified diff has them in.
      if (row.left && row.left.kind === "del") {
        out.push({ at: "line", kind: "del", no: row.left.line, cell: row.left });
        under("left", row.left.line);
      }
      if (row.right && row.right.kind !== "del") {
        out.push({ at: "line", kind: row.right.kind, no: row.right.line, cell: row.right });
        under("right", row.right.line);
      }
      // A context row has the same content on both sides, so the right
      // one above is the whole of it.
    }
  }
  // A thread whose line went is still a finding, so it is drawn at the
  // end rather than dropped with the line it was pinned to.
  for (const thread of mine.filter((t) => t.orphaned)) out.push({ at: "thread", thread });
  return out;
}

// What a thread is waiting for. Sent is done; a reply of yours that has
// not gone is the one thing a review can leave behind.
export function unsent(threads: Thread[]): number {
  return threads.filter((t) => !t.resolved && !t.externalId && !t.sentAt).length;
}

// What the tree colours a file by. It takes three, so anything unusual
// reads as a change rather than as nothing.
export function toneOf(status: string): "added" | "changed" | "removed" {
  if (status.startsWith("a") || status.startsWith("?")) return "added";
  if (status.startsWith("d")) return "removed";
  return "changed";
}

// Listed is one open comment as the Review sidebar names it: how bad, and
// the first line of what it says.
export interface Listed {
  id: string;
  file: string;
  line: number;
  severity: string;
  title: string;
}

// At is what a jump into a file named: the comment to land on, and the line
// it is about when that comment is not drawn in the diff.
export interface At {
  thread?: string;
  line?: number;
}

// wanted is the selector a jump lands on. The two ids are ade's own — a hex
// string and a line number — so anything else in them is not a selector.
export function wanted(jump?: At): string {
  const safe = (value: string) => value.replace(/[^\w-]/g, "");
  const parts: string[] = [];
  if (jump?.thread) parts.push(`[data-thread="${safe(jump.thread)}"]`);
  if (jump?.line) parts.push(`[data-line="${safe(String(jump.line))}"]`);
  return parts.join(", ");
}

// drawnFrom is everything one file's diff draws besides the diff itself. A
// pane built from one of these is stale the moment it changes.
export function drawnFrom(
  path: string,
  threads: Thread[],
  open: Set<string>,
  hasPR: boolean,
): string {
  const mine = threads
    .filter((t) => t.file === path)
    .map((t) =>
      [
        t.id,
        t.comments?.length ?? 0,
        t.severity ?? "",
        t.resolved ? "r" : "",
        t.orphaned ? "o" : "",
        t.outgoing ? "w" : "",
        t.sentAt ?? "",
        t.draftReview ?? "",
        open.has(t.id) ? "open" : "",
      ].join("."),
    );
  return `${hasPR ? "pr" : "no-pr"}|${mine.join(",")}`;
}

const RANK: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 };

// listed is the comments still to deal with, worst first: what would be said
// once written up, and the finding before that. Resolved and sent are done.
export function listed(threads: Thread[]): Listed[] {
  return threads
    .filter((t) => !t.resolved && !t.sentAt)
    .map((t) => ({
      id: t.id,
      file: t.file,
      line: t.line,
      severity: t.severity || "medium",
      title: firstLine(t.outgoing || t.comments?.[0]?.body || ""),
    }))
    .sort(
      (a, b) =>
        (RANK[a.severity] ?? 2) - (RANK[b.severity] ?? 2) ||
        a.file.localeCompare(b.file) ||
        a.line - b.line,
    );
}

function firstLine(body: string): string {
  const line = body.split("\n").find((one) => one.trim()) ?? "";
  return line.replace(/^[#>*\-\s]+/, "").replace(/`|\*\*/g, "").trim();
}
