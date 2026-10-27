import { describe, expect, it } from "vitest";
import * as real from "./real";
import {
  ago,
  byProject,
  designs,
  inbox,
  issues,
  prs,
  sessions,
  status,
  waiting,
} from "./real";
import type { Dash, Design, Repo } from "./real";

const now = Date.parse("2026-10-22T12:00:00Z");

const DASH: Dash = {
  reviews: [
    { repo: "infracost/cli", number: 412, title: "Add usage tiles", author: "dan", url: "x", checks: "pass", review: { state: "ready" } },
    { repo: "owenrumney/go-sarif", number: 88, title: "Taxonomies", author: "mia", url: "x", review: { state: "drafted", unsent: 2 } },
    { repo: "infracost/cli", number: 77, title: "Plan v2", author: "sam", url: "x", review: { state: "running" } },
    { repo: "infracost/cli", number: 9, title: "Muted", author: "sam", url: "x", muted: true },
  ],
  notifications: [
    { id: "n1", repo: "ade", title: "Queue edits", reason: "review_requested", type: "PullRequest", url: "x" },
    { id: "n2", repo: "ade", title: "Old", reason: "mention", type: "Issue", url: "x", dismissed: true },
  ],
  linear: { issues: [{ identifier: "ADE-5", title: "Editor fork", url: "x", state: "In Progress", priority: 2 }] },
  started: { "ADE-5": { dir: "/code/ade-fork", branch: "feat/fork", session: "s1" } },
};

describe("pull requests, as ade answers them", () => {
  it("puts what you muted aside, where it can be brought back", () => {
    const muted = prs(DASH).rows.find((r) => r.id === "infracost/cli#9");
    expect(muted?.group).toBe("Put aside");
    expect(prs(DASH).rows.filter((r) => r.group !== "Put aside")).toHaveLength(3);
  });

  it("counts the drafts, because the number is what sends you back", () => {
    const row = prs(DASH).rows.find((r) => r.id.endsWith("#88"));
    expect(row?.tag).toEqual({ text: "2 unsent", kind: "unsent" });
    expect(row?.tone).toBe("bad");
  });

  it("marks a finished review as ready and a going one as reviewing", () => {
    const by = (n: number) => prs(DASH).rows.find((r) => r.id.endsWith("#" + n));
    expect(by(412)?.tag?.text).toBe("ready");
    expect(by(77)?.tag?.text).toBe("reviewing");
    expect(by(77)?.tone).toBeUndefined();
  });

  it("says where it is and who wrote it", () => {
    expect(prs(DASH).rows[0].meta).toContain("infracost/cli");
    expect(prs(DASH).rows[0].meta).toContain("@dan");
  });
});

describe("what is waiting on you", () => {
  it("is the reviews only you can move", () => {
    expect(waiting(DASH).rows.map((r) => r.id)).toEqual([
      "infracost/cli#412",
      "owenrumney/go-sarif#88",
    ]);
  });

  it("leaves out one that is still running, and the muted one", () => {
    const names = waiting(DASH).rows.map((r) => r.name).join(" ");
    expect(names).not.toContain("Plan v2");
    expect(names).not.toContain("Muted");
  });
});

const DESIGNS: Design[] = [
  { name: "one-window", dir: "/d/a", file: "/d/a/design.md", meta: { repos: ["owenrumney/ade"] }, repo: "owenrumney/ade", here: ["/code/ade"], live: true, branch: "feat/one-window" },
  { name: "retry-429", dir: "/d/b", file: "/d/b/design.md", meta: { repos: ["infracost/cli", "infracost/infra"], ticket: "FIX-623" }, repo: "infracost/cli", here: ["/code/cli"], missing: ["infracost/infra"] },
];

describe("designs, from the shared store", () => {
  it("groups by the repository each is home to", () => {
    expect(designs(DESIGNS).rows.map((r) => r.group)).toEqual([
      "Shared store · owenrumney/ade",
      "Shared store · infracost/cli",
    ]);
  });

  it("warns about one this machine cannot start, and names what is missing", () => {
    const row = designs(DESIGNS).rows[1];
    expect(row.icon).toBe("warn");
    expect(row.tone).toBe("bad");
    expect(row.meta).toContain("needs infracost/infra");
    expect(row.meta).toContain("2 repos");
    expect(row.meta).toContain("FIX-623");
  });

  it("marks the one being worked on", () => {
    expect(designs(DESIGNS).rows[0].tone).toBe("good");
    expect(designs(DESIGNS).rows[0].meta).toContain("running");
  });
});

const REPOS: Repo[] = [
  {
    name: "ade",
    path: "/code/ade",
    checkouts: [
      { path: "/code/ade", name: "ade", sessions: [{ id: "s1", name: "a", dir: "/code/ade", live: true, title: "one window" }] },
      { path: "/code/ade-old", name: "ade-old", sessions: [{ id: "s2", name: "b", dir: "/code/ade-old", archived: true }] },
    ],
  },
];

describe("sessions, flattened out of the scan", () => {
  it("leaves out what you archived", () => {
    expect(sessions(REPOS).rows.map((r) => r.id)).toEqual(["s1"]);
  });

  it("marks the one you are in", () => {
    expect(sessions(REPOS, "/code/ade").rows[0].tag).toEqual({ text: "here", kind: "here" });
    expect(sessions(REPOS, "/elsewhere").rows[0].tag).toBeUndefined();
  });

  // The running group is read while it is being written to. Ordered by
  // recency it reshuffles under the pointer, so it is ordered by name.
  it("keeps the running ones in one order however they speak", () => {
    const running: Repo[] = [
      {
        name: "ade",
        path: "/code/ade",
        checkouts: [
          {
            path: "/code/ade",
            name: "ade",
            sessions: [
              { id: "s1", name: "a", dir: "/code/ade", live: true, title: "zebra", lastUsed: "2026-10-23T12:00:00Z" },
              { id: "s2", name: "b", dir: "/code/ade", live: true, title: "apple", lastUsed: "2026-10-23T09:00:00Z" },
            ],
          },
        ],
      },
    ];
    const order = sessions(running).rows.map((r) => r.id);
    expect(order).toEqual(["s2", "s1"]);
    running[0].checkouts![0].sessions![1].lastUsed = "2026-10-23T13:00:00Z";
    expect(sessions(running).rows.map((r) => r.id)).toEqual(order);
  });

  // The scan calls a session live from its transcript's age or the process
  // table, and a sandboxed agent waiting on you is neither.
  it("runs what an ade is serving, however old the transcript", () => {
    const quiet: Repo[] = [
      {
        name: "ade",
        path: "/code/ade",
        checkouts: [
          {
            path: "/code/ade-fix",
            name: "ade-fix",
            worktree: true,
            sessions: [
              { id: "s9", name: "fix", dir: "/code/ade-fix", title: "fix it", lastUsed: "2026-10-23T09:00:00Z" },
            ],
          },
        ],
      },
    ];
    expect(sessions(quiet).rows[0].group).not.toBe("Running");
    const row = sessions(quiet, "", Date.now(), [], [{ id: "s9", dir: "/code/ade-fix" }]).rows[0];
    expect(row.group).toBe("Running");
    expect(row.acts?.[0]).toEqual({ label: "Stop…", what: "session-end", name: "s9" });
    // The point of the change: a finished one is removable from its row too.
    expect(sessions(quiet).rows[0].acts?.[0]).toMatchObject({
      label: "Put away…",
      what: "session-end",
    });
  });
});

// A row can say a session is running when nothing is serving it: the
// scan reads the process table, and an agent outside ade is in it. Stop
// then has nothing to cancel, which is not a refusal to act.
describe("a session nothing is serving", () => {
  it("is told apart from a real failure", () => {
    expect(real.unserved("no ade is serving that session")).toBe(true);
    expect(real.unserved("/api/graph/stop: no ade is running for this repository")).toBe(true);
    expect(real.unserved("the agent is wedged")).toBe(false);
    expect(real.unserved()).toBe(false);
  });
});

describe("the other two lists", () => {
  it("drops a dismissed notification", () => {
    expect(inbox(DASH).rows.map((r) => r.id)).toEqual(["n1"]);
  });

  it("marks an issue already being worked on", () => {
    expect(issues(DASH).rows[0].icon).toBe("play");
  });
});

describe("the status bar", () => {
  it("counts what is waiting", () => {
    expect(status(DASH, REPOS).waiting).toBe(2);
  });
});

describe("how long ago", () => {
  it("reads the way a glance does", () => {
    expect(ago("2026-10-22T11:59:40Z", now)).toBe("just now");
    expect(ago("2026-10-22T11:30:00Z", now)).toBe("30m");
    expect(ago("2026-10-21T12:00:00Z", now)).toBe("24h");
    expect(ago("2026-10-15T12:00:00Z", now)).toBe("7d");
  });

  it("says nothing for a stamp that did not come", () => {
    expect(ago(undefined, now)).toBe("");
    expect(ago("not a date", now)).toBe("");
  });
});

describe("the session the window lands on", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [
            { id: "old", name: "old", archived: true },
            { id: "cold", name: "cold" },
          ],
        },
        { name: "fix", path: "/r/ade-fix", sessions: [{ id: "hot", name: "hot", live: true }] },
      ],
    },
  ] as unknown as real.Repo[];

  it("prefers one that is running", () => {
    expect(real.landOn(repos)).toBe("hot");
  });

  it("falls back to the first that is not archived", () => {
    const cold = [{ ...repos[0], checkouts: [repos[0].checkouts![0]] }] as real.Repo[];
    expect(real.landOn(cold)).toBe("cold");
    expect(real.landOn([])).toBe("");
  });

  it("says which checkout each session is in", () => {
    expect([...real.whereSessionsAre(repos).entries()]).toEqual([
      ["old", "/r/ade"],
      ["cold", "/r/ade"],
      ["hot", "/r/ade-fix"],
    ]);
  });
});

describe("designs in the checkout itself", () => {
  it("lists them before the store, under the checkout's name", () => {
    const place = real.designs(DESIGNS, ["one-window.md", "retry.md"], "/code/ade");
    expect(place.rows.slice(0, 2).map((r) => [r.id, r.name, r.group])).toEqual([
      ["here:one-window.md", "one-window", "In ade"],
      ["here:retry.md", "retry", "In ade"],
    ]);
    expect(place.rows).toHaveLength(4);
  });

  it("is the store alone when no session is picked", () => {
    expect(real.designs(DESIGNS).rows).toHaveLength(2);
  });
});

describe("what the picked session is doing", () => {
  const state = {
    agent: {
      running: true,
      kind: "review",
      model: "opus",
      sandbox: true,
      queued: 2,
      sideRuns: [{ id: "a", name: "security" }, { id: "b", name: "tester" }],
    },
  };

  it("names every agent in flight", () => {
    expect(real.saying(real.doingIn(state))).toBe("@security, @tester · review · 2 queued");
    expect(real.busy(real.doingIn(state))).toBe(true);
  });

  it("is quiet when nothing runs", () => {
    expect(real.saying(real.doingIn({ agent: {} }))).toBe("");
    expect(real.busy(real.doingIn({}))).toBe(false);
    expect(real.doingIn(undefined)).toEqual(real.NOTHING);
  });

  it("carries the sandbox and the model into the status bar", () => {
    const status = real.status({}, [], real.doingIn(state));
    expect(status.sandboxed).toBe(true);
    expect(status.model).toBe("opus");
    expect(status.doing).toContain("@security");
  });
});

describe("the conversation the server is in", () => {
  it("is read beside what the agent is doing", () => {
    const doing = real.doingIn({
      agent: { running: false, provider: "claude" },
      chat: { session: "other-one" },
    });
    expect(doing.on).toBe("other-one");
    expect(doing.provider).toBe("claude");
  });

  it("is empty when the session server said nothing about it", () => {
    expect(real.doingIn({ agent: {} }).on).toBe("");
  });
});

describe("sessions, under the repository each is in", () => {
  const at = (h: number) => new Date(Date.parse("2026-10-22T12:00:00Z") - h * 3600_000).toISOString();
  const repos = [
    {
      name: "ade",
      owner: "owenrumney",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [
            { id: "old", name: "old", title: "older", dir: "/r/ade", lastUsed: at(40) },
            { id: "mid", name: "mid", title: "middling", dir: "/r/ade", lastUsed: at(5) },
            { id: "gone", name: "gone", dir: "/r/ade", archived: true, lastUsed: at(1) },
          ],
        },
        {
          name: "fix",
          path: "/r/ade-fix",
          worktree: true,
          sessions: [{ id: "hot", name: "hot", branch: "fix", dir: "/r/ade-fix", live: true, lastUsed: at(1) }],
        },
        { name: "empty", path: "/r/ade-empty", worktree: true, sessions: [] },
      ],
    },
    {
      name: "lazytrivy",
      path: "/r/lt",
      checkouts: [{ name: "lt", path: "/r/lt", sessions: [{ id: "lt1", name: "lt1", dir: "/r/lt", lastUsed: at(20) }] }],
    },
  ] as unknown as real.Repo[];

  const place = real.sessions(repos, "/r/ade-fix", Date.parse("2026-10-22T12:00:00Z"));

  it("groups by repository, so three worktrees of ade are one heading", () => {
    expect([...new Set(place.rows.map((r) => r.group))]).toEqual([
      "Running",
      "lazytrivy",
      "owenrumney/ade",
    ]);
  });

  // What is up is what you are coming back to, so it is never hunted
  // for; the rest is alphabetical, which is the only order you can
  // point at from memory.
  it("puts what is running first, then the repositories by name", () => {
    expect(place.rows.map((r) => r.id)).toEqual(["hot", "lt1", "mid", "old"]);
  });

  it("leaves out an archived session and a repository with none", () => {
    expect(place.rows.map((r) => r.id)).not.toContain("gone");
    expect(place.rows.some((r) => r.group === "ade · empty")).toBe(false);
  });

  it("names the worktree on the row, since the heading no longer does", () => {
    const hot = place.rows[0];
    expect(hot.tag).toEqual({ text: "here", kind: "here" });
    expect(hot.meta).toBe("fix · fix · 1h");
  });

  it("says nothing of a worktree for a session in the main checkout", () => {
    expect(place.rows.find((r) => r.id === "mid")?.meta).toBe("5h");
  });

  it("names the main checkout on a running row", () => {
    const up = real.sessions(
      [{ name: "ci", path: "/r/ci", checkouts: [{ name: "ci", path: "/r/ci", sessions: [{ id: "c", name: "c", dir: "/r/ci", live: true, lastUsed: at(2) }] }] }] as unknown as real.Repo[],
      "",
      Date.parse("2026-10-22T12:00:00Z"),
    );
    expect(up.rows[0].meta).toBe("ci · 2h");
  });

  it("keeps what is running in a repository outside the spaces", () => {
    const spaced = real.sessions(repos, "", Date.parse("2026-10-22T12:00:00Z"), [], [], (r) => r.name === "lazytrivy");
    expect(spaced.rows.map((r) => r.id)).toEqual(["hot", "lt1"]);
  });

  it("names a worktree once when its branch matches its name", () => {
    const same = real.sessions(
      [{ name: "ade", path: "/r/ade", checkouts: [{ name: "fix", path: "/r/f", branch: "fix", worktree: true, sessions: [{ id: "a", name: "a", branch: "fix", dir: "/r/f", lastUsed: at(2) }] }] }] as unknown as real.Repo[],
      "",
      Date.parse("2026-10-22T12:00:00Z"),
    );
    expect(same.rows[0].meta).toBe("fix · 2h");
  });
});

describe("sessions on another machine", () => {
  const machines = [
    {
      machine: "mini",
      label: "the mini",
      repos: [
        {
          name: "ade",
          path: "/far/ade",
          checkouts: [
            {
              name: "ade",
              path: "/far/ade",
              sessions: [
                { id: "far1", name: "far1", title: "Bump the pin", dir: "/far/ade", live: true },
                { id: "far0", name: "far0", title: "An older one", dir: "/far/ade", archived: true },
              ],
            },
          ],
        },
      ],
    },
  ] as unknown as real.Machine[];

  it("lists them after this machine's, under the machine", () => {
    const place = real.sessions([], "", Date.now(), machines);
    expect(place.rows.map((r) => [r.id, r.group, r.tag?.text])).toEqual([
      ["far1", "the mini", "mini"],
    ]);
  });

  it("leaves an archived one out there too", () => {
    const place = real.sessions([], "", Date.now(), machines);
    expect(place.rows.map((r) => r.id)).not.toContain("far0");
  });
});

describe("the todo list", () => {
  const now = Date.parse("2026-10-22T12:00:00Z");
  const items = [
    { id: "1", text: "Send the review replies", due: "2026-10-21T09:00:00Z" },
    { id: "2", text: "Bump the editor pin", from: "@architect", note: "monthly" },
    { id: "3", text: "Already done", doneAt: "2026-10-20T09:00:00Z" },
    { id: "4", text: "Being worked on", started: "sess-1" },
  ] as real.Todo[];

  it("leaves out what is finished", () => {
    expect(real.todos(items, now).rows.map((r) => r.id)).toEqual(["1", "2", "4"]);
  });

  it("marks one that is late, and one that is under way", () => {
    const [late, , going] = real.todos(items, now).rows;
    expect(late.tone).toBe("bad");
    expect(late.meta).toContain("overdue");
    expect(going.tone).toBe("good");
  });

  it("says who filed it", () => {
    expect(real.todos(items, now).rows[1].meta).toBe("monthly · @architect");
  });

  it("offers to start one, and not one already under way", () => {
    const [late, , going] = real.todos(items, now).rows;
    expect(late.acts).toEqual([{ label: "Start", what: "todo-start", name: "1" }]);
    expect(going.acts).toEqual([]);
  });
});

describe("why a list from the dash is empty", () => {
  it("names the read that failed", () => {
    const place = real.prs({ error: "/api/dash: 404 Not Found" });
    expect(place.rows).toEqual([]);
    expect(place.note).toBe("/api/dash — /api/dash: 404 Not Found");
  });

  it("passes on what GitHub said", () => {
    expect(real.prs({ reviewTrouble: { error: "gh: not logged in" } }).note).toBe(
      "GitHub — gh: not logged in",
    );
  });

  it("counts what the standing rules hid, so a quiet list is not a broken one", () => {
    expect(real.prs({ reviewTrouble: { hidden: 3 } }).note).toContain("3 hidden");
  });

  it("says nothing when there is nothing to say", () => {
    expect(real.prs({ reviews: [] }).note).toBe("");
  });

  it("reaches the other three lists the dash fills", () => {
    const broken = { error: "no launcher" };
    expect(real.waiting(broken).note).toContain("no launcher");
    expect(real.inbox(broken).note).toContain("no launcher");
    expect(real.issues(broken).note).toContain("no launcher");
    // Linear has a key of its own, so its own reason.
    expect(real.issues({ linear: { error: "no Linear key" } }).note).toBe("no Linear key");
  });
});

describe("your own pull requests", () => {
  const now = Date.parse("2026-10-22T12:00:00Z");
  const mine = [
    { number: 412, title: "Add usage tiles", headRef: "tiles", mine: true, checks: "fail", updatedAt: "2026-10-22T11:00:00Z" },
    { number: 9, title: "Someone else's", mine: false },
    { number: 413, title: "A draft", draft: true, mine: true, reviewDecision: "APPROVED" },
  ] as real.Listed[];

  it("come after the ones asked of you, under their own heading", () => {
    const place = real.prs({ reviews: [] }, mine, "owenrumney/ade", now);
    expect(place.rows.map((r) => [r.id, r.group])).toEqual([
      ["owenrumney/ade#412", "Yours"],
      ["owenrumney/ade#413", "Yours"],
    ]);
  });

  it("leaves out what is not yours", () => {
    const place = real.prs({}, mine, "r", now);
    expect(place.rows.some((r) => r.name.includes("Someone else"))).toBe(false);
  });

  it("says where each one stands", () => {
    const [failing, draft] = real.prs({}, mine, "r", now).rows;
    expect(failing.meta).toBe("tiles · fail · 1h");
    expect(failing.tone).toBe("bad");
    expect(draft.meta).toContain("approved");
    expect(draft.tag).toEqual({ text: "draft", kind: "draft" });
  });
});

describe("a row that does something besides open", () => {
  it("offers to start a design the machine can check out", () => {
    const [one] = real.designs([{ ...DESIGNS[0], live: false }] as Design[]).rows;
    expect(one.acts).toEqual([{ label: "Start work", what: "design-start", name: "one-window" }]);
  });

  it("offers nothing for one already being worked on", () => {
    expect(real.designs([DESIGNS[0]]).rows[0].acts).toEqual([]);
  });

  it("offers nothing for one this machine is missing a repository for", () => {
    expect(real.designs([DESIGNS[1]]).rows[0].acts).toEqual([]);
  });
});

describe("a notification", () => {
  it("knows the pull request it is about, from its url", () => {
    expect(real.prOf("https://github.com/owenrumney/ade/pull/412")).toEqual({
      repo: "owenrumney/ade",
      number: 412,
    });
    expect(real.prOf("https://github.com/owenrumney/ade/issues/9")).toBeUndefined();
    expect(real.prOf(undefined)).toBeUndefined();
  });

  it("carries both, so a row opens here or in the browser", () => {
    const dash = {
      notifications: [
        { id: "n1", repo: "o/r", title: "a pr", reason: "review_requested", type: "PullRequest", url: "https://github.com/o/r/pull/7" },
        { id: "n2", repo: "o/r", title: "a discussion", reason: "mention", type: "Discussion", url: "https://github.com/o/r/discussions/3" },
      ],
    } as real.Dash;
    const [pr, other] = real.inbox(dash).rows;
    expect(pr.pr).toEqual({ repo: "o/r", number: 7 });
    expect(other.pr).toBeUndefined();
    expect(other.url).toContain("/discussions/3");
  });
});

describe("the repository's checks, in the status bar", () => {
  it("says what is running, with how far it got", () => {
    expect(real.checksIn({ verify: { running: true, ran: 2, total: 3 } })).toBe("checks 2/3");
  });

  it("names the one that failed", () => {
    expect(real.checksIn({ verify: { failed: "go test", ran: 2, total: 3 } })).toBe("go test failed");
  });

  it("says a narrow pass is a narrow one", () => {
    expect(real.checksIn({ verify: { passed: true, narrow: true } })).toBe("checks pass (narrow)");
    expect(real.checksIn({ verify: { passed: true } })).toBe("checks pass");
  });

  it("says nothing for a repository with no checks", () => {
    expect(real.checksIn({})).toBe("");
    expect(real.checksIn({ verify: {} })).toBe("");
  });
});

describe("the team, as agentbus has it", () => {
  const now = Date.parse("2026-10-22T12:00:00Z");
  const bus: real.Bus = {
    online: true,
    offers: [{ id: "o1", kind: "review", from: "kim@x", pr: "o/r#3" }],
    replies: [{ id: "r1", from: "kim@x", text: "looks fine", at: "2026-10-22T11:00:00Z" }],
    sent: [{ id: "s1", to: "anyone", question: "who knows the sandbox?" }],
    team: [
      { user: "owen@x", presence: "active", you: true, agents: 2 },
      { user: "kim@x", presence: "idle", harness: "claude" },
    ],
  };

  it("leads with what is waiting on you", () => {
    expect(real.team(bus, now).rows.map((r) => r.group)).toEqual([
      "Offered to you",
      "Answers",
      "You asked",
      "Who is about",
      "Who is about",
    ]);
  });

  it("offers Dismiss on an offer and Clear on an answer", () => {
    const acts = real.team(bus, now).rows.map((r) => r.acts?.map((a) => `${a.label}:${a.what}`));
    expect(acts.slice(0, 2)).toEqual([["Dismiss:bus-dismiss"], ["Clear:bus-dismiss"]]);
    expect(acts.slice(2).every((a) => a === undefined)).toBe(true);
  });

  it("counts only what needs doing something with", () => {
    expect(real.team(bus, now).count).toBe(2);
  });

  it("says so when ade is not on the bus", () => {
    const place = real.team({ online: false });
    expect(place.rows).toEqual([]);
    expect(place.note).toContain("not connected to agentbus");
  });
});

describe("the jobs list", () => {
  const now = Date.parse("2026-10-22T12:00:00Z");
  const all = [
    { name: "editor-bump", schedule: "monthly", nextDue: "2026-11-01T09:00:00Z" },
    { name: "morning-brief", schedule: "weekdays at 08:00", paused: true },
    { name: "drift", schedule: "nightly", failing: true, lastFired: "2026-10-22T02:00:00Z" },
  ] as real.Job[];

  it("counts the ones that are failing", () => {
    expect(real.jobs(all, now).count).toBe(1);
  });

  it("offers to run one now, and to stop it running itself", () => {
    const [bump, paused] = real.jobs(all, now).rows;
    expect(bump.acts?.map((a) => a.label)).toEqual(["Run now", "Pause"]);
    expect(paused.acts?.map((a) => a.label)).toEqual(["Run now", "Resume"]);
  });

  it("says when it next goes, or that it is paused", () => {
    const [bump, paused, failing] = real.jobs(all, now).rows;
    expect(bump.meta).toContain("next");
    expect(paused.meta).toContain("paused");
    expect(failing.tone).toBe("bad");
  });

  it("opens recorded failures as run history", () => {
    expect(real.jobReport({
      name: "drift",
      schedule: "nightly",
      outcomes: [{ firedAt: "2026-10-22T02:00:00Z", millis: 1, error: "exit status 1" }],
    })).toContain("**Failed:** exit status 1");
  });
});

describe("the queue, under Waiting on you", () => {
  const stacked: real.Active[] = [
    {
      id: "one",
      dir: "/r/a",
      name: "a",
      repo: "ade",
      agent: {
        running: true,
        stacked: [
          { id: "q1", kind: "message", what: "look at the sandbox size" },
          { id: "q2", kind: "review" },
        ],
      },
    },
    {
      id: "two",
      dir: "/r/b",
      name: "b",
      repo: "squealer",
      agent: { running: true, stacked: [{ id: "q3", kind: "message" }] },
    },
  ];

  it("numbers them in the order they will run", () => {
    const rows = real.waiting({}, stacked).rows;
    expect(rows.map((r) => r.meta)).toEqual([
      "1 · message",
      "2 · review",
      "1 · message",
    ]);
  });

  it("says which session each is waiting on, since they are not one queue", () => {
    const rows = real.waiting({}, stacked).rows;
    expect(rows.map((r) => r.group)).toEqual([
      "Queued · ade",
      "Queued · ade",
      "Queued · squealer",
    ]);
  });

  it("numbers each session's queue from its own front", () => {
    const rows = real.waiting({}, stacked).rows;
    // q3 is third in the list and first in its own session.
    expect(rows[2].acts).toEqual([]);
  });

  it("offers to move one up, but not the one already next", () => {
    const [first, second] = real.waiting({}, stacked).rows;
    expect(first.acts).toEqual([]);
    // The session rides along: this queue is not the picked session's.
    expect(second.acts?.[0]).toEqual({
      label: "Next",
      what: "queue-first",
      name: "q2",
      on: "one",
    });
  });

  it("is not grouped when there is nothing queued", () => {
    const dash = { reviews: [{ repo: "o/r", number: 1, title: "t", author: "a", url: "", review: { state: "ready" } }] } as real.Dash;
    expect(real.waiting(dash).rows[0].group).toBeUndefined();
  });
});

describe("landing on a session with nothing live", () => {
  it("takes the one touched last, not the first repository scanned", () => {
    const repos = [
      {
        name: "old",
        path: "/r/old",
        checkouts: [{ name: "old", path: "/r/old", sessions: [{ id: "stale", name: "s", lastUsed: "2026-09-01T09:00:00Z" }] }],
      },
      {
        name: "new",
        path: "/r/new",
        checkouts: [{ name: "new", path: "/r/new", sessions: [{ id: "fresh", name: "f", lastUsed: "2026-10-22T09:00:00Z" }] }],
      },
    ] as unknown as real.Repo[];
    expect(real.landOn(repos)).toBe("fresh");
  });
});

describe("the agents tree", () => {
  const active: real.Active[] = [
    { id: "quiet", dir: "/r/a", name: "a", repo: "alpha", agent: { running: false } },
    {
      id: "busy",
      dir: "/r/b",
      name: "b",
      repo: "beta",
      branch: "feat/x",
      agent: {
        running: true,
        model: "opus",
        sideRuns: ["security"],
        children: [{ kind: "agent", name: "explore", doing: "reading web.go" }],
      },
    },
    { id: "stuck", dir: "/r/c", name: "c", repo: "gamma", agent: { running: true, asking: 1 } },
  ];

  it("names the same five states as internal/machine", () => {
    expect(real.stateOf(undefined)).toBe("idle");
    expect(real.stateOf({ error: "boom", running: true, asking: 1 })).toBe("failed");
    expect(real.stateOf({ queued: 2 })).toBe("wedged");
    expect(real.stateOf({ running: true, asking: 1 })).toBe("asking");
    expect(real.stateOf({ running: true, queued: 2 })).toBe("working");
    expect(real.stateOf({ running: true })).toBe("working");
  });

  it("goes red for a turn that is yours and not for a long one", () => {
    expect(real.attention("asking")).toBe(true);
    expect(real.attention("failed")).toBe(true);
    expect(real.attention("wedged")).toBe(true);
    expect(real.attention("working")).toBe(false);
    expect(real.attention("idle")).toBe(false);
  });

  it("keeps the sessions in name order and hangs what each called under it", () => {
    const place = real.agents(active);
    expect(place.rows.map((r) => r.name)).toEqual([
      "alpha",
      "beta",
      "@security",
      "explore",
      "gamma",
    ]);
    expect(place.rows.map((r) => r.depth ?? 0)).toEqual([0, 0, 1, 1, 0]);
    expect(place.alert).toBe(true);
    expect(place.count).toBe(1);
  });

  it("stays quiet when nothing is waiting on you", () => {
    const place = real.agents(active.filter((a) => a.id !== "stuck"));
    expect(place.alert).toBe(false);
    expect(place.count).toBe(0);
  });

  it("says a sub-agent's session in its id, so the row opens that chat", () => {
    const place = real.agents(active);
    expect(place.rows.find((r) => r.name === "explore")?.id).toBe("busy:child:explore");
  });
});

describe("keeping the session you picked", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [{ name: "ade", path: "/r/ade", sessions: [{ id: "scanned", name: "s" }] }],
    },
  ] as unknown as real.Repo[];
  const active: real.Active[] = [
    { id: "announced", dir: "/elsewhere/ade", name: "ade", repo: "ade" },
    { id: "far", dir: "/on/mini", name: "go-lsp", repo: "go-lsp", machine: "mini" },
  ];

  it("places an announced session the scan never saw, so it can be started", () => {
    const where = real.whereSessionsAre(repos, active);
    expect(where.get("announced")).toBe("/elsewhere/ade");
  });

  it("gives no path for one on another machine, since starting it is not ours", () => {
    expect(real.whereSessionsAre(repos, active).has("far")).toBe(false);
  });

  it("knows a session it cannot place, so the pick is not thrown away", () => {
    const known = real.knownSessions(repos, active);
    expect(known.has("far")).toBe(true);
    expect(known.has("scanned")).toBe(true);
    expect(known.has("nobody")).toBe(false);
  });

  it("counts another machine's listed sessions as known", () => {
    const machines = [
      {
        machine: "mini",
        repos: [
          {
            name: "go-lsp",
            path: "/on/mini",
            checkouts: [{ name: "go-lsp", path: "/on/mini", sessions: [{ id: "remote", name: "r" }] }],
          },
        ],
      },
    ] as unknown as real.Machine[];
    expect(real.knownSessions(repos, [], machines).has("remote")).toBe(true);
  });
});

describe("what a verb came back with", () => {
  it("takes ade's own sentence out of a refusal", () => {
    // The shell throws a non-ok answer, reading ade's `{error}` body.
    const got = { error: "that is not one of that checkout's sessions" };
    expect(real.answered(got).error).toBe("that is not one of that checkout's sessions");
  });

  it("hands back the body when it worked", () => {
    expect(real.answered({ agents: ["security"] } as never)).toEqual({
      error: "",
      body: { agents: ["security"] },
    });
  });

  it("takes an answer with nothing in it as success", () => {
    expect(real.answered({})).toEqual({ error: "", body: {} });
    expect(real.answered(undefined)).toEqual({ error: "", body: undefined });
  });
});

describe("keeping the session you picked", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [{ name: "ade", path: "/r/ade", sessions: [{ id: "scanned", name: "s" }] }],
    },
  ] as unknown as real.Repo[];
  const active: real.Active[] = [
    { id: "announced", dir: "/elsewhere/ade", name: "ade", repo: "ade" },
    { id: "far", dir: "/on/mini", name: "go-lsp", repo: "go-lsp", machine: "mini" },
  ];

  it("places an announced session the scan never saw, so it can be started", () => {
    const where = real.whereSessionsAre(repos, active);
    expect(where.get("announced")).toBe("/elsewhere/ade");
  });

  it("gives no path for one on another machine, since starting it is not ours", () => {
    expect(real.whereSessionsAre(repos, active).has("far")).toBe(false);
  });

  it("knows a session it cannot place, so the pick is not thrown away", () => {
    const known = real.knownSessions(repos, active);
    expect(known.has("far")).toBe(true);
    expect(known.has("scanned")).toBe(true);
    expect(known.has("nobody")).toBe(false);
  });

  it("counts another machine's listed sessions as known", () => {
    const machines = [
      {
        machine: "mini",
        repos: [
          {
            name: "go-lsp",
            path: "/on/mini",
            checkouts: [{ name: "go-lsp", path: "/on/mini", sessions: [{ id: "remote", name: "r" }] }],
          },
        ],
      },
    ] as unknown as real.Machine[];
    expect(real.knownSessions(repos, [], machines).has("remote")).toBe(true);
  });
});

describe("the checkout the window was opened on", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [{ id: "old", name: "o", dir: "/r/ade", lastUsed: "2026-10-01T09:00:00Z" }],
        },
      ],
    },
    {
      name: "squealer",
      path: "/r/sq",
      checkouts: [
        {
          name: "sq",
          path: "/r/sq",
          sessions: [{ id: "fresh", name: "f", dir: "/r/sq", lastUsed: "2026-10-22T09:00:00Z" }],
        },
      ],
    },
  ] as unknown as real.Repo[];

  it("takes the newest when the window was opened on nothing", () => {
    expect(real.landOn(repos)).toBe("fresh");
  });

  it("takes that checkout's when it was opened on one, old or not", () => {
    expect(real.landOn(repos, "/r/ade")).toBe("old");
  });

  it("falls back to the newest when the checkout has no session", () => {
    expect(real.landOn(repos, "/r/nothing")).toBe("fresh");
  });

  it("prefers a live session in that checkout over a newer dead one", () => {
    const two = [
      {
        name: "ade",
        path: "/r/ade",
        checkouts: [
          {
            name: "ade",
            path: "/r/ade",
            sessions: [
              { id: "dead", name: "d", dir: "/r/ade", lastUsed: "2026-10-22T10:00:00Z" },
              { id: "live", name: "l", dir: "/r/ade", live: true, lastUsed: "2026-10-01T09:00:00Z" },
            ],
          },
        ],
      },
    ] as unknown as real.Repo[];
    expect(real.landOn(two, "/r/ade")).toBe("live");
  });
});

describe("issues, grouped by project", () => {
  const issue = (identifier: string, project?: string, started = false) =>
    ({ identifier, title: "t", url: "", state: started ? "In Progress" : "Todo", project }) as never;

  const place = (all: unknown[]) =>
    issues({ linear: { issues: all } } as never);

  it("files each issue under its project", () => {
    const got = place([issue("A-1", "Pricing"), issue("B-2", "Onboarding")]);
    expect(got.rows.map((r) => [r.id, r.group])).toEqual([
      ["A-1", "Pricing"],
      ["B-2", "Onboarding"],
    ]);
  });

  it("keeps a project's issues together, in the order they came", () => {
    const got = byProject([
      issue("A-1", "Pricing"),
      issue("B-1", "Onboarding"),
      issue("A-2", "Pricing"),
    ]);
    expect(got.map((i) => i.identifier)).toEqual(["A-1", "A-2", "B-1"]);
  });

  // The server sorts in-progress and urgent first, so the first heading is
  // the project the most pressing issue is in.
  it("orders the projects by their first issue", () => {
    const got = byProject([issue("B-1", "Onboarding"), issue("A-1", "Pricing")]);
    expect(got.map((i) => i.project)).toEqual(["Onboarding", "Pricing"]);
  });

  it("puts issues in no project last, under a heading that says so", () => {
    const got = place([issue("X-1"), issue("A-1", "Pricing")]);
    expect(got.rows.map((r) => [r.id, r.group])).toEqual([
      ["A-1", "Pricing"],
      ["X-1", "No project"],
    ]);
  });
});

// "In Progress" is Linear's word for the ticket; the play is this machine
// being on its branch. The two disagree often, so the row has to say which.
describe("what an issue's icon means", () => {
  const dash = {
    linear: {
      issues: [
        { identifier: "A-1", title: "t", url: "", state: "In Progress" },
        { identifier: "A-2", title: "t", url: "", state: "In Progress" },
      ],
    },
    started: { "A-2": { dir: "/c/ade", branch: "a-2" } },
  } as never;

  it("marks only the one checked out here, and names the mark", () => {
    const [notHere, here] = issues(dash).rows;
    expect(notHere.icon).toBe("issue");
    expect(notHere.tag).toBeUndefined();
    expect(here.icon).toBe("play");
    expect(here.tag).toEqual({ text: "checked out", kind: "here" });
  });

  it("keeps Linear's own state in the meta either way", () => {
    for (const row of issues(dash).rows) expect(row.meta).toContain("In Progress");
  });
});

describe("the conversation serving one checkout", () => {
  const where = new Map([
    ["older", "/r/ade"],
    ["pending-abc", "/r/infra-pr-1535"],
  ]);

  it("is the one announced for that directory", () => {
    expect(real.servingDir(where, "/r/infra-pr-1535")).toBe("pending-abc");
  });

  // The trap: landOn() answers with any session when the directory has
  // none, which would land a new review on the conversation you left.
  it("is nothing while that checkout has announced none", () => {
    expect(real.servingDir(where, "/r/infra-pr-9999")).toBe("");
    expect(real.servingDir(new Map(), "/r/infra-pr-1535")).toBe("");
    expect(real.servingDir(where, "")).toBe("");
  });
});

describe("the Team place with no bus", () => {
  it("says why, where the launcher knows", () => {
    expect(real.team({ online: false, why: 'tailscale status said "The Tailscale GUI failed to start"' }).note)
      .toBe('ade is not connected to agentbus. tailscale status said "The Tailscale GUI failed to start"');
  });

  it("says only that it is not connected when nothing said why", () => {
    expect(real.team({ online: false }).note).toBe("ade is not connected to agentbus.");
    expect(real.team(undefined).note).toBe("ade is not connected to agentbus.");
  });

  it("says nothing at all once it is connected", () => {
    expect(real.team({ online: true, why: "stale" }).note).toBe("");
  });
});

describe("a checkout an ade is serving with nothing said in it", () => {
  const repos = [
    {
      owner: "infracost",
      name: "infra",
      path: "/r/infra",
      checkouts: [
        { path: "/r/infra", name: "infra", branch: "master", sessions: [
          { id: "old", dir: "/r/infra", name: "infra", branch: "master", lastUsed: "2026-10-16T13:54:33Z" },
        ] },
        { path: "/r/infra-pr-1535", name: "infra-pr-1535", branch: "feat/x", worktree: true, sessions: null },
      ],
    },
  ] as unknown as real.Repo[];
  const up = [
    { id: "pending-abc", dir: "/r/infra-pr-1535", name: "infra-pr-1535", repo: "infra", branch: "feat/x" },
  ] as real.Active[];

  // Opening a pull request makes a worktree and an ade in it, and names
  // no conversation. Without a row there is nothing to pick, so Review
  // has nothing to follow — which is the whole point of opening it.
  it("is a row, so it can be picked", () => {
    const row = real.sessions(repos, "", Date.now(), [], up).rows.find((r) => r.id === "pending-abc");
    expect(row?.name).toBe("feat/x");
    expect(row?.meta).toBe("infra-pr-1535 · no conversation yet");
    // An ade is up in it, so it belongs with what is up.
    expect(row?.group).toBe("Running");
    expect(row?.acts?.[0]).toMatchObject({ label: "Stop…", what: "session-end" });
  });

  // ade create names the worktree after the branch, so the heading has
  // already said it; the repeat pushed "no conversation yet" off the end.
  it("does not say the worktree's name twice", () => {
    const same = JSON.parse(JSON.stringify(repos)) as real.Repo[];
    same[0].checkouts![1] = {
      ...same[0].checkouts![1],
      name: "ci-check-quota-issues",
      path: "/r/ci-check-quota-issues",
      branch: "ci-check-quota-issues",
    };
    const here = [
      { id: "pending-abc", dir: "/r/ci-check-quota-issues", name: "x", repo: "infra", branch: "ci-check-quota-issues" },
    ] as real.Active[];
    const row = real.sessions(same, "", Date.now(), [], here).rows.find((r) => r.id === "pending-abc");
    expect(row?.name).toBe("ci-check-quota-issues");
    expect(row?.meta).toBe("no conversation yet");
  });

  it("is not a second row once something is said in it", () => {
    const spoken = JSON.parse(JSON.stringify(repos)) as real.Repo[];
    spoken[0].checkouts![1].sessions = [
      { id: "real", dir: "/r/infra-pr-1535", name: "infra-pr-1535", branch: "feat/x" },
    ] as never;
    const ids = real.sessions(spoken, "", Date.now(), [], up).rows.map((r) => r.id);
    expect(ids).toContain("real");
    expect(ids).not.toContain("pending-abc");
  });

  it("is nothing when no ade is serving that checkout", () => {
    const ids = real.sessions(repos, "", Date.now(), [], []).rows.map((r) => r.id);
    expect(ids).toEqual(["old"]);
  });
});

describe("what one catch-up did", () => {
  it("says nothing happened when there was nothing to replay", () => {
    expect(real.caughtUp({ base: "origin/main", behind: 0, replayed: 0 }))
      .toBe("already on origin/main");
  });

  it("says what it replayed and what it settled", () => {
    expect(real.caughtUp({
      base: "origin/main", behind: 6, replayed: 2,
      resolved: ["a.go", "b.go"], by: "haiku", stashed: true,
    })).toBe("replayed 2 on origin/main; haiku settled 2 conflict(s)");
  });

  // Markers left in the tree need you before anything else does, so they are
  // the whole of what it says.
  it("leads with the work that came back conflicted", () => {
    expect(real.caughtUp({
      base: "origin/main", behind: 6, replayed: 2, stashed: true,
      resolved: ["a.go"], by: "haiku", stuck: ["c.go"],
    })).toBe("replayed 2 on origin/main; haiku settled 1 conflict(s) — your work came back with conflicts in c.go");
  });

  it("says when the base it replayed on may be old", () => {
    expect(real.caughtUp({ base: "main", behind: 1, replayed: 1, stale: "no origin" }))
      .toContain("the base could not be fetched");
  });
});

describe("the conversation with one person", () => {
  const now = Date.parse("2026-10-23T12:00:00Z");
  const said: real.Said[] = [
    { at: "2026-10-23T10:00:00Z", id: "q1", kind: "ask", mine: true, question: "Is dev RDS safe to resize?", options: ["yes", "no"] },
    { at: "2026-10-23T11:00:00Z", id: "q1", kind: "answer", option: "yes", text: "after 6pm" },
    { at: "2026-10-23T11:30:00Z", id: "r1", kind: "review", agent: true, pr: "infracost/infra#9" },
  ];

  it("reads as who said what, oldest first", () => {
    const page = real.historyPage("liam@infracost.io", said, now);
    const you = page.indexOf("**you**");
    const liam = page.indexOf("**liam@infracost.io**");
    const agent = page.indexOf("**liam@infracost.io's agent**");
    expect(you).toBeGreaterThan(0);
    expect(liam).toBeGreaterThan(you);
    expect(agent).toBeGreaterThan(liam);
    expect(page).toContain("> Is dev RDS safe to resize?");
    expect(page).toContain("Options: yes · no");
    expect(page).toContain("yes — after 6pm");
    expect(page).toContain("Asked for a review of infracost/infra#9");
  });

  it("says where the history comes from when there is none", () => {
    expect(real.historyPage("sam@x.io", [], now)).toContain("Nothing said yet");
  });
});

// An older ade with no draft route answers 405. Counted as zeros, that read
// "nothing to say yet" over thirteen findings.
describe("where the review stands, when it cannot be read", () => {
  it("says the read failed rather than that there is nothing", () => {
    const note = real.reviewNote({ inDraft: 0, ready: 0, notGoing: 0, error: "405 Method Not Allowed" });
    expect(note).toBe("Could not read the review: 405 Method Not Allowed");
  });
  it("says nothing at all when there really is nothing", () => {
    expect(real.reviewNote({ inDraft: 0, ready: 0, notGoing: 0 })).toBe("");
  });
});

// GitHub answers 200 and makes nothing of a comment on a line the diff does
// not touch. The press has to say so, or the comment is simply gone.
describe("what an add to the review said", () => {
  it("names what was refused before what went", () => {
    expect(real.draftSaid({ drafted: 2 }, 7)).toBe("2 in the draft review on #7");
    expect(real.draftSaid({ drafted: 2, refused: ["a.go:30 is not in the diff"] }, 7)).toBe(
      "1 refused: a.go:30 is not in the diff · 2 in the draft review on #7",
    );
  });

  it("says only the reason when nothing went at all", () => {
    expect(real.draftSaid({ drafted: 0, refused: ["a.go:30 is not in the diff"] }, 7)).toBe(
      "a.go:30 is not in the diff",
    );
    expect(real.draftSaid({ drafted: 0, refused: ["one", "two"] }, 7)).toBe("2 refused: one");
  });
});

// One verb on an agent row covers the whole session: the run, the ade under
// it, and whether the conversation stays in the lists at all.
describe("ending a session from the agents list", () => {
  const working: real.Active[] = [
    { id: "s1", dir: "/code/ade", name: "ade", agent: { running: true } },
  ];

  it("offers the row the verb, named for what it will do first", () => {
    expect(real.agents(working).rows[0].acts).toEqual([
      { label: "Stop…", what: "session-end", name: "s1" },
    ]);
    const idle: real.Active[] = [{ id: "s1", dir: "/code/ade", name: "ade" }];
    expect(real.agents(idle).rows[0].acts?.[0].label).toBe("Put away…");
  });

  // Another machine's session is stopped where it runs, not from here.
  it("offers nothing on a row from elsewhere", () => {
    const away: real.Active[] = [{ id: "s2", dir: "/code/ade", name: "ade", machine: "mini" }];
    expect(real.agents(away).rows[0].acts).toEqual([]);
  });

  it("only offers to stop a run when there is one", () => {
    expect(real.endings("working").map((e) => e.value)).toEqual([
      "agent",
      "ade",
      "done",
      "archive",
      "delete",
    ]);
    expect(real.endings("idle").map((e) => e.value)).toEqual(["ade", "done", "archive", "delete"]);
  });

  it("offers only the stops when no conversation is behind the row", () => {
    expect(real.endings("idle", false).map((e) => e.value)).toEqual(["ade"]);
    expect(real.endings("working", false).map((e) => e.value)).toEqual(["agent", "ade"]);
    expect(real.endings("idle", false)[0].note).toMatch(/nothing has been said/);
  });

  it("knows which ids have a transcript behind them", () => {
    const repos = [
      { name: "ade", path: "/r/ade", checkouts: [{ path: "/r/ade", name: "ade", sessions: [{ id: "s1" }] }] },
    ] as unknown as real.Repo[];
    expect(real.spoken(repos, "s1")).toBe(true);
    expect(real.spoken(repos, "pending-abc")).toBe(false);
    expect(real.spoken([], "s1")).toBe(false);
  });

  it("does not claim it removed something it did not", () => {
    expect(real.ended("delete", { removed: 0 })).toMatch(/nothing to delete/);
    expect(real.ended("delete", { removed: 1 })).toBe("deleted");
    expect(real.ended("archive", { changed: 0 })).toMatch(/nothing to archive/);
    expect(real.ended("archive", { changed: 2 })).toBe("archived");
    expect(real.ended("ade")).toBe("ade stopped here");
  });
});

// The set has to empty as reads agree, or a session archived and brought back
// would stay invisible.
describe("what this window has archived but not yet read back", () => {
  const repos: real.Repo[] = [
    {
      name: "ade",
      owner: "owenrumney",
      path: "/code/ade",
      checkouts: [
        {
          path: "/code/ade",
          name: "ade",
          sessions: [
            { id: "listed", name: "still here", dir: "/code/ade" },
            { id: "agreed", name: "put away", dir: "/code/ade", archived: true },
          ],
        },
      ],
    },
  ];

  it("keeps an id the read still lists as live", () => {
    expect([...real.stillListed(repos, new Set(["listed"]))]).toEqual(["listed"]);
  });

  it("forgets one the read now calls archived", () => {
    expect(real.stillListed(repos, new Set(["agreed"])).size).toBe(0);
  });

  it("forgets one the read has stopped listing at all", () => {
    expect(real.stillListed(repos, new Set(["vanished"])).size).toBe(0);
  });

  it("does nothing when nothing is waiting", () => {
    expect(real.stillListed(repos, new Set()).size).toBe(0);
  });
});
