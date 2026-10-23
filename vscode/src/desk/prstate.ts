import type { Review, ReviewState } from "../launcher";

// What a row shows for each state: the word, and the icon beside it.
const WORDS: Record<ReviewState["state"], { say: string; icon: string }> = {
  requested: { say: "", icon: "git-pull-request" },
  running: { say: "reviewing", icon: "loading~spin" },
  ready: { say: "ready to read", icon: "eye" },
  drafted: { say: "", icon: "edit" },
  sent: { say: "sent", icon: "check" },
};

export function icon(r: Review): string {
  return WORDS[r.review?.state ?? "requested"].icon;
}

// says is the state as a row puts it. Drafts are counted, because the number
// is the thing that makes you go back to them.
export function says(r: Review): string {
  const state = r.review?.state ?? "requested";
  if (state !== "drafted") return WORDS[state].say;
  const n = r.review?.unsent ?? 0;
  return n === 1 ? "1 unsent" : `${n} unsent`;
}

// needsYou is a row that belongs in Waiting on you: a review that finished
// while you were elsewhere, and a draft nobody has sent.
export function needsYou(r: Review): boolean {
  const state = r.review?.state;
  return !r.muted && (state === "ready" || state === "drafted");
}

// runnable is a row "Review it" would do something for. A review already
// going, or findings already filed, is not one to start again.
export function runnable(r: Review): boolean {
  const state = r.review?.state ?? "requested";
  return state === "requested";
}
