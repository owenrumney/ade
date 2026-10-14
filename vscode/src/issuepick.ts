import type { Issue } from "./client";
import { issueLine, rankIssue } from "./issues";

// Row is one Linear ticket as the picker draws it. The identifier leads the
// description because it is what you type to find one.
export interface Row {
  issue: Issue;
  label: string;
  description: string;
  detail: string;
}

export function rows(all: Issue[]): Row[] {
  return [...all].sort(rankIssue).map((issue) => ({
    issue,
    label: issue.title,
    description: [issue.identifier, issueLine(issue)]
      .filter(Boolean)
      .join(" · "),
    detail: "",
  }));
}

export interface Act {
  id: "start" | "linear";
  label: string;
  detail: string;
}

// Starting one already in progress is picking it back up, which is a different
// sentence for the same call: the server decides whether it branches afresh.
export function acts(issue: Issue): Act[] {
  return [
    {
      id: "start",
      label: issue.started ? "Pick it back up" : "Start here",
      detail: issue.branchName
        ? `on ${issue.branchName}`
        : "in a worktree of this repository",
    },
    { id: "linear", label: "Open in Linear", detail: issue.url },
  ];
}
