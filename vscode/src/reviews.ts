import type { Review } from "./launcher";
import { age } from "./tree";

// Beside the title: repository, author, age, checks. The repo leads because the
// list crosses them, and the age is what tells a live request from a stale one.
export function reviewLine(r: Review, now = Date.now()): string {
  const bits = [r.repo, r.author && `@${r.author}`, age(r.updatedAt, now)];
  if (r.draft) bits.push("draft");
  if (r.checks) bits.push(`checks ${r.checks}`);
  return bits.filter(Boolean).join(" · ");
}

// Oldest first: a request that has been waiting three days is the one being
// waited on, and recency ordering buries it.
export function rankReview(a: Review, b: Review): number {
  return (a.updatedAt ?? "").localeCompare(b.updatedAt ?? "");
}

export function waiting(all: Review[]): Review[] {
  return all.filter((r) => !r.muted).sort(rankReview);
}

export function muted(all: Review[]): Review[] {
  return all.filter((r) => r.muted).sort(rankReview);
}
