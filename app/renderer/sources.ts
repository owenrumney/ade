import type { From } from "./doing";

// Where a picker's list comes from, for the ones ade holds rather than the
// window. One read each, and one row shape, so a new picker is a row here.

export interface Pickable {
  label: string;
  note?: string;
  value: unknown;
}

// The sources the window fills itself, from what it already drew.
export type Held = "repo" | "session" | "scope";
export type Choice = Exclude<From, Held>;

export interface Source {
  at: "launcher" | "session";
  path: string;
  rows: (got: Record<string, unknown>) => Pickable[];
}

type Row = Record<string, string | number | boolean | undefined | null>;

export const SOURCES: Record<Choice, Source> = {
  // A repository to open as a space: the picker leaves out what is open
  // already and anything with no owner, which is a scratch directory.
  space: {
    at: "launcher",
    path: "/api/spaces",
    rows: (got) =>
      list(got.offered).map((r) => ({
        label: str(r.repo),
        note: [r.sessions ? `${String(r.sessions)} sessions` : "", str(r.branch)]
          .filter(Boolean)
          .join(" · "),
        value: r.repo,
      })),
  },
  openspace: {
    at: "launcher",
    path: "/api/spaces",
    rows: (got) =>
      list(got.spaces).map((r) => ({
        label: str(r.repo),
        note: r.here ? "" : "not cloned here",
        value: r.repo,
      })),
  },
  model: {
    at: "launcher",
    path: "/api/models",
    rows: (got) =>
      list(got.models).map((m) => ({ label: str(m.label) || str(m.id), value: m.id })),
  },
  role: {
    at: "launcher",
    path: "/api/roles",
    rows: (got) => list(got.roles).map((r) => ({ label: str(r.name), note: str(r.about), value: r.name })),
  },
  // The list the switch is gated on, so nothing offered is refused. The
  // checkout the session is already in is not somewhere to move it to.
  // Worktrees move the session to another checkout; branches switch the one
  // it is in. Each row carries the field the switch takes.
  worktree: {
    at: "session",
    path: "/api/worktrees/switch",
    rows: (got) => [
      ...list(got.worktrees)
        .filter((t) => t.path !== got.here)
        .map((t) => ({
          label: str(t.name),
          note: str(t.branch) || str(t.path),
          value: { path: str(t.path) },
        })),
      ...list(got.branches).map((b) => ({
        label: str(b.name),
        note: b.stashed ? `${String(b.stashed)} files set aside · ${str(b.at)}` : str(b.at),
        value: { branch: str(b.name) },
      })),
    ],
  },
  agent: {
    at: "session",
    path: "/api/state",
    rows: (got) =>
      list((got.agent as { sideRuns?: unknown })?.sideRuns).map((run) => ({
        label: `@${str(run.name)}`,
        note: run.startedAt ? `since ${new Date(String(run.startedAt)).toLocaleTimeString()}` : "",
        value: run.id,
      })),
  },
  // This machine is not somewhere else, so it is not offered.
  machine: {
    at: "launcher",
    path: "/api/hosts",
    rows: (got) =>
      list(got.hosts)
        .filter((h) => !h.local)
        .map((h) => ({ label: str(h.label) || str(h.name), note: str(h.name), value: h.name })),
  },
  // What the server will take as a reviewer: `reviews: true`, and user
  // scope only — a branch could otherwise ship the reviewer that reviews it.
  reviewer: {
    at: "session",
    path: "/api/agents",
    rows: (got) =>
      list(got.agents)
        .filter((a) => a.reviews && a.scope === "user")
        .map((a) => ({ label: `@${str(a.name)}`, note: str(a.description), value: a.name })),
  },
  // A resolved thread is not one to resolve, so it is not offered.
  // Two sources, because kept and proposed take different verbs.
  remembered: {
    at: "session",
    path: "/api/project",
    rows: (got) =>
      list(got.entries)
        .filter((e) => e.state !== "superseded")
        .map((e) => ({ label: str(e.heading), note: first1(e.body), value: e.id })),
  },
  proposed: {
    at: "session",
    path: "/api/project",
    rows: (got) =>
      list(got.proposed).map((e) => ({
        label: str(e.heading),
        note: first1(e.body),
        value: e.id,
      })),
  },
  // The requirements this change is checked against, split by whether
  // they are still being checked: the two take opposite verbs.
  drifted: {
    at: "session",
    path: "/api/drift",
    rows: (got) =>
      list(got.items)
        .filter((i) => !i.dismissed)
        .map((i) => ({ label: str(i.text), note: str(i.verdict), value: i.id })),
  },
  dismissed: {
    at: "session",
    path: "/api/drift",
    rows: (got) =>
      list(got.items)
        .filter((i) => i.dismissed)
        .map((i) => ({ label: str(i.text), note: "dismissed", value: i.id })),
  },
  thread: {
    at: "session",
    path: "/api/state",
    rows: (got) =>
      list(got.threads)
        .filter((t) => !t.resolved)
        .map((t) => ({
          label: `${str(t.file)}:${t.line ?? 0}`,
          note: first(t.comments),
          value: t.id,
        })),
  },
  reply: {
    at: "session",
    path: "/api/state",
    rows: (got) =>
      list(bus(got).replies).map((r) => ({
        label: str(r.from),
        note: str(r.text) || str(r.verdict) || str(r.question),
        value: r.id,
      })),
  },
  offer: {
    at: "session",
    path: "/api/state",
    rows: (got) =>
      list(bus(got).offers).map((o) => ({
        label: `${str(o.kind)} from ${str(o.from)}`,
        note: str(o.question) || str(o.pr),
        value: o.id,
      })),
  },
  // Anyone leads: the first to claim it wins, which is the common answer.
  person: {
    at: "session",
    path: "/api/bus/who",
    rows: (got) => {
      if (!got.online) return [];
      const seen = new Map<string, Pickable>();
      for (const row of list(got.instances)) {
        const user = str(row.user);
        if (!user || seen.has(user)) continue;
        const where = row.instance as { harness?: string; label?: string } | undefined;
        const bits = [str(row.presence), where?.harness, str(row.status) || where?.label];
        seen.set(user, { label: user, note: bits.filter(Boolean).join(" · "), value: user });
      }
      return [
        { label: "Anyone", note: "the first to claim it wins", value: "anyone" },
        ...[...seen.values()].sort((a, b) => a.label.localeCompare(b.label)),
      ];
    },
  },
};

function bus(got: Record<string, unknown>): { replies?: unknown; offers?: unknown } {
  return (got.bus as { replies?: unknown; offers?: unknown }) ?? {};
}

// The comment a thread opened with, which is what it is about.
function first(comments: unknown): string {
  return str(list(comments)[0]?.body).split("\n")[0];
}

function list(of: unknown): Row[] {
  return Array.isArray(of) ? (of as Row[]) : [];
}

function str(of: unknown): string {
  return typeof of === "string" ? of : "";
}

// first1 is a body's opening line, which is what a pick has room for.
function first1(body: unknown): string {
  return String(body ?? "").split("\n")[0].trim();
}
