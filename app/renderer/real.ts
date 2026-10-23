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

export interface Design {
  name: string;
  dir: string;
  file: string;
  meta: { repos?: string[]; ticket?: string; title?: string };
  repo?: string;
  here?: string[];
  missing?: string[];
  session?: string;
  branch?: string;
  live?: boolean;
  modified?: string;
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
  const asked = all.filter((r) => !r.muted).map(reviewRow);
  const aside = all.filter((r) => r.muted).map((r) => ({ ...reviewRow(r), group: "Put aside" }));
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

// designs is the shared store and the documents written in the work: every
// open space's, or the picked checkout's when no space is open.
export function designs(
  all: Design[],
  here: string[] = [],
  where = "",
  open: { repo: string; path?: string; designs?: string[] }[] = [],
): Place {
  const mine: Row[] =
    open.length > 0
      ? open.flatMap((space) =>
          (space.designs ?? []).map((file) => ({
            // The space is in the id: which checkout a document is in is
            // what a reader of another space's design needs.
            id: `space:${space.path ?? ""}|${file}`,
            group: space.repo,
            icon: "design",
            name: file.replace(/\.md$/, ""),
            meta: "",
          })),
        )
      : here.map((file) => ({
          id: `here:${file}`,
          group: where ? `In ${where.split("/").pop()}` : "In this checkout",
          icon: "design",
          name: file.replace(/\.md$/, ""),
          meta: "",
        }));
  const place = store(all);
  return { ...place, rows: [...mine, ...place.rows] };
}

function store(all: Design[]): Place {
  return {
    id: "designs",
    title: "Designs",
    icon: "design",
    rows: all.map((d) => ({
      id: d.name,
      group: "Shared store" + (d.repo ? " · " + d.repo : ""),
      icon: (d.missing ?? []).length > 0 ? "warn" : d.live ? "play" : "ring",
      tone: (d.missing ?? []).length > 0 ? "bad" : d.live ? "good" : undefined,
      name: d.name,
      meta: [
        d.branch,
        d.live ? "running" : "",
        (d.meta.repos ?? []).length > 1 ? `${d.meta.repos?.length} repos` : "",
        (d.missing ?? []).length > 0 ? `needs ${d.missing?.join(", ")}` : "",
        d.meta.ticket,
        ago(d.modified),
      ]
        .filter(Boolean)
        .join(" · "),
      // One this machine cannot check out has nothing to start, and
      // one already being worked on is open rather than started.
      acts:
        (d.missing ?? []).length > 0 || d.live
          ? []
          : [{ label: "Start work", what: "design-start" as const, name: d.name }],
    })),
  };
}

export interface Machine {
  machine: string;
  label?: string;
  repos?: Repo[] | null;
}

// RUNNING is the group every conversation that is up appears in, above
// the repositories: what is open is what you are coming back to.
const RUNNING = "Running";

export function sessions(
  repos: Repo[],
  here?: string,
  now = Date.now(),
  machines: Machine[] = [],
  active: Active[] = [],
): Place {
  // By repository, not by checkout: three worktrees of ade are three
  // ways into one repository, not three repositories called ade. What
  // is up is lifted out of them and listed first, in one group.
  const up: { at: number; row: Row }[] = [];
  const groups: { name: string; at: number; rows: Row[] }[] = [];
  for (const repo of repos) {
    const name = repo.owner ? `${repo.owner}/${repo.name}` : repo.name;
    const found: { at: number; row: Row }[] = [];
    for (const checkout of repo.checkouts ?? []) {
      for (const session of (checkout.sessions ?? []).filter((s) => !s.archived)) {
        (session.live ? up : found).push({ at: when(session.lastUsed), row: {
          id: session.id,
          group: session.live ? RUNNING : name,
          icon: session.live ? "play" : "history",
          tone: session.live ? ("good" as const) : undefined,
          name: session.title || session.branch || session.name,
          // The worktree names itself now the heading does not, and the
          // branch only when it differs, or `fix` on `fix` says it twice.
          meta: [
            checkout.worktree ? checkout.name : "",
            session.branch === checkout.branch ? "" : session.branch,
            ago(session.lastUsed, now),
          ]
            .filter(Boolean)
            .join(" · "),
          tag: session.dir === here ? { text: "here", kind: "here" as const } : undefined,
          // A running session can be stopped from its row: it is the
          // thing you want when you see one still going.
          acts: session.live
            ? [{ label: "Stop", what: "session-stop" as const, name: session.id }]
            : [],
        } });
      }
    }
    // A checkout an ade is serving with nothing said in it yet: without
    // a row it cannot be picked, so Review has nothing to follow.
    for (const checkout of repo.checkouts ?? []) {
      const spoken = (checkout.sessions ?? []).some((s) => !s.archived);
      const serving = active.find((a) => !a.machine && a.dir === checkout.path);
      if (spoken || !serving) continue;
      up.push({ at: now, row: {
        id: serving.id,
        group: RUNNING,
        icon: "play",
        tone: "good" as const,
        name: serving.branch || checkout.name,
        meta: [checkout.worktree ? checkout.name : "", "no conversation yet"]
          .filter(Boolean)
          .join(" · "),
        tag: checkout.path === here ? { text: "here", kind: "here" as const } : undefined,
      } });
    }
    if (found.length === 0) continue;
    found.sort((a, b) => b.at - a.at);
    groups.push({ name, at: found[0].at, rows: found.map((f) => f.row) });
  }
  groups.sort((a, b) => a.name.localeCompare(b.name));
  // By name, not by when it last spoke: a list ordered by recency
  // reshuffles under the pointer every time an agent writes a line.
  up.sort((a, b) => a.row.name.localeCompare(b.row.name) || a.row.id.localeCompare(b.row.id));

  // Another machine's, after this one's: the proxy forwards a session
  // it does not serve to the launcher that does.
  const away: Row[] = [];
  for (const machine of machines) {
    const name = machine.label || machine.machine;
    const rows: Row[] = [];
    for (const repo of machine.repos ?? []) {
      for (const checkout of repo.checkouts ?? []) {
        for (const session of (checkout.sessions ?? []).filter((s) => !s.archived)) {
          rows.push({
            id: session.id,
            group: name,
            icon: session.live ? "play" : "history",
            tone: session.live ? ("good" as const) : undefined,
            name: session.title || session.branch || session.name,
            meta: [repo.name, ago(session.lastUsed, now)].filter(Boolean).join(" · "),
            tag: { text: machine.machine, kind: "where" },
          });
        }
      }
    }
    rows.sort((a, b) => a.name.localeCompare(b.name));
    away.push(...rows);
  }

  return {
    id: "sessions",
    title: "Sessions",
    icon: "session",
    rows: [...up.map((u) => u.row), ...groups.flatMap((g) => g.rows), ...away],
  };
}

// The tree, the states and the ordering are `vscode/src/agents.ts`:
// one answer for both windows. Renamed, as waiting() is taken here.
import {
  detail,
  queues,
  tree,
  waiting as wantingYou,
} from "../../vscode/src/agents";
import type { Active } from "../../vscode/src/agents";
export type { Active, Health } from "../../vscode/src/agents";
export { attention, stateOf } from "../../vscode/src/agents";

const TONE: Record<string, Row["tone"]> = {
  failed: "bad",
  wedged: "bad",
  asking: "bad",
  working: "warn",
  idle: undefined,
};

export function running(state: string): boolean {
  return state === "working" || state === "asking";
}

// Ending is one question with four answers, worst last. What is running
// decides the first of them: there is no run to stop when nothing is going.
export interface Ending {
  label: string;
  note: string;
  value: "agent" | "ade" | "archive" | "delete";
}

export function endings(state: string): Ending[] {
  const out: Ending[] = [];
  if (running(state)) {
    out.push({ label: "Stop the agent", note: "the turn stops; ade keeps serving the checkout", value: "agent" });
  }
  out.push(
    { label: "Stop ade here", note: "the server for this checkout goes too", value: "ade" },
    { label: "Archive the session", note: "out of the lists, kept on disk", value: "archive" },
    { label: "Delete the session", note: "gone from ade for good", value: "delete" },
  );
  return out;
}

// ended is what was done, in the words of the thing that was done to.
export function ended(what: string): string {
  switch (what) {
    case "agent":
      return "stopped";
    case "ade":
      return "ade stopped here";
    case "archive":
      return "archived";
    default:
      return "deleted";
  }
}

export function agents(all: Active[]): Place {
  const rows: Row[] = [];
  for (const branch of tree(all)) {
    const { row } = branch;
    rows.push({
      id: row.id,
      icon: row.state === "idle" ? "ring" : "dot",
      tone: TONE[row.state],
      name: row.name,
      meta: detail(row),
      tag: row.machine ? { text: row.machine, kind: "where" } : undefined,
      // One verb for the whole session: stopping the run, stopping the ade
      // under it, and putting the conversation away are the same decision.
      acts: row.machine
        ? []
        : [
            {
              label: running(row.state) ? "Stop…" : "Put away…",
              what: "session-end" as const,
              name: row.session,
            },
          ],
    });
    for (const child of branch.children) {
      rows.push({
        id: child.id,
        depth: 1,
        icon: "dot",
        tone: "warn",
        name: child.name,
        meta: detail(child),
      });
    }
  }
  const wanted = wantingYou(all);
  return {
    id: "agents",
    title: "Agents",
    icon: "robot",
    rows,
    alert: wanted > 0,
    count: wanted,
    note: "No agent is running anywhere.",
  };
}

function when(stamp: string | undefined): number {
  const at = stamp ? Date.parse(stamp) : Number.NaN;
  return Number.isNaN(at) ? 0 : at;
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
    title: "GitHub inbox",
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
  offers?: { id: string; kind: string; from: string; question?: string; pr?: string; claimed?: boolean }[];
  replies?: { id: string; from: string; question?: string; text?: string; verdict?: string; at?: string }[];
  sent?: { id: string; to?: string; question?: string; pr?: string; at?: string }[];
  team?: {
    user: string;
    presence: string;
    status?: string;
    harness?: string;
    agents?: number;
    you?: boolean;
  }[];
}

// team is the bus as a list: what is waiting on you, then what you
// asked for, then who is about.
// notConnected says why, where the launcher knows: "not connected" with
// no reason is what left a broken tailnet lookup invisible for an hour.
export function notConnected(why?: string): string {
  const said = "ade is not connected to agentbus.";
  return why ? `${said} ${why}` : said;
}

export function team(bus: Bus | undefined, now = Date.now()): Place {
  const rows: Row[] = [];
  for (const offer of bus?.offers ?? []) {
    rows.push({
      id: offer.id,
      group: "Offered to you",
      icon: offer.kind === "review" ? "review" : "ask",
      tone: "warn",
      name: offer.question || offer.pr || offer.kind,
      meta: [offer.from, offer.claimed ? "claimed" : ""].filter(Boolean).join(" · "),
    });
  }
  for (const reply of bus?.replies ?? []) {
    rows.push({
      id: reply.id,
      group: "Answers",
      icon: "answer",
      tone: "good",
      name: reply.text || reply.verdict || reply.question || "a reply",
      meta: [reply.from, ago(reply.at, now)].filter(Boolean).join(" · "),
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
  for (const member of bus?.team ?? []) {
    rows.push({
      id: `who:${member.user}`,
      group: "Who is about",
      icon: "people",
      tone: member.presence === "active" ? "good" : undefined,
      name: member.user + (member.you ? " · you" : ""),
      meta: [member.presence, member.harness, member.status, member.agents ? `${member.agents} running` : ""]
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

export interface Todo {
  id: string;
  text: string;
  note?: string;
  due?: string;
  from?: string;
  started?: string;
  doneAt?: string;
}

// todos is the list, the overdue first and the done left out: a todo
// that is finished is history, and this is what is waiting.
export function todos(items: Todo[], now = Date.now()): Place {
  const rows: Row[] = items
    .filter((item) => !item.doneAt)
    .map((item) => {
      const due = item.due ? Date.parse(item.due) : Number.NaN;
      const late = !Number.isNaN(due) && due < now;
      return {
        id: item.id,
        icon: item.started ? "play" : late ? "warn" : "todo",
        tone: late ? ("bad" as const) : item.started ? ("good" as const) : undefined,
        name: item.text,
        meta: [item.note, item.due ? (late ? "overdue" : `due ${ago(item.due, now)}`) : "", item.from]
          .filter(Boolean)
          .join(" · "),
        // A todo is a thing to do, so the row does it: a session with
        // the todo as its first message. One already started has one.
        acts: item.started
          ? []
          : [{ label: "Start", what: "todo-start" as const, name: item.id }],
      };
    });
  return { id: "todos", title: "Todos", icon: "todo", rows };
}

// One turn waiting behind the run, in the order it will go.
export interface Waiting {
  id: string;
  kind: string;
  what?: string;
}

// queued is every session's queue, each turn under the session it is
// stacked behind: turns with no session say when but not against what.
export function queued(all: Active[]): Row[] {
  return queues(all).map((each) => ({
    id: `q:${each.id}`,
    group: `Queued · ${each.where}`,
    icon: each.position === 1 ? "play" : "ring",
    name: each.what || each.kind,
    meta: [`${each.position}`, each.kind].join(" · "),
    acts:
      each.position === 1
        ? []
        : [
            {
              label: "Next",
              what: "queue-first" as const,
              name: each.id,
              on: each.session,
            },
          ],
  }));
}

// One scheduled job, as `/api/jobs` lists them.
export interface Job {
  name: string;
  description?: string;
  schedule: string;
  project?: string;
  nextDue?: string;
  lastFired?: string;
  paused?: boolean;
  failing?: boolean;
}

// jobs is what ade does on a timer. A list, not a report: you want to
// run one now or stop it running again.
export function jobs(all: Job[], now = Date.now()): Place {
  return {
    id: "jobs",
    title: "Jobs",
    icon: "ring",
    count: all.filter((j) => j.failing).length,
    rows: all.map((job) => ({
      id: `job:${job.name}`,
      icon: job.paused ? "ring" : job.failing ? "warn" : "play",
      tone: job.failing ? ("bad" as const) : job.paused ? undefined : ("good" as const),
      name: job.name,
      meta: [
        job.schedule,
        job.paused ? "paused" : job.nextDue ? `next ${ago(job.nextDue, now)}` : "",
        job.lastFired ? `last ${ago(job.lastFired, now)}` : "",
        job.project,
      ]
        .filter(Boolean)
        .join(" · "),
      acts: [
        { label: "Run now", what: "job-run" as const, name: job.name },
        {
          label: job.paused ? "Resume" : "Pause",
          what: "job-pause" as const,
          name: job.name,
        },
      ],
    })),
  };
}

// waiting is the desk's first list, and the machine's rather than this
// checkout's: every repository's reviews, every session's queues.
export function waiting(dash: Dash & { error?: string }, active: Active[] = []): Place {
  const stacked = queued(active);
  const rows = (dash.reviews ?? [])
    .filter((r) => !r.muted && (r.review?.state === "ready" || r.review?.state === "drafted"))
    .map(reviewRow)
    // Grouped only when there is a second list to tell it from.
    .map((row) => ({ ...row, group: stacked.length > 0 ? "Yours to move" : undefined }));
  return {
    id: "waiting",
    title: "Waiting on you",
    icon: "bell",
    rows: [...rows, ...stacked],
    note: why(dash),
  };
}

export function offline(why: string): World {
  return {
    places: [
      {
        id: "waiting",
        title: "Waiting on you",
        icon: "bell",
        rows: [{ id: "offline", icon: "warn", tone: "bad", name: why }],
      },
    ],
    status: {
      repo: "",
      branch: "",
      sandboxed: false,
      checks: "",
      waiting: 0,
      model: "",
    },
    blame: "",
  };
}

// What the picked session is doing: the main agent, and each named
// agent running beside it.
export interface Doing {
  running: boolean;
  kind: string;
  model: string;
  sandbox: boolean;
  sides: { id: string; name: string }[];
  queued: number;
  // on is the conversation the server is actually in, which picking one
  // in the window does not change.
  on: string;
  provider: string;
  // The repository's own checks, in a few words.
  checks: string;
  // The session's own scope and branch: the scan's branch is the
  // checkout's, which is not the same thing.
  scope: string;
  branch: string;
  // The pull request for this branch, when there is one.
  pr?: number;
  // How many commits the base has that this branch does not.
  behind?: number;
}

export const NOTHING: Doing = {
  running: false,
  kind: "",
  model: "",
  sandbox: false,
  sides: [],
  queued: 0,
  on: "",
  provider: "",
  checks: "",
  scope: "",
  branch: "",
};

// doingIn reads it out of /api/state.
export function doingIn(got: unknown): Doing {
  const agent = ((got as { agent?: Record<string, unknown> })?.agent ?? {}) as {
    running?: boolean;
    kind?: string;
    model?: string;
    sandbox?: boolean;
    sideRuns?: { id: string; name: string }[] | null;
    queued?: number;
  };
  const chat = ((got as { chat?: { session?: string } })?.chat ?? {}) as { session?: string };
  const branchPr = (got as { branchPr?: { number?: number } })?.branchPr;
  const tree = (got as { worktree?: { behind?: number } | null })?.worktree;
  const meta = ((got as { meta?: { scope?: string; branch?: string } })?.meta ?? {}) as {
    scope?: string;
    branch?: string;
  };
  return {
    running: Boolean(agent.running),
    kind: agent.kind ?? "",
    model: agent.model ?? "",
    sandbox: Boolean(agent.sandbox),
    sides: agent.sideRuns ?? [],
    queued: agent.queued ?? 0,
    on: chat.session ?? "",
    provider: (agent as { provider?: string }).provider ?? "",
    checks: checksIn(got),
    scope: meta.scope ?? "",
    branch: meta.branch ?? "",
    pr: branchPr?.number,
    behind: tree?.behind,
  };
}

// busy is whether anything is in flight, which is what decides how
// often the window asks again.
export function busy(doing: Doing): boolean {
  return doing.running || doing.sides.length > 0;
}

// saying is what the status bar carries: what is running, by name,
// because "working" would not say which came back.
export function saying(doing: Doing): string {
  const bits: string[] = [];
  if (doing.sides.length > 0) bits.push(doing.sides.map((s) => `@${s.name}`).join(", "));
  if (doing.running) bits.push(doing.kind || "working");
  if (doing.queued > 0) bits.push(`${doing.queued} queued`);
  return bits.join(" · ");
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

export function status(dash: Dash, repos: Repo[], doing: Doing = NOTHING): Status {
  const live = repos
    .flatMap((r) => r.checkouts ?? [])
    .find((c) => (c.sessions ?? []).some((s) => s.live));
  return {
    repo: repos[0]?.name ?? "",
    branch: doing.branch || (live?.branch ?? ""),
    sandboxed: doing.sandbox,
    checks: doing.checks,
    waiting: waiting(dash).rows.length,
    model: doing.model,
    scope: doing.scope,
    doing: saying(doing),
    pr: doing.pr,
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

// knownSessions is every conversation this window may point at. Not
// whereSessionsAre: a remote one has no path here but is still a pick.
export function knownSessions(
  repos: Repo[],
  active: Active[] = [],
  machines: Machine[] = [],
): Set<string> {
  const out = new Set<string>();
  for (const repo of [...repos, ...machines.flatMap((m) => m.repos ?? [])]) {
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
    // Another machine's checkout is not a path here: a session there is
    // reached through the proxy, and starting it is that machine's job.
    if (!each.machine && each.dir) out.set(each.id, each.dir);
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

// unserved is the launcher's answer for a session with no ade behind
// it. Not a failure to act: there was nothing there to act on.
export function unserved(error = ""): boolean {
  return /no ade is serving/.test(error);
}

// ago is a stamp as a glance reads it, or nothing for one that did not
// come. The same rule the extension's own lists use.
export function ago(stamp: string | undefined, now = Date.now()): string {
  if (!stamp) return "";
  const at = Date.parse(stamp);
  if (Number.isNaN(at)) return "";
  const m = Math.max(0, Math.round((now - at) / 60_000));
  if (m < 1) return "just now";
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.round(h / 24)}d`;
}
