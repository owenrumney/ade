// What the purge panel draws, worked out from the launcher's sweep. The DOM
// is main.ts. Design: .git/ade/docs/purge-worktrees.md.
import type { Tree } from "../client";
import { age } from "../tree";

// SweepRepo is one repository of the sweep. why is set when ade could not
// read it, and then there are no worktrees to show.
export interface SweepRepo {
  path: string;
  name: string;
  owner?: string;
  why?: string;
  worktrees: Tree[];
}

export interface Sweep {
  repos: SweepRepo[];
  at?: string;
}

// Row is one worktree, as the table draws it.
export interface Row {
  repo: string;
  repoPath: string;
  path: string;
  name: string;
  branch: string;
  lastUsed: string;
  behind: string;
  state: string;
  removable: boolean;
  // forced marks a row only force frees: what the modal must warn about,
  // without reading the words state() chose.
  forced: boolean;
}

// rows flattens the sweep, repository order kept, each repository's worktrees
// oldest first as the scan returned them.
export function rows(sweep: Sweep | undefined, force = false, now = Date.now()): Row[] {
  const out: Row[] = [];
  for (const repo of sweep?.repos ?? []) {
    for (const tree of repo.worktrees ?? []) {
      out.push({
        repo: repo.owner ? `${repo.owner}/${repo.name}` : repo.name,
        repoPath: repo.path,
        path: tree.path,
        name: tree.name,
        branch: tree.branch ?? "",
        lastUsed: when(tree.lastUsed, now),
        behind: tree.behind ? `${tree.behind} behind` : "",
        state: state(tree, force),
        removable: removable(tree, force),
        forced: !tree.removable && removable(tree, force),
      });
    }
  }
  return out;
}

// when dates a row. Go writes a time.Time it never read as the zero time, and
// age() would call that the oldest thing on the machine.
export function when(at: string | undefined, now: number): string {
  if (!at || at.startsWith("0001-01-01")) return "";
  return age(at, now);
}

// removable mirrors the server's rule: force only ever frees a row whose one
// problem is unpushed work. The scan is still the one that decides.
export function removable(tree: Tree, force: boolean): boolean {
  if (tree.removable) return true;
  return force && !tree.serving && !tree.uncommitted && Boolean(tree.unpushed);
}

// state is why a row is kept, in words, or that nothing stands in the way.
// A row force frees says both: what it would lose is still worth reading.
export function state(tree: Tree, force = false): string {
  const why = kept(tree);
  if (tree.removable) return "Removable";
  if (removable(tree, force)) return `Removable · ${why}`;
  return why;
}

// kept is the keep reason as a sentence. The server's own word is the key;
// the counts beside it are what make it mean something.
export function kept(tree: Tree): string {
  if (tree.serving) return `ade is serving it on ${port(tree.serving)}`;
  if (tree.uncommitted) {
    return `${tree.uncommitted} file${tree.uncommitted === 1 ? "" : "s"} uncommitted`;
  }
  if (tree.unpushed) {
    return tree.unpushed === 1
      ? "1 commit is only here"
      : `${tree.unpushed} commits are only here`;
  }
  // Not one of the three: Scan puts a git failure in the same field.
  return tree.keep ?? "";
}

// port is the serving address shortened to what tells one ade from another.
export function port(url: string): string {
  const found = /:(\d+)/.exec(url.replace(/^https?:\/\//, ""));
  return found ? `:${found[1]}` : url;
}

// chosen is the ticked rows that are actually removable, so a stale tick on a
// row that has since gained a session is dropped before anything is asked.
export function chosen(all: Row[], ticked: Set<string>): Row[] {
  return all.filter((row) => ticked.has(row.path) && row.removable);
}

// selectable is every row Remove would take: what the header's tick picks.
export function selectable(all: Row[]): string[] {
  return all.filter((row) => row.removable).map((row) => row.path);
}

// byRepo groups what to remove, because prune is per repository: a failure in
// one leaves the rest done.
export function byRepo(picked: Row[]): { repo: string; paths: string[] }[] {
  const out: { repo: string; paths: string[] }[] = [];
  for (const row of picked) {
    const found = out.find((g) => g.repo === row.repoPath);
    if (found) found.paths.push(row.path);
    else out.push({ repo: row.repoPath, paths: [row.path] });
  }
  return out;
}

// confirm is what the modal says before anything goes: the count, the
// repositories it spans, and what force is about to lose.
export function confirm(picked: Row[]): string {
  const repos = new Set(picked.map((row) => row.repoPath));
  const parts = [
    `${picked.length} worktree${picked.length === 1 ? "" : "s"} in ${repos.size} repositor${repos.size === 1 ? "y" : "ies"}.`,
  ];
  const forced = picked.filter((row) => row.forced);
  if (forced.length > 0) {
    parts.push(
      `${forced.length} of them hold${forced.length === 1 ? "s" : ""} commits that are nowhere else. The branch is kept; the checkout is not.`,
    );
  }
  parts.push("Branches are never deleted.");
  return parts.join("\n\n");
}

// told is what to say after a sweep's worth of prunes: what went, and every
// one that would not with its own reason. A refusal said nowhere else is lost.
export function told(
  removed: string[],
  keptBack: { path: string; why: string }[],
): string {
  if (removed.length === 0 && keptBack.length === 0) return "";
  if (keptBack.length === 0) return `Removed ${removed.length}.`;
  const why = keptBack.map((row) => `${base(row.path)}: ${row.why}`).join("; ");
  return `Removed ${removed.length}. Kept ${why}`;
}

// summary heads the table: how much is here, and how much of it could go.
export function summary(all: Row[]): string {
  if (all.length === 0) return "No linked worktrees on this machine.";
  const free = all.filter((row) => row.removable).length;
  return `${all.length} worktree${all.length === 1 ? "" : "s"} · ${free} removable`;
}

function base(path: string): string {
  return path.split("/").pop() ?? path;
}
