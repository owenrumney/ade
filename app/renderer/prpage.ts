import type { BranchPR } from "../pages/client";

export type PrActId = "ade" | "web" | "mute" | "unmute" | "approve" | "request-changes" | "merge";

export interface PrAct {
  id: PrActId;
  label: string;
  detail: string;
  primary?: boolean;
  // Why GitHub would refuse it; the button is drawn but cannot be pressed.
  blocked?: string;
}

// prActs is what the page offers: a review request can be taken on or put
// aside, and the session's own pull request can be given a verdict.
export function prActs(at: {
  url: string;
  requested?: { muted?: boolean };
  branch?: BranchPR;
}): PrAct[] {
  const out: PrAct[] = [];
  if (at.requested) {
    out.push({
      id: "ade",
      label: "Open PR with ade",
      detail: "check it out where this machine has the repository, and start on it",
      primary: true,
    });
  }
  if (at.branch) {
    out.push(
      { id: "approve", label: "Approve", detail: "post an approving review as you", blocked: at.branch.approveBlocked || undefined },
      {
        id: "request-changes",
        label: "Request changes",
        detail: "post a review asking for changes, as you",
        blocked: at.branch.requestChangesBlocked || undefined,
      },
      { id: "merge", label: "Merge", detail: "merge it on GitHub", blocked: at.branch.mergeBlocked || undefined },
    );
  }
  if (at.url) out.push({ id: "web", label: "Open in web", detail: at.url });
  if (at.requested) {
    out.push(
      at.requested.muted
        ? { id: "unmute", label: "Bring it back", detail: "list it among the waiting ones again" }
        : { id: "mute", label: "Put it aside", detail: "take it off the list until you bring it back" },
    );
  }
  return out;
}
