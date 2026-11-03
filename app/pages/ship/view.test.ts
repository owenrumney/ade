import { describe, expect, it } from "vitest";
import { columns, rail, steps, type Project, type State } from "./view";

const branch: Project = {
  root: "/w/ade",
  name: "ade",
  branch: "work",
  home: true,
  base: "origin/main",
  files: 0,
  staged: 0,
  ahead: 0,
  behind: 0,
};

function said(project: Project, key: string): string {
  return steps(project).find((step) => step.key === key)?.said ?? "";
}

function act(project: Project, key: string): string {
  return steps(project).find((step) => step.key === key)?.act?.do ?? "";
}

describe("the uncommitted card", () => {
  it("is done when there is nothing uncommitted", () => {
    expect(steps(branch)[0].state).toBe("done");
    expect(act(branch, "work")).toBe("");
  });

  it("asks for a message before it offers to commit", () => {
    const project = { ...branch, files: 2, staged: 2 };
    expect(act(project, "work")).toBe("message");
    expect(steps(project, { title: "fix the thing" })[0].act?.do).toBe("commit");
    expect(steps(project, { title: "fix the thing" })[0].said).toContain("fix the thing");
  });

  it("offers nothing while the work is all unstaged", () => {
    const project = { ...branch, files: 2, staged: 0 };
    expect(act(project, "work")).toBe("");
    expect(steps(project)[0].because).toBe("nothing is staged");
  });
});

describe("the branch cards", () => {
  it("offers a rebase when the base has moved on", () => {
    const project = { ...branch, behind: 3 };
    expect(said(project, "base")).toBe("3 commits behind origin/main");
    expect(act(project, "base")).toBe("rebase");
  });

  it("offers a push for commits the remote has never had", () => {
    const project = { ...branch, unpushed: [{ sha: "a", subject: "one" }] };
    expect(said(project, "push")).toBe("1 commit, none pushed");
    expect(act(project, "push")).toBe("push");
  });

  it("offers a push for commits ahead of the upstream", () => {
    const project = { ...branch, upstream: "origin/work", ahead: 2 };
    expect(act(project, "push")).toBe("push");
    expect(said(project, "push")).toBe("2 commits unpushed");
  });

  it("is done when the upstream has everything", () => {
    const project = { ...branch, upstream: "origin/work", ahead: 0 };
    expect(steps(project).find((s) => s.key === "push")?.state).toBe("done");
  });
});

describe("the pull request cards", () => {
  const raised: Project = {
    ...branch,
    upstream: "origin/work",
    pr: { number: 41, title: "ship", state: "OPEN", failing: 0, checks: [] },
  };

  it("offers to raise one only once there is a commit to raise it for", () => {
    expect(act(branch, "pr")).toBe("");
    expect(act({ ...branch, unpushed: [{ sha: "a", subject: "one" }] }, "pr")).toBe("raise");
  });

  it("names the pull request rather than offering to raise another", () => {
    expect(said(raised, "pr")).toBe("#41 open · ship");
    expect(act(raised, "pr")).toBe("");
  });

  it("says why the checks failed and offers the fix", () => {
    const failing = {
      ...raised,
      pr: { ...raised.pr!, failing: 1, failLine: "test (ci): one failed", checks: [{ name: "test" }] },
    };
    expect(said(failing, "checks")).toBe("test (ci): one failed");
    expect(act(failing, "checks")).toBe("fix");
  });

  it("gives the merge its blocker instead of a button", () => {
    const blocked = { ...raised, pr: { ...raised.pr!, mergeBlocked: "it conflicts with main" } };
    expect(said(blocked, "merge")).toBe("it conflicts with main");
    expect(act(blocked, "merge")).toBe("");
    expect(act(raised, "merge")).toBe("merge");
  });

  // The routes behind these refuse a diff pointed at another repository, so
  // the card says so rather than offering a button that cannot work.
  it("leaves GitHub's steps to this session's own checkout", () => {
    const elsewhere = { ...raised, home: false };
    expect(act(elsewhere, "merge")).toBe("");
    expect(steps(elsewhere).find((s) => s.key === "merge")?.because).toContain("own checkout");
    // Commit and push are the diff's, and the diff can be pointed anywhere.
    expect(act({ ...elsewhere, behind: 1 }, "base")).toBe("rebase");
  });
});

describe("the page as a whole", () => {
  it("gives the draft to the repository the diff is on and no other", () => {
    const other: Project = { ...branch, root: "/w/ci", name: "ci", home: false, files: 1, staged: 1 };
    const state: State = {
      projects: [{ ...branch, files: 1, staged: 1 }, other],
      home: "/w/ade",
      view: "/w/ci",
      draft: { title: "the message" },
    };
    const [home, ci] = columns(state);
    expect(home.steps[0].act?.do).toBe("message");
    expect(ci.steps[0].act?.do).toBe("commit");
  });

  it("draws nothing but the reason for a repository git would not answer for", () => {
    const [only] = columns({ projects: [{ ...branch, note: "not a git repository" }] });
    expect(only.steps).toEqual([]);
    expect(only.note).toBe("not a git repository");
  });

  it("puts the gates in the way first", () => {
    const gates = rail({
      gates: [
        { name: "a", state: "pass", note: "" },
        { name: "b", state: "optional", note: "" },
        { name: "c", state: "fail", note: "" },
        { name: "d", state: "warn", note: "" },
      ],
    });
    expect(gates.map((gate) => gate.name)).toEqual(["c", "d", "a", "b"]);
  });
});
