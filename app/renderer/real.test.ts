import { describe, expect, it } from "vitest";
import * as real from "./real";
import { ago, byProject, inbox, issues, prs, sessions, status, waiting } from "./real";
import type { Dash, Repo } from "./real";

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

describe("what a pull request's line does besides open", () => {
  // Opening one checks it out as a session, so the website is its own act.
  it("offers the website, and to put one asked of you aside", () => {
    expect(prs(DASH).rows[0].acts).toEqual([
      { label: "Web", what: "pr-web", name: "x" },
      { label: "Put aside", what: "pr-mute", name: "infracost/cli#412" },
    ]);
  });

  it("offers to bring back one that was put aside", () => {
    const muted = prs(DASH).rows.find((r) => r.id === "infracost/cli#9");
    expect(muted?.acts?.[1]).toEqual({ label: "Bring back", what: "pr-unmute", name: "infracost/cli#9" });
  });

  it("offers only the website on one of yours, when it has an address", () => {
    const mine = [
      { number: 1, title: "a", mine: true, url: "https://github.com/o/r/pull/1" },
      { number: 2, title: "b", mine: true },
    ] as real.Listed[];
    const [first, second] = prs({}, mine, "o/r", now).rows;
    expect(first.acts).toEqual([{ label: "Web", what: "pr-web", name: "https://github.com/o/r/pull/1" }]);
    expect(second.acts).toEqual([]);
  });
});

describe("Notifications, what is waiting on you", () => {
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

  it("is called Notifications, under the bell", () => {
    expect(waiting(DASH)).toMatchObject({ id: "waiting", title: "Notifications", icon: "bell" });
  });

  it("is not grouped, and paints nothing, with no offer from the bus", () => {
    const place = waiting(DASH);
    expect(place.rows.every((r) => r.group === undefined)).toBe(true);
    expect(place.alert).toBe(false);
  });

  // Grouped only when there is a second list to tell the reviews from.
  it("leads with what teammates asked, and paints the bell for it", () => {
    const bus: real.Bus = { offers: [{ id: "o1", kind: "ask", from: "liam", question: "Ship it?" }] };
    const place = waiting(DASH, bus);
    expect(place.rows.map((r) => [r.id, r.group])).toEqual([
      ["o1", "Offered to you"],
      ["infracost/cli#412", "Yours to move"],
      ["owenrumney/go-sarif#88", "Yours to move"],
    ]);
    expect(place.alert).toBe(true);
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

describe("sessions, the list on the left", () => {
  const at = (h: number) => new Date(now - h * 3600_000).toISOString();
  const repos: Repo[] = [
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
            { id: "mid", name: "mid", title: "middling", dir: "/r/ade", agent: "codex", lastUsed: at(5) },
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
  ];
  const place = sessions(repos, now);

  it("is one flat list, with no headings to fold", () => {
    expect(place).toMatchObject({ id: "sessions", title: "Sessions" });
    expect(place.rows.every((r) => r.group === undefined)).toBe(true);
  });

  // By name, running or not: the only order you can point at from memory.
  it("is in name order, whichever repository each is in", () => {
    expect(place.rows.map((r) => r.name)).toEqual(["fix", "lt1", "middling", "older"]);
    expect(place.rows.map((r) => r.id)).toEqual(["hot", "lt1", "mid", "old"]);
  });

  it("leaves out what you archived", () => {
    expect(place.rows.map((r) => r.id)).not.toContain("gone");
    expect(sessions(REPOS).rows.map((r) => r.id)).toEqual(["s1"]);
  });

  it("says the repository, the worktree, the agent, and how long ago", () => {
    const by = (id: string) => place.rows.find((r) => r.id === id);
    expect(by("old")?.meta).toBe("ade · 40h");
    expect(by("mid")?.meta).toBe("ade · codex · 5h");
    expect(by("lt1")?.meta).toBe("lazytrivy · 20h");
  });

  // A live one's badge says it is live, so the meta does not say when.
  it("names the worktree and leaves the age off a live one", () => {
    expect(place.rows.find((r) => r.id === "hot")?.meta).toBe("ade · fix");
  });

  it("names a worktree once when it is called what the repository is", () => {
    const same: Repo[] = [
      {
        name: "ade",
        path: "/r/ade",
        checkouts: [{ name: "ade", path: "/r/f", worktree: true, sessions: [{ id: "a", name: "a", dir: "/r/f", lastUsed: at(2) }] }],
      },
    ];
    expect(sessions(same, now).rows[0].meta).toBe("ade · 2h");
  });

  // Ordered by recency the list reshuffles under the pointer as agents write.
  it("keeps one order however the sessions speak", () => {
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

  it("tells two sessions of one name apart by id, the same way every time", () => {
    const twins: Repo[] = [
      {
        name: "ade",
        path: "/r/ade",
        checkouts: [
          {
            name: "ade",
            path: "/r/ade",
            sessions: [
              { id: "b", name: "same", dir: "/r/ade" },
              { id: "a", name: "same", dir: "/r/ade" },
            ],
          },
        ],
      },
    ];
    expect(sessions(twins, now).rows.map((r) => r.id)).toEqual(["a", "b"]);
  });

  // The scan calls a session live from the process table; an ade serving it
  // says so however old its last word is.
  it("badges what an ade is serving as live, however long it has been quiet", () => {
    const quiet = place.rows.find((r) => r.id === "old");
    expect([quiet?.icon, quiet?.tone]).toEqual(["history", undefined]);
    const served = sessions(repos, now, [{ id: "old", dir: "/r/ade", name: "old" }]).rows.find((r) => r.id === "old");
    expect([served?.icon, served?.tone]).toEqual(["play", "good"]);
    expect(served?.meta).toBe("ade");
  });
});

describe("a session's tags", () => {
  const tagged: Repo[] = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [
            { id: "t", name: "tagged", dir: "/r/ade", ticket: "DEV-12", brief: "/r/ade/.git/ade/docs/lean.md", tags: ["urgent", "api"] },
            { id: "p", name: "plain", dir: "/r/ade" },
          ],
        },
      ],
    },
  ];

  it("are its ticket, its brief, then the ones typed by hand", () => {
    expect(real.tagsOf(tagged[0].checkouts![0].sessions![0])).toEqual([
      { text: "DEV-12", kind: "ticket" },
      { text: "brief", kind: "brief" },
      { text: "urgent", kind: "own" },
      { text: "api", kind: "own" },
    ]);
  });

  it("are none for a session ade recorded nothing about", () => {
    expect(real.tagsOf(tagged[0].checkouts![0].sessions![1])).toEqual([]);
  });

  it("sit on the session's line in the list", () => {
    const [plain, withTags] = sessions(tagged, now).rows;
    expect(plain.tags).toEqual([]);
    expect(withTags.tags?.map((t) => t.text)).toEqual(["DEV-12", "brief", "urgent", "api"]);
  });

  // Only the tags typed by hand are the box's to edit: the rest are ade's.
  it("hand the box only the ones typed by hand", () => {
    expect(real.ownTags(tagged, "t")).toEqual(["urgent", "api"]);
    expect(real.ownTags(tagged, "p")).toEqual([]);
    expect(real.ownTags(tagged, "nobody")).toEqual([]);
  });

  it("name the brief by its path, or nothing when there is none", () => {
    expect(real.briefOf(tagged, "t")).toBe("/r/ade/.git/ade/docs/lean.md");
    expect(real.briefOf(tagged, "p")).toBe("");
    expect(real.briefOf(tagged, "nobody")).toBe("");
  });

  it("are read from the box with commas or spaces between, once each, without the hash", () => {
    expect(real.typedTags("  #urgent, api  urgent\nfix ")).toEqual(["urgent", "api", "fix"]);
    expect(real.typedTags("")).toEqual([]);
    expect(real.typedTags(" , ")).toEqual([]);
  });
});

describe("what a session's line does besides open", () => {
  const repos: Repo[] = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [
            { id: "b", name: "briefed", dir: "/r/ade", brief: "/r/ade/.git/ade/docs/lean.md" },
            { id: "p", name: "plain", dir: "/r/ade" },
          ],
        },
      ],
    },
  ];
  const [briefed, plain] = sessions(repos, now).rows;

  // The rest would crowd the line: they are the context menu's.
  it("shows Close on hover and keeps Tags to the menu", () => {
    expect(plain.acts).toEqual([
      { label: "Close", what: "session-end", name: "p" },
      { label: "Tags…", what: "session-tags", name: "p", menu: true },
    ]);
  });

  it("offers the brief only on a session that has one", () => {
    expect(briefed.acts?.map((a) => [a.label, a.what, Boolean(a.menu)])).toEqual([
      ["Close", "session-end", false],
      ["Tags…", "session-tags", true],
      ["Open the brief", "brief-open", true],
      ["Tell the agent to read the brief", "brief-send", true],
    ]);
    expect(briefed.acts?.every((a) => a.name === "b")).toBe(true);
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
    expect(ago("0001-01-01T00:00:00Z", now)).toBe("");
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

describe("what the picked session is doing", () => {
  const state = {
    agent: {
      running: true,
      kind: "review",
      sideRuns: [{ id: "a", name: "security" }, { id: "b", name: "tester" }],
    },
  };

  it("names every agent in flight", () => {
    expect(real.saying(real.doingIn(state))).toBe("@security, @tester · review");
    expect(real.busy(real.doingIn(state))).toBe(true);
  });

  it("is quiet when nothing runs", () => {
    expect(real.saying(real.doingIn({ agent: {} }))).toBe("");
    // Whose pull request it is decides which buttons the Review bar offers.
    expect(real.doingIn({ agent: {}, branchPr: { number: 34, mine: false } }).prMine).toBe(false);
    expect(real.doingIn({ agent: {}, branchPr: { number: 7, mine: true } }).prMine).toBe(true);
    expect(real.doingIn({ agent: {} }).prMine).toBeUndefined();
    expect(real.busy(real.doingIn({}))).toBe(false);
    expect(real.doingIn(undefined)).toEqual(real.NOTHING);
  });

  // The agent's own terminal runs no turn of ade's, and is still at work.
  it("counts an agent working in its own terminal as busy", () => {
    const doing = real.doingIn({ agent: { terminal: "working" } });
    expect(doing.terminal).toBe("working");
    expect(real.busy(doing)).toBe(true);
    expect(real.busy(real.doingIn({ agent: { terminal: "needs" } }))).toBe(false);
    expect(real.saying(doing)).toBe("");
  });

  it("reads the scope, the branch, the pull request and how far behind", () => {
    const doing = real.doingIn({
      meta: { scope: "branch", branch: "feat/x" },
      branchPr: { number: 7 },
      worktree: { behind: 3 },
    });
    expect(doing).toMatchObject({ scope: "branch", branch: "feat/x", pr: 7, behind: 3 });
  });

  it("carries all of it into the status bar", () => {
    const doing = real.doingIn({ ...state, meta: { scope: "branch", branch: "feat/x" }, branchPr: { number: 7 } });
    const bar = real.status({}, [], doing);
    expect(bar.doing).toContain("@security");
    expect(bar).toMatchObject({ scope: "branch", branch: "feat/x", pr: 7 });
  });

  // The session's own branch wins: the scan's is the checkout's.
  it("falls back to the live checkout's branch when the session names none", () => {
    const repos = [
      { name: "ade", path: "/r", checkouts: [{ name: "ade", path: "/r", branch: "main", sessions: [{ id: "s", name: "s", dir: "/r", live: true }] }] },
    ] as Repo[];
    expect(real.status({}, repos).branch).toBe("main");
  });

  it("counts what teammates asked in what is waiting", () => {
    const bus: real.Bus = { offers: [{ id: "o1", kind: "ask", from: "liam", question: "Ship it?" }] };
    expect(real.status(DASH, REPOS, real.NOTHING, bus).waiting).toBe(3);
  });
});

describe("whether an agent is in the middle of something", () => {
  it("is when it is working or asking, which is when closing asks first", () => {
    expect(real.running("working")).toBe(true);
    expect(real.running("asking")).toBe(true);
    expect(real.running("idle")).toBe(false);
    expect(real.running("failed")).toBe(false);
  });
});

describe("the window with no launcher behind it", () => {
  it("says why where the sessions would be, and counts nothing", () => {
    const world = real.offline("ade is not running. Start it from a checkout.");
    expect(world.places.map((p) => p.id)).toEqual(["sessions"]);
    expect(world.places[0].rows[0]).toMatchObject({ tone: "bad", name: "ade is not running. Start it from a checkout." });
    expect(world.status.waiting).toBe(0);
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

  it("offers Claim and Dismiss on a review asked of you, and Clear on an answer", () => {
    const acts = real.team(bus, now).rows.map((r) => r.acts?.map((a) => `${a.label}:${a.what}`));
    expect(acts.slice(0, 2)).toEqual([["Claim:bus-claim", "Dismiss:bus-dismiss"], ["Clear:bus-dismiss"]]);
    expect(acts.slice(2).every((a) => a === undefined)).toBe(true);
  });

  it("counts only what needs doing something with", () => {
    expect(real.team(bus, now).count).toBe(2);
  });

  // agentbus reports an instance. Two ades signed in as the same person put
  // that person in the list twice, both rows opening the same conversation.
  it("is one row per person, however many ades they are signed in on", () => {
    const twice: real.Bus = {
      online: true,
      team: [
        { user: "owen@x", presence: "idle", harness: "claude", node: "mac", you: true, agents: 1 },
        { user: "owen@x", presence: "active", harness: "claude", node: "pi", you: true, agents: 2, status: "an agent is working" },
      ],
    };
    const rows = real.team(twice, now).rows;
    expect(rows).toHaveLength(1);
    expect(rows[0].id).toBe("who:owen@x");
    expect(rows[0].name).toBe("owen@x · you");
  });

  it("takes the busiest instance's line, and counts the rest", () => {
    const twice: real.Bus = {
      online: true,
      team: [
        { user: "owen@x", presence: "idle", node: "mac", agents: 1 },
        { user: "owen@x", presence: "active", node: "pi", agents: 2, status: "an agent is working" },
      ],
    };
    const row = real.team(twice, now).rows[0];
    expect(row.tone).toBe("good");
    expect(row.meta).toContain("an agent is working");
    expect(row.meta).toContain("3 running");
    expect(row.meta).toContain("2 machines");
  });

  it("says nothing about machines for a person on one", () => {
    expect(real.team(bus, now).rows[3].meta).not.toContain("machines");
  });

  it("says so when ade is not on the bus", () => {
    const place = real.team({ online: false });
    expect(place.rows).toEqual([]);
    expect(place.note).toContain("not connected to agentbus");
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


describe("the session a restarted window opens on", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        { name: "ade", path: "/r/ade", sessions: [{ id: "cold", name: "cold", dir: "/r/ade", lastUsed: "2026-11-01T09:00:00Z" }] },
        { name: "fix", path: "/r/ade-fix", sessions: [{ id: "hot", name: "hot", dir: "/r/ade-fix", live: true }] },
      ],
    },
  ] as unknown as real.Repo[];
  const known = new Set(["cold", "hot"]);

  it("goes back to the one the last run was in", () => {
    expect(real.landAgain(repos, known, "", "cold")).toBe("cold");
  });

  it("falls back when that conversation is gone, or there was none", () => {
    expect(real.landAgain(repos, known, "", "closed")).toBe("hot");
    expect(real.landAgain(repos, known, "", "")).toBe("hot");
  });

  // `ade .` names a checkout on purpose, which outranks where you were.
  it("yields to the checkout ade was opened on", () => {
    expect(real.landAgain(repos, known, "/r/ade", "hot")).toBe("cold");
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
    { id: "nowhere", dir: "", name: "x" },
  ];

  it("places an announced session the scan never saw, so it can be started", () => {
    const where = real.whereSessionsAre(repos, active);
    expect(where.get("announced")).toBe("/elsewhere/ade");
    expect(where.get("scanned")).toBe("/r/ade");
  });

  it("gives no path for one that announced no directory", () => {
    expect(real.whereSessionsAre(repos, active).has("nowhere")).toBe(false);
  });

  it("knows a session it cannot place, so the pick is not thrown away", () => {
    const known = real.knownSessions(repos, active);
    expect(known.has("nowhere")).toBe(true);
    expect(known.has("scanned")).toBe(true);
    expect(known.has("nobody")).toBe(false);
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

  // Opening a pull request makes a worktree and an ade in it. Without a
  // line there is nothing to pick, so the Diff has nothing to follow.
  it("is a line, so it can be picked", () => {
    const row = sessions(repos, now, up).rows.find((r) => r.id === "pending-abc");
    expect(row?.name).toBe("feat/x");
    expect(row?.meta).toBe("infra · starting");
    expect([row?.icon, row?.tone]).toEqual(["play", "good"]);
    expect(row?.acts?.[0]).toEqual({ label: "Close", what: "session-end", name: "pending-abc" });
  });

  it("is named by its checkout, then its id, when it announced no branch", () => {
    const bare = [{ id: "pending-x", dir: "/r/x", name: "x-checkout" }, { id: "pending-y", dir: "/r/y", name: "" }];
    expect(sessions([], now, bare).rows.map((r) => r.name)).toEqual(["pending-y", "x-checkout"]);
  });

  it("is not a second line once the list names it", () => {
    const listed = JSON.parse(JSON.stringify(repos)) as real.Repo[];
    listed[0].checkouts![1].sessions = [
      { id: "pending-abc", dir: "/r/infra-pr-1535", name: "infra-pr-1535", branch: "feat/x" },
    ] as never;
    const ids = sessions(listed, now, up).rows.map((r) => r.id);
    expect(ids.filter((id) => id === "pending-abc")).toHaveLength(1);
  });

  it("is nothing when no ade is serving that checkout", () => {
    expect(sessions(repos, now, []).rows.map((r) => r.id)).toEqual(["old"]);
  });
});

describe("what one catch-up did", () => {
  it("explains an absent brief in the tab, not just the status line", () => {
    const body = real.noBrief("this checkout has no brief under .git/ade/docs");
    expect(body).toContain("# Brief");
    expect(body).toContain("this checkout has no brief under .git/ade/docs.");
    expect(body).toContain("newest file under `.git/ade/docs`");
  });

  it("says what a pull of the branch brought", () => {
    expect(real.pulled({ branch: "origin/feature", pulled: 0 })).toBe("already up to date with origin/feature");
    expect(real.pulled({ branch: "origin/feature", pulled: 1 })).toBe("pulled 1 commit from origin/feature");
    expect(real.pulled({ branch: "origin/feature", pulled: 3 })).toBe("pulled 3 commits from origin/feature");
  });

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

// "removeAllComments done" was the whole of the feedback.
describe("what a comment sweep reports", () => {
  it("counts what went", () => {
    expect(real.sweptSaid({ removed: 6, kept: 0 }, "comments")).toBe("removed 6 comments");
    expect(real.sweptSaid({ removed: 1, kept: 0 }, "comments")).toBe("removed 1 comment");
  });

  it("says what it had to leave, and why", () => {
    expect(real.sweptSaid({ removed: 6, kept: 4 }, "comments")).toBe(
      "removed 6 comments; 4 on the pull request stay",
    );
  });

  // Nothing removed and four left is not the same as nothing to remove.
  it("tells an empty sweep from one that could take none of them", () => {
    expect(real.sweptSaid({ removed: 0, kept: 0 }, "orphaned comments")).toBe(
      "no orphaned comments to remove",
    );
    expect(real.sweptSaid({ removed: 0, kept: 4 }, "comments")).toBe(
      "nothing to remove: 4 on the pull request stay",
    );
  });
});

describe("My Day, behind the calendar icon", () => {
  const at = Date.parse("2026-11-02T12:00:00Z");
  const job = (outcomes: real.Outcome[]): real.Job => ({ name: "my-day", outcomes });

  it("is found by the name the built-in report has", () => {
    expect(real.myDay([job([]), { name: "other" }])?.name).toBe("my-day");
    expect(real.myDay([{ name: "other" }])).toBeUndefined();
  });

  it("draws the newest build and not the ones before it", () => {
    const body = real.myDayReport(
      job([
        { firedAt: "2026-11-01T08:00:00Z", millis: 900, report: "# My Day · Sun 1 Nov" },
        { firedAt: "2026-11-02T08:00:00Z", millis: 900, report: "# My Day · Mon 2 Nov" },
      ]),
    );
    expect(body).toBe("# My Day · Mon 2 Nov");
    expect(body).not.toContain("Sun 1 Nov");
  });

  it("puts the builds latest first, whatever order they were kept in", () => {
    const kept = [
      { firedAt: "2026-11-01T08:00:00Z", millis: 1 },
      { firedAt: "2026-11-03T08:00:00Z", millis: 1 },
      { firedAt: "2026-11-02T08:00:00Z", millis: 1 },
    ];
    expect(real.newest(kept).map((o) => o.firedAt.slice(8, 10))).toEqual(["03", "02", "01"]);
    expect(kept[0].firedAt).toContain("-01T");
  });

  it("says which of the quiet cases it is", () => {
    expect(real.myDayReport(undefined)).toContain("no My Day report");
    expect(real.myDayReport(job([]))).toContain("has not been built yet");
    expect(
      real.myDayReport(job([{ firedAt: "2026-11-02T08:00:00Z", millis: 10, error: "gh timed out" }])),
    ).toContain("**It failed:** gh timed out");
    // An empty morning is an empty string, not a report.
    expect(real.myDayReport(job([{ firedAt: "2026-11-02T08:00:00Z", millis: 10, report: "" }])))
      .toContain("Nothing waiting");
  });

  // The tab title is fixed so the tab is reused; the age goes on the tooltip.
  it("carries how old the build is on the button, not in the tab", () => {
    expect(real.myDayTitle(job([{ firedAt: "2026-11-02T08:00:00Z", millis: 10, report: "x" }]), at))
      .toBe("My Day · 4h");
    expect(real.myDayTitle(job([]), at)).toBe("My Day");
    expect(real.myDayTitle(undefined, at)).toBe("My Day");
  });

  it("counts a build that said something, or failed, until it is read", () => {
    const said = { firedAt: "2026-11-02T08:00:00Z", millis: 1, report: "# My Day" };
    const failed = { firedAt: "2026-11-01T08:00:00Z", millis: 1, error: "gh timed out" };
    const empty = { firedAt: "2026-10-31T08:00:00Z", millis: 1, report: "  " };
    expect(real.jobUnread(job([said, failed, empty]))).toBe(2);
    expect(real.jobUnread(job([{ ...said, seen: true }, { ...failed, seen: true }]))).toBe(0);
    expect(real.jobUnread({ name: "my-day" })).toBe(0);
  });
});

describe("a live session's badge", () => {
  const repos = [
    {
      name: "ade",
      path: "/r/ade",
      checkouts: [
        {
          name: "ade",
          path: "/r/ade",
          sessions: [
            { id: "busy", name: "busy", live: true },
            { id: "stuck", name: "stuck", live: true },
            { id: "calm", name: "calm", live: true },
            { id: "gone", name: "gone", live: true },
          ],
        },
      ],
    },
  ] as unknown as real.Repo[];
  const active = [
    { id: "busy", dir: "/r/ade", name: "busy", agent: { terminal: "working" } },
    { id: "stuck", dir: "/r/ade", name: "stuck", agent: { terminal: "needs" } },
    { id: "calm", dir: "/r/ade", name: "calm", agent: { terminal: "idle" } },
    { id: "gone", dir: "/r/ade", name: "gone", agent: { terminal: "ended" } },
  ];

  it("is amber at work, red when it is your turn, green when idle", () => {
    const rows = sessions(repos, now, active).rows;
    const by = (id: string) => rows.find((row) => row.id === id);
    expect(by("busy")).toMatchObject({ icon: "play", tone: "warn" });
    expect(by("stuck")).toMatchObject({ icon: "bell", tone: "bad" });
    expect(by("calm")).toMatchObject({ icon: "play", tone: "good" });
    expect(by("gone")?.icon).toBe("history");
    expect(by("gone")?.tone).toBeUndefined();
  });

  it("is green for one that has said nothing about itself", () => {
    expect(real.badge(undefined)).toEqual({ icon: "play", tone: "good" });
  });

  it("is red for an agent that failed, whatever its terminal says", () => {
    expect(real.badge({ error: "boom", terminal: "working" })).toEqual({ icon: "bell", tone: "bad" });
  });
});

describe("when opening the window builds My Day", () => {
  const thursday = new Date(2026, 10, 5, 9, 30);
  const job = (over: Partial<real.Job> = {}): real.Job => ({ name: real.MY_DAY, ...over });
  const built = (at: Date): real.Job => job({ outcomes: [{ firedAt: at.toISOString(), millis: 1 }] });

  it("builds on a weekday with nothing built today", () => {
    expect(real.myDayDue(job(), thursday)).toBe(true);
    expect(real.myDayDue(built(new Date(2026, 10, 4, 8, 0)), thursday)).toBe(true);
  });

  it("leaves a report already built today alone", () => {
    expect(real.myDayDue(built(new Date(2026, 10, 5, 8, 0)), thursday)).toBe(false);
    expect(real.myDayDue(job({ lastFired: new Date(2026, 10, 5, 8, 0).toISOString() }), thursday)).toBe(false);
  });

  it("does nothing at the weekend", () => {
    expect(real.myDayDue(job(), new Date(2026, 10, 7, 9, 30))).toBe(false);
    expect(real.myDayDue(job(), new Date(2026, 10, 8, 9, 30))).toBe(false);
  });

  it("leaves a build in flight and a machine without the report alone", () => {
    expect(real.myDayDue(job({ running: true }), thursday)).toBe(false);
    expect(real.myDayDue(undefined, thursday)).toBe(false);
  });
});

describe("what teammates asked of you over agentbus", () => {
  const bus: real.Bus = {
    offers: [
      { id: "o1", kind: "review", from: "kim", pr: "owenrumney/ade#412" },
      { id: "o2", kind: "ask", from: "liam", question: "Ship it?", options: ["yes", "no"] },
      { id: "o3", kind: "review", from: "kim", pr: "owenrumney/ade#413", claimed: true },
    ],
  };

  it("offers a review to claim and a question to answer", () => {
    const rows = real.offered(bus);
    expect(rows.map((row) => row.acts?.map((act) => `${act.label}:${act.what}`))).toEqual([
      ["Claim:bus-claim", "Dismiss:bus-dismiss"],
      ["Answer:bus-answer", "Dismiss:bus-dismiss"],
      ["Open:bus-claim", "Dismiss:bus-dismiss"],
    ]);
    expect(rows[1].name).toBe("Ship it?");
  });

  it("lists them in Notifications, first, so the bell counts them", () => {
    const place = real.waiting({}, bus);
    expect(place.rows.map((row) => row.id)).toEqual(["o1", "o2", "o3"]);
    expect(real.waiting({}).rows).toEqual([]);
  });

  it("lists them in Team as before", () => {
    expect(real.team(bus).rows.slice(0, 3).map((row) => row.id)).toEqual(["o1", "o2", "o3"]);
  });
});

describe("the conversations claude keeps on this machine", () => {
  const now = Date.parse("2026-11-05T20:00:00Z");
  const one = {
    id: "7fea93ea-06ff-4930-85cd-83f21e2e85be",
    title: "Lean terminal host",
    dir: "/Users/owen/code/owenrumney/ade",
    used: "2026-11-05T19:30:00Z",
    running: false,
    here: false,
    checkout: true,
  };

  it("names each by its title, then its folder and how it stands", () => {
    const rows = real.conversationRows(
      [
        { ...one, running: true },
        { ...one, id: "feae2ed8-9d59-4995-9e80-a2602b60fcbe", title: "", running: true, here: true },
        { ...one, dir: "/Users/owen", checkout: false },
        one,
      ],
      now,
    );
    expect(rows[0]).toEqual({ label: "Lean terminal host", note: "ade · open in another terminal · 30m", value: one.id });
    expect(rows[1].label).toBe("feae2ed8");
    // Its claude is ade's own, which is not another terminal.
    expect(rows[1].note).toBe("ade · already in ade · 30m");
    expect(rows[2].note).toBe("owen · not a git checkout · 30m");
    expect(rows[3].note).toBe("ade · 30m");
  });
});

describe("a review by named agents", () => {
  const started = "2026-11-05T20:43:48Z";
  const run = {
    running: true,
    startedAt: started,
    endedAt: "0001-01-01T00:00:00Z",
    stage: "1 of 3 reviewers finished",
    summary: "",
    reviewers: [
      { name: "security", state: "done", doing: "", why: "", found: 3, endedAt: "2026-11-05T20:45:00Z" },
      { name: "staff-reviewer", state: "running", doing: "Read web.go", why: "", found: 0, endedAt: "" },
      { name: "tester", state: "failed", doing: "", why: "exit status 1", found: 0, endedAt: "2026-11-05T20:44:00Z" },
      { name: "simplifier", state: "stopped", doing: "", why: "", found: 1, endedAt: "2026-11-05T20:44:10Z" },
    ],
  };
  const now = Date.parse("2026-11-05T20:45:53Z");

  it("says how each reviewer stands", () => {
    const review = real.doingIn({ reviewRun: run }).review;
    expect(review).toBeDefined();
    expect(real.reviewLines(review!, now)).toEqual([
      { name: "@security", state: "done", note: "3 points · 1m 12s" },
      { name: "@staff-reviewer", state: "running", note: "2m 05s · Read web.go" },
      { name: "@tester", state: "failed", note: "failed: exit status 1" },
      { name: "@simplifier", state: "stopped", note: "stopped · 1 point" },
    ]);
  });

  it("carries the stage to the status bar and keeps the window asking", () => {
    const doing = real.doingIn({ agent: { sideRuns: [{ id: "a", name: "security" }] }, reviewRun: run });
    expect(real.saying(doing)).toBe("1 of 3 reviewers finished");
    expect(real.busy(real.doingIn({ reviewRun: run }))).toBe(true);
  });

  it("is over when the server says so, and says how long it took", () => {
    const over = { ...run, running: false, stage: "", endedAt: "2026-11-05T20:46:03Z" };
    const doing = real.doingIn({ reviewRun: over });
    expect(real.reviewHead(doing.review!)).toBe("Review · finished in 2m 15s");
    expect(real.reviewHead(real.doingIn({ reviewRun: run }).review!)).toBe("Review · running");
    expect(real.saying(doing)).toBe("");
    expect(real.busy(doing)).toBe(false);
  });

  it("is not there when no review has run", () => {
    expect(real.doingIn({ agent: {} }).review).toBeUndefined();
  });
});
