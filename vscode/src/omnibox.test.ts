import { describe, expect, it } from "vitest";
import type { Session } from "./launcher";
import { allLauncherRows, designRowsFor, everyLauncherRow, everyRow, featuredVerbs, type GroupedVerb, inboxRowsFor, isQuestion, launcherRows, plain, pullRowsFor, queueRowsFor, recentRows, remember, type Remembered, route, type Verb, verbRowsFor, wikiRowsFor, worktreeRowsFor } from "./omnibox";
import type { Row } from "./switcher";

const verbs: Verb[] = [
  { command: "ade.review", title: "ade: Review this branch" },
  { command: "ade.reviewWithAgents", title: "ade: Review with agents" },
  { command: "ade.commit", title: "ade: Commit" },
  { command: "ade.push", title: "Push" },
];

describe("the omnibox", () => {
  it("says a sentence to the agent", () => {
    expect(route("add a logger to main", verbs)).toEqual({
      kind: "say",
      message: "add a logger to main",
    });
  });

  it("focuses the chat on nothing at all", () => {
    expect(route("   ", verbs)).toEqual({ kind: "focus" });
  });

  it("runs the one verb a slash names", () => {
    expect(route("/com", verbs)).toEqual({ kind: "run", command: "ade.commit" });
  });

  it("offers a pick when the slash is ambiguous", () => {
    expect(route("/rev", verbs)).toEqual({
      kind: "pick",
      commands: ["ade.review", "ade.reviewWithAgents"],
    });
  });

  it("prefers a title that starts with the word over one that contains it", () => {
    expect(route("/push", verbs)).toEqual({ kind: "run", command: "ade.push" });
    expect(route("/agents", verbs)).toEqual({
      kind: "run",
      command: "ade.reviewWithAgents",
    });
  });

  it("offers every verb on a bare slash", () => {
    expect(route("/", verbs).kind).toBe("pick");
  });

  it("says a slash it cannot match, slash and all", () => {
    expect(route("/deploy to prod", verbs)).toEqual({
      kind: "say",
      message: "/deploy to prod",
    });
  });

  it("matches titles without the palette prefix", () => {
    expect(plain("ade: Review this branch")).toBe("review this branch");
    expect(plain("Push")).toBe("push");
  });
});

describe("the launcher rows", () => {
  const row = (s: Partial<Session>, repo: string, waiting = false): Row => ({
    session: { id: "x", agent: "claude", name: "", branch: "", dir: "", ...s } as Session,
    repo,
    waiting,
    here: false,
  });
  const sessions = [
    row({ id: "1", name: "tilt", branch: "main", dir: "/r/tilt", live: true }, "tilt-vscode-ext"),
    row({ id: "2", name: "ade", branch: "feat/vscodium", dir: "/r/ade", title: "the editor fork" }, "ade", true),
  ];
  const threads = [
    { id: "t1", file: "main.go", line: 5, comments: [{ body: "Printing from main is fine for a demo" }] },
    { id: "t2", file: "a.go", line: 1, resolved: true, comments: [{ body: "main" }] },
  ];

  it("offers a session by name, repo or branch, saying what it is doing", () => {
    expect(launcherRows("tilt", sessions, [])).toEqual([
      expect.objectContaining({ label: "Switch to main", description: "tilt-vscode-ext · main · running" }),
    ]);
    expect(launcherRows("vscodium", sessions, [])[0]).toEqual(
      expect.objectContaining({ label: "Switch to the editor fork", description: "ade · feat/vscodium · asked you" }),
    );
  });

  it("leaves out the session this window already is", () => {
    expect(launcherRows("ade", sessions, [], "2")).toEqual([]);
  });

  it("offers an open comment by its file or its words, never a resolved one", () => {
    const rows = launcherRows("main", [], threads);
    expect(rows.map((r) => r.description)).toEqual(["main.go:5"]);
    expect(rows[0].args).toEqual([{ kind: "thread", file: "main.go", line: 5 }]);
  });

  it("needs every typed word somewhere in the row", () => {
    expect(launcherRows("demo main", [], threads)).toHaveLength(1);
    expect(launcherRows("demo elsewhere", [], threads)).toHaveLength(0);
  });
});

describe("the launcher rows for worktrees and pull requests", () => {
  const trees = [
    { path: "/r/ade", name: "ade", branch: "main", kind: "main" },
    { path: "/r/ade-fork", name: "ade-fork", branch: "feat/vscodium", kind: "linked", uncommitted: 25, unpushed: 2 },
  ];
  const pulls = [
    { number: 16, title: "Concierge front desk", author: "owenrumney", headRef: "feat/concierge", reviewRequested: true, checks: "passing" },
    { number: 17, title: "Queue edits", author: "owenrumney", headRef: "feat/queue", draft: true },
  ];

  it("offers a worktree by branch, kind or name, saying what is in it", () => {
    expect(worktreeRowsFor("vscodium", trees)).toEqual([
      expect.objectContaining({
        label: "Open worktree feat/vscodium",
        description: "linked · 25 uncommitted · 2 unpushed",
        args: [{ kind: "worktree", path: "/r/ade-fork" }],
      }),
    ]);
    expect(worktreeRowsFor("main", trees, "/r/ade")).toEqual([]);
  });

  it("offers a pull request by number, title, author or branch", () => {
    expect(pullRowsFor("16", pulls)[0]).toEqual(
      expect.objectContaining({
        label: "Review #16 · Concierge front desk",
        description: "owenrumney · feat/concierge · review requested · passing",
        args: [{ kind: "pr", number: 16 }],
      }),
    );
    expect(pullRowsFor("queue", pulls)[0].description).toBe("owenrumney · feat/queue · draft");
  });

  it("puts sessions first, then pull requests, worktrees and comments", () => {
    const rows = allLauncherRows("feat", [], [], trees, pulls);
    expect(rows.map((r) => r.label)).toEqual([
      "Review #16 · Concierge front desk",
      "Review #17 · Queue edits",
      "Open worktree feat/vscodium",
    ]);
  });
});

describe("the launcher rows for the inbox and the queue", () => {
  const offers = [
    { id: "o1", kind: "ask", from: "sam", question: "Which port does the worker bind?" },
    { id: "o2", kind: "review", from: "kim", repo: "ade", number: 21, claimed: false },
  ];
  const waiting = [
    { id: "w1", kind: "prompt", message: "then add tests for the logger" },
    { id: "w2", kind: "prompt", message: "and update the README" },
  ];

  it("offers an ask to answer and a review to take", () => {
    expect(inboxRowsFor("port", offers)).toEqual([
      expect.objectContaining({ label: "Answer sam · Which port does the worker bind?", args: [{ kind: "ask", offer: offers[0] }] }),
    ]);
    expect(inboxRowsFor("ade#21", offers)[0]).toEqual(
      expect.objectContaining({ label: "Take review ade#21", description: "kim · asked of the team" }),
    );
  });

  it("offers what is queued, counting it once", () => {
    const rows = queueRowsFor("queued", waiting);
    expect(rows.map((r) => r.label)).toEqual(["Queued · then add tests for the logger", "Queued · and update the README"]);
    expect(rows.map((r) => r.description)).toEqual(["2 waiting for the agent", ""]);
  });

  it("puts the inbox first of everything", () => {
    const rows = everyLauncherRow("ade", { sessions: [], threads: [], trees: [], pulls: [], offers, waiting: [] });
    expect(rows[0].label).toBe("Take review ade#21");
  });
});

describe("a new session from the box", () => {
  it("takes the rest of the line as the first message", () => {
    expect(route("/new add a logger to main", verbsForNew)).toEqual({ kind: "new", message: "add a logger to main" });
  });

  it("is the plain verb with nothing after it", () => {
    expect(route("/new", verbsForNew)).toEqual({ kind: "run", command: "ade.newSession" });
  });
});
const verbsForNew = [{ command: "ade.newSession", title: "ade: New Session" }];

describe("a verb named exactly", () => {
  it("runs rather than offering its longer cousins", () => {
    const verbs = [
      { command: "ade.stop", title: "ade: Stop" },
      { command: "ade.stopAgent", title: "ade: Stop the agent" },
      { command: "ade.stopElsewhere", title: "ade: Stop elsewhere" },
    ];
    expect(route("/stop", verbs)).toEqual({ kind: "run", command: "ade.stop" });
    expect(route("/stop the", verbs).kind).toBe("run");
  });
});

describe("a question", () => {
  it("is a line that ends in a question mark", () => {
    expect(isQuestion("which port does the worker bind? ")).toBe(true);
    expect(isQuestion("add a logger to main")).toBe(false);
  });
});

describe("what the box remembers", () => {
  const verbs = [{ command: "ade.review", title: "ade: Review this branch" }];
  it("keeps the newest first, once each, up to a few", () => {
    let had: Remembered[] = [];
    had = remember(had, { kind: "turn", text: "add a logger", at: 1 });
    had = remember(had, { kind: "ask", text: "which port?", answer: "8080", at: 2 });
    had = remember(had, { kind: "turn", text: "add a logger", at: 3 });
    expect(had.map((r) => r.at)).toEqual([3, 2]);
    expect(remember(had, { kind: "verb", text: "ade.review", at: 4 }, 2)).toHaveLength(2);
  });

  it("reads back as rows that say what each was", () => {
    const rows = recentRows(
      [
        { kind: "ask", text: "which port?", answer: "8080\nsee config", at: 2 },
        { kind: "verb", text: "ade.review", at: 1 },
        { kind: "turn", text: "add a logger", at: 0 },
      ],
      verbs,
    );
    expect(rows.map((r) => r.label)).toEqual(["Asked · which port?", "Run again · review this branch", "Sent · add a logger"]);
    expect(rows[0].description).toBe("8080");
    expect(rows[1].args).toEqual([{ kind: "recent", index: 1 }]);
  });

  it("answers /again with the last verb", () => {
    expect(route("/again", verbs)).toEqual({ kind: "again" });
  });
});

describe("what the box knows", () => {
  it("offers a design document by its name", () => {
    expect(designRowsFor("fork", ["editor-fork.md", "concierge.md"])).toEqual([
      expect.objectContaining({ label: "Open design · editor-fork", args: [{ kind: "design", file: "editor-fork.md" }] }),
    ]);
  });

  it("offers wiki hits with the matching line, one row a page", () => {
    const rows = wikiRowsFor([
      { path: "sandbox.md", line: 12, snippet: "Apple's container when its services are up\nelse docker" },
      { path: "sandbox.md", line: 40, snippet: "later on the same page" },
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toEqual(
      expect.objectContaining({ label: "Wiki · sandbox", description: "Apple's container when its services are up", args: [{ kind: "wiki", path: "sandbox.md", line: 12 }] }),
    );
  });

  it("says which archived design a wiki hit is", () => {
    const rows = wikiRowsFor([
      { path: "repos/owner/repo/designs/editor-fork.md", line: 3, snippet: "VSCodium's scripts over a pinned vscode" },
    ]);
    expect(rows[0].label).toBe("Design (archived) · repos/owner/repo/designs/editor-fork");
  });

  it("takes /note as a line for the design doc", () => {
    expect(route("/note the pill should also say the branch", [])).toEqual({ kind: "note", text: "the pill should also say the branch" });
    expect(route("/note", [{ command: "ade.noteDesign", title: "ade: Note" }])).toEqual({ kind: "run", command: "ade.noteDesign" });
  });

  it("puts knowledge after the launcher rows", () => {
    const rows = everyRow("fork", { sessions: [], threads: [], trees: [], pulls: [], offers: [], waiting: [], designs: ["editor-fork.md"], wiki: [{ path: "fork.md", line: 1, snippet: "x" }] });
    expect(rows.map((r) => r.label)).toEqual(["Open design · editor-fork", "Wiki · fork"]);
  });
});

describe("/go", () => {
  it("names a session to switch to without the pick", () => {
    expect(route("/go editor fork", [])).toEqual({ kind: "go", name: "editor fork" });
  });
});

describe("the verbs as rows", () => {
  const verbs: GroupedVerb[] = [
    { command: "ade.commit", title: "ade: Commit", group: "Commit and pull request", icon: "$(check)" },
    { command: "ade.commitAmend", title: "ade: Commit (amend)", group: "Commit and pull request" },
    { command: "ade.review", title: "ade: Review the diff", group: "Review", icon: "$(checklist)" },
    { command: "ade.reviewWithAgents", title: "ade: Review the diff with named agents", group: "Review", icon: "$(organization)" },
    { command: "ade.openChat", title: "ade: Open Chat", group: "Conversation", icon: "$(comment-discussion)" },
  ];

  it("offers the verbs a word names, the ones starting with it first", () => {
    expect(verbRowsFor("commit", verbs).map((r) => r.label)).toEqual(["Commit", "Commit (amend)"]);
    expect(verbRowsFor("agents", verbs).map((r) => r.label)).toEqual(["Review the diff with named agents"]);
    expect(verbRowsFor("commit", verbs)[0]).toEqual(
      expect.objectContaining({ description: "Commit and pull request", icon: "check", command: "ade.commit" }),
    );
  });

  it("matches on the group too, and offers nothing for nothing", () => {
    expect(verbRowsFor("conversation", verbs).map((r) => r.command)).toEqual(["ade.openChat"]);
    expect(verbRowsFor("  ", verbs)).toEqual([]);
  });

  it("fronts a fixed few for the empty box, in that order, skipping the absent", () => {
    expect(featuredVerbs(verbs).map((r) => r.command)).toEqual(["ade.review", "ade.reviewWithAgents", "ade.commit", "ade.openChat"]);
  });
});
