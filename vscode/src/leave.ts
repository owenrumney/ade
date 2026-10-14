import type { Tree } from "./client";

// What to do with the conversation on the way out. The ids are what
// POST /api/exit takes, and the transcript stays on disk whichever is picked.
export const PARTINGS = [
  {
    id: "keep",
    label: "Leave it listed",
    detail: "the launcher and the switcher still offer it",
  },
  {
    id: "archive",
    label: "Archive it",
    detail: "out of the way, brought back from the launcher",
  },
  {
    id: "delete",
    label: "Delete it",
    detail: "never listed again; the transcript stays on disk",
  },
] as const;

export function here(trees: Tree[], root: string): Tree | undefined {
  return trees.find((t) => t.path === root);
}

// The repository's own checkout, which is what the launcher prunes against.
export function mainOf(trees: Tree[]): Tree | undefined {
  return trees.find((t) => t.main);
}

// Whether closing could also remove the checkout. The main one never goes:
// closing a session there leaves the repository where it was.
export function removable(trees: Tree[], root: string): boolean {
  const mine = here(trees, root);
  return Boolean(mine) && !mine?.main;
}

// Why it would not go even so. The server itself holds the worktree open, so
// `serving` is not a reason — it is about to stop.
export function keeping(t: Tree | undefined): string {
  if (!t) return "";
  if (t.uncommitted)
    return `${t.uncommitted} uncommitted change${t.uncommitted === 1 ? "" : "s"}`;
  if (t.unpushed)
    return `${t.unpushed} unpushed commit${t.unpushed === 1 ? "" : "s"}`;
  return "";
}

// What the confirmation offers. Unpushed commits can be overridden, because
// the branch keeps them; uncommitted work cannot, because nothing else has it.
export function forceable(t: Tree | undefined): boolean {
  return Boolean(t && !t.uncommitted && t.unpushed);
}
