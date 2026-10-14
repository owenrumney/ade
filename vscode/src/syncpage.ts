// SyncReport mirrors worktrees.Report: one sync of a repository's checkouts
// onto a freshly fetched base, or what one would do.
export interface SyncReport {
  base: string;
  fetched: number;
  note?: string;
  results: SyncResult[] | null;
}

export interface SyncResult {
  path: string;
  name: string;
  branch?: string;
  main?: boolean;
  outcome: string;
  detail?: string;
}

// lines is a report as the confirmation lists it: a line per checkout, with
// what would happen to it.
export function lines(report: SyncReport): string {
  const head = `${report.base} moved ${report.fetched} commit${report.fetched === 1 ? "" : "s"}.`;
  const rows = (report.results ?? []).map(
    (r) => `${r.name}: ${r.outcome}${r.detail ? ` (${r.detail})` : ""}`,
  );
  return [head, ...(report.note ? [report.note] : []), "", ...rows].join("\n");
}

// page is the same report as a document, for after it has run.
export function page(repo: string, report: SyncReport): string {
  const out = [
    `# Synced ${repo}`,
    "",
    `Against \`${report.base}\`, which moved ${report.fetched} commit${report.fetched === 1 ? "" : "s"}.`,
    "",
  ];
  if (report.note) out.push(report.note, "");
  out.push("| Checkout | Branch | What happened |", "| --- | --- | --- |");
  for (const r of report.results ?? [])
    out.push(
      `| ${r.name}${r.main ? " (main)" : ""} | ${r.branch ?? ""} | ${r.outcome}${r.detail ? ` — ${r.detail}` : ""} |`,
    );
  return out.join("\n") + "\n";
}
