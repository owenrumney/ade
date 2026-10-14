// Stats mirrors transcripts.Stats: what the local transcripts say was spent.
// Nothing here asks a provider.
export interface Stats {
  sessions: number;
  requests: number;
  tools: number;
  cost: number;
  usage?: {
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
  };
  byDay?: Group[] | null;
  byAgent?: Group[] | null;
  byModel?: Group[] | null;
  byTool?: Group[] | null;
}

export interface Group {
  key: string;
  cost: number;
  tokens: number;
  sessions: number;
  unpriced?: number;
  requests?: number;
}

const DAYS = 14;

// page is the stats as a document: numbers in tables, which is all the dials
// on the old page were saying.
export function page(stats: Stats): string {
  const out = [
    "# What ade has spent",
    "",
    "Read from the transcripts on this machine. Nothing here asks a provider.",
    "",
    `- **${money(stats.cost)}** across ${count(stats.sessions, "session")}`,
    `- ${count(stats.requests, "request")}, ${count(stats.tools, "tool call")}`,
  ];
  if (stats.usage)
    out.push(
      `- ${tokens(stats.usage.input)} in, ${tokens(stats.usage.output)} out, ${tokens(stats.usage.cacheRead)} read from cache`,
    );
  table(out, `The last ${DAYS} days`, "Day", lastDays(stats.byDay ?? []));
  table(out, "By agent", "Agent", stats.byAgent ?? []);
  table(out, "By model", "Model", stats.byModel ?? []);
  tools(out, stats.byTool ?? []);
  return out.join("\n") + "\n";
}

// lastDays is the newest days, newest first, whatever order they arrived in.
function lastDays(groups: Group[]): Group[] {
  return [...groups].sort((a, b) => b.key.localeCompare(a.key)).slice(0, DAYS);
}

function table(
  out: string[],
  title: string,
  head: string,
  groups: Group[],
): void {
  if (groups.length === 0) return;
  out.push(
    "",
    `## ${title}`,
    "",
    `| ${head} | Cost | Tokens | Sessions |`,
    "| --- | ---: | ---: | ---: |",
  );
  for (const group of groups)
    out.push(
      `| ${group.key || "unknown"} | ${money(group.cost)}${group.unpriced ? " \\*" : ""} | ${tokens(group.tokens)} | ${group.sessions} |`,
    );
  if (groups.some((group) => group.unpriced))
    out.push("", "\\* some requests had no price, so the cost is a floor.");
}

function tools(out: string[], groups: Group[]): void {
  if (groups.length === 0) return;
  out.push("", "## By tool", "", "| Tool | Calls |", "| --- | ---: |");
  for (const group of [...groups].sort(
    (a, b) => (b.requests ?? 0) - (a.requests ?? 0),
  ))
    out.push(`| ${group.key} | ${group.requests ?? 0} |`);
}

export function money(cost: number): string {
  return `$${cost.toFixed(2)}`;
}

// tokens rounds to what is worth reading: 1.2M, 340k, 812.
export function tokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
}

function count(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}
