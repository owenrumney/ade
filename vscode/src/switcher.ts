import type { Repo, Session, Up, Waiting } from "./launcher";
import { age } from "./tree";

// One row of the switcher: a session, and the two things that decide where it
// sits — whether it is waiting on you, and whether it is running.
export interface Row {
  session: Session;
  repo: string;
  waiting: boolean;
  here: boolean;
  // asked is the line it ended on, when it ended on a question.
  asked?: string;
}

// Worth switching to now: still running, or it asked you something.
// Everything else is history, and the tree is where history lives.
export function open(row: Row): boolean {
  return Boolean(row.session.live) || row.waiting;
}

// A switcher is a flat list, so the repository becomes a column. An archived
// session is left out: putting one away is saying not this one.
export function flatten(
  repos: Repo[],
  waiting: Waiting[],
  root: string | undefined,
  serving: Up[] = [],
): Row[] {
  const asked = new Map(waiting.map((w) => [w.id, w.asked ?? ""]));
  // What is running comes from the launcher's own list, not the scan's flag:
  // the scan reads transcripts and is as old as its last sweep.
  const up = new Set(serving.map((one) => one.id));
  const out: Row[] = [];
  for (const repo of repos) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of checkout.sessions ?? []) {
        if (session.archived) continue;
        const live = up.size > 0 ? up.has(session.id) : Boolean(session.live);
        out.push({
          session: { ...session, live },
          repo: repo.name,
          waiting: asked.has(session.id),
          here: session.dir === root,
          asked: asked.get(session.id) || undefined,
        });
      }
    }
  }
  return out.sort(order);
}

// Asked you something, then running, then recent. A switcher you have to
// read is no faster than the tree it replaces.
export function order(a: Row, b: Row): number {
  if (a.waiting !== b.waiting) return a.waiting ? -1 : 1;
  if (Boolean(a.session.live) !== Boolean(b.session.live))
    return a.session.live ? -1 : 1;
  return (b.session.lastUsed ?? "").localeCompare(a.session.lastUsed ?? "");
}

// recent is the search's order: last used first and nothing else. A search
// reads top-down, so urgency ranking only hides the one you just closed.
export function recent(a: Row, b: Row): number {
  return stamp(b).localeCompare(stamp(a));
}

// running is the checkouts with an ade up, rather than every conversation the
// scan has seen. One the scan does not know still belongs: it is running.
export function running(
  rows: Row[],
  serving: Up[],
  root: string | undefined,
): Row[] {
  const known = new Map(rows.map((row) => [row.session.id, row]));
  const out: Row[] = [];
  for (const one of serving) {
    const found = known.get(one.id);
    if (found) {
      out.push(found);
      continue;
    }
    out.push({
      session: {
        id: one.id,
        agent: "",
        dir: one.dir,
        name: one.name,
        branch: one.branch ?? "",
        live: true,
      },
      repo: one.repo || one.name,
      waiting: false,
      here: one.dir === root,
    });
  }
  return out.sort(order);
}

// shortlist is what the switcher offers: anything waiting on you, always, then
// what is running, or — with nothing up — what asked, or the recent.
export function shortlist(
  all: Row[],
  serving: Up[],
  root: string | undefined,
): Row[] {
  const asking = all.filter((row) => row.waiting);
  const up = running(all, serving, root);
  const open_ = all.filter(open);
  const rest = up.length > 0 ? up : open_.length > 0 ? open_ : all;
  const held = new Set(asking.map((row) => row.session.id));
  return [...asking, ...rest.filter((row) => !held.has(row.session.id))];
}

// sections are the switcher's headings. Waiting gets its own, above the
// repositories: a bell on a row inside one is a thing you scroll past.
export function sections(rows: Row[]): Group[] {
  const asking = rows.filter((row) => row.waiting);
  const rest = rows.filter((row) => !row.waiting);
  const head: Group[] =
    asking.length > 0 ? [{ repo: "Waiting for you", rows: asking }] : [];
  return [...head, ...group(rest)];
}

// every is what a search looks through: the scan's sessions and any running
// one it has not swept yet, best-first and across every repository.
export function every(
  rows: Row[],
  serving: Up[],
  root: string | undefined,
): Row[] {
  const seen = new Set(rows.map((row) => row.session.id));
  const extra = running(rows, serving, root).filter(
    (row) => !seen.has(row.session.id),
  );
  return [...rows, ...extra].sort(recent);
}

// found is the row as a search reads it. A flat list has no heading, so the
// repository goes in the text, where it can be typed.
export function found(row: Row, now = Date.now()): string {
  return [row.repo, description(row), age(row.session.lastUsed, now)]
    .filter(Boolean)
    .join(" · ");
}

// Group is one repository's rows, under its name. The repository was on every
// row and repeated itself; as a heading it is said once.
export interface Group {
  repo: string;
  rows: Row[];
}

// Arrival order is kept, not name order: the rows come best-first, so the
// repository holding the most urgent session has to lead.
export function group(rows: Row[]): Group[] {
  const out: Group[] = [];
  for (const row of rows) {
    const last = out.find((g) => g.repo === row.repo);
    if (last) last.rows.push(row);
    else out.push({ repo: row.repo, rows: [row] });
  }
  return out;
}

// label is what the row reads as. The title is what the conversation is
// about; a session with none has only its branch to offer.
export function label(row: Row): string {
  const mark = row.waiting
    ? "$(bell) "
    : row.session.live
      ? "$(circle-filled) "
      : "";
  return mark + (row.session.title || row.session.branch || row.session.name);
}

// description is where it is, within the repository its heading already
// named. The branch, unless the label is already the branch.
export function description(row: Row): string {
  const bits: string[] = [];
  if (row.session.branch && row.session.branch !== row.session.title)
    bits.push(row.session.branch);
  if (row.here) bits.push("this window");
  return bits.join(" · ");
}

// detail is the rest: which agent, and whether switching means changing
// folder, which is the one consequence worth warning about before the click.
export function detail(row: Row): string {
  const bits = [row.session.agent].filter(Boolean);
  if (!row.session.resumable)
    bits.push("cannot be resumed — opens the folder only");
  else if (!row.here) bits.push("opens its folder in this window");
  return bits.join(" · ");
}

// A running session the scan has not stamped yet is in use now, so it sorts
// above every dated one rather than below the lot.
function stamp(row: Row): string {
  return row.session.lastUsed ?? (row.session.live ? "9999" : "");
}
