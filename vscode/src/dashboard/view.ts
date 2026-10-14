import type { Issue, Listed } from "../client";
import type { Notification, Review, Started } from "../launcher";
import type { LauncherRow } from "../omnibox";
import type { Row } from "../switcher";
import { found } from "../switcher";

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
}

export function sections(b: Board): Section[] {
  return [
    {
      id: "waiting",
      title: "Waiting on you",
      empty: "Nothing. The agents are either working or done.",
      lines: b.waiting.map((r) => ({
        icon: r.icon ?? "circle-outline",
        label: r.label,
        meta: r.description ?? "",
        do: { what: "row", command: r.command, args: r.args ?? [] },
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
      id: "reviews",
      title: "Reviews asked of you",
      empty: "No pull request is waiting on your review.",
      lines: b.reviews
        .filter((r) => !r.muted)
        .map((r) => ({
          icon: "git-pull-request",
          label: `${r.repo}#${r.number} · ${r.title}`,
          meta: [r.author, r.draft ? "draft" : "", r.checks ?? "", r.reviewDecision ?? "", ago(r.updatedAt, b.at)]
            .filter(Boolean)
            .join(" · "),
          do: { what: "open-review", repo: r.repo, number: r.number },
          also: [
            { label: "GitHub", do: { what: "open-url", url: r.url } },
            { label: "Mute", do: { what: "mute", repo: r.repo, number: r.number } },
          ],
        })),
    },
    {
      id: "mine",
      title: "Your pull requests",
      empty: "No open pull request of yours in this repository.",
      lines: b.mine.map((p) => ({
        icon: p.draft ? "git-pull-request-draft" : "git-pull-request",
        label: `#${p.number} · ${p.title}`,
        meta: [p.headRef, p.checks ?? "", p.reviewDecision ?? "", ago(p.updatedAt, b.at)].filter(Boolean).join(" · "),
        do: { what: "open-pr", number: p.number },
        also: [{ label: "GitHub", do: { what: "open-url", url: p.url } }],
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
  ];
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
