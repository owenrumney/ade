import { describe, expect, it } from "vitest";
import * as real from "./real";
import { ago, designs, inbox, issues, prs, sessions, status, waiting } from "./real";
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
  it("leaves out what you muted", () => {
    expect(prs(DASH).rows.map((r) => r.id)).not.toContain("infracost/cli#9");
    expect(prs(DASH).rows).toHaveLength(3);
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
      "owenrumney/ade",
      "lazytrivy",
    ]);
  });

  it("puts the repository touched last first, and the same inside it", () => {
    expect(place.rows.map((r) => r.id)).toEqual(["hot", "mid", "old", "lt1"]);
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
    expect(place.rows[1].meta).toBe("5h");
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

  it("counts only what needs doing something with", () => {
    expect(real.team(bus, now).count).toBe(2);
  });

  it("says so when the session is not on the bus", () => {
    const place = real.team({ online: false });
    expect(place.rows).toEqual([]);
    expect(place.note).toContain("not on agentbus");
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

  it("puts what wants you first and hangs what it called under it", () => {
    const place = real.agents(active);
    expect(place.rows.map((r) => r.name)).toEqual([
      "gamma",
      "beta",
      "@security",
      "explore",
      "alpha",
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
