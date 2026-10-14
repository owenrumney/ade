import { describe, expect, it } from "vitest";
import {
  count,
  elsewhere,
  interesting,
  label,
  labelAll,
  live,
  mine,
  only,
  rank,
  sessionLine,
  sessionsAcross,
  blameWhen,
  combinable,
  repoContext,
  sessionContext,
  sessionsOf,
  since,
  tally,
  title,
  trim,
  workspacesOf,
  worktreesOf,
} from "./shape";
import type { Checkout, Repo, Session } from "./launcher";
import type { Tree } from "./client";

const session = (id: string, over: Partial<Session> = {}): Session => ({
  id,
  agent: "claude",
  dir: "/r",
  name: id,
  branch: "main",
  ...over,
});

const checkout = (path: string, sessions: Session[] = []): Checkout => ({
  path,
  name: path.split("/").pop() ?? path,
  branch: "main",
  sessions,
});

const repo = (name: string, checkouts: Checkout[]): Repo => ({
  path: `/r/${name}`,
  name,
  owner: "owenrumney",
  checkouts,
});

describe("rank", () => {
  it("puts a live session above a dead one", () => {
    const got = [session("a"), session("b", { live: true })].sort(rank);
    expect(got[0].id).toBe("b");
  });

  it("orders the rest most recent first", () => {
    const got = [
      session("old", { lastUsed: "2026-09-01T00:00:00Z" }),
      session("new", { lastUsed: "2026-09-30T00:00:00Z" }),
    ].sort(rank);
    expect(got[0].id).toBe("new");
  });

  it("does not throw on a session the launcher gave no date", () => {
    expect([session("a"), session("b")].sort(rank)).toHaveLength(2);
  });
});

describe("sessionsOf", () => {
  const many = checkout(
    "/r",
    Array.from({ length: 12 }, (_, i) => session(`s${i}`)),
  );

  it("cuts to the cap and counts the rest", () => {
    const got = sessionsOf(many, 5, false);
    expect(got.shown).toHaveLength(5);
    expect(got.hidden).toBe(7);
  });

  it("hides nothing when asked for everything", () => {
    expect(sessionsOf(many, 5, true).hidden).toBe(0);
    expect(sessionsOf(many, 0, false).hidden).toBe(0);
  });

  it("hides nothing when there is less than the cap", () => {
    expect(sessionsOf(checkout("/r", [session("a")]), 5, false).hidden).toBe(0);
  });

  // A live session must never be the one cut: it is the one being watched.
  it("keeps the live ones, whatever the cap", () => {
    const mixed = checkout("/r", [
      ...Array.from({ length: 9 }, (_, i) => session(`s${i}`)),
      session("running", { live: true }),
    ]);
    expect(sessionsOf(mixed, 3, false).shown.map((s) => s.id)).toContain(
      "running",
    );
  });
});

describe("sessionsAcross", () => {
  it("is every checkout's conversations in one ordered, cut list", () => {
    const r = repo("coast", [
      checkout("/a", [session("a1"), session("a2", { live: true })]),
      checkout("/b", [session("b1")]),
    ]);
    const got = sessionsAcross(r, 2, false);
    expect(got.shown.map((s) => s.id)[0]).toBe("a2");
    expect(got.shown).toHaveLength(2);
    expect(got.hidden).toBe(1);
  });
});

describe("only", () => {
  it("is the one checkout a repository has", () => {
    expect(only(repo("ade", [checkout("/r/ade")]))?.path).toBe("/r/ade");
  });

  it("is nothing when there are several to tell apart", () => {
    expect(only(repo("ade", [checkout("/a"), checkout("/b")]))).toBeUndefined();
  });

  it("is nothing when the launcher listed none", () => {
    expect(
      only({ path: "/r", name: "ade", owner: "o", checkouts: null }),
    ).toBeUndefined();
  });
});

describe("interesting", () => {
  it("keeps a repository with a conversation in it", () => {
    expect(
      interesting(repo("ade", [checkout("/a", [session("s")])]), undefined),
    ).toBe(true);
  });

  it("keeps the one this window is open on, even with no sessions", () => {
    expect(interesting(repo("ade", [checkout("/a")]), "/a")).toBe(true);
  });

  it("drops a scanned checkout nobody has talked to", () => {
    expect(interesting(repo("other", [checkout("/b")]), "/a")).toBe(false);
  });
});

describe("trim", () => {
  const repos = [
    repo("ade", [checkout("/a", [session("s")])]),
    repo("quiet", [checkout("/b")]),
    repo("also-quiet", [checkout("/c")]),
  ];

  it("drops the quiet ones and says how many went", () => {
    const got = trim(repos, "/a", false);
    expect(got.shown.map((r) => r.name)).toEqual(["ade"]);
    expect(got.hidden).toBe(2);
  });

  it("keeps everything when asked", () => {
    expect(trim(repos, "/a", true).hidden).toBe(0);
    expect(trim(repos, "/a", true).shown).toHaveLength(3);
  });
});

describe("label", () => {
  it("counts the checkouts only when there is more than one", () => {
    expect(label(repo("ade", [checkout("/a", [session("s")])]))).not.toContain(
      "checkout",
    );
    expect(label(repo("ade", [checkout("/a"), checkout("/b")]))).toContain(
      "2 checkouts",
    );
  });

  it("says what is running in preference to the total", () => {
    const r = repo("ade", [
      checkout("/a", [session("s"), session("t", { live: true })]),
    ]);
    expect(label(r)).toContain("1 live");
    expect(label(r)).not.toContain("2");
  });

  it("falls back to the total when nothing is running", () => {
    expect(
      label(repo("ade", [checkout("/a", [session("s"), session("t")])])),
    ).toBe("2");
  });

  it("says nothing about an empty repository", () => {
    expect(label(repo("ade", [checkout("/a")]))).toBe("");
  });
});

describe("count and live", () => {
  const r = repo("ade", [
    checkout("/a", [session("s", { live: true }), session("t")]),
    checkout("/b", [session("u")]),
  ]);

  it("adds up across the checkouts", () => {
    expect(count(r)).toBe(3);
    expect(live(r)).toBe(1);
  });
});

describe("what a conversation row says it is about", () => {
  const at = Date.parse("2026-10-01T12:00:00Z");
  const one = (over: Partial<Session> = {}): Session => ({
    id: "s1",
    agent: "claude",
    dir: "/code/ade",
    name: "ade",
    branch: "TRK-1072",
    lastUsed: "2026-10-01T10:00:00Z",
    ...over,
  });

  // A title is written from the first turn and never again: one that started
  // on TRK-1065 and moved to TRK-1072 still reads as TRK-1065.
  it("leads with the branch, not the title", () => {
    expect(sessionLine(one(), at)).toBe("TRK-1072 · 2h ago");
  });

  it("marks the one that is running", () => {
    expect(sessionLine(one({ live: true }), at)).toBe(
      "TRK-1072 · 2h ago · live",
    );
  });

  it("says what it has rather than an empty separator", () => {
    expect(sessionLine(one({ branch: "", lastUsed: undefined }), at)).toBe("");
    expect(sessionLine(one({ branch: "" }), at)).toBe("2h ago");
  });
});

describe("how long ago", () => {
  const at = Date.parse("2026-10-01T12:00:00Z");
  const ago = (when: string) => since(when, at);

  it("counts in the unit that fits", () => {
    expect(ago("2026-10-01T11:59:30Z")).toBe("now");
    expect(ago("2026-10-01T11:25:00Z")).toBe("35m ago");
    expect(ago("2026-10-01T04:00:00Z")).toBe("8h ago");
    expect(ago("2026-09-28T12:00:00Z")).toBe("3d ago");
  });

  it("says nothing for a time it cannot read", () => {
    expect(since(undefined, at)).toBe("");
    expect(since("not a date", at)).toBe("");
  });
});

describe("when a blamed line was committed", () => {
  const at = Date.parse("2026-10-01T12:00:00Z");

  it("counts the recent ones the way everything else does", () => {
    expect(blameWhen("2026-10-01T04:00:00Z", at)).toBe("8h ago");
    expect(blameWhen("2026-09-28T12:00:00Z", at)).toBe("3d ago");
  });

  // "730d ago" is a number nobody converts; a year-old line gets a date.
  it("gives a date past a month", () => {
    expect(blameWhen("2024-10-01T12:00:00Z", at)).not.toContain("ago");
    expect(blameWhen("2024-10-01T12:00:00Z", at)).toContain("2024");
  });

  it("says nothing for a time it cannot read", () => {
    expect(blameWhen(undefined, at)).toBe("");
    expect(blameWhen("not a date", at)).toBe("");
  });
});

describe("the checkout's worktree rows", () => {
  const tree = (path: string, over: Partial<Tree> = {}): Tree => ({
    path,
    name: path.split("/").pop() ?? path,
    kind: "linked",
    removable: true,
    ...over,
  });
  const ade = repo("ade", [
    checkout("/r/ade-fix", [session("s")]),
    checkout("/r/ade"),
  ]);

  // The launcher knows the conversations and the server knows the removal
  // state; a row is the two joined by path.
  it("joins the launcher's checkout to the server's tree", () => {
    const rows = worktreesOf(ade, [
      tree("/r/ade", { main: true, branch: "main" }),
      tree("/r/ade-fix", { branch: "fix" }),
    ]);
    expect(rows.map((r) => r.path)).toEqual(["/r/ade", "/r/ade-fix"]);
    expect(rows[1].checkout?.sessions).toHaveLength(1);
    expect(rows[1].tree?.branch).toBe("fix");
  });

  // The switch list marks the main checkout; a name that sorts last must
  // still come first.
  it("puts the repository's own checkout first, then the rest by name", () => {
    const rows = worktreesOf(undefined, [
      tree("/r/zed"),
      tree("/r/work", { main: true }),
      tree("/r/beta"),
    ]);
    expect(rows.map((r) => r.name)).toEqual(["work", "beta", "zed"]);
  });

  // With no server there is no Tree to say so; the launcher's repository
  // path is its main checkout.
  it("knows the main checkout from the launcher alone", () => {
    const work = repo("work", [checkout("/r/work-fix"), checkout("/r/work")]);
    const rows = worktreesOf(work, []);
    expect(rows.map((r) => r.name)).toEqual(["work", "work-fix"]);
    expect(rows[0].main).toBe(true);
  });

  it("still lists a worktree only one side knows about", () => {
    const rows = worktreesOf(ade, [tree("/r/elsewhere")]);
    expect(rows.map((r) => r.path).sort()).toEqual([
      "/r/ade",
      "/r/ade-fix",
      "/r/elsewhere",
    ]);
    expect(rows.find((r) => r.path === "/r/ade")?.tree).toBeUndefined();
    expect(
      rows.find((r) => r.path === "/r/elsewhere")?.checkout,
    ).toBeUndefined();
  });

  it("takes the branch from whichever side has it", () => {
    const rows = worktreesOf(ade, [tree("/r/ade-fix", { branch: "" })]);
    expect(rows.find((r) => r.path === "/r/ade-fix")?.branch).toBe("main");
  });

  it("is empty with nothing from either side", () => {
    expect(worktreesOf(undefined, [])).toEqual([]);
  });
});

describe("this window's repository and the rest", () => {
  const here = repo("ade", [checkout("/r/ade"), checkout("/r/ade-fix")]);
  const busy = repo("coast", [checkout("/r/coast", [session("s")])]);
  const quiet = repo("quiet", [checkout("/r/quiet")]);
  const all = [here, busy, quiet];

  it("finds the repository by any of its checkouts", () => {
    expect(mine(all, "/r/ade-fix")?.name).toBe("ade");
    expect(mine(all, "/r/nowhere")).toBeUndefined();
    expect(mine(all, undefined)).toBeUndefined();
  });

  // Elsewhere is the one thing the machine-wide list was for: work in a
  // repository this window is not open on. A quiet one is not work.
  it("is the other repositories with a conversation, not this one", () => {
    expect(elsewhere(all, "/r/ade").map((r) => r.name)).toEqual(["coast"]);
  });

  it("does not drop a repository when the window is nowhere", () => {
    expect(elsewhere(all, undefined).map((r) => r.name)).toEqual(["coast"]);
  });
});

describe("labelAll", () => {
  it("says what is running across repositories, else the total", () => {
    const a = repo("a", [checkout("/a", [session("s", { live: true })])]);
    const b = repo("b", [checkout("/b", [session("t"), session("u")])]);
    expect(labelAll([a, b])).toBe("1 live");
    expect(labelAll([b])).toBe("2");
    expect(labelAll([])).toBe("");
  });
});

describe("archived sessions", () => {
  const s = (id: string, more: Partial<Session> = {}): Session =>
    ({
      id,
      agent: "claude",
      dir: "/r",
      name: "r",
      branch: "main",
      ...more,
    }) as Session;
  const checkout = {
    path: "/r",
    sessions: [s("a"), s("b", { archived: true }), s("c")],
  } as Checkout;

  it("are behind the more row until everything is shown", () => {
    const cut = sessionsOf(checkout, 5, false);
    expect(cut.shown.map((x) => x.id).sort()).toEqual(["a", "c"]);
    expect(cut.hidden).toBe(1);
    expect(sessionsOf(checkout, 5, true).shown).toHaveLength(3);
  });

  it("do not take a place under the cap", () => {
    const cut = sessionsOf(checkout, 2, false);
    expect(cut.shown.map((x) => x.id).sort()).toEqual(["a", "c"]);
  });

  it("get their own menu, wherever they are", () => {
    expect(sessionContext(s("b", { archived: true }), true)).toBe(
      "ade.session.archived",
    );
    expect(sessionContext(s("a"), true)).toBe("ade.session.here");
    expect(sessionContext(s("a"), false)).toBe("ade.session.elsewhere");
  });
});

describe("combining sessions", () => {
  const s = (id: string, more: Partial<Session> = {}): Session =>
    ({
      id,
      agent: "claude",
      dir: "/r",
      name: "r",
      branch: "main",
      ...more,
    }) as Session;

  it("offers only the finished, ungrouped ones", () => {
    const got = combinable([
      s("done"),
      s("live", { live: true }),
      s("away", { archived: true }),
      s("in-one", { group: "g1" }),
    ]);
    expect(got.map((x) => x.id)).toEqual(["done"]);
  });

  it("gives a grouped session its own menu, wherever it is", () => {
    expect(sessionContext(s("a", { group: "g1" }), true)).toBe(
      "ade.session.here.grouped",
    );
    expect(sessionContext(s("a", { group: "g1" }), false)).toBe(
      "ade.session.elsewhere.grouped",
    );
    expect(sessionContext(s("a"), true)).toBe("ade.session.here");
  });

  // Archived wins: putting it away is what you did to it last.
  it("keeps archived ahead of grouped", () => {
    expect(sessionContext(s("a", { group: "g", archived: true }), true)).toBe(
      "ade.session.archived",
    );
  });
});

describe("workspacesOf", () => {
  const tree = (path: string, over: Partial<Tree> = {}): Tree => ({
    path,
    name: path.split("/").pop() ?? path,
    kind: "linked",
    removable: true,
    ...over,
  });
  const ade = repo("ade", [
    checkout("/r/ade", [
      session("scratch"),
      session("detached", { branch: "HEAD" }),
      session("unknown", { branch: "" }),
      session("left", { branch: "old-idea" }),
      session("left2", { branch: "old-idea" }),
      session("early", { branch: "fix" }),
    ]),
    checkout("/r/ade-fix", [session("s", { branch: "renamed-since" })]),
  ]);
  const trees = [
    tree("/r/ade", { main: true, branch: "main" }),
    tree("/r/ade-fix", { branch: "fix" }),
  ];
  const ids = (row?: { sessions?: Session[] }) =>
    (row?.sessions ?? []).map((s) => s.id);

  it("names a row by its branch and puts the main checkout last", () => {
    const rows = workspacesOf(ade, trees);
    expect(rows.map(title)).toEqual(["fix", "old-idea", "main"]);
    expect(rows[2].main).toBe(true);
    expect(rows[2].catchAll).toBe(true);
  });

  it("folds what was on the main checkout's branch behind its row", () => {
    const rows = workspacesOf(ade, trees);
    expect(ids(rows[2])).toEqual(["scratch", "detached", "unknown"]);
  });

  it("gives a branch left behind in the main checkout a row of its own", () => {
    const row = workspacesOf(ade, trees)[1];
    expect(row.loose).toBe(true);
    expect(row.path).toBe("/r/ade");
    expect(row.tree).toBeUndefined();
    expect(ids(row)).toEqual(["left", "left2"]);
  });

  it("keeps a worktree's conversations and adds its branch's earlier ones", () => {
    const row = workspacesOf(ade, trees)[0];
    expect(row.loose).toBeUndefined();
    expect(ids(row)).toEqual(["s", "early"]);
  });

  it("does not fold a main checkout that is the only row", () => {
    const alone = repo("solo", [checkout("/r/solo", [session("a")])]);
    const rows = workspacesOf(alone, []);
    expect(rows).toHaveLength(1);
    expect(rows[0].catchAll).toBe(false);
    expect(ids(rows[0])).toEqual(["a"]);
  });

  it("adds this repository's clones on a machine, and nobody else's", () => {
    const far = (name: string, sessions: Session[]): Repo => ({
      ...repo(name, [
        { ...checkout(`/srv/ade/0123/${name}`, sessions), branch: "worker" },
      ]),
    });
    const rows = workspacesOf(ade, trees, [
      {
        machine: "bigboy",
        repos: [
          far("ade", [session("w")]),
          far("other", [session("x")]),
          { ...far("ade", [session("y")]), owner: "someone-else" },
        ],
      },
      { machine: "quiet", repos: [far("ade", [])] },
      { machine: "off", repos: null },
    ]);
    expect(rows.map(title)).toEqual(["fix", "old-idea", "worker", "main"]);
    expect(rows[2].machine).toBe("bigboy");
    expect(ids(rows[2])).toEqual(["w"]);
  });

  it("is empty with nothing from either side", () => {
    expect(workspacesOf(undefined, [])).toEqual([]);
  });
});

describe("tally", () => {
  it("counts the conversations and says how many are running", () => {
    expect(tally([])).toBe("");
    expect(tally([session("a")])).toBe("1 conversation");
    expect(tally([session("a"), session("b", { live: true })])).toBe(
      "2 conversations · 1 live",
    );
  });
});

describe("a repository row", () => {
  it("offers to start a memory, or to stop one", () => {
    expect(
      repoContext({ path: "/r", name: "r", owner: "o", checkouts: null }),
    ).toBe("ade.repo");
    expect(
      repoContext({
        path: "/r",
        name: "r",
        owner: "o",
        checkouts: null,
        remembered: true,
      }),
    ).toBe("ade.repo.remembered");
  });

  it("is a plain row when there is no repository under it", () => {
    expect(repoContext(undefined)).toBe("ade.repo");
  });
});
