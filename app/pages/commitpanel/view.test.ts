import { describe, expect, it } from "vitest";
import {
  bar,
  branchable,
  drifted,
  idle,
  loose,
  onBase,
  rows,
  said,
  signing,
  staged,
  suggest,
  verbs,
  where,
  widest,
  rebase,
} from "./view";

const tree = {
  staged: ["internal/web/chat.go", "internal/web/agent.go"],
  unstaged: [
    { path: "vscode/src/scm.ts", status: "M" },
    { path: "internal/web/chat.go", status: "M" },
  ],
};

describe("the commit panel's files", () => {
  it("is one row a path, staged ones checked", () => {
    const all = rows(tree);
    expect(all).toHaveLength(3);
    expect(staged(all).sort()).toEqual([
      "internal/web/agent.go",
      "internal/web/chat.go",
    ]);
    expect(all.find((r) => r.path === "vscode/src/scm.ts")?.staged).toBe(false);
  });

  // A tree of one-file directories was more chrome than the paths it hid.
  it("reads as a path, with the directory to one side", () => {
    expect(where("internal/web/chat.go")).toEqual({
      dir: "internal/web/",
      name: "chat.go",
    });
    expect(where("Makefile")).toEqual({ dir: "", name: "Makefile" });
  });

  it("knows what Stage all would add", () => {
    expect(loose(rows(tree))).toEqual(["vscode/src/scm.ts"]);
    expect(loose([])).toEqual([]);
  });

  it("copes with nothing changed", () => {
    expect(rows(undefined)).toEqual([]);
    expect(said([], [])).toContain("Nothing is checked");
  });

  it("draws the busiest file full width and the rest against it", () => {
    const all = rows({
      ...tree,
      lines: {
        "internal/web/agent.go": { added: 75, removed: 25 },
        "internal/web/chat.go": { added: 50, removed: 0 },
      },
    });
    expect(widest(all)).toBe(100);

    const big = bar(
      all.find((r) => r.path === "internal/web/agent.go")!,
      100,
    );
    expect(big.added + big.removed).toBeCloseTo(100);
    expect(big.added).toBeCloseTo(75);
    expect(big.title).toBe("+75 −25");

    const half = bar(
      all.find((r) => r.path === "internal/web/chat.go")!,
      100,
    );
    expect(half.added).toBeCloseTo(50);
    expect(half.removed).toBe(0);
  });

  // A one-line change beside a thousand-line one rounds to nothing, and a row
  // that changed something must not read as a row that did not.
  it("gives the smallest change a length you can see", () => {
    const tiny = bar(
      { path: "a", status: "M", staged: true, lines: { added: 1, removed: 0 } },
      4000,
    );
    expect(tiny.added).toBeGreaterThan(2);
  });

  it("draws nothing for a path git has no count for", () => {
    const one = rows(tree).find((r) => r.path === "vscode/src/scm.ts")!;
    expect(bar(one, 100)).toEqual({ added: 0, removed: 0, title: "" });
    expect(widest([])).toBe(0);
  });
});

describe("a message written for a different set", () => {
  const draft = {
    title: "cache the cart",
    body: "",
    staged: ["a.go", "b.go"],
  };

  it("is called out once the files move", () => {
    expect(drifted(draft, ["a.go", "b.go"])).toBe(false);
    expect(drifted(draft, ["b.go", "a.go"])).toBe(false);
    expect(drifted(draft, ["a.go"])).toBe(true);
    expect(drifted(draft, ["a.go", "b.go", "c.go"])).toBe(true);
  });

  it("says nothing when there is no message yet", () => {
    expect(drifted(undefined, ["a.go"])).toBe(false);
    expect(drifted({ title: "", body: "", staged: null }, ["a.go"])).toBe(
      false,
    );
  });
});

describe("what the panel offers", () => {
  it("needs words and files before it can commit", () => {
    expect(verbs("", ["a.go"], false, 0).commit).toBe(false);
    expect(verbs("a title", [], false, 0).commit).toBe(false);
    expect(verbs("a title", ["a.go"], false, 0).commit).toBe(true);
  });

  // Nothing to commit but commits to push is the ordinary state after one.
  it("offers a push for commits already made", () => {
    expect(verbs("", [], false, 2).push).toBe(true);
    expect(verbs("", [], false, 0).push).toBe(false);
  });

  it("offers the pull request it does not have, and shows the one it does", () => {
    expect(verbs("t", ["a"], false, 0).createPR).toBe(true);
    expect(verbs("t", ["a"], false, 0).showPR).toBe(false);
    expect(verbs("t", ["a"], true, 0).createPR).toBe(false);
    expect(verbs("t", ["a"], true, 0).showPR).toBe(true);
  });

  it("says what a commit would carry", () => {
    const all = rows(tree);
    expect(said(staged(all), all)).toContain("leaving 1 out");
    expect(
      said(
        all.map((r) => r.path),
        all,
      ),
    ).toContain("everything you have changed");
  });
});

describe("what the panel does on arrival", () => {
  // Opening it is saying "I want to commit": everything in, and a message
  // for exactly that set.
  it("stages what is loose", () => {
    expect(loose(rows(tree))).toEqual(["vscode/src/scm.ts"]);
  });

  // A draft already written for that set is not worth a second run.
  it("knows when the draft already fits", () => {
    const all = [...(tree.staged ?? []), "vscode/src/scm.ts"];
    expect(drifted({ title: "t", body: "", staged: all }, all)).toBe(false);
    expect(drifted({ title: "t", body: "", staged: tree.staged }, all)).toBe(
      true,
    );
  });
});

describe("whether the commit will be signed", () => {
  it("names the key and how", () => {
    const got = signing({
      on: true,
      format: "ssh",
      key: "~/.ssh/id_ed25519.pub",
    });
    expect(got.on).toBe(true);
    expect(got.text).toContain("SSH");
    expect(got.text).toContain("id_ed25519.pub");
  });

  it("calls an unset format GPG, which is git's own default", () => {
    expect(signing({ on: true }).text).toContain("GPG");
  });

  it("says so when it will not be signed", () => {
    expect(signing({ on: false }).text).toBe("Not signed");
    expect(signing(undefined)).toEqual({ text: "Not signed", on: false });
  });
});

describe("while something is running", () => {
  // A run owns the message and the index. Clicking during one acts on what it
  // is in the middle of changing.
  it("the panel is not to be touched", () => {
    expect(idle("")).toBe(true);
    expect(idle("redraft")).toBe(false);
    expect(idle("commit")).toBe(false);
  });
});

describe("starting a branch to merge onto", () => {
  it("knows the branches a commit should not land on", () => {
    expect(onBase("main")).toBe(true);
    expect(onBase("master")).toBe(true);
    expect(onBase("feat/thing")).toBe(false);
    expect(onBase(undefined)).toBe(false);
  });

  it("names the branch off the message, ticket first", () => {
    expect(suggest("feat(commit): start a branch [ABC-12]")).toBe(
      "abc-12-start-a-branch",
    );
  });

  it("drops the conventional-commit prefix and keeps five words", () => {
    expect(suggest("fix: one two three four five six")).toBe(
      "one-two-three-four-five",
    );
  });

  it("has nothing to suggest for an empty message", () => {
    expect(suggest("")).toBe("");
  });

  it("will not create a name git refuses or the one you are on", () => {
    expect(branchable("feat/thing", "main")).toBe(true);
    expect(branchable("  ", "main")).toBe(false);
    expect(branchable("main", "main")).toBe(false);
    expect(branchable("two words", "main")).toBe(false);
    expect(branchable("--force", "main")).toBe(false);
    expect(branchable("what?", "main")).toBe(false);
  });
});

describe("the rebase button", () => {
  it("names the base it would replay onto", () => {
    expect(rebase(3, 0, "origin/main").label).toBe("Rebase onto origin/main");
    expect(rebase(3, 0, "").label).toBe("Rebase onto the base branch");
  });

  it("can act only when the branch is behind and nothing is uncommitted", () => {
    expect(rebase(3, 0, "origin/main")).toMatchObject({ can: true, why: "3 commits behind" });
    expect(rebase(1, 0, "origin/main")).toMatchObject({ can: true, why: "1 commit behind" });
  });

  // A rebase over uncommitted work loses it, so the server refuses. Saying so
  // here means the button explains itself rather than failing when pressed.
  it("says what to do first when there is uncommitted work", () => {
    expect(rebase(3, 2, "origin/main")).toMatchObject({
      can: false,
      why: "3 commits behind · commit or stash first",
    });
  });

  it("is drawn and inert when there is nothing to replay", () => {
    expect(rebase(0, 0, "origin/main")).toMatchObject({ can: false, why: "up to date" });
    expect(rebase(undefined, 0, "origin/main")).toMatchObject({ can: false, why: "up to date" });
  });
});
