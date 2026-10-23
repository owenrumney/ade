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

// A comment thread, as the session server keeps it: anchored to a file
// and a line of one side, with every comment on it.
export interface Said {
  id: string;
  author: string;
  body: string;
  at?: string;
  sentAt?: string;
  outcome?: string;
}

export interface Thread {
  id: string;
  file: string;
  side: string;
  line: number;
  comments?: Said[] | null;
  resolved?: boolean;
  orphaned?: boolean;
  category?: string;
  severity?: string;
  sentAt?: string;
  externalId?: string;
}

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
  const under = (side: string, no: number) => {
    for (const thread of mine.filter((t) => t.side === side && t.line === no)) {
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
        under("old", row.left.line);
      }
      if (row.right && row.right.kind !== "del") {
        out.push({ at: "line", kind: row.right.kind, no: row.right.line, cell: row.right });
        under("new", row.right.line);
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
