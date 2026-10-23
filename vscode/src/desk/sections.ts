import type { Issue, Listed } from "../client";
import type { Notification, Review, Started } from "../launcher";
import type { LauncherRow } from "../omnibox";
import type { Row } from "../switcher";
import { found } from "../switcher";
import { icon, needsYou, runnable, says } from "./prstate";

// TodoLine is a todo dressed on the extension side: the helpers that say
// when and who live beside node imports a webview bundle cannot carry.
export interface TodoLine {
  id: string;
  text: string;
  meta: string;
  icon: string;
  started?: string;
}

// Board is everything the dashboard shows, gathered once; sections is how
// it reads. A line acts: what it does is posted back as it stands.
export interface Board {
  waiting: LauncherRow[];
  reviews: Review[];
  notifications: Notification[];
  tickets: Issue[];
  ticketsError?: string;
  started: Record<string, Started>;
  mine: Listed[];
  sessions: Row[];
  designs: DesignRow[];
  todos: TodoLine[];
  agent?: string;
  at: number;
}

export interface Do {
  what: string;
  [key: string]: unknown;
}

export interface Line {
  icon: string;
  label: string;
  meta: string;
  do: Do;
  also?: { label: string; do: Do }[];
}

export interface Section {
  id: string;
  title: string;
  empty: string;
  lines: Line[];
  // A section whose rows read better under headings carries them instead:
  // the pull requests by who is waiting, the designs by repository.
  groups?: { name: string; lines: Line[] }[];
}

// DesignRow is one design in the shared store, with what this machine knows
// about work going on against it.
export interface DesignRow {
  name: string;
  dir: string;
  file: string;
  meta: { repos?: string[]; ticket?: string; title?: string };
  modified?: string;

  // repo is the first of its repositories, which is home. here is the
  // checkouts it resolves to on this machine, missing the ones it does not.
  repo?: string;
  here?: string[];
  missing?: string[];

  session?: string;
  branch?: string;
  live?: boolean;
}

// sections is the sidebar, top to bottom. What is waiting leads; the three
// that are read rather than acted on come last, collapsed.
export function sections(b: Board): Section[] {
  return [
    {
      id: "waiting",
      title: "Waiting on you",
      empty: "Nothing. The agents are either working or done.",
      lines: [
        ...b.waiting.map((r) => ({
          icon: r.icon ?? "circle-outline",
          label: r.label,
          meta: r.description ?? "",
          do: { what: "row", command: r.command, args: r.args ?? [] },
        })),
        // A review that finished while you were elsewhere, and a draft
        // nobody sent, are both things only you can move.
        ...b.reviews.filter(needsYou).map((r) => reviewLine(r, b.at)),
      ],
    },
    prs(b),
    designs(b),
    {
      id: "sessions",
      title: "Sessions",
      empty: "No session yet. Start one from the box with /new.",
      lines: b.sessions.map((r) => ({
        icon: r.waiting ? "bell" : r.session.live ? "debug-start" : "history",
        label: r.session.title || r.session.branch || r.session.name,
        meta: [found(r), r.waiting ? "asked you" : r.session.live ? "running" : ""].filter(Boolean).join(" · "),
        do: { what: "session", id: r.session.id },
      })),
    },
    {
      id: "todos",
      title: "Todos",
      empty: "Nothing to get to. ⌘⌥T captures one.",
      lines: b.todos.map((t) => ({
        icon: t.icon,
        label: t.text,
        meta: t.meta,
        do: { what: "todo", id: t.id },
        also: [
          ...(t.started ? [{ label: "Session", do: { what: "session", id: t.started } }] : []),
          { label: "Done", do: { what: "todo-done", id: t.id } },
        ],
      })),
    },
    {
      id: "tickets",
      title: "Issues in flight",
      empty: b.ticketsError ? `Linear: ${b.ticketsError}` : "No issue is assigned to you.",
      lines: [...b.tickets]
        .sort((x, y) => Number(inFlight(y, b.started)) - Number(inFlight(x, b.started)))
        .map((t) => {
          const started = b.started[t.identifier];
          return {
            icon: inFlight(t, b.started) ? "debug-start" : "issues",
            label: `${t.identifier} · ${t.title}`,
            meta: [t.state, t.priority ? `P${t.priority}` : "", started ? `on ${started.branch}` : "", ago(t.updatedAt, b.at)]
              .filter(Boolean)
              .join(" · "),
            do: started?.session ? { what: "session", id: started.session } : { what: "open-url", url: t.url },
            also: started
              ? [{ label: "Linear", do: { what: "open-url", url: t.url } }]
              : [{ label: "Start", do: { what: "start-ticket", identifier: t.identifier } }],
          };
        }),
    },
    {
      id: "inbox",
      title: "GitHub inbox",
      empty: "Nothing unread.",
      lines: b.notifications
        .filter((n) => !n.dismissed)
        .map((n) => ({
          icon: n.type === "PullRequest" ? "git-pull-request" : "issues",
          label: `${n.repo} · ${n.title}`,
          meta: [n.reason.replace(/_/g, " "), ago(n.updatedAt, b.at)].filter(Boolean).join(" · "),
          do: { what: "open-url", url: n.url },
          also: [{ label: "Dismiss", do: { what: "dismiss", id: n.id } }],
        })),
    },
  ];
}

// prs is both lists in one: what is waiting on your review, then your own.
// Grouped, because a row's meaning depends on which side it is.
function prs(b: Board): Section {
  const asked = b.reviews.filter((r) => !r.muted).map((r) => reviewLine(r, b.at));
  const mine = b.mine.map((p) => ({
    icon: p.draft ? "git-pull-request-draft" : "git-pull-request",
    label: `#${p.number} · ${p.title}`,
    meta: [p.headRef, p.checks ?? "", p.reviewDecision ?? "", ago(p.updatedAt, b.at)].filter(Boolean).join(" · "),
    do: { what: "open-pr", number: p.number },
    also: [{ label: "GitHub", do: { what: "open-url", url: p.url } }],
  }));
  return {
    id: "prs",
    title: "Pull requests",
    empty: "Nothing to review, and none of your own open.",
    lines: [...asked, ...mine],
    groups: [
      { name: "To review", lines: asked },
      { name: "Yours", lines: mine },
    ].filter((g) => g.lines.length > 0),
  };
}

// reviewLine is one pull request asked of you. The state leads the rest of
// the facts: it is what decides whether to open it at all.
function reviewLine(r: Review, at: number): Line {
  return {
    icon: icon(r),
    label: `${r.repo}#${r.number} · ${r.title}`,
    meta: [says(r), r.author, r.draft ? "draft" : "", r.checks ?? "", r.reviewDecision ?? "", ago(r.updatedAt, at)]
      .filter(Boolean)
      .join(" · "),
    do: { what: "open-review", repo: r.repo, number: r.number },
    also: [
      ...(runnable(r)
        ? [{ label: "Review it", do: { what: "run-review", repo: r.repo, number: r.number } }]
        : []),
      { label: "GitHub", do: { what: "open-url", url: r.url } },
      { label: "Mute", do: { what: "mute", repo: r.repo, number: r.number } },
    ],
  };
}

// designs is the shared store, grouped by the repository each is home to.
// A design the machine has no checkout for still shows, and says so.
function designs(b: Board): Section {
  const byRepo = new Map<string, Line[]>();
  for (const d of b.designs) {
    const where = d.repo ?? "No repository named";
    byRepo.set(where, [...(byRepo.get(where) ?? []), designLine(d, b.at)]);
  }
  return {
    id: "designs",
    title: "Designs",
    empty: "No designs yet. ade designs import brings in what a checkout holds.",
    lines: b.designs.map((d) => designLine(d, b.at)),
    groups: [...byRepo].map(([name, lines]) => ({ name, lines })),
  };
}

// designLine opens the session on a design that has one, and starts one on a
// design that does not. A repository this machine lacks is said, not hidden.
function designLine(d: DesignRow, at: number): Line {
  const spans = (d.meta.repos ?? []).length;
  const short = (d.missing ?? []).length > 0;
  return {
    icon: short ? "warning" : d.live ? "debug-start" : d.session ? "circle-filled" : "circle-outline",
    label: d.name,
    meta: [
      d.branch,
      d.live ? "running" : "",
      spans > 1 ? `${spans} repos` : "",
      short ? `needs ${(d.missing ?? []).join(", ")}` : "",
      d.meta.ticket ?? "",
      ago(d.modified, at),
    ]
      .filter(Boolean)
      .join(" · "),
    do: d.session
      ? { what: "session", id: d.session }
      : { what: "design-open", file: d.file },
    also: [
      { label: "Open", do: { what: "design-open", file: d.file } },
      ...(d.session || short
        ? []
        : [{ label: "Start work", do: { what: "design-start", name: d.name } }]),
    ],
  };
}

export function inFlight(t: Issue, started: Record<string, Started>): boolean {
  return Boolean(t.started || started[t.identifier] || /progress|started|review/i.test(t.state));
}

// ago is a stamp as a glance reads it, or nothing for one that did not come.
export function ago(stamp: string | undefined, now: number): string {
  if (!stamp) return "";
  const t = Date.parse(stamp);
  if (Number.isNaN(t)) return "";
  const m = Math.max(0, Math.round((now - t) / 60_000));
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}
