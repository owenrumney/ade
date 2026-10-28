// What the worktree panel draws, kept out of the page so it can be tested:
// the order, the rails and the words. The relations are the server's.
import type { Overlap, OverlapEdge, OverlapNode, Rev } from "../client";
import { facts } from "../tree";

export const KIND_ABSORBED = "absorbed";
export const KIND_SHARES = "shares";
export const KIND_DUPLICATES = "duplicates";

// TRUNK is the base's own rail, which no worktree is given.
export const TRUNK = -1;

// Lane is one row: a worktree, what it says about itself, the verdict its
// commits earn it, and the rail it rides.
export interface Lane {
  node: OverlapNode;
  label: string;
  // state is the scan's own facts, without the branch: the label is that.
  state: string;
  // commits are oldest first. Time runs down the drawing, from the base at
  // the top to the tips, and a list that ran the other way would fight it.
  commits: Rev[];
  // verdict is why this one could go, in words, or empty when it could not.
  verdict: string;
  removable: boolean;
  main: boolean;

  // column is where this rail sits, TRUNK for the checkout that is the base.
  column: number;
  // from is the lane this rail leaves, TRUNK when it leaves the base, and
  // shared how many commits it ran alongside before it did.
  from: number;
  shared: number;
}

// Tie is a link between two rails that the rails themselves cannot say: the
// same patch committed twice is two commits, not one line.
export interface Tie {
  from: number;
  to: number;
  count: number;
  label: string;
  column: number;
}

// lanes is the drawing's rows: the repository's own checkout, then each
// worktree under the one it left, most recently used first among siblings.
export function lanes(o: Overlap | undefined): Lane[] {
  const removable = new Set(o?.removable ?? []);
  const absorbed = absorbedBy(o);

  const rows: Lane[] = (o?.nodes ?? []).map((node) => ({
    node,
    label: node.branch || node.name,
    state: facts(node).join(" · "),
    commits: [...(node.commits ?? [])].reverse(),
    verdict: verdictOf(node, absorbed, where(o)),
    removable: removable.has(node.path),
    main: Boolean(node.main),
    column: TRUNK,
    from: TRUNK,
    shared: 0,
  }));
  if (rows.length === 0) return rows;

  const forks = forkedFrom(rows, o);
  const ordered = order(rows, forks);
  return railed(ordered, forks);
}

// forkedFrom is which other worktree each one left, by path: the one its work
// is all on, else the one it shares most with. Only ever a longer branch.
export function forkedFrom(
  rows: Lane[],
  o: Overlap | undefined,
): Map<string, { path: string; shared: number }> {
  const held = new Map(rows.map((row) => [row.node.path, row]));
  const out = new Map<string, { path: string; shared: number }>();

  const shared = new Map<string, number>();
  for (const edge of o?.edges ?? [])
    if (edge.kind === KIND_SHARES) shared.set(pair(edge.from, edge.to), edge.count ?? 0);
  // Absorbed first and read whole: its work being on another rail is a
  // stronger answer than merely sharing most with it.
  for (const edge of o?.edges ?? []) {
    if (edge.kind !== KIND_ABSORBED || !held.has(edge.to) || out.has(edge.from))
      continue;
    out.set(edge.from, {
      path: edge.to,
      shared: shared.get(pair(edge.from, edge.to)) ?? 0,
    });
  }

  for (const row of rows) {
    if (row.main || out.has(row.node.path)) continue;
    let best: { path: string; shared: number } | undefined;
    for (const other of rows) {
      if (other === row || other.main) continue;
      const n = shared.get(pair(row.node.path, other.node.path)) ?? 0;
      // Strictly longer, so a pair can never each be the other's parent.
      if (n === 0 || !longer(other, row)) continue;
      if (!best || n > best.shared) best = { path: other.node.path, shared: n };
    }
    if (best) out.set(row.node.path, best);
  }
  return out;
}

// order walks the forks from the base down, so a branch is drawn under the
// one it left. Siblings keep the tree's order: most recently used first.
export function order(
  rows: Lane[],
  forks: Map<string, { path: string; shared: number }>,
): Lane[] {
  const byRecency = [...rows].sort(
    (a, b) =>
      Number(b.main) - Number(a.main) ||
      since(b.node) - since(a.node) ||
      a.label.localeCompare(b.label),
  );
  const children = new Map<string, Lane[]>();
  const roots: Lane[] = [];
  for (const row of byRecency) {
    const fork = row.main ? undefined : forks.get(row.node.path);
    if (!fork) {
      roots.push(row);
      continue;
    }
    children.set(fork.path, [...(children.get(fork.path) ?? []), row]);
  }

  const out: Lane[] = [];
  const seen = new Set<string>();
  const walk = (row: Lane) => {
    if (seen.has(row.node.path)) return;
    seen.add(row.node.path);
    out.push(row);
    for (const child of children.get(row.node.path) ?? []) walk(child);
  };
  roots.forEach(walk);
  // A fork naming a worktree that never got walked would strand its children.
  byRecency.forEach(walk);
  return out;
}

// railed gives each lane the rail it rides: where it leaves, and the
// innermost column no rail alive over the same rows is already using.
export function railed(
  rows: Lane[],
  forks: Map<string, { path: string; shared: number }>,
): Lane[] {
  const at = new Map(rows.map((row, i) => [row.node.path, i]));
  const live: { top: number; bottom: number }[][] = [];

  rows.forEach((row, i) => {
    const fork = row.main ? undefined : forks.get(row.node.path);
    const from = fork ? (at.get(fork.path) ?? TRUNK) : TRUNK;
    row.from = from;
    row.shared = fork?.shared ?? 0;
    // Holding nothing of its own, it is the base: a dot on the trunk rather
    // than the longest rail on the drawing for the branch with the least on it.
    if (row.main || (from === TRUNK && row.commits.length === 0)) {
      row.column = TRUNK;
      return;
    }
    // A rail lives below the row it leaves, not on it: at the junction it is
    // still riding the other rail, so the two want no column between them.
    const span = { top: (from === TRUNK ? -1 : from) + 0.5, bottom: i };
    let col = 0;
    while (live[col]?.some((other) => crosses(span, other))) col++;
    live[col] = [...(live[col] ?? []), span];
    row.column = col;
  });
  return rows;
}

// ties is what the rails cannot draw: a patch committed on two branches is
// two commits, so there is no line between them to follow.
export function ties(o: Overlap | undefined, rows: Lane[]): Tie[] {
  const at = new Map(rows.map((row, i) => [row.node.path, i]));
  const found: Tie[] = [];
  for (const edge of o?.edges ?? []) {
    if (edge.kind !== KIND_DUPLICATES) continue;
    const from = at.get(edge.from);
    const to = at.get(edge.to);
    if (from === undefined || to === undefined) continue;
    const count = edge.count ?? 0;
    found.push({
      from,
      to,
      count,
      label: count === 1 ? "1 copied" : `${count} copied`,
      column: 0,
    });
  }
  found.sort((a, b) => reach(b) - reach(a) || a.from - b.from);
  const live: Tie[][] = [];
  for (const tie of found) {
    let col = 0;
    while (live[col]?.some((other) => crosses(ends(tie), ends(other)))) col++;
    tie.column = col;
    live[col] = [...(live[col] ?? []), tie];
  }
  return found;
}

// summary is the one line above the drawing: how many worktrees, and the two
// findings worth acting on.
export function summary(o: Overlap | undefined): string {
  const rows = o?.nodes ?? [];
  const linked = rows.filter((node) => !node.main);
  if (linked.length === 0) return "No worktrees besides the repository itself.";

  const parts = [`${linked.length} worktree${linked.length === 1 ? "" : "s"}`];
  const spare = (o?.removable ?? []).length;
  if (spare) parts.push(`${spare} could go`);
  const copies = (o?.edges ?? [])
    .filter((edge) => edge.kind === KIND_DUPLICATES)
    .reduce((sum, edge) => sum + (edge.count ?? 0), 0);
  if (copies)
    parts.push(`${copies} patch${copies === 1 ? "" : "es"} in two places`);
  return parts.join(" · ");
}

// where is the base, as a label for the rail everything leaves.
export function where(o: Overlap | undefined): string {
  return o?.baseRef || o?.baseBranch || "the base";
}

// leaves says where a rail starts, for the row to say it in words: a rail
// drawn from another rail is only honest about which, not about where.
export function leaves(row: Lane, rows: Lane[]): string {
  if (row.main || row.from === TRUNK) return "";
  const parent = rows[row.from];
  if (!parent) return "";
  const after = row.shared
    ? `, after ${row.shared} commit${row.shared === 1 ? "" : "s"}`
    : "";
  return `from ${parent.label}${after}`;
}

// short is a SHA as a commit list shows it.
export function short(sha: string): string {
  return sha.slice(0, 7);
}

// marked is a commit's mark in an expanded lane: whether the same commit, or
// the same patch under another SHA, is held by another worktree too.
export function marked(rev: Rev, rows: Lane[], mine: string): string {
  for (const row of rows) {
    if (row.node.path === mine) continue;
    for (const other of row.commits) {
      if (other.sha === rev.sha) return `also on ${row.label}`;
      if (rev.patchId && other.patchId === rev.patchId)
        return `copied on ${row.label}`;
    }
  }
  return "";
}

function pair(a: string, b: string): string {
  return a < b ? `${a}\u0000${b}` : `${b}\u0000${a}`;
}

function longer(a: Lane, b: Lane): boolean {
  if (a.commits.length !== b.commits.length)
    return a.commits.length > b.commits.length;
  return a.node.path < b.node.path;
}

function absorbedBy(o: Overlap | undefined): Map<string, string> {
  const names = new Map(
    (o?.nodes ?? []).map((node) => [node.path, node.branch || node.name]),
  );
  const out = new Map<string, string>();
  for (const edge of o?.edges ?? [])
    if (edge.kind === KIND_ABSORBED && !out.has(edge.from))
      out.set(edge.from, names.get(edge.to) ?? edge.to);
  return out;
}

function verdictOf(node: OverlapNode, absorbed: Map<string, string>, base: string): string {
  if (node.why) return "could not be read";
  if (node.main) return "";
  const holder = absorbed.get(node.path);
  if (holder) return `all of its work is on ${holder}`;
  if (node.redundant) return "holds nothing of its own";
  // Its own wording: the commit count beside it says the opposite.
  if (node.landed) return `already on ${base}, squashed or rebased`;
  return "";
}

function since(node: OverlapNode): number {
  const at = Date.parse(node.lastUsed ?? "");
  return Number.isNaN(at) ? 0 : at;
}

function ends(tie: Tie): { top: number; bottom: number } {
  return {
    top: Math.min(tie.from, tie.to),
    bottom: Math.max(tie.from, tie.to),
  };
}

function reach(tie: Tie): number {
  return Math.abs(tie.to - tie.from);
}

function crosses(
  a: { top: number; bottom: number },
  b: { top: number; bottom: number },
): boolean {
  return a.top <= b.bottom && b.top <= a.bottom;
}

// A lane's edges are not needed by the drawing any more — the rails say what
// shares meant — but a row still names what it is tied to.
export function tiedTo(ties: Tie[], i: number): Tie[] {
  return ties.filter((tie) => tie.from === i || tie.to === i);
}

export type { OverlapEdge };

// Step is one thing the consolidate run does, in the order it must happen:
// the blockers first, then the removal.
export interface Step {
  path: string;
  label: string;
  what: "push" | "close" | "prune";
}

// Blocker is what stands between a worktree and removal, and whether the
// panel can clear it. Uncommitted work it never clears: see the design doc.
export interface Blocker {
  why: string;
  // clear is the verb to run first, or none when there is nothing to run.
  clear?: "push" | "close";
  // stuck is a blocker only you can deal with, so the row cannot be ticked.
  stuck: boolean;
}

// Uncommitted first, not merely first found: a served and dirty worktree
// would otherwise offer a close the prune behind it refuses anyway.
export function blocker(row: Lane): Blocker | undefined {
  const t = row.node;
  if (t.uncommitted) return { why: `${t.uncommitted} uncommitted`, stuck: true };
  if (t.serving) return { why: "an ade is serving it", clear: "close", stuck: false };
  if (t.unpushed) return { why: `${t.unpushed} unpushed`, clear: "push", stuck: false };
  return undefined;
}

// clearable is whether a row can be ticked at all: the main checkout never,
// one git could not be read never, and one holding work only it has never.
export function clearable(row: Lane): boolean {
  if (row.main || row.node.why) return false;
  return !blocker(row)?.stuck;
}

// wanted is what arrives ticked: the ones the commits prove are spare, minus
// any whose blocker only you can clear.
export function wanted(rows: Lane[]): string[] {
  return rows
    .filter((row) => row.removable && clearable(row))
    .map((row) => row.node.path);
}

// plan is the run: every blocker cleared first, then one prune for all of
// them, because git serialises worktree removal on its own admin files.
export function plan(rows: Lane[], chosen: string[]): Step[] {
  const picked = rows.filter(
    (row) => chosen.includes(row.node.path) && clearable(row),
  );
  const steps: Step[] = [];
  for (const row of picked) {
    const stop = blocker(row)?.clear;
    if (stop)
      steps.push({
        path: row.node.path,
        label: row.label,
        what: stop,
      });
  }
  if (picked.length)
    steps.push({
      path: picked.map((row) => row.node.path).join("\n"),
      label: picked.map((row) => row.label).join(", "),
      what: "prune",
    });
  return steps;
}

// said is the button: what it will do, and to how many.
export function said(rows: Lane[], chosen: string[]): string {
  const n = rows.filter(
    (row) => chosen.includes(row.node.path) && clearable(row),
  ).length;
  return n === 0 ? "Nothing selected" : `Remove ${n}`;
}

// stuckSaid is the warning under the button: what was ticked for you and
// could not be, so a row left out is explained rather than just absent.
export function stuckSaid(rows: Lane[]): string {
  const stuck = rows.filter((row) => row.removable && !clearable(row));
  if (stuck.length === 0) return "";
  const names = stuck.map((row) => row.label).join(", ");
  return stuck.length === 1
    ? `${names} could go, but has work only it holds. Open it to deal with that.`
    : `${names} could go, but each has work only it holds.`;
}

// mergeable is the rail this one's work could fold into: the one it left,
// while it still holds work of its own. The design doc has the limits.
export function mergeable(row: Lane, rows: Lane[]): Lane | undefined {
  if (row.main || row.node.why || row.removable) return undefined;
  if (row.commits.length === 0 || row.node.uncommitted) return undefined;
  // Every rail leaves one, and the trunk's is the repository's own checkout:
  // a branch off the base merges there, which is the common case.
  const into = row.from === TRUNK ? rows.find((one) => one.main) : rows[row.from];
  if (!into || into === row || into.node.why) return undefined;
  return into.node.uncommitted ? undefined : into;
}

// mergeSaid is the button, naming where the work goes. A button that said
// only "Merge" would not say what it was about to write to.
export function mergeSaid(into: Lane): string {
  return `Merge into ${into.label} and remove`;
}
