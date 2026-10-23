import { UNDER as ASSETS, type Panel } from "./panel";
import { body as chatBody } from "../../vscode/src/chat/body";
import { fresh, names, type Chosen } from "../../vscode/src/newsession/view";
import { BODY as SETTINGS_BODY } from "../../vscode/src/settings/body";


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
      // The page reads `view` or `error`, as the extension's relay
      // answers. Any other name draws nothing and says nothing.
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
  // acts maps the page's buttons to paths. The name is the page's own
  // word, off its `send(...)` calls: guessing it wires nothing.
  acts?: Record<string, Act>;
}

// What one of a page's buttons does. A path, with the body it needs,
// or a routine where one route is not enough.
type Act =
  | { at: "launcher" | "session"; path: string; how?: "PUT" }
  | { run: (reach: Reach, said: Record<string, unknown>) => Promise<void> };

// part is the answer, or the one field of it the page was written for.
async function part(reach: Reach, spec: Stateful): Promise<unknown> {
  const got = (await reach.get(spec.at, spec.path)) as Record<string, unknown>;
  return spec.of ? got?.[spec.of] : got;
}

function stateful(spec: Stateful, reach: Reach): Panel {
  return {
    id: spec.id,
    title: spec.title,
    root: spec.root,
    bundle: spec.bundle,
    styles: spec.styles,
    stirs: true,
    async onMessage(said) {
      if (said.kind === "ready") {
        return { kind: "state", [spec.key]: await part(reach, spec), busy: false, note: "" };
      }
      if (said.kind !== "do") return undefined;
      const what = String(said.what ?? "");
      const again = async (note = "") => ({
        kind: "state",
        [spec.key]: await part(reach, spec),
        busy: false,
        note,
      });
      if (what === "refresh") return again();
      const act = spec.acts?.[what];
      if (!act) return { kind: "state", busy: false, note: `${what} is not wired yet` };
      try {
        if ("run" in act) await act.run(reach, said as Record<string, unknown>);
        else await reach.send(act.at, act.path, said as Record<string, unknown>, act.how);
      } catch (err) {
        return { kind: "state", busy: false, note: String(err) };
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
    // The page calls it remove; the launcher calls it prune.
    acts: { remove: { at: "launcher", path: "/api/prune" } },
  },
];

const BY_ID = new Map(STATEFUL.map((s) => [s.id, s]));

function from(id: string): (reach: Reach) => Panel {
  return (reach) => stateful(BY_ID.get(id)!, reach);
}

// picked is what the form chose, in the names `/api/open` and
// `/api/scratch` take. Empty is a default, not a choice, so it is left out.
function picked(want: Chosen): Record<string, unknown> {
  return {
    agent: want.agent || undefined,
    model: want.model || undefined,
    role: want.role || undefined,
    prompt: want.message.trim(),
    machine: want.machine || undefined,
    sandbox: want.sandbox,
  };
}

// started is where it went, or why it did not: the launcher answers a
// failure that still opened a session, so both can be true.
function started(got: unknown): string {
  const back = (got ?? {}) as { error?: string; path?: string };
  if (back.error) return back.error;
  return back.path ? `started in ${back.path}` : "started";
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
}

export function written(
  reach: Reach,
  at: { base: string; token: string } | undefined,
  doc: Written,
): Panel {
  let last = "";
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
          },
        ];
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
    const found = await waitForQuiz(reach);
    return found
      ? { kind: "state", quiz: found, busy: false, note: "" }
      : { kind: "state", quiz: undefined, busy: false, note: "the questions are still being written" };
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

// How long the host waits for a queued quiz, and how often it looks.
const QUIZ_WAIT = 90_000;
const QUIZ_EVERY = 2_000;

async function waitForQuiz(reach: Reach): Promise<unknown> {
  const until = Date.now() + QUIZ_WAIT;
  while (Date.now() < until) {
    await new Promise((wake) => setTimeout(wake, QUIZ_EVERY));
    const state = (await reach
      .get("session", "/api/state")
      .catch(() => ({}))) as { quiz?: unknown };
    if (state.quiz) return state.quiz;
  }
  return undefined;
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

  const hand = async (note = "") => {
    const state = (await reach.get("session", "/api/state").catch(() => ({}))) as {
      worktree?: { ahead?: number; behind?: number };
      draft?: unknown;
      branchPr?: unknown;
      meta?: { base?: string; branch?: string };
    };
    // Before the state is built, not after as the editor does: the
    // editor gets another push shortly and this host does not.
    if (busy === "redraft" && state.draft) busy = "";
    last = {
      kind: "state",
      tree: state.worktree,
      draft: state.draft,
      hasPR: Boolean(state.branchPr),
      ahead: state.worktree?.ahead ?? 0,
      behind: state.worktree?.behind ?? 0,
      base: state.meta?.base ?? "",
      branch: state.meta?.branch ?? "",
      busy,
      committed,
      note,
    };
    committed = false;
    return last;
  };

  const doing = async (what: string, run: () => Promise<unknown>) => {
    busy = what;
    // Said before the work, not after it: the answer below lands when it is
    // already over, by which time the button has been pressed four times.
    if (last) reach.push("commit", { ...last, busy, note: "" });
    try {
      await run();
      if (what !== "redraft") busy = "";
      return await hand();
    } catch (err) {
      busy = "";
      return await hand(String(err));
    }
  };

  return {
    id: "commit",
    title: "Commit",
    root: "commit",
    bundle: "commit.js",
    styles: ["commit.css"],
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
          return doing("stage", () => reach.send("session", "/api/stage", body));
        case "unstage":
          return doing("unstage", () => reach.send("session", "/api/unstage", body));
        case "redraft":
          return doing("redraft", () => reach.send("session", "/api/commit-message", body));
        case "branch":
          return doing("branch", () => reach.send("session", "/api/branch", body));
        case "rebase":
          return doing("rebase", () => reach.send("session", "/api/base/rebase", body));
        case "createPR":
          return doing("createPR", () => reach.send("session", "/api/pr-create", body));
        case "commit":
          return doing("commit", async () => {
            await reach.send("session", "/api/commit", body);
            if (body.push) await reach.send("session", "/api/push", { force: false });
            committed = true;
          });
        case "showPR":
          reach.run("ade.showPR");
          return undefined;
        case "quizMe":
          reach.run("ade.quizMe");
          return undefined;
        default:
          return { kind: "state", busy, note: `${what} is not wired yet` };
      }
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

export function newSession(reach: Reach, seed: Partial<Chosen> = {}): Panel {
  // The extension's own default, plus what the thing you started from
  // knows: a todo's text is the first message.
  let chosen: Record<string, unknown> = { ...fresh, mode: "direct", ...seed };
  const offer = async () => {
    const [repos, roles, models] = await Promise.all([
      reach.get("launcher", "/api/repos").catch(() => ({ repos: [] })),
      reach.get("launcher", "/api/roles").catch(() => ({ roles: [] })),
      reach.get("launcher", "/api/models").catch(() => ({ models: [] })),
    ]);
    const list = (repos as { repos?: { name: string; path: string }[] }).repos ?? [];
    const listed = await designsIn(reach, chosen as unknown as Chosen);
    return {
      machines: [],
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
      recent: [],
    };
  };
  return {
    id: "newsession",
    title: "New session",
    root: "newsession",
    bundle: "newsession.js",
    styles: ["newsession.css", "mounts.css"],
    async onMessage(said) {
      if (said.kind === "ready") {
        return { kind: "state", chosen, offer: await offer(), busy: false, note: "" };
      }
      if (said.kind === "change") {
        chosen = (said.chosen ?? chosen) as Record<string, unknown>;
        return { kind: "state", chosen, offer: await offer(), busy: false, note: "" };
      }
      if (said.kind === "start") {
        chosen = (said.chosen ?? chosen) as Record<string, unknown>;
        const want = chosen as unknown as Chosen;
        // Scratch is the one with nowhere to live yet, so it has no
        // repository to tick and its own route.
        if (want.mode === "scratch") {
          const got = await reach.send("launcher", "/api/scratch", {
            name: want.name.trim(),
            ...picked(want),
          });
          return { kind: "state", chosen, busy: false, note: started(got) };
        }
        const [home, ...also] = want.repos;
        if (!home) return { kind: "state", chosen, busy: false, note: "tick a repository" };
        // Every default the row showed: one on screen that Start does
        // not send is accepted without being made.
        // names(), not the name field: on the Design row it is the document,
        // and a session started without it is a plain one with no brief.
        const got = await reach.send("launcher", "/api/open", {
          dir: home,
          name: names(want) || undefined,
          also: also.length > 0 ? also : undefined,
          fresh: true,
          direct: !want.machine,
          ...picked(want),
        });
        return { kind: "state", chosen, busy: false, note: started(got) };
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
  "ade.commitPanel": commit,
  "ade.purgeWorktrees": from("purge"),
  "ade.disk": from("purge"),
  "ade.purgeDesigns": prune,
  "ade.newSession": (reach) => newSession(reach),
  "ade.quizMe": quiz,
};
