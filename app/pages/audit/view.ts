// What the audit panel draws, worked out from the lines the trail kept.
// The DOM is main.ts. Design: .git/ade/docs/remove-vscode.md.

export interface Noted {
  at: string;
  kind: string;
  what: string;
  where?: string;
  session?: string;
  // Who wrote the line. Absent is the window itself; the launcher names
  // itself, because work ade did for you is not a press you made.
  from?: string;
  ok?: boolean;
  why?: string;
  ms?: number;
  fields?: string[];
  trace?: string;
}

// Shown is one line as the list draws it: the clock, what it was, and how
// it went. An http line under a command is folded into it.
export interface Shown {
  at: string;
  kind: string;
  what: string;
  where: string;
  outcome: "" | "ok" | "slow" | "failed";
  why: string;
  under: Noted[];
}

// How long a thing may take before the list says so. The window's own
// reads give up at ten seconds; half of that is already worth seeing.
export const SLOW_MS = 5_000;

export function outcomeOf(one: Noted): Shown["outcome"] {
  if (one.ok === false) return "failed";
  if ((one.ms ?? 0) >= SLOW_MS) return "slow";
  if (one.ok === true) return "ok";
  return "";
}

// The kinds that fold under the thing that caused them, rather than standing
// as a line of their own. `step` is the launcher's: a connect is a dial, a
// check and a worker, and the one that refused is the answer.
const UNDER = new Set(["http", "step"]);

// listed is the trail newest first, with what a command caused folded under
// it. A line with no trace stands on its own, which is most of them.
export function listed(all: Noted[], filter = ""): Shown[] {
  const want = filter.trim().toLowerCase();
  const out: Shown[] = [];
  const byTrace = new Map<string, Shown>();
  for (const one of all) {
    const row: Shown = {
      at: one.at,
      kind: one.kind,
      what: one.what,
      where: one.where ?? "",
      outcome: outcomeOf(one),
      why: one.why ?? "",
      under: [],
    };
    const held = one.trace ? byTrace.get(one.trace) : undefined;
    if (held && UNDER.has(one.kind)) {
      held.under.push(one);
      if (row.outcome === "failed") held.outcome = "failed";
      else if (row.outcome === "slow" && held.outcome !== "failed") held.outcome = "slow";
      continue;
    }
    if (one.trace) byTrace.set(one.trace, row);
    out.push(row);
  }
  const shown = want
    ? out.filter((row) =>
        [row.what, row.where, row.why].join(" ").toLowerCase().includes(want),
      )
    : out;
  return shown.reverse();
}

// clock is the time of day a line happened, which is how you find the press
// you are thinking of. The date is the file it came from.
export function clock(at: string): string {
  const when = new Date(at);
  return Number.isNaN(when.getTime()) ? at : when.toTimeString().slice(0, 8);
}

// text is the shown trail as something to paste into a bug report.
export function text(rows: Shown[]): string {
  return rows
    .map((row) => {
      const bits = [clock(row.at), row.kind, row.what];
      if (row.where) bits.push(`in ${row.where}`);
      if (row.outcome === "failed") bits.push(`FAILED ${row.why}`);
      const line = bits.join("  ");
      const under = row.under.map((one) => `    ${one.what} ${one.ok ? "ok" : one.why} ${one.ms}ms`);
      return [line, ...under].join("\n");
    })
    .join("\n");
}
