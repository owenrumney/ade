// What the design prune panel draws, worked out from the launcher's sweep.
// The DOM is main.ts. Design: .git/ade/docs/design-doc-prune.md.

// Candidate is one document the sweep matched to a branch that has gone.
export interface Candidate {
  dir: string;
  file: string;
  stem: string;
  branch?: string;
  pr?: string;
  number?: number;
}

// Stale is a document no branch ever matched, dated from its file.
export interface Stale {
  dir: string;
  file: string;
  stem: string;
  touched?: string;
}

export interface WikiState {
  enabled: boolean;
  why?: string;
}

// Prunable is one repository's answer. error is set instead of the groups.
export interface Prunable {
  repo: string;
  merged?: Candidate[];
  abandoned?: Candidate[];
  unmatched?: Stale[];
  error?: string;
  wiki?: WikiState;
}

// Group is one repository's place in the panel, pending until its own call
// lands: a repository that hangs hangs its own row and nothing else.
export interface Group {
  repo: string;
  pending: boolean;
  answer?: Prunable;
}

export type Kind = "merged" | "abandoned" | "unmatched";
export type State = "queued" | "distilling" | "published" | "failed";

// Row is one document, as the table draws it.
export interface Row {
  key: string;
  repo: string;
  dir: string;
  file: string;
  title: string;
  why: string;
  kind: Kind;
  // ticked is the default, before anybody has touched a box: what merged and
  // went is what you came to archive.
  ticked: boolean;
}

// Said is what the panel is told each time anything changes.
export interface Said {
  groups?: Group[];
  wiki?: WikiState;
  busy?: boolean;
  running?: boolean;
  note?: string;
  states?: Record<string, { state: State; page?: string; why?: string }>;
}

// key identifies a row across a redraw. A repository scopes it: two
// repositories can hold a document of the same name.
export function key(repo: string, file: string): string {
  return `${repo}\u0000${file}`;
}

// rows flattens the groups: repository order kept, and within each the three
// groups in the order the panel reads top to bottom.
export function rows(groups: Group[] | undefined, now = Date.now()): Row[] {
  const out: Row[] = [];
  for (const group of groups ?? []) {
    const answer = group.answer;
    if (!answer) continue;
    for (const doc of answer.merged ?? []) out.push(row(group.repo, doc, "merged", now));
    for (const doc of answer.abandoned ?? []) out.push(row(group.repo, doc, "abandoned", now));
    for (const doc of answer.unmatched ?? []) out.push(row(group.repo, doc, "unmatched", now));
  }
  return out;
}

function row(repo: string, doc: Candidate | Stale, kind: Kind, now: number): Row {
  return {
    key: key(repo, doc.file),
    repo,
    dir: doc.dir,
    file: doc.file,
    title: doc.stem || doc.file,
    why: why(doc, kind, now),
    kind,
    ticked: kind === "merged",
  };
}

// why is what qualifies the document, in the words the row carries.
export function why(doc: Candidate | Stale, kind: Kind, now = Date.now()): string {
  if (kind === "merged") {
    const pr = (doc as Candidate).number;
    return pr ? `merged in #${pr}` : "merged";
  }
  if (kind === "abandoned") return "branch gone, never merged";
  return touched((doc as Stale).touched, now) || "no branch matched";
}

// touched dates a document from its mtime: never in a commit, so git has no
// date. Go writes an unread time as the zero time, which is no date at all.
export function touched(at: string | undefined, now = Date.now()): string {
  if (!at || at.startsWith("0001-01-01")) return "";
  const then = Date.parse(at);
  if (Number.isNaN(then)) return "";
  const days = Math.floor((now - then) / 86400000);
  if (days <= 0) return "touched today";
  if (days === 1) return "touched yesterday";
  return `touched ${days} days ago`;
}

// chosen is the ticked rows, in the order the table drew them, so the run
// goes down the page.
export function chosen(all: Row[], ticked: Set<string>): Row[] {
  return all.filter((row) => ticked.has(row.key));
}

// ticks is the boxes a freshly landed sweep starts with: merged ticked, the
// rest listed and left alone.
export function ticks(all: Row[]): Set<string> {
  return new Set(all.filter((row) => row.ticked).map((row) => row.key));
}

// summary heads the table: how much is here, and how much of it merged.
export function summary(all: Row[], groups: Group[] | undefined): string {
  if ((groups ?? []).some((group) => group.pending) && all.length === 0) {
    return "Sweeping…";
  }
  if (all.length === 0) return "Nothing here has finished.";
  const done = all.filter((row) => row.kind === "merged").length;
  return `${all.length} document${all.length === 1 ? "" : "s"} · ${done} merged`;
}

// trouble is every repository that could not be swept, each as its own line.
export function trouble(groups: Group[] | undefined): string[] {
  return (groups ?? [])
    .filter((group) => group.answer?.error)
    .map((group) => `${group.repo}: ${group.answer?.error}`);
}

// asked is the modal's question. It is the one place that says the local
// files go.
export function asked(picked: Row[]): string {
  return `Archive ${picked.length} document${picked.length === 1 ? "" : "s"} to the wiki and delete ${picked.length === 1 ? "it" : "them"} here?`;
}

// confirm is what that modal reads under the question.
export function confirm(picked: Row[]): string {
  const repos = new Set(picked.map((row) => row.repo));
  return [
    `${picked.length} document${picked.length === 1 ? "" : "s"} in ${repos.size} repositor${repos.size === 1 ? "y" : "ies"}.`,
    "Each is published to the wiki before its file is removed, so a failure leaves the document where it is.",
  ].join("\n\n");
}

// told is what to say when the run stops: what went, and that the failures
// are on the rows rather than in this line.
export function told(done: number, failed: number, stopped: boolean): string {
  const parts = [`Archived ${done}.`];
  if (failed > 0) parts.push(`${failed} failed; the reason is on the row.`);
  if (stopped) parts.push("Stopped; the rest were not touched.");
  return parts.join(" ");
}
