import { describe, expect, it } from "vitest";
import type { Overlap, OverlapNode, Rev } from "../client";
import {
  blocker,
  clearable,
  KIND_ABSORBED,
  KIND_DUPLICATES,
  KIND_SHARES,
  type Lane,
  lanes,
  leaves,
  marked,
  mergeable,
  mergeSaid,
  plan,
  said,
  short,
  stuckSaid,
  summary,
  ties,
  TRUNK,
  wanted,
  where,
} from "./view";

const rev = (sha: string, patchId = sha): Rev => ({
  sha,
  subject: `work ${sha}`,
  author: "t",
  at: "2026-10-01T00:00:00Z",
  patchId,
});

const node = (over: Partial<OverlapNode> = {}): OverlapNode => ({
  path: `/r/${over.name ?? "one"}`,
  name: "one",
  kind: "session",
  removable: true,
  commits: [],
  redundant: false,
  ...over,
});

// A worktree, named once: the path, the name and the branch all follow it.
const wt = (name: string, over: Partial<OverlapNode> = {}): OverlapNode =>
  node({ path: `/r/${name}`, name, branch: name, ...over });

const graph = (over: Partial<Overlap> = {}): Overlap => ({
  base: "a".repeat(40),
  baseRef: "origin/main",
  baseBranch: "main",
  nodes: [],
  edges: [],
  removable: [],
  ...over,
});

const at = (rows: Lane[], label: string): Lane =>
  rows.find((row) => row.label === label)!;

describe("lanes", () => {
  it("puts the repository's own checkout first however old it is", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("new", { lastUsed: "2026-10-05T00:00:00Z" }),
          wt("r", { main: true, lastUsed: "2020-01-01T00:00:00Z" }),
        ],
      }),
    );
    expect(got.map((l) => l.label)).toEqual(["r", "new"]);
  });

  it("orders worktrees off the base most recently used first", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("old", { lastUsed: "2024-01-01T00:00:00Z" }),
          wt("new", { lastUsed: "2026-01-01T00:00:00Z" }),
        ],
      }),
    );
    expect(got.map((l) => l.label)).toEqual(["new", "old"]);
  });

  it("labels a lane by its branch and falls back to the folder", () => {
    const got = lanes(
      graph({ nodes: [wt("fix-thing"), node({ path: "/r/two", name: "two" })] }),
    );
    expect(got.map((l) => l.label).sort()).toEqual(["fix-thing", "two"]);
  });

  it("carries the scan's own state line without repeating the branch", () => {
    const got = lanes(graph({ nodes: [wt("b", { uncommitted: 2, behind: 3 })] }));
    expect(got[0].state).toBe("2 uncommitted · 3 behind");
  });

  // Time runs down the drawing, from the base at the top to the tips, so a
  // newest-first list would run against its own rail.
  it("turns the commits oldest first", () => {
    const got = lanes(
      graph({ nodes: [wt("b", { commits: [rev("new"), rev("old")] })] }),
    );
    expect(got[0].commits.map((c) => c.sha)).toEqual(["old", "new"]);
  });

  it("says a worktree holds nothing of its own", () => {
    const got = lanes(
      graph({ nodes: [wt("one", { redundant: true })], removable: ["/r/one"] }),
    );
    expect(got[0].verdict).toBe("holds nothing of its own");
    expect(got[0].removable).toBe(true);
  });

  it("names the worktree an absorbed one's work is on", () => {
    const got = lanes(
      graph({
        nodes: [wt("behind"), wt("ahead", { commits: [rev("1")] })],
        edges: [{ from: "/r/behind", to: "/r/ahead", kind: KIND_ABSORBED }],
        removable: ["/r/behind"],
      }),
    );
    expect(at(got, "behind").verdict).toBe("all of its work is on ahead");
  });

  it("never offers a verdict on the main checkout", () => {
    const got = lanes(graph({ nodes: [wt("r", { main: true, redundant: true })] }));
    expect(got[0].verdict).toBe("");
  });

  it("says so when git could not be read, rather than calling it empty", () => {
    const got = lanes(
      graph({ nodes: [wt("one", { why: "not a git repository", redundant: true })] }),
    );
    expect(got[0].verdict).toBe("could not be read");
  });

  it("is empty with no graph at all", () => {
    expect(lanes(undefined)).toEqual([]);
  });
});

describe("the rails", () => {
  it("leaves every worktree on the base when none share a commit", () => {
    const got = lanes(graph({ nodes: [wt("a", { commits: [rev("1")] }), wt("b", { commits: [rev("2")] })] }));
    for (const row of got) expect(row.from).toBe(TRUNK);
  });

  it("puts the main checkout on the trunk and nothing else", () => {
    const got = lanes(graph({ nodes: [wt("r", { main: true }), wt("a", { commits: [rev("1")] })] }));
    expect(at(got, "r").column).toBe(TRUNK);
    expect(at(got, "a").column).not.toBe(TRUNK);
  });

  // The whole point of rails over arcs: a branch started from another leaves
  // that one's line, and the relation needs no second mark.
  it("leaves a branch that shares commits on the longer one's rail", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("long", { commits: [rev("1"), rev("2"), rev("3")] }),
          wt("short", { commits: [rev("1"), rev("9")] }),
        ],
        edges: [{ from: "/r/short", to: "/r/long", kind: KIND_SHARES, count: 1 }],
      }),
    );
    expect(at(got, "short").from).toBe(got.indexOf(at(got, "long")));
    expect(at(got, "short").shared).toBe(1);
  });

  it("draws a branch under the one it left, whatever the recency says", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("child", { lastUsed: "2026-10-05T00:00:00Z", commits: [rev("1"), rev("9")] }),
          wt("parent", { lastUsed: "2020-01-01T00:00:00Z", commits: [rev("1"), rev("2"), rev("3")] }),
        ],
        edges: [{ from: "/r/child", to: "/r/parent", kind: KIND_SHARES, count: 1 }],
      }),
    );
    expect(got.map((l) => l.label)).toEqual(["parent", "child"]);
  });

  it("puts an absorbed worktree on the rail that carries its work", () => {
    const got = lanes(
      graph({
        nodes: [wt("gone", { commits: [rev("1")] }), wt("holder", { commits: [rev("1"), rev("2")] })],
        edges: [
          { from: "/r/gone", to: "/r/holder", kind: KIND_ABSORBED },
          { from: "/r/gone", to: "/r/holder", kind: KIND_SHARES, count: 1 },
        ],
      }),
    );
    expect(at(got, "gone").from).toBe(got.indexOf(at(got, "holder")));
  });

  // Two branches each sharing with the other would otherwise each be the
  // other's parent, and the walk would never reach either.
  it("never makes a pair each other's parent", () => {
    const got = lanes(
      graph({
        nodes: [wt("a", { commits: [rev("1"), rev("2")] }), wt("b", { commits: [rev("1"), rev("3")] })],
        edges: [{ from: "/r/a", to: "/r/b", kind: KIND_SHARES, count: 1 }],
      }),
    );
    expect(got).toHaveLength(2);
    const parents = got.map((l) => l.from).filter((from) => from !== TRUNK);
    expect(parents).toHaveLength(1);
  });

  it("gives two rails alive over the same rows separate columns", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("a", { lastUsed: "2026-03-01T00:00:00Z", commits: [rev("1")] }),
          wt("b", { lastUsed: "2026-02-01T00:00:00Z", commits: [rev("2")] }),
          wt("c", { lastUsed: "2026-01-01T00:00:00Z", commits: [rev("3")] }),
        ],
      }),
    );
    expect(got.map((l) => l.column)).toEqual([0, 1, 2]);
  });

  it("reuses a column once the rail above it has ended", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("a", { lastUsed: "2026-03-01T00:00:00Z", commits: [rev("1"), rev("2")] }),
          wt("b", { lastUsed: "2026-02-01T00:00:00Z", commits: [rev("1"), rev("3")] }),
        ],
        edges: [{ from: "/r/b", to: "/r/a", kind: KIND_SHARES, count: 1 }],
      }),
    );
    // b leaves a's rail at a's own row, so the two never run side by side.
    expect(at(got, "a").column).toBe(0);
    expect(at(got, "b").column).toBe(0);
  });
});

describe("leaves", () => {
  const forked = () =>
    lanes(
      graph({
        nodes: [
          wt("parent", { commits: [rev("1"), rev("2"), rev("3")] }),
          wt("child", { commits: [rev("1"), rev("9")] }),
        ],
        edges: [{ from: "/r/child", to: "/r/parent", kind: KIND_SHARES, count: 1 }],
      }),
    );

  it("says in words which rail a branch left, and how far along", () => {
    const rows = forked();
    expect(leaves(at(rows, "child"), rows)).toBe("from parent, after 1 commit");
  });

  it("says nothing for a branch straight off the base", () => {
    const rows = lanes(graph({ nodes: [wt("a", { commits: [rev("1")] })] }));
    expect(leaves(rows[0], rows)).toBe("");
  });

  it("says nothing for the checkout itself", () => {
    const rows = lanes(graph({ nodes: [wt("r", { main: true })] }));
    expect(leaves(rows[0], rows)).toBe("");
  });
});

describe("ties", () => {
  const two = () =>
    lanes(
      graph({
        nodes: [
          wt("a", { lastUsed: "2026-02-01T00:00:00Z", commits: [rev("1")] }),
          wt("b", { lastUsed: "2026-01-01T00:00:00Z", commits: [rev("2")] }),
        ],
      }),
    );

  // Only the duplicate earns a line of its own: the rails say the rest.
  it("is drawn for a copied patch and for nothing else", () => {
    const rows = two();
    const got = ties(
      graph({
        edges: [
          { from: "/r/a", to: "/r/b", kind: KIND_DUPLICATES, count: 2 },
          { from: "/r/a", to: "/r/b", kind: KIND_SHARES, count: 1 },
          { from: "/r/a", to: "/r/b", kind: KIND_ABSORBED },
        ],
      }),
      rows,
    );
    expect(got).toHaveLength(1);
    expect(got[0].label).toBe("2 copied");
    expect(got[0].from).toBe(0);
    expect(got[0].to).toBe(1);
  });

  it("says one copy in the singular", () => {
    const got = ties(
      graph({ edges: [{ from: "/r/a", to: "/r/b", kind: KIND_DUPLICATES, count: 1 }] }),
      two(),
    );
    expect(got[0].label).toBe("1 copied");
  });

  it("drops a tie naming a worktree with no lane", () => {
    const got = ties(
      graph({ edges: [{ from: "/r/a", to: "/r/gone", kind: KIND_DUPLICATES, count: 1 }] }),
      two(),
    );
    expect(got).toEqual([]);
  });

  it("gives two ties over the same rows separate columns", () => {
    const rows = lanes(
      graph({
        nodes: [
          wt("a", { lastUsed: "2026-04-01T00:00:00Z", commits: [rev("1")] }),
          wt("b", { lastUsed: "2026-03-01T00:00:00Z", commits: [rev("2")] }),
          wt("c", { lastUsed: "2026-02-01T00:00:00Z", commits: [rev("3")] }),
          wt("d", { lastUsed: "2026-01-01T00:00:00Z", commits: [rev("4")] }),
        ],
      }),
    );
    const got = ties(
      graph({
        edges: [
          { from: "/r/a", to: "/r/d", kind: KIND_DUPLICATES, count: 1 },
          { from: "/r/b", to: "/r/c", kind: KIND_DUPLICATES, count: 1 },
        ],
      }),
      rows,
    );
    expect(got.map((t) => t.column)).toEqual([0, 1]);
  });
});

describe("summary", () => {
  it("says there is nothing to manage when there is only the checkout", () => {
    expect(summary(graph({ nodes: [wt("r", { main: true })] }))).toBe(
      "No worktrees besides the repository itself.",
    );
  });

  it("counts the worktrees, what could go and what is duplicated", () => {
    const got = summary(
      graph({
        nodes: [wt("a"), wt("b"), wt("r", { main: true })],
        edges: [{ from: "/r/a", to: "/r/b", kind: KIND_DUPLICATES, count: 2 }],
        removable: ["/r/a"],
      }),
    );
    expect(got).toBe("2 worktrees · 1 could go · 2 patches in two places");
  });

  it("leaves out what there is none of", () => {
    expect(summary(graph({ nodes: [wt("a")] }))).toBe("1 worktree");
  });
});

describe("where", () => {
  it("names the base by the ref that resolved it", () => {
    expect(where(graph())).toBe("origin/main");
  });

  it("falls back to the branch with no remote, then to words", () => {
    expect(where(graph({ baseRef: "" }))).toBe("main");
    expect(where(graph({ baseRef: "", baseBranch: "" }))).toBe("the base");
    expect(where(undefined)).toBe("the base");
  });
});

describe("marked", () => {
  const rows = () =>
    lanes(
      graph({
        nodes: [
          wt("a", { commits: [rev("1"), rev("2")] }),
          wt("b", { commits: [rev("9", "1")] }),
        ],
      }),
    );

  it("says nothing about a commit only this worktree has", () => {
    expect(marked(rev("2"), rows(), "/r/a")).toBe("");
  });

  it("names the worktree a copy of the patch is on", () => {
    expect(marked(rev("1"), rows(), "/r/a")).toBe("copied on b");
  });

  it("calls the same SHA on two branches shared, not copied", () => {
    const got = lanes(
      graph({ nodes: [wt("a", { commits: [rev("1")] }), wt("b", { commits: [rev("1")] })] }),
    );
    expect(marked(rev("1"), got, "/r/a")).toBe("also on b");
  });

  it("does not match a merge to another merge", () => {
    const got = lanes(
      graph({
        nodes: [wt("a", { commits: [rev("1", "")] }), wt("b", { commits: [rev("2", "")] })],
      }),
    );
    expect(marked(rev("1", ""), got, "/r/a")).toBe("");
  });
});

describe("short", () => {
  it("is the seven characters a commit list shows", () => {
    expect(short("abcdef1234567890")).toBe("abcdef1");
  });
});

describe("a worktree with nothing of its own", () => {
  // Its tip is the base. Giving it a rail would draw the longest line on the
  // page for the branch with the least on it.
  it("is a dot on the trunk and not a rail", () => {
    const got = lanes(
      graph({
        nodes: [wt("spare", { redundant: true }), wt("real", { commits: [rev("1")] })],
        removable: ["/r/spare"],
      }),
    );
    expect(at(got, "spare").column).toBe(TRUNK);
    expect(at(got, "real").column).not.toBe(TRUNK);
  });

  it("still gets a rail when it forked off another branch", () => {
    const got = lanes(
      graph({
        nodes: [wt("holder", { commits: [rev("1")] }), wt("gone", { commits: [rev("9", "1")] })],
        edges: [{ from: "/r/gone", to: "/r/holder", kind: KIND_ABSORBED }],
      }),
    );
    expect(at(got, "gone").column).not.toBe(TRUNK);
  });
});

describe("how far along a rail a branch left", () => {
  // Absorbed says every patch is on the other rail; it does not say how many
  // commits they ran together, which is what the words claim.
  it("counts the shared commits and never the branch's own", () => {
    const got = lanes(
      graph({
        nodes: [
          wt("holder", { commits: [rev("1"), rev("2"), rev("3")] }),
          wt("gone", { commits: [rev("1"), rev("9", "2")] }),
        ],
        edges: [
          { from: "/r/gone", to: "/r/holder", kind: KIND_ABSORBED },
          { from: "/r/gone", to: "/r/holder", kind: KIND_SHARES, count: 1 },
        ],
      }),
    );
    expect(at(got, "gone").shared).toBe(1);
    expect(leaves(at(got, "gone"), got)).toBe("from holder, after 1 commit");
  });
});

describe("blocker", () => {
  const one = (over: Partial<OverlapNode>) =>
    lanes(graph({ nodes: [wt("one", { commits: [rev("1")], ...over })] }))[0];

  it("is nothing for a clean worktree", () => {
    expect(blocker(one({}))).toBeUndefined();
  });

  it("offers to close a served worktree with nothing uncommitted", () => {
    const got = blocker(one({ serving: "http://x", unpushed: 2 }));
    expect(got?.clear).toBe("close");
    expect(got?.stuck).toBe(false);
  });

  // Closing the session would not make it removable: the prune behind it
  // refuses uncommitted work whatever else was cleared first.
  it("calls a served worktree stuck when it is also dirty", () => {
    const got = blocker(one({ serving: "http://x", uncommitted: 1 }));
    expect(got?.stuck).toBe(true);
    expect(got?.clear).toBeUndefined();
  });

  // Uncommitted work exists nowhere else. A bulk button that deleted it
  // could not be undone, so the panel refuses to offer one.
  it("calls uncommitted work stuck and offers no verb for it", () => {
    const got = blocker(one({ uncommitted: 3 }));
    expect(got?.stuck).toBe(true);
    expect(got?.clear).toBeUndefined();
  });

  it("offers a push for unpushed commits", () => {
    expect(blocker(one({ unpushed: 2 }))?.clear).toBe("push");
  });
});

describe("clearable", () => {
  const rows = (over: Partial<OverlapNode>) =>
    lanes(graph({ nodes: [wt("one", over)] }))[0];

  it("is false for the repository's own checkout", () => {
    expect(clearable(rows({ main: true }))).toBe(false);
  });

  it("is false for one git could not read", () => {
    expect(clearable(rows({ why: "not a git repository" }))).toBe(false);
  });

  it("is false while it holds uncommitted work", () => {
    expect(clearable(rows({ uncommitted: 1 }))).toBe(false);
  });

  it("is true for one whose only blocker the panel can clear", () => {
    expect(clearable(rows({ unpushed: 2 }))).toBe(true);
    expect(clearable(rows({ serving: "http://x" }))).toBe(true);
  });
});

describe("wanted", () => {
  it("ticks what the commits prove is spare and nothing else", () => {
    const rows = lanes(
      graph({
        nodes: [wt("spare", { redundant: true }), wt("busy", { commits: [rev("1")] })],
        removable: ["/r/spare"],
      }),
    );
    expect(wanted(rows)).toEqual(["/r/spare"]);
  });

  it("leaves out a spare one whose work only it holds", () => {
    const rows = lanes(
      graph({
        nodes: [wt("spare", { redundant: true, uncommitted: 2 })],
        removable: ["/r/spare"],
      }),
    );
    expect(wanted(rows)).toEqual([]);
  });
});

describe("plan", () => {
  const three = () =>
    lanes(
      graph({
        nodes: [
          wt("clean", { lastUsed: "2026-03-01T00:00:00Z", commits: [rev("1")] }),
          wt("unpushed", { lastUsed: "2026-02-01T00:00:00Z", unpushed: 2, commits: [rev("2")] }),
          wt("serving", { lastUsed: "2026-01-01T00:00:00Z", serving: "http://x", commits: [rev("3")] }),
        ],
      }),
    );

  it("clears every blocker before it removes anything", () => {
    const rows = three();
    const steps = plan(rows, rows.map((r) => r.node.path));
    expect(steps.map((s) => s.what)).toEqual(["push", "close", "prune"]);
  });

  it("is one prune for all of them, not one each", () => {
    const rows = three();
    const steps = plan(rows, rows.map((r) => r.node.path));
    expect(steps.filter((s) => s.what === "prune")).toHaveLength(1);
  });

  it("is empty when nothing is chosen", () => {
    expect(plan(three(), [])).toEqual([]);
  });

  it("drops a chosen row the panel may not touch", () => {
    const rows = lanes(graph({ nodes: [wt("stuck", { uncommitted: 1, commits: [rev("1")] })] }));
    expect(plan(rows, ["/r/stuck"])).toEqual([]);
  });
});

describe("said and stuckSaid", () => {
  it("counts what the button will remove", () => {
    const rows = lanes(
      graph({ nodes: [wt("a", { commits: [rev("1")] }), wt("b", { commits: [rev("2")] })] }),
    );
    expect(said(rows, [])).toBe("Nothing selected");
    expect(said(rows, ["/r/a"])).toBe("Remove 1");
    expect(said(rows, ["/r/a", "/r/b"])).toBe("Remove 2");
  });

  // A row that could go but was not ticked for you needs saying, or it looks
  // like the panel simply missed it.
  it("explains a spare worktree it would not tick", () => {
    const rows = lanes(
      graph({
        nodes: [wt("spare", { redundant: true, uncommitted: 2 })],
        removable: ["/r/spare"],
      }),
    );
    expect(stuckSaid(rows)).toContain("spare could go");
    expect(stuckSaid(rows)).toContain("work only it holds");
  });

  it("says nothing when there is nothing stuck", () => {
    const rows = lanes(graph({ nodes: [wt("a", { commits: [rev("1")] })] }));
    expect(stuckSaid(rows)).toBe("");
  });
});

describe("mergeable", () => {
  // A branch stacked on another: the only shape with somewhere for the work
  // to go that the rails already know about.
  const stacked = (over: Partial<OverlapNode> = {}, into: Partial<OverlapNode> = {}) =>
    lanes(
      graph({
        nodes: [
          wt("parent", { commits: [rev("1"), rev("2"), rev("3")], ...into }),
          wt("child", { commits: [rev("1"), rev("9")], ...over }),
        ],
        edges: [{ from: "/r/child", to: "/r/parent", kind: KIND_SHARES, count: 1 }],
      }),
    );

  it("is the rail a stacked branch left", () => {
    const rows = stacked();
    expect(mergeable(at(rows, "child"), rows)?.label).toBe("parent");
  });

  // The common case: every rail leaves one, and the trunk's is the checkout.
  it("is the repository's own checkout for a branch off the base", () => {
    const rows = lanes(
      graph({
        nodes: [wt("r", { main: true }), wt("a", { commits: [rev("1")] })],
      }),
    );
    expect(mergeable(at(rows, "a"), rows)?.label).toBe("r");
  });

  it("is nothing off the base when there is no checkout to merge into", () => {
    const rows = lanes(graph({ nodes: [wt("a", { commits: [rev("1")] })] }));
    expect(mergeable(rows[0], rows)).toBeUndefined();
  });

  it("is nothing off the base when the checkout is dirty", () => {
    const rows = lanes(
      graph({
        nodes: [wt("r", { main: true, uncommitted: 2 }), wt("a", { commits: [rev("1")] })],
      }),
    );
    expect(mergeable(at(rows, "a"), rows)).toBeUndefined();
  });

  it("is nothing for the parent itself", () => {
    const rows = stacked();
    expect(mergeable(at(rows, "parent"), rows)).toBeUndefined();
  });

  // Already absorbed means the work is on the parent; there is nothing left
  // to send, and Remove is the verb.
  it("is nothing for a branch whose work is already there", () => {
    const rows = lanes(
      graph({
        nodes: [wt("parent", { commits: [rev("1")] }), wt("child", { commits: [rev("1")] })],
        edges: [
          { from: "/r/child", to: "/r/parent", kind: KIND_ABSORBED },
          { from: "/r/child", to: "/r/parent", kind: KIND_SHARES, count: 1 },
        ],
        removable: ["/r/child"],
      }),
    );
    expect(mergeable(at(rows, "child"), rows)).toBeUndefined();
  });

  it("is nothing while either side holds uncommitted work", () => {
    expect(mergeable(at(stacked({ uncommitted: 1 }), "child"), stacked({ uncommitted: 1 })))
      .toBeUndefined();
    const dirtyParent = stacked({}, { uncommitted: 2 });
    expect(mergeable(at(dirtyParent, "child"), dirtyParent)).toBeUndefined();
  });

  it("is nothing for one git could not read", () => {
    const rows = stacked({ why: "not a git repository" });
    expect(mergeable(at(rows, "child"), rows)).toBeUndefined();
  });

  it("names where the work goes, because the button writes to it", () => {
    const rows = stacked();
    expect(mergeSaid(at(rows, "parent"))).toBe("Merge into parent and remove");
  });
});

describe("a branch the forge squashed or rebased", () => {
  const squashed = () =>
    lanes(
      graph({
        nodes: [wt("done", { commits: [rev("1"), rev("2")], landed: true })],
        removable: ["/r/done"],
      }),
    );

  // Its commits are its own and its work is not. The commit count beside the
  // verdict says the opposite, so the wording has to be its own.
  it("says it is already on the base rather than that it holds nothing", () => {
    expect(squashed()[0].verdict).toBe("already on origin/main, squashed or rebased");
  });

  it("is ticked for removal like any other spare worktree", () => {
    const rows = squashed();
    expect(rows[0].removable).toBe(true);
    expect(wanted(rows)).toEqual(["/r/done"]);
  });

  // There is nothing left to send: Remove is the verb, not Merge.
  it("is not offered a merge", () => {
    const rows = lanes(
      graph({
        nodes: [
          wt("r", { main: true }),
          wt("done", { commits: [rev("1")], landed: true }),
        ],
        removable: ["/r/done"],
      }),
    );
    expect(mergeable(at(rows, "done"), rows)).toBeUndefined();
  });

  it("counts towards what could go", () => {
    expect(summary(graph({
      nodes: [wt("done", { commits: [rev("1")], landed: true })],
      removable: ["/r/done"],
    }))).toBe("1 worktree · 1 could go");
  });
});
