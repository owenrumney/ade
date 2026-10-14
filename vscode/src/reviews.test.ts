import { describe, expect, it } from "vitest";
import { muted, rankReview, reviewLine, waiting } from "./reviews";
import type { Review } from "./launcher";

const one = (over: Partial<Review>): Review => ({
  repo: "owenrumney/ade",
  number: 1,
  title: "a change",
  author: "someone",
  url: "https://example.invalid",
  ...over,
});

describe("review requests", () => {
  it("names the repository first, because the list crosses them", () => {
    expect(reviewLine(one({ checks: "fail" }))).toBe(
      "owenrumney/ade · @someone · checks fail",
    );
  });

  // How long it has been sitting there is the thing the old list could not
  // say: five requests from the spring read exactly like five from today.
  it("says how long it has been waiting", () => {
    const now = Date.parse("2026-10-06T00:00:00Z");
    expect(reviewLine(one({ updatedAt: "2026-07-01T00:00:00Z" }), now)).toBe(
      "owenrumney/ade · @someone · 3 months ago",
    );
  });

  it("puts the longest wait at the top", () => {
    const old = one({ number: 1, updatedAt: "2026-01-01" });
    const now = one({ number: 2, updatedAt: "2026-09-01" });
    expect([now, old].sort(rankReview).map((r) => r.number)).toEqual([1, 2]);
  });

  it("splits what is waiting from what was put aside", () => {
    const all = [one({ number: 1 }), one({ number: 2, muted: true })];
    expect(waiting(all).map((r) => r.number)).toEqual([1]);
    expect(muted(all).map((r) => r.number)).toEqual([2]);
  });
});
