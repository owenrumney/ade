import type { Repo } from "./launcher";

// Where a ticket can be started. `ade create --linear` runs in a repository's
// main checkout, so a worktree is not a choice even when a window is on one.
export type Choice = {
  label: string;
  description: string;
  detail?: string;
  path: string;
  here: boolean;
};

export function choices(repos: Repo[], root: string | undefined): Choice[] {
  const out = repos.map((repo) => ({
    label: repo.owner ? `${repo.owner}/${repo.name}` : repo.name || repo.path,
    description: repo.path === root ? "this window" : "",
    detail: repo.path,
    path: repo.path,
    here: repo.path === root,
  }));
  // This window's repository first: starting a ticket where you already are is
  // the common case, and the rest of the list is a machine's worth of projects.
  out.sort((a, b) => Number(b.here) - Number(a.here));
  return out;
}

// A ticket started outside this window's repository makes a worktree ade is
// not attached to, so the confirmation says where the work will be.
export function detail(title: string, path: string, here: boolean): string {
  return here
    ? `${title}\n\nA branch and a session in ${path}.`
    : `${title}\n\nA branch and a session in ${path}. This window stays where it is.`;
}
