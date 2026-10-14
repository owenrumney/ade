import { describe, expect, it } from "vitest";
import {
  LEVELS,
  SCOPES,
  empty,
  levelled,
  nothing,
  preselect,
  reviewers,
  scopeable,
  what,
} from "./review";

describe("what", () => {
  it("names the branch, the count and the scope", () => {
    expect(what({ branch: "fix-thing", scope: "branch", files: 14 })).toBe(
      "14 files on fix-thing, this branch",
    );
  });

  it("says which half when the scope is not the whole branch", () => {
    expect(what({ branch: "b", scope: "uncommitted", files: 2 })).toContain(
      "uncommitted only",
    );
  });

  it("is singular for one file and plural for none", () => {
    expect(what({ branch: "b", scope: "branch", files: 1 })).toBe(
      "1 file on b, this branch",
    );
    expect(what({ branch: "b", scope: "branch", files: 0 })).toBe(
      "0 files on b, this branch",
    );
  });

  it("copes with a detached head, which has no branch", () => {
    expect(what({ scope: "branch", files: 3 })).toBe("3 files, this branch");
  });

  it("assumes the branch when the server named no scope", () => {
    expect(what({ branch: "b", files: 1 })).toContain("this branch");
  });

  // A scope nobody has named yet is shown as it came rather than dropped.
  it("passes an unknown scope through", () => {
    expect(what({ branch: "b", scope: "staged", files: 1 })).toContain(
      "staged",
    );
  });
});

describe("empty", () => {
  it("is true for a diff with nothing in it", () => {
    expect(empty({ files: 0 })).toBe(true);
    expect(empty(undefined)).toBe(true);
  });

  it("is false once there is something to read", () => {
    expect(empty({ files: 1 })).toBe(false);
  });
});

describe("nothing to review", () => {
  // One window can see several ade servers and attaches to the first folder
  // with one, so the message has to say which checkout it is talking about.
  it("names the checkout and the view it found nothing in", () => {
    const said = nothing({
      repo: "/Users/owen/code/infracost/ci-fix-739",
      branch: "fix-739",
      scope: "branch",
      files: 0,
    });
    expect(said).toContain("ci-fix-739");
    expect(said).toContain("fix-739");
    expect(said).toContain("this branch");
  });

  it("drops the checkout rather than saying nothing, when there is no repo", () => {
    expect(nothing(undefined)).toBe(
      "ade: nothing to review — 0 files, this branch.",
    );
  });
});

describe("scopeable", () => {
  it("is false for a fetched snapshot, which has only one view", () => {
    expect(scopeable({ mode: "pr snapshot" })).toBe(false);
  });

  it("is true for an ordinary session", () => {
    expect(scopeable({ mode: "worktree" })).toBe(true);
    expect(scopeable(undefined)).toBe(true);
  });
});

describe("the lists match the server", () => {
  it("offers the three scopes gitdiff.Scopes names, in that order", () => {
    expect(SCOPES.map((s) => s.id)).toEqual([
      "branch",
      "committed",
      "uncommitted",
    ]);
  });

  it("offers exactly the levels internal/review accepts", () => {
    expect([...LEVELS.map((l) => l.id)].sort()).toEqual(
      ["high", "low", "max", "medium", "xhigh"].sort(),
    );
  });
});

describe("reviewers", () => {
  const a = (
    name: string,
    over: Partial<{ reviews: boolean; scope: string }> = {},
  ) => ({
    name,
    reviews: true,
    scope: "user",
    ...over,
  });

  it("keeps a user agent that reviews", () => {
    expect(reviewers([a("security")]).map((x) => x.name)).toEqual(["security"]);
  });

  // Its definition says it answers rather than reviews.
  it("drops one with reviews: false", () => {
    expect(reviewers([a("helper", { reviews: false })])).toEqual([]);
  });

  // A branch under review could otherwise ship the reviewer that reviews it.
  it("drops a repository agent, which the server refuses", () => {
    expect(reviewers([a("security", { scope: "repo" })])).toEqual([]);
  });

  it("keeps the user one when a repository shadows the name", () => {
    const got = reviewers([a("security", { scope: "repo" }), a("security")]);
    expect(got).toHaveLength(1);
    expect(got[0].scope).toBe("user");
  });
});

describe("levelled", () => {
  it("asks for a level only for the cold reviewer", () => {
    expect(levelled([])).toBe(true);
  });

  // handleReview reads `level` only when no agent is named.
  it("does not ask when agents are named, since the level is ignored", () => {
    expect(levelled(["security"])).toBe(false);
    expect(levelled(["security", "architect"])).toBe(false);
  });
});

describe("preselect", () => {
  const offered = [
    { name: "security" },
    { name: "staff-reviewer" },
    { name: "tester" },
  ];

  it("ticks the ones the setting names", () => {
    expect(
      [...preselect(offered, ["security", "staff-reviewer"])].sort(),
    ).toEqual(["security", "staff-reviewer"]);
  });

  it("tolerates the @ people will type", () => {
    expect([...preselect(offered, ["@security"])]).toEqual(["security"]);
  });

  // The setting is a preference, not an assertion about this machine.
  it("ignores a name this checkout cannot offer", () => {
    expect([...preselect(offered, ["security", "nobody"])]).toEqual([
      "security",
    ]);
  });

  it("ticks nothing for an empty or blank setting", () => {
    expect(preselect(offered, []).size).toBe(0);
    expect(preselect(offered, ["", "  "]).size).toBe(0);
  });
});
