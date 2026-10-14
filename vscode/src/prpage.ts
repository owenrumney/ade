import type { BranchPR, Check } from "./client";

// page is the pull request as a document. Its comments are threads and live in
// the Comments view, so they are not repeated here.
export function page(pr: BranchPR): string {
  const out = [
    `# #${pr.number} ${pr.title}`,
    "",
    [
      pr.draft ? "draft" : pr.state.toLowerCase(),
      pr.author ? `by ${pr.author}` : "",
      pr.headRef && pr.baseRef ? `${pr.headRef} → ${pr.baseRef}` : "",
    ]
      .filter(Boolean)
      .join(" · "),
    "",
    pr.url,
    "",
  ];

  const body = (pr.body ?? "").trim();
  out.push("## Description", "", body || "_Nothing written yet._", "");

  const reviews = pr.reviews ?? [];
  if (reviews.length) {
    out.push("## Reviews", "");
    for (const review of reviews)
      out.push(`- **${review.author}** ${verdict(review.state)}`);
    out.push("");
  }
  if (pr.reviewDecision)
    out.push(`Decision: ${verdict(pr.reviewDecision)}.`, "");

  const checks = pr.checks ?? [];
  if (checks.length) {
    out.push("## Checks", "", "| Check | State |", "| --- | --- |");
    for (const check of sorted(checks))
      out.push(`| ${check.name} | ${check.bucket} |`);
    out.push("");
  }

  for (const [why, what] of [
    [pr.mergeBlocked, "Merging is blocked"],
    [pr.approveBlocked, "Approving is blocked"],
    [pr.requestChangesBlocked, "Requesting changes is blocked"],
  ] as [string | undefined, string][])
    if (why) out.push(`${what}: ${why}.`, "");

  return out.join("\n");
}

// sorted puts what needs looking at first: failing, then pending, then the
// rest. A passing check is the one you do not read.
const ORDER = ["fail", "cancel", "pending", "skipping", "pass"];

function sorted(checks: Check[]): Check[] {
  return [...checks].sort(
    (a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name),
  );
}

function rank(check: Check): number {
  const at = ORDER.indexOf(check.bucket);
  return at < 0 ? ORDER.length : at;
}

// verdict is GitHub's word in ours.
function verdict(state: string): string {
  return (
    {
      APPROVED: "approved",
      CHANGES_REQUESTED: "asked for changes",
      COMMENTED: "commented",
      REVIEW_REQUIRED: "a review is required",
      DISMISSED: "dismissed",
    }[state] ?? state.toLowerCase().replace(/_/g, " ")
  );
}
