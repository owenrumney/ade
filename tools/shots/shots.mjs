// Drives a running ade and captures named views. A screenshot flag can only
// photograph a first paint; anything behind a click needs a driven browser.
import { mkdir, writeFile } from "node:fs/promises";
import { realpathSync } from "node:fs";
import puppeteer from "puppeteer-core";
import { build as buildEditorChat, buildCommit as buildEditorCommit, buildDesignPrune as buildEditorDesignPrune, buildNewSession as buildEditorNewSession, buildPurge as buildEditorPurge, buildQuiz as buildEditorQuiz, buildSettings as buildEditorSettings, buildWorktrees as buildEditorWorktrees } from "./editor.mjs";
import { address, run as runCommand, settle } from "./workbench.mjs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// serverURL is a ade serving a checkout, when one was started. Optional: the
// launcher views are worth having on their own.
const [url, outDir, serverURL, fixtureDir, ...wanted] = process.argv.slice(2);
// tools/shots/ is two below the checkout, which is where the extension's css
// and bundle are read from.
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
if (!url || !outDir) {
  console.error("usage: shots.mjs <launcher-url> <out-dir> [server-url] [fixtures] [view...]");
  process.exit(2);
}

// The views worth having. Each is a name and what to do before the shutter:
// nothing for the page as it loads, or clicks to reach what is behind them.
const SETTINGS = [
  { find: ".launcher-more > .btn" },
  { click: ".launcher-menu .menuitem", text: "Settings…" },
  { click: "nav button", text: "Appearance" },
];

// BUS is an inbox the fixture cannot have: agentbus is a tailnet away, and
// what it offers comes from other people.
const BUS = {
  online: true,
  user: "owen@infracost.io",
  instance: "owen@infracost.io/ade-7f3a",
  offers: [
    {
      id: "msg_01",
      kind: "ask",
      from: "liam@infracost.io",
      node: "liams-mbp",
      harness: "crit",
      agent: "claude-code",
      model: "claude-opus-5",
      question:
        "The dev RDS instance is at 94% storage and the resize needs a 40 minute window with writes paused. Nothing is scheduled on it tonight. Do you want me to take it now, or wait for the morning?",
      options: ["take it now", "wait for the morning", "leave it to me"],
      createdAt: new Date(Date.now() - 4 * 60 * 1000).toISOString(),
    },
    {
      id: "msg_02",
      kind: "review",
      from: "sam@infracost.io",
      node: "sam-linux",
      harness: "crit",
      repo: "infracost/infra",
      number: 412,
      pr: "https://github.com/infracost/infra/pull/412",
      note: "touches the billing path",
      createdAt: new Date(Date.now() - 26 * 60 * 1000).toISOString(),
    },
  ],
  sent: [
    {
      id: "msg_03",
      kind: "ask",
      to: "sam@infracost.io",
      question: "Is the staging billing job safe to re-run after the migration?",
      at: new Date(Date.now() - 9 * 60 * 1000).toISOString(),
    },
    {
      id: "msg_04",
      kind: "review",
      to: "engineering",
      pr: "https://github.com/infracost/ade/pull/118",
      at: new Date(Date.now() - 51 * 60 * 1000).toISOString(),
      forAgent: true,
    },
  ],
};

// LAUNCHER_VIEWS are of the machine-wide page: no checkout, every repository.
const LAUNCHER_VIEWS = [
  { name: "launcher", steps: [] },
  // Opening a session from the launcher navigates this tab to it: nothing is
  // left to hand a session over to, so landing on the app page is the check.
  {
    name: "launcher-open-recent",
    steps: [{ find: ".recentitem .reviews-title", text: "Equal-width usage bars" }, { waitFor: "nav.rail" }],
  },
  {
    name: "launcher-open-link",
    query: `open=11111111-2222-3333-4444-555555555555&dir=${encodeURIComponent(`${outDir}/repos/ade`)}`,
    steps: [{ waitFor: "nav.rail" }],
  },
  {
    name: "settings-instructions",
    steps: [...SETTINGS.slice(0, 2), { click: "nav button", text: "Instructions" }],
  },
  {
    name: "launcher-palette",
    steps: [{ press: "Control+p" }, { waitFor: ".palette-item" }],
  },
  {
    name: "launcher-new-session",
    steps: [
      { click: "button.btn-primary", text: "New session" },
      { waitFor: ".newsession-target select" },
      { select: ".newsession-target select", value: "" },
      { waitFor: ".newsession-prompt" },
      { find: ".newsession-list .mention-item" },
      { waitFor: ".newsession-list .mention-item.chosen" },
    ],
  },
  { name: "launcher-menu", steps: [{ find: ".launcher-more > .btn" }, { waitFor: ".launcher-menu" }] },
  { name: "launcher-sessions", steps: [{ click: "button", text: "Sessions" }] },
  {
    name: "launcher-stats",
    // Tall: the page is six panels, and the row charts are the last two.
    viewport: { width: 1200, height: 1700 },
    steps: [{ click: "button", text: "Stats" }, { waitFor: ".stats-dial svg" }],
  },
  { name: "launcher-jobs", steps: [{ click: "button", text: "Jobs" }] },
  { name: "launcher-machines", steps: [{ click: "button", text: "Machines" }, { waitFor: ".machine" }] },
  {
    name: "launcher-worktrees",
    steps: [{ click: "button", text: "Worktrees" }],
  },
  {
    name: "launcher-worktrees-picking",
    steps: [
      { click: "button", text: "Worktrees" },
      { waitFor: ".graph-pick" },
      { tick: ".graph-pick" },
    ],
  },
  {
    name: "launcher-worktree-remove",
    steps: [
      { click: "button", text: "Worktrees" },
      { find: ".graph-control" },
      // Fails the view rather than photographing the graph without it.
      { waitFor: ".backdrop" },
    ],
  },
  {
    name: "launcher-job-new",
    steps: [{ click: "button", text: "Jobs" }, { find: "button", text: "New job" }],
  },
  // The job with no clock behind it: the cron field goes, and Run now is all
  // that starts it.
  {
    name: "launcher-job-manual",
    steps: [
      { click: "button", text: "Jobs" },
      { find: "button", text: "New job" },
      { find: "button", text: "Only when I run it" },
    ],
  },
  {
    name: "launcher-job-edit",
    steps: [{ click: "button", text: "Jobs" }, { find: ".jobrow button", text: "Edit" }],
  },
  { name: "settings-appearance", steps: SETTINGS },
  {
    name: "settings-agents",
    steps: [...SETTINGS, { click: "nav button", text: "Agents" }, { waitFor: ".setpanel textarea" }],
  },
  {
    name: "settings-models",
    steps: [...SETTINGS, { click: "nav button", text: "Models" }, { waitFor: ".modeltable" }],
  },
  {
    name: "settings-sandbox",
    steps: [...SETTINGS.slice(0, 2), { click: "nav button", text: "Sandbox" }],
  },
  // The disk panel's detail, and the confirmation for a tree that does not
  // rebuild itself. Reached by the label the fixture checkout is listed under.
  {
    name: "disk-attachments",
    steps: [{ clickText: ["li.diskitem span.recentname", "ade"] }],
  },
  {
    name: "disk-purge-attachments",
    steps: [
      { clickText: ["li.diskitem span.recentname", "ade"] },
      { find: "button", text: "Empty the message attachments" },
    ],
  },
  {
    name: "disk-purge-confirmed",
    steps: [
      { clickText: ["li.diskitem span.recentname", "ade"] },
      { find: "button", text: "Empty the message attachments" },
      { find: "button", text: "Delete them permanently" },
    ],
  },
  // One per palette, so a new one is looked at rather than described.
  ...["ayu", "monokai", "adwaita-dark"].map((palette) => ({
    name: `palette-${palette}`,
    steps: [...SETTINGS, { select: "select", value: palette }],
  })),
];

// SERVER_VIEWS are of a ade serving one checkout, which is where everything
// behind the top bar lives. Reached by aria-label: the buttons are icons.
const SERVER_VIEWS = [
  // Taller than the rest: the dialog is a form and two lists, and a shot that
  // crops it is a shot of the top half of the thing being reviewed.
  {
    name: "project-memory",
    viewport: { width: 1200, height: 1800 },
    steps: [
      { press: "Control+p" },
      { find: ".palette-item .palette-label", text: "Project memory…" },
      { waitFor: ".modal" },
    ],
  },
  { name: "exit-session", steps: [{ find: 'button[aria-label="Leave this session"]' }] },
  // The status bar branch, opened: the repository's other worktrees and how
  // far behind each is, which is where drift gets noticed.
  {
    name: "branch-switcher",
    steps: [{ find: ".store-branchbtn" }, { waitFor: ".branchpop" }],
  },
  // Attachments in the composer: a thumbnail for the picture, a chip for the
  // log. Uploaded through the picker, which is the same route a drop takes.
  {
    name: "chat-attachments",
    steps: [{ upload: [".chat-clip input[type=file]", ["screenshot.png", "run.log"]] }],
  },
  // A drag of files over the composer. Synthesised: there is no way to drive
  // a real drag, and what is being photographed is the response to the event.
  { name: "chat-drop", steps: [{ dragOver: "form.chat-form" }] },
  {
    // The agent's own interface, in place of the conversation. ADE's agent
    // stops while it is there, which is why the composer goes with it.
    name: "chat-takeover",
    steps: [
      { click: "button", text: "Terminal" },
      { waitFor: ".chat-takeover .xterm" },
    ],
  },
  {
    // A dev server's url in tool output is a link, which is what makes a
    // forwarded port on a worker clickable rather than copied by hand.
    name: "chat-tool-link",
    steps: [{ find: ".chat-tool summary" }, { waitFor: ".chat-tool-output a" }],
  },
  { name: "chat-question", steps: [{ waitFor: ".chat-question" }] },
  // The team bus: what somebody else's agent is waiting on you for. The list
  // in the top bar, and one question opened out of it.
  {
    name: "bus-inbox",
    bus: BUS,
    steps: [{ find: ".btn.bus" }, { waitFor: ".bus-menu" }],
  },
  {
    name: "bus-question",
    bus: BUS,
    steps: [
      { find: ".btn.bus" },
      { waitFor: ".bus-menu" },
      { find: ".bus-row .reviews-title" },
      { waitFor: ".bus-question" },
    ],
  },
  // A tool call the agent is holding. The rules already cover `head`, so the
  // card asks only about `curl`, and says what covered the rest.
  {
    name: "chat-permission",
    agent: {
      running: true,
      asking: [
        {
          toolId: "seed-permission",
          tool: "Bash",
          rule: "Bash(curl:*)",
          target: "curl -s https://example.com",
          allowed: ["Bash(head:*)"],
        },
      ],
    },
    steps: [{ scroll: ".chat-permission.open" }],
  },
  // A call a rule decided, with no card: the rule is named on the row.
  { name: "chat-tool-rule", steps: [{ scroll: ".chat-tool-rule" }] },
  // The rules ade is holding you to, as its own page: the editor opens it from
  // the ade palette, and a browser can just visit it.
  { name: "permissions", path: "/permissions", steps: [{ waitFor: "#lists ul" }] },
  // Run free taken: the gate is off for the session, and the page says so
  // until it is back on, because nobody turns back on what they cannot see.
  {
    name: "chat-free",
    agent: { running: true, free: true },
    steps: [{ waitFor: ".chat-free" }],
  },
  // The same card with the turn actually paused on it: the buttons live, and
  // the bar saying so rather than "Agent running".
  {
    name: "chat-question-open",
    agent: { running: true, asking: [{ toolId: "seed-question", tool: "AskUserQuestion" }] },
    steps: [{ waitFor: ".chat-question.open" }, { find: ".chat-question .btn", text: "By path" }],
  },
  // The lightbox over a conversation with a picture in each of two messages:
  // the arrows are what a per-message modal could not offer.
  {
    name: "chat-gallery",
    steps: [{ find: ".chat-msg .chat-imagebtn" }, { waitFor: ".chat-image-full" }],
  },
  {
    name: "chat-gallery-next",
    steps: [
      { find: ".chat-msg .chat-imagebtn" },
      { waitFor: ".chat-image-full" },
      { find: "button.btn", text: "Next \u203a" },
    ],
  },
  // The design picker, and the same list with two documents selected: the
  // fixture has docs and no branch-named one, so this is the state you land in.
  // The Files tab: the three roots on the left, one document previewed.
  {
    name: "files",
    hash: "#files",
    steps: [{ waitFor: ".files-item" }, { find: ".files-item" }, { waitFor: ".files-preview" }],
  },
  {
    name: "files-image",
    hash: "#files",
    steps: [
      { clickText: [".files-item .files-name", "session-registry.png"] },
      { waitFor: ".files-preview-image img" },
    ],
  },
  {
    name: "files-text",
    hash: "#files",
    steps: [{ clickText: [".files-item .files-name", "0004-.env"] }, { waitFor: ".files-text" }],
  },
  { name: "design-picker", hash: "#design", steps: [{ waitFor: ".doclist" }] },
  {
    name: "design-picking",
    hash: "#design",
    steps: [{ waitFor: ".doclist-pick" }, { tick: ".doclist-pick" }],
  },
  // What a named agent's reply carries: its author, and the button that folds
  // what it said into the document rather than leaving it in the chat.
  {
    name: "chat-fold-doc",
    hash: "#design",
    steps: [
      { waitFor: ".doclist" },
      { click: ".linkbtn", text: "cold-sandbox.md" },
      { find: 'button[aria-label^="Chat"]' },
      { waitFor: ".chat-named .chat-act" },
    ],
  },
  // The Diff tab's file list. The hash lands on the tab directly: the tab
  // button carries a count, so matching it by its text is brittle.
  { name: "diff-files", hash: "#diff", steps: [{ waitFor: ".stage-all" }] },
  {
    name: "diff-scope-menu",
    hash: "#diff",
    steps: [{ find: ".scopepick .btn" }, { waitFor: ".scopepick-menu" }],
  },
  {
    name: "diff-picking",
    hash: "#diff",
    steps: [
      { find: ".stage-pick" },
      { waitFor: ".filecheck" },
      { tick: ".filecheck" },
    ],
  },
  // The two filters side by side: pick severities and categories and the diff
  // narrows to their intersection.
  {
    name: "diff-filters",
    hash: "#diff",
    steps: [
      { waitFor: ".classlist" },
      { find: ".classchip.sev-low" },
      { find: ".classchip.sev-medium" },
      { find: ".classchip.sev-other" },
    ],
  },
  // The repository switcher: which roots are offered, and the bar with a
  // foreign one on screen, where everything that writes is off.
  {
    name: "diff-root-menu",
    hash: "#diff",
    steps: [{ waitFor: ".diffroot-btn" }, { find: ".diffroot-btn" }, { waitFor: ".diffroot-menu" }],
  },
  {
    name: "diff-root-away",
    hash: "#diff",
    steps: [
      { waitFor: ".diffroot-btn" },
      { find: ".diffroot-btn" },
      { find: ".diffroot-item:not(.here)" },
      { waitFor: ".diffroot-btn.away" },
    ],
  },
  // Who the review picker offers: the agents that review, and not @architect,
  // @condenser, @commenter, @fixer or @eli5.
  // A reviewer's comment handed to the chat: the prompt written for you, the
  // comment quoted under it, and the rest of the review folded in.
  {
    name: "comment-investigate",
    hash: "#diff",
    steps: [
      { waitFor: ".comment" },
      { clickText: [".btn", "Investigate"] },
      { waitFor: ".chat-attachment" },
    ],
  },
  {
    name: "diff-reviewers",
    hash: "#diff",
    steps: [
      { waitFor: ".stage-all" },
      { find: 'button[aria-label="Who reviews"]' },
      { waitFor: ".reviewer-menu" },
    ],
  },
  // What ade wrote, debug lines included: the window that answers "it says it
  // failed and I cannot see why".
  {
    name: "diff-logs",
    hash: "#diff",
    steps: [
      { press: "Control+p" },
      { find: ".palette-item .palette-label", text: "Logs" },
      { waitFor: ".logs-body" },
      { find: ".logs-bar .seg", text: "debug" },
    ],
  },
  // The game in the corner of the diff it is meant to be read around, and the
  // same window paused because the agent has something to say.
  {
    name: "tetris",
    hash: "#diff",
    steps: [
      { press: "Control+p" },
      { find: ".palette-item .palette-label", text: "Tetris" },
      { waitFor: ".float" },
    ],
  },
  {
    name: "tetris-paused",
    hash: "#diff",
    steps: [
      { press: "Control+p" },
      { find: ".palette-item .palette-label", text: "Tetris" },
      { waitFor: ".float" },
      { find: ".tetris-foot button", text: "Pause" },
      { waitFor: ".float-note" },
    ],
  },
  // Both ways to start, now that the split button has gone: the palette rows
  // that replaced it, and the scratch modal one of them opens.
  {
    name: "new-session-commands",
    // Typed after the box has the focus, or the first letters land on the body.
    steps: [
      { press: "Control+p" },
      { waitFor: ".palette input:focus" },
      { type: "session" },
      { waitFor: ".palette-item" },
    ],
  },
  {
    name: "new-session-scratch",
    viewport: { width: 1200, height: 1100 },
    steps: [
      { press: "Control+p" },
      { find: ".palette-item .palette-label", text: "New scratch session…" },
      { waitFor: ".modal" },
    ],
  },
  // The shell, and the same shell split twice: tabs along the bar, dividers
  // between the panes.
  {
    name: "terminal",
    hash: "#terminal",
    steps: [{ waitFor: ".terminal-host .xterm" }],
  },
  {
    name: "terminal-split",
    hash: "#terminal",
    steps: [
      { waitFor: ".terminal-host .xterm" },
      { find: 'button[aria-label="New terminal"]' },
      { find: 'button[aria-label="Split right"]' },
      { find: 'button[aria-label="Split down"]' },
      { waitFor: ".term-divider" },
    ],
  },
  // The palette's terminal commands, reached from the chat: ⌘⇧D is the
  // browser's own, and inside a pane xterm cancels the keys the shell uses.
  {
    name: "terminal-commands",
    steps: [
      { press: "Control+p" },
      { waitFor: ".palette input:focus" },
      { type: "Terminal:" },
      { waitFor: ".palette-item" },
    ],
  },
  // From a tab that has never been opened: the split has to wait for it.
  {
    name: "terminal-commanded",
    steps: [
      { press: "Control+p" },
      { waitFor: ".palette input:focus" },
      { type: "split right" },
      { find: ".palette-item .palette-label", text: "Terminal: split right" },
      { waitFor: ".term-divider" },
    ],
  },
  // Ctrl+I from a tab the box is not on: it raises the chat and takes the caret.
  {
    name: "chat-focus-key",
    hash: "#diff",
    steps: [
      { waitFor: ".stage-all" },
      { press: "Control+i" },
      { waitFor: "form.chat-form textarea:focus" },
    ],
  },
  {
    name: "chat-focus-command",
    steps: [
      { press: "Control+p" },
      { waitFor: ".palette input:focus" },
      { type: "Focus the chat" },
      { waitFor: ".palette-item" },
    ],
  },
  // The activity bar, on a tab that is not the one it opens on.
  { name: "rail", hash: "#diff", steps: [{ waitFor: ".rail-btn.active" }] },
  // Ctrl+S: the sessions that are up, and where you were lately.
  {
    name: "switch-session",
    // Not .hint: that also matches the loading line, which would pass empty.
    steps: [{ press: "Control+s" }, { waitFor: ".switch-item, .switch-empty" }],
  },
  // Last of these: it stops the server, so nothing after it has one to drive.
  // The exit dialog is the route a user actually takes to the closed screen.
  {
    name: "session-closed",
    steps: [
      { find: 'button[aria-label="Leave this session"]' },
      { find: "button.btn-danger", text: "Stop ade" },
      { waitFor: ".standing" },
    ],
  },
];

// PROXIED_VIEWS are of a session reached through the launcher, which is the
// only place the rail's session group appears: a page a checkout served has no
// launcher on its origin to ask.
const FIXTURE_SESSION = "11111111-2222-3333-4444-555555555555";
const PROXIED_VIEWS = [
  { name: "session-rail", hash: "#diff", steps: [{ waitFor: ".rail-session" }] },
  {
    name: "session-rail-keys",
    steps: [{ waitFor: ".rail-session" }, { hold: "Control" }],
  },
  // The build modal, which now names the agent the build starts on.
  {
    name: "design-build",
    hash: "#design",
    steps: [
      { waitFor: ".doclist" },
      { click: ".linkbtn", text: "cold-sandbox.md" },
      { waitFor: ".design-body" },
      { find: "button", text: "Build" },
      { waitFor: ".choice" },
    ],
  },
  // The markdown behind the rendered page.
  {
    name: "design-source",
    hash: "#design",
    steps: [
      { waitFor: ".doclist" },
      { click: ".linkbtn", text: "cold-sandbox.md" },
      { waitFor: ".design-body" },
      { find: "button", text: "Source" },
      { waitFor: ".design-source" },
    ],
  },
  // Proxied, because that is where the mermaid bundle is fetched from the
  // wrong origin if its src is root-absolute.
  {
    name: "session-rail-diagram",
    hash: "#design",
    steps: [
      { waitFor: ".doclist" },
      { click: ".linkbtn", text: "session-registry.md" },
      { waitFor: ".markdown-body svg" },
    ],
  },
];

// EDITOR_VIEWS are the VS Code extension's own pages, which are html of their
// own rather than ade's. Narrow and wide: the complaint about the chat was
// that a table had 46rem of a 2000px editor.
const EDITOR_VIEWS = [
  { name: "editor-settings", viewport: { width: 900, height: 900 }, steps: [] },
  { name: "editor-commit", viewport: { width: 900, height: 1000 }, steps: [] },
  // The worktree panel, as a glance and with a lane open to its commits.
  { name: "editor-worktrees", viewport: { width: 1100, height: 720 }, steps: [] },
  {
    name: "editor-worktrees-open",
    viewport: { width: 1100, height: 900 },
    steps: [{ click: ".lane-name", text: "checks-in-ci" }, { waitFor: ".commits .commit" }],
  },
  {
    name: "editor-worktrees-remove",
    viewport: { width: 1100, height: 720 },
    steps: [{ waitFor: ".bar button" }],
  },
  // The purge panel: every worktree on the machine, with a row ticked.
  {
    name: "editor-purge",
    viewport: { width: 1100, height: 720 },
    steps: [{ find: ".row.free .pick input" }, { waitFor: ".bar .remove" }],
  },
  // The design prune panel: one repository swept, one that could not be, and
  // one still going.
  {
    name: "editor-designprune",
    viewport: { width: 1100, height: 720 },
    steps: [{ waitFor: ".bar .go" }],
  },
  // The quiz: a question just marked, and the scorecard it ends on.
  {
    name: "editor-quiz",
    viewport: { width: 900, height: 900 },
    steps: [{ find: ".option" }, { waitFor: ".verdict" }],
  },
  {
    name: "editor-quiz-scorecard",
    viewport: { width: 900, height: 1100 },
    steps: [
      { find: ".option" },
      { click: "button", text: "Next" },
      { find: ".option" },
      { click: "button", text: "Next" },
      { find: ".option" },
      { click: "button", text: "Next" },
      { find: ".option" },
      { click: "button", text: "See the scorecard" },
      { waitFor: ".tally" },
    ],
  },
  // The new-session screen: the five ways in, two repositories ticked, and
  // what it would make listed before you press Start.
  { name: "editor-newsession", viewport: { width: 1100, height: 820 }, steps: [] },
  {
    name: "editor-newsession-design",
    viewport: { width: 1100, height: 780 },
    steps: [{ click: ".mode-label", text: "Design" }, { waitFor: ".make" }],
  },
  // Starting on another machine: the documents here are offered too, and the
  // one picked is copied over when it starts.
  {
    name: "editor-newsession-elsewhere",
    viewport: { width: 1100, height: 720 },
    steps: [{ click: ".mode-label", text: "Design" }, { waitFor: ".make" }],
  },
  {
    name: "editor-newsession-direct",
    viewport: { width: 1100, height: 760 },
    steps: [{ click: ".mode-label", text: "Direct" }, { waitFor: ".make" }],
  },
  { name: "editor-chat", viewport: { width: 900, height: 900 }, steps: [] },
  { name: "editor-chat-wide", viewport: { width: 1800, height: 900 }, steps: [] },
  // A failing check's verdict, folded open: the note and its output.
  {
    name: "editor-chat-checks",
    viewport: { width: 900, height: 900 },
    steps: [
      { find: ".note-row.failed > summary" },
      { waitFor: ".note-row.failed pre" },
    ],
  },
  // The copy button a fenced block keeps in its corner, under the pointer.
  {
    name: "editor-chat-copy",
    viewport: { width: 900, height: 900 },
    steps: [{ scroll: ".body .block" }, { hover: ".body .block" }],
  },
  {
    name: "editor-chat-undo",
    viewport: { width: 900, height: 900 },
    steps: [{ click: ".linkact", text: "Undo" }, { waitFor: ".undo" }],
  },
  {
    name: "editor-chat-hidden",
    viewport: { width: 900, height: 900 },
    steps: [{ click: ".linkact", text: "Hide" }, { waitFor: ".msg.hid" }],
  },
  // Two prompts typed during one run, sent as one turn: the second under the first.
  {
    name: "editor-chat-welded",
    viewport: { width: 900, height: 900 },
    steps: [{ scroll: ".folded" }, { waitFor: ".fold-note" }],
  },
  // Named agents at work beside the main one, which the conversation used to
  // show no sign of at all.
  {
    name: "editor-chat-sides",
    viewport: { width: 900, height: 900 },
    agent: {
      running: true,
      sideRuns: [
        { id: "s1", name: "security", startedAt: new Date(Date.now() - 74000).toISOString() },
        { id: "s2", name: "simplifier", startedAt: new Date(Date.now() - 12000).toISOString() },
        { id: "s3", name: "staff-reviewer", startedAt: new Date(Date.now() - 3000).toISOString() },
      ],
    },
    steps: [{ scroll: ".side-row" }, { waitFor: ".side-row" }],
  },
  // The editor draws the same conversation, so a question has to land there too.
  {
    name: "editor-chat-question",
    viewport: { width: 900, height: 900 },
    agent: { running: true, asking: [{ toolId: "seed-question", tool: "AskUserQuestion" }] },
    steps: [{ waitFor: ".question.open" }],
  },
  // What the sharpener asks about a vague prompt, above Send.
  {
    name: "editor-chat-sharpen",
    viewport: { width: 900, height: 900 },
    sharpen: [
      { ask: "Which login is slow?", options: ["Web sign-in", "CLI token refresh"] },
      { ask: "Faster by how much?", options: ["Under 1 second", "Any gain", "Match the last release"] },
    ],
    steps: [{ find: "#box" }, { type: "make the login faster" }, { waitFor: "#sharpen.on" }],
  },
];

// CODE_VIEWS are of the editor itself, served by `code serve-web`: the trees,
// rows and menus the extension draws natively. Each opens the ADE sidebar first.
const CODE_VIEWPORT = { width: 1400, height: 1000 };
const pane = (title) => ({ find: ".pane-header .title", text: title });
// Diff, Comments and Team open by default and crowd out whatever is below, so
// a view of one pane shuts those three first. pause is for the tree's own read.
const only1 = (title) => [
  { run: "View: Show ADE" },
  pane("Diff"),
  pane("Comments"),
  pane("Team"),
  pane(title),
  { pause: 2500 },
];
const CODE_VIEWS = [
  { name: "code-sidebar", viewport: CODE_VIEWPORT, steps: [{ run: "View: Show ADE" }] },
  // The desk itself, in the panel beside the terminal.
  {
    name: "code-concierge",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Concierge" }, { pause: 600 }, { press: "Enter" }, { pause: 6000 }],
  },
  // The card: what the desk proposed, and the press that is not its to make.
  {
    name: "code-card",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Concierge" }, { pause: 600 }, { press: "Enter" }, { pause: 6000 }],
  },
  // The todo list, which is a quick pick and not a pane.
  {
    name: "code-todos",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Todo" }, { pause: 600 }, { press: "Enter" }, { pause: 2500 }],
  },
  // Hovered: the thread menu is proposed api, and it only draws on the row
  // under the pointer. shots.sh turns the proposal on in the served product.
  {
    name: "code-comments",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "Comments: Focus on Comments View" },
      { pause: 2500 },
      { hover: ".comments-panel .monaco-list-row:nth-child(4)" },
      { pause: 1200 },
    ],
  },
  { name: "code-drift", viewport: CODE_VIEWPORT, steps: only1("Drift") },
  { name: "code-design", viewport: CODE_VIEWPORT, steps: only1("Design") },
  // The design document as it reads, drawn rather than written out.
  {
    name: "code-design-rendered",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "ade" },
      { type: "Design" },
      { pause: 800 },
      { press: "Enter" },
      { pause: 2500 },
      { type: "cold-sandbox" },
      { pause: 1200 },
      { press: "Enter" },
      { pause: 2500 },
      { press: "Enter" },
      { pause: 9000 },
    ],
  },
  { name: "code-machines", viewport: CODE_VIEWPORT, steps: only1("Machines") },
  { name: "code-worktrees", viewport: CODE_VIEWPORT, steps: only1("Worktrees") },
  {
    name: "code-worktree-graph",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "how the worktrees overlap" }, { pause: 600 }, { press: "Enter" }, { pause: 6000 }],
  },
  { name: "code-sessions", viewport: CODE_VIEWPORT, steps: only1("Sessions") },
  {
    name: "code-stats",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Show what has been spent" }, { pause: 600 }, { press: "Enter" }, { pause: 5000 }],
  },
  {
    name: "code-graph",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Show what is running" }, { pause: 600 }, { press: "Enter" }, { pause: 5000 }],
  },
  {
    name: "code-sync",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Sync a repository" }, { pause: 600 }, { press: "Enter" }, { pause: 6000 }],
  },
  {
    name: "code-disk",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "costing in space" }, { pause: 600 }, { press: "Enter" }, { pause: 7000 }],
  },
  {
    name: "code-jobs",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Scheduled jobs" }, { pause: 600 }, { press: "Enter" }, { pause: 3000 }],
  },
  {
    name: "code-memory",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "remembers about this project" }, { pause: 600 }, { press: "Enter" }, { pause: 3000 }],
  },
  {
    name: "code-roles",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Role for new chats" }, { pause: 600 }, { press: "Enter" }, { pause: 2500 }],
  },
  {
    name: "code-usage",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Show plan usage" }, { pause: 600 }, { press: "Enter" }, { pause: 5000 }],
  },
  {
    name: "code-session-menu",
    viewport: CODE_VIEWPORT,
    steps: [
      ...only1("Sessions"),
      { menu: [".monaco-list-row .label-name", "A week of work"] },
      { pause: 800 },
    ],
  },
  { name: "code-menu", viewport: CODE_VIEWPORT, steps: [{ run: "ade" }] },
  {
    name: "code-settings",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Settings" }, { pause: 600 }, { press: "Enter" }, { pause: 2500 }],
  },
  {
    name: "code-keys",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Keys and tokens" }, { pause: 600 }, { press: "Enter" }, { pause: 2000 }],
  },
  {
    name: "code-sandbox",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Sandbox mounts" }, { pause: 600 }, { press: "Enter" }, { pause: 2000 }],
  },
  {
    name: "code-diffroot",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Choose which repository" }, { pause: 600 }, { press: "Enter" }, { pause: 2000 }],
  },
  {
    name: "code-preview",
    viewport: CODE_VIEWPORT,
    steps: [{ run: "ade" }, { type: "Set the page to capture" }, { pause: 600 }, { press: "Enter" }, { pause: 2000 }],
  },
  // A file with comments on it, opened and its threads unfolded: where the
  // thread verbs are drawn.
  {
    name: "code-thread",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "Go to File..." },
      { type: "main.go" },
      { pause: 800 },
      { press: "Enter" },
      { pause: 2500 },
      { run: "Comments: Expand All Comments" },
      { pause: 1500 },
    ],
  },
  // The blame of the cursor's line, which is a pick and not a pane.
  {
    name: "code-blame",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "Go to File..." },
      { type: "README.md" },
      { pause: 800 },
      { press: "Enter" },
      { pause: 2500 },
      { run: "ade" },
      { type: "Blame" },
      { pause: 600 },
      { press: "Enter" },
      { pause: 2500 },
    ],
  },
  // A file's commits, newest first, each one openable as it was then.
  {
    name: "code-history",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "Go to File..." },
      { type: "README.md" },
      { pause: 800 },
      { press: "Enter" },
      { pause: 2500 },
      { run: "ade" },
      { type: "History" },
      { pause: 600 },
      { press: "Enter" },
      { pause: 2500 },
    ],
  },
  // The file as that commit left it, read-only beside the working tree.
  {
    name: "code-file-at",
    viewport: CODE_VIEWPORT,
    steps: [
      { run: "Go to File..." },
      { type: "README.md" },
      { pause: 800 },
      { press: "Enter" },
      { pause: 2500 },
      { run: "ade" },
      { type: "History" },
      { pause: 600 },
      { press: "Enter" },
      { pause: 2500 },
      { press: "Enter" },
      { pause: 2500 },
    ],
  },
];

// A run can name the views it wants. A reply carries four pictures at most, so
// shooting the whole gallery buries the two that are about the change.
const only = (views) =>
  wanted.length === 0 ? views : views.filter((view) => wanted.includes(view.name));

const known = new Set(
  [...LAUNCHER_VIEWS, ...SERVER_VIEWS, ...PROXIED_VIEWS, ...EDITOR_VIEWS, ...CODE_VIEWS].map(
    (view) => view.name,
  ),
);
const unknown = wanted.filter((name) => !known.has(name));
if (unknown.length) {
  console.error(`no view named: ${unknown.join(", ")}`);
  console.error(`known: ${[...known].sort().join(", ")}`);
  process.exit(2);
}

// VIEWPORT is what a view gets unless it asks for more.
const VIEWPORT = { width: 1200, height: 800 };

// clickByText finds the control by what it says rather than by a class, so a
// restyle does not silently stop the capture reaching the view.
async function clickByText(page, selector, text) {
  const handle = await page.evaluateHandle(
    (sel, want) =>
      [...document.querySelectorAll(sel)].find(
        (el) => el.textContent?.trim() === want,
      ) ?? null,
    selector,
    text,
  );
  const element = handle.asElement();
  if (!element) throw new Error(`no ${selector} saying ${JSON.stringify(text)}`);
  await element.click();
}

// waitForText waits for a control that arrives with a fetch rather than with
// the page: waitForSelector alone matches a button that is already there.
async function waitForText(page, selector, text) {
  await page.waitForFunction(
    (sel, want) =>
      [...document.querySelectorAll(sel)].some((el) => el.textContent?.trim() === want),
    { timeout: 5000 },
    selector,
    text,
  );
}

// dragFilesOver fakes the dragover a real drag would send. A DataTransfer
// holding a file is what puts "Files" in types, which is what ade accepts.
async function dragFilesOver(page, selector) {
  await page.waitForSelector(selector, { timeout: 5000 });
  await page.evaluate((sel) => {
    const data = new DataTransfer();
    data.items.add(new File(["x"], "spec.pdf", { type: "application/pdf" }));
    document
      .querySelector(sel)
      .dispatchEvent(new DragEvent("dragover", { dataTransfer: data, bubbles: true }));
  }, selector);
}

// press sends a chord. The palette is only reachable by one, and what is
// behind it cannot be photographed without it.
async function press(page, chord) {
  const keys = chord.split("+");
  const last = keys.pop();
  for (const mod of keys) await page.keyboard.down(mod);
  await page.keyboard.press(last);
  for (const mod of keys.reverse()) await page.keyboard.up(mod);
}

const browser = await puppeteer.launch({
  executablePath: process.env.ADE_CHROME || "/usr/bin/chromium",
  headless: true,
  args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
});

// fakeState merges fragments into every /api/state the page reads, so a view
// can photograph what the fixture cannot reach — a paused turn needs a live
// agent holding a question, and an inbox needs a bus with somebody on it.
async function fakeState(page, agent, bus, sharpen) {
  await page.evaluateOnNewDocument((over) => {
    if (!over.agent && !over.bus && !over.sharpen) return;
    const real = window.fetch;
    window.fetch = async (...args) => {
      const url = String(args[0]?.url ?? args[0] ?? "");
      // The sharpener is a model call, which the fixture home cannot make.
      if (over.sharpen && url.includes("/api/sharpen")) {
        return new Response(JSON.stringify({ questions: over.sharpen }), {
          headers: { "content-type": "application/json" },
        });
      }
      const res = await real(...args);
      if (!url.includes("/api/state")) return res;
      const body = await res.clone().json();
      if (over.agent) body.agent = { ...body.agent, ...over.agent };
      if (over.bus) body.bus = over.bus;
      return new Response(JSON.stringify(body), {
        headers: { "content-type": "application/json" },
      });
    };
  }, { agent: agent ?? null, bus: bus ?? null, sharpen: sharpen ?? null });
}

// capture drives one page to each view and photographs it.
// after, when given, replaces the loaded page with something else on the same
// origin: the editor's webview is its own html but ade's fetches.
async function capture(page, at, views, after) {
  for (const view of views) {
    await page.setViewport(view.viewport ?? VIEWPORT);
    await fakeState(page, view.agent, view.bus, view.sharpen);
    // Every view starts from a fresh load, so one that fails cannot leave the
    // next one photographing a modal it did not open.
    // A view may name a page of ade's own rather than a tab of the app, in
    // which case the path changes and the query carrying the token does not.
    const to = view.path
      ? at.replace(/\/\?/, `${view.path}?`)
      : at + (view.hash ?? "");
    const asked = view.query ? to.replace("/?", `/?${view.query}&`) : to;
    await page.goto(asked, { waitUntil: "networkidle2" });
    try {
      if (after) await after(page);
      // A goto that only changes the hash is same-document, and the app reads
      // the hash once at mount; reload so the tab it names is the one that opens.
      if (view.hash) await page.reload({ waitUntil: "networkidle2" });
      for (const step of view.steps) {
        if (step.menu) {
          // A right click, for the verbs a row keeps in its context menu.
          const [selector, text] = step.menu;
          await waitForText(page, selector, text);
          const row = await page.evaluateHandle(
            (sel, want) =>
              [...document.querySelectorAll(sel)].find((el) => el.textContent?.trim() === want) ?? null,
            selector,
            text,
          );
          await row.asElement().click({ button: "right" });
        } else if (step.pause) {
          await new Promise((done) => setTimeout(done, step.pause));
        } else if (step.run) {
          await runCommand(page, step.run);
        } else if (step.press) {
          await press(page, step.press);
        } else if (step.hold) {
          // Held, not pressed: the rail labels itself while the key is down.
          await page.keyboard.down(step.hold);
        } else if (step.type) {
          await page.keyboard.type(step.type);
        } else if (step.select) {
          await page.select(step.select, step.value);
        } else if (step.upload) {
          const [selector, names] = step.upload;
          await page.waitForSelector(selector, { timeout: 5000 });
          const input = await page.$(selector);
          await input.uploadFile(...names.map((name) => `${fixtureDir}/${name}`));
        } else if (step.hover) {
          await page.waitForSelector(step.hover, { timeout: 15000 });
          await page.hover(step.hover);
        } else if (step.waitFor) {
          await page.waitForSelector(step.waitFor, { timeout: 15000 });
        } else if (step.scroll) {
          await page.waitForSelector(step.scroll, { timeout: 15000 });
          await page.$eval(step.scroll, (el) => el.scrollIntoView({ block: "center" }));
        } else if (step.tick) {
          // Every box, not the first: the view is of both buttons live at once.
          await page.waitForSelector(step.tick, { timeout: 5000 });
          for (const box of await page.$$(step.tick)) await box.click();
        } else if (step.dragOver) {
          await dragFilesOver(page, step.dragOver);
        } else if (step.clickText) {
          const [selector, text] = step.clickText;
          await page.waitForSelector(selector, { timeout: 5000 });
          await clickByText(page, selector, text);
        } else if (step.find && step.text) {
          await waitForText(page, step.find, step.text);
          await clickByText(page, step.find, step.text);
        } else if (step.find) {
          await page.waitForSelector(step.find, { timeout: 5000 });
          await page.click(step.find);
        } else {
          await clickByText(page, step.click, step.text);
        }
      }
      // The panels fill from their own fetches; settle before the shutter.
      await new Promise((done) => setTimeout(done, 1200));
      const path = `${outDir}/${view.name}.png`;
      await page.screenshot({ path, fullPage: false });
      console.log(path);
      await page.keyboard.up("Control").catch(() => {});
    } catch (err) {
      // One unreachable view is not a reason to lose the others.
      const path = `${outDir}/${view.name}.FAILED.txt`;
      await writeFile(path, String(err));
      console.error(`${view.name}: ${err}`);
    }
  }
}

try {
  await mkdir(outDir, { recursive: true });
  const page = await browser.newPage();

  await capture(page, url, only(LAUNCHER_VIEWS));
  if (serverURL) await capture(page, serverURL, only(SERVER_VIEWS));
  await capture(page, url.replace("/?t=", `/s/${FIXTURE_SESSION}/?t=`), only(PROXIED_VIEWS));
  if (serverURL) {
    // Each of these has a builder of its own, so none of them wants the
    // chat harness. The worktree views are matched by prefix: there are three.
    const own = (name) =>
      ["editor-settings", "editor-commit", "editor-purge", "editor-designprune"].includes(name) ||
      name.startsWith("editor-worktrees") ||
      name.startsWith("editor-newsession") ||
      name.startsWith("editor-quiz");
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => !own(view.name))), (p) =>
      buildEditorChat(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name === "editor-commit")), (p) =>
      buildEditorCommit(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name === "editor-settings")), (p) =>
      buildEditorSettings(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name.startsWith("editor-worktrees"))), (p) =>
      buildEditorWorktrees(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name === "editor-purge")), (p) =>
      buildEditorPurge(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name === "editor-designprune")), (p) =>
      buildEditorDesignPrune(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name.startsWith("editor-newsession") && view.name !== "editor-newsession-elsewhere")), (p) =>
      buildEditorNewSession(p, repoRoot),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name === "editor-newsession-elsewhere")), (p) =>
      buildEditorNewSession(p, repoRoot, true),
    );
    await capture(page, serverURL, only(EDITOR_VIEWS.filter((view) => view.name.startsWith("editor-quiz"))), (p) =>
      buildEditorQuiz(p, repoRoot),
    );
  }
  const codeURL = process.env.ADE_CODE_URL;
  // A profile per view: the workbench remembers which panes are open, and one
  // view's clicks would otherwise be the next one's starting point.
  for (const view of codeURL ? only(CODE_VIEWS) : []) {
    const profile = await browser.createBrowserContext();
    try {
      // The real path: ade names files under /private/tmp on a Mac, and a thread
      // pinned there is not on the same file as one opened through /tmp.
      const at = address(codeURL, realpathSync(`${outDir}/repos/ade`));
      await capture(await profile.newPage(), at, [view], settle);
    } finally {
      await profile.close();
    }
  }
} finally {
  await browser.close();
}
