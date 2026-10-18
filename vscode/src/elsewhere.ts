import type { Checkout, Repo } from "./launcher";

// Row is somewhere to start on another machine: a checkout it already has, or
// a repository it has not got, which opening clones.
export interface Row {
  // dir is what /api/open takes: a path for a checkout it has, an origin for
  // one it has not, which is what makes the launcher clone it.
  dir: string;
  label: string;
  description: string;
  detail: string;
  clone: boolean;
}

// rows is where work could start: checkouts holding sessions first, then an
// empty one per repository and branch, then what the machine has not got.
export function rows(repos: Repo[]): Row[] {
  const busy: { row: Row; last: string }[] = [];
  const idle: Row[] = [];
  const missing: Row[] = [];
  for (const repo of repos) {
    const checkouts = repo.checkouts ?? [];
    if (checkouts.length === 0) {
      missing.push({
        dir: repo.path,
        label: repo.name,
        description: "not there yet",
        detail: `clone ${repo.path}`,
        clone: true,
      });
      continue;
    }
    for (const checkout of checkouts.filter(held)) busy.push(row(checkout));
    idle.push(...spare(checkouts.filter((c) => !held(c))));
  }
  busy.sort(byRecent);
  return [...busy.map(({ row }) => row), ...idle, ...missing];
}

// held is a checkout with conversations in it. One is not interchangeable
// with another, so it always gets a row of its own.
function held(checkout: Checkout): boolean {
  return (checkout.sessions ?? []).length > 0;
}

// spare collapses a repository's empty checkouts to a row per branch: sixteen
// fresh clones of one branch are one choice, not sixteen.
function spare(checkouts: Checkout[]): Row[] {
  const byBranch = new Map<string, Checkout[]>();
  for (const checkout of checkouts) {
    const key = checkout.branch || "";
    byBranch.set(key, [...(byBranch.get(key) ?? []), checkout]);
  }
  return [...byBranch.values()].map((same) => {
    const row = plain(same[0]);
    if (same.length === 1) return row;
    // Named, because a row that silently stands for fifteen others is a lie.
    return {
      ...row,
      description: `${row.description} · 1 of ${same.length} empty checkouts`,
    };
  });
}

function row(checkout: Checkout): { row: Row; last: string } {
  const sessions = checkout.sessions ?? [];
  const count = `${sessions.length} session${sessions.length === 1 ? "" : "s"}`;
  const last =
    sessions
      .map((s) => s.lastUsed ?? "")
      .sort()
      .pop() ?? "";
  return {
    row: {
      ...plain(checkout),
      description: [checkout.branch, count].filter(Boolean).join(" · "),
    },
    last,
  };
}

function plain(checkout: Checkout): Row {
  return {
    dir: checkout.path,
    label: checkout.name,
    description: checkout.branch ?? "",
    detail: checkout.path,
    clone: false,
  };
}

function byRecent(a: { last: string }, b: { last: string }): number {
  return b.last.localeCompare(a.last);
}
