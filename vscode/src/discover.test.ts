import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  gitDir,
  label,
  nearest,
  probe,
  read,
  recordPath,
  split,
} from "./discover";

const rec = { pid: 42, url: "http://127.0.0.1:7777/?t=secret", root: "/repo" };

async function tempRecord(body: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "ade-vscode-"));
  const path = join(dir, "server.json");
  await writeFile(path, body);
  return path;
}

function answers(health: unknown, ok = true): typeof fetch {
  return (async () =>
    ({
      ok,
      json: async () => health,
      text: async () => "",
    }) as unknown as Response) as typeof fetch;
}

describe("the record", () => {
  it("is read from inside the git dir, where it is never committed", () => {
    expect(recordPath("/repo/.git")).toBe("/repo/.git/ade/server.json");
  });

  it("is absent rather than an error when ade is not running", async () => {
    expect(await read("/nowhere/server.json")).toBeUndefined();
  });

  it("is absent when it does not name a running server", async () => {
    expect(await read(await tempRecord('{"pid":0,"url":""}'))).toBeUndefined();
    expect(await read(await tempRecord("not json"))).toBeUndefined();
  });

  it("carries the token in its url", async () => {
    const got = await read(await tempRecord(JSON.stringify(rec)));
    expect(got?.url).toBe(rec.url);
    expect(split(rec.url)).toEqual({
      base: "http://127.0.0.1:7777",
      token: "secret",
    });
  });

  it("is unusable without a token, whatever else it says", () => {
    expect(split("http://127.0.0.1:7777/")).toBeUndefined();
    expect(split("not a url")).toBeUndefined();
  });
});

describe("the git dir", () => {
  it("is .git when a checkout has one", async () => {
    const dir = await mkdtemp(join(tmpdir(), "ade-vscode-"));
    await mkdir(join(dir, ".git"));
    expect(await gitDir(dir)).toBe(join(dir, ".git"));
  });

  it("is the one a worktree's .git file names", async () => {
    // ade records into the worktree's own git dir, so a worktree is missed
    // entirely if .git is read as a directory.
    const dir = await mkdtemp(join(tmpdir(), "ade-vscode-"));
    await writeFile(
      join(dir, ".git"),
      "gitdir: /main/.git/worktrees/feature\n",
    );
    expect(await gitDir(dir)).toBe("/main/.git/worktrees/feature");
  });

  it("resolves a relative gitdir against the folder", async () => {
    const dir = await mkdtemp(join(tmpdir(), "ade-vscode-"));
    await writeFile(
      join(dir, ".git"),
      "gitdir: ../main/.git/worktrees/feature",
    );
    expect(await gitDir(dir)).toBe(join(dir, "../main/.git/worktrees/feature"));
  });

  it("is absent for a folder that is not a checkout", async () => {
    expect(
      await gitDir(await mkdtemp(join(tmpdir(), "ade-vscode-"))),
    ).toBeUndefined();
  });

  it("is absent when the .git file says nothing usable", async () => {
    const dir = await mkdtemp(join(tmpdir(), "ade-vscode-"));
    await writeFile(join(dir, ".git"), "not a gitdir line\n");
    expect(await gitDir(dir)).toBeUndefined();
  });
});

describe("the probe", () => {
  it("accepts a port the recorded process still holds", async () => {
    const got = await probe(
      rec,
      answers({ ade: true, pid: 42, root: "/repo" }),
    );
    expect(got).toEqual({
      base: "http://127.0.0.1:7777",
      token: "secret",
      root: "/repo",
    });
  });

  it("refuses a port some other process took", async () => {
    // A live pid alone proves nothing, since pids are reused.
    expect(
      await probe(rec, answers({ ade: true, pid: 99, root: "/repo" })),
    ).toBeUndefined();
  });

  it("refuses an ade serving a different checkout", async () => {
    // The port is predictable from the checkout, so the root has to agree too.
    expect(
      await probe(rec, answers({ ade: true, pid: 42, root: "/elsewhere" })),
    ).toBeUndefined();
  });

  it("refuses something on the port that is not ade at all", async () => {
    expect(await probe(rec, answers({ hello: true }))).toBeUndefined();
    expect(
      await probe(rec, answers({ ade: true, pid: 42, root: "/repo" }, false)),
    ).toBeUndefined();
  });

  it("refuses when nothing answers", async () => {
    const dead = (async () => {
      throw new Error("ECONNREFUSED");
    }) as typeof fetch;
    expect(await probe(rec, dead)).toBeUndefined();
  });
});
// A window attaches to one ade, and it was the first folder in the list. Using
// the menu then reviewed a checkout that was not the one on screen.
describe("which folder a window attaches to", () => {
  const folders = ["/code/infracost/ci-fix-739", "/code/owenrumney/ade"];

  it("puts the folder holding the open file first", () => {
    expect(
      nearest(folders, "/code/owenrumney/ade/internal/web/web.go"),
    ).toEqual(["/code/owenrumney/ade", "/code/infracost/ci-fix-739"]);
  });

  it("leaves the order alone with no editor open", () => {
    expect(nearest(folders, undefined)).toEqual(folders);
  });

  it("leaves the order alone for a file outside every folder", () => {
    expect(nearest(folders, "/tmp/scratch.md")).toEqual(folders);
  });

  it("does not match a sibling that shares a prefix", () => {
    expect(nearest(["/code/ci"], "/code/ci-fix-739/x.go")).toEqual([
      "/code/ci",
    ]);
  });

  it("prefers a checkout nested inside another", () => {
    const nested = ["/code/repo", "/code/repo/vendor/dep"];
    expect(nearest(nested, "/code/repo/vendor/dep/a.go")[0]).toBe(
      "/code/repo/vendor/dep",
    );
  });
});

// The activity bar container's title is static — VS Code has no api to rename
// one — so the checkout is named on the topmost view instead.
describe("naming the checkout in the view header", () => {
  it("is the project when the directory is the repository's own", () => {
    expect(label("owenrumney/ade", "/Users/owen/code/owenrumney/ade")).toBe(
      "owenrumney/ade",
    );
  });

  it("adds the worktree, since two worktrees are one project and two jobs", () => {
    expect(label("infracost/ci", "/Users/owen/code/infracost/ci-fix-739")).toBe(
      "infracost/ci · ci-fix-739",
    );
  });

  it("falls back to the directory with no origin remote", () => {
    expect(label(undefined, "/Users/owen/code/scratch")).toBe("scratch");
    expect(label("", "/Users/owen/code/scratch")).toBe("scratch");
  });

  it("says nothing rather than something wrong with neither", () => {
    expect(label(undefined, undefined)).toBe("");
  });
});

describe("a session on a worker", () => {
  // Its own port is the container's, and nothing on the host it runs on
  // answers there. The launcher's is the one docker publishes.
  const worker = {
    ...rec,
    url: "http://127.0.0.1:24590/?t=inside",
    proxy: "http://127.0.0.1:36448/s/abc/?t=worker",
  };

  it("keeps the session's path, so the launcher is not addressed instead", () => {
    expect(split(worker.proxy)).toEqual({
      base: "http://127.0.0.1:36448/s/abc",
      token: "worker",
    });
  });

  it("is reached through the launcher when its own address answers nothing", async () => {
    const asked: string[] = [];
    const fetchFn = (async (at: string) => {
      asked.push(at);
      if (at.startsWith("http://127.0.0.1:24590")) throw new Error("refused");
      return {
        ok: true,
        json: async () => ({ ade: true, pid: 42, root: "/repo" }),
      };
    }) as unknown as typeof fetch;

    expect(await probe(worker, fetchFn)).toEqual({
      base: "http://127.0.0.1:36448/s/abc",
      token: "worker",
      root: "/repo",
    });
    expect(asked[0]).toContain(":24590");
    expect(asked[1]).toBe("http://127.0.0.1:36448/s/abc/healthz?t=worker");
  });

  it("prefers its own address when that answers", async () => {
    const got = await probe(
      worker,
      answers({ ade: true, pid: 42, root: "/repo" }),
    );
    expect(got?.base).toBe("http://127.0.0.1:24590");
  });

  it("is absent when neither answers", async () => {
    const dead = (async () => {
      throw new Error("refused");
    }) as unknown as typeof fetch;
    expect(await probe(worker, dead)).toBeUndefined();
  });
});

describe("a record whose proxied session has ended", () => {
  const stale = {
    ...rec,
    url: "http://127.0.0.1:24590/?t=inside",
    proxy: "http://127.0.0.1:36448/s/gone/?t=worker",
  };

  it("asks the launcher which session is serving, and reaches that", async () => {
    const asked: string[] = [];
    const fetchFn = (async (at: string) => {
      asked.push(at);
      if (at.includes("/s/gone/")) return { ok: false, status: 404 };
      if (at.startsWith("http://127.0.0.1:24590")) throw new Error("refused");
      if (at.includes("/api/serving"))
        return { ok: true, json: async () => [{ id: "live", dir: "/repo" }] };
      return {
        ok: true,
        json: async () => ({ ade: true, pid: 42, root: "/repo" }),
      };
    }) as unknown as typeof fetch;

    expect(await probe(stale, fetchFn)).toEqual({
      base: "http://127.0.0.1:36448/s/live",
      token: "worker",
      root: "/repo",
    });
    expect(asked.at(-1)).toBe("http://127.0.0.1:36448/s/live/healthz?t=worker");
  });

  it("gives up when the launcher knows of no session for this checkout", async () => {
    const fetchFn = (async (at: string) => {
      if (at.includes("/api/serving"))
        return { ok: true, json: async () => [{ id: "x", dir: "/elsewhere" }] };
      return { ok: false, status: 404 };
    }) as unknown as typeof fetch;

    expect(await probe(stale, fetchFn)).toBeUndefined();
  });
});
