import { describe, expect, it } from "vitest";
import { headline, NOTHING, refusal, standingIn } from "./commitbox";

describe("what the commit box reads from a session", () => {
  it("counts what is staged, what is not, and what is unpushed", () => {
    const at = standingIn({ worktree: { staged: ["a.go", "b.go"], unstaged: [{ path: "c.go" }], ahead: 3 } });
    expect(at).toMatchObject({ staged: 2, unstaged: 1, ahead: 3, drafting: false });
    expect(at.draft).toBeUndefined();
  });

  it("is nothing for a session that has not answered", () => {
    expect(standingIn(undefined)).toEqual({ ...NOTHING, draft: undefined });
    expect(standingIn({ worktree: null, draft: null })).toEqual({ ...NOTHING, draft: undefined });
  });

  it("is drafting only while the run is a commit message", () => {
    expect(standingIn({ agent: { running: true, kind: "commit-message" } }).drafting).toBe(true);
    expect(standingIn({ agent: { running: true, kind: "review" } }).drafting).toBe(false);
    expect(standingIn({ agent: { running: false, kind: "commit-message" } }).drafting).toBe(false);
  });

  it("takes the drafted title and body", () => {
    const at = standingIn({ draft: { title: "fix(term): keep the pane", body: "It was dropped.", staged: ["a.go"] } });
    expect(at.draft).toEqual({ title: "fix(term): keep the pane", body: "It was dropped." });
  });

  it("shows a reply that did not parse as the body, to be edited", () => {
    const at = standingIn({ draft: { raw: "I could not read the diff.", staged: ["a.go"] } });
    expect(at.draft).toEqual({ title: "", body: "I could not read the diff." });
  });
});

describe("the commit box's one line of standing", () => {
  it("says what a commit would take", () => {
    expect(headline({ ...NOTHING, staged: 2 })).toBe("2 staged");
    expect(headline({ ...NOTHING, staged: 2, unstaged: 3 })).toBe("2 staged · 3 not staged");
  });

  it("says when a commit would take nothing", () => {
    expect(headline({ ...NOTHING, unstaged: 3 })).toBe("nothing staged · 3 not staged");
    expect(headline(NOTHING)).toBe("nothing to commit");
  });
});

describe("what the box knows of the branch", () => {
  it("reads the branch and its pull request", () => {
    const at = standingIn({ meta: { branch: "feat/x" }, branchPr: { number: 12 } });
    expect(at.branch).toBe("feat/x");
    expect(at.pr).toBe(12);
    expect(standingIn({ meta: { branch: "feat/x" } }).pr).toBeUndefined();
  });
});

describe("whether a press can commit", () => {
  const staged = { ...NOTHING, staged: 2 };
  it("needs something staged and a title", () => {
    expect(refusal(NOTHING, {}, "fix")).toBe("nothing is staged to commit");
    expect(refusal(staged, {}, "")).toBe("a commit needs a title");
    expect(refusal(staged, { push: true }, "fix")).toBe("");
  });

  // An amend with nothing staged changes the message; with no title it keeps it.
  it("lets an amend go with either missing", () => {
    expect(refusal(NOTHING, { amend: true }, "")).toBe("");
    expect(refusal(staged, { amend: true }, "")).toBe("");
  });
});
