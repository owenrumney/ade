// DriftState mirrors drift.State: the design document's requirements, each
// scored against the diff by the last pass.
export interface DriftState {
  file?: string;
  at?: string;
  items: DriftItem[] | null;
}

export interface DriftItem {
  id: string;
  kind?: string;
  text: string;
  line: number;
  verdict?: string;
  note?: string;
  // Set only on a contradiction: where in the code it was found.
  file?: string;
  fileLine?: number;
  dismissed?: boolean;
}

export type Row =
  { kind: "item"; item: DriftItem } | { kind: "dismissed"; items: DriftItem[] };

// roots is the checklist, worst first, with what was waved off folded away
// under one row so it can be brought back.
export function roots(state: DriftState | undefined): Row[] {
  const items = state?.items ?? [];
  const live = items
    .filter((item) => !item.dismissed)
    .sort((a, b) => rank(a) - rank(b) || a.line - b.line);
  const gone = items.filter((item) => item.dismissed);
  const out: Row[] = live.map((item) => ({ kind: "item", item }));
  if (gone.length) out.push({ kind: "dismissed", items: gone });
  return out;
}

export function children(row: Row): Row[] {
  return row.kind === "dismissed"
    ? row.items.map((item) => ({ kind: "item", item }))
    : [];
}

const ORDER = ["contradicted", "absent", "partial", "", "done"];

function rank(item: DriftItem): number {
  const at = ORDER.indexOf(item.verdict ?? "");
  return at < 0 ? ORDER.length : at;
}

// open is how many requirements the change has not met and nobody waved off.
export function open(state: DriftState | undefined): number {
  return (state?.items ?? []).filter((item) => !item.dismissed && drifted(item))
    .length;
}

// drifted is a verdict worth a squiggle. Unscored is not: no pass has run.
export function drifted(item: DriftItem): boolean {
  return ["contradicted", "absent", "partial"].includes(item.verdict ?? "");
}

export function context(row: Row): string {
  if (row.kind === "dismissed") return "ade.drift.dismissed";
  return row.item.dismissed ? "ade.drift.item.off" : "ade.drift.item";
}

export function icon(item: DriftItem): string {
  switch (item.verdict) {
    case "done":
      return "pass";
    case "partial":
      return "warning";
    case "absent":
      return "circle-large-outline";
    case "contradicted":
      return "error";
    default:
      return "circle-outline";
  }
}

// said is the verdict as a sentence, for the row and for the squiggle.
export function said(item: DriftItem): string {
  const verdict =
    {
      done: "done",
      partial: "partly done",
      absent: "not in the change",
      contradicted: "contradicted by the change",
    }[item.verdict ?? ""] ?? "not checked yet";
  return item.note ? `${verdict} — ${item.note}` : verdict;
}

// Mark is one squiggle: on the design document, or on the code a
// contradiction was found in.
export interface Mark {
  // design is the document itself; otherwise file is relative to the checkout.
  design: boolean;
  file: string;
  // line is 1-based, as the server counts.
  line: number;
  message: string;
  error: boolean;
}

// marks is every squiggle the checklist earns. Dismissed, done and unscored
// requirements earn none.
export function marks(state: DriftState | undefined): Mark[] {
  if (!state?.file) return [];
  const out: Mark[] = [];
  for (const item of state.items ?? []) {
    if (item.dismissed || !drifted(item)) continue;
    const error = item.verdict === "contradicted";
    out.push({
      design: true,
      file: state.file,
      line: Math.max(1, item.line),
      message: `${said(item)}: ${item.text}`,
      error,
    });
    if (error && item.file)
      out.push({
        design: false,
        file: item.file,
        line: Math.max(1, item.fileLine ?? 1),
        message: `contradicts ${state.file}: ${item.text}`,
        error: true,
      });
  }
  return out;
}
