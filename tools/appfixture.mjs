// A shell for the picture harness: the same bridge the Electron app
// gives the window, answering with fixed data instead of a launcher.
// The window's own code paths are unchanged — this only stands in for
// the process that would reach ade.
// The my-day job with its report unread, which is what paints the Jobs icon.
const MY_DAY = {
  name: "my-day",
  description: "What your day looks like",
  schedule: "0 8 * * 1-5",
  nextDue: "2026-10-23T08:00:00Z",
  lastFired: "2026-10-22T08:00:00Z",
  outcomes: [
    {
      firedAt: "2026-10-22T08:00:00Z",
      millis: 41000,
      costUsd: 0.07,
      report: [
        "# My Day · Thu 22 Oct — 2 to review, 1 of yours open, 3 tickets at 9–16h",
        "",
        "## Reviews waiting on you (2)",
        "",
        "- [owenrumney/ade#412](https://github.com/owenrumney/ade/pull/412) **Add usage tiles to the desk** — kim · checks pass",
        "- [infracost/coast#541](https://github.com/infracost/coast/pull/541) **Cache the plan reader** — liam · checks fail",
        "",
        "## Your pull requests (1)",
        "",
        "- [owenrumney/ade#33](https://github.com/owenrumney/ade/pull/33) **Ship cloud logins to workers** — approved · checks pass",
        "",
        "## Your tickets (3) · 9–16h",
        "",
        "### ade",
        "- [ENG-204](https://linear.app/ENG-204) **Purge the worktrees nobody opened** — **4–8h** · in progress · high · *A git operation per worktree, and the safety check is the hard half*",
        "- [ENG-211](https://linear.app/ENG-211) **Quiz me on the design doc** — **3–5h** · todo · medium · *Mostly prompt work; the grading loop is the unknown*",
        "",
        "### No project",
        "- [ENG-188](https://linear.app/ENG-188) **Drop the stale dismissals** — **2–3h** · todo · *One store and its sweep*",
      ].join("\n"),
    },
  ],
};

export const WORLD = {
  jobs: [
    MY_DAY,
  ],
  spaces: [
    { repo: "owenrumney/ade", owner: "owenrumney", name: "ade", path: "/Users/owen/code/owenrumney/ade",
      branch: "main", sessions: 13, here: true },
    { repo: "infracost/coast", owner: "infracost", name: "coast", path: "/Users/owen/code/infracost/coast",
      branch: "main", sessions: 19, here: true },
    { repo: "infracost/docs", owner: "infracost", name: "docs", sessions: 0, here: false },
    { repo: "infracost/infra", owner: "infracost", name: "infra", path: "/Users/owen/code/infracost/infra",
      branch: "master", sessions: 2, here: true },
  ],
  dash: {
    reviews: [
      {
        repo: "owenrumney/ade",
        number: 412,
        title: "Add usage tiles to the desk",
        author: "owenrumney",
        url: "https://github.com/owenrumney/ade/pull/412",
        checks: "passing",
        updatedAt: "2026-10-21T09:12:00Z",
        review: { state: "drafted", unsent: 3, threads: 4 },
      },
      {
        repo: "owenrumney/lazytrivy",
        number: 88,
        title: "Cache the vulnerability database between runs",
        author: "kim",
        url: "https://github.com/owenrumney/lazytrivy/pull/88",
        checks: "failing",
        updatedAt: "2026-10-20T16:40:00Z",
      },
    ],
    notifications: [
      {
        id: "n1",
        repo: "owenrumney/go-sarif",
        title: "Review requested: support SARIF 2.2",
        reason: "review_requested",
        type: "PullRequest",
        url: "https://github.com/owenrumney/go-sarif/pull/31",
        updatedAt: "2026-10-21T08:00:00Z",
      },
    ],
    linear: {
      issues: [
        {
          identifier: "ADE-204",
          title: "One window for every session",
          url: "https://linear.app/ade/issue/ADE-204",
          state: "In Progress",
          priority: 1,
          project: "The window",
        },
        {
          identifier: "ADE-211",
          title: "Spaces: a repository you keep open",
          url: "https://linear.app/ade/issue/ADE-211",
          state: "Todo",
          priority: 2,
          project: "The window",
        },
        {
          identifier: "COAST-88",
          title: "Usage tiles on the desk",
          url: "https://linear.app/coast/issue/COAST-88",
          state: "Todo",
          priority: 2,
          project: "Pricing rework",
        },
        {
          identifier: "ADE-97",
          title: "Tidy the scratch directories",
          url: "https://linear.app/ade/issue/ADE-97",
          state: "Todo",
        },
      ],
    },
    started: {
      ade: { dir: "/Users/owen/code/owenrumney/ade", branch: "rethink-workflow-and-uk", session: "ade" },
      "ADE-211": { dir: "/Users/owen/code/owenrumney/ade-211", branch: "ade-211-spaces", session: "ade" },
    },
  },
  active: [
    {
      id: "pending-0d210b89",
      dir: "/Users/owen/code/infracost/infra-pr-1535",
      name: "infra-pr-1535",
      repo: "infra",
      branch: "feat/inspector-ec2-scanning",
      agent: { terminal: "needs" },
    },
    {
      id: "ade",
      dir: "/Users/owen/code/owenrumney/ade",
      name: "ade",
      repo: "ade",
      branch: "rethink-workflow-and-uk",
      agent: {
        running: true,
      },
    },
    {
      id: "squealer",
      dir: "/Users/owen/code/owenrumney/squealer",
      name: "squealer",
      repo: "squealer",
      branch: "fix/entropy",
      agent: {
        running: true,
        asking: 1,
      },
    },
  ],
  repos: [
    {
      name: "infra",
      owner: "infracost",
      path: "/Users/owen/code/infracost/infra",
      branch: "master",
      checkouts: [
        {
          name: "infra",
          path: "/Users/owen/code/infracost/infra",
          branch: "master",
          sessions: [
            {
              id: "infra-old",
              name: "infra",
              title: "Loki push path split and cluster ingress",
              branch: "trk-1234",
              dir: "/Users/owen/code/infracost/infra",
              lastUsed: "2026-10-19T22:17:06Z",
            },
          ],
        },
        {
          name: "infra-pr-1535",
          path: "/Users/owen/code/infracost/infra-pr-1535",
          branch: "feat/inspector-ec2-scanning",
          worktree: true,
          sessions: null,
        },
      ],
    },
    {
      name: "ade",
      owner: "owenrumney",
      path: "/Users/owen/code/owenrumney/ade",
      branch: "rethink-workflow-and-uk",
      checkouts: [
        {
          name: "ade",
          path: "/Users/owen/code/owenrumney/ade",
          branch: "rethink-workflow-and-uk",
          sessions: [
            {
              id: "ade",
              name: "ade",
              title: "Rethink the workflow and the window",
              branch: "rethink-workflow-and-uk",
              dir: "/Users/owen/code/owenrumney/ade",
              live: true,
              agent: "claude",
              ticket: "ADE-211",
              tags: ["layout"],
              brief: "/Users/owen/code/owenrumney/ade/.git/ade/docs/lean-terminal-host.md",
              lastUsed: "2026-10-22T10:30:00Z",
            },
            {
              id: "ade-older",
              name: "ade-older",
              title: "Pruning the design docs",
              branch: "rethink-workflow-and-uk",
              dir: "/Users/owen/code/owenrumney/ade",
              lastUsed: "2026-10-20T09:00:00Z",
            },
          ],
        },
        {
          name: "webview-for-sandbox",
          path: "/Users/owen/code/owenrumney/ade-webview-for-sandbox",
          branch: "webview-for-sandbox",
          worktree: true,
          sessions: [
            {
              id: "wfs",
              name: "wfs",
              title: "Webview for the sandbox",
              branch: "webview-for-sandbox",
              dir: "/Users/owen/code/owenrumney/ade-webview-for-sandbox",
              agent: "codex",
              tags: ["parked", "spike"],
              lastUsed: "2026-10-22T08:10:00Z",
            },
          ],
        },
      ],
    },
    {
      name: "lazytrivy",
      owner: "owenrumney",
      path: "/Users/owen/code/owenrumney/lazytrivy",
      branch: "main",
      checkouts: [
        {
          name: "lazytrivy",
          path: "/Users/owen/code/owenrumney/lazytrivy",
          branch: "main",
          sessions: [
            {
              id: "lt",
              name: "lt",
              title: "Cache the vulnerability database",
              branch: "main",
              dir: "/Users/owen/code/owenrumney/lazytrivy",
              lastUsed: "2026-10-19T14:00:00Z",
            },
          ],
        },
      ],
    },
  ],
};

const DIFF = {
  version: "1",
  diff: {
    base: "main",
    head: "rethink-workflow-and-uk",
    mode: "branch",
    commits: 4,
    files: [
      {
        path: "app/renderer/doing.ts",
        status: "modified",
        binary: false,
        additions: 6,
        deletions: 2,
        hunks: [
          {
            header: "@@ -62,8 +62,12 @@",
            heading: "export const WIRED",
            rows: [
              { left: cell(62, "context", "  // ── Review ──"), right: cell(62, "context", "  // ── Review ──") },
              {
                left: cell(63, "del", '  "ade.reviewWithAgents": soon("a multi-select of named agents"),'),
                right: cell(63, "add", '  "ade.reviewWithAgents": S("/api/review", { level: "" },'),
              },
              { left: null, right: cell(64, "add", '    many("reviewer", "agents", "Which agents should review?")),') },
              { left: cell(64, "context", '  "ade.resolveAll": S("/api/resolve", { all: true }),'), right: cell(65, "context", '  "ade.resolveAll": S("/api/resolve", { all: true }),') },
            ],
          },
        ],
      },
      {
        path: "app/renderer/sources.ts",
        status: "added",
        binary: false,
        additions: 84,
        deletions: 0,
        hunks: [
          {
            header: "@@ -0,0 +1,6 @@",
            rows: [
              { left: null, right: cell(1, "add", 'import type { From } from "./doing";') },
              { left: null, right: cell(2, "add", "") },
              { left: null, right: cell(3, "add", "export const SOURCES: Record<Choice, Source> = {") },
              { left: null, right: cell(4, "add", '  model: { at: "launcher", path: "/api/models", rows: (got) => [] },') },
              { left: null, right: cell(5, "add", "};") },
            ],
          },
        ],
      },
    ],
  },
};

// A walkthrough of the diff above, as the agent would have written it.
function cell(line, kind, content) {
  return { line, kind, content };
}

// A review by named agents, by the session it runs in: one under way, one over.
const since = (s) => new Date(Date.now() - s * 1000).toISOString();
const REVIEWS = {
  "ade-older": {
    running: true,
    startedAt: since(94),
    endedAt: "0001-01-01T00:00:00Z",
    stage: "1 of 3 reviewers finished",
    summary: "",
    reviewers: [
      { name: "security", state: "done", doing: "", why: "", found: 3, endedAt: since(22) },
      { name: "staff-reviewer", state: "running", doing: "Read internal/web/rebuttal.go", why: "", found: 0, endedAt: "0001-01-01T00:00:00Z" },
      { name: "tester", state: "failed", doing: "", why: "claude exited with status 1", found: 0, endedAt: since(80) },
    ],
  },
  wfs: {
    running: false,
    startedAt: since(600),
    endedAt: since(465),
    stage: "",
    summary: "5 finding(s) from 2 reviewer(s) → 4 point(s). 4 being put in your words, ready to send to GitHub.",
    reviewers: [
      { name: "security", state: "done", doing: "", why: "", found: 2, endedAt: since(540) },
      { name: "staff-reviewer", state: "done", doing: "", why: "", found: 3, endedAt: since(522) },
    ],
  },
};

const STATE = {
  threads: [
    {
      id: "t1",
      file: "app/renderer/doing.ts",
      side: "right",
      line: 63,
      category: "correctness",
      severity: "medium",
      comments: [
        {
          id: "c1",
          author: "@security",
          at: "2026-10-22T11:20:00Z",
          body: "`many()` takes the whole list. A reviewer that is not on this machine would be offered and then refused.",
        },
        { id: "c2", author: "you", at: "2026-10-22T11:35:00Z", body: "Filtered in `sources.ts` — user scope only." },
      ],
    },
    {
      id: "t5",
      file: "app/renderer/doing.ts",
      side: "right",
      line: 64,
      category: "security",
      severity: "high",
      comments: [
        { id: "c10", author: "@security", at: "2026-10-22T11:20:00Z", body: "The resource_types list omits LAMBDA, so an apply disables the Lambda scan." },
        { id: "c11", author: "@staff-reviewer", at: "2026-10-22T11:21:00Z", body: "The provider's delete ignores the configured resource_types." },
        { id: "c12", author: "@security", at: "2026-10-22T11:22:00Z", body: "Two resources set the ECR scan with no order between them." },
        { id: "c13", author: "@tester", at: "2026-10-22T11:23:00Z", body: "No test covers the count guard." },
      ],
    },
    {
      id: "t6",
      file: "app/renderer/doing.ts",
      side: "right",
      line: 65,
      category: "correctness",
      severity: "critical",
      outgoing: "`resource_types` leaves out `LAMBDA`, so applying this turns off Lambda scanning for the whole account.",
      comments: [
        { id: "c20", author: "@security", at: "2026-10-22T11:24:00Z", body: "The finding, in full, with the working out." },
      ],
    },
    {
      id: "t7",
      file: "app/renderer/doing.ts",
      side: "right",
      line: 62,
      category: "correctness",
      severity: "low",
      outgoing: "Worth a `depends_on` here so the apply order is fixed.",
      draftReview: "REVIEW1",
      draftThread: "COMMENT1",
      comments: [
        { id: "c30", author: "@staff-reviewer", at: "2026-10-22T11:25:00Z", body: "Ordering is arbitrary between these two." },
      ],
    },
    {
      id: "t2",
      file: "app/renderer/sources.ts",
      side: "right",
      line: 4,
      resolved: true,
      comments: [{ id: "c3", author: "@tester", at: "2026-10-22T10:05:00Z", body: "No test for an empty answer." }],
    },
  ],
  agent: { sideRuns: [{ id: "s1", name: "security", startedAt: "2026-10-22T09:40:00Z" }] },
  worktree: { staged: [], unstaged: [], ahead: 2, behind: 6 },
  branchPr: { number: 412, title: "Add usage tiles to the desk", body: "# What\n\nUsage tiles on the desk.\n" },
  openPrs: [
    {
      number: 414,
      title: "Tabs in the window",
      headRef: "rethink-workflow-and-uk",
      mine: true,
      checks: "pass",
      reviewDecision: "APPROVED",
      updatedAt: "2026-10-22T11:30:00Z",
    },
    { number: 415, title: "Scrollbar colours", headRef: "scrollbars", mine: true, draft: true },
  ],
  bus: {
    online: true,
    offers: [
      { id: "o1", kind: "review", from: "kim", pr: "owenrumney/ade#412" },
      { id: "o2", kind: "ask", from: "liam", question: "Is the proxy hop safe to ship today?", options: ["yes", "not yet"] },
    ],
    replies: [{ id: "r1", from: "kim", text: "the proxy hop looks right", at: "2026-10-22T11:40:00Z" }],
    sent: [{ id: "s1", to: "anyone", question: "anyone seen the sandbox image size?" }],
    // Two instances of one person, which is the ordinary case: an ade on the
    // laptop and one on the pi. The list folds them into a row each.
    team: [
      { user: "owen", presence: "idle", node: "mac", you: true, agents: 0 },
      { user: "owen", presence: "active", node: "pi5", you: true, agents: 2, status: "an agent is working" },
      { user: "kim", presence: "idle", harness: "claude", node: "kim-mbp" },
    ],
  },
};

const READS = {
  // offered is what is not open yet, which is the whole of the Open a space pick.
  "/api/spaces": {
    spaces: WORLD.spaces,
    offered: [
      { repo: "infracost/cloud-pricing-api", owner: "infracost", name: "cloud-pricing-api", sessions: 4, branch: "main" },
      { repo: "owenrumney/squealer", owner: "owenrumney", name: "squealer", sessions: 1, branch: "main" },
    ],
  },
  // What the New session screen's Open branch row lists.
  "/api/branches": {
    worktrees: [
      { path: "/Users/owen/code/owenrumney/ade", name: "ade", branch: "main", main: true },
      {
        path: "/Users/owen/code/owenrumney/ade-rethink-workflow-and-uk",
        name: "ade-rethink-workflow-and-uk",
        branch: "rethink-workflow-and-uk",
      },
      {
        path: "/Users/owen/code/owenrumney/ade-webview-for-sandbox",
        name: "ade-webview-for-sandbox",
        branch: "webview-for-sandbox",
      },
    ],
    branches: [
      { name: "fix/comment-side-vocabulary", at: "4 days ago" },
      { name: "add-push-with-lease-in", at: "6 days ago" },
      { name: "feat/import-a-session", at: "last week" },
    ],
  },
  // The session's brief: the newest file under its design docs.
  "/api/brief": {
    path: "/Users/owen/code/owenrumney/ade/.git/ade/docs/lean-terminal-host.md",
    body: [
      "# ade becomes a lean host of terminals",
      "",
      "## Goal",
      "",
      "One tab per session on the left. Each runs the claude or codex CLI in a terminal.",
      "",
      "## Decisions",
      "",
      "- Bottom tabs are Agent, Diff and Shell. Review lives inside Diff.",
      "- The bar is on the right with five buttons.",
      "",
      "## Progress",
      "",
      "Step 4 of 5 done: agents on the bus. Next: the layout.",
    ].join("\n"),
  },
  "/api/diff": DIFF,
  "/api/state": STATE,
  "/api/conversations": {
    conversations: [
      { id: "7fea93ea-06ff-4930-85cd-83f21e2e85be", title: "Lean terminal host with PR review", dir: "/Users/owen/code/owenrumney/ade", used: new Date(Date.now() - 4 * 60_000).toISOString(), running: true, here: false, checkout: true },
      { id: "c41d7a0e-52b6-4f0e-9d1a-6b1f2a3c4d5e", title: "Lambda scanning for the inspector module", dir: "/Users/owen/code/infracost/infra", used: new Date(Date.now() - 50 * 60_000).toISOString(), running: true, here: true, checkout: true },
      { id: "feae2ed8-9d59-4995-9e80-a2602b60fcbe", title: "Garmin token expiry in health-mcp", dir: "/Users/owen/code/owenrumney/health-mcp", used: new Date(Date.now() - 26 * 3_600_000).toISOString(), running: false, here: false, checkout: true },
      { id: "82cdb767-1c1e-4f0b-9a57-0f6a4c9d2b11", title: "mattpocock-skills usage advice", dir: "/Users/owen", used: new Date(Date.now() - 9 * 86_400_000).toISOString(), running: false, here: false, checkout: false },
    ],
  },
  "/api/agents": {
    agents: [
      { name: "security", description: "trust boundaries and injection", reviews: true, scope: "user" },
      { name: "staff-reviewer", description: "adversarial design and code critique", reviews: true, scope: "user" },
      { name: "tester", description: "the cases the tests miss", reviews: true, scope: "user" },
    ],
  },
  "/api/jobs": {
    jobs: [
      MY_DAY,
    ],
  },
  "/api/permissions": {
    deny: ["Bash(rm -rf:*)", "WebFetch"],
    file: "/Users/owen/.claude/settings.json",
  },
};

// The script the harness installs before the page loads. It is a string
// because it runs in the page, where this module's scope does not reach.
export function shell() {
  return `
    const WORLD = ${JSON.stringify(WORLD)};
    const READS = ${JSON.stringify(READS)};
    const REVIEWS = ${JSON.stringify(REVIEWS)};
    // The real shell takes the keystroke before the page sees it. Chromium
    // cannot, so the stub listens instead: the command names are the same.
    window.ade = {
      watch: async () => ({ ok: true }),
      listen: async () => ({ ok: true }),
      term: async () => ({ ok: true }),
      termSend() {},
      termResize() {},
      onTerm() {},
      onTermClosed() {},
      onEvent() {},
      onStream() {},
      onCommand(then) {
        document.addEventListener("keydown", (e) => {
          if (!(e.metaKey || e.ctrlKey)) return;
          const of = { k: "omnibox", n: "ade.newSession" };
          const command = of[e.key.toLowerCase()];
          if (!command) return;
          e.preventDefault();
          then(command);
        });
      },
      keys: async () => [],
      opened: async () => "",
      spaces: async () => WORLD.spaces,
      world: async () => WORLD,
      // Opening a space moves it from offered to open, so the window can be
      // driven through the whole verb rather than only up to the pick.
      act: async (at, path, body) => {
        // Real routes take a second or two, which is the whole of what a
        // button disabled while it runs is for.
        if (path.split("?")[0] === "/api/review/draft")
          return new Promise((done) => setTimeout(() => done({ added: 1 }), 2000));
        if (path.split("?")[0] !== "/api/spaces") return {};
        const reads = READS["/api/spaces"];
        const want = reads.offered.find((one) => one.repo === body.repo);
        if (!want) return {};
        reads.offered = reads.offered.filter((one) => one !== want);
        reads.spaces.push({ ...want, path: "/Users/owen/code/" + want.repo, here: true });
        WORLD.spaces = reads.spaces;
        return {};
      },
      // The raw one: the left side of a diff, as the editor reads it.
      call: async (at, path) =>
        path.startsWith("/api/blob")
          ? { status: 200, ok: true, text: "// internal/web/web.go\\npackage web\\n\\nfunc serve() {\\n}\\n" }
          : { status: 404, ok: false, text: "" },
      link: async () => ({}),
      read: async (at, path, session) => {
        if (path.startsWith("/api/source"))
          return { source: "// internal/web/web.go\\npackage web\\n\\nfunc serve() error {\\n  return nil\\n}\\n" };
        if (path.split("?")[0] === "/api/bus/history")
          return {
            with: "kim",
            said: [
              { at: "2026-10-23T09:10:00Z", id: "q1", kind: "ask", mine: true, agent: true,
                question: "Is dev RDS safe to resize during the day?", options: ["yes", "no", "after 6pm"] },
              { at: "2026-10-23T09:40:00Z", id: "q1", kind: "answer", option: "after 6pm",
                text: "Batch jobs hit it until then." },
              { at: "2026-10-23T11:02:00Z", id: "r1", kind: "review", pr: "owenrumney/ade#412" },
              { at: "2026-10-23T11:20:00Z", id: "r1", kind: "answer", mine: true, verdict: "approved" },
            ],
          };
        if (path.split("?")[0] === "/api/review/draft")
          return { pr: 412, review: "REVIEW1", inDraft: 1, ready: 1, notGoing: 1 };
        if (path.split("?")[0] === "/api/state" && REVIEWS[session])
          return { ...READS["/api/state"], reviewRun: REVIEWS[session] };
        return READS[path.split("?")[0]] ?? {};
      },
      // A panel here is the real skeleton with its scheme pointed at the
      // built files and its policy dropped: file:// is not ade-webview:.
      panel: async (id, html) => {
        const base = location.href.replace(/\\/renderer\\/index\\.html.*$/, "");
        const page = html
          .replace(/<meta http-equiv="Content-Security-Policy"[\\s\\S]*?\\/>/, "")
          .split("ade-webview://asset").join(base);
        return URL.createObjectURL(new Blob([page], { type: "text/html" }));
      },
      where: async () => ({ base: "http://127.0.0.1:7777", token: "picture" }),
    };
  `;
}
