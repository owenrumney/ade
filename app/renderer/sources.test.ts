import { describe, expect, it } from "vitest";
import { SOURCES } from "./sources";

describe("a picker's list", () => {
  it("reads ade's own answer", () => {
    expect(SOURCES.model.rows({ models: [{ id: "opus", label: "Opus" }] })).toEqual([
      { label: "Opus", value: "opus" },
    ]);
    expect(SOURCES.role.rows({ roles: [{ name: "oss", about: "maintainer" }] })).toEqual([
      { label: "oss", note: "maintainer", value: "oss" },
    ]);
  });

  it("leaves out what the verb cannot act on", () => {
    const trees = {
      here: "/a",
      worktrees: [{ name: "a", path: "/a" }, { name: "b", path: "/b", branch: "fix" }],
    };
    expect(SOURCES.worktree.rows(trees)).toEqual([{ label: "b", note: "fix", value: "/b" }]);

    const threads = {
      threads: [
        { id: "1", file: "x.go", line: 4, comments: [{ body: "this\nand more" }] },
        { id: "2", file: "y.go", line: 1, resolved: true, comments: [] },
      ],
    };
    expect(SOURCES.thread.rows(threads)).toEqual([
      { label: "x.go:4", note: "this", value: "1" },
    ]);
  });

  it("offers anyone before a person, and nobody when the bus is off", () => {
    const who = {
      online: true,
      instances: [
        { user: "b@x", presence: "idle", instance: { harness: "claude" } },
        { user: "a@x", presence: "active", instance: {}, status: "reviewing" },
        { user: "a@x", presence: "dnd", instance: {} },
      ],
    };
    const rows = SOURCES.person.rows(who);
    expect(rows.map((r) => r.label)).toEqual(["Anyone", "a@x", "b@x"]);
    expect(rows[1].note).toBe("active · reviewing");
    expect(SOURCES.person.rows({ online: false })).toEqual([]);
  });

  it("names a bus row by who it came from", () => {
    const state = {
      bus: {
        replies: [{ id: "r1", from: "a@x", text: "yes" }],
        offers: [{ id: "o1", kind: "review", from: "b@x", pr: "o/r#1" }],
      },
    };
    expect(SOURCES.reply.rows(state)).toEqual([{ label: "a@x", note: "yes", value: "r1" }]);
    expect(SOURCES.offer.rows(state)).toEqual([
      { label: "review from b@x", note: "o/r#1", value: "o1" },
    ]);
  });

  it("holds up when ade answers with nothing", () => {
    for (const [name, source] of Object.entries(SOURCES)) {
      expect(source.rows({}), name).toEqual([]);
      expect(source.path, name).toMatch(/^\/api\//);
    }
  });
});
