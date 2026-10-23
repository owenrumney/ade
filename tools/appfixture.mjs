// A shell for the picture harness: the same bridge the Electron app
// gives the window, answering with fixed data instead of a launcher.
// The window's own code paths are unchanged — this only stands in for
// the process that would reach ade.
export const WORLD = {
  jobs: [
    { name: "editor-bump", schedule: "monthly", nextDue: "2026-11-01T09:00:00Z", project: "ade" },
    { name: "morning-brief", schedule: "weekdays at 08:00", paused: true },
    { name: "drift", schedule: "nightly", failing: true, lastFired: "2026-10-22T02:00:00Z" },
  ],
  todos: [
    { id: "1", text: "Send the review replies on #412", due: "2026-10-21T09:00:00Z" },
    { id: "2", text: "Bump the VSCodium pin", from: "editor-bump", note: "monthly job" },
    { id: "3", text: "Look at the sandbox image size", started: "ade" },
  ],
  spaces: [
    { repo: "owenrumney/ade", owner: "owenrumney", name: "ade", path: "/Users/owen/code/owenrumney/ade",
      branch: "main", sessions: 13, here: true,
      designs: ["spaces.md", "terminal-turns.md", "quiz-me.md"] },
    { repo: "infracost/coast", owner: "infracost", name: "coast", path: "/Users/owen/code/infracost/coast",
      branch: "main", sessions: 19, here: true, designs: ["pricing-rework.md", "usage-tiles.md"] },
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
  designs: [
    {
      name: "rethink-workflow-and-uk",
      dir: "/Users/owen/code/owenrumney/ade/.git/ade/docs",
      file: "rethink-workflow-and-uk.md",
      meta: { repos: ["owenrumney/ade"], title: "Rethink the workflow and the window" },
      repo: "owenrumney/ade",
    },
    {
      name: "agentbus-harness",
      dir: "/Users/owen/code/owenrumney/ade/.git/ade/docs",
      file: "agentbus-harness.md",
      meta: { repos: ["owenrumney/ade"] },
      repo: "owenrumney/ade",
    },
  ],
  limits: {
    claude: {
      provider: "claude",
      checkedAt: "2026-10-23T12:30:00Z",
      windows: [
        { name: "5h", percent: 62, resetsAt: "2026-10-23T16:10:00Z" },
        { name: "7d", percent: 84, resetsAt: "2026-10-27T09:00:00Z" },
      ],
    },
  },
  active: [
    {
      id: "pending-0d210b89",
      dir: "/Users/owen/code/infracost/infra-pr-1535",
      name: "infra-pr-1535",
      repo: "infra",
      branch: "feat/inspector-ec2-scanning",
    },
    {
      id: "ade",
      dir: "/Users/owen/code/owenrumney/ade",
      name: "ade",
      repo: "ade",
      branch: "rethink-workflow-and-uk",
      agent: {
        running: true,
        model: "opus",
        sideRuns: ["security"],
        stacked: [
          { id: "q1", kind: "message", what: "look at the sandbox image size" },
          { id: "q2", kind: "review" },
        ],
        children: [
          { kind: "agent", name: "tester", doing: "reading app/renderer/real.test.ts" },
          { kind: "task", name: "go test ./internal/web", doing: "ok 112.4s" },
        ],
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
        model: "sonnet",
        asking: 1,
        stacked: [{ id: "q3", kind: "message", what: "rerun the entropy tests" }],
      },
    },
    {
      id: "lsp",
      dir: "/Users/owen/code/owenrumney/go-lsp",
      name: "go-lsp",
      repo: "go-lsp",
      branch: "main",
      machine: "mini",
      agent: { running: false },
    },
  ],
  hosts: [
    {
      machine: "mini",
      label: "the mini",
      repos: [
        {
          name: "go-lsp",
          path: "/Users/owen/code/owenrumney/go-lsp",
          checkouts: [
            {
              name: "go-lsp",
              path: "/Users/owen/code/owenrumney/go-lsp",
              branch: "main",
              sessions: [
                {
                  id: "lsp",
                  name: "lsp",
                  title: "New client code, full codebase",
                  branch: "main",
                  dir: "/Users/owen/code/owenrumney/go-lsp",
                  lastUsed: "2026-10-22T07:00:00Z",
                },
              ],
            },
          ],
        },
      ],
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

function cell(line, kind, content) {
  return { line, kind, content };
}

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
    offers: [{ id: "o1", kind: "review", from: "kim", pr: "owenrumney/ade#412" }],
    replies: [{ id: "r1", from: "kim", text: "the proxy hop looks right", at: "2026-10-22T11:40:00Z" }],
    sent: [{ id: "s1", to: "anyone", question: "anyone seen the sandbox image size?" }],
    team: [
      { user: "owen", presence: "active", you: true, agents: 2 },
      { user: "kim", presence: "idle", harness: "claude" },
    ],
  },
};

// A year of commits for the space page's heatmap, worked out rather
// than typed: 365 days of hand-written JSON is not a fixture.
function year() {
  const out = [];
  const start = Date.parse("2025-10-26T00:00:00Z");
  for (let i = 0; i < 364; i++) {
    const at = new Date(start + i * 86_400_000);
    const weekend = at.getUTCDay() === 0 || at.getUTCDay() === 6;
    const commits = weekend ? (i % 9 === 0 ? 2 : 0) : (i * 7) % 11;
    out.push({ date: at.toISOString().slice(0, 10), commits });
  }
  return out;
}

const REPO = {
  repo: "ade",
  dir: "/Users/owen/code/owenrumney/ade",
  branch: "rethink-workflow-and-uk",
  upstream: "origin/rethink-workflow-and-uk",
  ahead: 2,
  behind: 0,
  dirty: 7,
  lines: 184_320,
  files: 612,
  languages: [
    { name: "Go", lines: 121_400, files: 388 },
    { name: "TypeScript", lines: 48_900, files: 164 },
    { name: "CSS", lines: 7_200, files: 14 },
    { name: "Markdown", lines: 5_100, files: 38 },
    { name: "Shell", lines: 1_720, files: 8 },
  ],
  days: year(),
  commits: 1_284,
  busiestDay: "Tuesday",
  contributors: [
    { name: "Owen Rumney", commits: 1_190 },
    { name: "dependabot", commits: 61 },
    { name: "kim", commits: 33 },
  ],
  last: {
    sha: "5927858",
    subject: "rethink workflow and uk (#20)",
    author: "Owen Rumney",
    at: "2026-10-22T18:04:00Z",
  },
  agent: {
    cost: 184.52,
    turns: 926,
    sessions: 13,
    tokens: { input: 1_900_000, output: 410_000, cacheRead: 48_000_000, cacheWrite: 2_300_000, reasoning: 120_000 },
    byDay: Array.from({ length: 30 }, (_, i) => ({
      key: new Date(Date.parse("2026-09-24T00:00:00Z") + i * 86_400_000).toISOString().slice(0, 10),
      cost: Math.round(((i * 13) % 17) * 1.4 * 100) / 100,
      tokens: 0,
      sessions: 1,
    })),
    tools: [
      { key: "Bash", cost: 0, tokens: 0, sessions: 412 },
      { key: "Edit", cost: 0, tokens: 0, sessions: 268 },
      { key: "Read", cost: 0, tokens: 0, sessions: 211 },
    ],
    models: [],
  },
};

const READS = {
  "/api/diff": DIFF,
  "/api/state": STATE,
  "/api/repo": REPO,
  "/api/models": { models: [{ id: "opus", label: "Opus 5.5" }, { id: "sonnet", label: "Sonnet 5.5" }] },
  "/api/roles": { roles: [{ name: "oss", about: "open-source maintainer" }] },
  "/api/agents": {
    agents: [
      { name: "security", description: "trust boundaries and injection", reviews: true, scope: "user" },
      { name: "tester", description: "the cases the tests miss", reviews: true, scope: "user" },
    ],
  },
  "/api/hosts": { hosts: [{ name: "mini", label: "the mini", local: false }] },
  "/api/jobs": {
    jobs: [
      { name: "editor-bump", schedule: "monthly", nextDue: "2026-11-01T09:00:00Z", project: "ade" },
      { name: "morning-brief", schedule: "weekdays at 08:00", paused: true },
      { name: "drift", schedule: "nightly", failing: true, lastFired: "2026-10-22T02:00:00Z" },
    ],
  },
  "/api/tree": {
    files: ["app/renderer/app.ts", "app/renderer/sources.ts", "internal/web/web.go", "README.md"],
  },
  "/api/permissions": {
    allow: ["Bash(go test:*)", "Bash(git status)", "Read(**)", "Edit(internal/**)"],
    deny: ["Bash(rm -rf:*)", "WebFetch"],
    session: ["Bash(gh pr view:*)"],
    free: false,
    file: "/Users/owen/.claude/settings.json",
  },
};

// The script the harness installs before the page loads. It is a string
// because it runs in the page, where this module's scope does not reach.
export function shell() {
  return `
    const WORLD = ${JSON.stringify(WORLD)};
    const READS = ${JSON.stringify(READS)};
    // The real shell takes the keystroke before the page sees it. Chromium
    // cannot, so the stub listens instead: the command names are the same.
    window.ade = {
      watch: async () => ({ ok: true }),
      term: async () => ({ ok: true }),
      termSend() {},
      onTerm() {},
      history: async () => ({ commits: [{ sha: "a1b2c3d", author: "owen", date: "2026-10-21", subject: "Thread the picked session through" }] }),
      onEvent() {},
      onStream() {},
      onCommand(then) {
        document.addEventListener("keydown", (e) => {
          if (!(e.metaKey || e.ctrlKey)) return;
          const of = { k: "omnibox" };
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
      pr: async (repo, number) => ({
        pr: {
          number,
          title: "Add usage tiles to the desk",
          author: "owenrumney",
          url: "https://github.com/" + repo + "/pull/" + number,
          state: "OPEN",
          baseRef: "main",
          headRef: "desk-usage",
          body: "Usage tiles on the desk, one per model.",
          reviews: [{ author: "kim", state: "COMMENTED" }],
        },
        checks: [
          { name: "test", bucket: "fail" },
          { name: "lint", bucket: "pass" },
        ],
      }),
      act: async () => ({}),
      link: async () => ({}),
      tree: async () => ({ files: ["app/renderer/app.ts", "app/renderer/sources.ts", "internal/web/web.go", "README.md"] }),
      file: async (dir, name) => ({ source: "// " + name + "\\npackage web\\n\\nfunc serve() error {\\n  return nil\\n}\\n" }),
      read: async (at, path) => {
        if (path.startsWith("/api/source"))
          return { source: "// internal/web/web.go\\npackage web\\n\\nfunc serve() error {\\n  return nil\\n}\\n" };
        if (path.startsWith("/api/history"))
          return {
            commits: [
              { sha: "a1b2c3d4", author: "owen", at: "2026-10-21T09:00:00Z", subject: "Thread the picked session through" },
            ],
          };
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
        return READS[path.split("?")[0]] ?? {};
      },
      panel: async () => "about:blank",
      where: async () => ({ base: "http://127.0.0.1:7777", token: "picture" }),
    };
  `;
}
