import type { Place, Row, Status, World } from "./model";

// ade's own answers, as the window draws them: one function per
// endpoint. Nothing is invented that the launcher does not send.

export interface Dash {
  reviews?: Review[];
  notifications?: Notification[];
  linear?: { issues?: Issue[]; error?: string };
  started?: Record<string, { dir: string; branch: string; session?: string }>;
  reviewTrouble?: { error?: string; hidden?: number };
}

export interface Review {
  repo: string;
  number: number;
  title: string;
  author: string;
  url: string;
  draft?: boolean;
  updatedAt?: string;
  checks?: string;
  reviewDecision?: string;
  muted?: boolean;
  review?: { state: string; unsent?: number; threads?: number };
}

export interface Notification {
  id: string;
  repo: string;
  title: string;
  reason: string;
  type: string;
  url: string;
  updatedAt?: string;
  dismissed?: boolean;
}

export interface Issue {
  identifier: string;
  title: string;
  url: string;
  state: string;
  priority?: number;
  updatedAt?: string;
  project?: string;
}


export interface Repo {
  name: string;
  owner?: string;
  path: string;
  checkouts?: Checkout[];
}

export interface Checkout {
  path: string;
  name: string;
  branch?: string;
  worktree?: boolean;
  sessions?: Session[];
}

export interface Session {
  id: string;
  name: string;
  title?: string;
  branch?: string;
  dir: string;
  live?: boolean;
  archived?: boolean;
  lastUsed?: string;
  // What ade recorded when it started the session: its CLI, ticket, tags and brief.
  agent?: string;
  ticket?: string;
  tags?: string[];
  brief?: string;
}

export interface Listed {
  number: number;
  title: string;
  headRef?: string;
  draft?: boolean;
  mine?: boolean;
  url?: string;
  checks?: string;
  reviewDecision?: string;
  updatedAt?: string;
}

// prs is both lists under one heading: what is waiting on your review,
// from the launcher, and your own, from the session's server.
export function prs(
  dash: Dash & { error?: string },
  mine: Listed[] = [],
  repo = "",
  now = Date.now(),
): Place {
  const all = dash.reviews ?? [];
  // Opening one checks it out as a session, so the website is a row's own act.
  const acted = (r: Review): Row => ({
    ...reviewRow(r),
    acts: [
      { label: "Web", what: "pr-web", name: r.url },
      { label: r.muted ? "Bring back" : "Put aside", what: r.muted ? "pr-unmute" : "pr-mute", name: `${r.repo}#${r.number}` },
    ],
  });
  const asked = all.filter((r) => !r.muted).map(acted);
  const aside = all.filter((r) => r.muted).map((r) => ({ ...acted(r), group: "Put aside" }));
  const yours = mine
    .filter((p) => p.mine)
    .map((p) => ({
      id: `${repo}#${p.number}`,
      group: "Yours",
      icon: p.draft ? "edit" : "pr",
      tone: p.checks === "fail" ? ("bad" as const) : undefined,
      name: `#${p.number} ${p.title}`,
      meta: [p.headRef, p.checks, decided(p.reviewDecision), ago(p.updatedAt, now)]
        .filter(Boolean)
        .join(" · "),
      tag: p.draft ? { text: "draft", kind: "draft" } : undefined,
      acts: p.url ? [{ label: "Web", what: "pr-web" as const, name: p.url }] : [],
    }));
  return {
    id: "prs",
    title: "Pull requests",
    icon: "pr",
    rows: [...asked, ...yours, ...aside],
    note: why(dash),
  };
}

// prOf is the pull request a GitHub url points at: a notification's
// own id is not one.
export function prOf(url: string | undefined): { repo: string; number: number } | undefined {
  const at = /github\.com\/([^/]+\/[^/]+)\/pull\/(\d+)/.exec(url ?? "");
  return at ? { repo: at[1], number: Number(at[2]) } : undefined;
}

function decided(state: string | undefined): string {
  if (state === "APPROVED") return "approved";
  if (state === "CHANGES_REQUESTED") return "changes asked";
  return "";
}

// why an empty list is empty, in the dash's own words: an outage and a
// quiet morning look the same otherwise.
export function why(dash: Dash & { error?: string }): string {
  if (dash.error) return `/api/dash — ${dash.error}`;
  const trouble = dash.reviewTrouble ?? {};
  if (trouble.error) return `GitHub — ${trouble.error}`;
  if (trouble.hidden) {
    return `${trouble.hidden} hidden by the rules in Settings.`;
  }
  return "";
}

function reviewRow(r: Review): Row {
  const state = r.review?.state ?? "requested";
  return {
    id: `${r.repo}#${r.number}`,
    group: "To review",
    url: r.url,
    pr: { repo: r.repo, number: r.number },
    icon: stateIcon(state),
    tone: state === "drafted" ? "bad" : state === "ready" ? "warn" : undefined,
    name: `#${r.number} ${r.title}`,
    meta: [r.repo, r.author && `@${r.author}`, r.checks, ago(r.updatedAt)]
      .filter(Boolean)
      .join(" · "),
    tag: tagFor(r),
  };
}

function stateIcon(state: string): string {
  switch (state) {
    case "running":
      return "play";
    case "ready":
      return "eye";
    case "drafted":
      return "edit";
    case "sent":
      return "check";
    default:
      return "pr";
  }
}

function tagFor(r: Review): Row["tag"] {
  const state = r.review?.state;
  if (state === "drafted") {
    const n = r.review?.unsent ?? 0;
    return { text: n === 1 ? "1 unsent" : `${n} unsent`, kind: "unsent" };
  }
  if (state === "ready") return { text: "ready", kind: "ready" };
  if (state === "running") return { text: "reviewing", kind: "running" };
  if (state === "sent") return { text: "sent", kind: "sent" };
  if (r.checks === "fail") return { text: "checks ✗", kind: "unsent" };
  return undefined;
}

// badge is a live row's icon and tone: red when it is your turn, amber at work.
export function badge(agent: Active["agent"]): { icon: string; tone: Row["tone"] } {
  if (agent?.terminal === "ended") return { tone: undefined, icon: "history" };
  const state = stateOf(agent);
  if (attention(state)) return { tone: "bad", icon: "bell" };
  const tone = state === "working" ? "warn" : "good";
  return { tone, icon: "play" };
}

// tagsOf is what sits beside a session's name: its ticket, its brief, then
// the tags typed by hand.
export function tagsOf(session: Session): NonNullable<Row["tags"]> {
  return [
    ...(session.ticket ? [{ text: session.ticket, kind: "ticket" }] : []),
    ...(session.brief ? [{ text: "brief", kind: "brief" }] : []),
    ...(session.tags ?? []).map((text) => ({ text, kind: "own" })),
  ];
}

// sessionActs is what a session's row does besides open. Only Close shows on
// hover; the rest are the context menu's.
function sessionActs(id: string, brief: boolean): NonNullable<Row["acts"]> {
  const menu = true as const;
  return [
    { label: "Close", what: "session-end", name: id },
    { label: "Tags…", what: "session-tags", name: id, menu },
    ...(brief
      ? [
          { label: "Open the brief", what: "brief-open" as const, name: id, menu },
          { label: "Tell the agent to read the brief", what: "brief-send" as const, name: id, menu },
        ]
      : []),
  ];
}

// sessions is the left side: one row per session, by name. Not by when it
// last spoke, which reshuffles under the pointer every time an agent writes.
export function sessions(repos: Repo[], now = Date.now(), active: Active[] = []): Place {
  const rows: Row[] = [];
  const serving = new Map(active.map((a) => [a.id, a]));
  const listed = new Set<string>();
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of (checkout.sessions ?? []).filter((s) => !s.archived)) {
        const up = serving.get(session.id);
        const live = Boolean(session.live) || Boolean(up);
        listed.add(session.id);
        rows.push({
          id: session.id,
          ...(live ? badge(up?.agent) : { tone: undefined, icon: "history" }),
          name: session.title || session.branch || session.name,
          meta: [
            repo.name,
            checkout.worktree && checkout.name !== repo.name ? checkout.name : "",
            session.agent,
            live ? "" : ago(session.lastUsed, now),
          ]
            .filter(Boolean)
            .join(" · "),
          tags: tagsOf(session),
          acts: sessionActs(session.id, Boolean(session.brief)),
        });
      }
    }
  }
  // An ade serving a checkout the list does not name yet: without a row it
  // cannot be picked, so its diff has nothing to follow.
  for (const up of active.filter((a) => !listed.has(a.id))) {
    rows.push({
      id: up.id,
      ...badge(up.agent),
      name: up.branch || up.name || up.id,
      meta: [up.repo, "starting"].filter(Boolean).join(" · "),
      acts: sessionActs(up.id, false),
    });
  }
  rows.sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
  return { id: "sessions", title: "Sessions", icon: "session", rows };
}

// briefOf is the path of a session's brief, or empty when it has none.
export function briefOf(repos: Repo[], id: string): string {
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      const found = (checkout.sessions ?? []).find((s) => s.id === id);
      if (found) return found.brief ?? "";
    }
  }
  return "";
}

// ownTags is the tags typed by hand on a session, which the box edits.
export function ownTags(repos: Repo[], id: string): string[] {
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      const found = (checkout.sessions ?? []).find((s) => s.id === id);
      if (found) return found.tags ?? [];
    }
  }
  return [];
}

// typedTags reads the box: commas or spaces between tags, no repeats, no "#".
export function typedTags(text: string): string[] {
  const out = text
    .split(/[\s,]+/)
    .map((one) => one.replace(/^#/, "").trim())
    .filter(Boolean);
  return [...new Set(out)];
}

import { attention, stateOf } from "../pages/agents";
import type { Active } from "../pages/agents";
export type { Active, Health } from "../pages/agents";
export { attention, stateOf } from "../pages/agents";

export function running(state: string): boolean {
  return state === "working" || state === "asking";
}

// What an issue in no project is filed under. Named rather than blank: an
// unlabelled group reads as a bug in the grouping.
export const NO_PROJECT = "No project";

// byProject keeps the server's order inside a project, and puts the
// projects in the order their first issue appears. Issues in none go last.
export function byProject(all: Issue[]): Issue[] {
  const order: string[] = [];
  const by = new Map<string, Issue[]>();
  for (const issue of all) {
    const key = issue.project || NO_PROJECT;
    if (!by.has(key)) {
      by.set(key, []);
      order.push(key);
    }
    by.get(key)!.push(issue);
  }
  const named = order.filter((k) => k !== NO_PROJECT);
  if (by.has(NO_PROJECT)) named.push(NO_PROJECT);
  return named.flatMap((k) => by.get(k) ?? []);
}

export function issues(dash: Dash & { error?: string }): Place {
  const started = dash.started ?? {};
  return {
    id: "issues",
    title: "Issues",
    icon: "issue",
    rows: byProject(dash.linear?.issues ?? []).map((t) => ({
      id: t.identifier,
      group: t.project || NO_PROJECT,
      // The play is this machine being on the ticket's branch, which is
      // not Linear's "In Progress": the tag is what says so.
      icon: started[t.identifier] ? "play" : "issue",
      tone: started[t.identifier] ? "warn" : undefined,
      tag: started[t.identifier] ? { text: "checked out", kind: "here" } : undefined,
      name: `${t.identifier} · ${t.title}`,
      url: t.url,
      meta: [t.state, t.priority ? `P${t.priority}` : "", ago(t.updatedAt)]
        .filter(Boolean)
        .join(" · "),
    })),
    // Linear has a key of its own, so its own reason for being quiet.
    note: dash.error ? `/api/dash — ${dash.error}` : (dash.linear?.error ?? ""),
  };
}

export function inbox(dash: Dash & { error?: string }): Place {
  return {
    id: "inbox",
    title: "Inbox",
    icon: "inbox",
    rows: (dash.notifications ?? [])
      .filter((n) => !n.dismissed)
      .map((n) => ({
        id: n.id,
        icon: n.type === "PullRequest" ? "pr" : "issue",
        name: `${n.repo} · ${n.title}`,
        meta: [n.reason.replace(/_/g, " "), ago(n.updatedAt)].filter(Boolean).join(" · "),
        url: n.url,
        pr: prOf(n.url),
      })),
    note: dash.error ? `/api/dash — ${dash.error}` : "",
  };
}

export interface Bus {
  online?: boolean;
  why?: string;
  user?: string;
  offers?: { id: string; kind: string; from: string; question?: string; options?: string[]; pr?: string; claimed?: boolean }[];
  replies?: { id: string; from: string; question?: string; text?: string; verdict?: string; at?: string }[];
  sent?: { id: string; to?: string; question?: string; pr?: string; at?: string }[];
  team?: Member[];
}

export interface Member {
  user: string;
  presence: string;
  status?: string;
  harness?: string;
  node?: string;
  agents?: number;
  you?: boolean;
}

// folded is one row per person. agentbus reports an instance, so a second
// ade on another machine was a second row naming the same person to ask.
export function folded(team: Member[]): (Member & { nodes: number })[] {
  const out = new Map<string, Member & { nodes: number }>();
  const machines = new Map<string, Set<string>>();
  for (const member of team) {
    const seen = machines.get(member.user) ?? new Set<string>();
    seen.add(member.node || member.harness || "");
    machines.set(member.user, seen);
    const held = out.get(member.user);
    if (!held) {
      out.set(member.user, { ...member, nodes: 0 });
      continue;
    }
    held.agents = (held.agents ?? 0) + (member.agents ?? 0);
    held.you = held.you || member.you;
    // The instance with something to say wins the line: one ade idle on a
    // laptop says nothing about the one running an agent beside it.
    if (member.presence === "active" && held.presence !== "active") {
      held.presence = member.presence;
      held.status = member.status;
      held.harness = member.harness;
    } else if (!held.status && member.status) {
      held.status = member.status;
    }
  }
  for (const [user, row] of out) row.nodes = machines.get(user)?.size ?? 1;
  return [...out.values()];
}

// team is the bus as a list: what is waiting on you, then what you
// asked for, then who is about.
// notConnected says why, where the launcher knows: "not connected" with
// no reason is what left a broken tailnet lookup invisible for an hour.
export function notConnected(why?: string): string {
  const said = "ade is not connected to agentbus.";
  return why ? `${said} ${why}` : said;
}

// offered is what teammates asked of you over agentbus: a question to answer
// or a review to claim. Team lists it, and so does Waiting on you.
export function offered(bus: Bus | undefined): Row[] {
  return (bus?.offers ?? []).map((offer) => ({
    id: offer.id,
    group: "Offered to you",
    icon: offer.kind === "review" ? "review" : "ask",
    tone: "warn" as const,
    name: offer.question || offer.pr || offer.kind,
    meta: [offer.from, offer.claimed ? "claimed" : ""].filter(Boolean).join(" · "),
    acts: [
      offer.kind === "review"
        ? { label: offer.claimed ? "Open" : "Claim", what: "bus-claim" as const, name: offer.id }
        : { label: "Answer", what: "bus-answer" as const, name: offer.id },
      { label: "Dismiss", what: "bus-dismiss" as const, name: offer.id },
    ],
  }));
}

export function team(bus: Bus | undefined, now = Date.now()): Place {
  const rows: Row[] = offered(bus);
  for (const reply of bus?.replies ?? []) {
    rows.push({
      id: reply.id,
      group: "Answers",
      icon: "answer",
      tone: "good",
      name: reply.text || reply.verdict || reply.question || "a reply",
      meta: [reply.from, ago(reply.at, now)].filter(Boolean).join(" · "),
      acts: [{ label: "Clear", what: "bus-dismiss" as const, name: reply.id }],
    });
  }
  for (const sent of bus?.sent ?? []) {
    rows.push({
      id: sent.id,
      group: "You asked",
      icon: "ask",
      name: sent.question || sent.pr || "a question",
      meta: [sent.to, ago(sent.at, now)].filter(Boolean).join(" · "),
    });
  }
  for (const member of folded(bus?.team ?? [])) {
    rows.push({
      id: `who:${member.user}`,
      group: "Who is about",
      icon: "people",
      tone: member.presence === "active" ? "good" : undefined,
      name: member.user + (member.you ? " · you" : ""),
      meta: [
        member.presence,
        member.harness,
        member.status,
        member.agents ? `${member.agents} running` : "",
        member.nodes > 1 ? `${member.nodes} machines` : "",
      ]
        .filter(Boolean)
        .join(" · "),
    });
  }
  return {
    id: "team",
    title: "Team",
    icon: "people",
    rows,
    count: (bus?.offers ?? []).length + (bus?.replies ?? []).length,
    note: bus?.online ? "" : notConnected(bus?.why),
  };
}

export * from "./myday";

// waiting is Notifications: what teammates asked of you over the bus, then
// the reviews of yours that are ready to move. The machine's, not one checkout's.
export function waiting(dash: Dash & { error?: string }, bus?: Bus): Place {
  const asked = offered(bus);
  const rows = (dash.reviews ?? [])
    .filter((r) => !r.muted && (r.review?.state === "ready" || r.review?.state === "drafted"))
    .map(reviewRow)
    // Grouped only when there is a second list to tell it from.
    .map((row) => ({ ...row, group: asked.length > 0 ? "Yours to move" : undefined }));
  return {
    id: "waiting",
    title: "Notifications",
    icon: "bell",
    rows: [...asked, ...rows],
    alert: asked.length > 0,
    note: why(dash),
  };
}

export function offline(why: string): World {
  return {
    places: [
      {
        id: "sessions",
        title: "Sessions",
        icon: "session",
        rows: [{ id: "offline", icon: "warn", tone: "bad", name: why }],
      },
    ],
    status: { branch: "", checks: "", waiting: 0 },
  };
}

// What the picked session is doing: a review or a commit message in flight,
// and each named agent running beside it.
export interface Doing {
  running: boolean;
  // terminal is what the agent's own terminal is doing, or empty with none.
  terminal: string;
  kind: string;
  sides: { id: string; name: string }[];
  // The repository's own checks, in a few words.
  checks: string;
  // The session's own scope and branch: the scan's branch is the
  // checkout's, which is not the same thing.
  scope: string;
  branch: string;
  // The pull request for this branch, when there is one, and whether you
  // opened it: Resolve is for your own, Send the review for anybody's.
  pr?: number;
  prMine?: boolean;
  // How many commits the base has that this branch does not.
  behind?: number;
  // The last review by named agents, while the session remembers one.
  review?: ReviewRun;
}

export const NOTHING: Doing = {
  running: false,
  terminal: "",
  kind: "",
  sides: [],
  checks: "",
  scope: "",
  branch: "",
};

// doingIn reads it out of /api/state.
export function doingIn(got: unknown): Doing {
  const agent = ((got as { agent?: Record<string, unknown> })?.agent ?? {}) as {
    running?: boolean;
    kind?: string;
    sideRuns?: { id: string; name: string }[] | null;
    terminal?: string;
  };
  const branchPr = (got as { branchPr?: { number?: number; mine?: boolean } })?.branchPr;
  const tree = (got as { worktree?: { behind?: number } | null })?.worktree;
  const meta = ((got as { meta?: { scope?: string; branch?: string } })?.meta ?? {}) as {
    scope?: string;
    branch?: string;
  };
  return {
    running: Boolean(agent.running),
    terminal: agent.terminal ?? "",
    kind: agent.kind ?? "",
    sides: agent.sideRuns ?? [],
    checks: checksIn(got),
    scope: meta.scope ?? "",
    branch: meta.branch ?? "",
    pr: branchPr?.number,
    prMine: branchPr?.mine,
    behind: tree?.behind,
    review: reviewIn(got),
  };
}

// busy is whether anything is in flight, which is what decides how
// often the window asks again.
export function busy(doing: Doing): boolean {
  return doing.running || doing.sides.length > 0 || doing.terminal === "working" || Boolean(doing.review?.running);
}

// saying is what the status bar carries: what is running, by name,
// because "working" would not say which came back.
export function saying(doing: Doing): string {
  const bits: string[] = [];
  // A review says where it has got to: its reviewers are in that sentence.
  const stage = doing.review?.running ? doing.review.stage : "";
  if (stage) bits.push(stage);
  else if (doing.sides.length > 0) bits.push(doing.sides.map((s) => `@${s.name}`).join(", "));
  if (doing.running) bits.push(doing.kind || "working");
  return bits.join(" · ");
}

// A review by named agents as `/api/state` carries it: each reviewer, then
// the stage it has reached.
export interface Reviewer {
  name: string;
  state: string;
  doing: string;
  why: string;
  found: number;
  endedAt: string;
}

export interface ReviewRun {
  reviewers: Reviewer[];
  stage: string;
  summary: string;
  running: boolean;
  startedAt: string;
  endedAt: string;
}

function reviewIn(got: unknown): ReviewRun | undefined {
  const run = (got as { reviewRun?: Partial<ReviewRun> | null })?.reviewRun;
  if (!run?.startedAt) return undefined;
  return {
    reviewers: run.reviewers ?? [],
    stage: run.stage ?? "",
    summary: run.summary ?? "",
    running: Boolean(run.running),
    startedAt: run.startedAt,
    endedAt: run.endedAt ?? "",
  };
}

// lasted is a run's length in the units a person counts a review in.
export function lasted(from: string, to: string | number): string {
  const ms = (typeof to === "number" ? to : Date.parse(to)) - Date.parse(from);
  if (Number.isNaN(ms) || ms < 0) return "";
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
}

export interface ReviewLine {
  name: string;
  state: string;
  note: string;
}

// reviewLines is the strip's rows: each reviewer and how it stands.
export function reviewLines(run: ReviewRun, now = Date.now()): ReviewLine[] {
  return run.reviewers.map((r) => {
    const found = r.found === 1 ? "1 point" : `${r.found} points`;
    const bits =
      r.state === "running"
        ? [lasted(run.startedAt, now), r.doing]
        : r.state === "done"
          ? [found, lasted(run.startedAt, r.endedAt)]
          : r.state === "stopped"
            ? ["stopped", r.why, r.found ? found : ""]
            : [`failed: ${r.why}`];
    return { name: `@${r.name}`, state: r.state, note: bits.filter(Boolean).join(" · ") };
  });
}

// reviewHead is the strip's first line: going, or how long it took.
export function reviewHead(run: ReviewRun): string {
  if (run.running) return "Review · running";
  const took = lasted(run.startedAt, run.endedAt);
  return took ? `Review · finished in ${took}` : "Review · finished";
}

// What `/api/state` says about the repository's own checks.
export interface Verify {
  running?: boolean;
  passed?: boolean;
  stopped?: boolean;
  failed?: string;
  ran?: number;
  total?: number;
  narrow?: boolean;
}

// checksIn is the checks in a few words. Empty when the repository has
// none, which is not the same as none having run.
export function checksIn(got: unknown): string {
  const v = (got as { verify?: Verify })?.verify;
  if (!v) return "";
  const of = v.total ? ` ${v.ran ?? 0}/${v.total}` : "";
  if (v.running) return `checks${of}`;
  if (v.failed) return `${v.failed} failed`;
  if (v.stopped) return "checks stopped";
  if (v.passed) return v.narrow ? "checks pass (narrow)" : "checks pass";
  return "";
}

export function status(dash: Dash, repos: Repo[], doing: Doing = NOTHING, bus?: Bus): Status {
  const live = repos
    .flatMap((r) => r.checkouts ?? [])
    .find((c) => (c.sessions ?? []).some((s) => s.live));
  return {
    branch: doing.branch || (live?.branch ?? ""),
    checks: doing.checks,
    waiting: waiting(dash, bus).rows.length,
    scope: doing.scope,
    doing: saying(doing),
    pr: doing.pr,
    prMine: doing.prMine,
    behind: doing.behind,
  };
}

// Answer is a verb's own answer, parsed, or `{error}` when it failed.
export interface Answer {
  error?: string;
}

// answered opens it: the shell puts ade's own sentence in `error`, so a
// body without one is a verb that worked.
export function answered(got?: Answer): { error: string; body: unknown } {
  if (!got) return { error: "", body: undefined };
  if (got.error) return { error: got.error, body: undefined };
  return { error: "", body: got };
}

// stillListed drops the archived-here ids a read has caught up with.
export function stillListed(repos: Repo[], gone: ReadonlySet<string>): Set<string> {
  if (gone.size === 0) return new Set();
  const listed = new Set<string>();
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of checkout.sessions ?? []) {
        if (!session.archived) listed.add(session.id);
      }
    }
  }
  return new Set([...gone].filter((id) => listed.has(id)));
}

// knownSessions is every session this window may point at.
export function knownSessions(repos: Repo[], active: Active[] = []): Set<string> {
  const out = new Set<string>();
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of checkout.sessions ?? []) out.add(session.id);
    }
  }
  for (const each of active) out.add(each.id);
  return out;
}

// Where a session is on this machine. An announced one need not be
// under a scan root, and the launcher resolves it before the scan.
export function whereSessionsAre(repos: Repo[], active: Active[] = []): Map<string, string> {
  const out = new Map<string, string>();
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of checkout.sessions ?? []) out.set(session.id, checkout.path);
    }
  }
  for (const each of active) {
    if (each.dir) out.set(each.id, each.dir);
  }
  return out;
}

// Said is one thing in the history with a person, as the launcher keeps it.
export interface Said {
  at: string;
  id: string;
  kind: "ask" | "review" | "answer";
  mine?: boolean;
  agent?: boolean;
  question?: string;
  options?: string[];
  pr?: string;
  text?: string;
  option?: string;
  verdict?: string;
}

// historyPage is the conversation with one person, oldest first, as the
// window draws a document. Who said each thing is the first word of it.
export function historyPage(who: string, said: Said[], now = Date.now()): string {
  const out = [`# ${who}`, ""];
  if (said.length === 0) {
    out.push("Nothing said yet. What you ask and are asked on agentbus is kept here.");
    return out.join("\n");
  }
  for (const one of said) {
    const by = one.mine ? (one.agent ? "your agent" : "you") : one.agent ? `${who}'s agent` : who;
    out.push(`**${by}** · ${ago(one.at, now) || "just now"}`, "");
    if (one.kind === "answer") {
      const words = [one.option, one.text, one.verdict].filter(Boolean).join(" — ");
      out.push(words || "_answered_", "");
      continue;
    }
    if (one.kind === "review") out.push(`Asked for a review of ${one.pr ?? "a pull request"}`, "");
    if (one.question) out.push(`> ${one.question.split("\n").join("\n> ")}`, "");
    if ((one.options ?? []).length > 0) out.push(`Options: ${(one.options ?? []).join(" · ")}`, "");
  }
  return out.join("\n");
}

// CaughtUp is what one catch-up did.
// noBrief is the Brief tab when there is nothing to show: the status line
// is crowded while an agent works, and a tab stays until it is read.
export function noBrief(why: string): string {
  return [
    "# Brief",
    "",
    `${why.replace(/\.?$/, ".")}`,
    "",
    "ade reads the newest file under `.git/ade/docs`, in this worktree first and then the repository's own `.git`. Nothing has put one there for this session.",
  ].join("\n");
}

export interface Pulled {
  branch: string;
  pulled: number;
}

// pulled is the status line after a pull of the branch itself.
export function pulled(got: Pulled): string {
  if (got.pulled === 0) return `already up to date with ${got.branch}`;
  return `pulled ${got.pulled} commit${got.pulled === 1 ? "" : "s"} from ${got.branch}`;
}

export interface CaughtUp {
  base: string;
  behind: number;
  replayed: number;
  stashed?: boolean;
  resolved?: string[] | null;
  by?: string;
  stuck?: string[] | null;
  stale?: string;
}

// caughtUp says what happened, worst first: markers left in the tree are the
// one outcome that needs you before anything else does.
export function caughtUp(got: CaughtUp): string {
  if (got.behind === 0) return `already on ${got.base}`;
  const said = [`replayed ${got.replayed} on ${got.base}`];
  const fixed = (got.resolved ?? []).length;
  if (fixed > 0) said.push(`${got.by ?? "the agent"} settled ${fixed} conflict(s)`);
  const stuck = got.stuck ?? [];
  if (stuck.length > 0) {
    return `${said.join("; ")} — your work came back with conflicts in ${stuck.join(", ")}`;
  }
  if (got.stale) said.push("the base could not be fetched, so it may be old");
  return said.join("; ");
}

// DraftState is how far the review has got: waiting on GitHub, ready here,
// or written up by nobody.
export interface DraftState {
  pr?: number;
  review?: string;
  inDraft: number;
  ready: number;
  notGoing: number;
  error?: string;
}

// reviewNote says where the review stands, including what is going nowhere:
// the draft answers what will be sent and nothing else answers what was left.
export function reviewNote(at: DraftState): string {
  if (at.error) return `Could not read the review: ${at.error}`;
  const said = [
    at.inDraft > 0 ? `${at.inDraft} in the draft review` : "",
    at.ready > 0 ? `${at.ready} ready` : "",
    at.notGoing > 0 ? `${at.notGoing} not written up` : "",
  ].filter(Boolean);
  return said.join(" · ");
}

// servingDir is the conversation the ade in one checkout announced, or
// empty while none has. Never landOn, which falls back to any session.
export function servingDir(where: Map<string, string>, dir: string): string {
  if (!dir) return "";
  for (const [session, at] of where) {
    if (at === dir) return session;
  }
  return "";
}

// The session the window lands on with nothing picked. `dir` is what
// `ade .` named: that checkout's conversation, not the last one.
export function landOn(repos: Repo[], dir = ""): string {
  const all = repos
    .flatMap((r) => r.checkouts ?? [])
    .flatMap((c) => c.sessions ?? [])
    .filter((s) => !s.archived);
  const here = dir ? all.filter((s) => s.dir === dir) : [];
  // A live one, else the one touched last: the scan's order is the
  // roots' order, which is nobody's conversation.
  const from = here.length > 0 ? here : all;
  const newest = [...from].sort(
    (a, b) => Date.parse(b.lastUsed ?? "") - Date.parse(a.lastUsed ?? ""),
  );
  return (from.find((s) => s.live) ?? newest[0])?.id ?? "";
}


// landAgain is the session a window opens on. A checkout named by `ade .`
// wins, then the conversation the last run was in.
export function landAgain(repos: Repo[], known: Set<string>, dir: string, was: string): string {
  if (!dir && was && known.has(was)) return was;
  return landOn(repos, dir);
}
// Drafted is what an add to the review answered: what went into it, and
// what GitHub would not take.
export interface Drafted {
  drafted?: number;
  refused?: string[];
}

// draftSaid is that answer in one line. A refusal nobody reads is a comment
// the author never sees, so what was refused is said before what went.
export function draftSaid(body: Drafted, pr: number): string {
  const n = body.drafted ?? 0;
  const refused = body.refused ?? [];
  const went = `${n} in the draft review on #${pr}`;
  if (refused.length === 0) return went;
  if (n === 0) return refused.length === 1 ? refused[0] : `${refused.length} refused: ${refused[0]}`;
  return `${refused.length} refused: ${refused[0]} · ${went}`;
}

// sweptSaid is what a reset did. What it left is said as well as what went.
export function sweptSaid(body: unknown, what: string): string {
  const n = (body ?? {}) as { removed?: number; kept?: number };
  const gone = n.removed ?? 0;
  const kept = n.kept ?? 0;
  const left = kept === 0 ? "" : `; ${kept} on the pull request stay`;
  if (gone === 0) return kept === 0 ? `no ${what} to remove` : `nothing to remove: ${kept} on the pull request stay`;
  return `removed ${gone} ${gone === 1 ? "comment" : "comments"}${left}`;
}

// unserved is the launcher's answer for a session with no ade behind
// it. Not a failure to act: there was nothing there to act on.
export function unserved(error = ""): boolean {
  return /no ade is (serving|running)/.test(error);
}

// ago is a stamp as a glance reads it, or nothing for one that did not come.
export function ago(stamp: string | undefined, now = Date.now()): string {
  if (!stamp) return "";
  const at = Date.parse(stamp);
  // A Go time that was never set is year 1, which is no age to show.
  if (Number.isNaN(at) || at <= 0) return "";
  const m = Math.max(0, Math.round((now - at) / 60_000));
  if (m < 1) return "just now";
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.round(h / 24)}d`;
}

// Conversation is one conversation claude keeps, and how it stands with ade.
export interface Conversation {
  id: string;
  title: string;
  // The folder it was started in, which is where its session goes.
  dir: string;
  used: string;
  running: boolean;
  here: boolean;
  checkout: boolean;
}

// conversationRows are the omnibox's rows for them: the name, then where it
// was had and how it stands.
export function conversationRows(
  list: Conversation[],
  now = Date.now(),
): { label: string; note: string; value: string }[] {
  return list.map((one) => ({
    label: one.title || one.id.slice(0, 8),
    note: [(one.dir ?? "").split("/").pop() ?? "", standing(one), ago(one.used, now)].filter(Boolean).join(" · "),
    value: one.id,
  }));
}

// A session already on it comes first: the claude that has it open is ade's own.
function standing(one: Conversation): string {
  if (one.here) return "already in ade";
  if (!one.checkout) return "not a git checkout";
  return one.running ? "open in another terminal" : "";
}
