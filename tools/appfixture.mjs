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
        },
      ],
    },
    started: { ade: { dir: "/Users/owen/code/owenrumney/ade", branch: "rethink-workflow-and-uk", session: "ade" } },
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
  active: [
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
      side: "new",
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
      id: "t2",
      file: "app/renderer/sources.ts",
      side: "new",
      line: 4,
      resolved: true,
      comments: [{ id: "c3", author: "@tester", at: "2026-10-22T10:05:00Z", body: "No test for an empty answer." }],
    },
  ],
  agent: { sideRuns: [{ id: "s1", name: "security", startedAt: "2026-10-22T09:40:00Z" }] },
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

const READS = {
  "/api/diff": DIFF,
  "/api/state": STATE,
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
      world: async () => WORLD,
      pr: async () => ({ error: "no GitHub in a picture" }),
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
        return READS[path.split("?")[0]] ?? {};
      },
      panel: async () => "about:blank",
      where: async () => ({ base: "http://127.0.0.1:7777", token: "picture" }),
    };
  `;
}
