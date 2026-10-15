// The VS Code chat webview, photographed outside VS Code. The page is plain
// html, its own css and one bundle, so it can be built over a page already on
// ade's origin — which is what makes its fetches work with no CORS at all.
import { readFile } from "node:fs/promises";

// Dark+ as the webview would be handed it. chat.css names these and nothing
// else, so without them the shot is unstyled text on white.
const THEME = {
  "font-family": "-apple-system, Ubuntu, 'Droid Sans', sans-serif",
  "font-size": "13px",
  "editor-font-family": "'SF Mono', Menlo, Consolas, monospace",
  foreground: "#cccccc",
  "editor-foreground": "#cccccc",
  descriptionForeground: "#9d9d9d",
  errorForeground: "#f14c4c",
  focusBorder: "#0078d4",
  "editor-background": "#1f1f1f",
  "editorWidget-background": "#252526",
  "editorWidget-border": "#454545",
  "panel-border": "#2b2b2b",
  "widget-border": "#3c3c3c",
  "textBlockQuote-background": "#2a2d2e",
  "textCodeBlock-background": "#0a0a0a",
  "textLink-foreground": "#4daafc",
  "badge-background": "#4d4d4d",
  "badge-foreground": "#ffffff",
  "button-background": "#0078d4",
  "button-foreground": "#ffffff",
  "button-hoverBackground": "#026ec1",
  "button-secondaryBackground": "#313131",
  "button-secondaryForeground": "#cccccc",
  "button-secondaryHoverBackground": "#3c3c3c",
  "input-background": "#313131",
  "input-foreground": "#cccccc",
  "input-border": "#3c3c3c",
  "keybindingTable-headerBackground": "#2a2a2a",
  "keybindingTable-rowsBackground": "#242424",
  "charts-green": "#89d185",
  "editorWarning-foreground": "#cca700",
  "gitDecoration-addedResourceForeground": "#81b88b",
  "gitDecoration-deletedResourceForeground": "#c74e39",
};

// The body of Chat.page in vscode/src/chat.ts. Kept in step by
// TestTheShotHarnessDrawsTheSameChatPage, which reads both.
const BODY = `
  <div id="ade" data-base="BASE">
    <div id="list"></div>
    <div id="composer">
      <div id="tray"></div>
      <textarea id="box" placeholder="Ask for something. It can read the repository, answer, or make the change."></textarea>
      <div id="sharpen"></div>
      <div id="row">
        <button id="send">Send</button>
        <button id="mic" aria-label="Dictate" title="Hold to dictate. Cmd+Alt+V or Ctrl+Alt+V toggles it."><svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z" /><path d="M19 11v1a7 7 0 0 1-14 0v-1" /><path d="M12 19v3" /><path d="M8 22h8" /></svg></button>
        <button id="stop" title="Cancel the running turn. The session stays up." hidden>Stop</button>
        <span id="hint">Enter to send · Shift+Enter for a new line · paste or drop an image</span>
      </div>
      <div id="note"></div>
    </div>
  </div>`;

// build replaces whatever ade served with the webview, on ade's own origin.
// The token goes in the way the editor hands it over: a posted message the
// page asks for, so the handshake is the real one.
export async function build(page, repoRoot) {
  await page.evaluate(
    (vars, body) => {
      // Read off the page rather than off the url it was asked for: a server
      // with a launcher watching redirects onto the launcher's proxy, and
      // fetching the address behind it is a cross-origin request.
      const at = location.origin + location.pathname.replace(/\/+$/, "");
      const tok = new URLSearchParams(location.search).get("t") ?? "";
      const root = document.documentElement;
      for (const [name, value] of Object.entries(vars))
        root.style.setProperty(`--vscode-${name}`, value);
      document.body.innerHTML = body.replace("BASE", at);
      // The editor's own api, as much of it as the page uses: it stands in
      // for the extension host, which makes every request now.
      const reply = (data) => window.dispatchEvent(new MessageEvent("message", { data }));
      window.acquireVsCodeApi = () => ({
        postMessage(message) {
          if (message?.kind === "ready") {
            reply({ kind: "token", token: tok });
            reply({ kind: "stream", open: true });
            return;
          }
          if (message?.kind !== "api") return;
          fetch(`${at}${message.path}${message.path.includes("?") ? "&" : "?"}t=${encodeURIComponent(tok)}`, {
            method: message.method ?? "GET",
            headers: { "X-Ade-Token": tok, "Content-Type": "application/json" },
            body: message.body,
          })
            .then((res) => res.text().then((text) => reply({ kind: "api.result", id: message.id, ok: res.ok, status: res.status, text })))
            .catch((err) => reply({ kind: "api.result", id: message.id, ok: false, status: 0, error: String(err) }));
        },
      });
    },
    THEME,
    BODY,
  );

  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/chat.css`, "utf8") });
  // The page says why it could not connect in #note; without this a failed
  // fetch is only ever a selector timeout.
  const said = [];
  page.on("console", (m) => said.push(m.text()));
  page.on("pageerror", (e) => said.push(`error: ${e.message}`));

  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/chat.js`, "utf8") });
  try {
    await page.waitForSelector("#list .msg", { timeout: 15000 });
  } catch (err) {
    const note = await page.$eval("#note", (el) => el.textContent).catch(() => "");
    throw new Error(`${err.message}\n  note: ${note}\n  console: ${said.join(" | ")}`);
  }
}

// buildSettings runs the shipped Settings script over the real server origin.
export async function buildSettings(page, repoRoot) {
  const body = `<main><h1>Settings</h1><p id="path"></p><div id="status"></div><form id="settings">
  <section><h2>Repositories</h2><label>Scan directories<textarea id="scan-dirs"></textarea></label><label>Ignored directories<textarea id="ignored-dirs"></textarea></label></section>
  <section><h2>Instructions</h2><label>Every new agent<textarea id="instructions"></textarea></label><p id="instruction-limit"></p></section>
  <section><h2>Dashboard</h2><label>URL<input id="dashboard-url" /></label><label>Machine name<input id="dashboard-host" /></label><label>Claude account<input id="dashboard-claude" /></label><label>Codex account<input id="dashboard-codex" /></label><div class="secret"><label>Token <span id="dashboard-token-state"></span><input id="dashboard-token" /></label><button type="button" data-secret="dashboard-token">Save token</button><button type="button" data-forget="dashboard-token">Forget</button></div></section>
  <section><h2>Integrations</h2><div class="secret"><label>Linear API key <span id="linear-key-state"></span><input id="linear-key" /></label><button type="button" data-secret="linear-key">Save key</button><button type="button" data-forget="linear-key">Forget</button></div><label>Home Assistant URL<input id="home-assistant-url" /></label><label>Desk light<input id="home-assistant-desk" /></label><label>Notify service<input id="home-assistant-notify" /></label><div class="secret"><label>Home Assistant token <span id="home-assistant-token-state"></span><input id="home-assistant-token" /></label><button type="button" data-secret="home-assistant-token">Save token</button><button type="button" data-forget="home-assistant-token">Forget</button></div><div class="secret"><label>GitHub Packages token <span id="github-packages-token-state"></span><input id="github-packages-token" /></label><button type="button" data-secret="github-packages-token">Save token</button><button type="button" data-forget="github-packages-token">Forget</button></div></section>
  <section><h2>Preview</h2><label>App URL<input id="preview-url" /></label><label class="check"><input id="preview-off" type="checkbox" /> Turn off capture</label></section><section><h2>Prompts</h2><label class="check"><input id="sharpen-off" type="checkbox" /> Do not ask about a vague prompt before it is sent</label></section><button id="save" type="submit">Save settings</button></form></main>`;
  await page.evaluate(
    (vars, markup) => {
      const at = location.origin + location.pathname.replace(/\/+$/, "");
      const tok = new URLSearchParams(location.search).get("t") ?? "";
      for (const [name, value] of Object.entries(vars)) document.documentElement.style.setProperty(`--vscode-${name}`, value);
      document.body.innerHTML = markup;
      // The page asks the editor for every route now, so this stands in for
      // the extension host and makes the same request itself.
      const reply = (data) => window.dispatchEvent(new MessageEvent("message", { data }));
      window.acquireVsCodeApi = () => ({ postMessage(message) {
        if (message?.kind === "ready") reply({ kind: "serving" });
        if (message?.kind !== "api") return;
        fetch(`${at}${message.path}?t=${encodeURIComponent(tok)}`, {
          method: message.method ?? "GET",
          headers: { "X-Ade-Token": tok, "Content-Type": "application/json" },
          body: message.body === undefined ? undefined : JSON.stringify(message.body),
        })
          .then((res) => res.json().then((view) => reply({ kind: "api.result", id: message.id, view, error: res.ok ? undefined : view.error || String(res.status) })))
          .catch((err) => reply({ kind: "api.result", id: message.id, error: String(err) }));
      } });
    },
    THEME,
    body,
  );
  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/settings.css`, "utf8") });
  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/settings.js`, "utf8") });
  await page.waitForFunction(() => document.querySelector("#path")?.textContent !== "", { timeout: 15000 });
}

// buildCommit runs the shipped commit panel over a fixture change set: the
// page draws from one posted message, so nothing has to be served for it.
export async function buildCommit(page, repoRoot) {
  await page.evaluate((vars) => {
    for (const [name, value] of Object.entries(vars))
      document.documentElement.style.setProperty(`--vscode-${name}`, value);
    document.body.innerHTML = '<main id="commit"></main>';
    window.acquireVsCodeApi = () => ({
      postMessage(message) {
        if (message?.kind !== "ready") return;
        window.dispatchEvent(new MessageEvent("message", { data: {
          kind: "state",
          tree: {
            staged: [
              "internal/web/agent.go",
              "internal/web/chat.go",
              "vscode/src/commitview.ts",
            ],
            unstaged: [
              { path: "vscode/src/scm.ts", status: "M" },
              { path: "tools/shots/editor.mjs", status: "M" },
              { path: "vscode/media/new.svg", status: "??" },
            ],
            lines: {
              "internal/web/agent.go": { added: 118, removed: 4 },
              "internal/web/chat.go": { added: 9, removed: 27 },
              "vscode/src/commitview.ts": { added: 6, removed: 6 },
              "vscode/src/scm.ts": { added: 0, removed: 41 },
              "tools/shots/editor.mjs": { added: 3, removed: 0 },
            },
            ahead: 0,
            behind: 3,
            signing: { on: true, format: "ssh", key: "~/.ssh/id_ed25519.pub" },
          },
          behind: 3,
          base: "origin/main",
          branch: "commit-panel",
          draft: {
            title: "feat(commit): stage, describe and commit in one panel",
            body: "The source control view, the message and the verbs after it were three places that each knew part of the commit.\n\nUnchecking a file the message was written for now says so, and offers a redraft.",
            staged: [
              "internal/web/agent.go",
              "internal/web/chat.go",
              "vscode/src/commitview.ts",
            ],
          },
          hasPR: false,
        } }));
      },
    });
  }, THEME);
  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/commit.css`, "utf8") });
  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/commit.js`, "utf8") });
  await page.waitForSelector(".verbs button", { timeout: 15000 });
}

// buildWorktrees runs the shipped worktree panel over a fixture overlap: the
// page draws from one posted message, so nothing has to be served for it.
export async function buildWorktrees(page, repoRoot) {
  const ago = (mins) => new Date(Date.now() - mins * 60 * 1000).toISOString();
  const rev = (sha, subject, patchId = `p${sha}`) => ({
    sha: sha.repeat(8).slice(0, 40),
    subject,
    author: "owen",
    at: ago(600),
    patchId,
  });
  const overlap = {
    base: "1".repeat(40),
    baseRef: "origin/main",
    baseBranch: "main",
    nodes: [
      {
        path: "/code/ade",
        name: "ade",
        branch: "main",
        kind: "session",
        main: true,
        removable: false,
        lastUsed: ago(4),
        commits: [],
        redundant: false,
      },
      {
        path: "/code/ade-fix-remote",
        name: "ade-fix-remote",
        branch: "fix-remote",
        kind: "session",
        removable: false,
        keep: "uncommitted",
        uncommitted: 3,
        behind: 2,
        serving: "http://127.0.0.1:29312",
        lastUsed: ago(1),
        redundant: false,
        commits: [
          rev("a", "fix(remote): dial the bookmarked host, not the tailnet name"),
          rev("b", "test(remote): a LAN host that refuses is not a routing fault"),
        ],
      },
      {
        path: "/code/ade-checks-in-ci",
        name: "ade-checks-in-ci",
        branch: "checks-in-ci",
        kind: "session",
        removable: false,
        keep: "unpushed",
        unpushed: 2,
        lastUsed: ago(90),
        redundant: false,
        commits: [
          rev("c", "ci: run the checks the editor runs"),
          rev("b", "test(remote): a LAN host that refuses is not a routing fault"),
          rev("d", "chore(gotmp): point GOTMPDIR at the cache", "pz"),
        ],
      },
      {
        path: "/code/ade-clean-up-designs",
        name: "ade-clean-up-designs",
        branch: "clean-up-designs",
        kind: "session",
        removable: true,
        lastUsed: ago(1400),
        redundant: false,
        commits: [
          rev("c", "ci: run the checks the editor runs"),
          rev("e", "chore(gotmp): point GOTMPDIR at the cache", "pz"),
        ],
      },
      {
        path: "/code/ade-polish-ui",
        name: "ade-polish-ui",
        branch: "polish-ui",
        kind: "session",
        removable: true,
        lastUsed: ago(240),
        redundant: false,
        commits: [
          rev("c", "ci: run the checks the editor runs"),
          rev("f", "feat(ui): tighten the lane spacing"),
        ],
      },
      {
        path: "/code/ade-stop-in-the-chat",
        name: "ade-stop-in-the-chat",
        branch: "stop-in-the-chat",
        kind: "session",
        removable: true,
        lastUsed: ago(900),
        redundant: false,
        landed: true,
        commits: [
          rev("g", "feat(chat): stop the turn from the chat"),
          rev("h", "test(chat): a stop mid-tool leaves no half-turn"),
        ],
      },
      {
        path: "/code/ade-12-finding-consolidation",
        name: "ade-12-finding-consolidation",
        branch: "12-finding-consolidation",
        kind: "session",
        removable: true,
        merged: true,
        lastUsed: ago(4300),
        redundant: true,
        commits: [],
      },
    ],
    edges: [
      {
        from: "/code/ade-clean-up-designs",
        to: "/code/ade-checks-in-ci",
        kind: "absorbed",
      },
      {
        from: "/code/ade-fix-remote",
        to: "/code/ade-checks-in-ci",
        kind: "shares",
        count: 1,
      },
      {
        from: "/code/ade-clean-up-designs",
        to: "/code/ade-checks-in-ci",
        kind: "shares",
        count: 1,
      },
      {
        from: "/code/ade-polish-ui",
        to: "/code/ade-checks-in-ci",
        kind: "shares",
        count: 1,
      },
      {
        from: "/code/ade-clean-up-designs",
        to: "/code/ade-checks-in-ci",
        kind: "duplicates",
        count: 1,
      },
    ],
    removable: [
      "/code/ade-clean-up-designs",
      "/code/ade-stop-in-the-chat",
      "/code/ade-12-finding-consolidation",
    ],
  };

  await page.evaluate(
    (vars, graph) => {
      for (const [name, value] of Object.entries(vars))
        document.documentElement.style.setProperty(`--vscode-${name}`, value);
      document.body.innerHTML = '<main id="worktrees"></main>';
      window.acquireVsCodeApi = () => ({
        postMessage(message) {
          if (message?.kind !== "ready") return;
          window.dispatchEvent(
            new MessageEvent("message", { data: { kind: "state", overlap: graph } }),
          );
        },
      });
    },
    THEME,
    overlap,
  );
  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/worktrees.css`, "utf8") });
  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/worktrees.js`, "utf8") });
  await page.waitForSelector(".lane-head", { timeout: 15000 });
}

// buildPurge runs the shipped purge panel over a fixture sweep: the page
// draws from one posted message, so nothing has to be served for it.
export async function buildPurge(page, repoRoot) {
  const ago = (mins) => new Date(Date.now() - mins * 60 * 1000).toISOString();
  const sweep = {
    at: new Date().toISOString(),
    repos: [
      {
        path: "/code/ade",
        name: "ade",
        worktrees: [
          {
            path: "/code/ade-12-finding-consolidation",
            name: "ade-12-finding-consolidation",
            branch: "12-finding-consolidation",
            kind: "session",
            removable: true,
            merged: true,
            behind: 214,
            lastUsed: ago(60 * 24 * 130),
          },
          {
            path: "/code/ade-checks-in-ci",
            name: "ade-checks-in-ci",
            branch: "checks-in-ci",
            kind: "session",
            removable: false,
            keep: "unpushed",
            unpushed: 2,
            behind: 41,
            lastUsed: ago(60 * 24 * 26),
          },
          {
            path: "/code/ade-fix-remote",
            name: "ade-fix-remote",
            branch: "fix-remote",
            kind: "session",
            removable: false,
            keep: "serving",
            serving: "http://127.0.0.1:4310/",
            uncommitted: 3,
            behind: 2,
            lastUsed: ago(14),
          },
        ],
      },
      {
        path: "/code/coast",
        name: "coast",
        worktrees: [
          {
            path: "/code/coast-fix-541",
            name: "coast-fix-541",
            branch: "fix-541",
            kind: "session",
            removable: false,
            keep: "uncommitted",
            uncommitted: 7,
            behind: 9,
            lastUsed: ago(60 * 30),
          },
          {
            path: "/code/coast-bump-pricing",
            name: "coast-bump-pricing",
            branch: "bump-pricing",
            kind: "session",
            removable: true,
            behind: 88,
            lastUsed: ago(60 * 24 * 61),
          },
        ],
      },
      {
        path: "/code/cloud-pricing-api",
        name: "cloud-pricing-api",
        why: "fatal: not a git repository",
        worktrees: [],
      },
    ],
  };

  await page.evaluate(
    (vars, state) => {
      for (const [name, value] of Object.entries(vars))
        document.documentElement.style.setProperty(`--vscode-${name}`, value);
      document.body.innerHTML = '<main id="purge"></main>';
      window.acquireVsCodeApi = () => ({
        postMessage(message) {
          if (message?.kind !== "ready") return;
          window.dispatchEvent(
            new MessageEvent("message", { data: { kind: "state", sweep: state } }),
          );
        },
      });
    },
    THEME,
    sweep,
  );
  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/purge.css`, "utf8") });
  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/purge.js`, "utf8") });
  await page.waitForSelector(".rows tbody tr", { timeout: 15000 });
}

// buildQuiz runs the shipped quiz panel over a fixture set of questions: the
// page draws from one posted message, so nothing has to be served for it.
export async function buildQuiz(page, repoRoot) {
  const quiz = {
    files: 23,
    base: "origin/main",
    scope: "branch",
    questions: [
      {
        ref: "Q1",
        file: "internal/web/commit.go",
        line: 88,
        about: "internal/commitmsg/commitmsg.go",
        question:
          "The commit message run now asks twice when /ctb is missing. What did a machine without the skill get before?",
        options: [
          "No message at all, and an empty modal",
          "The agent's refusal, shown as the commit title",
          "The staged file list as the body",
        ],
        answer: "B",
        why: "Parse takes the first conventional-commit line, and a refusal has none, so the modal showed the raw reply.",
      },
      {
        ref: "Q2",
        file: "internal/web/queue.go",
        line: 231,
        question:
          "A new run kind that is queued but missing from this switch does what?",
        options: [
          "It runs with no label on the queue row",
          "It is dropped when the agent goes idle",
          "It waits forever and never runs",
        ],
        answer: "C",
        why: "drain only starts the kinds it has a case for; anything else is taken off the queue and never spawned.",
      },
      {
        ref: "Q3",
        file: "internal/quiz/quiz.go",
        line: 64,
        about: "internal/review/verdict.go",
        question:
          "Parse drops a block whose file is not in the diff. What is lost if that check goes?",
        options: [
          "Nothing: the panel would simply not draw the link",
          "The link opens a file the branch never touched, so the question is not about the change",
          "The shuffle can no longer remap the answer letter",
        ],
        answer: "B",
        why: "file and line are the anchor; a question pointing outside the diff is no longer tied to the change under review.",
      },
      {
        ref: "Q4",
        file: "internal/web/web.go",
        line: 1238,
        question: "Why is the held quiz sent with /api/state rather than fetched on its own?",
        options: [
          "The questions arrive after the run finishes, as the commit draft does",
          "So another machine can read your score",
          "Because the panel has no client of its own",
        ],
        answer: "A",
        why: "The run answers before the quiz exists, so the panel needs the same later-arrival path the commit draft uses.",
      },
    ],
  };

  await page.evaluate(
    (vars, state) => {
      for (const [name, value] of Object.entries(vars))
        document.documentElement.style.setProperty(`--vscode-${name}`, value);
      document.body.innerHTML = '<main id="quiz"></main>';
      window.acquireVsCodeApi = () => ({
        postMessage(message) {
          if (message?.kind !== "ready") return;
          window.dispatchEvent(
            new MessageEvent("message", { data: { kind: "state", quiz: state } }),
          );
        },
      });
    },
    THEME,
    quiz,
  );
  await page.addStyleTag({ content: await readFile(`${repoRoot}/vscode/media/quiz.css`, "utf8") });
  await page.addScriptTag({ content: await readFile(`${repoRoot}/vscode/dist/quiz.js`, "utf8") });
  await page.waitForSelector(".option", { timeout: 15000 });
}
