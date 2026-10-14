import { describe, expect, it } from "vitest";
import {
  description,
  detail,
  every,
  flatten,
  found,
  group,
  label,
  open,
  running,
  sections,
  shortlist,
} from "./switcher";
import type { Repo, Session, Waiting } from "./launcher";

const session = (id: string, over: Partial<Session> = {}): Session => ({
  id,
  agent: "claude",
  dir: "/r/ade",
  name: id,
  branch: "main",
  ...over,
});

const repo = (name: string, sessions: Session[], path = "/r/ade"): Repo => ({
  path,
  name,
  owner: "owenrumney",
  checkouts: [{ path, name, branch: "main", sessions }],
});

const waiting = (id: string): Waiting => ({
  id,
  dir: "/r/ade",
  name: id,
  lastUsed: "2026-09-30T18:00:00Z",
});

describe("flatten", () => {
  it("drops the repository and checkout levels", () => {
    const got = flatten(
      [repo("ade", [session("a"), session("b")])],
      [],
      undefined,
    );
    expect(got.map((r) => r.session.id)).toEqual(["a", "b"]);
    expect(got[0].repo).toBe("ade");
  });

  // Putting a session away is saying not this one.
  it("leaves out an archived session", () => {
    const got = flatten(
      [repo("ade", [session("a", { archived: true }), session("b")])],
      [],
      undefined,
    );
    expect(got.map((r) => r.session.id)).toEqual(["b"]);
  });

  it("marks the one this window is open on", () => {
    const got = flatten([repo("ade", [session("a")])], [], "/r/ade");
    expect(got[0].here).toBe(true);
  });

  it("marks the ones waiting on you", () => {
    const got = flatten(
      [repo("ade", [session("a"), session("b")])],
      [waiting("b")],
      undefined,
    );
    expect(got.find((r) => r.session.id === "b")?.waiting).toBe(true);
    expect(got.find((r) => r.session.id === "a")?.waiting).toBe(false);
  });

  it("is empty rather than throwing when the launcher lists nothing", () => {
    expect(flatten([], [], undefined)).toEqual([]);
    expect(
      flatten(
        [{ path: "/r", name: "r", owner: "o", checkouts: null }],
        [],
        undefined,
      ),
    ).toEqual([]);
  });
});

describe("order", () => {
  const at = (n: number) => `2026-09-${String(n).padStart(2, "0")}T00:00:00Z`;

  it("puts one that asked you something first, even over a running one", () => {
    const got = flatten(
      [repo("ade", [session("running", { live: true }), session("asked")])],
      [waiting("asked")],
      undefined,
    );
    expect(got[0].session.id).toBe("asked");
  });

  it("puts a running session above an idle one", () => {
    const got = flatten(
      [repo("ade", [session("idle"), session("live", { live: true })])],
      [],
      undefined,
    );
    expect(got[0].session.id).toBe("live");
  });

  it("orders the rest most recent first", () => {
    const got = flatten(
      [
        repo("ade", [
          session("old", { lastUsed: at(1) }),
          session("new", { lastUsed: at(29) }),
        ]),
      ],
      [],
      undefined,
    );
    expect(got.map((r) => r.session.id)).toEqual(["new", "old"]);
  });
});

describe("label", () => {
  const row = (over: Partial<Session>, waitingToo = false) => ({
    session: session("a", over),
    repo: "ade",
    waiting: waitingToo,
    here: false,
  });

  it("is the title when the conversation has one", () => {
    expect(label(row({ title: "fix the pi" }))).toContain("fix the pi");
  });

  it("falls back to the branch, then the name", () => {
    expect(label(row({ branch: "fix-thing" }))).toContain("fix-thing");
    expect(label(row({ branch: "", name: "0d4f" }))).toContain("0d4f");
  });

  it("marks a waiting session differently from a merely running one", () => {
    expect(label(row({ live: true }, true))).toContain("$(bell)");
    expect(label(row({ live: true }))).toContain("$(circle-filled)");
    expect(label(row({}))).not.toContain("$(");
  });
});

describe("description", () => {
  const row = (over: Partial<Session>, here = false) => ({
    session: session("a", over),
    repo: "ade",
    waiting: false,
    here,
  });

  // The repository is the heading now, so repeating it on every row was the
  // duplication that made the list unreadable.
  it("does not name the repository, which the heading already did", () => {
    expect(description(row({ branch: "fix-thing" }))).toBe("fix-thing");
  });

  it("is empty when the branch is already the label", () => {
    expect(description(row({ title: "main", branch: "main" }))).toBe("");
  });

  it("says which one this window is on", () => {
    expect(description(row({}, true))).toContain("this window");
  });
});

describe("detail", () => {
  const row = (over: Partial<Session>, here = false) => ({
    session: session("a", { resumable: true, ...over }),
    repo: "ade",
    waiting: false,
    here,
  });

  it("warns that picking one elsewhere changes folder", () => {
    expect(detail(row({}))).toContain("opens its folder");
    expect(detail(row({}, true))).not.toContain("opens its folder");
  });

  it("says when the conversation cannot come with it", () => {
    expect(detail(row({ resumable: false }))).toContain("cannot be resumed");
  });

  it("names the agent", () => {
    expect(detail(row({}, true))).toContain("claude");
  });
});

describe("open", () => {
  const row = (over: Partial<Session>, waitingToo = false) => ({
    session: session("a", over),
    repo: "ade",
    waiting: waitingToo,
    here: false,
  });

  it("is a running session", () => {
    expect(open(row({ live: true }))).toBe(true);
  });

  it("is one that asked you something, running or not", () => {
    expect(open(row({}, true))).toBe(true);
  });

  // Everything else is history, and history is what the tree is for.
  it("is not an idle session nobody is waiting on", () => {
    expect(open(row({}))).toBe(false);
  });
});

describe("group", () => {
  const rows = (...specs: [string, string][]) =>
    specs.map(([repo, id]) => ({
      session: session(id),
      repo,
      waiting: false,
      here: false,
    }));

  it("says each repository once", () => {
    const got = group(rows(["ade", "a"], ["ade", "b"], ["other", "c"]));
    expect(got.map((g) => g.repo)).toEqual(["ade", "other"]);
    expect(got[0].rows).toHaveLength(2);
  });

  // The rows arrive best-first, so the repository holding the most urgent
  // session has to lead rather than being sorted alphabetically.
  it("keeps the order the rows arrived in", () => {
    const got = group(rows(["z-repo", "a"], ["a-repo", "b"]));
    expect(got.map((g) => g.repo)).toEqual(["z-repo", "a-repo"]);
  });

  it("regroups a repository whose rows are not adjacent", () => {
    const got = group(rows(["ade", "a"], ["other", "b"], ["ade", "c"]));
    expect(got.map((g) => g.repo)).toEqual(["ade", "other"]);
    expect(got[0].rows.map((r) => r.session.id)).toEqual(["a", "c"]);
  });

  it("is empty for no rows", () => {
    expect(group([])).toEqual([]);
  });
});

describe("which sessions the switcher calls running", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      owner: "o",
      checkouts: [
        {
          path: "/r/ade",
          name: "ade",
          branch: "main",
          sessions: [
            { id: "a", agent: "claude", dir: "/r/ade", name: "ade", branch: "main", live: true },
            { id: "b", agent: "claude", dir: "/r/ade", name: "ade", branch: "x" },
          ],
        },
      ],
    },
  ] as unknown as Repo[];

  // The scan is as old as its last sweep, and got both of these wrong.
  it("takes it from the launcher's list, not the scan's flag", () => {
    const rows = flatten(repos, [], undefined, [{ id: "b", dir: "/r/ade", name: "ade" }]);
    expect(rows.find((r) => r.session.id === "b")?.session.live).toBe(true);
    expect(rows.find((r) => r.session.id === "a")?.session.live).toBe(false);
  });

  it("puts the running one first", () => {
    const rows = flatten(repos, [], undefined, [{ id: "b", dir: "/r/ade", name: "ade" }]);
    expect(rows[0].session.id).toBe("b");
  });

  it("falls back to the flag when the list is empty", () => {
    const rows = flatten(repos, [], undefined, []);
    expect(rows.find((r) => r.session.id === "a")?.session.live).toBe(true);
  });
});

describe("the switcher's list", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      owner: "o",
      checkouts: [
        {
          path: "/r/ade",
          name: "ade",
          branch: "main",
          sessions: [
            { id: "a", agent: "claude", dir: "/r/ade", name: "ade", branch: "main" },
            { id: "old", agent: "claude", dir: "/r/ade", name: "ade", branch: "x" },
          ],
        },
      ],
    },
  ] as unknown as Repo[];

  // 58 conversations the scan has ever seen is not "what am I working on".
  it("is what is up, not everything the scan knows", () => {
    const rows = running(flatten(repos, [], undefined), [
      { id: "a", dir: "/r/ade", name: "ade" },
    ], undefined);
    expect(rows.map((r) => r.session.id)).toEqual(["a"]);
  });

  it("keeps one the scan has never seen", () => {
    const rows = running(flatten(repos, [], undefined), [
      { id: "z", dir: "/elsewhere/cli", name: "cli", repo: "cli", branch: "trk-1" },
    ], undefined);
    expect(rows).toHaveLength(1);
    expect(rows[0].repo).toBe("cli");
    expect(rows[0].session.branch).toBe("trk-1");
    expect(rows[0].session.live).toBe(true);
  });

  it("marks the one this window is in", () => {
    const rows = running(flatten(repos, [], "/r/ade"), [
      { id: "a", dir: "/r/ade", name: "ade" },
    ], "/r/ade");
    expect(rows[0].here).toBe(true);
  });
});

describe("the switcher's sections", () => {
  const all = flatten(
    [repo("ade", [session("a"), session("asked"), session("old")])],
    [waiting("asked")],
    undefined,
  );

  // The toast is gone, so this heading is where a waiting session is seen.
  it("puts what is waiting above the repositories", () => {
    const got = sections(shortlist(all, [{ id: "a", dir: "/r/ade", name: "ade" }], undefined));
    expect(got[0].repo).toBe("Waiting for you");
    expect(got[0].rows.map((r) => r.session.id)).toEqual(["asked"]);
    expect(got[1].repo).toBe("ade");
  });

  it("prints no heading when nothing is waiting", () => {
    const quiet = flatten([repo("ade", [session("a")])], [], undefined);
    expect(sections(quiet).map((g) => g.repo)).toEqual(["ade"]);
  });

  // The shortlist used to cut to what is running, which hid the one session
  // actually asking for you.
  it("keeps a waiting session that is not running", () => {
    const rows = shortlist(all, [{ id: "a", dir: "/r/ade", name: "ade" }], undefined);
    expect(rows.map((r) => r.session.id)).toContain("asked");
    expect(rows.map((r) => r.session.id)).toContain("a");
  });

  it("lists it once, not in both halves", () => {
    const rows = shortlist(all, [{ id: "asked", dir: "/r/ade", name: "ade" }], undefined);
    expect(rows.filter((r) => r.session.id === "asked")).toHaveLength(1);
  });

  it("falls back to everything with nothing up and nothing asking", () => {
    const quiet = flatten([repo("ade", [session("a"), session("b")])], [], undefined);
    expect(shortlist(quiet, [], undefined)).toHaveLength(2);
  });
});

describe("the search's list", () => {
  const all = flatten(
    [
      repo("ade", [session("a", { title: "the chat panel" })]),
      repo(
        "coast",
        [session("b", { dir: "/r/coast", branch: "fix-726" })],
        "/r/coast",
      ),
    ],
    [],
    undefined,
  );

  it("keeps every repository, where the switcher cuts to what is up", () => {
    const rows = every(
      all,
      [{ id: "a", dir: "/r/ade", name: "ade" }],
      undefined,
    );
    expect(rows.map((r) => r.session.id).sort()).toEqual(["a", "b"]);
  });

  it("takes in a running session the scan has not swept yet", () => {
    const rows = every(
      all,
      [{ id: "new", dir: "/r/x", name: "x", repo: "x" }],
      undefined,
    );
    expect(rows[0].session.id).toBe("new");
  });

  it("adds a running one once, not twice", () => {
    const rows = every(
      all,
      [{ id: "a", dir: "/r/ade", name: "ade" }],
      undefined,
    );
    expect(rows.filter((r) => r.session.id === "a")).toHaveLength(1);
  });

  // No heading to carry it, so typing the repository has to find the row.
  it("puts the repository in the text", () => {
    const row = all.find((r) => r.session.id === "b")!;
    expect(found(row)).toContain("coast");
    expect(found(row)).toContain("fix-726");
  });

  it("says when the session was last used", () => {
    const row = flatten(
      [repo("ade", [session("a", { lastUsed: "2026-10-01T00:00:00Z" })])],
      [],
      undefined,
    )[0];
    expect(found(row, Date.parse("2026-10-04T00:00:00Z"))).toContain(
      "3 days ago",
    );
  });

  it("orders by last used, not by what is waiting or running", () => {
    const rows = every(
      flatten(
        [
          repo("ade", [
            session("asked", { lastUsed: "2026-09-01T00:00:00Z" }),
            session("live", { live: true, lastUsed: "2026-09-02T00:00:00Z" }),
            session("idle", { lastUsed: "2026-10-01T00:00:00Z" }),
          ]),
        ],
        [waiting("asked")],
        undefined,
      ),
      [],
      undefined,
    );
    expect(rows.map((r) => r.session.id)).toEqual(["idle", "live", "asked"]);
  });
});
