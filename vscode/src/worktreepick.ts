import type { Tree } from "./client";
import { facts, label as labelOf, listed, why } from "./tree";

// Row is one worktree as the picker draws it. facts rather than line: the row
// is already labelled with the branch, and saying it twice wastes the width.
export interface Row {
  tree: Tree;
  label: string;
  description: string;
  detail: string;
}

export function rows(main: Tree | undefined, linked: Tree[]): Row[] {
  return listed(main, linked).map((tree) => ({
    tree,
    label: labelOf(tree),
    description: [tree.main ? "the repository's own checkout" : tree.kind]
      .concat(facts(tree))
      .filter(Boolean)
      .join(" · "),
    detail: tree.path,
  }));
}

export interface Act {
  id: "open" | "session" | "remove";
  label: string;
  detail: string;
}

// The repository's own checkout is never removed, and one that cannot be
// removed still offers it: the command asks, and says what is in the way.
export function acts(tree: Tree): Act[] {
  const out: Act[] = [
    { id: "open", label: "Open the folder", detail: tree.path },
    {
      id: "session",
      label: "New session here",
      detail: "start an agent in this worktree",
    },
  ];
  if (!tree.main)
    out.push({
      id: "remove",
      label: "Remove it",
      detail: tree.removable ? "the worktree and its branch" : why(tree),
    });
  return out;
}
