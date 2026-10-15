import { describe, expect, it } from "vitest";
import { GROUPS, ROW_ONLY } from "./menu";
import type { BusOnline } from "./client";
import { ANYONE, askedLine, describe as describeRow, EVERYONE, OTHER, reviewers, reviewTarget, targetItems } from "./busreview";

function row(over: Partial<BusOnline> = {}): BusOnline {
  return {
    user: "liam@infracost.io",
    node: "liams-mbp",
    instance: { id: "liam@infracost.io/crit-1", harness: "crit" },
    accepts: { ask: true, review: { repos: null } },
    presence: "active",
    connected: true,
    ...over,
  };
}

describe("who to offer", () => {
  it("leaves you off your own list", () => {
    const got = reviewers([row(), row({ user: "owen@infracost.io" })], "owen@infracost.io");
    expect(got.map((r) => r.user)).toEqual(["liam@infracost.io"]);
  });

  it("leaves off anyone who does not take reviews", () => {
    const got = reviewers([row({ user: "sam@infracost.io", accepts: { ask: true } })], "owen@x");
    expect(got).toEqual([]);
  });

  it("shows a person once, at their most available instance", () => {
    const got = reviewers(
      [
        row({ presence: "dnd", instance: { id: "liam@infracost.io/crit-2", harness: "crit" } }),
        row({ presence: "active" }),
      ],
      "owen@x",
    );
    expect(got).toHaveLength(1);
    expect(got[0]?.presence).toBe("active");
  });

  it("copes with a bus that answered nothing", () => {
    expect(reviewers(null, "owen@x")).toEqual([]);
    expect(reviewers(undefined, "owen@x")).toEqual([]);
  });
});

describe("the picker", () => {
  it("offers anyone, the people, and a way to type a group", () => {
    const items = targetItems(reviewers([row()], "owen@x"));
    const labels = items.map((i) => i.label).filter(Boolean);
    expect(labels[0]).toBe(ANYONE);
    expect(labels).toContain("liam@infracost.io");
    expect(labels).toContain(EVERYONE);
    expect(labels[labels.length - 1]).toBe(OTHER);
  });

  it("still offers a target when nobody is online", () => {
    const labels = targetItems([]).map((i) => i.label).filter(Boolean);
    expect(labels).toEqual([ANYONE, EVERYONE, OTHER]);
  });

  it("sends the short form the server parses", () => {
    const items = targetItems(reviewers([row()], "owen@x"));
    expect(items.find((i) => i.label === ANYONE)?.to).toBe("anyone");
    expect(items.find((i) => i.label === EVERYONE)?.to).toBe("*");
    expect(items.find((i) => i.label === "liam@infracost.io")?.to).toBe("liam@infracost.io");
    expect(items.find((i) => i.label === OTHER)?.to).toBeUndefined();
  });

  it("says where a person is and what they are doing", () => {
    expect(describeRow(row({ status: "deep in FIX-702" }))).toBe("active · crit · deep in FIX-702");
    expect(describeRow(row({ instance: { id: "x", harness: "ade", label: "infra on main" } })))
      .toBe("active · ade · infra on main");
  });
});

// A tree row is one way in; the branch you are on is the other, and the one
// you have when no pull request is on screen.
describe("asking the team to review this branch", () => {
  const branch = async () => ({ url: "https://github.com/o/r/pull/500" });

  it("asks about the row handed in", async () => {
    expect(await reviewTarget({ url: "https://github.com/o/r/pull/412" }, branch)).toBe(
      "https://github.com/o/r/pull/412",
    );
  });

  it("falls back to this branch's pull request when no row was passed", async () => {
    expect(await reviewTarget(undefined, branch)).toBe("https://github.com/o/r/pull/500");
    expect(await reviewTarget(undefined, async () => undefined)).toBe("");
  });

  it("is offered from the palette, under Team", () => {
    expect(GROUPS.find((g) => g.title === "Team")?.commands).toContain(
      "ade.askTeamForReview",
    );
    expect(ROW_ONLY).not.toContain("ade.askTeamForReview");
  });
});

describe("askedLine", () => {
  it("leads with the ask and adds the reviewer", () => {
    expect(
      askedLine({ to: "liam@infracost.io", reviewerSet: true, reviewer: "liamg" }),
    ).toBe("asked liam@infracost.io to review · liamg is on the pull request");
  });

  // A group reached the people it meant; there was nobody single to name.
  it("says why nobody was named", () => {
    expect(
      askedLine({ to: "anyone", reviewerNote: "anyone is not one person, so no reviewer was set" }),
    ).toBe("asked anyone to review · anyone is not one person, so no reviewer was set");
  });

  it("is just the ask when there is nothing to add", () => {
    expect(askedLine({ to: "liam@infracost.io" })).toBe(
      "asked liam@infracost.io to review",
    );
  });
});
