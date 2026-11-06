// What the Electron preload used to be. The page is served over loopback,
// so it fetches and opens its own sockets.
import { keeper } from "./held";
import { Keymap, merge, shared, type Binding } from "./keymap";

export interface Where {
  base: string;
  token: string;
}

const commands: Array<(command: string) => void> = [];
const events: Array<(name: string) => void> = [];
const streams: Array<(open: boolean) => void> = [];
const terms: Array<(pane: string, data: string) => void> = [];
const closed: Array<(pane: string, why: string) => void> = [];

const sockets = new Map<string, WebSocket>();
const sizes = new Map<string, string>();
const kept = keeper(why);

let place: Promise<Where> | undefined;
const where = (): Promise<Where> =>
  (place ??= fetch("/host/where").then((r) => r.json() as Promise<Where>));

// why is what a failed read says. A 404 from the launcher is almost
// always age: it predates the routes this branch added.
export function why(e: Error): string {
  return /: (404 Not Found|405 Method Not Allowed)$/.test(e.message)
    ? `${e.message} — this ade is older than the window. Reinstall it and restart.`
    : e.message;
}

// reach is the proxied path to ade. A session verb goes through the
// launcher's proxy, the one address that works.
function reach(at: string, path: string, session?: string): string | undefined {
  if (at !== "session") return `/ade${path}`;
  return session ? `/ade/s/${session}${path}` : undefined;
}

async function asJSON(path: string, init?: RequestInit): Promise<unknown> {
  const res = await fetch(path, init);
  if (!res.ok) throw new Error(await said(res, path));
  return res.status === 204 ? undefined : ((await res.json()) as unknown);
}

// said is what went wrong, in ade's words where it gave any. The window reads
// those words: "no ade is serving that session" is what starts one.
async function said(res: Response, path: string): Promise<string> {
  const text = await res.text().catch(() => "");
  try {
    const body = JSON.parse(text) as { error?: string };
    if (body.error) return `${path}: ${body.error}`;
  } catch {
    // Not JSON: the status is all there is.
  }
  return `${path}: ${res.status} ${res.statusText}`;
}

// pump carries one event stream to the window: the event's name, and
// whether the stream is up.
async function pump(path: string, say: boolean): Promise<void> {
  try {
    const res = await fetch(path);
    if (!res.body) return;
    if (say) streams.forEach((then) => then(true));
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let rest = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      rest += decoder.decode(value, { stream: true });
      const frames = rest.split("\n\n");
      rest = frames.pop() ?? "";
      for (const frame of frames) {
        const name = /^event: (\w+)$/m.exec(frame)?.[1];
        if (name) events.forEach((then) => then(name));
      }
    }
  } catch {
    // A stream that drops is told as closed; the window polls regardless.
  }
  if (say) streams.forEach((then) => then(false));
}

async function openTerm(session: string, pane: string): Promise<unknown> {
  sockets.get(pane)?.close();
  sockets.delete(pane);
  if (!session) return { ok: true };

  const at = await where();
  const url = new URL(at.base);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = `/s/${session}/api/terminal`;
  url.searchParams.set("t", at.token);
  url.searchParams.set("pane", pane);

  const sock = new WebSocket(url.toString());
  sock.binaryType = "arraybuffer";
  sockets.set(pane, sock);
  // A character may be split across two frames.
  const decoder = new TextDecoder();
  sock.onopen = () => {
    const size = sizes.get(pane);
    if (size) sock.send(size);
  };
  sock.onmessage = (e: MessageEvent<ArrayBuffer | string>) => {
    const data = typeof e.data === "string" ? e.data : decoder.decode(e.data, { stream: true });
    terms.forEach((then) => then(pane, data));
  };
  sock.onclose = () => {
    if (sockets.get(pane) !== sock) return;
    sockets.delete(pane);
    closed.forEach((then) => then(pane, ""));
  };
  sock.onerror = () => closed.forEach((then) => then(pane, "the terminal socket failed"));
  return { ok: true };
}

// gather is everything the sidebar draws, in one round trip. A part that
// fails keeps the rows it had and says so by name.
async function gather(dir = ""): Promise<unknown> {
  // No launcher is not a failed read: the window has a state for it, and
  // offers to start one.
  if (!(await fetch("/host/where")).ok) {
    return { offline: "ade is not running. Start it from a checkout." };
  }
  const trouble: Record<string, string> = {};
  const rows = <T>(key: string, path: string, pick: (got: any) => T[]) =>
    kept(key, dir, asJSON(`/ade${path}`).then(pick), trouble);
  const [dash, repos, jobs, active, spaces] = await Promise.all([
    asJSON("/ade/api/dash").catch((e: Error) => ({ error: why(e) })),
    rows("sessions", "/api/repos", (g) => g.repos ?? []),
    rows("jobs", "/api/jobs", (g) => g.jobs ?? []),
    rows("agents", "/api/serving", (g) => g ?? []),
    rows("spaces", dir ? `/api/spaces?dir=${encodeURIComponent(dir)}` : "/api/spaces",
      (g) => g.spaces ?? []),
  ]);
  return { dash, repos, jobs, active, spaces, trouble };
}

// chords put the keyboard in the page. The table is the same one the
// shell held, so a binding means here what it has always meant.
function chords(): void {
  let map = new Keymap(merge(undefined), true);
  void fetch("/host/keys")
    .then((r) => r.json() as Promise<Binding[]>)
    .then((mine) => {
      map = new Keymap(merge(mine), true);
    })
    .catch(() => undefined);

  window.addEventListener(
    "keydown",
    (e) => {
      const command = map.match({
        key: e.key,
        code: e.code,
        control: e.ctrlKey,
        meta: e.metaKey,
        alt: e.altKey,
        shift: e.shiftKey,
        type: "keyDown",
      });
      if (!command) return;
      if (!shared(command)) e.preventDefault();
      commands.forEach((then) => then(command));
    },
    // Before the page: the chord table decides and nothing else gets a say.
    true,
  );
}

export const bridge = {
  onCommand: (then: (command: string) => void) => commands.push(then),
  keys: () => fetch("/host/keys").then((r) => r.json()),
  world: (dir?: string) => gather(dir),
  start: () => asJSON("/host/start", { method: "POST" }).catch((e: Error) => ({ error: e.message })),
  watch: (session?: string) => {
    if (session) void pump(`/ade/s/${session}/api/events`, true);
    return Promise.resolve({ ok: Boolean(session) });
  },
  listen: () => {
    void pump("/ade/api/events", false);
    return Promise.resolve({ ok: true });
  },
  onEvent: (then: (name: string) => void) => events.push(then),
  onStream: (then: (open: boolean) => void) => streams.push(then),
  // The host proxies this path and puts the session's token on it.
  shotSrc: (session: string, name: string) =>
    `/ade/s/${encodeURIComponent(session)}/api/shots/${encodeURIComponent(name)}`,
  term: (session: string, pane: string) => openTerm(session, pane),
  termSend: (pane: string, data: string) => {
    const sock = sockets.get(pane);
    if (sock?.readyState === WebSocket.OPEN) sock.send(new TextEncoder().encode(data));
  },
  termResize: (pane: string, cols: number, rows: number) => {
    const size = JSON.stringify({ type: "resize", cols, rows });
    sizes.set(pane, size);
    const sock = sockets.get(pane);
    if (sock?.readyState === WebSocket.OPEN) sock.send(size);
  },
  onTerm: (then: (pane: string, data: string) => void) => terms.push(then),
  onTermClosed: (then: (pane: string, why: string) => void) => closed.push(then),
  link: (href: string) =>
    fetch("/host/open", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ href }),
    }),
  act: async (
    at: string,
    path: string,
    body: Record<string, unknown>,
    session?: string,
    method?: string,
  ) => {
    const full = reach(at, path, session);
    if (!full) return { error: "pick a session first; this acts on one" };
    return asJSON(full, {
      method: method ?? "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    }).catch((e: Error) => ({ error: why(e) }));
  },
  call: async (at: string, path: string, method = "GET", body?: string, session?: string) => {
    const full = reach(at, path, session);
    if (!full) return { status: 0, error: "no session picked" };
    try {
      const res = await fetch(full, {
        method,
        headers: { "Content-Type": "application/json" },
        body,
      });
      return { status: res.status, ok: res.ok, text: await res.text() };
    } catch (e) {
      return { status: 0, error: (e as Error).message };
    }
  },
  read: async (at: string, path: string, session?: string) => {
    const full = reach(at, path, session);
    if (!full) return { error: "no session picked" };
    return asJSON(full).catch((e: Error) => ({ error: why(e) }));
  },
  panel: async (id: string, html: string) => {
    const got = (await asJSON(`/host/panel/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: html,
    }).catch(() => ({}))) as { url?: string };
    return got.url ?? "";
  },
  where: () => where(),
  opened: () =>
    fetch("/host/opened")
      .then((r) => r.json() as Promise<{ dir: string }>)
      .then((got) => got.dir)
      .catch(() => ""),
};

// The screenshot harness puts its own bridge on the page before the
// bundle runs; it is a fixture, and the real one must not replace it.
if (!(window as unknown as { ade?: unknown }).ade) {
  chords();
  (window as unknown as { ade: typeof bridge }).ade = bridge;
}
