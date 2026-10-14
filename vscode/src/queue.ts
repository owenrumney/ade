import type { Waiting } from "./client";

// Row is one line of the queue picker: a waiting turn, or the run-all entry.
export interface Row {
  id?: string;
  label: string;
  description?: string;
  // Absent on a button, which has no wording of its own.
  message?: string;
}

export function rows(waiting: Waiting[], running: boolean): Row[] {
  const out: Row[] = waiting.map((w, i) => ({
    id: w.id,
    label: w.what || w.kind,
    description: i === 0 ? `next · ${w.kind}` : w.kind,
    message: w.message,
  }));
  // A running turn drains the queue itself, so there is nothing to start.
  if (!running && out.length) out.unshift({ label: "$(play) Run the queue" });
  return out;
}

// TreeRow is one waiting turn in the view. Numbered, because its position is
// the whole of what decides when it runs.
export interface TreeRow {
  id: string;
  kind: string;
  what: string;
  position: number;
  next: boolean;
}

export function treeRows(waiting: Waiting[] | null | undefined): TreeRow[] {
  return (waiting ?? [])
    .filter((w) => Boolean(w.id))
    .map((w, i) => ({
      id: w.id,
      kind: w.kind,
      what: (w.what ?? "").trim(),
      position: i + 1,
      next: i === 0,
    }));
}

export function treeLabel(row: TreeRow): string {
  return `${row.position}. ${row.kind}`;
}

export function treeIcon(row: TreeRow): string {
  return row.next ? "debug-step-over" : "circle-outline";
}

// same stops a redraw the state poll would otherwise fire every few seconds,
// which closes nothing here but reorders what a click was aimed at.
export function same(a: TreeRow[], b: TreeRow[]): boolean {
  return (
    a.length === b.length &&
    a.every((row, i) => row.id === b[i].id && row.what === b[i].what)
  );
}
