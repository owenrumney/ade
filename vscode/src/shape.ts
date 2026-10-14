import type { Tree } from "./client";
import type { Checkout, Repo, Session } from "./launcher";

// Three things shrink the tree, and none of them puts a session out of reach.

// rank orders sessions: live first, then most recently used. A list that is
// not ordered has to be read; one that is ordered has to be glanced at.
export function rank(a: Session, b: Session): number {
  if (Boolean(a.live) !== Boolean(b.live)) return a.live ? -1 : 1;
  return (b.lastUsed ?? "").localeCompare(a.lastUsed ?? "");
}

// sessionsOf is a checkout's conversations, ordered and cut to the cap. The
// rest are behind one row rather than gone: cap 0 means show everything.
export function sessionsOf(
  checkout: Checkout,
  cap: number,
  all: boolean,
): { shown: Session[]; hidden: number } {
  return cut(checkout.sessions ?? [], cap, all);
}

// sessionsAcross is a repository's conversations in one list, for a row that
// stands for every checkout it has: the branch on each row says which.
export function sessionsAcross(
  repo: Repo,
  cap: number,
  all: boolean,
): { shown: Session[]; hidden: number } {
  return cut(
    (repo.checkouts ?? []).flatMap((c) => c.sessions ?? []),
    cap,
    all,
  );
}

// An archived session is put away, not gone: it is behind the same row as the
// ones over the cap, and showing everything brings it back to be unarchived.
export function cut(
  sessions: Session[],
  cap: number,
  all: boolean,
): { shown: Session[]; hidden: number } {
  const ordered = [...sessions].sort(rank);
  if (all) return { shown: ordered, hidden: 0 };
  const kept = ordered.filter((session) => !session.archived);
  const shown = cap <= 0 ? kept : kept.slice(0, cap);
  return { shown, hidden: ordered.length - shown.length };
}

// only is the single checkout a repository has, when it has one. Then the
// middle level says nothing the repository row does not, and is skipped.
export function only(repo: Repo): Checkout | undefined {
  const checkouts = repo.checkouts ?? [];
  return checkouts.length === 1 ? checkouts[0] : undefined;
}

// interesting is a repository worth a row: one with a session, or the one this
// window is in. A scanned checkout nobody has talked to is not news.
export function interesting(repo: Repo, root: string | undefined): boolean {
  const checkouts = repo.checkouts ?? [];
  if (root && checkouts.some((c) => c.path === root)) return true;
  return checkouts.some((c) => (c.sessions ?? []).length > 0);
}

// trim drops the repositories that are neither. Returns what is left and how
// many went, so the tree can say so rather than appearing to have lost them.
export function trim(
  repos: Repo[],
  root: string | undefined,
  all: boolean,
): { shown: Repo[]; hidden: number } {
  if (all) return { shown: repos, hidden: 0 };
  const shown = repos.filter((repo) => interesting(repo, root));
  return { shown, hidden: repos.length - shown.length };
}

// count is how many sessions a repository holds, for the row's description:
// the number is why you would open it.
export function count(repo: Repo): number {
  return (repo.checkouts ?? []).reduce(
    (n, c) => n + (c.sessions ?? []).length,
    0,
  );
}

// live is how many of them are running, which is the part worth a colour.
export function live(repo: Repo): number {
  return (repo.checkouts ?? []).reduce(
    (n, c) => n + (c.sessions ?? []).filter((s) => s.live).length,
    0,
  );
}

// label is what a repository row says beside its name: the checkouts it has
// when it has several, and what is running.
export function label(repo: Repo): string {
  const checkouts = (repo.checkouts ?? []).length;
  const bits: string[] = [];
  if (checkouts > 1) bits.push(`${checkouts} checkouts`);
  const running = live(repo);
  if (running) bits.push(`${running} live`);
  else if (count(repo)) bits.push(`${count(repo)}`);
  return bits.join(" · ");
}

// blameWhen reads a commit date. `since` stops at days, which turns an old
// line into "730d ago"; past a month the calendar date says more.
export function blameWhen(when: string | undefined, now = Date.now()): string {
  if (!when) return "";
  const at = new Date(when).getTime();
  if (Number.isNaN(at)) return "";
  if (now - at < 30 * 24 * 60 * 60 * 1000) return since(when, now);
  return new Date(at).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

export function since(when: string | undefined, now = Date.now()): string {
  if (!when) return "";
  const at = new Date(when).getTime();
  if (Number.isNaN(at)) return "";
  const mins = Math.floor((now - at) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

// sessionLine is what a conversation is about now. Its title is not: that is
// written from the first turn and never again.
export function sessionLine(session: Session, now = Date.now()): string {
  return [
    session.branch,
    since(session.lastUsed, now),
    session.live ? "live" : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

// Worktree is one row of the checkout tree: the launcher's checkout, which
// knows the conversations, joined to the server's tree, which knows removal.
export interface Worktree {
  path: string;
  name: string;
  branch: string;
  main: boolean;
  checkout?: Checkout;
  tree?: Tree;
  // What the row holds once conversations are grouped by branch.
  sessions?: Session[];
  // loose is a branch talked on in the main checkout, with no checkout of its
  // own; catchAll is the main checkout's row when it has others to fold behind.
  loose?: boolean;
  catchAll?: boolean;
  machine?: string;
}

export interface Away {
  machine: string;
  repos: Repo[] | null;
}

// workspacesOf is the repository's rows by branch: each worktree, each branch
// only talked on in the main checkout, each clone on a machine, then the rest.
export function workspacesOf(
  repo: Repo | undefined,
  trees: Tree[],
  away: Away[] = [],
): Worktree[] {
  const rows = worktreesOf(repo, trees);
  const main = rows.find((row) => row.main);
  const named = new Map<string, Worktree>();
  for (const row of rows) {
    row.sessions = row.main ? [] : [...(row.checkout?.sessions ?? [])];
    if (!row.main && row.branch && !named.has(row.branch))
      named.set(row.branch, row);
  }
  const out = rows.filter((row) => !row.main);
  for (const session of main?.checkout?.sessions ?? []) {
    const on = session.branch;
    if (!main || !on || on === "HEAD" || on === main.branch) {
      main?.sessions?.push(session);
      continue;
    }
    let row = named.get(on);
    if (!row) {
      const { path } = main;
      row = { path, name: on, branch: on, main: false, loose: true };
      row.sessions = [];
      named.set(on, row);
      out.push(row);
    }
    row.sessions?.push(session);
  }
  out.sort((a, b) => title(a).localeCompare(title(b)));
  if (repo) out.push(...awayOf(repo, away));
  if (main) {
    main.catchAll = out.length > 0;
    out.push(main);
  }
  return out;
}

// awayOf is this repository's clones on other machines that hold a
// conversation: the same shape as a worktree, somewhere else.
function awayOf(repo: Repo, away: Away[]): Worktree[] {
  return away.flatMap(({ machine, repos }) =>
    (repos ?? [])
      .filter((r) => r.name === repo.name && r.owner === repo.owner)
      .flatMap((r) => r.checkouts ?? [])
      .filter((c) => (c.sessions ?? []).length > 0)
      .map((c) => ({
        path: c.path,
        name: c.name,
        branch: c.branch,
        main: false,
        checkout: c,
        sessions: c.sessions ?? [],
        machine,
      })),
  );
}

// title is a workspace row's name: its branch, or the folder with none known.
export function title(w: Worktree): string {
  return w.branch || w.name;
}

export function tally(sessions: Session[]): string {
  if (sessions.length === 0) return "";
  const running = sessions.filter((s) => s.live).length;
  const said = `${sessions.length} conversation${sessions.length === 1 ? "" : "s"}`;
  return running ? `${said} · ${running} live` : said;
}

// worktreesOf joins the two lists by path. Either side may be missing: no
// launcher means no conversations, no server means no removal state.
export function worktreesOf(repo: Repo | undefined, trees: Tree[]): Worktree[] {
  const rows = new Map<string, Worktree>();
  for (const t of trees) {
    const { path, name } = t;
    const main = Boolean(t.main);
    rows.set(path, { path, name, branch: t.branch ?? "", main, tree: t });
  }
  // The launcher's repository path is its main checkout, so the main one is
  // known with no server at all.
  for (const c of repo?.checkouts ?? []) {
    const main = c.path === repo?.path;
    const held = rows.get(c.path);
    if (held) {
      held.checkout = c;
      held.main ||= main;
      if (!held.branch) held.branch = c.branch;
    } else {
      const { path, name, branch } = c;
      rows.set(path, { path, name, branch, main, checkout: c });
    }
  }
  return [...rows.values()].sort(rankWorktree);
}

// The repository's own checkout first, then by name: the main one is where
// the branch you opened lives, and the rest are a list to scan.
function rankWorktree(a: Worktree, b: Worktree): number {
  if (a.main !== b.main) return a.main ? -1 : 1;
  return a.name.localeCompare(b.name);
}

// mine is the repository this window is in, by any of its checkouts.
export function mine(
  repos: Repo[],
  root: string | undefined,
): Repo | undefined {
  if (!root) return undefined;
  return repos.find((repo) =>
    (repo.checkouts ?? []).some((c) => c.path === root),
  );
}

// elsewhere is every other repository worth a row: one with a conversation.
export function elsewhere(repos: Repo[], root: string | undefined): Repo[] {
  const here = mine(repos, root);
  return repos.filter((repo) => repo !== here && interesting(repo, undefined));
}

// labelAll is label() over several repositories, for the Elsewhere row.
export function labelAll(repos: Repo[]): string {
  const running = repos.reduce((n, repo) => n + live(repo), 0);
  if (running) return `${running} live`;
  const total = repos.reduce((n, repo) => n + count(repo), 0);
  return total ? `${total}` : "";
}

// sessionContext is what a session row's menu matches on. Archived wins over
// where it is: the only thing to do with one is bring it back or delete it.
export function sessionContext(session: Session, here: boolean): string {
  if (session.archived) return "ade.session.archived";
  const where = here ? "here" : "elsewhere";
  return session.group
    ? `ade.session.${where}.grouped`
    : `ade.session.${where}`;
}

// combinable is a checkout's finished, ungrouped conversations: the only ones
// there is any sense in folding into one card.
export function combinable(sessions: Session[]): Session[] {
  return sessions.filter((s) => !s.live && !s.archived && !s.group);
}

// repoContext is what a repository row's menu matches on. Whether ade keeps a
// memory for it is the only thing to decide there, so it is the only split.
export function repoContext(repo: Repo | undefined): string {
  return repo?.remembered ? "ade.repo.remembered" : "ade.repo";
}
