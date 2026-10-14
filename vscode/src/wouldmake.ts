import * as vscode from "vscode";
import { basename, dirname, join, posix, sep } from "node:path";
import { homedir } from "node:os";
import { gitDir } from "./discover";

// wouldMake is what `ade create <name>` makes of the name, shown as it is
// typed: the branch, and the worktree beside the checkout that gets it.
export function wouldMake(
  name: string,
  root: string,
): vscode.InputBoxValidationMessage | undefined {
  const branch = branchFor(name);
  if (!branch) return undefined;
  return {
    message: `Branch ${branch} · worktree ${tilde(worktreeFor(root, branch))}`,
    severity: vscode.InputBoxValidationSeverity.Info,
  };
}

// branchFor is the server's rule, docs.Filename then web.Slug in lower case:
// the document's file name, unsafe runs as dashes, trimmed of dashes and dots.
export function branchFor(name: string): string {
  const file = filename(name);
  if (!file) return "";
  return posix
    .basename(file)
    .replace(/\.[^.]*$/, "")
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^[-.]+|[-.]+$/g, "")
    .toLowerCase();
}

export function worktreeFor(root: string, branch: string): string {
  const at = root.replace(/\/+$/, "");
  return join(dirname(at), `${basename(at)}-${branch}`);
}

// madeFrom is the checkout a worktree was made from, which is what the server
// puts the next one beside. Anything else is its own main.
export async function madeFrom(folder: string): Promise<string> {
  const dot = await gitDir(folder);
  const made = /^(.*)\/\.git\/worktrees\/[^/]+$/.exec(dot ?? "");
  return made ? made[1] : folder;
}

export function tilde(path: string): string {
  const home = homedir();
  return path === home || path.startsWith(home + sep)
    ? "~" + path.slice(home.length)
    : path;
}

function filename(name: string): string {
  const said = name.replace(/\\/g, "/").trim();
  if (!said) return "";
  const clean = posix
    .normalize(said)
    .replace(/^\.\//, "")
    .replace(/\/+$/, "");
  if (!clean || clean === "." || clean === ".." || clean.startsWith("../"))
    return "";
  if (clean.startsWith("/")) return "";
  return /\.(md|markdown)$/i.test(clean) ? clean : clean + ".md";
}
