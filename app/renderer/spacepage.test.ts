import { describe, expect, it } from "vitest";
import { designsOf, drawnFrom, sessionsOf } from "./spacepage";
import type { Design, Repo } from "./real";

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
  const all: Design[] = [
    {
      name: "mechanics",
      dir: "/d/mechanics",
      file: "/d/mechanics/design.md",
      meta: { repos: ["owenrumney/ade"], ticket: "DEV-1" },
      repo: "owenrumney/ade",
      branch: "mechanics",
      live: true,
    },
    {
      name: "other-thing",
      dir: "/d/other-thing",
      file: "/d/other-thing/design.md",
      meta: { repos: ["owenrumney/mdview"] },
      repo: "owenrumney/mdview",
    },
  ];

  // The page listed the repository's own markdown: nine files against a
  // store of a hundred and fifty.
  it("lists the store's designs for this repository and nobody else's", () => {
    expect(designsOf(all, "owenrumney/ade").map((d) => d.name)).toEqual(["mechanics"]);
  });

  it("says what the Designs list says about a row", () => {
    const [one] = designsOf(all, "owenrumney/ade");
    expect(one.live).toBe(true);
    expect(one.meta).toContain("mechanics");
    expect(one.meta).toContain("DEV-1");
  });

  it("has no rows for a repository with nothing in the store", () => {
    expect(designsOf(all, "owenrumney/squealer")).toEqual([]);
  });
});

// The page is built once and kept, so what it was built from has to say when
// it is stale: a design that starts is a row that has to change while the
// page is open.
describe("when a drawn space page is stale", () => {
  const seen = {
    space: { repo: "owenrumney/ade", sessions: 1, here: true, designs: ["a.md", "b.md"] },
    sessions: [{ id: "s1", name: "one", meta: "main · live", live: true }],
    designs: [
      { name: "a", meta: "main · 2m" },
      { name: "b", meta: "main · 3m" },
    ],
  };

  // The key follows the list drawn, not the space's own: the row goes when
  // this window archives it, before any read agrees.
  it("changes when a design leaves the store", () => {
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
