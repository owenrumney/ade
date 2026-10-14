import { readFile, stat } from "node:fs/promises";
import type { Stats } from "node:fs";
import { isAbsolute, join, sep } from "node:path";
import { currentProxy, launcherAt, type Serving } from "./remote";

// Record is <git dir>/ade/server.json, written 0600 by the running ade. Its URL
// carries the session token, which is why the file is a credential.
export interface Record {
  pid: number;
  url: string;
  root: string;
  startedAt?: string;

  // proxy is the same session through its launcher. On a worker, url is the
  // container's own port and only the launcher's is published to this host.
  proxy?: string;
}

// Health is what /healthz answers without a token.
export interface Health {
  ade?: boolean;
  root?: string;
  pid?: number;
}

export interface Server {
  base: string;
  token: string;
  root: string;
}

export function recordPath(gitDir: string): string {
  return join(gitDir, "ade", "server.json");
}

// label names the checkout a window is attached to, for the view header. Two
// worktrees of one repository are the same project and not the same work.
export function label(
  project: string | undefined,
  root: string | undefined,
): string {
  const dir = root ? (root.split(sep).pop() ?? "") : "";
  if (!project) return dir;
  const repo = project.split("/").pop();
  return dir && dir !== repo ? `${project} · ${dir}` : project;
}

// nearest orders a window's folders by what you are looking at. A window
// attaches to one ade, and folder order picked a checkout you may not have open.
export function nearest(
  folders: string[],
  active: string | undefined,
): string[] {
  if (!active) return folders;
  const under = folders.filter(
    (f) => active === f || active.startsWith(f + sep),
  );
  if (under.length === 0) return folders;
  // The longest match, so a checkout nested inside another wins over its parent.
  const own = under.reduce((a, b) => (b.length > a.length ? b : a));
  return [own, ...folders.filter((f) => f !== own)];
}

// gitDir is the folder's git directory. In a worktree `.git` is a file naming
// the real one, and ade records into that, so a worktree is missed without this.
export async function gitDir(folder: string): Promise<string | undefined> {
  const dot = join(folder, ".git");
  let info: Stats;
  try {
    info = await stat(dot);
  } catch {
    return undefined;
  }
  if (info.isDirectory()) return dot;

  let raw: string;
  try {
    raw = await readFile(dot, "utf8");
  } catch {
    return undefined;
  }
  const named = /^gitdir:\s*(.+)$/m.exec(raw);
  if (!named) return undefined;
  const path = named[1].trim();
  return isAbsolute(path) ? path : join(folder, path);
}

// read loads the record, or undefined when ade is not running for this checkout.
// A missing file is the ordinary case and never an error.
export async function read(path: string): Promise<Record | undefined> {
  let raw: string;
  try {
    raw = await readFile(path, "utf8");
  } catch {
    return undefined;
  }
  try {
    const rec = JSON.parse(raw) as Record;
    return rec.pid > 0 && rec.url ? rec : undefined;
  } catch {
    return undefined;
  }
}

// split pulls the token out of the recorded URL. The path is kept: a proxied
// session lives under /s/<id>, and dropping it would address the launcher.
export function split(
  url: string,
): { base: string; token: string } | undefined {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return undefined;
  }
  const token = parsed.searchParams.get("t");
  if (!token) return undefined;
  return { base: parsed.origin + parsed.pathname.replace(/\/$/, ""), token };
}

// probe confirms the address belongs to the process the record names. Its own
// first, then through the launcher, which on a worker is the only way in.
export async function probe(
  rec: Record,
  fetchFn: typeof fetch = fetch,
): Promise<Server | undefined> {
  for (const url of [rec.url, rec.proxy]) {
    if (!url) continue;
    const found = await reach(rec, url, fetchFn);
    if (found) return found;
  }
  // The proxied path carries a session id, and a new conversation changes it.
  // The launcher it names knows which session is serving this checkout now.
  const now = rec.proxy ? await asked(rec, rec.proxy, fetchFn) : undefined;
  return now ? reach(rec, now, fetchFn) : undefined;
}

// asked is the launcher's own answer for this checkout, or nothing when it
// will not say. A failure here is ordinary: the launcher may have gone too.
async function asked(
  rec: Record,
  proxy: string,
  fetchFn: typeof fetch,
): Promise<string | undefined> {
  const parts = split(proxy);
  if (!parts) return undefined;
  const at = launcherAt(parts.base, parts.token);
  if (!at) return undefined;
  try {
    const res = await fetchFn(
      `${at.base}/api/serving?local=1&t=${encodeURIComponent(at.token)}`,
    );
    if (!res.ok) return undefined;
    const serving = (await res.json()) as Serving[];
    return currentProxy(at.base, at.token, rec.root, serving ?? []);
  } catch {
    return undefined;
  }
}

async function reach(
  rec: Record,
  url: string,
  fetchFn: typeof fetch,
): Promise<Server | undefined> {
  const parts = split(url);
  if (!parts) return undefined;

  let health: Health;
  try {
    const res = await fetchFn(
      `${parts.base}/healthz?t=${encodeURIComponent(parts.token)}`,
    );
    if (!res.ok) return undefined;
    health = (await res.json()) as Health;
  } catch {
    return undefined;
  }
  if (!health.ade || health.pid !== rec.pid || health.root !== rec.root) {
    return undefined;
  }
  return { base: parts.base, token: parts.token, root: rec.root };
}
