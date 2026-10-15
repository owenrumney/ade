import { describe, expect, it } from "vitest";
import type { Tree } from "../client";
import {
  byRepo,
  chosen,
  confirm,
  kept,
  port,
  removable,
  rows,
  state,
  summary,
  told,
  type Sweep,
} from "./view";

function tree(over: Partial<Tree> = {}): Tree {
  return {
    path: "/code/ade-fix",
    name: "ade-fix",
    branch: "fix",
    kind: "session",
    removable: true,
    ...over,
  };
}

const sweep: Sweep = {
  repos: [
    {
      path: "/code/ade",
      name: "ade",
      worktrees: [
        tree({ path: "/code/ade-old", name: "ade-old", branch: "old", behind: 12 }),
        tree({
          path: "/code/ade-live",
          name: "ade-live",
          branch: "live",
          removable: false,
          keep: "serving",
          serving: "http://127.0.0.1:4310/",
        }),
      ],
    },
    {
      path: "/code/coast",
      name: "coast",
      worktrees: [
        tree({
          path: "/code/coast-wip",
          name: "coast-wip",
          branch: "wip",
          removable: false,
          keep: "unpushed",
          unpushed: 2,
        }),
      ],
    },
  ],
};

describe("rows", () => {
  it("flattens the sweep, keeping which repository each came from", () => {
    const out = rows(sweep);
    expect(out.map((r) => r.repo)).toEqual(["ade", "ade", "coast"]);
    expect(out.map((r) => r.name)).toEqual(["ade-old", "ade-live", "coast-wip"]);
    expect(out[0].behind).toBe("12 behind");
    expect(out[1].behind).toBe("");
  });

  it("is empty for a sweep that found nothing", () => {
    expect(rows(undefined)).toEqual([]);
    expect(rows({ repos: [] })).toEqual([]);
  });

  it("dates each row by when git last touched it", () => {
    const now = Date.parse("2026-10-15T12:00:00Z");
    const [row] = rows(
      { repos: [{ path: "/code/ade", name: "ade", worktrees: [tree({ lastUsed: "2026-08-15T12:00:00Z" })] }] },
      false,
      now,
    );
    expect(row.lastUsed).toBe("2 months ago");
  });

  // Go writes a time it never read as 0001-01-01, which age() would date to
  // twenty thousand months ago and put at the top of the oldest-first list.
  it("leaves a worktree it could not date undated", () => {
    const [row] = rows({
      repos: [
        {
          path: "/code/ade",
          name: "ade",
          worktrees: [tree({ lastUsed: "0001-01-01T00:00:00Z" })],
        },
      ],
    });
    expect(row.lastUsed).toBe("");
  });

  it("names the owner when two scan roots hold the same name", () => {
    const [row] = rows({
      repos: [{ path: "/code/infracost/ade", name: "ade", owner: "infracost", worktrees: [tree()] }],
    });
    expect(row.repo).toBe("infracost/ade");
  });

  it("marks the rows only force frees", () => {
    expect(rows(sweep, true).map((r) => r.forced)).toEqual([false, false, true]);
    expect(rows(sweep).map((r) => r.forced)).toEqual([false, false, false]);
  });
});

describe("state", () => {
  it("says removable when nothing stands in the way", () => {
    expect(state(tree())).toBe("Removable");
  });

  it("names the ade that is using it, by port", () => {
    expect(state(tree({ removable: false, keep: "serving", serving: "http://127.0.0.1:4310/" }))).toBe(
      "ade is serving it on :4310",
    );
  });

  it("counts uncommitted files, and says file once for one", () => {
    expect(state(tree({ removable: false, keep: "uncommitted", uncommitted: 3 }))).toBe(
      "3 files uncommitted",
    );
    expect(state(tree({ removable: false, keep: "uncommitted", uncommitted: 1 }))).toBe(
      "1 file uncommitted",
    );
  });

  it("says where commits exist and nowhere else", () => {
    expect(state(tree({ removable: false, keep: "unpushed", unpushed: 2 }))).toBe(
      "2 commits are only here",
    );
    expect(state(tree({ removable: false, keep: "unpushed", unpushed: 1 }))).toBe(
      "1 commit is only here",
    );
  });

  it("keeps saying what force would lose", () => {
    const wip = tree({ removable: false, keep: "unpushed", unpushed: 2 });
    expect(state(wip, true)).toBe("Removable · 2 commits are only here");
  });

  it("repeats a git failure the scan could not read past", () => {
    expect(kept(tree({ removable: false, keep: "fatal: not a git repository" }))).toBe(
      "fatal: not a git repository",
    );
  });
});

describe("force", () => {
  it("only ever frees a row whose one problem is unpushed work", () => {
    const wip = tree({ removable: false, keep: "unpushed", unpushed: 2 });
    expect(removable(wip, false)).toBe(false);
    expect(removable(wip, true)).toBe(true);
  });

  it("leaves a served or a dirty worktree blocked", () => {
    const live = tree({ removable: false, keep: "serving", serving: "http://127.0.0.1:4310/" });
    const dirty = tree({ removable: false, keep: "uncommitted", uncommitted: 3 });
    expect(removable(live, true)).toBe(false);
    expect(removable(dirty, true)).toBe(false);
  });
});

describe("port", () => {
  it("shortens a serving address to what tells one ade from another", () => {
    expect(port("http://127.0.0.1:4310/")).toBe(":4310");
    expect(port("somewhere-with-no-port")).toBe("somewhere-with-no-port");
  });
});

describe("chosen", () => {
  it("drops a tick on a row the latest sweep says is kept", () => {
    const all = rows(sweep);
    const ticked = new Set(["/code/ade-old", "/code/ade-live"]);
    expect(chosen(all, ticked).map((r) => r.path)).toEqual(["/code/ade-old"]);
  });
});

describe("byRepo", () => {
  it("groups the removals, because prune is one repository at a time", () => {
    const all = rows(sweep, true);
    const picked = chosen(all, new Set(["/code/ade-old", "/code/coast-wip"]));
    expect(byRepo(picked)).toEqual([
      { repo: "/code/ade", paths: ["/code/ade-old"] },
      { repo: "/code/coast", paths: ["/code/coast-wip"] },
    ]);
  });
});

describe("confirm", () => {
  it("names the count and the repositories it spans", () => {
    const picked = chosen(rows(sweep), new Set(["/code/ade-old"]));
    expect(confirm(picked)).toContain("1 worktree in 1 repository.");
    expect(confirm(picked)).toContain("Branches are never deleted.");
  });

  it("says out loud what a forced row loses", () => {
    const picked = chosen(rows(sweep, true), new Set(["/code/ade-old", "/code/coast-wip"]));
    expect(confirm(picked)).toContain("2 worktrees in 2 repositories.");
    expect(confirm(picked)).toContain("commits that are nowhere else");
  });

  // Two scan roots hold two repositories called ade. Counting by name says one.
  it("counts the repositories by path", () => {
    const two: Sweep = {
      repos: [
        { path: "/code/ade", name: "ade", worktrees: [tree({ path: "/code/ade-a" })] },
        { path: "/work/ade", name: "ade", worktrees: [tree({ path: "/work/ade-b" })] },
      ],
    };
    const picked = chosen(rows(two), new Set(["/code/ade-a", "/work/ade-b"]));
    expect(confirm(picked)).toContain("2 worktrees in 2 repositories.");
  });
});

describe("told", () => {
  it("reports what went and every one that would not, with its reason", () => {
    expect(told(["/code/a", "/code/b"], [])).toBe("Removed 2.");
    expect(told(["/code/a"], [{ path: "/code/b", why: "uncommitted" }])).toBe(
      "Removed 1. Kept b: uncommitted",
    );
    expect(
      told([], [
        { path: "/code/b", why: "uncommitted" },
        { path: "/code/c", why: "serving" },
      ]),
    ).toBe("Removed 0. Kept b: uncommitted; c: serving");
  });
});

describe("summary", () => {
  it("counts what is here and what could go", () => {
    expect(summary(rows(sweep))).toBe("3 worktrees · 1 removable");
    expect(summary(rows(sweep, true))).toBe("3 worktrees · 2 removable");
    expect(summary([])).toBe("No linked worktrees on this machine.");
  });
});
