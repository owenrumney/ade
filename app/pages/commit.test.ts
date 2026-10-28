import { describe, expect, it } from "vitest";
import {
  ahead,
  committable,
  groups,
  icon,
  join,
  rejected,
  split,
  stageFirst,
} from "./commit";

describe("split", () => {
  it("takes the first line as the subject", () => {
    expect(split("fix: the thing")).toEqual({
      title: "fix: the thing",
      body: "",
    });
  });

  it("drops the blank line git puts after the subject", () => {
    expect(split("fix: it\n\nbecause of the other thing")).toEqual({
      title: "fix: it",
      body: "because of the other thing",
    });
  });

  it("keeps the blank lines inside a body", () => {
    expect(split("a\n\none\n\ntwo").body).toBe("one\n\ntwo");
  });

  it("survives a box with windows line endings", () => {
    expect(split("a\r\n\r\nb")).toEqual({ title: "a", body: "b" });
  });

  it("is empty for an empty box, rather than a title of spaces", () => {
    expect(split("   \n\n  ")).toEqual({ title: "", body: "" });
  });

  it("round-trips a draft through the box", () => {
    const draft = { title: "feat: x", body: "why\n\nmore", staged: null };
    expect(split(join(draft))).toEqual({
      title: "feat: x",
      body: "why\n\nmore",
    });
  });

  it("writes no trailing blank lines for a draft with no body", () => {
    expect(join({ title: "chore: y", body: "", staged: null })).toBe(
      "chore: y",
    );
  });
});

describe("groups", () => {
  it("splits the worktree into the two the editor draws", () => {
    const got = groups({
      staged: ["a.go"],
      unstaged: [{ path: "b.go", status: "modified" }],
      ahead: 0,
    });
    expect(got.staged).toEqual([{ path: "a.go", status: "" }]);
    expect(got.changed).toEqual([{ path: "b.go", status: "modified" }]);
  });

  it("is two empty groups when nothing has changed", () => {
    expect(groups(undefined)).toEqual({ staged: [], changed: [] });
    expect(groups({ staged: null, unstaged: null, ahead: 0 })).toEqual({
      staged: [],
      changed: [],
    });
  });
});

describe("icon", () => {
  it("draws the words git's porcelain spells out", () => {
    expect(icon("modified")).toBe("diff-modified");
    expect(icon("untracked")).toBe("diff-added");
    expect(icon("deleted")).toBe("diff-removed");
  });

  it("falls back rather than drawing nothing for a status it does not know", () => {
    expect(icon("typechange")).toBe("diff-modified");
  });
});

describe("ahead", () => {
  it("says nothing about a branch with no upstream", () => {
    expect(ahead({ staged: null, unstaged: null, ahead: 3 })).toBe("");
  });

  it("says nothing when the branch has not run ahead", () => {
    expect(
      ahead({
        staged: null,
        unstaged: null,
        upstream: "origin/main",
        ahead: 0,
      }),
    ).toBe("");
  });

  it("names the upstream it is ahead of", () => {
    expect(
      ahead({
        staged: null,
        unstaged: null,
        upstream: "origin/main",
        ahead: 2,
      }),
    ).toBe("2 ahead of origin/main");
  });
});

describe("committable", () => {
  const one = [{ path: "a.go", status: "" }];

  it("needs a title and something staged", () => {
    expect(committable("fix: it", one, false)).toBe(true);
    expect(committable("", one, false)).toBe(false);
    expect(committable("fix: it", [], false)).toBe(false);
  });

  // Amending only the message is a real thing to want, and the server allows
  // an empty index for it.
  it("lets an amend through with an empty box", () => {
    expect(committable("", [], true)).toBe(true);
  });
});

describe("rejected", () => {
  // An amend always lands here, and git names the refusal without saying that
  // a force is the way past it.
  it("reads git's refusal to fast-forward", () => {
    expect(rejected("! [rejected] main -> main (non-fast-forward)")).toBe(true);
    expect(
      rejected("hint: Updates were rejected. Integrate the remote: fetch first"),
    ).toBe(true);
  });

  it("is not every failure: a dead remote is not force-pushable", () => {
    expect(rejected("Could not resolve host: github.com")).toBe(false);
    expect(rejected("Permission denied (publickey)")).toBe(false);
  });
});

describe("staging before a message is written", () => {
  it("stages everything when nothing is staged", () => {
    expect(stageFirst(0, 3)).toBe(true);
  });

  it("leaves a chosen subset alone", () => {
    expect(stageFirst(2, 5)).toBe(false);
  });

  it("does nothing when there is nothing to stage", () => {
    expect(stageFirst(0, 0)).toBe(false);
  });
});
