import type { Repo } from "./launcher";

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

// rows is a machine's checkouts first, then what it has not got. The launcher
// sends both in one list, told apart by having no checkouts.
export function rows(repos: Repo[]): Row[] {
  const here: Row[] = [];
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
    for (const checkout of checkouts)
      here.push({
        dir: checkout.path,
        label: checkout.name,
        description: checkout.branch ?? "",
        detail: checkout.path,
        clone: false,
      });
  }
  return [...here, ...missing];
}
