import { found, type Row } from "./switcher";

// What the omnibox hands over. A leading slash names one of ade's verbs by
// its title; anything else is said to the agent as the next turn.
export type Route =
  | { kind: "focus" }
  | { kind: "new"; message: string }
  | { kind: "again" }
  | { kind: "note"; text: string }
  | { kind: "go"; name: string }
  | { kind: "run"; command: string }
  | { kind: "pick"; commands: string[] }
  | { kind: "say"; message: string };

export interface Verb {
  command: string;
  title: string;
}

export function route(text: string, verbs: Verb[]): Route {
  const typed = text.trim();
  if (!typed) return { kind: "focus" };
  if (!typed.startsWith("/")) return { kind: "say", message: typed };
  const want = typed.slice(1).trim().toLowerCase();
  if (!want) return { kind: "pick", commands: verbs.map((v) => v.command) };
  if (want === "again") return { kind: "again" };
  const noted = /^note\b\s*(.*)$/s.exec(typed.slice(1).trim());
  if (noted && noted[1].trim()) return { kind: "note", text: noted[1].trim() };
  const going = /^go\b\s*(.*)$/s.exec(typed.slice(1).trim());
  if (going && going[1].trim()) return { kind: "go", name: going[1].trim() };
  const fresh = /^new\b\s*(.*)$/s.exec(typed.slice(1).trim());
  if (fresh && fresh[1].trim()) return { kind: "new", message: fresh[1].trim() };
  const exact = verbs.filter((v) => plain(v.title) === want);
  if (exact.length === 1) return { kind: "run", command: exact[0].command };
  const named = verbs.filter((v) => plain(v.title).startsWith(want));
  const near = named.length
    ? named
    : verbs.filter((v) => plain(v.title).includes(want));
  if (near.length === 1) return { kind: "run", command: near[0].command };
  if (near.length > 1) return { kind: "pick", commands: near.map((v) => v.command) };
  return { kind: "say", message: typed };
}

// The title without the "ade: " the palette needs, lower-cased for matching.
export function plain(title: string): string {
  return title.replace(/^ade:\s*/i, "").toLowerCase();
}

// A launcher row: the editor's quick open shows these above the file results
// while you type, and picking one runs the command with its arguments.
export interface LauncherRow {
  label: string;
  description?: string;
  icon?: string;
  command: string;
  args?: unknown[];
}

interface ThreadLike {
  id: string;
  file: string;
  line: number;
  resolved?: boolean;
  comments: { body: string }[];
}

// Every word typed has to appear somewhere in the row, in any order.
export function matches(query: string, haystack: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const text = haystack.toLowerCase();
  return words.length > 0 && words.every((w) => text.includes(w));
}

export function sessionRowsFor(
  query: string,
  rows: Row[],
  hereId?: string,
  limit = 4,
): LauncherRow[] {
  return rows
    .filter((r) => r.session.id !== hereId)
    .filter((r) =>
      matches(query, `${r.session.name} ${r.repo} ${r.session.branch} ${r.session.title ?? ""}`),
    )
    .slice(0, limit)
    .map((r) => ({
      label: `Switch to ${r.session.title || r.session.branch || r.session.name}`,
      description: [found(r), r.waiting ? "asked you" : r.session.live ? "running" : ""]
        .filter(Boolean)
        .join(" · "),
      icon: r.waiting ? "question" : r.session.live ? "debug-start" : "history",
      command: "ade.omnibox.open",
      args: [{ kind: "session", session: r.session }],
    }));
}

export function threadRowsFor(
  query: string,
  threads: ThreadLike[],
  limit = 4,
): LauncherRow[] {
  return threads
    .filter((t) => !t.resolved && t.file && t.line > 0)
    .filter((t) => matches(query, `${t.file} ${t.comments[0]?.body ?? ""}`))
    .slice(0, limit)
    .map((t) => ({
      label: `Comment · ${(t.comments[0]?.body ?? "").split("\n")[0].slice(0, 70)}`,
      description: `${t.file}:${t.line}`,
      icon: "comment",
      command: "ade.omnibox.open",
      args: [{ kind: "thread", file: t.file, line: t.line }],
    }));
}

export function launcherRows(
  query: string,
  sessions: Row[],
  threads: ThreadLike[],
  hereId?: string,
): LauncherRow[] {
  return [...sessionRowsFor(query, sessions, hereId), ...threadRowsFor(query, threads)];
}

interface TreeLike {
  path: string;
  name: string;
  branch?: string;
  kind: string;
  uncommitted?: number;
  unpushed?: number;
}

interface PullLike {
  number: number;
  title: string;
  author: string;
  headRef: string;
  draft?: boolean;
  reviewRequested?: boolean;
  checks?: string;
}

export function worktreeRowsFor(
  query: string,
  trees: TreeLike[],
  here?: string,
  limit = 3,
): LauncherRow[] {
  return trees
    .filter((t) => t.path !== here)
    .filter((t) => matches(query, `${t.branch ?? ""} ${t.name} ${t.kind} worktree`))
    .slice(0, limit)
    .map((t) => ({
      label: `Open worktree ${t.branch ?? t.name}`,
      description: [
        t.kind,
        t.uncommitted ? `${t.uncommitted} uncommitted` : "",
        t.unpushed ? `${t.unpushed} unpushed` : "",
      ]
        .filter(Boolean)
        .join(" · "),
      icon: "folder-library",
      command: "ade.omnibox.open",
      args: [{ kind: "worktree", path: t.path }],
    }));
}

export function pullRowsFor(
  query: string,
  pulls: PullLike[],
  limit = 3,
): LauncherRow[] {
  return pulls
    .filter((p) => matches(query, `#${p.number} ${p.title} ${p.author} ${p.headRef} pull request pr`))
    .slice(0, limit)
    .map((p) => ({
      label: `Review #${p.number} · ${p.title}`,
      description: [
        p.author,
        p.headRef,
        p.draft ? "draft" : "",
        p.reviewRequested ? "review requested" : "",
        p.checks ?? "",
      ]
        .filter(Boolean)
        .join(" · "),
      icon: "git-pull-request",
      command: "ade.omnibox.open",
      args: [{ kind: "pr", number: p.number }],
    }));
}

export function allLauncherRows(
  query: string,
  sessions: Row[],
  threads: ThreadLike[],
  trees: TreeLike[],
  pulls: PullLike[],
  hereId?: string,
  herePath?: string,
): LauncherRow[] {
  return [
    ...sessionRowsFor(query, sessions, hereId),
    ...pullRowsFor(query, pulls),
    ...worktreeRowsFor(query, trees, herePath),
    ...threadRowsFor(query, threads),
  ];
}

interface OfferLike {
  id: string;
  kind: string;
  from: string;
  question?: string;
  repo?: string;
  number?: number;
  claimed?: boolean;
}

interface WaitingLike {
  id: string;
  kind: string;
  what?: string;
  message?: string;
}

// The team's asks and review requests, which otherwise sit in the inbox tree.
export function inboxRowsFor(
  query: string,
  offers: OfferLike[],
  limit = 3,
): LauncherRow[] {
  return offers
    .filter((o) =>
      matches(query, `${o.from} ${o.question ?? ""} ${o.repo ?? ""}#${o.number ?? ""} #${o.number ?? ""} ${o.kind} inbox ask review`),
    )
    .slice(0, limit)
    .map((o) =>
      o.kind === "review"
        ? {
            label: `Take review ${o.repo}#${o.number}`,
            description: [o.from, o.claimed ? "yours" : "asked of the team"].join(" · "),
            icon: "git-pull-request",
            command: "ade.omnibox.open",
            args: [{ kind: "review", offer: o }],
          }
        : {
            label: `Answer ${o.from} · ${(o.question ?? "a question").split("\n")[0].slice(0, 60)}`,
            description: "waiting on you",
            icon: "comment-discussion",
            command: "ade.omnibox.open",
            args: [{ kind: "ask", offer: o }],
          },
    );
}

// What is queued behind the running turn: one row each, and all of them open
// the queue, where a waiting prompt can be edited or dropped.
export function queueRowsFor(
  query: string,
  waiting: WaitingLike[],
  limit = 3,
): LauncherRow[] {
  return waiting
    .filter((w) => matches(query, `${w.what ?? ""} ${w.message ?? ""} ${w.kind} queue queued waiting`))
    .slice(0, limit)
    .map((w, i) => ({
      label: `Queued · ${(w.message ?? w.what ?? w.kind).split("\n")[0].slice(0, 60)}`,
      description: i === 0 ? `${waiting.length} waiting for the agent` : "",
      icon: "clock",
      command: "ade.omnibox.open",
      args: [{ kind: "queue" }],
    }));
}

export interface Launchable {
  sessions: Row[];
  threads: ThreadLike[];
  trees: TreeLike[];
  pulls: PullLike[];
  offers: OfferLike[];
  waiting: WaitingLike[];
  hereId?: string;
  herePath?: string;
}

export function everyLauncherRow(query: string, from: Launchable): LauncherRow[] {
  return [
    ...inboxRowsFor(query, from.offers),
    ...sessionRowsFor(query, from.sessions, from.hereId),
    ...pullRowsFor(query, from.pulls),
    ...worktreeRowsFor(query, from.trees, from.herePath),
    ...queueRowsFor(query, from.waiting),
    ...threadRowsFor(query, from.threads),
  ];
}

// A line that ends in a question mark wants an answer, not a turn.
export function isQuestion(text: string): boolean {
  return /\?\s*$/.test(text.trim());
}

// What the box was asked lately, kept per workspace: a question with its
// answer, a turn sent, or a verb run, newest first.
export interface Remembered {
  kind: "ask" | "turn" | "verb";
  text: string;
  answer?: string;
  at: number;
}

export function remember(had: Remembered[], one: Remembered, keep = 8): Remembered[] {
  const rest = had.filter((r) => !(r.kind === one.kind && r.text === one.text));
  return [one, ...rest].slice(0, keep);
}

export function recentRows(had: Remembered[], verbs: Verb[] = []): LauncherRow[] {
  return had.map((r, i) => ({
    label:
      r.kind === "verb"
        ? `Run again · ${plain(verbs.find((v) => v.command === r.text)?.title ?? r.text)}`
        : r.kind === "ask"
          ? `Asked · ${r.text}`
          : `Sent · ${r.text.split("\n")[0].slice(0, 70)}`,
    description: r.kind === "ask" ? (r.answer ?? "").split("\n")[0].slice(0, 90) : "",
    icon: r.kind === "verb" ? "play" : r.kind === "ask" ? "question" : "send",
    command: "ade.omnibox.open",
    args: [{ kind: "recent", index: i }],
  }));
}

interface HitLike {
  path: string;
  line: number;
  snippet: string;
}

// The design documents of this checkout, by name: where the reasoning lives.
export function designRowsFor(query: string, files: readonly string[], limit = 3): LauncherRow[] {
  return files
    .filter((f) => matches(query, `${f} design doc`))
    .slice(0, limit)
    .map((f) => ({
      label: `Open design · ${f.replace(/\.md$/, "")}`,
      description: ".git/ade/docs",
      icon: "book",
      command: "ade.omnibox.open",
      args: [{ kind: "design", file: f }],
    }));
}

export function wikiRowsFor(hits: HitLike[], limit = 3): LauncherRow[] {
  const seen = new Set<string>();
  const pages = hits.filter((h) => !seen.has(h.path) && seen.add(h.path));
  return pages.slice(0, limit).map((h) => ({
    label: `Wiki · ${h.path.replace(/\.md$/, "")}`,
    description: h.snippet.split("\n")[0].trim().slice(0, 90),
    icon: "library",
    command: "ade.omnibox.open",
    args: [{ kind: "wiki", path: h.path, line: h.line }],
  }));
}

export interface Knowable extends Launchable {
  designs: readonly string[];
  wiki: HitLike[];
}

export function everyRow(query: string, from: Knowable): LauncherRow[] {
  return [
    ...everyLauncherRow(query, from),
    ...designRowsFor(query, from.designs),
    ...wikiRowsFor(from.wiki),
  ];
}

// A verb with the menu group it sits in and the icon the palette gives it.
export interface GroupedVerb extends Verb {
  group: string;
  icon?: string;
}

function iconOf(icon: string | undefined): string {
  const m = /^\$\(([\w-]+)\)$/.exec(icon ?? "");
  return m ? m[1] : "play";
}

function verbRow(v: GroupedVerb): LauncherRow {
  const title = plain(v.title);
  return {
    label: title.charAt(0).toUpperCase() + title.slice(1),
    description: v.group,
    icon: iconOf(v.icon),
    command: v.command,
  };
}

// The verbs as rows while you type: "commit" is Commit, "review" the three
// reviews, before any session or file called the same.
export function verbRowsFor(query: string, verbs: GroupedVerb[], limit = 5): LauncherRow[] {
  const want = query.trim().toLowerCase();
  if (!want) return [];
  const scored = verbs
    .map((v) => ({ v, text: plain(v.title) }))
    .filter(({ v, text }) => matches(want, `${text} ${v.group}`))
    .sort((a, b) => Number(b.text.startsWith(want)) - Number(a.text.startsWith(want)));
  return scored.slice(0, limit).map(({ v }) => verbRow(v));
}

export const FEATURED = [
  "ade.review",
  "ade.reviewWithAgents",
  "ade.commit",
  "ade.newSession",
  "ade.openChat",
  "ade.dashboard",
];

export function featuredVerbs(verbs: GroupedVerb[], ids = FEATURED): LauncherRow[] {
  return ids
    .map((id) => verbs.find((v) => v.command === id))
    .filter((v): v is GroupedVerb => Boolean(v))
    .map(verbRow);
}
