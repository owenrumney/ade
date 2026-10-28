import { describe, expect, it } from "vitest";
import { designsOf, drawnFrom, sessionsOf } from "./spacepage";
import type { Repo } from "./real";
import type { Space } from "./spaces";

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
        { id: "gone", name: "put away", dir: "/code/ade", archived: true },
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

  // Archived is what putting a session away means. One in the list is one
  // you archived and were given back.
  it("leaves out the archived", () => {
    expect(sessionsOf(repo, now).some((l) => l.id === "gone")).toBe(false);
  });

  it("says where a session is and how long it has been", () => {
    const [live, older] = sessionsOf(repo, now);
    expect(live.meta).toBe("fix-7 · ade-fix-7 · live");
    expect(older.meta).toBe("main · main checkout · 3d");
  });

  it("has nothing to say about a repository it has no reading of", () => {
    expect(sessionsOf(undefined, now)).toEqual([]);
  });

  // The launcher derives sessions from transcripts, so an archived row is
  // still in the list this window holds.
  it("leaves out what this window has just archived", () => {
    expect(sessionsOf(repo, now, new Set(["new"])).map((l) => l.id)).toEqual(["old"]);
  });

  // An adopted session is listed under the checkout that took it, which is the
  // one the launcher put it in. Where it was recorded is the surprise.
  it("says where an adopted session came from", () => {
    const adopted: Repo = {
      ...repo,
      checkouts: [
        {
          ...repo.checkouts![0],
          sessions: [
            {
              id: "taken",
              name: "taken",
              dir: "/code/ade",
              pinned: true,
              from: "/code/somewhere-else",
              lastUsed: "2026-10-23T11:00:00Z",
            },
          ],
        },
      ],
    };
    const [line] = sessionsOf(adopted, now);
    expect(line.pinned).toBe(true);
    expect(line.meta).toBe("main · main checkout · from /code/somewhere-else · 1h");
  });

  it("says nothing extra about one listed where it was recorded", () => {
    expect(sessionsOf(repo, now).every((l) => !l.meta.includes("from "))).toBe(true);
  });
});

describe("designsOf", () => {
  // The id is the designs list's own, so a document opens the same way
  // from either place; the file alone is what the archive takes.
  it("carries the checkout in the id and the path on its own", () => {
    const space: Space = {
      repo: "owenrumney/ade",
      path: "/code/ade",
      sessions: 2,
      here: true,
      designs: ["space-page.md"],
    };
    expect(designsOf(space)).toEqual([
      { file: "space-page.md", id: "/code/ade|space-page.md", name: "space-page" },
    ]);
  });

  it("leaves out what this window has just archived", () => {
    const space: Space = {
      repo: "owenrumney/ade",
      path: "/code/ade",
      sessions: 2,
      here: true,
      designs: ["a.md", "b.md"],
    };
    expect(designsOf(space, new Set(["a.md"])).map((d) => d.file)).toEqual(["b.md"]);
  });
});

// The page is built once and kept, so what it was built from has to say when
// it is stale: archiving a design takes a minute on the server, and the row
// has to go when it lands rather than when the page is next opened.
describe("when a drawn space page is stale", () => {
  const seen = {
    space: { repo: "owenrumney/ade", sessions: 1, here: true, designs: ["a.md", "b.md"] },
    sessions: [{ id: "s1", name: "one", meta: "main · live", live: true }],
    designs: [
      { file: "a.md", id: "/code/ade|a.md", name: "a" },
      { file: "b.md", id: "/code/ade|b.md", name: "b" },
    ],
  };

  // The key follows the list drawn, not the space's own: the row goes when
  // this window archives it, before any read agrees.
  it("changes when a design is archived", () => {
    const after = { ...seen, designs: seen.designs.slice(1) };
    expect(drawnFrom(after)).not.toBe(drawnFrom(seen));
  });

  it("changes when a session is archived", () => {
    expect(drawnFrom({ ...seen, sessions: [] })).not.toBe(drawnFrom(seen));
  });

  it("changes when a session stops, and not when the numbers are still coming", () => {
    const stopped = { ...seen, sessions: [{ id: "s1", name: "one", meta: "main · 2m" }] };
    expect(drawnFrom(stopped)).not.toBe(drawnFrom(seen));
    expect(drawnFrom({ ...seen, note: "reading" })).toBe(drawnFrom(seen));
  });
});
