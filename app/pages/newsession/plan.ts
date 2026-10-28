import { branchFor, tilde, worktreeFor } from "../wouldmake";
import type { Chosen, Make, Plan } from "./view";
import { names, rowFor } from "./view";

// plan is what starting would create, read back before you commit to it: one
// branch, a worktree per ticked repository, and the document in home.
export function plan(chosen: Chosen, home = ""): Plan {
  const row = rowFor(chosen.mode);
  const from = names(chosen);
  const branch = row.picks ? branchFor(from) : "";
  const makes: Make[] = [];
  if (row.picks) {
    for (const [i, path] of chosen.repos.entries()) {
      makes.push({
        repo: path.split("/").filter(Boolean).pop() ?? path,
        path: tilde(branch ? worktreeFor(path, branch) : path, home),
        home: i === 0,
      });
    }
  }
  // A document already written keeps its own name: ade reuses it rather than
  // scaffolding a second, empty one beside it.
  const written = row.names === "design" && Boolean(from);
  const doc = written ? from : branch ? `${branch}.md` : "";
  return { branch, doc, written, carried: written && chosen.ship, makes };
}
