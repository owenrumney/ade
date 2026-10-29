import { page as statsPage, type Stats } from "../pages/statspage";
import { page as graphPage, type GraphRepo } from "../pages/graphpage";
import { page as usagePage, type WindowSpend } from "../pages/windowpage";
import type { Usage } from "../pages/usage";
import { roots, type Entry, type ProjectView } from "../pages/projectrow";

export const REPORTS: Record<string, (got: unknown, also?: unknown) => string> = {
  stats: (got) => statsPage(got as Stats),
  graph: (got) => graphPage(((got as { repos?: GraphRepo[] }).repos ?? [])),
  // The plan windows, and what spent the current one, which is a second read.
  usage: (got, also) => usagePage(got as Usage, (also ?? {}) as WindowSpend, Date.now()),

  // A quick pick in the extension, so it has no page builder. A list until
  // it earns a view of its own, as Machines did.
  drift: (got) => list("Drift", (got as Has<"items">).items, (d) =>
    `**${d.title ?? d.id}** — ${d.note ?? d.state ?? ""}`),
  // What ade keeps about this repository between sessions. The fork
  // puts the same rows in a pick; a document can show the bodies too.
  memory: (got) => memoryPage(got as ProjectView),

  logs: (got) => {
    const lines = (got as { lines?: string[] }).lines ?? [];
    if (lines.length === 0) return "# The log\n\nNothing written yet.";
    return "# The log\n\n```\n" + lines.slice(-400).join("\n") + "\n```";
  },
};

type Row = Record<string, string | boolean | undefined>;
type Has<K extends string> = { [P in K]?: Row[] };

function list(title: string, rows: Row[] | undefined, line: (row: Row) => string): string {
  if (!rows || rows.length === 0) return `# ${title}\n\nNothing here.`;
  return `# ${title}\n\n` + rows.map((r) => `- ${line(r).trim()}`).join("\n") + "\n";
}

// memoryPage draws `/api/project` with the fork's own row helpers, so
// the two cannot come to disagree about what counts as remembered.
function memoryPage(view: ProjectView | undefined): string {
  if (!view) return "# What ade remembers\n\nNothing to read.";
  const out = [`# What ade remembers about ${view.name}`, ""];
  for (const row of roots(view)) {
    if (row.kind === "off") {
      out.push("Nothing is kept between sessions for this repository.", "");
      continue;
    }
    if (row.kind === "instructions") {
      out.push(`Instructions: ${row.length ? `${row.length} characters` : "none yet"}`, "");
      continue;
    }
    if (row.kind === "distil") {
      out.push(`${row.unread} transcripts have not been read yet.`, "");
      continue;
    }
    if (row.kind === "entry") {
      out.push(...entryLines(row.entry));
      continue;
    }
    out.push(`## ${row.title}`, "");
    for (const entry of row.entries) out.push(...entryLines(entry));
  }
  return out.join("\n");
}

function entryLines(entry: Entry): string[] {
  const when = entry.added ? ` · ${entry.added.slice(0, 10)}` : "";
  return [`### ${entry.heading}${when}`, "", entry.body.trim(), ""];
}
