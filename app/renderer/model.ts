import { wired } from "./doing";
import { VOCABULARY } from "./verbs";
import type { At, Change, Thread } from "./changes";
import type { Seen } from "./spacepage";


export interface Row {
  id: string;
  name: string;
  meta?: string;
  icon: string;
  tone?: "good" | "warn" | "bad";
  tag?: { text: string; kind: string };
  // A session's tags: its ticket, its brief, and the ones typed by hand.
  tags?: { text: string; kind: string }[];
  group?: string;
  // Where this row is, when it is somewhere other than ade: a
  // notification and an issue both live on a website.
  url?: string;
  // The pull request a row is about, where it is about one. A
  // notification carries a url and not an id, so this is read off it.
  pr?: { repo: string; number: number };
  // What a row does besides open, drawn on hover unless `menu`, which
  // keeps it to the context menu. `on` is the session it runs against.
  acts?: { label: string; what: Act; name: string; on?: string; menu?: true }[];
}

// What /api/permissions answers with: what ade refuses, and where it is kept.
export interface Rules {
  deny: string[];
  file: string;
}

export type { At } from "./changes";

export type Act =
  | "session-end"
  | "session-tags"
  | "brief-open"
  | "brief-send"
  | "space-close"
  | "space-clone"
  | "pr-web"
  | "pr-mute"
  | "pr-unmute"
  | "bus-dismiss"
  | "bus-claim"
  | "bus-answer";

export type View =
  // The Diff tab with no file picked yet.
  | { kind: "diff"; title: string }
  // One file of the diff, with the threads anchored in it drawn under
  // the lines they are about.
  // editing swaps the drawn diff for the file over its left side in the
  // editor, in the same tab: a finding is fixed where it is read.
  | {
      kind: "hunks";
      title: string;
      file: Change;
      threads?: Thread[];
      at?: At;
      editing?: boolean;
      base?: string;
      source?: string;
      sum?: string;
    }
  // What ade refuses an agent, drawn here rather than framed from
  // the server's own page: a frame wears its own stylesheet.
  | { kind: "rules"; title: string; rules: Rules }
  // A document the window drew: read-only.
  | { kind: "doc"; title: string; body: string; again?: "idle" | "busy"; brief?: true }
  // A conversation with one person on agentbus, and the box to say more.
  | { kind: "person"; title: string; who: string; body: string; count: number }
  // A repository kept open, and the sessions in it.
  | { kind: "space"; title: string; seen: Seen }
  | { kind: "term"; title: string }
  // The agent's own interface, running in the session's checkout.
  | { kind: "agent"; title: string }
  | { kind: "none" };

export interface Place {
  id: string;
  title: string;
  icon: string;
  rows: Row[];
  // files makes this place a tree rather than a list. The Diff is the one.
  files?: { path: string; status?: "added" | "changed" | "removed" }[];

  count?: number;

  // What an empty list says: the read that failed, or why there is
  // nothing. A list that is empty and silent reads as a broken window.
  note?: string;

  // alert paints the activity bar icon itself, not a badge on it: a
  // number is a thing to read, a colour is a thing you see across a room.
  alert?: boolean;
}

export interface Status {
  branch: string;
  checks: string;
  waiting: number;
  // What the picked session is doing right now, named: "@security,
  // @tester" rather than a spinner.
  doing?: string;
  // Which view of the diff Review is showing.
  scope?: string;
  // The pull request this branch has, when it has one: nothing can be said
  // on GitHub without it.
  pr?: number;
  // How many commits the base has that this branch does not.
  behind?: number;
}

export interface World {
  places: Place[];
  status: Status;
}

export function grouped(rows: Row[]): { name: string; rows: Row[] }[] {
  const out: { name: string; rows: Row[] }[] = [];
  for (const row of rows) {
    const name = row.group ?? "";
    const held = out.find((g) => g.name === name);
    if (held) held.rows.push(row);
    else out.push({ name, rows: [row] });
  }
  return out;
}

// A verb the window can do, as the omnibox lists it. The keyboard table
// names the same commands, so a binding and a box entry never disagree.
export interface Verb {
  command: string;
  name: string;
  icon: string;
  keys?: string;
  group?: string;
  live?: boolean;
}

// The group each command sits under decides its icon: the table in
// menu.ts carries the meaning, and this only dresses it.
const ICONS: Record<string, string> = {
  Sessions: "session",
  Diff: "diff",
  Review: "review",
  "Commit and pull request": "pr",
  "Lists": "inbox",
  Team: "people",
  "ade itself": "shield",
  Other: "dot",
};

export const VERBS: Verb[] = VOCABULARY.flatMap(({ group, verbs }) =>
  verbs.map(([command, name]) => ({
    command,
    name,
    icon: ICONS[group] ?? "dot",
    group,
    // Listed either way, because the vocabulary is ade's; marked,
    // because a verb that does nothing reads as broken.
    live: Boolean(wired(command)),
  })),
);

// BAR is the bar on the right, top to bottom: a button each, My Day between
// the first two. SIDE is every list its panel can show, the unbuttoned included.
export const BAR = ["waiting", "prs", "inbox", "team"];
export const SIDE = [...BAR, "issues", "spaces"];

const ORDER = ["sessions", ...SIDE, "review"];

// inBarOrder puts the places in the bar's order, whatever order they were
// built in. One the bar does not name goes last, rather than vanishing.
export function inBarOrder(places: Place[]): Place[] {
  const at = (p: Place) => {
    const i = ORDER.indexOf(p.id);
    return i < 0 ? ORDER.length : i;
  };
  return [...places].sort((a, b) => at(a) - at(b));
}

export type Hit =
  | { kind: "row"; row: Row; where: string }
  | { kind: "verb"; verb: Verb };

export function hitName(hit: Hit): string {
  return hit.kind === "row" ? hit.row.name : hit.verb.name;
}

export function hitIcon(hit: Hit): string {
  return hit.kind === "row" ? hit.row.icon : hit.verb.icon;
}

export function hitWhere(hit: Hit): string {
  if (hit.kind === "row") return hit.where;
  return hit.verb.keys ?? hit.verb.group ?? "command";
}

// hits is what the omnibox offers: every row and every verb, matched on
// the name first and the place second.
export function hits(places: Place[], query: string): Hit[] {
  const rows: Hit[] = places.flatMap((place) =>
    place.rows.map((row) => ({ kind: "row" as const, row, where: place.title })),
  );
  const verbs: Hit[] = VERBS.map((verb) => ({ kind: "verb" as const, verb }));
  const all = [...rows, ...verbs];

  const want = query.trim().toLowerCase();
  // Nothing typed leads with the verbs that work, because a box that
  // opens on things it cannot do is worse than one that opens empty.
  if (!want) {
    const doable = verbs.filter((h) => h.kind === "verb" && h.verb.live);
    return [...doable.slice(0, 8), ...rows.slice(0, 4)];
  }

  return all
    .map((hit) => ({ hit, at: rank(hitName(hit), sought(hit), want) }))
    .filter((s) => s.at >= 0)
    .sort((a, b) => a.at - b.at)
    .slice(0, 12)
    .map((s) => s.hit);
}

// rank is lower for a better match. Every word counts, in any order, or
// "open space" misses "Open a space…" and the verb reads as missing.
export function rank(name: string, where: string, want: string): number {
  const low = name.toLowerCase();
  const at = low.indexOf(want);
  if (at === 0) return 0;
  if (at > 0) return 1 + at;
  const words = want.split(/\s+/).filter(Boolean);
  if (words.length > 1 && words.every((word) => low.includes(word))) return 200;
  return where.toLowerCase().includes(want) ? 500 : -1;
}

export function counted(place: Place): number {
  // A place may say its own number: Review counts what is waiting to
  // be sent, not how many files changed.
  if (place.count !== undefined) return place.count;
  return place.rows.filter((row) => row.tone === "warn" || row.tone === "bad")
    .length;
}

// matches is a list's filter: the name, what is said beside it, and its tags.
export function matches(row: Row, want: string): boolean {
  const tags = (row.tags ?? []).map((t) => t.text).join(" ");
  return `${row.name} ${row.meta ?? ""} ${tags}`.toLowerCase().includes(want.toLowerCase());
}

// sought is what a hit is found by besides its name: a session's tags count.
function sought(hit: Hit): string {
  if (hit.kind !== "row") return hitWhere(hit);
  return [hit.where, ...(hit.row.tags ?? []).map((t) => t.text)].join(" ");
}
