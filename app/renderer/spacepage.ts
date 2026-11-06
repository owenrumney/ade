import { el } from "./omnibox";
import { ago, type Repo, type Session } from "./real";
import type { Space } from "./spaces";

// What a space opens as: the sessions in the repository, and a way to start one.

export interface SessionLine {
  id: string;
  name: string;
  meta: string;
  live?: boolean;
}

export interface Seen {
  space: Space;
  sessions: SessionLine[];
}

export type SpaceAct =
  | { what: "open-session"; id: string }
  | { what: "close-session"; id: string }
  | { what: "new-session" };

// sessionsOf is every session in the repository, newest first. `gone` is
// what this window closed, which a read has not caught up with yet.
export function sessionsOf(
  repo: Repo | undefined,
  now = Date.now(),
  gone: ReadonlySet<string> = new Set(),
): SessionLine[] {
  const out: (SessionLine & { at: number })[] = [];
  for (const checkout of repo?.checkouts ?? []) {
    for (const session of (checkout.sessions ?? []).filter((s: Session) => !s.archived && !gone.has(s.id))) {
      out.push({
        id: session.id,
        name: session.title || session.name || session.id,
        live: session.live,
        at: Date.parse(session.lastUsed ?? "") || 0,
        meta: [
          session.branch ?? checkout.branch,
          checkout.worktree ? checkout.name : "main checkout",
          session.agent,
          session.live ? "live" : ago(session.lastUsed, now),
        ]
          .filter(Boolean)
          .join(" · "),
      });
    }
  }
  out.sort((a, b) => b.at - a.at);
  return out.map(({ at: _at, ...line }) => line);
}

// drawnFrom is what the page draws. A page built from one of these is stale
// the moment it changes.
export function drawnFrom(seen: Seen): string {
  return seen.sessions.map((one) => `${one.id}.${one.live ? 1 : 0}.${one.name}.${one.meta}`).join(",");
}

export function spacePage(seen: Seen, act: (did: SpaceAct) => void): HTMLElement {
  const out = el("div", "doc space-page");
  out.append(head(seen, act));
  const box = el("section", "space-list");
  const title = el("div", "space-head-row");
  title.append(el("h2", "", `Sessions${seen.sessions.length ? ` (${seen.sessions.length})` : ""}`));
  box.append(title);
  if (seen.sessions.length === 0) box.append(el("p", "sub", "No sessions in this repository."));
  for (const one of seen.sessions) {
    const row = el("div", "space-row");
    const open = el("button", "link", one.name);
    open.addEventListener("click", () => act({ what: "open-session", id: one.id }));
    row.append(open);
    if (one.live) row.append(el("span", "pip", "live"));
    row.append(el("span", "meta", one.meta));
    const away = el("button", "away", "Close");
    away.addEventListener("click", () => act({ what: "close-session", id: one.id }));
    row.append(away);
    box.append(row);
  }
  out.append(box);
  return out;
}

function head(seen: Seen, act: (did: SpaceAct) => void): HTMLElement {
  const box = el("header", "space-head");
  const top = el("div", "space-top");
  top.append(el("h1", "", seen.space.repo));
  // A space not cloned here has no checkout to start in.
  if (seen.space.path) {
    const start = el("button", "btn", "New session");
    start.title = `Open the New session screen with ${seen.space.repo} ticked`;
    start.addEventListener("click", () => act({ what: "new-session" }));
    top.append(start);
  }
  box.append(top);
  box.append(el("div", "sub", [seen.space.path, seen.space.branch].filter(Boolean).join(" · ")));
  return box;
}
