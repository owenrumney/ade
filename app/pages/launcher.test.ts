import { describe, expect, it } from "vitest";
import { ensure, holds, launcherRecord, type Repo } from "./launcher";
import type { Server } from "./discover";

const up = { base: "http://127.0.0.1:1", token: "t", root: "/r" } as Server;

function repo(over: Partial<Repo> = {}): Repo {
  return {
    path: "/r",
    name: "r",
    owner: "o",
    checkouts: [
      { path: "/r", name: "r", branch: "main", sessions: [] },
      { path: "/r-feature", name: "r", branch: "feature", sessions: [] },
    ],
    ...over,
  };
}

describe("the launcher", () => {
  it("records under ade's home state, not inside a checkout", () => {
    // It serves no checkout, so there is no git dir to write into.
    expect(launcherRecord("/home/owen")).toBe("/home/owen/.ade/server.json");
  });
});

describe("the repository this window is in", () => {
  it("is the one holding the checkout on screen, whichever checkout that is", () => {
    expect(holds(repo(), "/r-feature")).toBe(true);
    expect(holds(repo(), "/r")).toBe(true);
  });

  it("is not one holding a different checkout", () => {
    expect(holds(repo(), "/elsewhere")).toBe(false);
  });

  it("is nothing at all when no ade is running here", () => {
    expect(holds(repo(), undefined)).toBe(false);
  });

  it("survives a repository with no checkouts recorded", () => {
    expect(holds(repo({ checkouts: null }), "/r")).toBe(false);
  });
});

describe("making sure there is a launcher", () => {
  it("starts nothing when one is already running", async () => {
    let spawned = 0;
    const got = await ensure(
      async () => up,
      () => spawned++,
      async () => undefined,
    );
    expect(got).toBe(up);
    expect(spawned).toBe(0);
  });

  it("spawns one and waits for it to answer", async () => {
    let spawned = 0;
    const got = await ensure(
      async () => (spawned ? up : undefined),
      () => spawned++,
      (look) => look(),
    );
    expect(spawned).toBe(1);
    expect(got).toBe(up);
  });

  it("gives back nothing when it never comes up", async () => {
    expect(
      await ensure(
        async () => undefined,
        () => {},
        (look) => look(),
      ),
    ).toBeUndefined();
  });
});
