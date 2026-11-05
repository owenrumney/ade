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

function list(of: unknown): Row[] {
  return Array.isArray(of) ? (of as Row[]) : [];
}

function str(of: unknown): string {
  return typeof of === "string" ? of : "";
}

