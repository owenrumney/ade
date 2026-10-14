import type { BranchPR, Check, Issue, Listed } from "./client";

// line is what an issue row says beside its id: where it stands, and nothing
// else. The title is the label and the rest is noise in a narrow column.
export function issueLine(issue: Issue): string {
  return issue.state || (issue.started ? "in progress" : "");
}

export function issueIcon(issue: Issue): string {
  return issue.started ? "debug-start" : "circle-outline";
}

// Urgent first, then started, then whatever Linear touched last. Priority 0
// is "no priority" in Linear, not the top one, which is 1.
export function rankIssue(a: Issue, b: Issue): number {
  const p = (i: Issue) => (i.priority && i.priority > 0 ? i.priority : 99);
  if (p(a) !== p(b)) return p(a) - p(b);
  if (Boolean(a.started) !== Boolean(b.started)) return a.started ? -1 : 1;
  return (b.updatedAt ?? "").localeCompare(a.updatedAt ?? "");
}

// prLine is where a pull request stands: the branch, the checks, and the
// review decision as a word rather than GitHub's shouting.
export function prLine(pr: Listed): string {
  const bits = [pr.headRef];
  if (pr.draft) bits.push("draft");
  if (pr.checks) bits.push(`checks ${pr.checks}`);
  const decision = DECISIONS[pr.reviewDecision ?? ""];
  if (decision) bits.push(decision);
  return bits.filter(Boolean).join(" · ");
}

const DECISIONS: Record<string, string> = {
  APPROVED: "approved",
  CHANGES_REQUESTED: "changes requested",
  REVIEW_REQUIRED: "needs review",
};

export function prIcon(pr: Listed): string {
  if (pr.reviewRequested) return "bell";
  if (pr.checks === "fail") return "error";
  if (pr.draft) return "git-pull-request-draft";
  return "git-pull-request";
}

// Waiting on you first, then yours, then the rest by recency: the list is
// read to find what needs doing, not to enumerate the repository.
export function rankPR(a: Listed, b: Listed): number {
  if (Boolean(a.reviewRequested) !== Boolean(b.reviewRequested))
    return a.reviewRequested ? -1 : 1;
  if (Boolean(a.mine) !== Boolean(b.mine)) return a.mine ? -1 : 1;
  return (b.updatedAt ?? "").localeCompare(a.updatedAt ?? "");
}

// A check's bucket is ade's word for where a run stands. The icons say the
// same thing the PR view said, in the space a tree row has.
const CHECKS: Record<string, string> = {
  fail: "error",
  pending: "sync~spin",
  cancel: "circle-slash",
  skipping: "debug-step-over",
  pass: "pass-filled",
};

export function checkIcon(check: Check): string {
  return CHECKS[check.bucket] ?? "circle-outline";
}

// What a check row says beside its name: the workflow it belongs to, and
// whatever GitHub said about it. The state repeats the icon, so it is out.
export function checkLine(check: Check): string {
  return [check.workflow, check.description].filter(Boolean).join(" · ");
}

export function failing(checks: Check[] | null | undefined): Check[] {
  return (checks ?? []).filter((c) => c.bucket === "fail");
}

// Failing first, then the ones still running: a list of checks is read to
// find what is wrong, and a green one is never the answer.
export function rankCheck(a: Check, b: Check): number {
  const order = ["fail", "pending", "cancel", "skipping"];
  const at = (c: Check) => {
    const i = order.indexOf(c.bucket);
    return i < 0 ? order.length : i;
  };
  if (at(a) !== at(b)) return at(a) - at(b);
  return a.name.localeCompare(b.name);
}

// The branch's own pull request, as a row. Its checks are what the PR view
// was for, so the row says how they stand before it is expanded.
export function branchLine(pr: BranchPR): string {
  const bits: string[] = [];
  const bad = failing(pr.checks).length;
  if (bad) bits.push(`${bad} failing`);
  else if ((pr.checks ?? []).length) bits.push("checks pass");
  if (pr.draft) bits.push("draft");
  if (pr.mergeBlocked) bits.push(pr.mergeBlocked);
  return bits.join(" · ");
}
