import type { Place, Row } from "./model";
import type { Repo } from "./real";

// A space is a repository kept open. Every worktree of it belongs to one,
// which is why the key is owner/name and never a path.
export interface Space {
  repo: string;
  owner?: string;
  name?: string;
  path?: string;
  branch?: string;
  sessions: number;
  designs?: string[];
  here: boolean;
  // The repository the window is working in. Open because you are in it,
  // so there is nothing to close.
  current?: boolean;
}

export function keyOf(repo: Repo): string {
  return repo.owner && repo.name ? `${repo.owner}/${repo.name}`.toLowerCase() : "";
}

// within is the repositories the open spaces cover, in the spaces' order.
// None open is every repository: opening none is not asking to see less.
export function within(repos: Repo[], open: Space[]): Repo[] {
  if (open.length === 0) return repos;
  const by = new Map(repos.map((r) => [keyOf(r), r]));
  return open.map((s) => by.get(s.repo)).filter((r): r is Repo => Boolean(r));
}

// place is the Spaces list: what is open, and what each holds.
export function spaces(open: Space[]): Place {
  const rows: Row[] = open.map((space) => ({
    id: `space:${space.repo}`,
    icon: space.here ? "space" : "warn",
    tone: space.here ? undefined : ("bad" as const),
    name: space.repo,
    meta: space.here
      ? [
          space.current ? "you are here" : "",
          space.sessions ? `${space.sessions} session${space.sessions === 1 ? "" : "s"}` : "",
          (space.designs ?? []).length ? `${space.designs?.length} designs` : "",
          space.branch,
        ]
          .filter(Boolean)
          .join(" · ")
      : "not cloned here",
    acts: space.current
      ? []
      : space.here
        ? [{ label: "Close", what: "space-close" as const, name: space.repo }]
        : [{ label: "Clone", what: "space-clone" as const, name: space.repo }],
  }));
  return {
    id: "spaces",
    title: "Spaces",
    icon: "space",
    rows,
    note: rows.length ? "" : "No spaces open. ⌘K, “Open a space”.",
  };
}
