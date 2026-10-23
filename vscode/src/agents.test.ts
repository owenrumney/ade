import { describe, expect, it } from "vitest";
import {
  type Active,
  attention,
  detail,
  icon,
  same,
  stateOf,
  tree,
  waiting,
} from "./agents";

const ACTIVE: Active[] = [
  { id: "quiet", dir: "/r/a", name: "a", repo: "alpha", agent: { running: false } },
  {
    id: "busy",
    dir: "/r/b",
    name: "b",
    repo: "beta",
    branch: "feat/x",
    agent: {
      running: true,
      model: "opus",
      sideRuns: ["security"],
      children: [{ kind: "agent", name: "explore", doing: "reading web.go" }],
    },
  },
  {
    id: "stuck",
    dir: "/r/c",
    name: "c",
    repo: "gamma",
    machine: "mini",
    agent: { running: true, asking: 1 },
  },
];

describe("what a server's agent is doing", () => {
  it("ranks the same five states as internal/machine, in the same order", () => {
    expect(stateOf(undefined)).toBe("idle");
    expect(stateOf({ error: "boom", running: true, asking: 1 })).toBe("failed");
    expect(stateOf({ queued: 2 })).toBe("wedged");
    expect(stateOf({ running: true, asking: 1 })).toBe("asking");
    expect(stateOf({ running: true, queued: 2 })).toBe("working");
    expect(stateOf({ running: true })).toBe("working");
  });

  it("calls a turn yours only when it is, so a long run stays quiet", () => {
    expect(attention("failed")).toBe(true);
    expect(attention("wedged")).toBe(true);
    expect(attention("asking")).toBe(true);
    expect(attention("working")).toBe(false);
    expect(attention("idle")).toBe(false);
  });
});

describe("the agents tree", () => {
  it("puts what wants you first and what it called under it", () => {
    const built = tree(ACTIVE);
    expect(built.map((b) => b.row.name)).toEqual(["gamma", "beta", "alpha"]);
    expect(built[1].children.map((c) => c.name)).toEqual([
      "@security",
      "explore",
    ]);
    expect(built[2].children).toEqual([]);
  });

  it("keeps a child's session on it, so its row opens that conversation", () => {
    const child = tree(ACTIVE)[1].children[1];
    expect(child.session).toBe("busy");
    expect(child.dir).toBe("/r/b");
    expect(child.id).toBe("busy:child:explore");
  });

  it("counts what wants an answer, not what is running", () => {
    expect(waiting(ACTIVE)).toBe(1);
    expect(waiting(ACTIVE.filter((a) => a.id !== "stuck"))).toBe(0);
  });

  it("leads each row with its state, and a child with what it is", () => {
    const built = tree(ACTIVE);
    expect(detail(built[1].row)).toBe("working · opus · feat/x");
    expect(detail(built[1].children[1])).toBe("sub-agent · reading web.go");
    expect(detail(built[0].row)).toBe("asking");
  });

  it("draws a command as a terminal and a question as a question", () => {
    const built = tree([
      {
        id: "x",
        dir: "/r/x",
        name: "x",
        agent: {
          running: true,
          asking: 1,
          children: [{ kind: "task", name: "go test ./..." }],
        },
      },
    ]);
    expect(icon(built[0].row)).toBe("question");
    expect(icon(built[0].children[0])).toBe("terminal");
  });

  it("redraws when a state moves and not when the same reading comes back", () => {
    expect(same(tree(ACTIVE), tree(ACTIVE))).toBe(true);
    const settled = ACTIVE.map((a) =>
      a.id === "stuck" ? { ...a, agent: { running: true } } : a,
    );
    expect(same(tree(ACTIVE), tree(settled))).toBe(false);
  });
});

describe("a side run and a sub-agent are not the same row", () => {
  it("says which is which, because only one was asked for by name", () => {
    const built = tree([
      {
        id: "x",
        dir: "/r/x",
        name: "x",
        agent: {
          running: true,
          sideRuns: ["security"],
          children: [{ kind: "agent", name: "explore" }],
        },
      },
    ]);
    expect(detail(built[0].children[0])).toBe("running beside");
    expect(detail(built[0].children[1])).toBe("sub-agent");
  });
});
