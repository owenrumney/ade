import { describe, expect, it } from "vitest";
import type { Repo } from "./launcher";
import { rows } from "./elsewhere";

const has = (name: string, path: string): Repo => ({
  path,
  name,
  owner: "owenrumney",
  checkouts: [{ path, name, branch: "main", sessions: null }],
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
});
