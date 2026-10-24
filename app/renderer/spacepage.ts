import { el } from "./omnibox";
import { ago, type Repo, type Session } from "./real";
import type { Space } from "./spaces";
import {
  against,
  count,
  heat,
  kinds,
  money,
  months,
  percent,
  spark,
  topLanguages,
  weeks,
  type Repo as Stats,
} from "../../vscode/src/repo/view";

// What a space opens as: its conversations, its designs and what the
// repository has been doing. Design: .git/ade/docs/space-page.md.

export interface SessionLine {
  id: string;
  name: string;
  meta: string;
  live?: boolean;
}

export interface DesignLine {
  // file is the path inside the repository, which is what the archive
  // takes; id carries the checkout it is in, which is what opens it.
  file: string;
  id: string;
  name: string;
}

export interface Seen {
  space: Space;
  sessions: SessionLine[];
  designs: DesignLine[];
  stats?: Stats;
  // Why there are no numbers: still reading, or the read failed.
  note?: string;
}

export type SpaceAct =
  | { what: "open-session"; id: string }
  | { what: "archive-session"; id: string }
  | { what: "open-design"; id: string }
  | { what: "archive-design"; id: string };

// sessionsOf is every conversation in the repository, newest first. An
// archived one is put away, so it is not a link.
export function sessionsOf(repo: Repo | undefined, now = Date.now()): SessionLine[] {
  const out: (SessionLine & { at: number })[] = [];
  for (const checkout of repo?.checkouts ?? []) {
    for (const session of (checkout.sessions ?? []).filter((s: Session) => !s.archived)) {
      out.push({
        id: session.id,
        name: session.title || session.name || session.id,
        live: session.live,
        at: Date.parse(session.lastUsed ?? "") || 0,
        meta: [
          session.branch ?? checkout.branch,
          checkout.worktree ? checkout.name : "main checkout",
          session.live ? "live" : ago(session.lastUsed, now),
        ]
          .filter(Boolean)
          .join(" · "),
      });
    }
  }
  out.sort((a, b) => b.at - a.at);
  return out.map(({ at: _at, ...line }) => line);
}

// designsOf is the documents written anywhere in the repository. The id
// is the one the designs list uses, so both open the same editor.
export function designsOf(space: Space): DesignLine[] {
  return (space.designs ?? []).map((file) => ({
    file,
    id: `${space.path ?? ""}|${file}`,
    name: file.replace(/\.md$/, ""),
  }));
}

// drawnFrom is what the page draws beside the repository's numbers. A page
// built from one of these is stale the moment it changes.
export function drawnFrom(seen: Seen): string {
  return [
    (seen.space.designs ?? []).join(","),
    seen.sessions.map((one) => `${one.id}.${one.live ? 1 : 0}.${one.meta}`).join(","),
    seen.stats ? "stats" : (seen.note ?? "reading"),
  ].join("|");
}

export function spacePage(seen: Seen, act: (did: SpaceAct) => void): HTMLElement {
  const out = el("div", "doc space-page");
  out.append(head(seen));
  out.append(
    list(
      "Sessions",
      seen.sessions.map((one) => ({
        name: one.name,
        meta: one.meta,
        tone: one.live ? "live" : "",
        open: () => act({ what: "open-session", id: one.id }),
        away: { label: "Archive", run: () => act({ what: "archive-session", id: one.id }) },
      })),
      "No conversations in this repository.",
    ),
  );
  out.append(
    list(
      "Designs",
      seen.designs.map((one) => ({
        name: one.name,
        meta: one.file,
        tone: "",
        open: () => act({ what: "open-design", id: one.id }),
        away: { label: "Archive", run: () => act({ what: "archive-design", id: one.file }) },
      })),
      "No design documents in this repository.",
    ),
  );
  if (!seen.stats) {
    out.append(el("p", "sub", seen.note ?? "Reading the repository…"));
    return out;
  }
  const grid = el("div", "cards");
  grid.append(activity(seen.stats), spent(seen.stats), languages(seen.stats), people(seen.stats));
  out.append(grid);
  return out;
}

function head(seen: Seen): HTMLElement {
  const box = el("header", "space-head");
  box.append(el("h1", "", seen.space.repo));
  const under = [
    seen.space.path,
    seen.stats ? against(seen.stats) : seen.space.branch,
    seen.stats && seen.stats.dirty > 0 ? `${seen.stats.dirty} uncommitted` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  box.append(el("div", "sub", under));
  return box;
}

interface Line {
  name: string;
  meta: string;
  tone: string;
  open: () => void;
  away: { label: string; run: () => void };
}

function list(title: string, lines: Line[], empty: string): HTMLElement {
  const box = el("section", "space-list");
  box.append(el("h2", "", `${title}${lines.length ? ` (${lines.length})` : ""}`));
  if (lines.length === 0) {
    box.append(el("p", "sub", empty));
    return box;
  }
  for (const line of lines) {
    const row = el("div", "space-row");
    const open = el("button", "link", line.name);
    open.addEventListener("click", line.open);
    row.append(open);
    if (line.tone) row.append(el("span", "pip", line.tone));
    row.append(el("span", "meta", line.meta));
    const away = el("button", "away", line.away.label);
    away.addEventListener("click", line.away.run);
    row.append(away);
    box.append(row);
  }
  return box;
}

function card(title: string): HTMLElement {
  const box = el("section", "card");
  box.append(el("h2", "", title));
  return box;
}

function activity(stats: Stats): HTMLElement {
  const box = card("Commit activity");
  box.classList.add("wide");
  const busiest = stats.days.reduce((n, d) => Math.max(n, d.commits), 0);
  box.append(
    el(
      "p",
      "sub",
      `${stats.commits} commits in the last year${stats.busiestDay ? ` · busiest ${stats.busiestDay}` : ""}`,
    ),
  );
  const cols = weeks(stats.days);
  const grid = el("div", "heat");
  for (const column of cols) {
    const week = el("div", "week");
    for (const day of column) {
      const cell = el("span", day ? `cell h${heat(day.commits, busiest)}` : "cell gap");
      if (day) cell.title = `${day.date} · ${day.commits} commits`;
      week.append(cell);
    }
    grid.append(week);
  }
  const scale = el("div", "months");
  for (const name of months(cols)) scale.append(el("span", "month", name ?? ""));
  // One scroller, so a month label cannot drift from the week it names.
  const scroller = el("div", "scroller");
  scroller.append(grid, scale);
  box.append(scroller);
  if (stats.last) {
    const last = el("div", "last");
    last.append(el("div", "subject", stats.last.subject));
    last.append(el("div", "meta", `${stats.last.sha} · ${stats.last.author}`));
    box.append(last);
  }
  return box;
}

function languages(stats: Stats): HTMLElement {
  const box = card("Languages");
  const total = stats.lines;
  const big = el("div", "big");
  big.append(el("span", "num", total.toLocaleString()));
  big.append(el("span", "unit", `lines · ${stats.files.toLocaleString()} files`));
  box.append(big);
  const bar = el("div", "bar");
  const legend = el("div", "legend");
  topLanguages(stats.languages).forEach((lang, i) => {
    const slice = el("span", `slice c${i % 6}`);
    slice.style.width = `${total > 0 ? (lang.lines / total) * 100 : 0}%`;
    slice.title = `${lang.name} · ${lang.lines.toLocaleString()} lines`;
    bar.append(slice);
    const row = el("div", "legend-row");
    row.append(el("span", `dot c${i % 6}`), el("span", "name", lang.name));
    row.append(el("span", "share", percent(lang.lines, total)));
    legend.append(row);
  });
  box.append(bar, legend);
  return box;
}

function spent(stats: Stats): HTMLElement {
  const box = card("What ade did here");
  box.append(el("p", "sub", "This machine's transcripts, last 30 days."));
  const figures = el("div", "figures");
  const figure = (value: string, label: string) => {
    const one = el("div", "figure");
    one.append(el("span", "num", value), el("span", "unit", label));
    figures.append(one);
  };
  figure(money(stats.agent.cost), "API value");
  figure(String(stats.agent.turns), "turns");
  figure(String(stats.agent.sessions), "sessions");
  figure(count(kinds(stats.agent).reduce((n, k) => n + k.value, 0)), "tokens");
  box.append(figures);
  const chart = el("div", "spark");
  for (const day of spark(stats.agent.byDay)) {
    const bar = el("span", "sparkbar");
    bar.style.height = `${Math.max(day.height * 100, day.cost > 0 ? 4 : 1)}%`;
    bar.title = `${day.key} · ${money(day.cost)}`;
    chart.append(bar);
  }
  box.append(chart);
  return box;
}

function people(stats: Stats): HTMLElement {
  const box = card("Contributors");
  const top = stats.contributors.slice(0, 8);
  const most = top.reduce((n, p) => Math.max(n, p.commits), 0);
  for (const person of top) {
    const row = el("div", "person");
    row.append(el("span", "who", person.name));
    const track = el("span", "track");
    const fill = el("span", "fill");
    fill.style.width = `${most > 0 ? (person.commits / most) * 100 : 0}%`;
    track.append(fill);
    row.append(track, el("span", "num", String(person.commits)));
    box.append(row);
  }
  if (top.length === 0) box.append(el("p", "sub", "No commits in the last year."));
  return box;
}
