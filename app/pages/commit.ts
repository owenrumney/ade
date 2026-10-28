import type { CommitDraft, Worktree } from "./client";

// Message is git's shape: a subject, a blank line, then the rest. The SCM
// input box is one string, so the split happens here rather than at the server.
export interface Message {
  title: string;
  body: string;
}

export function split(input: string): Message {
  const lines = input.replace(/\r\n/g, "\n").split("\n");
  const title = (lines.shift() ?? "").trim();
  while (lines.length && lines[0].trim() === "") lines.shift();
  return { title, body: lines.join("\n").trim() };
}

// join is the reverse, for putting a written draft into the input box.
export function join(draft: CommitDraft): string {
  return draft.body ? `${draft.title}\n\n${draft.body}` : draft.title;
}

// Row is one path in a group, with the letter git gave it.
export interface Row {
  path: string;
  status: string;
}

// groups is what the two resource groups hold. Staged comes back as bare
// paths, so the letter is only known for what is not staged.
export function groups(worktree: Worktree | null | undefined): {
  staged: Row[];
  changed: Row[];
} {
  const staged = (worktree?.staged ?? []).map((path) => ({ path, status: "" }));
  const changed = (worktree?.unstaged ?? []).map((c) => ({
    path: c.path,
    status: c.status,
  }));
  return { staged, changed };
}

// icon is the codicon against a row. The `letter` decoration the built-in git
// view uses is a proposed API, and these read the same at a glance.
const ICONS: Record<string, string> = {
  added: "diff-added",
  untracked: "diff-added",
  modified: "diff-modified",
  deleted: "diff-removed",
  renamed: "diff-renamed",
  copied: "diff-renamed",
  conflicted: "warning",
};

export function icon(status: string): string {
  return ICONS[status.toLowerCase()] ?? "diff-modified";
}

// ahead is what the branch line says under the input box. Nothing when the
// branch has no upstream: "0 ahead of nothing" is not a fact about anything.
export function ahead(worktree: Worktree | null | undefined): string {
  if (!worktree?.upstream || worktree.ahead <= 0) return "";
  return `${worktree.ahead} ahead of ${worktree.upstream}`;
}

// committable reports whether the button should do anything. An amend with no
// title keeps the old message, which is the one case an empty box is allowed.
export function committable(
  input: string,
  staged: Row[],
  amend: boolean,
): boolean {
  if (amend) return true;
  return split(input).title !== "" && staged.length > 0;
}

// rejected reads git's refusal to fast-forward out of a push error. An amend
// always lands here, so the one way past it has to be offered rather than typed.
export function rejected(why: string): boolean {
  return /non-fast-forward|fetch first|\[rejected\]/i.test(why);
}

// stageFirst: nothing staged is "describe my work"; a staged subset is a
// decision already made, and widening it describes more than you chose.
export function stageFirst(staged: number, changed: number): boolean {
  return staged === 0 && changed > 0;
}
