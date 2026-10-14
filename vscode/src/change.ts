import type { Diff, DiffFile } from "./client";

// The scopes the Diff view switches between, in the order the server's own
// switcher uses. The ids are what POST /api/scope takes.
export const SCOPES = [
  {
    id: "branch",
    label: "Branch",
    detail: "everything on the branch, committed or not",
  },
  { id: "committed", label: "Committed", detail: "what has been committed" },
  {
    id: "uncommitted",
    label: "Uncommitted",
    detail: "what is still pending commit",
  },
] as const;

export function scopeLabel(scope: string | undefined): string {
  return SCOPES.find((s) => s.id === scope)?.label ?? "Branch";
}

// The view's own subtitle: how many files, and how much moved.
export function summary(diff: Diff | undefined): string {
  if (!diff) return "";
  const files = diff.files ?? [];
  if (files.length === 0) return diff.note || "no changes";
  const count = files.length === 1 ? "1 file" : `${files.length} files`;
  const add = files.reduce((n, f) => n + f.additions, 0);
  const cut = files.reduce((n, f) => n + f.deletions, 0);
  return `${count} +${add} −${cut}`;
}

// A file's own tally, for the row. A binary file has no lines to count and
// saying "+0 −0" of one is worse than saying nothing.
export function tally(file: DiffFile): string {
  if (file.binary) return "binary";
  return `+${file.additions} −${file.deletions}`;
}

// The letter git uses, and the decoration badge. Untracked counts as added:
// to a reader of the diff it is the same news.
export function letter(file: DiffFile): string {
  if (file.untracked) return "U";
  const first = (file.status || "M").charAt(0).toUpperCase();
  return "AMDRC".includes(first) ? first : "M";
}

// Green for what arrived, red for what went, amber for a move. Theme colour
// ids, so the host decides the actual colour.
const COLOURS: Record<string, string> = {
  A: "gitDecoration.addedResourceForeground",
  U: "gitDecoration.untrackedResourceForeground",
  D: "gitDecoration.deletedResourceForeground",
  R: "gitDecoration.renamedResourceForeground",
  C: "gitDecoration.renamedResourceForeground",
  M: "gitDecoration.modifiedResourceForeground",
};

export function colour(file: DiffFile): string {
  return COLOURS[letter(file)] ?? COLOURS.M;
}

// A deleted file has no right side and an added one no left, so opening
// either against the working tree would compare something with nothing twice.
export function sides(file: DiffFile): { left: boolean; right: boolean } {
  const kind = letter(file);
  return { left: kind !== "A" && kind !== "U", right: kind !== "D" };
}

// Alphabetical by path. A diff is read by looking for a file you have in
// mind, not by size: ordering by churn moves it every time you look.
export function rank(a: DiffFile, b: DiffFile): number {
  return a.path.localeCompare(b.path);
}

export function name(path: string): string {
  return path.split("/").pop() ?? path;
}

// The directory, for the row's second column. Empty at the top level rather
// than "." — a dot there reads as a hidden file.
export function folder(path: string): string {
  const cut = path.lastIndexOf("/");
  return cut < 0 ? "" : path.slice(0, cut);
}

// A row of the tree: a directory, or a file in one.
export type Node =
  | { kind: "dir"; path: string; label: string; kids: Node[] }
  | { kind: "file"; file: DiffFile };

// nest turns the flat list into directories, folding single-child chains into
// one row — `internal/web`, not `internal` then `web`.
export function nest(files: DiffFile[]): Node[] {
  const root: Dir = { kids: new Map(), files: [] };
  for (const file of [...files].sort(rank)) {
    let at = root;
    const parts = file.path.split("/");
    for (const part of parts.slice(0, -1)) {
      let next = at.kids.get(part);
      if (!next) {
        next = { kids: new Map(), files: [] };
        at.kids.set(part, next);
      }
      at = next;
    }
    at.files.push(file);
  }
  return emit(root, "");
}

interface Dir {
  kids: Map<string, Dir>;
  files: DiffFile[];
}

function emit(dir: Dir, at: string): Node[] {
  const out: Node[] = [];
  for (const [part, kid] of [...dir.kids].sort((a, b) =>
    a[0].localeCompare(b[0]),
  )) {
    out.push(fold(kid, at ? `${at}/${part}` : part, part));
  }
  for (const file of dir.files) out.push({ kind: "file", file });
  return out;
}

// One child and nothing else of its own is not a level worth a row.
function fold(dir: Dir, path: string, label: string): Node {
  if (dir.kids.size === 1 && dir.files.length === 0) {
    const [part, kid] = [...dir.kids][0];
    return fold(kid, `${path}/${part}`, `${label}/${part}`);
  }
  return { kind: "dir", path, label, kids: emit(dir, path) };
}

// What the diff editor's tab is called. Both ends named, because the whole
// point of the view is that the left end changes with the scope.
export function title(file: DiffFile, diff: Diff | undefined): string {
  const was =
    file.oldPath && file.oldPath !== file.path
      ? `${name(file.oldPath)} → `
      : "";
  const ends =
    diff?.mode === "uncommitted"
      ? "HEAD ↔ working tree"
      : `${diff?.base ?? "base"} ↔ ${diff?.head || "working tree"}`;
  return `${was}${name(file.path)} (${ends})`;
}

export const BLOB = "ade-blob";
export const REV = "ade-rev";

// A file as one commit left it, named "blame@11f7987.go": two tabs of one file
// are otherwise both "blame.go", and the language comes off the extension.
export function revUri(sha: string, path: string): { path: string } {
  const cut = path.lastIndexOf(".");
  const slash = path.lastIndexOf("/");
  if (cut <= slash + 1) return { path: `/${path}@${sha}` };
  return { path: `/${path.slice(0, cut)}@${sha}${path.slice(cut)}` };
}

// atOf is revUri read back, splitting on the last @ so a file whose own name
// has one still resolves. An empty answer is a uri that is not one of ours.
export function atOf(uri: { path: string }): { sha: string; path: string } {
  const whole = uri.path.replace(/^\//, "");
  const cut = whole.lastIndexOf(".");
  const slash = whole.lastIndexOf("/");
  const [stem, ext] =
    cut <= slash + 1
      ? [whole, ""]
      : [whole.slice(0, cut), whole.slice(cut)];
  const at = stem.lastIndexOf("@");
  if (at < 0) return { sha: "", path: "" };
  return { sha: stem.slice(at + 1), path: stem.slice(0, at) + ext };
}

// Every open side, which is what has to be read again when the diff moves.
export function staleSides<T extends { scheme: string }>(open: T[]): T[] {
  return open.filter((uri) => uri.scheme === BLOB);
}
