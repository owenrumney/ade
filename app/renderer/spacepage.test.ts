import { describe, expect, it } from "vitest";
import { drawnFrom, sessionsOf } from "./spacepage";
import type { Repo } from "./real";

const now = Date.parse("2026-10-23T12:00:00Z");

const repo: Repo = {
  name: "ade",
  owner: "owenrumney",
  path: "/code/ade",
  checkouts: [
    {
      path: "/code/ade",
      name: "ade",
      branch: "main",
      sessions: [
        { id: "old", name: "older", dir: "/code/ade", lastUsed: "2026-10-20T12:00:00Z" },
      ],
    },
    {
      path: "/code/ade-fix-7",
      name: "ade-fix-7",
      branch: "fix-7",
      worktree: true,
      sessions: [
        {
          id: "new",
          name: "newer",
          title: "The comment side",
          dir: "/code/ade-fix-7",
          live: true,
          lastUsed: "2026-10-23T11:00:00Z",
        },
      ],
    },
  ],
};

describe("sessionsOf", () => {
  it("lists every checkout's conversations, newest first", () => {
    const lines = sessionsOf(repo, now);
    expect(lines.map((l) => l.id)).toEqual(["new", "old"]);
    expect(lines[0].name).toBe("The comment side");
  });

  it("says where a session is and how long it has been", () => {
    const [live, older] = sessionsOf(repo, now);
    expect(live.meta).toBe("fix-7 · ade-fix-7 · live");
    expect(older.meta).toBe("main · main checkout · 3d");
  });

  it("has nothing to say about a repository it has no reading of", () => {
    expect(sessionsOf(undefined, now)).toEqual([]);
  });

  // The list this window holds still names a session it has just closed.
  it("leaves out what this window has just closed", () => {
    expect(sessionsOf(repo, now, new Set(["new"])).map((l) => l.id)).toEqual(["old"]);
  });

  it("says which agent a session runs, when the record names one", () => {
    const withAgent: Repo = {
      ...repo,
      checkouts: [
        {
          ...repo.checkouts![0],
          sessions: [{ id: "c", name: "c", dir: "/code/ade", agent: "codex", lastUsed: "2026-10-23T11:00:00Z" }],
        },
      ],
    };
    expect(sessionsOf(withAgent, now)[0].meta).toBe("main · main checkout · codex · 1h");
  });
});

// The page is built once and kept, so what it was built from has to say when
// it is stale: a session that stops is a line that has to change.
describe("when a drawn space page is stale", () => {
  const seen = {
    space: { repo: "owenrumney/ade", sessions: 1, here: true },
    sessions: [{ id: "s1", name: "one", meta: "main · live", live: true }],
  };

  it("changes when a session is closed", () => {
    expect(drawnFrom({ ...seen, sessions: [] })).not.toBe(drawnFrom(seen));
  });

  it("changes when a session is renamed", () => {
    const renamed = { ...seen, sessions: [{ ...seen.sessions[0], name: "two" }] };
    expect(drawnFrom(renamed)).not.toBe(drawnFrom(seen));
  });

  it("changes when a session stops, and not when the space says more about itself", () => {
    const stopped = { ...seen, sessions: [{ id: "s1", name: "one", meta: "main · 2m" }] };
    expect(drawnFrom(stopped)).not.toBe(drawnFrom(seen));
    expect(drawnFrom({ ...seen, space: { ...seen.space, branch: "main" } })).toBe(drawnFrom(seen));
  });
});
