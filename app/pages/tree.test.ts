import { describe, expect, it } from "vitest";
import {
  age,
  context,
  forceable,
  icon,
  label,
  line,
  listed,
  report,
  tooltip,
  why,
} from "./tree";
import type { Tree } from "./client";

const tree = (over: Partial<Tree> = {}): Tree => ({
  path: "/r/.worktrees/fix",
  name: "fix",
  kind: "linked",
  removable: true,
  ...over,
});

describe("line", () => {
  it("leads with the branch", () => {
    expect(line(tree({ branch: "fix-thing" }))).toBe("fix-thing");
  });

  it("names everything standing in the way, not just the first", () => {
    const got = line(
      tree({ branch: "b", uncommitted: 2, unpushed: 1, removable: false }),
    );
    expect(got).toContain("2 uncommitted");
    expect(got).toContain("1 unpushed");
  });

  it("says a served worktree is in use", () => {
    expect(line(tree({ serving: "http://127.0.0.1:1" }))).toContain("in use");
  });

  it("is empty for a bare worktree with nothing to report", () => {
    expect(line(tree())).toBe("");
  });
});

describe("icon", () => {
  it("marks the repository's own checkout", () => {
    expect(icon(tree({ main: true }))).toBe("repo");
  });

  it("marks one being served before one that could go", () => {
    expect(icon(tree({ serving: "http://x", removable: true }))).toBe(
      "circle-filled",
    );
  });
});

describe("context", () => {
  it("never offers the main checkout for pruning", () => {
    expect(context(tree({ main: true }))).toBe("ade.tree.main");
  });

  it("separates removable from kept", () => {
    expect(context(tree())).toBe("ade.tree.removable");
    expect(context(tree({ removable: false }))).toBe("ade.tree.kept");
  });
});

describe("why", () => {
  it("repeats the server's own reason rather than inventing one", () => {
    expect(
      why(tree({ removable: false, keep: "2 unpushed commits" })),
    ).toContain("2 unpushed commits");
  });

  it("says who is holding it when one is served", () => {
    expect(why(tree({ serving: "http://x" }))).toContain("close that session");
  });
});

describe("forceable", () => {
  // Force lets unpushed work go on purpose. It never lets uncommitted go.
  it("offers to force past unpushed commits", () => {
    expect(forceable(tree({ removable: false, unpushed: 2 }))).toBe(true);
  });

  it("refuses to offer it when there is uncommitted work", () => {
    expect(
      forceable(tree({ removable: false, unpushed: 2, uncommitted: 1 })),
    ).toBe(false);
  });

  it("refuses to offer it for one being served", () => {
    expect(
      forceable(tree({ removable: false, unpushed: 2, serving: "http://x" })),
    ).toBe(false);
  });

  it("is pointless for one that would go anyway", () => {
    expect(forceable(tree({ unpushed: 1 }))).toBe(false);
  });
});

describe("label", () => {
  it("is the branch", () => {
    expect(label(tree({ branch: "fix-thing" }))).toBe("fix-thing");
  });

  it("falls back to the folder for a detached worktree", () => {
    expect(label(tree())).toBe("fix");
  });
});

describe("age", () => {
  const now = Date.parse("2026-10-06T12:00:00Z");
  const ago = (ms: number) => new Date(now - ms).toISOString();

  it("is empty when git said nothing", () => {
    expect(age(undefined, now)).toBe("");
    expect(age("not a date", now)).toBe("");
  });

  it("coarsens as it gets older", () => {
    expect(age(ago(60_000), now)).toBe("just now");
    expect(age(ago(3 * 3600_000), now)).toBe("3 hours ago");
    expect(age(ago(86400_000), now)).toBe("yesterday");
    expect(age(ago(5 * 86400_000), now)).toBe("5 days ago");
    expect(age(ago(90 * 86400_000), now)).toBe("3 months ago");
  });
});

describe("tooltip", () => {
  it("carries what the row has no room for", () => {
    const got = tooltip(
      tree({
        branch: "fix",
        behind: 3,
        merged: true,
        serving: "http://127.0.0.1:1",
      }),
    );
    expect(got).toContain("/r/.worktrees/fix");
    expect(got).toContain("3 behind the base");
    expect(got).toContain("merged into the base");
    expect(got).toContain("served by http://127.0.0.1:1");
  });

  it("says why a kept worktree is kept", () => {
    const got = tooltip(tree({ removable: false, keep: "uncommitted" }));
    expect(got).toContain("uncommitted");
  });

  it("names the main checkout rather than its kind", () => {
    const got = tooltip(tree({ main: true, kind: "linked" }));
    expect(got).toContain("the repository's own checkout");
  });
});

describe("listed", () => {
  const at = (iso: string, over: Partial<Tree> = {}) =>
    tree({ lastUsed: iso, ...over });

  it("puts the main checkout first however old it is", () => {
    const main = at("2020-01-01T00:00:00Z", { path: "/r", name: "r" });
    const got = listed(main, [at("2026-01-01T00:00:00Z")]);
    expect(got[0].path).toBe("/r");
    expect(got[0].main).toBe(true);
  });

  it("orders the rest most recently used first", () => {
    const old = at("2024-01-01T00:00:00Z", { path: "/a", name: "a" });
    const recent = at("2026-01-01T00:00:00Z", { path: "/b", name: "b" });
    expect(listed(undefined, [old, recent]).map((t) => t.path)).toEqual([
      "/b",
      "/a",
    ]);
  });

  // The scan skips the main checkout, but a server that changes its mind
  // must not get it twice.
  it("never lists the main checkout twice", () => {
    const main = tree({ path: "/r", name: "r", main: true });
    expect(listed(main, [main, tree()])).toHaveLength(2);
  });

  it("is just the scan when the switch list could not be read", () => {
    expect(listed(undefined, [tree()])).toHaveLength(1);
  });
});

describe("report", () => {
  it("is a count when everything went", () => {
    expect(report(["/a", "/b"], [])).toBe("Removed 2.");
  });

  it("names the first that would not, and counts the rest", () => {
    const got = report(
      [],
      [
        { path: "/r/.worktrees/a", why: "uncommitted" },
        { path: "/r/.worktrees/b", why: "in use" },
      ],
    );
    expect(got).toContain("a: uncommitted");
    expect(got).toContain("and 1 more");
  });
});
