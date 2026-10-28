import type { Tree } from "./client";

// line is what a worktree row says about itself: the branch, then whatever
// stands in the way of removing it. A clean one says so in one word.
export function line(t: Tree): string {
  return [...(t.branch ? [t.branch] : []), ...facts(t)].join(" · ");
}

// facts is the same without the branch, for a surface whose row is already
// labelled with it: saying it twice is what a tree row cannot afford.
export function facts(t: Tree): string[] {
  const parts: string[] = [];
  if (t.serving) parts.push("in use");
  if (t.uncommitted) parts.push(`${t.uncommitted} uncommitted`);
  if (t.unpushed) parts.push(`${t.unpushed} unpushed`);
  if (t.behind) parts.push(`${t.behind} behind`);
  if (t.merged) parts.push("merged");
  return parts;
}

// listed pairs the two endpoints: the scan skips the main checkout, the
// switch list skips review ones. Main first, then most recently used.
export function listed(main: Tree | undefined, linked: Tree[]): Tree[] {
  const rest = linked
    .filter((t) => !t.main && !(main && t.path === main.path))
    .sort((a, b) => since(b) - since(a) || label(a).localeCompare(label(b)));
  return main ? [{ ...main, main: true }, ...rest] : rest;
}

function since(t: Tree): number {
  const at = Date.parse(t.lastUsed ?? "");
  return Number.isNaN(at) ? 0 : at;
}

// label is the branch, or the folder when a worktree is detached.
export function label(t: Tree): string {
  return t.branch || t.name;
}

// tooltip is everything a row cannot fit.
export function tooltip(t: Tree): string {
  const parts = [label(t), t.path];
  const facts: string[] = [];
  if (t.main) facts.push("the repository's own checkout");
  else if (t.kind) facts.push(t.kind);
  if (t.serving) facts.push(`served by ${t.serving}`);
  if (t.uncommitted) facts.push(`${t.uncommitted} uncommitted`);
  if (t.unpushed) facts.push(`${t.unpushed} unpushed`);
  if (t.behind) facts.push(`${t.behind} behind the base`);
  if (t.merged) facts.push("merged into the base");
  const since = age(t.lastUsed);
  if (since) facts.push(`last used ${since}`);
  if (facts.length) parts.push(facts.join("\n"));
  if (!t.main && !t.removable) parts.push(why(t));
  return parts.join("\n\n");
}

// age is coarsened: the unit that still tells one worktree from the next.
export function age(at: string | undefined, now = Date.now()): string {
  if (!at) return "";
  const then = Date.parse(at);
  if (Number.isNaN(then)) return "";
  const days = Math.floor((now - then) / 86400000);
  if (days >= 60) return `${Math.floor(days / 30)} months ago`;
  if (days >= 1) return days === 1 ? "yesterday" : `${days} days ago`;
  const hours = Math.floor((now - then) / 3600000);
  if (hours >= 1) return hours === 1 ? "an hour ago" : `${hours} hours ago`;
  return "just now";
}

export function icon(t: Tree): string {
  if (t.main) return "repo";
  if (t.serving) return "circle-filled";
  if (t.removable) return "trash";
  return "git-branch";
}

// context decides which verbs a row offers. Only a removable one may be
// pruned, and the main checkout is never either.
export function context(t: Tree): string {
  if (t.main) return "ade.tree.main";
  return t.removable ? "ade.tree.removable" : "ade.tree.kept";
}

// why is the sentence a refusal shows. Keep is the server's own first reason,
// and repeating it beats inventing a second wording for the same fact.
export function why(t: Tree): string {
  if (t.serving)
    return `${t.name} is being served by an ade; close that session first.`;
  return t.keep ? `${t.name}: ${t.keep}` : `${t.name} cannot be removed.`;
}

// forceable is whether "prune anyway" is worth offering. Unpushed work can be
// let go on purpose; uncommitted work never can, whatever the flag says.
export function forceable(t: Tree): boolean {
  return !t.removable && !t.serving && !t.uncommitted && Boolean(t.unpushed);
}

// report is what to say after a prune: what went, and what would not.
export function report(
  removed: string[],
  kept: { path: string; why: string }[],
): string {
  if (kept.length === 0) return `Removed ${removed.length}.`;
  const first = kept[0];
  const rest = kept.length > 1 ? ` (and ${kept.length - 1} more)` : "";
  return `Removed ${removed.length}. Kept ${base(first.path)}: ${first.why}${rest}`;
}

function base(path: string): string {
  return path.split("/").pop() ?? path;
}
