// ProjectView mirrors web.ProjectView: what ade remembers about a repository
// across sessions, and what it has been told to do in every one.
export interface ProjectView {
  name: string;
  remembered: boolean;
  instructions: string;
  maxLength: number;
  entries: Entry[] | null;
  proposed: Entry[] | null;
  unread: number;
}

export interface Entry {
  id: string;
  heading: string;
  body: string;
  state?: string;
  session?: string;
  added?: string;
  replacesHeading?: string;
}

export type Row =
  | { kind: "instructions"; length: number }
  | { kind: "off" }
  | { kind: "distil"; unread: number }
  | { kind: "group"; title: string; entries: Entry[]; proposed: boolean }
  | { kind: "entry"; entry: Entry; proposed: boolean };

// roots is the top of the tree. A superseded entry is history, not memory, so
// it is left out; an empty group is left out rather than drawn as a zero.
export function roots(view: ProjectView | undefined): Row[] {
  if (!view) return [];
  const out: Row[] = [
    { kind: "instructions", length: view.instructions.length },
  ];
  if (!view.remembered) return [...out, { kind: "off" }];

  if (view.unread > 0) out.push({ kind: "distil", unread: view.unread });
  const proposed = view.proposed ?? [];
  const kept = (view.entries ?? []).filter((e) => e.state !== "superseded");
  if (proposed.length)
    out.push({
      kind: "group",
      title: "Proposed",
      entries: proposed,
      proposed: true,
    });
  if (kept.length)
    out.push({
      kind: "group",
      title: "Remembered",
      entries: kept,
      proposed: false,
    });
  return out;
}

export function children(row: Row): Row[] {
  if (row.kind !== "group") return [];
  return row.entries.map((entry) => ({
    kind: "entry",
    entry,
    proposed: row.proposed,
  }));
}

// context is what the row's menu matches on.
export function context(row: Row): string {
  if (row.kind !== "entry") return `ade.memory.${row.kind}`;
  return row.proposed ? "ade.memory.proposed" : "ade.memory.kept";
}

// page is an entry as the Markdown the editor opens: the heading is the first
// line, so saving the page can say what both are now.
export function page(entry: Entry): string {
  return `# ${entry.heading}\n\n${entry.body}\n`;
}

// parse is the reverse. A page with no heading line is refused by returning
// nothing, rather than saving the first sentence as one.
export function parse(
  text: string,
): { heading: string; body: string } | undefined {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const first = lines.shift() ?? "";
  if (!first.startsWith("# ")) return undefined;
  const heading = first.slice(2).trim();
  if (!heading) return undefined;
  return { heading, body: lines.join("\n").trim() };
}

// flat is the tree as one list, for a pick: the groups become nothing and
// their entries carry whether they are proposals.
export function flat(view: ProjectView | undefined): Row[] {
  const out: Row[] = [];
  for (const row of roots(view)) {
    if (row.kind !== "group") {
      out.push(row);
      continue;
    }
    out.push(...children(row));
  }
  return out;
}

// label is a row's words in a pick, where there is no indentation to lean on.
export function label(row: Row): string {
  switch (row.kind) {
    case "instructions":
      return "Instructions";
    case "off":
      return "Start remembering this project";
    case "distil":
      return "Read the new transcripts";
    case "group":
      return row.title;
    case "entry":
      return row.entry.heading;
  }
}

// line is what the row says beside its label.
export function line(row: Row): string {
  switch (row.kind) {
    case "instructions":
      return row.length ? `${row.length} characters` : "none yet";
    case "off":
      return "nothing is kept between sessions";
    case "distil":
      return `${row.unread} unread`;
    case "group":
      return String(row.entries.length);
    case "entry":
      return row.proposed ? "proposed" : "remembered";
  }
}
