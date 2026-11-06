import type { Panel } from "./panel";
import { fresh, names, type Chosen } from "../pages/newsession/view";
import { BODY as SETTINGS_BODY } from "../pages/settings/body";
import { newSeed, setLastAgent } from "./terminalmode";

// The two pages the window frames, and what each asks of ade.

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
  // A panel is a document with no shell, so following a link is the window's.
  link(href: string): void;
  // follow moves the window onto a session just started in a checkout.
  follow(dir: string): void;
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

// picked is what the form chose, in the names `/api/open` and `/api/scratch` take.
function picked(want: Chosen): Record<string, unknown> {
  return { agent: want.agent || undefined, prompt: want.message.trim(), terminal: true };
}

// started is where it went, or why it did not: the launcher answers a
// failure that still opened a session, so both can be true.
function started(reach: Reach, got: unknown, dir: string): string {
  const back = (got ?? {}) as { error?: string; path?: string };
  if (back.error) return back.error;
  const at = back.path ?? dir;
  if (!at) return "started";
  reach.follow(at);
  return "";
}

interface Checkout {
  name: string;
  at: string;
  note: string;
}

// checkoutsIn is what Open branch offers: the worktrees home has, then the
// branches with none.
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

export function newSession(reach: Reach, seed: Partial<Chosen> = {}): Panel {
  let chosen: Chosen = { ...fresh, mode: "direct", ...seed };
  const offer = async () => {
    const repos = await reach.get("launcher", "/api/repos").catch(() => ({ repos: [] }));
    return {
      repos: (repos as { repos?: { name: string; path: string }[] }).repos ?? [],
      agents: [
        { id: "claude", label: "claude" },
        { id: "codex", label: "codex" },
      ],
      ...(await checkoutsIn(reach, chosen)),
    };
  };
  const state = (note: string, busy = false) => ({ kind: "state", chosen, busy, note });
  return {
    id: "newsession",
    title: "New session",
    root: "newsession",
    bundle: "newsession.js",
    styles: ["newsession.css"],
    launcher: true,
    async onMessage(said) {
      if (said.kind === "ready") return { ...state(""), offer: await offer() };
      if (said.kind === "change") {
        const was = chosen;
        chosen = (said.chosen ?? chosen) as Chosen;
        // Only a new home or a new row has a new list to offer. Answering
        // every keystroke redraws the page and takes the cursor out of the box.
        if (chosen.repos[0] === was.repos[0] && chosen.mode === was.mode) return undefined;
        return { ...state(""), offer: await offer() };
      }
      if (said.kind !== "start") return undefined;
      chosen = (said.chosen ?? chosen) as Chosen;
      const want = chosen;
      setLastAgent(want.agent);
      // Scratch has nowhere to live yet, so no repository and its own route.
      if (want.mode === "scratch") {
        reach.push("newsession", state(`Making a checkout for ${want.name.trim() || "it"}…`, true));
        const got = await reach.send("launcher", "/api/scratch", { name: want.name.trim(), ...picked(want) });
        return state(started(reach, got, ""));
      }
      const [home, ...also] = want.repos;
      if (!home) return state("tick a repository");
      // Open branch names a branch the repository already has: find its
      // checkout, make one only when it has none, and open that.
      if (want.mode === "branch") {
        reach.push("newsession", state(want.at ? `Opening ${want.branch}…` : `Checking ${want.branch} out…`, true));
        let at = want.at;
        if (!at) {
          const made = (await reach.send("launcher", "/api/branches/worktree", {
            repo: home,
            branch: want.branch,
          })) as { path?: string; error?: string };
          if (made.error || !made.path) return state(made.error ?? "no checkout was made");
          at = made.path;
        }
        const opened = await reach.send("launcher", "/api/open", { dir: at, fresh: true, direct: true, ...picked(want) });
        return state(started(reach, opened, at));
      }
      // The ticket instead of a name on that row: only the server can read
      // a GitHub issue, so it names the branch.
      const ticket = want.mode === "ticket" ? want.ticket.trim() : "";
      reach.push("newsession", state(`Starting ${ticket || names(want) || home.split("/").pop()}…`, true));
      const got = await reach.send("launcher", "/api/open", {
        dir: home,
        name: ticket ? undefined : names(want) || undefined,
        ticket: ticket || undefined,
        also: also.length > 0 ? also : undefined,
        fresh: true,
        direct: true,
        ...picked(want),
      });
      return state(started(reach, got, home));
    },
  };
}

// WHICH names the panel each command opens, so `doing.ts` can point at
// one without knowing how it is built.
export const WHICH: Record<string, (reach: Reach) => Panel> = {
  "ade.openSettings": settings,
  "ade.newSession": (reach) => newSession(reach, newSeed()),
};
