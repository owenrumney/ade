import { UNDER as ASSETS, type Panel } from "./panel";
import { body as chatBody } from "../pages/chat/body";
import { fresh, names, type Chosen } from "../pages/newsession/view";
import { BODY as SETTINGS_BODY } from "../pages/settings/body";
import { split } from "../pages/commit";
import { byRepo, chosen, confirm, rows, told, type Sweep } from "../pages/purgepanel/view";
import { newSeed, setLastAgent } from "./terminalmode";


export interface Reach {
  get(at: "launcher" | "session", path: string): Promise<unknown>;
  call(
    at: "launcher" | "session",
    path: string,
    method?: string,
    body?: string,
  ): Promise<{ status: number; ok?: boolean; text?: string; error?: string }>;
  send(
    at: "launcher" | "session",
    path: string,
    body: Record<string, unknown>,
    method?: "POST" | "PUT",
  ): Promise<unknown>;
  // push hands a panel a state it did not ask for. A host that only ever
  // answers cannot say "I have started".
  push(id: string, said: unknown): void;
  // A panel is a document with no editor and no shell, so opening a
  // file and following a link are the window's.
  open(path: string): void;
  link(href: string): void;
  // One of ade's own commands, which a page sometimes asks for:
  // the commit panel's Show pull request is the window's verb.
  run(command: string): void;
  // follow moves the window onto a session just started in a checkout.
  follow(dir: string): void;
  // Everything ade puts to a person goes through the omnibox, which a
  // panel cannot reach. Optional: a host without one is not asked.
  ask?(title: string, rows: { label: string; note?: string; value: string }[]): Promise<string | undefined>;
}

export function sandbox(reach: Reach): Panel {
  return {
    id: "sandbox",
    title: "Sandbox mounts and environment",
    root: "sandbox",
    bundle: "sandbox.js",
    styles: ["sandbox.css", "mounts.css"],
    async onMessage(said) {
      switch (said.kind) {
        case "ready": {
          const file = await reach.get("session", "/api/sandbox");
          return { kind: "state", file, note: "" };
        }
        case "save": {
          // The page redraws from state and nothing else, and the PUT
          // hands the new file back, so one round trip does it.
          const got = (await reach.send(
            "session",
            "/api/sandbox",
            said.body as Record<string, unknown>,
            "PUT",
          )) as { error?: string };
          if (!got?.error) return { kind: "state", file: got, note: "" };
          // Refused, so the file is unchanged: re-read rather than
          // leave the edit that did not take on screen.
          return { kind: "state", file: await reach.get("session", "/api/sandbox"), note: got.error };
        }
        case "browse": {
          // The fork opens the editor's folder picker. This window has
          // none yet, and a button that does nothing reads as broken.
          return {
            kind: "state",
            file: await reach.get("session", "/api/sandbox"),
            note: "Type the path: this window has no folder picker yet.",
          };
        }
        default:
          return undefined;
      }
    },
  };
}

// chat is the one panel that fetches ade itself, off `data-base`. It
// works because the webview scheme is the origin the server allows.
export function chat(reach: Reach, at?: { base: string; token: string }): Panel {
  return {
    id: "chat",
    title: "Chat",
    root: "ade",
    bundle: "chat.js",
    styles: ["chat.css"],
    body: at ? chatBody({ base: at.base, mic: `${ASSETS}/media/mic.js` }) : undefined,
    async onMessage(said) {
      if (said.kind === "ready") {
        return at ? { kind: "token", token: at.token } : { kind: "closed" };
      }
      // Every fetch the conversation makes comes through here, in the
      // page's own terms: status, ok, and the body as text.
      if (said.kind === "api" && said.id) {
        const path = String(said.path ?? "");
        if (!path.startsWith("/api/")) {
          return { kind: "api.result", id: said.id, status: 0, error: "not an ade path" };
        }
        const got = await reach.call(
          "session",
          path,
          String(said.method ?? "GET"),
          said.body === undefined ? undefined : String(said.body),
        );
        return { kind: "api.result", id: said.id, ...got };
      }
      if (said.kind === "copy" && typeof said.text === "string") {
        await navigator.clipboard.writeText(said.text).catch(() => undefined);
        return undefined;
      }
      // The page is a document with no editor and no shell, so both
      // of these are the window's.
      if (said.kind === "open" && typeof said.path === "string") {
        reach.open(said.path);
        return undefined;
      }
      if (said.kind === "link" && typeof said.href === "string") {
        reach.link(said.href);
        return undefined;
      }
      return undefined;
    },
  };
}

// settings is the form over ~/.ade/settings.json, asking through the
// `api` relay: the host carries the request and knows nothing of it.
export function settings(reach: Reach): Panel {
  return {
    id: "settings",
    title: "Settings",
    root: "settings-root",
    bundle: "settings.js",
    styles: ["settings.css"],
    body: SETTINGS_BODY,
    async onMessage(said) {
      if (said.kind === "ready") return { kind: "serving" };
      if (said.kind !== "api" || !said.id) return undefined;
      const path = String(said.path ?? "");
      if (!path.startsWith("/api/settings")) {
        return { kind: "api.result", id: said.id, error: "not a settings path" };
      }
      // The page reads `view` or `error`; any other name draws nothing.
      const got = await reach.call(
        "session",
        path,
        String(said.method ?? "GET"),
        said.body === undefined ? undefined : JSON.stringify(said.body),
      );
      const parsed = JSON.parse(got.text || "{}") as { error?: string };
      if (got.error || !got.ok) {
        return {
          kind: "api.result",
          id: said.id,
          error: got.error || parsed.error || `ade answered ${got.status}`,
        };
      }
      return { kind: "api.result", id: said.id, view: parsed };
    },
  };
}

// Most panels share one shape: `ready` gets `{kind:"state", <key>:
// data, …}` and `do` carries an action. So one implementation.
interface Stateful {
  id: string;
  title: string;
  root: string;
  bundle: string;
  styles: string[];
  key: string;
  // of is the field to take out of the answer: the commit page wants
  // `/api/state`'s worktree, and given the whole it draws no files.
  of?: string;
  at: "launcher" | "session";
  path: string;
  // again is the path Refresh reads, where the server serves a cache
  // otherwise: the purge sweep is minutes old until it is asked to rescan.
  again?: string;
  // acts maps the page's buttons to paths. The name is the page's own
  // word, off its `send(...)` calls: guessing it wires nothing.
  acts?: Record<string, Act>;
}

// What one of a page's buttons does. A path, with the body it needs,
// or a routine where one route is not enough.
type Act =
  | { at: "launcher" | "session"; path: string; how?: "PUT" }
  // A routine's answer is the note the page shows.
  | { run: (reach: Reach, said: Record<string, unknown>) => Promise<string | void> };

// The window's act answers an error rather than throwing one, so a route
// that refused has to be read off what it returned.
async function sent(
  reach: Reach,
  at: "launcher" | "session",
  path: string,
  body: Record<string, unknown>,
  how?: "PUT",
): Promise<Record<string, unknown>> {
  const got = ((await reach.send(at, path, body, how)) ?? {}) as Record<string, unknown>;
  if (typeof got.error === "string" && got.error) throw new Error(got.error);
  return got;
}

// part is the answer, or the one field of it the page was written for.
async function part(reach: Reach, spec: Stateful, fresh = false): Promise<unknown> {
  const path = fresh ? (spec.again ?? spec.path) : spec.path;
  const got = (await reach.get(spec.at, path)) as Record<string, unknown>;
  return spec.of ? got?.[spec.of] : got;
}

function stateful(spec: Stateful, reach: Reach): Panel {
  // What the page is showing, so a scan can say it is running without
  // taking the table away while it does.
  let last: unknown;
  return {
    id: spec.id,
    title: spec.title,
    root: spec.root,
    bundle: spec.bundle,
    styles: spec.styles,
    launcher: spec.at === "launcher",
    stirs: true,
    async onMessage(said) {
      if (said.kind === "ready") {
        last = await part(reach, spec);
        return { kind: "state", [spec.key]: last, busy: false, note: "" };
      }
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      const again = async (note = "", fresh = false) => {
        last = await part(reach, spec, fresh);
        return { kind: "state", [spec.key]: last, busy: false, note };
      };
      if (what === "refresh") {
        if (said.quiet) return again();
        reach.push(spec.id, { kind: "state", [spec.key]: last, busy: true, note: "" });
        return again("", true);
      }
      const act = spec.acts?.[what];
      if (!act) return { kind: "state", busy: false, note: `${what} is not wired yet` };
      // A live button through a long removal takes a second press.
      reach.push(spec.id, { kind: "state", [spec.key]: last, busy: true, note: "" });
      try {
        if ("run" in act) return again((await act.run(reach, said as Record<string, unknown>)) || "");
        await sent(reach, act.at, act.path, said as Record<string, unknown>, act.how);
      } catch (err) {
        return { kind: "state", busy: false, note: err instanceof Error ? err.message : String(err) };
      }
      return again();
    },
  };
}

const STATEFUL: Stateful[] = [
  {
    id: "worktrees",
    title: "Worktrees",
    root: "worktrees",
    bundle: "worktrees.js",
    styles: ["worktrees.css"],
    key: "overlap",
    at: "session",
    path: "/api/worktrees/overlap",
    acts: {
      merge: { at: "session", path: "/api/worktrees/merge" },
      // A routine, not a route: unblock each worktree, then prune.
      // Serially, so a failure drops only its own row.
      consolidate: {
        async run(reach, said) {
          const paths = (said.paths ?? []) as string[];
          const going: string[] = [];
          for (const path of paths) {
            const clear = String((said.clear as Record<string, string>)?.[path] ?? "");
            try {
              if (clear === "push") await reach.send("session", "/api/worktrees/push", { path });
              else if (clear === "close")
                await reach.send("session", "/api/worktrees/close", { path });
              going.push(path);
            } catch {
            }
          }
          if (going.length > 0) {
            await reach.send("session", "/api/worktrees/prune", { paths: going });
          }
        },
      },
    },
  },
  {
    id: "repo",
    title: "This repository",
    root: "repo",
    bundle: "repo.js",
    styles: ["repo.css"],
    key: "repo",
    at: "launcher",
    path: "/api/repo",
  },
  {
    id: "purge",
    title: "Purge worktrees",
    root: "purge",
    bundle: "purge.js",
    styles: ["purge.css"],
    key: "sweep",
    at: "launcher",
    path: "/api/worktrees/all",
    again: "/api/worktrees/all?fresh=1",
    // The page calls it remove; the launcher calls it prune, one
    // repository at a time, so a failure in one leaves the rest done.
    acts: {
      remove: {
        async run(reach, said) {
          const force = Boolean(said.force);
          const sweep = (await reach.get("launcher", "/api/worktrees/all")) as Sweep;
          const picked = chosen(rows(sweep, force), new Set((said.paths ?? []) as string[]));
          if (picked.length === 0) return "Nothing to remove: the sweep has moved on.";
          const yes = await reach.ask?.(
            `Remove ${picked.length} worktree${picked.length === 1 ? "" : "s"}?`,
            [
              { label: "Remove", note: confirm(picked).replace(/\n+/g, " "), value: "yes" },
              { label: "Cancel", value: "no" },
            ],
          );
          if (reach.ask && yes !== "yes") return "";
          const removed: string[] = [];
          const kept: { path: string; why: string }[] = [];
          for (const group of byRepo(picked)) {
            try {
              const out = await sent(reach, "launcher", "/api/prune", {
                repo: group.repo,
                paths: group.paths,
                force,
              });
              removed.push(...((out.removed ?? []) as string[]));
              kept.push(...((out.kept ?? []) as { path: string; why: string }[]));
            } catch (err) {
              const why = err instanceof Error ? err.message : String(err);
              for (const path of group.paths) kept.push({ path, why });
            }
          }
          return told(removed, kept);
        },
      },
    },
  },
];

const BY_ID = new Map(STATEFUL.map((s) => [s.id, s]));

function from(id: string): (reach: Reach) => Panel {
  return (reach) => stateful(BY_ID.get(id)!, reach);
}

// picked is what the form chose, in the names `/api/open` and
// `/api/scratch` take. Empty is a default, not a choice, so it is left out.
function picked(want: Chosen, files?: unknown[]): Record<string, unknown> {
  // The agent's own terminal: no model, role or machine, and never a container.
  if (want.terminal) {
    return {
      agent: want.agent || undefined,
      prompt: want.message.trim(),
      sandbox: false,
      terminal: true,
    };
  }
  return {
    agent: want.agent || undefined,
    model: want.model || undefined,
    role: want.role || undefined,
    prompt: want.message.trim(),
    machine: want.machine || undefined,
    sandbox: want.sandbox,
    files: files?.length ? files : undefined,
  };
}

// started is where it went, or why it did not: the launcher answers a
// failure that still opened a session, so both can be true. A plain start
// is followed instead of said.
function started(reach: Reach, got: unknown, dir: string): string {
  const back = (got ?? {}) as { error?: string; path?: string };
  if (back.error) return back.error;
  const at = back.path ?? dir;
  if (!at) return "started";
  reach.follow(at);
  return "";
}

// The design editor: the fork's own CodeMirror, markdown drawn and the
// source revealed under the cursor. A text box cannot stand in for it.

// What one of these edits: where the text comes from and where it goes.
// A design and a pull request description are the same job.
export interface Written {
  id: string;
  title: string;
  read(reach: Reach): Promise<string>;
  write(reach: Reach, text: string): Promise<void>;

  // notes turns on comments pinned to the text. Only the session's own design
  // document has them: the store is keyed per document on that server.
  notes?: boolean;

  // file names the session's document on disk, for a route that acts on one.
  // The server's own idea of which is open may be another document entirely.
  file?: string;
}

export function written(
  reach: Reach,
  at: { base: string; token: string } | undefined,
  doc: Written,
): Panel {
  let last = "";
  // notes reads the store back after every change: one route answers with the
  // list, so the page never has to merge anything itself.
  const notes = async () => {
    if (!doc.notes) return { kind: "notes", notes: [] };
    const got = (await reach.get("session", "/api/design/notes").catch(() => ({}))) as {
      notes?: unknown[];
    };
    return { kind: "notes", notes: got.notes ?? [] };
  };
  return {
    id: doc.id,
    title: doc.title,
    root: "doc",
    bundle: "designedit.js",
    styles: [],
    // Mermaid before the editor: the page hands it the theme's own
    // colours as it starts, so it has to be on the window already.
    before: ["mermaid.min.js"],
    css: "html,body,#doc{height:100%;margin:0}",
    async onMessage(said) {
      if (said.kind === "ready") {
        last = await doc.read(reach);
        return [
          // The window's own type, so the document reads here as it
          // does in the editor.
          { kind: "style", fontFamily: "", fontSize: 13, diagramFontSize: 16 },
          {
            kind: "doc",
            text: last,
            // A directory url, because the page resolves a picture
            // beside the document against it.
            base: at ? `${at.base}/api/docs/` : "",
            token: at?.token ?? "",
          },
          await notes(),
        ];
      }
      if (said.kind === "note") {
        await reach
          .send("session", "/api/design/notes", {
            quote: String(said.quote ?? ""),
            nth: Number(said.nth ?? 0),
            body: String(said.body ?? ""),
          })
          .catch(() => undefined);
        return notes();
      }
      if (said.kind === "notereply") {
        await reach
          .send("session", `/api/design/notes/${said.id}/reply`, { body: String(said.body ?? "") })
          .catch(() => undefined);
        return notes();
      }
      if (said.kind === "noteresolve") {
        await reach
          .send("session", `/api/design/notes/${said.id}/resolve`, { resolved: !!said.resolved })
          .catch(() => undefined);
        return notes();
      }
      if (said.kind === "accept") {
        await reach
          .send("session", "/api/design/accept", { rows: said.rows, file: doc.file })
          .catch(() => undefined);
        return undefined;
      }
      if (said.kind === "notedelete") {
        await reach.call("session", `/api/design/notes/${said.id}`, "DELETE").catch(() => undefined);
        return notes();
      }
      if (said.kind !== "edit" || typeof said.text !== "string") return undefined;
      if (said.text === last) return undefined;
      last = said.text;
      await doc.write(reach, said.text);
      return undefined;
    },
  };
}

// quiz is the questions about this change. Its own panel: the generic
// shape reads with a GET and this one posts, then waits for a run.
export function quiz(reach: Reach): Panel {
  const ask = async (regenerate: boolean) => {
    const got = (await reach
      .send("session", "/api/quiz", { regenerate })
      .catch((e: Error) => ({ error: e.message }))) as {
      quiz?: unknown;
      error?: string;
    };
    if (got.error) return { kind: "state", quiz: undefined, busy: false, note: got.error };
    if (got.quiz) return { kind: "state", quiz: got.quiz, busy: false, note: "" };
    // Queued: the host waits rather than the page, because the page has
    // no way to ask again and the window does not push to it.
    const waited = await waitFor(reach, "quiz", "quiz", QUIZ_WAIT);
    if (waited.found) return { kind: "state", quiz: waited.found, busy: false, note: "" };
    return {
      kind: "state",
      quiz: undefined,
      busy: false,
      note: waited.error || "the questions are still being written",
    };
  };
  return {
    id: "quiz",
    title: "Quiz me",
    root: "quiz",
    bundle: "quiz.js",
    styles: ["quiz.css"],
    async onMessage(said) {
      if (said.kind === "ready") return ask(false);
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      if (what === "regenerate") return ask(true);
      if (what === "cancel") {
        await reach.send("session", "/api/cancel", {}).catch(() => undefined);
        return { kind: "state", quiz: undefined, busy: false, note: "stopped" };
      }
      if (what === "open" && typeof said.file === "string") reach.open(said.file);
      return undefined;
    },
  };
}

// walkthrough is the change read back as ordered steps. Posts and waits like
// the quiz, and wears mermaid for the flow the steps walk.
export function explain(reach: Reach): Panel {
  const ask = async (regenerate: boolean) => {
    const got = (await reach
      .send("session", "/api/walkthrough", { regenerate })
      .catch((e: Error) => ({ error: e.message }))) as {
      walkthrough?: unknown;
      error?: string;
    };
    if (got.error) return { kind: "state", walkthrough: undefined, busy: false, note: got.error };
    if (got.walkthrough)
      return { kind: "state", walkthrough: got.walkthrough, busy: false, note: "" };
    // The page draws only what it is told, so it is told it is waiting
    // before the wait rather than after it.
    reach.push("explain", { kind: "state", walkthrough: undefined, busy: true, note: "" });
    const waited = await waitFor(reach, "walkthrough", "walkthrough", WALK_WAIT);
    if (waited.found) return { kind: "state", walkthrough: waited.found, busy: false, note: "" };
    return {
      kind: "state",
      walkthrough: undefined,
      busy: false,
      note: waited.error || "the steps are still being written",
    };
  };
  return {
    id: "explain",
    title: "Explain this change",
    root: "explain",
    bundle: "explain.js",
    styles: ["explain.css"],
    // Mermaid before the page: it hands it the theme's own colours as it
    // draws, so it has to be on the window already.
    before: ["mermaid.min.js"],
    async onMessage(said) {
      if (said.kind === "ready") return ask(false);
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      if (what === "regenerate") return ask(true);
      if (what === "cancel") {
        await stopWalkthrough(reach);
        return { kind: "state", walkthrough: undefined, busy: false, note: "stopped" };
      }
      if (what === "open" && typeof said.file === "string") reach.open(said.file);
      return undefined;
    },
  };
}

// How long the host waits for a queued run, and how often it looks. The
// walkthrough gets longer: it runs on the session's model, not the fast one.
const QUIZ_WAIT = 90_000;
const WALK_WAIT = 300_000;
const QUIZ_EVERY = 2_000;

// stopWalkthrough stops the run when the walkthrough is the one running, and
// otherwise drops it from the queue: /api/cancel would kill the turn in flight.
async function stopWalkthrough(reach: Reach): Promise<void> {
  const state = (await reach.get("session", "/api/state").catch(() => ({}))) as {
    agent?: { running?: boolean; kind?: string; waiting?: { id: string; kind: string }[] };
  };
  const agent = state.agent;
  if (agent?.running && agent.kind === "walkthrough") {
    await reach.send("session", "/api/cancel", {}).catch(() => undefined);
    return;
  }
  for (const each of agent?.waiting ?? []) {
    if (each.kind !== "Explain this change") continue;
    await reach.call("session", `/api/queue/${each.id}`, "DELETE").catch(() => undefined);
  }
}

// waitFor polls for the key until it lands. A run of this kind that ends in
// an error ends the wait: nothing will set the key now.
async function waitFor(
  reach: Reach,
  key: "quiz" | "walkthrough",
  kind: string,
  budget: number,
): Promise<{ found?: unknown; error?: string }> {
  const until = Date.now() + budget;
  while (Date.now() < until) {
    await new Promise((wake) => setTimeout(wake, QUIZ_EVERY));
    const state = (await reach.get("session", "/api/state").catch(() => ({}))) as Record<
      string,
      unknown
    > & { agent?: { running?: boolean; kind?: string; error?: string } };
    if (state[key]) return { found: state[key] };
    const agent = state.agent;
    if (agent && !agent.running && agent.kind === kind && agent.error) return { error: agent.error };
  }
  return {};
}

interface Head {
  title: string;
  body: string;
}

// commit is the working tree, the draft and the verbs around them. Its
// `busy` names the verb in flight, so a button can disable itself.
export function commit(reach: Reach): Panel {
  let busy = "";
  // Set for the one state that follows a commit. The page keeps what you
  // typed across every other read, and a message that went must not stay.
  let committed = false;
  // The last state handed over, so starting work can say so without a round
  // trip. The page redraws from each one, so a partial state blanks it.
  let last: Record<string, unknown> | undefined;
  let head: Head | undefined;

  const hand = async (note = "") => {
    const state = (await reach.get("session", "/api/state").catch(() => ({}))) as {
      worktree?: { ahead?: number; behind?: number };
      draft?: unknown;
      branchPr?: unknown;
      created?: { number?: string; url?: string };
      agent?: { running?: boolean; error?: string };
      meta?: { base?: string; branch?: string };
    };
    // Before the state is built, not after as the editor does: the
    // editor gets another push shortly and this host does not.
    if (busy === "redraft" && state.draft) busy = "";
    // The POST only starts the run: an agent writes the pull request. A
    // state that does not say leaves it busy; a failed read must not free it.
    if (busy === "createPR" && state.agent && !state.agent.running) {
      busy = "";
      // The run is over and opened nothing, so say why: the button comes
      // back below and pressing it again is the thing to do.
      if (!state.created && state.agent.error) note = note || state.agent.error;
    }
    last = {
      kind: "state",
      tree: state.worktree,
      draft: state.draft,
      // The PR the server just opened, named here long before the poll finds
      // it on the forge. Without it the button comes back and opens a second.
      hasPR: Boolean(state.branchPr) || Boolean(state.created),
      ahead: state.worktree?.ahead ?? 0,
      behind: state.worktree?.behind ?? 0,
      base: state.meta?.base ?? "",
      branch: state.meta?.branch ?? "",
      busy,
      committed,
      head,
      note,
    };
    committed = false;
    return last;
  };

  // sent turns a refusal into a throw. act answers with {error} rather than
  // throwing, so without this a server that said why arrives as a dead button.
  const sent = async (p: Promise<unknown>): Promise<unknown> => {
    const got = (await p) as { error?: string } | undefined;
    if (got?.error) throw new Error(got.error);
    return got;
  };

  const doing = async (what: string, run: () => Promise<unknown>) => {
    busy = what;
    // Said before the work, not after it: the answer below lands when it is
    // already over, by which time the button has been pressed four times.
    if (last) reach.push("commit", { ...last, busy, note: "" });
    try {
      await run();
      if (what !== "redraft" && what !== "createPR") busy = "";
      return await hand();
    } catch (err) {
      busy = "";
      return await hand(err instanceof Error ? err.message : String(err));
    }
  };

  return {
    id: "commit",
    title: "Commit",
    root: "commit",
    bundle: "commit.js",
    styles: ["commit.css", "menu.css"],
    // The drafting agent finishes elsewhere, and the page is how you
    // learn it did: without a push the box stays disabled.
    stirs: true,
    async onMessage(said) {
      if (said.kind === "ready") return hand();
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      const body = said as Record<string, unknown>;
      if (busy && what !== "refresh") return hand();
      switch (what) {
        case "refresh":
          return hand();
        // Staging is how anything becomes committable: without these the
        // Commit button is disabled for ever and says nothing about why.
        case "stage":
          return doing("stage", () => sent(reach.send("session", "/api/stage", body)));
        case "unstage":
          return doing("unstage", () => sent(reach.send("session", "/api/unstage", body)));
        case "redraft":
          return doing("redraft", () => sent(reach.send("session", "/api/commit-message", body)));
        case "branch":
          return doing("branch", () => sent(reach.send("session", "/api/branch", body)));
        case "rebase":
          return doing("rebase", () => sent(reach.send("session", "/api/base/rebase", body)));
        case "push":
          return doing("push", () =>
            sent(reach.send("session", "/api/push", { force: Boolean(body.force) })),
          );
        case "createPR":
          return doing("createPR", () => sent(reach.send("session", "/api/pr-create", body)));
        // The whole message of the commit an amend would fold into.
        case "headMessage":
          return doing("headMessage", async () => {
            head = (await sent(reach.get("session", "/api/commit/head"))) as Head;
          });
        case "commit":
          return doing("commit", async () => {
            const message = split(String(body.message ?? ""));
            const amend = Boolean(body.amend);
            await sent(reach.send("session", "/api/commit", { ...message, amend }));
            // An amend rewrites a commit the remote may already have, so the
            // only push that lands after one is a lease.
            if (body.push) await sent(reach.send("session", "/api/push", { force: amend }));
            head = undefined;
            committed = true;
          });
        // A file row's menu: the window opens it, the page cannot.
        case "open":
          reach.open(String(body.path ?? ""));
          return undefined;
        case "showPR":
          reach.run("ade.showPR");
          return undefined;
        case "quizMe":
          reach.run("ade.quizMe");
          return undefined;
        case "explainThis":
          reach.run("ade.explainThis");
          return undefined;
        default:
          return { kind: "state", busy, note: `${what} is not wired yet` };
      }
    },
  };
}

// audit is what the person did, as the trail kept it. The window's own
// record, so it reads the shell rather than ade.
export function audit(reach: Reach): Panel {
  let day = "";
  const state = async () => {
    const got = (await window.ade?.audit(day)) ?? {};
    day = day || (got.days ?? [])[0] || "";
    return { kind: "state", entries: got.entries ?? [], days: got.days ?? [], day };
  };
  return {
    id: "audit",
    title: "What I did",
    root: "audit",
    bundle: "audit.js",
    styles: ["audit.css"],
    launcher: true,
    async onMessage(said) {
      if (said.kind === "ready") return state();
      if (said.kind !== "do") return undefined;
      if (said.what === "day") day = String(said.day ?? "");
      return state();
    },
  };
}

// machines is every machine ade is bookmarked onto, with the one verb each
// row has. The launcher holds the connections, so this never asks a session.
export function machines(reach: Reach): Panel {
  let note = "";
  const state = async () => {
    const got = (await reach.get("launcher", "/api/hosts").catch((e: Error) => ({
      error: e.message,
    }))) as { hosts?: unknown[]; error?: string };
    return { kind: "state", hosts: got.hosts ?? [], note: got.error ?? note };
  };
  return {
    id: "machines",
    title: "Machines",
    root: "machines",
    bundle: "machines.js",
    styles: ["machines.css"],
    launcher: true,
    // The launcher broadcasts `hosts` whenever the list moves, and connecting
    // takes seconds, so the row that started it is told by the event.
    stirs: true,
    async onMessage(said) {
      if (said.kind === "ready") return state();
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      if (what !== "connect" && what !== "disconnect") return state();
      note = "";
      const path = what === "connect" ? "/api/connect" : "/api/disconnect";
      await reach
        .send("launcher", path, { host: String(said.host ?? "") })
        .catch(() => undefined);
      // Nothing is kept from the refusal: the launcher records it against
      // the machine, so the next read puts it on that row. Keeping it here
      // too printed the same sentence twice, once without a machine's name.
      return state();
    },
  };
}

// SHIP_ACTS is the route behind each step's verb. Every one of them already
// existed; Ship is the page that puts them in order.
const SHIP_ACTS: Record<string, { path: string; body?: Record<string, unknown> }> = {
  message: { path: "/api/commit-message" },
  rebase: { path: "/api/base/rebase" },
  push: { path: "/api/push" },
  raise: { path: "/api/pr-create" },
  fix: { path: "/api/checks/fix" },
  merge: { path: "/api/pr/merge", body: { method: "rebase" } },
  tests: { path: "/api/verify/run" },
};

// ship is where a change stands, from uncommitted work to a merged pull
// request, one column per repository the session touches.
export function ship(reach: Reach): Panel {
  let refused: { do?: string; root?: string; reason?: string } = {};
  let note = "";
  const read = async () =>
    (await reach.get("session", "/api/ship").catch((e: Error) => ({ error: e.message }))) as {
      home?: string;
      view?: string;
      draft?: { title?: string; body?: string };
      error?: string;
    };
  // quiet says nobody pressed, so a stir arriving mid-push does not re-enable
  // the buttons the page has disabled.
  const state = async (quiet = false) => {
    const got = await read();
    return { kind: "state", ...got, refused, note: got.error ?? note, quiet };
  };
  // The steps act on whatever the Diff tab is pointed at, so a card for
  // another repository points it there first; "" is home.
  const point = async (root: string, home: string, view: string): Promise<string> => {
    const want = root === home ? "" : root;
    if (want === view) return "";
    const got = (await reach
      .send("session", "/api/diff/root", { root: want })
      .catch((e: Error) => ({ error: e.message }))) as { error?: string };
    return got?.error ?? "";
  };
  return {
    id: "ship",
    title: "Ship",
    root: "ship",
    bundle: "ship.js",
    styles: ["ship.css"],
    stirs: true,
    async onMessage(said) {
      if (said.kind === "ready") return state();
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      if (what === "refresh") return state(Boolean(said.quiet));

      refused = {};
      note = "";
      if (what === "threads" || what === "design") {
        reach.run(what === "threads" ? "ade.showComments" : "ade.design");
        return state();
      }

      const root = String(said.root ?? "");
      const anyway = Boolean(said.anyway);
      const current = await read();
      // A gate button carries no root, and the test run only answers for the
      // home checkout, so it takes the diff back there first.
      const to = root || (what === "tests" ? (current.home ?? "") : "");
      if (to) {
        const why = await point(to, current.home ?? "", current.view ?? "");
        if (why) {
          note = why;
          return state();
        }
      }

      if (what === "commit") {
        const draft = current.draft;
        if (!draft?.title) {
          note = "no commit message has been written yet";
          return state();
        }
        const got = (await reach
          .send("session", "/api/commit", { title: draft.title, body: draft.body ?? "", anyway })
          .catch((e: Error) => ({ error: e.message }))) as { error?: string };
        if (got?.error) refused = { do: "commit", root, reason: got.error };
        return state();
      }

      const act = SHIP_ACTS[what];
      if (!act) return state();
      const got = (await reach
        .send("session", act.path, { ...(act.body ?? {}), anyway })
        .catch((e: Error) => ({ error: e.message }))) as { error?: string };
      if (got?.error) refused = { do: what, root, reason: got.error };
      return state();
    },
  };
}

// prune is the stale-design sweep. Its own panel: the list is two
// reads, the repositories and then a sweep for each.
export function prune(reach: Reach): Panel {
  return {
    id: "prune",
    title: "Prune designs",
    root: "prune",
    bundle: "designprune.js",
    styles: ["designprune.css"],
    launcher: true,
    async onMessage(said) {
      const what = said.kind === "do" ? String(said.what ?? "") : "";
      if (said.kind !== "ready" && what !== "refresh") {
        if (what === "open" && typeof said.page === "string") reach.open(said.page);
        return undefined;
      }
      const held = (await reach.get("launcher", "/api/designs/repos").catch((e: Error) => ({
        error: e.message,
      }))) as { repos?: { repo: string }[]; wiki?: unknown; error?: string };
      if (held.error) {
        return { kind: "state", groups: [], busy: false, note: held.error };
      }
      // Each sweep is its own call, so one slow repository does not
      // hold the others: the page draws a row as each lands.
      const groups = [];
      for (const each of held.repos ?? []) {
        const answer = await reach
          .get("launcher", `/api/designs/prunable?repo=${encodeURIComponent(each.repo)}`)
          .catch((e: Error) => ({ repo: each.repo, error: e.message }));
        groups.push({ repo: each.repo, pending: false, answer });
      }
      // `running` is a prune in flight, which this host does not start
      // in the background: the sweep is awaited before the page is told.
      return {
        kind: "state",
        groups,
        wiki: held.wiki,
        states: {},
        running: false,
        busy: false,
        note: "",
      };
    },
  };
}

// designsIn is home's design documents, for the Design row. A read that
// failed is said rather than counted as none: "no documents yet" is a lie.
async function designsIn(
  reach: Reach,
  chosen: Chosen,
): Promise<{ designs: { file: string; ship: boolean }[]; designNote?: string }> {
  const dir = chosen.repos?.[0];
  if (!dir || chosen.machine) return { designs: [] };
  try {
    const got = (await reach.get("launcher", `/api/designs?dir=${encodeURIComponent(dir)}`)) as {
      designs?: string[] | null;
      error?: string;
    };
    if (got.error) return { designs: [], designNote: `Could not list them: ${got.error}` };
    return { designs: (got.designs ?? []).map((file) => ({ file, ship: false })) };
  } catch (err) {
    return { designs: [], designNote: `Could not list them: ${String(err)}` };
  }
}

// Checkout is one row of the Open branch pick: a worktree carries its path,
// a branch without one carries none and is checked out when it is opened.
interface Checkout {
  name: string;
  at: string;
  note: string;
}

// checkoutsIn is home's worktrees and the branches that have none, newest
// first within each. Only for the row that asks: it costs a git call.
async function checkoutsIn(
  reach: Reach,
  chosen: Chosen,
): Promise<{ checkouts?: Checkout[]; checkoutNote?: string }> {
  const dir = chosen.repos?.[0];
  if (!dir || chosen.mode !== "branch") return {};
  try {
    const got = (await reach.get("launcher", `/api/branches?repo=${encodeURIComponent(dir)}`)) as {
      worktrees?: { path: string; name: string; branch?: string; main?: boolean }[] | null;
      branches?: { name: string; at?: string }[] | null;
      error?: string;
    };
    if (got.error) return { checkoutNote: `Could not list them: ${got.error}` };
    const held = (got.worktrees ?? [])
      // A detached worktree has no branch to open it by.
      .filter((one) => one.branch)
      .map((one) => ({
        name: one.branch ?? "",
        at: one.path,
        note: one.main ? "main checkout" : one.name,
      }));
    const loose = (got.branches ?? []).map((one) => ({ name: one.name, at: "", note: one.at ?? "" }));
    return { checkouts: [...held, ...loose] };
  } catch (err) {
    return { checkoutNote: `Could not list them: ${String(err)}` };
  }
}

interface Host {
  name: string;
  label?: string;
  connected?: boolean;
  local?: boolean;
}

// elsewhere is the machines a session can be started on besides this one. The
// page offers this machine itself, and an unconnected one has nothing to ask.
export function elsewhere(hosts: Host[] | undefined): { name: string; label: string }[] {
  return (hosts ?? [])
    .filter((one) => one.connected && !one.local && one.name)
    .map((one) => ({ name: one.name, label: one.label || one.name }));
}

export function newSession(reach: Reach, seed: Partial<Chosen> = {}): Panel {
  // The page's own default, plus what the thing you started from
  // knows: a todo's text is the first message.
  let chosen: Record<string, unknown> = { ...fresh, mode: "direct", ...seed };
  const offer = async () => {
    const [repos, roles, models, hosts] = await Promise.all([
      reach.get("launcher", "/api/repos").catch(() => ({ repos: [] })),
      reach.get("launcher", "/api/roles").catch(() => ({ roles: [] })),
      reach.get("launcher", "/api/models").catch(() => ({ models: [] })),
      reach.get("launcher", "/api/hosts").catch(() => ({ hosts: [] })),
    ]);
    const list = (repos as { repos?: { name: string; path: string }[] }).repos ?? [];
    const [listed, held] = await Promise.all([
      designsIn(reach, chosen as unknown as Chosen),
      checkoutsIn(reach, chosen as unknown as Chosen),
    ]);
    return {
      machines: elsewhere((hosts as { hosts?: Host[] }).hosts),
      repos: list,
      agents: [
        { id: "claude", label: "claude" },
        { id: "codex", label: "codex" },
      ],
      models: ((models as { models?: { id: string; label: string }[] }).models ?? []).map(
        (m) => ({ id: m.id, label: m.label }),
      ),
      roles: ((roles as { roles?: { name: string }[] }).roles ?? []).map((r) => r.name),
      ...listed,
      ...held,
      recent: [],
    };
  };
  return {
    id: "newsession",
    title: "New session",
    root: "newsession",
    bundle: "newsession.js",
    styles: ["newsession.css", "mounts.css"],
    launcher: true,
    async onMessage(said) {
      if (said.kind === "ready") {
        return { kind: "state", chosen, offer: await offer(), busy: false, note: "" };
      }
      if (said.kind === "change") {
        const was = chosen as unknown as Chosen;
        const want = (said.chosen ?? chosen) as unknown as Chosen;
        chosen = want as unknown as Record<string, unknown>;
        // Only a new home or a new row has a new list to offer. Answering
        // every keystroke redraws the page and takes the cursor out of the box.
        if (want.repos[0] === was.repos[0] && want.mode === was.mode) return undefined;
        return { kind: "state", chosen, offer: await offer(), busy: false, note: "" };
      }
      if (said.kind === "start") {
        chosen = (said.chosen ?? chosen) as Record<string, unknown>;
        const want = chosen as unknown as Chosen;
        const files = (said.files ?? []) as unknown[];
        if (want.terminal) setLastAgent(want.agent);
        // Scratch is the one with nowhere to live yet, so it has no
        // repository to tick and its own route.
        if (want.mode === "scratch") {
          reach.push("newsession", {
            kind: "state",
            chosen,
            busy: true,
            note: `Making a checkout for ${want.name.trim() || "it"}…`,
          });
          const got = await reach.send("launcher", "/api/scratch", {
            name: want.name.trim(),
            ...picked(want, files),
          });
          return { kind: "state", chosen, busy: false, note: started(reach, got, "") };
        }
        const [home, ...also] = want.repos;
        if (!home) return { kind: "state", chosen, busy: false, note: "tick a repository" };
        // Open branch names a branch the repository already has, so nothing is
        // scaffolded: find its checkout, make one only when it has none, and
        // open that as the plain open does.
        if (want.mode === "branch") {
          reach.push("newsession", {
            kind: "state",
            chosen,
            busy: true,
            note: want.at ? `Opening ${want.branch}…` : `Checking ${want.branch} out…`,
          });
          let at = want.at;
          if (!at) {
            const made = (await reach.send("launcher", "/api/branches/worktree", {
              repo: home,
              branch: want.branch,
            })) as { path?: string; error?: string };
            if (made.error || !made.path) {
              return { kind: "state", chosen, busy: false, note: made.error ?? "no checkout was made" };
            }
            at = made.path;
          }
          const opened = await reach.send("launcher", "/api/open", {
            dir: at,
            fresh: true,
            direct: !want.machine,
            ...picked(want, files),
          });
          return { kind: "state", chosen, busy: false, note: started(reach, opened, at) };
        }
        // The ticket instead of a name on that row: the document is named
        // after the ticket, and only the server can read a GitHub issue.
        const ticket = want.mode === "ticket" ? want.ticket.trim() : "";
        // `ade create` is seconds, and said nothing for all of them.
        reach.push("newsession", {
          kind: "state",
          chosen,
          busy: true,
          note: `Starting ${ticket || names(want) || home.split("/").pop()}…`,
        });
        // Every default the row showed: one on screen that Start does
        // not send is accepted without being made.
        // names(), not the name field: on the Design row it is the document,
        // and a session started without it is a plain one with no brief.
        const got = await reach.send("launcher", "/api/open", {
          dir: home,
          name: ticket ? undefined : names(want) || undefined,
          ticket: ticket || undefined,
          also: also.length > 0 ? also : undefined,
          fresh: true,
          direct: !want.machine,
          ...picked(want, files),
        });
        return { kind: "state", chosen, busy: false, note: started(reach, got, home) };
      }
      return undefined;
    },
  };
}

// WHICH names the panel each command opens, so `doing.ts` can point at
// one without knowing how it is built.
export const WHICH: Record<string, (reach: Reach, at?: { base: string; token: string }) => Panel> = {
  "ade.sandboxFile": sandbox,
  "ade.sandboxPanel": sandbox,
  "ade.openChat": chat,
  "ade.focusChat": chat,
  "ade.concierge": chat,
  "ade.openSettings": settings,
  "ade.worktrees": from("worktrees"),
  "ade.worktreeGraph": from("worktrees"),
  "ade.showRepo": from("repo"),
  "ade.audit": audit,
  "ade.machines": machines,
  "ade.commitPanel": commit,
  "ade.ship": ship,
  "ade.purgeWorktrees": from("purge"),
  "ade.disk": from("purge"),
  "ade.purgeDesigns": prune,
  "ade.newSession": (reach) => newSession(reach, newSeed()),
  "ade.quizMe": quiz,
  "ade.explainThis": explain,
};
