import { describe, expect, it } from "vitest";
import type { Repo } from "./launcher";
import { rows } from "./elsewhere";

const has = (name: string, path: string): Repo => ({
  path,
  name,
  owner: "owenrumney",
  checkouts: [{ path, name, branch: "main", sessions: null }],
});

const session = (lastUsed: string) => ({
  id: "s",
  agent: "claude",
  dir: "/d",
  name: "n",
  branch: "main",
  lastUsed,
});

// What bigboy actually looks like: one repository cloned over and over into
// hashed directories, every clone on main and all but one of them empty.
const clones = (paths: string[], busy: Record<string, string> = {}): Repo => ({
  path: "git@github.com:owenrumney/ade.git",
  name: "ade",
  owner: "owenrumney",
  checkouts: paths.map((path) => ({
    path,
    name: "ade",
    branch: "main",
    sessions: busy[path] ? [session(busy[path])] : null,
  })),
});

// What the launcher adds for a repository this machine has and that one does
// not: the origin as the path, and no checkouts.
const missing = (name: string, origin: string): Repo => ({
  path: origin,
  name,
  owner: "owenrumney",
  checkouts: null,
});

describe("where to start on another machine", () => {
  it("offers its own checkouts before the ones it would have to clone", () => {
    const got = rows([
      missing("ade", "git@github.com:owenrumney/ade.git"),
      has("comet", "/home/pi/code/owenrumney/comet"),
    ]);
    expect(got.map((r) => r.label)).toEqual(["comet", "ade"]);
    expect(got[0].clone).toBe(false);
    expect(got[1].clone).toBe(true);
  });

  it("sends the path for one it has and the origin for one it has not", () => {
    const [here, clone] = rows([
      has("comet", "/home/pi/code/owenrumney/comet"),
      missing("ade", "git@github.com:owenrumney/ade.git"),
    ]);
    expect(here.dir).toBe("/home/pi/code/owenrumney/comet");
    expect(clone.dir).toBe("git@github.com:owenrumney/ade.git");
    expect(clone.description).toBe("not there yet");
  });

  it("is empty when the machine offers nothing at all", () => {
    expect(rows([])).toEqual([]);
  });

  // Sixteen rows reading "ade main" and differing only by a hashed directory
  // is not a choice. Any empty clone of a branch is as good as another.
  it("offers one empty checkout per branch, and says how many it stands for", () => {
    const got = rows([clones(["/code/aa/ade", "/code/bb/ade", "/code/cc/ade"])]);
    expect(got).toHaveLength(1);
    expect(got[0].description).toBe("main · 1 of 3 empty checkouts");
    expect(got[0].dir).toBe("/code/aa/ade");
  });

  it("keeps a branch of its own apart from the empty ones", () => {
    const got = rows([
      {
        ...clones(["/code/aa/ade", "/code/bb/ade"]),
        checkouts: [
          { path: "/code/aa/ade", name: "ade", branch: "main", sessions: null },
          {
            path: "/code/bb/ade",
            name: "ade",
            branch: "chore/narrow-verify",
            sessions: null,
          },
        ],
      },
    ]);
    expect(got.map((r) => r.description)).toEqual([
      "main",
      "chore/narrow-verify",
    ]);
  });

  // A checkout with conversations in it is not interchangeable with anything.
  it("gives every checkout holding sessions its own row, newest first", () => {
    const got = rows([
      clones(["/code/aa/ade", "/code/bb/ade", "/code/cc/ade"], {
        "/code/aa/ade": "2026-10-01T09:00:00Z",
        "/code/bb/ade": "2026-10-18T09:00:00Z",
      }),
    ]);
    expect(got.map((r) => r.dir)).toEqual([
      "/code/bb/ade",
      "/code/aa/ade",
      "/code/cc/ade",
    ]);
    expect(got[0].description).toBe("main · 1 session");
    expect(got[2].description).toBe("main");
  });
});
