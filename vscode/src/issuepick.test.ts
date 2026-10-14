import { describe, expect, it } from "vitest";
import type { Issue } from "./client";
import { acts, rows } from "./issuepick";

const issue = (over: Partial<Issue> = {}): Issue => ({
  identifier: "ADE-1",
  title: "a ticket",
  url: "https://linear.app/ade/issue/ADE-1",
  state: "Todo",
  ...over,
});

describe("the issue rows", () => {
  // Priority 0 is "no priority" in Linear, not the top one, which is 1.
  it("puts the urgent one first, then what is started", () => {
    const got = rows([
      issue({ identifier: "none", priority: 0 }),
      issue({ identifier: "started", priority: 0, started: true }),
      issue({ identifier: "urgent", priority: 1 }),
    ]);
    expect(got.map((r) => r.issue.identifier)).toEqual([
      "urgent",
      "started",
      "none",
    ]);
  });

  // The identifier is what you type to find one, so it leads the description.
  it("leads the description with the identifier", () => {
    expect(rows([issue()])[0].description).toBe("ADE-1 · Todo");
  });

  it("titles the row with the ticket's title", () => {
    expect(rows([issue()])[0].label).toBe("a ticket");
  });
});

describe("what an issue offers", () => {
  it("starts it, or picks it back up when it is already going", () => {
    expect(acts(issue())[0].label).toBe("Start here");
    expect(acts(issue({ started: true }))[0].label).toBe("Pick it back up");
  });

  it("names the branch when Linear gave one", () => {
    expect(acts(issue({ branchName: "owen/ade-1" }))[0].detail).toBe(
      "on owen/ade-1",
    );
  });

  it("always offers the Linear page, with the link in view", () => {
    const linear = acts(issue()).find((a) => a.id === "linear");
    expect(linear?.detail).toBe("https://linear.app/ade/issue/ADE-1");
  });
});
