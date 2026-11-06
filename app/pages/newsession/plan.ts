import { branchFor, tilde, worktreeFor } from "../wouldmake";
import type { Chosen, Make, Plan } from "./view";
import { names, rowFor } from "./view";

// plan is what starting would create, read back before you commit to it: one
// branch, and a worktree per ticked repository.
export function plan(chosen: Chosen, home = ""): Plan {
  const row = rowFor(chosen.mode);
  const from = names(chosen);
  // A branch already in the repository keeps its own name: slugging it would
  // name a second branch beside the one you picked.
  const opening = row.names === "branch";
  const branch = !row.picks ? "" : opening ? from : branchFor(from);
  const makes: Make[] = [];
  if (row.picks) {
    for (const [i, path] of chosen.repos.entries()) {
      const at = opening && chosen.at ? chosen.at : branch ? worktreeFor(path, branch) : path;
      makes.push({
        repo: path.split("/").filter(Boolean).pop() ?? path,
        path: tilde(at, home),
        home: i === 0,
      });
    }
  }
  return { branch, makes };
}
