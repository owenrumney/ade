// What the commit panel draws, kept out of the page so it can be tested.
export interface Row {
  path: string;
  status: string;
  staged: boolean;
  lines?: Count;
}

export interface Count {
  added: number;
  removed: number;
}

export interface Draft {
  title: string;
  body: string;
  staged: string[] | null;
}

export interface Shape {
  staged: string[] | null;
  unstaged: { path: string; status: string }[] | null;
  lines?: Record<string, Count> | null;
  signing?: { on: boolean; format?: string; key?: string };
}

// rows is every file the commit could carry. A path staged and changed again
// is one row: the checkbox asks "in this commit", which has one answer.
export function rows(tree: Shape | undefined): Row[] {
  const out = new Map<string, Row>();
  for (const path of tree?.staged ?? [])
    out.set(path, { path, status: "staged", staged: true });
  for (const change of tree?.unstaged ?? []) {
    const held = out.get(change.path);
    if (held) {
      held.status = change.status;
      continue;
    }
    out.set(change.path, {
      path: change.path,
      status: change.status,
      staged: false,
    });
  }
  for (const row of out.values()) row.lines = tree?.lines?.[row.path];
  return [...out.values()].sort((a, b) => a.path.localeCompare(b.path));
}

// A one-line change against a thousand-line one would round to nothing, and a
// row that changed something must not look like a row that did not.
const least = 6;

// A row's churn as a length: two percentages of one cell, plus the numbers.
export interface Bar {
  added: number;
  removed: number;
  title: string;
}

// bar measures against the busiest file in the list, linearly, so the lengths
// can be read against each other rather than only against themselves.
export function bar(row: Row, widest: number): Bar {
  if (!row.lines) return { added: 0, removed: 0, title: "" };
  const { added, removed } = row.lines;
  const title = `+${added} −${removed}`;
  const total = added + removed;
  if (total === 0) return { added: 0, removed: 0, title };
  const width = Math.max(least, (total / Math.max(widest, total)) * 100);
  return {
    added: (width * added) / total,
    removed: width - (width * added) / total,
    title,
  };
}

// widest is the longest bar's worth of change, which sets the scale.
export function widest(all: Row[]): number {
  return all.reduce(
    (most, row) =>
      Math.max(most, (row.lines?.added ?? 0) + (row.lines?.removed ?? 0)),
    0,
  );
}

// where is the path as the row reads it: the directory dimmed, the name not.
export function where(path: string): { dir: string; name: string } {
  const at = path.lastIndexOf("/");
  return at < 0
    ? { dir: "", name: path }
    : { dir: path.slice(0, at + 1), name: path.slice(at + 1) };
}

export function staged(all: Row[]): string[] {
  return all.filter((row) => row.staged).map((row) => row.path);
}

// drifted is whether the message was written for a different set of files:
// a title about work the commit will not carry is why Redraft exists.
export function drifted(draft: Draft | undefined, now: string[]): boolean {
  if (!draft?.title) return false;
  const was = [...(draft.staged ?? [])].sort().join("\n");
  return was !== "" && was !== [...now].sort().join("\n");
}

// loose is the paths not in the commit, which is what Stage all would add.
export function loose(all: Row[]): string[] {
  return all.filter((row) => !row.staged).map((row) => row.path);
}

// idle is whether the panel may be touched. A run in flight owns the message
// and the index; a click during one would be acting on what it is changing.
export function idle(busy: string): boolean {
  return busy === "";
}

export interface Verbs {
  commit: boolean;
  push: boolean;
  createPR: boolean;
  showPR: boolean;
}

// verbs is what the panel offers, given what is checked and what exists. A
// commit needs words and files; the rest follow from there.
export function verbs(
  title: string,
  checked: string[],
  hasPR: boolean,
  ahead: number,
): Verbs {
  const can = title.trim() !== "" && checked.length > 0;
  return {
    commit: can,
    push: can || ahead > 0,
    createPR: !hasPR,
    showPR: hasPR,
  };
}

// signing is the line about the key. Both states are said: a commit you
// thought was signed and was not is the surprise worth preventing.
export function signing(of: Shape["signing"]): { text: string; on: boolean } {
  if (!of?.on) return { text: "Not signed", on: false };
  const how = { ssh: "SSH", x509: "X.509" }[of.format ?? ""] ?? "GPG";
  const key = of.key ? ` · ${of.key}` : "";
  return { text: `Signed with your ${how} key${key}`, on: true };
}

// said is the sentence under the buttons: what a commit would carry.
export function said(checked: string[], all: Row[]): string {
  const left = all.length - checked.length;
  if (checked.length === 0) return "Nothing is checked.";
  const files = `${checked.length} file${checked.length === 1 ? "" : "s"}`;
  return left === 0
    ? `${files}, everything you have changed.`
    : `${files}, leaving ${left} out of this commit.`;
}

// The names gitdiff treats as a base. A branch the panel does not know about
// is not nudged, which costs a hint rather than the facility.
const BASES = ["main", "master"];

export function onBase(branch: string | undefined): boolean {
  return BASES.includes((branch ?? "").trim());
}

// suggest is a branch name read off the message, so the usual case is one
// click. A conventional-commit prefix is scope rather than words, so it goes.
export function suggest(title: string): string {
  const ticket = title.match(/\b[A-Z]+-\d+\b/)?.[0].toLowerCase() ?? "";
  const words = title
    .replace(/^[a-z]+(\([^)]*\))?!?:/i, "")
    .replace(/\[[^\]]*\]/g, " ")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .split("-")
    .filter(Boolean)
    .slice(0, 5)
    .join("-");
  return [ticket, words].filter(Boolean).join("-");
}

// branchable is whether Create may fire. git has the last word on the name;
// this only stops the obvious refusals before the round trip.
export function branchable(name: string, current: string | undefined): boolean {
  const want = name.trim();
  if (want === "" || want === (current ?? "").trim()) return false;
  return !want.startsWith("-") && !/[\s~^:?*[\\]/.test(want);
}

// Rebase is the button that replays this branch on the base. It is drawn
// whenever the base is known, so being up to date is visible rather than blank.
export interface Rebase {
  label: string;
  can: boolean;
  why: string;
}

export function rebase(behind: number | undefined, dirty: number, base: string): Rebase {
  const onto = base || "the base branch";
  if (!behind) return { label: `Rebase onto ${onto}`, can: false, why: "up to date" };
  const many = `${behind} commit${behind === 1 ? "" : "s"} behind`;
  if (dirty > 0) {
    return { label: `Rebase onto ${onto}`, can: false, why: `${many} · commit or stash first` };
  }
  return { label: `Rebase onto ${onto}`, can: true, why: many };
}
