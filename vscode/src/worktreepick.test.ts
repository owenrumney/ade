import { describe, expect, it } from "vitest";
import type { Tree } from "./client";
import { acts, rows } from "./worktreepick";

const tree = (over: Partial<Tree> = {}): Tree => ({
  path: "/code/ade",
  name: "ade",
  kind: "linked",
  removable: true,
  ...over,
});

const main = tree({
  path: "/code/ade",
  name: "ade",
  main: true,
  branch: "main",
});

describe("the worktree rows", () => {
  it("puts the repository's own checkout first", () => {
    const got = rows(main, [tree({ path: "/code/ade-work", branch: "work" })]);
    expect(got.map((r) => r.label)).toEqual(["main", "work"]);
  });

  it("says which one is the repository's own", () => {
    expect(rows(main, [])[0].description).toContain(
      "the repository's own checkout",
    );
  });

  // The row is already labelled with the branch, so facts rather than line:
  // saying the branch twice is what a narrow picker cannot afford.
  it("does not repeat the branch in the description", () => {
    const got = rows(undefined, [
      tree({ branch: "work", uncommitted: 2, serving: "ade" }),
    ]);
    expect(got[0].label).toBe("work");
    expect(got[0].description).not.toContain("work");
    expect(got[0].description).toContain("2 uncommitted");
  });

  it("puts the path where you can read it before opening", () => {
    expect(rows(undefined, [tree()])[0].detail).toBe("/code/ade");
  });
});

describe("what a worktree offers", () => {
  const ids = (t: Tree) => acts(t).map((a) => a.id);

  it("opens the folder and starts a session on any of them", () => {
    for (const t of [main, tree()]) {
      expect(ids(t)).toContain("open");
      expect(ids(t)).toContain("session");
    }
  });

  it("never offers to remove the repository's own checkout", () => {
    expect(ids(main)).not.toContain("remove");
  });

  // A linked worktree that cannot be removed still offers it: the command
  // asks, and says what is in the way.
  it("offers remove on a linked worktree, removable or not", () => {
    expect(ids(tree())).toContain("remove");
    expect(ids(tree({ removable: false }))).toContain("remove");
  });

  it("says what stands in the way before you pick it", () => {
    const stuck = acts(tree({ removable: false, serving: "ade" })).find(
      (a) => a.id === "remove",
    );
    expect(stuck?.detail).toContain("being served");
  });
});
