import type { Review, Trouble } from "./launcher";
import { muted, reviewLine, waiting } from "./reviews";

// Saying "nothing is waiting" when your own rules hid five reads as a bug.
export function emptyBecause(trouble: Trouble): string {
  if (trouble.error) return "GitHub did not answer, so nothing can be listed.";
  const hidden = trouble.hidden ?? 0;
  if (hidden === 0) return "nothing is waiting on your review.";
  const them = hidden === 1 ? "one request" : `${hidden} requests`;
  return `nothing is waiting on your review — your ignore rules hid ${them}. They are the "ignore" block in ~/.ade/settings.json.`;
}

// Row is one pull request as the picker draws it.
export interface Row {
  review: Review;
  label: string;
  description: string;
  detail: string;
}

// What you put aside is not in the list: a picker that mixed the two would make
// you read every row to find out which is which.
export function rows(all: Review[], aside = false, now = Date.now()): Row[] {
  return (aside ? muted(all) : waiting(all)).map((review) => ({
    review,
    label: review.title,
    description: reviewLine(review, now),
    detail: "",
  }));
}

export function asideCount(all: Review[]): number {
  return muted(all).length;
}

// Act is what the second pick offers for the request you chose.
export interface Act {
  id: "ade" | "web" | "mute" | "unmute";
  label: string;
  detail: string;
}

export function acts(review: Review): Act[] {
  return [
    {
      id: "ade",
      label: "Open PR with ade",
      detail:
        "check it out where this machine has the repository, and start on it",
    },
    { id: "web", label: "Open in web", detail: review.url },
    review.muted
      ? {
          id: "unmute",
          label: "Bring it back",
          detail: "list it among the waiting ones again",
        }
      : {
          id: "mute",
          label: "Put it aside",
          detail: "take it off this list until you bring it back",
        },
  ];
}
