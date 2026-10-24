import type { Outcome } from "./jobpage";
import type { Issue } from "./client";
import { homedir } from "node:os";
import { join } from "node:path";
import { probe, read, type Server } from "./discover";
import type { Usage } from "./usage";

// Session is one conversation the launcher knows about.
export interface Session {
  id: string;
  agent: string;
  model?: string;
  dir: string;
  name: string;
  branch: string;
  title?: string;
  lastUsed?: string;
  resumable?: boolean;
  live?: boolean;
  here?: boolean;
  archived?: boolean;
  // group names the combined card this session is shown under, when it is in one.
  group?: string;
}

export interface Checkout {
  path: string;
  name: string;
  branch: string;
  sessions: Session[] | null;
  suggestion?: Suggestion;
}

// Suggestion is the completed, ungrouped sessions of one checkout, which the
// launcher thinks are worth combining. The fingerprint expires a dismissal.
export interface Suggestion {
  ids: string[];
  fingerprint: string;
}

export interface Repo {
  path: string;
  name: string;
  owner: string;
  checkouts: Checkout[] | null;
  // remembered is whether ade keeps a project memory for this repository,
  // which can be turned on from outside the checkout.
  remembered?: boolean;
}

// recordPath is the launcher's own record. It serves no checkout, so it writes
// under ade's home state rather than inside a git dir.
export function launcherRecord(home = homedir()): string {
  return join(home, ".ade", "server.json");
}

// find is the running launcher, or undefined when only a checkout's ade is up.
export async function find(home = homedir()): Promise<Server | undefined> {
  const rec = await read(launcherRecord(home));
  if (!rec) return undefined;
  return probe(rec);
}

// ensure is a running launcher, started if there was not one. The three steps
// are injected so this can be tested without spawning anything.
export async function ensure(
  look: () => Promise<Server | undefined>,
  spawn: () => void,
  wait: (
    look: () => Promise<Server | undefined>,
  ) => Promise<Server | undefined>,
): Promise<Server | undefined> {
  const found = await look();
  if (found) return found;
  spawn();
  return wait(look);
}

// all is every conversation, not the shortlist. Only the search wants it.
export async function repos(server: Server, all = false): Promise<Repo[]> {
  const res = await fetch(
    `${server.base}/api/repos?t=${encodeURIComponent(server.token)}${all ? "&all=1" : ""}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`repos: ${res.status}`);
  const body = (await res.json()) as { repos: Repo[] | null };
  return body.repos ?? [];
}

// open starts or reopens a session in a checkout. fresh is a new conversation
// rather than whatever that checkout was last in.
export async function open(
  server: Server,
  dir: string,
  fresh: boolean,
  agent = "",
  prompt = "",
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/open?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ dir, fresh, agent, prompt, direct: true }),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `open: ${res.status}`);
}

// workspace is `ade create <name>` from a checkout: a document, a worktree on
// the branch it names, and a session in it. path is that worktree.
export async function workspace(
  server: Server,
  dir: string,
  name: string,
  prompt: string,
): Promise<{ url: string; path: string }> {
  return tell<{ url: string; path: string }>(server, "/api/open", {
    dir,
    name,
    prompt,
    fresh: true,
    direct: true,
  });
}

// Host is a machine ade can put a session on. state is the launcher's own word
// for it: connected, off, connecting, and so on.
export interface Host {
  name: string;
  label?: string;
  connected?: boolean;
  local?: boolean;
  state?: string;
  sessions?: number;
  error?: string;
  load?: Load;
  // The row reads both from the server, not from a held promise.
  working?: string;
  failed?: string;
}

export async function hosts(server: Server): Promise<Host[]> {
  const res = await fetch(
    `${server.base}/api/hosts?t=${encodeURIComponent(server.token)}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`hosts: ${res.status}`);
  return ((await res.json()) as { hosts: Host[] | null }).hosts ?? [];
}

// reach connects a machine or lets it go. The name has to be one the launcher
// offered: it is handed to ssh.
export async function reach(
  server: Server,
  host: string,
  connect: boolean,
): Promise<void> {
  const path = connect ? "/api/connect" : "/api/disconnect";
  const res = await fetch(
    `${server.base}${path}?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ host }),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `${path}: ${res.status}`);
}

// editorServer is one running on another machine, forwarded to a port here.
// Minutes on a first install, one round trip on a reconnect.
export async function editorServer(
  server: Server,
  host: string,
  commit: string,
  template: string,
  unvalidated: boolean,
): Promise<{ port: number; token: string; commit: string }> {
  const res = await fetch(
    `${server.base}/api/remote/editor-server?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ host, commit, template, unvalidated }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `editor server: ${res.status}`);
  return (await res.json()) as {
    port: number;
    token: string;
    commit: string;
  };
}

// hostLine is the state and what is running on it, which is the whole of what a
// row has to say.
export function hostLine(host: Host): string {
  const parts = [host.state || (host.connected ? "connected" : "off")];
  if (host.sessions)
    parts.push(`${host.sessions} session${host.sessions === 1 ? "" : "s"}`);
  if (host.load) parts.push(loadLine(host.load));
  return parts.join(" · ");
}

export function hostIcon(host: Host): string {
  if (busy(host)) return "sync~spin";
  if (host.error) return "error";
  if (host.local) return "vm-active";
  return host.connected ? "vm-running" : "vm-outline";
}

// The launcher reports connected, quiet or off, never a state in between.
// These are the extension's own, held from the click until the call ends.
export const DOING = [
  "connecting",
  "disconnecting",
  "syncing",
  "updating",
  "stopping",
];

export function busy(host: Host): boolean {
  return DOING.includes(host.state ?? "");
}

// The launcher's own `working` outlasts this window, so it wins over `local`.
export function shown(host: Host, local?: string): Host {
  const doing = host.working || local;
  if (doing) return { ...host, state: doing, error: "" };
  if (host.failed) return { ...host, error: host.failed };
  return host;
}

// A finished action, named by the word the row showed while it ran.
export interface Settled {
  name: string;
  label: string;
  doing: string;
  failed?: string;
}

// A row that has stopped working has an outcome to report.
export function finished(
  was: Map<string, string>,
  now: Host[],
): { settled: Settled[]; working: Map<string, string> } {
  const settled: Settled[] = [];
  const working = new Map<string, string>();
  for (const host of now) {
    if (host.working) working.set(host.name, host.working);
    const doing = was.get(host.name);
    if (doing && !host.working)
      settled.push({
        name: host.name,
        label: host.label || host.name,
        doing,
        failed: host.failed,
      });
  }
  return { settled, working };
}

// holds reports whether one of a repository's checkouts is the one on screen,
// so the repository this window is in opens without being clicked.
export function holds(repo: Repo, root: string | undefined): boolean {
  if (!root) return false;
  return (repo.checkouts ?? []).some((checkout) => checkout.path === root);
}

// Waiting is a session that has asked you something and not been looked at.
export interface Waiting {
  id: string;
  dir: string;
  name: string;
  branch?: string;
  title?: string;
  lastUsed: string;
  asked?: string;
  seen?: string;
}

export async function waiting(server: Server): Promise<Waiting[]> {
  const res = await fetch(
    `${server.base}/api/waiting?t=${encodeURIComponent(server.token)}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`waiting: ${res.status}`);
  return ((await res.json()) as { waiting: Waiting[] | null }).waiting ?? [];
}

// seen marks a session looked at without opening it.
export async function seen(server: Server, id: string): Promise<void> {
  const res = await fetch(
    `${server.base}/api/waiting/seen?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ id }),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `seen: ${res.status}`);
}

// say answers another session where you are. The message queues behind
// whatever it is doing, and answering marks it seen.
export async function say(
  server: Server,
  id: string,
  message: string,
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/say?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ id, message }),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `say: ${res.status}`);
}

// Job is one scheduled definition as the launcher reports it. The file at
// `path` is the definition; everything else here is state around it.
export interface Job {
  name: string;
  description?: string;
  kind?: string;
  schedule: string;
  project?: string;
  continuity?: string;
  allow?: string;
  scope: string;
  path: string;
  nextDue?: string;
  lastFired?: string;
  paused?: boolean;
  failing?: boolean;
  running?: boolean;
  outcomes?: Outcome[] | null;
}

// Skipped is a file meant to be a job that could not be read as one.
export interface Skipped {
  path: string;
  error: string;
}

// dir adds that checkout's own jobs to the user's: the launcher is in none.
export async function jobs(
  server: Server,
  dir?: string,
): Promise<{ jobs: Job[]; skipped: Skipped[] }> {
  const where = dir ? `&dir=${encodeURIComponent(dir)}` : "";
  const res = await fetch(
    `${server.base}/api/jobs?t=${encodeURIComponent(server.token)}${where}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`jobs: ${res.status}`);
  const body = (await res.json()) as {
    jobs: Job[] | null;
    skipped: Skipped[] | null;
  };
  return { jobs: body.jobs ?? [], skipped: body.skipped ?? [] };
}

// run fires a job now and answers its outcome. A writes job started by hand
// branches beside dir, which is the window's checkout.
export async function run(
  server: Server,
  name: string,
  dir?: string,
): Promise<Outcome> {
  const res = await fetch(
    `${server.base}/api/jobs/run?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: { "X-Ade-Token": server.token, "Content-Type": "application/json" },
      body: JSON.stringify({ name, dir }),
    },
  );
  if (!res.ok) throw new Error((await res.text()).trim() || `run: ${res.status}`);
  return ((await res.json()) as { outcome: Outcome }).outcome;
}

// job posts one of the verbs on a row. Nothing reads the reply: the launcher
// broadcasts and the tree refetches.
export async function job(
  server: Server,
  path: string,
  body: unknown,
): Promise<void> {
  const res = await fetch(
    `${server.base}${path}?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `${path}: ${res.status}`);
}

// Hit is one line of one wiki page that matched.
export interface Hit {
  path: string;
  line: number;
  snippet: string;
  scope: string;
  updated?: string;
}

export async function wikiSearch(server: Server, q: string): Promise<Hit[]> {
  const at = `${server.base}/api/wiki/search?t=${encodeURIComponent(server.token)}&q=${encodeURIComponent(q)}`;
  const res = await fetch(at, { headers: { "X-Ade-Token": server.token } });
  if (!res.ok) throw new Error(`wiki: ${res.status}`);
  return ((await res.json()) as { hits: Hit[] | null }).hits ?? [];
}

// wikiPage is the Markdown, not the render: the editor opens the source.
export async function wikiPage(server: Server, path: string): Promise<string> {
  const bits = path.split("/").map(encodeURIComponent).join("/");
  const at = `${server.base}/api/wiki/page/${bits}?t=${encodeURIComponent(server.token)}`;
  const res = await fetch(at, { headers: { "X-Ade-Token": server.token } });
  if (!res.ok)
    throw new Error((await res.text()) || `wiki page: ${res.status}`);
  return ((await res.json()) as { markdown?: string }).markdown ?? "";
}

export async function expose(server: Server, port: number): Promise<void> {
  const res = await fetch(
    `${server.base}/api/ports/expose?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ port }),
    },
  );
  if (!res.ok) throw new Error((await res.text()) || `expose: ${res.status}`);
}

// Model is one entry of the catalogue the pickers offer.
export interface Model {
  id: string;
  label: string;
  agent: string;
  latest?: boolean;
  in?: string;
  out?: string;
}

export async function models(server: Server): Promise<Model[]> {
  const res = await fetch(
    `${server.base}/api/models?t=${encodeURIComponent(server.token)}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`models: ${res.status}`);
  return ((await res.json()) as { models: Model[] | null }).models ?? [];
}

// addHost bookmarks a machine. Its own call, not a settings save: the whole
// object going back would lose whatever else changed in between.
export async function addHost(
  server: Server,
  name: string,
  label: string,
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/hosts?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ name, label }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `add host: ${res.status}`);
}

export async function removeHost(server: Server, name: string): Promise<void> {
  const at = `${server.base}/api/hosts/${encodeURIComponent(name)}?t=${encodeURIComponent(server.token)}`;
  const res = await fetch(at, {
    method: "DELETE",
    headers: { "X-Ade-Token": server.token },
  });
  if (!res.ok)
    throw new Error(said(await res.text()) || `forget host: ${res.status}`);
}

// prune removes worktrees through the launcher rather than through the ade
// in one of them: a server cannot remove the checkout it is serving from.
export async function prune(
  server: Server,
  repo: string,
  paths: string[],
  force: boolean,
): Promise<{ removed?: string[]; kept?: { path: string; why: string }[] }> {
  const res = await fetch(
    `${server.base}/api/prune?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ repo, paths, force }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `prune: ${res.status}`);
  return (await res.json()) as {
    removed?: string[];
    kept?: { path: string; why: string }[];
  };
}

// said is the server's sentence out of its error envelope, so a refusal reads
// as the reason rather than as JSON.
export function said(body: string): string {
  try {
    return (JSON.parse(body) as { error?: string }).error ?? body;
  } catch {
    return body;
  }
}

// reason is the sentence out of a Client error, which reads
// "path: status {json}". Anything not of that shape is returned whole.
export function reason(err: unknown): string {
  const text = String(err);
  const at = text.indexOf("{");
  return at < 0 ? text : said(text.slice(at));
}

// Review is a pull request anywhere on GitHub waiting on your review. It
// comes from the launcher, not the checkout server: the search has no repo.
export interface Review {
  repo: string;
  number: number;
  title: string;
  author: string;
  draft?: boolean;
  url: string;
  updatedAt?: string;
  checks?: string;
  reviewDecision?: string;
  muted?: boolean;
  review?: ReviewState;
}

// ReviewState is how far your own review of a pull request has got, as the
// launcher read it off the worktree it was checked out into.
export interface ReviewState {
  state: "requested" | "running" | "ready" | "drafted" | "sent";
  threads?: number;
  unsent?: number;
  sent?: number;
  path?: string;
}

export async function reviews(server: Server): Promise<Asked> {
  const res = await fetch(
    `${server.base}/api/dash?t=${encodeURIComponent(server.token)}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`reviews: ${res.status}`);
  const said = (await res.json()) as {
    reviews: Review[] | null;
    reviewTrouble?: Trouble;
  };
  return { all: said.reviews ?? [], trouble: said.reviewTrouble ?? {} };
}

// Why the list may be short. Either way an empty one is not "nothing waiting".
export interface Trouble {
  error?: string;
  hidden?: number;
}

export interface Asked {
  all: Review[];
  trouble: Trouble;
}

export async function refreshDash(server: Server): Promise<void> {
  const res = await fetch(
    `${server.base}/api/dash/refresh?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `refresh: ${res.status}`);
}

// Muting is ade's own state, not GitHub's: a muted request is still listed,
// under its own heading, because putting something aside is not losing it.
export async function muteReview(
  server: Server,
  repo: string,
  number: number,
  muted: boolean,
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/reviews/mute?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ repo, number, muted }),
    },
  );
  if (!res.ok) throw new Error(said(await res.text()) || `mute: ${res.status}`);
}

// openReview checks the pull request out and starts an ade on it, which is
// what makes this more than a link to github.com.
export async function openReview(
  server: Server,
  repo: string,
  number: number,
): Promise<{ url?: string; path?: string }> {
  const res = await fetch(
    `${server.base}/api/reviews/open?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ repo, number }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `open review: ${res.status}`);
  return (await res.json()) as { url?: string; path?: string };
}

// runReview checks the pull request out and sets a cold review going on it.
// No window: the row is what reports on it, and the answer is immediate.
export async function runReview(
  server: Server,
  repo: string,
  number: number,
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/reviews/run?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ repo, number }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `run review: ${res.status}`);
}

export async function usage(server: Server): Promise<Usage> {
  const res = await fetch(
    `${server.base}/api/limits?t=${encodeURIComponent(server.token)}`,
    {
      headers: { "X-Ade-Token": server.token },
    },
  );
  if (!res.ok) throw new Error(`limits: ${res.status}`);
  return (await res.json()) as Usage;
}

// labelHost renames the row and nothing else: the name is what ssh dials, so
// it stays and the connection stays with it.
export async function labelHost(
  server: Server,
  name: string,
  label: string,
): Promise<void> {
  const at = `${server.base}/api/hosts/${encodeURIComponent(name)}?t=${encodeURIComponent(server.token)}`;
  const res = await fetch(at, {
    method: "PATCH",
    headers: {
      "X-Ade-Token": server.token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ label }),
  });
  if (!res.ok)
    throw new Error(said(await res.text()) || `rename host: ${res.status}`);
}

export type WorkerVerb = "credentials" | "update" | "stop";

// Returns once the launcher has the job: an update pulls images for minutes.
export async function worker(
  server: Server,
  verb: WorkerVerb,
  host: string,
): Promise<void> {
  const res = await fetch(
    `${server.base}/api/worker/${verb}?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ host }),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `${verb}: ${res.status}`);
}

// scratch makes a checkout for work with nowhere to live yet and starts ade
// in it. The reply names where it landed, which is the folder to open.
export async function scratch(
  server: Server,
  start: Omit<Start, "dir" | "also" | "brief"> & { name: string },
): Promise<{ url: string; path: string }> {
  const res = await fetch(
    `${server.base}/api/scratch?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(start),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `scratch: ${res.status}`);
  return (await res.json()) as { url: string; path: string };
}

// ask reads one of the launcher's own routes. path may carry a query.
export async function ask<T>(server: Server, path: string): Promise<T> {
  const join = path.includes("?") ? "&" : "?";
  const res = await fetch(
    `${server.base}${path}${join}t=${encodeURIComponent(server.token)}`,
    { headers: { "X-Ade-Token": server.token } },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `${path}: ${res.status}`);
  return (await res.json()) as T;
}

// Load is one machine's pressure, as the launcher last measured it.
export interface Load {
  name: string;
  cores: number;
  load1: number;
  memTotal: number;
  memUsed: number;
  agents: number;
}

// loadLine is that pressure in a row's worth of words: load against cores,
// memory in use, and how many agents are on it.
export function loadLine(load: Load): string {
  const gb = (bytes: number) => (bytes / 1024 ** 3).toFixed(1);
  const parts = [`load ${load.load1.toFixed(1)}/${load.cores}`];
  if (load.memTotal > 0)
    parts.push(`${gb(load.memUsed)}/${gb(load.memTotal)} GB`);
  if (load.agents > 0)
    parts.push(`${load.agents} agent${load.agents === 1 ? "" : "s"}`);
  return parts.join(" · ");
}

// withLoad puts each reading on the machine it is of. A machine with none —
// off, or one that did not answer — is left as it was.
export function withLoad(hosts: Host[], loads: Load[]): Host[] {
  const by = new Map(loads.map((load) => [load.name, load]));
  return hosts.map((host) => {
    const load = by.get(host.name);
    return load ? { ...host, load } : host;
  });
}

// tell posts to one of the launcher's own routes and reads what it answers.
export async function tell<T>(
  server: Server,
  path: string,
  body: unknown,
): Promise<T> {
  const res = await fetch(
    `${server.base}${path}?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `${path}: ${res.status}`);
  return (await res.json()) as T;
}

// put and drop are the launcher's other two verbs, for a route that has no
// POST. Neither reads a reply: the caller re-reads what it is showing.
export async function put(
  server: Server,
  path: string,
  body: unknown,
): Promise<void> {
  await send(server, path, "PUT", body);
}

export async function drop(server: Server, path: string): Promise<void> {
  await send(server, path, "DELETE", undefined);
}

async function send(
  server: Server,
  path: string,
  method: string,
  body: unknown,
): Promise<void> {
  const res = await fetch(
    `${server.base}${path}?t=${encodeURIComponent(server.token)}`,
    {
      method,
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    },
  );
  if (!res.ok)
    throw new Error(said(await res.text()) || `${path}: ${res.status}`);
}

// Target is what one machine can offer: the repositories it has checked out,
// and the models it can run. Asking connects it if it is not already.
export interface Target {
  repos: Repo[] | null;
  models: { id: string; label?: string }[] | null;
}

export async function target(server: Server, machine: string): Promise<Target> {
  return tell<Target>(server, "/api/target", { machine });
}

// Start is everything /api/open takes from the new-session screen: the
// defaults it shows, rather than the two the input chain asked for.
export interface Start {
  dir: string;
  name?: string;
  also?: string[];
  agent?: string;
  model?: string;
  role?: string;
  prompt?: string;
  machine?: string;
  sandbox?: boolean;
  // mounts and env go into the repository's sandbox file before the session
  // starts, so the first turn already has them.
  mounts?: { path: string; rw?: boolean }[];
  env?: { [key: string]: string };
  // brief is a document carried from this machine, written into the session
  // once it is serving.
  brief?: { name: string; body: string };
  // files go with the first message, as the chat's own attachments do.
  files?: { name: string; mediaType: string; data: string }[];
}

// begin returns a failure that still carries a url rather than throwing it:
// the session is open, and this is the only address anybody will get for it.
export async function begin(
  server: Server,
  start: Start,
): Promise<{ url: string; path?: string; error?: string }> {
  const res = await fetch(
    `${server.base}/api/open?t=${encodeURIComponent(server.token)}`,
    {
      method: "POST",
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        ...start,
        fresh: true,
        // A machine's reply is proxied through this launcher, so only a session
        // on this one is asked for its own address.
        direct: !start.machine,
      }),
    },
  );
  const text = await res.text();
  let body: { url?: string; path?: string } = {};
  try {
    body = JSON.parse(text) as { url?: string; path?: string };
  } catch {
    body = {};
  }
  if (res.ok) return { url: body.url ?? "", path: body.path };
  const why = said(text) || `/api/open: ${res.status}`;
  if (!body.url) throw new Error(why);
  return { url: body.url, path: body.path, error: why };
}

// openOn starts a session in a checkout on another machine.
export async function openOn(
  server: Server,
  machine: string,
  dir: string,
  prompt: string,
): Promise<{ url: string; path?: string }> {
  return tell<{ url: string; path?: string }>(server, "/api/open", {
    machine,
    dir,
    prompt,
  });
}

// Machine is one bookmarked machine and what it has checked out. Nothing is
// dialled for it: a machine not already connected is not on the list.
export interface Machine {
  machine: string;
  repos: Repo[] | null;
}

// Up is one ade the launcher has running, which is what a window has.
export interface Up {
  id: string;
  dir: string;
  name: string;
  repo?: string;
  branch?: string;
  agent?: { running?: boolean };
}

// serving is the sessions the launcher has up right now: the process list.
export async function serving(server: Server): Promise<Up[]> {
  const got = await ask<Up[] | null>(server, "/api/serving");
  return got ?? [];
}

export async function hostRepos(server: Server): Promise<Machine[]> {
  const got = await ask<{ hosts: Machine[] | null }>(
    server,
    "/api/hosts/repos",
  );
  return got.hosts ?? [];
}

// The launcher is a second server, with an event stream of its own.
// The server pings every 20s, so this is three missed pings.
export const stale = 60_000;

export function watch(
  server: Server,
  named: (event: string) => void,
  stopped?: AbortSignal,
): Promise<void> {
  return (async () => {
    const res = await fetch(
      `${server.base}/api/events?t=${encodeURIComponent(server.token)}`,
      { headers: { "X-Ade-Token": server.token }, signal: stopped },
    );
    if (res.body) await events(res.body, named);
  })();
}

// events names each event in an SSE body and throws when the stream goes
// quiet: a half-open socket delivers nothing and never errors.
export async function events(
  body: ReadableStream<Uint8Array>,
  named: (event: string) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let rest = "";
  for (;;) {
    let tick: ReturnType<typeof setTimeout> | undefined;
    const quiet = new Promise<never>((_, fail) => {
      tick = setTimeout(() => fail(new Error("ade stopped sending events")), stale);
    });
    let got: Awaited<ReturnType<typeof reader.read>>;
    try {
      got = await Promise.race([reader.read(), quiet]);
    } catch (err) {
      void reader.cancel().catch(() => undefined);
      throw err;
    } finally {
      clearTimeout(tick);
    }
    if (got.done) return;
    rest += decoder.decode(got.value, { stream: true });
    const frames = rest.split("\n\n");
    rest = frames.pop() ?? "";
    for (const frame of frames) {
      const name = /^event: (\w+)$/m.exec(frame);
      if (name) named(name[1]);
    }
  }
}

// patch is the launcher's fourth verb, for a route that edits one row.
export async function patch(
  server: Server,
  path: string,
  body: unknown,
): Promise<void> {
  await send(server, path, "PATCH", body);
}

export interface Notification {
  id: string;
  repo: string;
  reason: string;
  type: string;
  title: string;
  number?: number;
  url: string;
  updatedAt: string;
  unread: boolean;
  dismissed?: boolean;
}

export interface Started {
  dir: string;
  branch: string;
  session?: string;
}

export interface Dash {
  reviews: Review[] | null;
  notifications: Notification[] | null;
  linear?: { issues: Issue[] | null; error?: string } | null;
  started?: Record<string, Started> | null;
}

// dashOf is the whole of what the launcher keeps for the dashboard; reviews()
// above reads only the reviews out of the same answer.
export async function dashOf(server: Server): Promise<Dash> {
  const res = await fetch(
    `${server.base}/api/dash?t=${encodeURIComponent(server.token)}`,
    { headers: { "X-Ade-Token": server.token } },
  );
  if (!res.ok) throw new Error(`dash: ${res.status}`);
  return (await res.json()) as Dash;
}

export async function dismissNotification(
  server: Server,
  id: string,
  dismissed = true,
): Promise<void> {
  await tell(server, "/api/notifications/dismiss", { id, dismissed });
}

export async function startTicket(
  server: Server,
  identifier: string,
  dir: string,
): Promise<{ url?: string; path?: string }> {
  return tell<{ url?: string; path?: string }>(server, "/api/linear/start", {
    identifier,
    dir,
  });
}
