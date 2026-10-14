import { describe, expect, it } from "vitest";
import {
  branchLine,
  checkIcon,
  checkLine,
  failing,
  issueIcon,
  issueLine,
  prIcon,
  prLine,
  rankCheck,
  rankIssue,
  rankPR,
} from "./issues";
import type { BranchPR, Check, Issue, Listed } from "./client";

const issue = (over: Partial<Issue> = {}): Issue => ({
  identifier: "ENG-1",
  title: "do the thing",
  url: "https://linear.app/x/ENG-1",
  state: "Todo",
  ...over,
});

const pr = (over: Partial<Listed> = {}): Listed => ({
  number: 12,
  title: "feat: it",
  author: "owenrumney",
  headRef: "fix-thing",
  url: "https://github.com/o/r/pull/12",
  ...over,
});

describe("issueLine", () => {
  it("is the state Linear gave it", () => {
    expect(issueLine(issue({ state: "In Progress" }))).toBe("In Progress");
  });

  it("falls back to saying it is started when there is no state", () => {
    expect(issueLine(issue({ state: "", started: true }))).toBe("in progress");
    expect(issueLine(issue({ state: "" }))).toBe("");
  });
});

describe("rankIssue", () => {
  // Linear's priority 0 is "none", not the most urgent.
  it("does not read no-priority as top priority", () => {
    const got = [
      issue({ identifier: "none", priority: 0 }),
      issue({ identifier: "urgent", priority: 1 }),
    ].sort(rankIssue);
    expect(got[0].identifier).toBe("urgent");
  });

  it("puts a started issue above an untouched one of the same priority", () => {
    const got = [
      issue({ identifier: "a" }),
      issue({ identifier: "b", started: true }),
    ].sort(rankIssue);
    expect(got[0].identifier).toBe("b");
  });

  it("orders the rest by when Linear last touched them", () => {
    const got = [
      issue({ identifier: "old", updatedAt: "2026-09-01T00:00:00Z" }),
      issue({ identifier: "new", updatedAt: "2026-09-30T00:00:00Z" }),
    ].sort(rankIssue);
    expect(got[0].identifier).toBe("new");
  });
});

describe("issueIcon", () => {
  it("distinguishes started from not", () => {
    expect(issueIcon(issue({ started: true }))).toBe("debug-start");
    expect(issueIcon(issue())).toBe("circle-outline");
  });
});

describe("prLine", () => {
  it("leads with the branch", () => {
    expect(prLine(pr())).toBe("fix-thing");
  });

  it("says the checks and the decision in words", () => {
    const got = prLine(
      pr({ checks: "fail", reviewDecision: "CHANGES_REQUESTED" }),
    );
    expect(got).toContain("checks fail");
    expect(got).toContain("changes requested");
  });

  // GitHub's enum is shouted; a row is read, not parsed.
  it("does not print GitHub's own spelling", () => {
    expect(prLine(pr({ reviewDecision: "APPROVED" }))).toContain("approved");
    expect(prLine(pr({ reviewDecision: "APPROVED" }))).not.toContain(
      "APPROVED",
    );
  });

  it("ignores a decision it does not know rather than showing the enum", () => {
    expect(prLine(pr({ reviewDecision: "SOMETHING_NEW" }))).toBe("fix-thing");
  });
});

describe("prIcon", () => {
  it("marks one waiting on your review above all else", () => {
    expect(
      prIcon(pr({ reviewRequested: true, draft: true, checks: "fail" })),
    ).toBe("bell");
  });

  it("marks failing checks, then draft", () => {
    expect(prIcon(pr({ checks: "fail" }))).toBe("error");
    expect(prIcon(pr({ draft: true }))).toBe("git-pull-request-draft");
    expect(prIcon(pr())).toBe("git-pull-request");
  });
});

describe("rankPR", () => {
  it("puts one waiting on your review first", () => {
    const got = [
      pr({ number: 1, mine: true }),
      pr({ number: 2, reviewRequested: true }),
    ].sort(rankPR);
    expect(got[0].number).toBe(2);
  });

  it("then yours", () => {
    const got = [pr({ number: 1 }), pr({ number: 2, mine: true })].sort(rankPR);
    expect(got[0].number).toBe(2);
  });

  it("then the most recently touched", () => {
    const got = [
      pr({ number: 1, updatedAt: "2026-09-01T00:00:00Z" }),
      pr({ number: 2, updatedAt: "2026-09-30T00:00:00Z" }),
    ].sort(rankPR);
    expect(got[0].number).toBe(2);
  });
});

describe("checks", () => {
  const check = (over: Partial<Check> = {}): Check => ({
    name: "build",
    state: "SUCCESS",
    bucket: "pass",
    ...over,
  });

  it("draws each bucket differently", () => {
    expect(checkIcon(check({ bucket: "fail" }))).toBe("error");
    expect(checkIcon(check({ bucket: "pending" }))).toBe("sync~spin");
    expect(checkIcon(check({ bucket: "pass" }))).toBe("pass-filled");
  });

  it("falls back rather than drawing nothing for a bucket it does not know", () => {
    expect(checkIcon(check({ bucket: "whatever" }))).toBe("circle-outline");
  });

  // The icon already says the state; repeating it wastes the only column.
  it("says the workflow and the description, not the state", () => {
    const line = checkLine(
      check({ workflow: "CI", description: "3 failed", state: "FAILURE" }),
    );
    expect(line).toBe("CI · 3 failed");
  });

  it("is empty when there is nothing to add", () => {
    expect(checkLine(check())).toBe("");
  });

  it("picks out the failing ones", () => {
    const all = [
      check({ bucket: "pass" }),
      check({ name: "test", bucket: "fail" }),
    ];
    expect(failing(all).map((c) => c.name)).toEqual(["test"]);
    expect(failing(null)).toEqual([]);
  });

  // A list of checks is read to find what is wrong.
  it("puts failing first, then running, then the rest", () => {
    const got = [
      check({ name: "d", bucket: "pass" }),
      check({ name: "c", bucket: "skipping" }),
      check({ name: "b", bucket: "pending" }),
      check({ name: "a", bucket: "fail" }),
    ].sort(rankCheck);
    expect(got.map((c) => c.name)).toEqual(["a", "b", "c", "d"]);
  });

  it("orders two of the same bucket by name", () => {
    const got = [
      check({ name: "z", bucket: "fail" }),
      check({ name: "a", bucket: "fail" }),
    ].sort(rankCheck);
    expect(got.map((c) => c.name)).toEqual(["a", "z"]);
  });
});

describe("branchLine", () => {
  const branch = (over: Partial<BranchPR> = {}): BranchPR => ({
    number: 12,
    title: "feat: it",
    url: "https://github.com/o/r/pull/12",
    state: "OPEN",
    ...over,
  });

  it("leads with what is failing, which is why you looked", () => {
    const pr = branch({
      checks: [{ name: "t", state: "F", bucket: "fail" }],
      draft: true,
    });
    expect(branchLine(pr).startsWith("1 failing")).toBe(true);
  });

  it("says checks pass when they all did", () => {
    expect(
      branchLine(
        branch({ checks: [{ name: "t", state: "S", bucket: "pass" }] }),
      ),
    ).toBe("checks pass");
  });

  it("says nothing about checks that have not reported", () => {
    expect(branchLine(branch())).toBe("");
    expect(branchLine(branch({ checks: [] }))).toBe("");
  });

  it("carries the reason a merge is blocked", () => {
    expect(branchLine(branch({ mergeBlocked: "review required" }))).toContain(
      "review required",
    );
  });
});
