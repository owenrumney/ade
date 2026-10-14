import { describe, expect, it } from "vitest";
import type { Review } from "./launcher";
import { acts, asideCount, emptyBecause, rows } from "./pending";

const asked = (number: number, over: Partial<Review> = {}): Review => ({
  repo: "owenrumney/ade",
  number,
  title: `pull request ${number}`,
  author: "someone",
  url: `https://github.com/owenrumney/ade/pull/${number}`,
  updatedAt: `2026-10-0${number}T00:00:00Z`,
  ...over,
});

describe("the pending pull request rows", () => {
  it("lists what is waiting, oldest first", () => {
    const got = rows([asked(3), asked(2), asked(1)]);
    expect(got.map((r) => r.review.number)).toEqual([1, 2, 3]);
  });

  // A picker that mixed the two would make you read every row to find out
  // which is which; the trailing row in the command is the way back to them.
  it("leaves out what was put aside, and counts it separately", () => {
    const all = [asked(1), asked(2, { muted: true })];
    expect(rows(all).map((r) => r.review.number)).toEqual([1]);
    expect(asideCount(all)).toBe(1);
  });

  it("lists only what was put aside when asked for it", () => {
    const all = [asked(1), asked(2, { muted: true })];
    expect(rows(all, true).map((r) => r.review.number)).toEqual([2]);
  });

  it("says how long each has been waiting", () => {
    const now = Date.parse("2026-10-06T00:00:00Z");
    const got = rows(
      [asked(1, { updatedAt: "2026-07-01T00:00:00Z" })],
      false,
      now,
    );
    expect(got[0].description).toContain("3 months ago");
  });

  // The picker matches on the description, so the repository and the author
  // have to be in it: the list crosses every repository you watch.
  it("says which repository and who asked", () => {
    expect(rows([asked(1)])[0].description).toContain("owenrumney/ade");
    expect(rows([asked(1)])[0].description).toContain("@someone");
  });

  it("titles the row with the pull request's title", () => {
    expect(rows([asked(1)])[0].label).toBe("pull request 1");
  });
});

describe("an empty list says which kind of empty", () => {
  it("is plain when nothing was hidden and nothing failed", () => {
    expect(emptyBecause({})).toBe("nothing is waiting on your review.");
  });

  // Five real team requests read as none, and the answer was "nothing waiting".
  it("names the ignore rules when they took the lot", () => {
    const said = emptyBecause({ hidden: 5 });
    expect(said).toContain("5 requests");
    expect(said).toContain("settings.json");
    expect(emptyBecause({ hidden: 1 })).toContain("one request");
  });

  it("says a failed search is not an empty one", () => {
    expect(emptyBecause({ error: "gh: not logged in", hidden: 0 })).toContain(
      "GitHub did not answer",
    );
  });
});

describe("what the second pick offers", () => {
  it("opens it in ade or in a browser, whatever its state", () => {
    for (const review of [asked(1), asked(1, { muted: true })])
      expect(acts(review).map((a) => a.id)).toContain("ade");
    expect(acts(asked(1)).map((a) => a.id)).toContain("web");
  });

  // Only the one that would change anything: putting aside what is already
  // aside is a no-op the list would still have to draw.
  it("offers putting aside on a live request and bringing back on an aside one", () => {
    expect(acts(asked(1)).map((a) => a.id)).toEqual(["ade", "web", "mute"]);
    expect(acts(asked(1, { muted: true })).map((a) => a.id)).toEqual([
      "ade",
      "web",
      "unmute",
    ]);
  });

  it("puts the web link where you can read it before opening", () => {
    const web = acts(asked(7)).find((a) => a.id === "web");
    expect(web?.detail).toBe("https://github.com/owenrumney/ade/pull/7");
  });
});
