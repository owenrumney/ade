// Every command ade contributes, and what this window does for it. One
// it cannot do yet says which thing it needs, by name.

export type From =
  | "repo"
  | "session"
  | "space"
  | "openspace"
  | "scope"
  | "model"
  | "role"
  | "worktree"
  | "agent"
  | "person"
  | "thread"
  | "reply"
  | "offer"
  | "reviewer"
  | "machine"
  | "remembered"
  | "proposed"
  | "drifted"
  | "dismissed";

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
  | "resume"
  | "ask"
  | "dictate"
  | "history"
  | "terminal"
  | "float"
  | "review-draft"
  | "review-finish";

export type Method = "PUT" | "DELETE";

export type Doing =
  | { at: "window"; place?: string; verb?: Verb }
  | { at: "launcher"; path: string; body?: Record<string, unknown>; needs?: Ask[]; how?: Method }
  | { at: "session"; path: string; body?: Record<string, unknown>; needs?: Ask[]; how?: Method }
  | { at: "panel"; panel: string }
  | { at: "rules" }
  // A document the window renders through the extension's own page
  // builders. `also` is a second read a builder wants.
  | { at: "report"; from: "launcher" | "session"; path: string; also?: string; render: string }
  | { at: "find"; from: "launcher" | "session"; path: string; open: string; title: string }
  | { at: "edit"; what: "pr" }
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
  // ── The window's own, which have no extension command behind them ──
  omnibox: W(undefined, "omnibox"),
  dismiss: W(undefined, "dismiss"),
  save: W(undefined, "save"),
  // The review as GitHub builds one: comments join a draft, a verdict ends it.
  "ade.addToReview": W(undefined, "review-draft"),
  "ade.finishReview": W(undefined, "review-finish"),
  "ade.menu": W(undefined, "omnibox"),
  "place.waiting": W("waiting"),
  "place.spaces": W("spaces"),
  "place.agents": W("agents"),
  "place.prs": W("prs"),
  "place.designs": W("designs"),
  "place.sessions": W("sessions"),
  "place.todos": W("todos"),
  "place.issues": W("issues"),
  "place.inbox": W("inbox"),
  "place.team": W("team"),
  "place.jobs": W("jobs"),
  "place.review": W("review"),
  "place.code": W("code"),

  // ── Conversation ──
  "ade.openChat": { at: "panel", panel: "ade.openChat" },
  "ade.focusChat": { at: "panel", panel: "ade.focusChat" },
  "ade.dictate": W(undefined, "dictate"),
  "ade.arrange": W("waiting"),
  "ade.dashboard": W("waiting"),
  "ade.diffTurn": W("review"),
  "ade.ask": W(undefined, "ask"),
  "ade.newChat": S("/api/chat/new"),
  "ade.clearChat": S("/api/chat/clear"),
  "ade.stop": S("/api/cancel"),
  "ade.stopAgent": S("/api/sides/{id}/cancel", {}, pick("agent", "id", "Stop which agent?")),
  // An answer named `session` is which session the verb acts on, so this
  // is the same stop sent somewhere other than the one in the sidebar.
  "ade.stopElsewhere": S("/api/cancel", {}, pick("session", "session", "Stop which session?")),
  "ade.showQueue": W("waiting"),
  "ade.runQueued": S("/api/queue/run"),
  "ade.pickModel": S("/api/chat/model", {}, pick("model", "model", "Which model?")),

  // ── Sessions ──
  "ade.switchSession": W("sessions"),
  // One server per checkout, so this moves it: everything pointed at
  // that server follows.
  "ade.switchSessionHere": W(undefined, "resume"),
  "ade.findSession": W(undefined, "omnibox"),
  "ade.newSession": { at: "panel", panel: "ade.newSession" },
  "ade.closeSession": S("/api/exit"),
  "ade.renameSession": L("/api/sessions/rename", {}, type("title", "Rename this session to…")),
  "ade.portScratch": S(
    "/api/port",
    { remove: false },
    pick("repo", "target", "Move the work into which repository?"),
    blank("branch", "Branch name, or empty to let ade name it…"),
  ),
  "ade.newScratch": L("/api/scratch"),
  "ade.combineSessions": L("/api/sessions/combine", {}, many("session", "ids", "Combine which sessions?")),
  "ade.switchWorktree": S(
    "/api/worktrees/switch",
    {},
    pick("worktree", "path", "Move this session to which worktree?"),
  ),
  "ade.worktrees": { at: "panel", panel: "ade.worktrees" },
  "ade.worktreeGraph": { at: "panel", panel: "ade.worktreeGraph" },
  "ade.showAllSessions": W("sessions"),
  "ade.collapseSessions": W("sessions"),
  "ade.refreshSessions": W(undefined, "refresh"),
  "ade.archiveSession": S("/api/sessions/archive"),

  // ── Diff ──
  "ade.refreshChanges": W(undefined, "refresh"),
  "ade.scopeBranch": S("/api/scope", { scope: "branch" }),
  "ade.scopeCommitted": S("/api/scope", { scope: "committed" }),
  "ade.scopeUncommitted": S("/api/scope", { scope: "uncommitted" }),
  "ade.stageAll": S("/api/stage", { all: true, staged: true }),
  "ade.unstageAll": S("/api/stage", { all: true, staged: false }),
  "ade.blame": W("code"),
  "ade.history": W(undefined, "history"),
  "ade.setScope": S("/api/scope", {}, pick("scope", "scope", "Which scope?")),
  "ade.diffRoot": S("/api/diff/root", {}, pick("repo", "root", "Which repository?")),

  // ── Review ──
  "ade.reviewBranch": S("/api/review"),
  "ade.review": S("/api/review"),
  // A level is read only where no agent is named, so naming one is the
  // whole of the question.
  "ade.reviewWithAgents": S(
    "/api/review",
    { level: "" },
    many("reviewer", "agents", "Which agents should review?", ["security", "staff-reviewer"]),
  ),
  "ade.resolveAll": S("/api/resolve", { all: true }),
  "ade.resolveHere": S("/api/threads/{id}/resolve", {}, pick("thread", "id", "Resolve which comment?")),
  "ade.pruneOrphans": S("/api/threads/reset", { orphaned: true }),
  "ade.removeResolved": S("/api/threads/reset", { resolved: true }),
  "ade.removeDeclined": S("/api/threads/reset", { declined: true }),
  "ade.removeAllComments": S("/api/threads/reset", { all: true }),
  "ade.showComments": W("review"),
  "ade.openReview": W("prs"),
  "ade.explain": S("/api/explain"),
  "ade.quizMe": { at: "panel", panel: "ade.quizMe" },
  "ade.pullBase": S("/api/base/pull"),
  "ade.draftReplies": S("/api/draft-replies"),
  "ade.sendReplies": S("/api/threads/send-replies"),

  // ── Commit and pull request ──
  "ade.commitPanel": { at: "panel", panel: "ade.commitPanel" },
  // No title keeps the last commit's message, which the server allows
  // only for an amend.
  "ade.commitAmend": S(
    "/api/commit",
    { amend: true },
    blank("title", "New message, or empty to keep the old one…"),
  ),
  "ade.writeCommitMessage": S("/api/commit-message"),
  "ade.push": S("/api/push"),
  "ade.createPR": S("/api/pr-create", { draft: true }),
  "ade.showPR": W("prs"),
  "ade.spaces": W("spaces"),
  "ade.openSpace": L("/api/spaces", undefined, pick("space", "repo", "Open which repository as a space?")),
  "ade.closeSpace": {
    at: "launcher",
    path: "/api/spaces",
    how: "DELETE",
    needs: [pick("openspace", "repo", "Close which space? Nothing on disk goes with it…")],
  },
  "ade.checkoutPR": S("/api/pr/checkout"),
  // The title through the box, the description in the sheet: editing
  // rendered markdown is what the window is for.
  "ade.editPR": { at: "edit", what: "pr" },
  "ade.approvePR": S("/api/pr/approve"),
  "ade.requestChanges": S("/api/pr/request-changes"),
  "ade.mergePR": S("/api/pr/merge"),
  "ade.refreshChecks": S("/api/checks/refresh"),
  "ade.fixChecks": S("/api/checks/fix"),

  // ── Design and jobs ──
  "ade.concierge": { at: "panel", panel: "ade.concierge" },
  // The front desk, in a window of its own: it is what you keep
  // beside the work rather than in front of it.
  "ade.conciergeFloat": W(undefined, "float"),
  "ade.well": W("waiting"),
  "ade.addTodo": L("/api/todos", {}, type("text", "What is the todo?")),
  "ade.todos": W("todos"),
  "ade.newDesign": S("/api/design", {}, type("name", "What is the design called?")),
  "ade.design": W("designs"),
  "ade.grillDesign": S("/api/design/go", { mode: "grill" }),
  "ade.condenseDesign": S("/api/design/condense"),
  "ade.purgeDesigns": { at: "panel", panel: "ade.purgeDesigns" },
  "ade.build": S("/api/design/go", { mode: "build" }),
  // The file is the definition, so the box writes one and the document
  // is where it gets edited afterwards.
  "ade.newJob": L(
    "/api/jobs/save",
    { mode: "create" },
    type("name", "What is the job called?"),
    type("schedule", "When should it run?"),
    type("prompt", "What should it do?"),
  ),
  "ade.jobs": W("jobs"),
  "ade.searchWiki": {
    at: "find",
    from: "launcher",
    path: "/api/wiki/search",
    open: "/api/wiki/page/",
    title: "Search the shared wiki",
  },
  "ade.runDrift": S("/api/drift"),
  "ade.drift": { at: "report", from: "session", path: "/api/drift", render: "drift" },
  // A requirement the change is checked against, and the two ways out:
  // stop checking it, or start again.
  "ade.driftDismiss": S(
    "/api/drift/dismiss",
    { dismissed: true },
    pick("drifted", "id", "Stop checking which?"),
  ),
  "ade.driftRestore": S(
    "/api/drift/dismiss",
    { dismissed: false },
    pick("dismissed", "id", "Check which again?"),
  ),
  "ade.memory": { at: "report", from: "session", path: "/api/project", render: "memory" },
  // Kept and proposed are different picks: they take different verbs,
  // and Forget on a proposal is not one of them.
  "ade.memoryForget": S(
    "/api/project/entry",
    { action: "forget" },
    pick("remembered", "id", "Forget which?"),
  ),
  "ade.memoryProposal": S(
    "/api/project/entry",
    {},
    pick("proposed", "id", "Which proposal?"),
    one(
      [
        { label: "Remember this", value: "accept" },
        { label: "Reject, and do not propose it again", value: "reject" },
      ],
      "action",
      "What with it?",
    ),
  ),
  "ade.distilMemory": S("/api/project/distil"),

  // ── Issues and pull requests ──
  "ade.issues": W("issues"),
  "ade.pendingPRs": W("prs"),

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

  // ── Machines and ports ──
  "ade.machines": { at: "report", from: "launcher", path: "/api/hosts", render: "machines" },
  "ade.addMachine": L("/api/hosts", {}, type("name", "Which host, as ssh dials it?")),
  "ade.removeMachine": {
    at: "launcher",
    path: "/api/hosts/{name}",
    how: "DELETE",
    needs: [pick("machine", "name", "Forget which machine?")],
  },
  // Asking a machine for its repositories is what connects it, which the
  // window has no step for yet, so the directory is typed rather than picked.
  "ade.startElsewhere": L(
    "/api/open",
    {},
    pick("machine", "machine", "Start on which machine?"),
    type("dir", "Which directory there?"),
    blank("prompt", "What to start on, or empty for just a session…"),
  ),
  "ade.exposePort": L("/api/ports/expose", {}, type("port", "Which port?")),
  // xterm in the window, over the session server's own shell.
  "ade.openTerminal": W(undefined, "terminal"),
  "ade.diagnose": S("/api/diagnose/net"),

  // ── ade itself ──
  "ade.showInbox": W("inbox"),
  // A ping is no use when the thing to ping is what is missing.
  "ade.startLauncher": W(undefined, "start"),
  "ade.openSettings": { at: "panel", panel: "ade.openSettings" },
  "ade.roles": L("/api/roles/default", {}, pick("role", "name", "Which role for new chats?")),
  "ade.syncModels": S("/api/chat/model", { refetch: true }),
  // Asked rather than toggled: the window would have to hold the
  // session's state to know which way a toggle goes.
  "ade.toggleSandbox": {
    at: "session",
    path: "/api/sandbox/on",
    how: "PUT",
    needs: [
      one(
        [
          { label: "In a container", value: true },
          { label: "On the host", value: false },
        ],
        "on",
        "Run the agent where?",
      ),
    ],
  },
  "ade.toggleFree": S("/api/chat/free"),
  "ade.checks": S("/api/verify", { run: true }),
  "ade.sandboxFile": { at: "panel", panel: "ade.sandboxFile" },
  "ade.sandboxPanel": { at: "panel", panel: "ade.sandboxPanel" },
  // /api/preview only reads; setting the url is a settings write.
  // Both of these are fields on the Settings form, which is hosted.
  "ade.previewURL": { at: "panel", panel: "ade.openSettings" },
  "ade.showStats": { at: "report", from: "launcher", path: "/api/stats", render: "stats" },
  "ade.showRepo": { at: "panel", panel: "ade.showRepo" },
  "ade.showUsage": {
    at: "report",
    from: "launcher",
    path: "/api/limits",
    also: "/api/limits/window",
    render: "usage",
  },
  "ade.showGraph": { at: "report", from: "launcher", path: "/api/graph", render: "graph" },
  "ade.syncWorktrees": L("/api/sync", {}, pick("repo", "repo", "Sync which repository's worktrees?")),
  "ade.purgeWorktrees": { at: "panel", panel: "ade.purgeWorktrees" },
  "ade.disk": { at: "panel", panel: "ade.disk" },
  "ade.openPermissions": { at: "rules" },
  "ade.reconnect": W(undefined, "refresh"),
  "ade.keys": { at: "panel", panel: "ade.openSettings" },
  "ade.showLogs": { at: "report", from: "session", path: "/api/logs", render: "logs" },
};

export function wired(command: string): Doing | undefined {
  return WIRED[command];
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
