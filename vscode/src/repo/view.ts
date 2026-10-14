// What the repository page draws, worked out from the launcher's answer. The
// DOM is main.ts; everything here is a value, so it can be tested.
// Design: .git/ade/docs/repo-stats-webview.md.

export interface Language {
  name: string;
  lines: number;
  files: number;
}

export interface Day {
  date: string;
  commits: number;
}

export interface Person {
  name: string;
  email?: string;
  commits: number;
}

export interface Commit {
  sha: string;
  subject: string;
  author: string;
  at: string;
}

export interface Group {
  key: string;
  cost: number;
  tokens: number;
  sessions: number;
  requests?: number;
}

export interface Agent {
  cost: number;
  turns: number;
  sessions: number;
  tokens: {
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
    reasoning: number;
  };
  byDay: Group[];
  tools: Group[];
  models: Group[];
}

export interface Repo {
  repo: string;
  dir: string;
  branch: string;
  remote?: string;
  upstream?: string;
  ahead: number;
  behind: number;
  dirty: number;
  languages: Language[];
  lines: number;
  files: number;
  days: Day[];
  commits: number;
  busiestDay?: string;
  contributors: Person[];
  last?: Commit;
  agent: Agent;
}

// topLanguages keeps the largest few and folds the rest into Other, so the bar
// reads as a shape rather than a legend of one-file extensions.
export function topLanguages(languages: Language[], keep = 5): Language[] {
  const sorted = [...languages].sort((a, b) => b.lines - a.lines);
  if (sorted.length <= keep) return sorted;
  const rest = sorted.slice(keep);
  const other: Language = {
    name: "Other",
    lines: rest.reduce((n, l) => n + l.lines, 0),
    files: rest.reduce((n, l) => n + l.files, 0),
  };
  return [...sorted.slice(0, keep), other];
}

// percent is a language's share, to one place, and never 0% for a language
// that has any lines at all.
export function percent(lines: number, total: number): string {
  if (total <= 0) return "0%";
  const share = (lines / total) * 100;
  if (share > 0 && share < 1) return "<1%";
  return `${Math.round(share)}%`;
}

// weeks lays the year out as the calendar does: a column per week, Sunday at
// the top, the last column being the week we are in. The leading blanks keep
// the first column aligned to its weekday.
export function weeks(days: Day[]): (Day | null)[][] {
  if (days.length === 0) return [];
  const out: (Day | null)[][] = [];
  let column: (Day | null)[] = [];
  const first = new Date(`${days[0].date}T00:00:00Z`).getUTCDay();
  for (let i = 0; i < first; i++) column.push(null);
  for (const day of days) {
    column.push(day);
    if (column.length === 7) {
      out.push(column);
      column = [];
    }
  }
  if (column.length > 0) {
    while (column.length < 7) column.push(null);
    out.push(column);
  }
  return out;
}

// heat is the shade for one day's commits, 0 to 4, scaled against the busiest
// day so a quiet repository is not one flat colour.
export function heat(commits: number, busiest: number): number {
  if (commits <= 0) return 0;
  if (busiest <= 1) return 4;
  return Math.max(1, Math.min(4, Math.ceil((commits / busiest) * 4)));
}

// months names the columns, each month once, at the week it starts in.
export function months(cols: (Day | null)[][]): (string | null)[] {
  let last = "";
  return cols.map((column) => {
    const day = column.find((d) => d !== null);
    if (!day) return null;
    const name = new Date(`${day.date}T00:00:00Z`).toLocaleString("en", {
      month: "short",
      timeZone: "UTC",
    });
    if (name === last) return null;
    last = name;
    return name;
  });
}

// against says where the branch stands with its upstream, or that it has none.
export function against(repo: Repo): string {
  if (!repo.upstream) return `${repo.branch} · no upstream`;
  if (repo.ahead === 0 && repo.behind === 0) {
    return `${repo.branch} matches ${repo.upstream}`;
  }
  const parts: string[] = [];
  if (repo.ahead > 0) parts.push(`${repo.ahead} ahead`);
  if (repo.behind > 0) parts.push(`${repo.behind} behind`);
  return `${repo.branch} ${parts.join(", ")} of ${repo.upstream}`;
}

// count abbreviates a token count the way the dashboards do.
export function count(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)} M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)} K`;
  return String(n);
}

export function money(n: number): string {
  if (n >= 100) return `$${Math.round(n)}`;
  return `$${n.toFixed(2)}`;
}

// kinds is the token split, largest first, dropping what is not there.
export function kinds(agent: Agent): { name: string; value: number }[] {
  const all = [
    { name: "cache read", value: agent.tokens.cacheRead },
    { name: "input", value: agent.tokens.input },
    { name: "output", value: agent.tokens.output },
    { name: "cache write", value: agent.tokens.cacheWrite },
    { name: "reasoning", value: agent.tokens.reasoning },
  ];
  return all.filter((k) => k.value > 0).sort((a, b) => b.value - a.value);
}

// spark scales the daily costs to a 0-1 height each, against the busiest day.
export function spark(byDay: Group[]): { key: string; cost: number; height: number }[] {
  const top = byDay.reduce((n, d) => Math.max(n, d.cost), 0);
  return byDay.map((day) => ({
    key: day.key,
    cost: day.cost,
    height: top > 0 ? day.cost / top : 0,
  }));
}

// ago is how long since a commit, in the units a person would say.
export function ago(iso: string, now = new Date()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const mins = Math.max(0, Math.round((now.getTime() - then) / 60000));
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 60) return `${days}d ago`;
  return `${Math.round(days / 30)}mo ago`;
}
