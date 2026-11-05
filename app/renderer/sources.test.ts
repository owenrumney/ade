import { describe, expect, it } from "vitest";
import { SOURCES } from "./sources";

describe("a picker's list", () => {
  it("offers a space to open, and says what an open one holds", () => {
    const got = {
      offered: [{ repo: "o/a", sessions: 2, branch: "main" }, { repo: "o/b" }],
      spaces: [{ repo: "o/c", here: true }, { repo: "o/d", here: false }],
    };
    expect(SOURCES.space.rows(got)).toEqual([
      { label: "o/a", note: "2 sessions · main", value: "o/a" },
      { label: "o/b", note: "", value: "o/b" },
    ]);
    expect(SOURCES.openspace.rows(got)).toEqual([
      { label: "o/c", note: "", value: "o/c" },
      { label: "o/d", note: "not cloned here", value: "o/d" },
    ]);
  });

  // A branch could otherwise ship the reviewer that reviews it.
  it("leaves out an agent the server will not take as a reviewer", () => {
    const agents = {
      agents: [
        { name: "security", description: "trust boundaries", reviews: true, scope: "user" },
        { name: "writer", reviews: false, scope: "user" },
        { name: "planted", reviews: true, scope: "project" },
      ],
    };
    expect(SOURCES.reviewer.rows(agents)).toEqual([
      { label: "@security", note: "trust boundaries", value: "security" },
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
