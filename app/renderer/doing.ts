// Every command ade contributes, and what this window does for it. One
// it cannot do yet says which thing it needs, by name.

export type From =
  | "repo"
  | "session"
  | "space"
  | "openspace"
  | "scope"
  | "person"
  | "reply"
  | "offer"
  | "reviewer";

export type Ask =
  | { ask: "pick"; from: From; as: string; title: string }
  // ticks are on when the box opens, for a list with a usual answer.
  | { ask: "many"; from: From; as: string; title: string; ticks?: string[] }
  | { ask: "one"; of: (string | { label: string; value: unknown })[]; as: string; title: string }
  | { ask: "type"; as: string; title: string; blank?: true };

export type Verb =
  | "omnibox"
  | "dismiss"
  | "refresh"
  | "save"
  | "start"
  | "terminal"
  | "agent"
  | "diff"
  | "my-day"
  | "brief"
  | "brief-send"
  | "tags"
  | "import"
  | "findings"
  | "close-session"
  | "sidebar"
  | "review-draft"
  | "review-finish"
  | "open-pr";

export type Method = "PUT" | "PATCH" | "DELETE";

export type Doing =
  | { at: "window"; place?: string; verb?: Verb }
  | { at: "launcher"; path: string; body?: Record<string, unknown>; needs?: Ask[]; how?: Method }
  | { at: "session"; path: string; body?: Record<string, unknown>; needs?: Ask[]; how?: Method }
  | { at: "panel"; panel: string }
  | { at: "rules" }
  // A document the window renders from one read.
  | { at: "report"; from: "launcher" | "session"; path: string; render: string }
  // A verdict names the pull request it is on, which only the window
  // knows, so these cannot be a plain path and body.
  | { at: "verdict"; which: "approve" | "request-changes" | "merge" }
  | { at: "soon"; needs: string };

const W = (place?: string, verb?: Verb): Doing => ({
  at: "window",
  place,
  verb,
});
const L = (path: string, body?: Record<string, unknown>, ...needs: Ask[]): Doing => ({
  at: "launcher",
  path,
  body,
  needs,
});
const S = (path: string, body?: Record<string, unknown>, ...needs: Ask[]): Doing => ({
  at: "session",
  path,
  body,
  needs,
});
const type = (as: string, title: string): Ask => ({ ask: "type", as, title });
const blank = (as: string, title: string): Ask => ({ ask: "type", as, title, blank: true });
const one = (
  of: (string | { label: string; value: unknown })[],
  as: string,
  title: string,
): Ask => ({ ask: "one", of, as, title });
const pick = (from: From, as: string, title: string): Ask => ({
  ask: "pick",
  from,
  as,
  title,
});
const many = (from: From, as: string, title: string, ticks?: string[]): Ask => ({
  ask: "many",
  from,
  as,
  title,
  ticks,
});

export const WIRED: Record<string, Doing> = {
  // ── The window's own, which the keyboard names ──
  omnibox: W(undefined, "omnibox"),
  dismiss: W(undefined, "dismiss"),
  save: W(undefined, "save"),
  "ade.menu": W(undefined, "omnibox"),
  "place.sessions": W("sessions"),
  "place.waiting": W("waiting"),
  "place.prs": W("prs"),
  "place.inbox": W("inbox"),
  "place.team": W("team"),
  "place.issues": W("issues"),
  "place.spaces": W("spaces"),
  "place.review": W(undefined, "diff"),

  // ── Sessions ──
  "ade.newSession": { at: "panel", panel: "ade.newSession" },
  "ade.newScratch": L("/api/scratch", { terminal: true }),
  // The three tabs every session has.
  "ade.openAgent": W(undefined, "agent"),
  "ade.diffTurn": W(undefined, "diff"),
  "ade.openTerminal": W(undefined, "terminal"),
  "ade.renameSession": L("/api/sessions/rename", {}, type("title", "Rename this session to…")),
  "ade.tagSession": W(undefined, "tags"),
  "ade.importConversation": W(undefined, "import"),
  "ade.openBrief": W(undefined, "brief"),
  "ade.sendBrief": W(undefined, "brief-send"),
  "ade.closeSession": W(undefined, "close-session"),
  "ade.portScratch": S(
    "/api/port",
    { remove: false },
    pick("repo", "target", "Move the work into which repository?"),
    blank("branch", "Branch name, or empty to let ade name it…"),
  ),
  "ade.refreshSessions": W(undefined, "refresh"),

  // ── Diff ──
  "ade.refreshChanges": W(undefined, "refresh"),
  "ade.scopeBranch": S("/api/scope", { scope: "branch" }),
  "ade.scopeCommitted": S("/api/scope", { scope: "committed" }),
  "ade.scopeUncommitted": S("/api/scope", { scope: "uncommitted" }),
  "ade.stageAll": S("/api/stage", { all: true, staged: true }),
  "ade.unstageAll": S("/api/stage", { all: true, staged: false }),
  "ade.setScope": S("/api/scope", {}, pick("scope", "scope", "Which scope?")),
  "ade.diffRoot": S("/api/diff/root", {}, pick("repo", "root", "Which repository?")),
  "ade.pullBase": S("/api/base/pull"),

  // ── Review ──
  "ade.review": S("/api/review"),
  // A level is read only where no agent is named, so naming one is the
  // whole of the question.
  "ade.reviewWithAgents": S(
    "/api/review",
    { level: "" },
    many("reviewer", "agents", "Which agents should review?", ["security", "staff-reviewer"]),
  ),
  "ade.stop": S("/api/cancel"),
  "ade.sendFindings": W(undefined, "findings"),
  // The review as GitHub builds one: comments join a draft, a verdict ends it.
  "ade.addToReview": W(undefined, "review-draft"),
  "ade.finishReview": W(undefined, "review-finish"),
  "ade.sendReplies": S("/api/threads/send-replies"),
  // `which` names what the sweep takes; empty is every comment.
  "ade.pruneOrphans": S("/api/threads/reset", { which: "orphaned" }),
  "ade.removeResolved": S("/api/threads/reset", { which: "resolved" }),
  "ade.removeDeclined": S("/api/threads/reset", { which: "declined" }),
  "ade.removeAllComments": S("/api/threads/reset", { which: "" }),

  // ── Commit and pull request ──
  "ade.writeCommitMessage": S("/api/commit-message"),
  "ade.push": S("/api/push"),
  // The window follows the answer onto the session, so not a plain row.
  "ade.openPRLink": { at: "window", verb: "open-pr" },
  "ade.approvePR": { at: "verdict", which: "approve" },
  "ade.requestChanges": { at: "verdict", which: "request-changes" },
  "ade.mergePR": { at: "verdict", which: "merge" },
  "ade.refreshChecks": S("/api/checks/refresh"),

  // ── Lists: the five on the bar, and the two with no button ──
  "ade.well": W("waiting"),
  "ade.myDay": W(undefined, "my-day"),
  "ade.pendingPRs": W("prs"),
  "ade.showInbox": W("inbox"),
  "ade.team": W("team"),
  "ade.issues": W("issues"),
  "ade.spaces": W("spaces"),
  "ade.openSpace": L("/api/spaces", undefined, pick("space", "repo", "Open which repository as a space?")),
  "ade.closeSpace": {
    at: "launcher",
    path: "/api/spaces",
    how: "DELETE",
    needs: [pick("openspace", "repo", "Close which space? Nothing on disk goes with it…")],
  },

  // ── Team ──
  // The launcher refuses a review request with no pull request in it.
  "ade.askTeamForReview": S(
    "/api/bus/request-review",
    {},
    type("pr", "Which pull request?"),
    pick("person", "to", "Who should review it?"),
    blank("note", "Anything they should know…"),
  ),
  "ade.messageTeamMember": S(
    "/api/bus/ask",
    {},
    pick("person", "to", "Message who?"),
    type("question", "What do you want to ask them?"),
  ),
  "ade.setPresence": S("/api/bus/presence", {}, one(["active", "away"], "presence", "Show as?")),
  "ade.refreshTeam": S("/api/bus/refresh"),
  "ade.dismissReply": S("/api/bus/dismiss", {}, pick("reply", "id", "Clear which answer?")),
  "ade.dismissOffer": S("/api/bus/dismiss", {}, pick("offer", "id", "Dismiss which offer?")),

  // ── ade itself ──
  // A ping is no use when the thing to ping is what is missing.
  "ade.startLauncher": W(undefined, "start"),
  "ade.openSettings": { at: "panel", panel: "ade.openSettings" },
  "ade.keys": { at: "panel", panel: "ade.openSettings" },
  "ade.toggleSidebar": W(undefined, "sidebar"),
  "sidebar.toggle": W(undefined, "sidebar"),
  "ade.openPermissions": { at: "rules" },
  "ade.reconnect": W(undefined, "refresh"),
  "ade.showLogs": { at: "report", from: "session", path: "/api/logs", render: "logs" },
};

export function wired(command: string): Doing | undefined {
  return WIRED[command];
}

// fill puts one answer into the request and returns the path it leaves.
// An array is a `many` answer and goes under its own name; only a row that
// names its own fields is merged.
export function fill(
  body: Record<string, unknown>,
  path: string,
  as: string,
  said: unknown,
): string {
  const slot = `{${as}}`;
  if (!Array.isArray(said) && said !== null && typeof said === "object") Object.assign(body, said);
  else if (!Array.isArray(said) && path.includes(slot))
    return path.replace(slot, encodeURIComponent(String(said)));
  else body[as] = said;
  return path;
}

// live is whether picking it does something now. A `soon` is listed and
// dimmed rather than hidden: the vocabulary is ade's.
export function live(command: string): boolean {
  const what = WIRED[command];
  return Boolean(what) && what.at !== "soon";
}

export function panelFor(command: string): string | undefined {
  const what = WIRED[command];
  return what?.at === "panel" ? what.panel : undefined;
}

// owed is what the window says when it cannot do one yet. It names the
// missing part rather than the command, because that is the useful half.
export function owed(command: string): string {
  const what = WIRED[command];
  if (!what) return `${command} is not in the app's table yet`;
  if (what.at === "soon") return `${command} needs ${what.needs}`;
  return "";
}
