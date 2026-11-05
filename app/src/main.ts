import { app, BrowserWindow, Menu, ipcMain, nativeImage, shell } from "electron";
import { readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { StringDecoder } from "node:string_decoder";
// Electron 33's main is Node 20: no WebSocket global without a flag.
import WebSocket from "ws";
import { Keymap, merge, shared, type Binding } from "./keymap";
import { keeper } from "./held";
import { wire } from "./contextmenu";
import { away } from "./away";
import { shellPath } from "./shellpath";
import { ask, events, found, tell, type At } from "./launcher";
import { declare, hold, panelOf, serve } from "./webview";
import { launcherArgs, start } from "../pages/start";

// Before whenReady: a privileged scheme cannot be declared after it.
declare();

const mac = process.platform === "darwin";
// What the window is called: the menu bar on a Mac, and the directory it
// keeps its state in.
const NAME = "ADE";

// serving names the ade behind a session, for a read that failed: a
// route it has not got is the commonest cause, and the version says so.
async function serving(where: At, session?: string): Promise<string> {
  const health = await ask<{ version?: string; root?: string }>(
    where,
    `/s/${session}/healthz`,
  ).catch(() => undefined);
  if (!health) return `(session ${session}; nothing answered its /healthz)`;
  return `(session ${session}, served by ade ${health.version || "of unknown age"})`;
}

// opened is the directory the window was started on. Electron keeps the
// script path in a dev run and not in a packaged one.
function opened(): string {
  for (const arg of process.argv.slice(app.isPackaged ? 1 : 2)) {
    if (arg.startsWith("-")) continue;
    try {
      if (statSync(arg).isDirectory()) return resolve(arg);
    } catch {
      // Not a path: electron is given flags and a dot as well.
    }
  }
  return "";
}

// keys is the keyboard as this machine has it: ade's table, with the
// person's own laid over it.
function keys(): Keymap {
  let mine: Binding[] | undefined;
  try {
    const raw = readFileSync(join(homedir(), ".ade", "keys.json"), "utf8");
    mine = JSON.parse(raw) as Binding[];
  } catch {
    // No file, or one that does not parse: the defaults stand.
  }
  return new Keymap(merge(mine), mac);
}

// The window ade draws itself: no native title bar, because the omnibox
// sits where one would be and the traffic lights are inset over it.
function make(map: Keymap): BrowserWindow {
  const win = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 900,
    minHeight: 600,
    show: false,
    // Windows and Linux take the icon per window; a Mac takes the dock's.
    icon: mark(),
    titleBarStyle: mac ? "hiddenInset" : "hidden",
    trafficLightPosition: { x: 14, y: 12 },
    backgroundColor: "#1f1f1f",
    webPreferences: {
      preload: join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  wire(win, panelOf);
  away(win);
  void win.loadFile(join(__dirname, "..", "renderer", "index.html"));
  win.once("ready-to-show", () => win.show());
  // A page error used to go nowhere at all: `make app` prints them now.
  win.webContents.on("console-message", (_e, level, message, line, source) => {
    if (level >= 2) console.error(`renderer: ${message} (${source}:${line})`);
  });

  // Every keystroke, before the page sees it. This is why ade is an
  // app: the chord table decides and nothing else gets a say.
  win.webContents.on("before-input-event", (event, input) => {
    const command = map.match(input);
    if (!command) return;
    if (!shared(command)) event.preventDefault();
    win.webContents.send("command", command);
  });
  return win;
}

// Said here as well as in package.json, so an unpackaged run is the same
// name as the packaged one rather than "Electron".
app.setName(NAME);

// mark is ade's own icon. Packaged, the bundle's icns answers for it; an
// unpackaged run has no bundle and shows electron's until it is told.
function mark(): ReturnType<typeof nativeImage.createFromPath> | undefined {
  // dist/../icon, which is where it sits packaged and unpackaged alike.
  const img = nativeImage.createFromPath(join(__dirname, "..", "icon", "ade.png"));
  return img.isEmpty() ? undefined : img;
}

// ade's own menu, so no stock accelerator is registered behind its
// back. Edit and Window keep their roles; their keys go through first.
function menu(): void {
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(mac ? [{ role: "appMenu" as const }] : []),
      { role: "editMenu" },
      { label: "View", submenu: [{ role: "reload" as const }, { role: "toggleDevTools" as const }] },
      { role: "windowMenu" },
    ]),
  );
}

// why is what a failed read says. A 404 from the launcher is almost
// always age: it predates the routes this branch added.
function why(e: Error): string {
  // A bare status, with no error of ade's own: the route itself is
  // missing, so the ade answering is not this build.
  return /: (404 Not Found|405 Method Not Allowed)$/.test(e.message)
    ? `${e.message} — this ade is older than the window. Reinstall it and restart.`
    : e.message;
}

// gather is one round trip for everything the sidebar draws; each part
// may fail on its own. `dir` is the checkout the window is on.
const gatherWait = 10_000;

const kept = keeper(why);

async function gather(dir = ""): Promise<unknown> {
  const where = await found();
  if (!where) {
    return { offline: "ade is not running. Start it from a checkout." };
  }
  // A part that fails says so by name. Swallowing it draws an empty list,
  // which reads as ade having nothing rather than as a broken read.
  const trouble: Record<string, string> = {};
  const [dash, repos, jobs, active, spaces] = await Promise.all([
    ask(where, "/api/dash", gatherWait).catch((e: Error) => ({ error: e.message })),
    kept(
      "sessions",
      "",
      ask<{ repos?: unknown[] }>(where, "/api/repos", gatherWait).then((r) => r.repos ?? []),
      trouble,
    ),
    kept(
      "jobs",
      "",
      ask<{ jobs?: unknown[] }>(where, "/api/jobs", gatherWait).then((j) => j.jobs ?? []),
      trouble,
    ),
    // Every announced session and what its agent is doing.
    kept("agents", "", ask<unknown[]>(where, "/api/serving", gatherWait).then((a) => a ?? []), trouble),
    kept(
      "spaces",
      dir,
      ask<{ spaces?: unknown[] }>(
        where,
        dir ? `/api/spaces?dir=${encodeURIComponent(dir)}` : "/api/spaces",
        gatherWait,
      ).then((s) => s.spaces ?? []),
      trouble,
    ),
  ]);
  return { dash, repos, jobs, active, spaces, trouble };
}

void app.whenReady().then(async () => {
  if (mac) {
    process.env.PATH = (await shellPath()) ?? process.env.PATH;
  }
  const map = keys();
  const ico = mark();
  if (ico && app.dock) app.dock.setIcon(ico);
  // Where the panels' assets are: the repository root in a checkout,
  // and what pack.sh staged beside the app once packaged.
  serve(app.isPackaged ? join(process.resourcesPath, "ade") : join(__dirname, "..", ".."));
  menu();
  // A panel's page, held under the webview scheme and answered with its
  // url. The window puts that in an iframe.
  ipcMain.handle("ade.panel", (_e, id: string, html: string) => hold(id, html));
  // Where ade is, for a panel that fetches rather than posting messages.
  ipcMain.handle("ade.where", async () => (await found()) ?? null);
  // The checkout named on the command line, for `ade .` opening this
  // window on one rather than on whatever was last used.
  ipcMain.handle("ade.opened", () => opened());
  make(map);
  // start brings the launcher up, the extension's own spawn from the
  // same module. Nothing else on the machine starts one.
  ipcMain.handle("ade.start", async () => {
    if (await found()) return { ok: true };
    let missing = false;
    start(launcherArgs, undefined, () => {
      missing = true;
    });
    for (let tries = 0; tries < 20 && !missing; tries++) {
      await new Promise((done) => setTimeout(done, 400));
      if (await found()) return { ok: true };
    }
    return {
      error: missing
        ? "no ade binary found. go install ./cmd/ade, or set ADE_BIN."
        : "ade did not come up; see the log in the temporary directory.",
    };
  });
  // `tell` is off for the launcher's: the light is about the session.
  const pump = (
    e: Electron.IpcMainInvokeEvent,
    at: string,
    token: string,
    stop: AbortController,
    tell: boolean,
  ) =>
    void (async () => {
      // Told once either way: announcing a connection before it is
      // made is what put "lost the connection" on screen repeatedly.
      let open = false;
      let wait = 2_000;
      for (;;) {
        try {
          const res = await fetch(at, {
            headers: { "X-Ade-Token": token },
            signal: stop.signal,
          });
          if (!res.ok || !res.body) throw new Error(`events: ${res.status}`);
          if (tell) e.sender.send("stream", true);
          open = true;
          wait = 2_000;
          await events(res.body, (name) => e.sender.send("event", name));
        } catch {
          // Refused, dropped, or gone quiet: the wait below is the retry.
        }
        if (stop.signal.aborted) return;
        if (open && tell) e.sender.send("stream", false);
        open = false;
        await new Promise((done) => setTimeout(done, wait));
        // A session nothing serves refuses every time, so backing off
        // is the difference between a quiet window and a flashing one.
        wait = Math.min(wait * 2, 30_000);
        if (stop.signal.aborted) return;
      }
    })();

  // watch forwards the session's event stream: a webview expects its
  // host to carry it. In the shell, where no origin can be refused.
  let watching: AbortController | undefined;
  ipcMain.handle("ade.watch", async (e, session?: string) => {
    watching?.abort();
    const where = await found();
    if (!where || !session) return { ok: false };
    const stop = new AbortController();
    watching = stop;
    pump(e, `${where.base}/s/${session}/api/events?t=${encodeURIComponent(where.token)}`,
      where.token, stop, true);
    return { ok: true };
  });

  let listening: AbortController | undefined;
  ipcMain.handle("ade.listen", async (e) => {
    const where = await found();
    if (!where) return { ok: false };
    listening?.abort();
    const stop = new AbortController();
    listening = stop;
    pump(e, `${where.base}/api/events?t=${encodeURIComponent(where.token)}`,
      where.token, stop, false);
    return { ok: true };
  });
  // The terminal's sockets, in the shell: the server refuses a websocket
  // from a non-loopback origin, and the shell sends none. One per pane.
  const terms = new Map<string, WebSocket>();
  // The last size each pane asked for: one sent before its socket opened is sent again then.
  const sizes = new Map<string, string>();
  const closeTerm = (pane: string) => {
    terms.get(pane)?.close();
    terms.delete(pane);
  };
  ipcMain.handle("ade.term", async (e, session: string, pane: string) => {
    closeTerm(pane);
    if (!session) return { ok: true };
    const where = await found();
    if (!where) return { error: "pick a session first" };
    const at = new URL(`${where.base}/s/${session}/api/terminal`);
    at.protocol = at.protocol === "https:" ? "wss:" : "ws:";
    at.searchParams.set("t", where.token);
    at.searchParams.set("pane", pane);
    const ws = new WebSocket(at.toString());
    terms.set(pane, ws);
    // A character may be split across two chunks.
    const decoder = new StringDecoder("utf8");
    ws.on("open", () => {
      const size = sizes.get(pane);
      if (size) ws.send(size);
    });
    ws.on("message", (data: Buffer | string) => {
      e.sender.send("term", pane, typeof data === "string" ? data : decoder.write(data));
    });
    // Only the socket the pane is on: one replaced by a newer says nothing.
    ws.on("error", (why: Error) => {
      if (terms.get(pane) === ws) e.sender.send("term.closed", pane, why.message);
    });
    ws.on("close", () => {
      if (terms.get(pane) !== ws) return;
      terms.delete(pane);
      e.sender.send("term.closed", pane, "");
    });
    return { ok: true };
  });
  // Binary is a keystroke, text a control message: the server reads the frame type.
  ipcMain.on("ade.term.send", (_e, pane: string, data: string) => {
    const ws = terms.get(pane);
    if (ws?.readyState === WebSocket.OPEN) ws.send(Buffer.from(data, "utf8"), { binary: true });
  });
  ipcMain.on("ade.term.resize", (_e, pane: string, cols: number, rows: number) => {
    const size = JSON.stringify({ type: "resize", cols, rows });
    sizes.set(pane, size);
    const ws = terms.get(pane);
    if (ws?.readyState === WebSocket.OPEN) ws.send(size);
  });
  // A link in a panel leaves for the browser. Nothing else does: a
  // page of ade's never navigates the window it is in.
  ipcMain.handle("ade.link", (_e, href: string) => {
    if (/^https?:\/\//i.test(href)) void shell.openExternal(href);
  });
  ipcMain.handle("ade.keys", () => map.listed());
  ipcMain.handle("ade.world", (_e, dir?: string) => gather(dir));
  // act performs one verb from the table; this only knows how to reach
  // ade. A session verb goes through the proxy, the one address that works.
  ipcMain.handle(
    "ade.act",
    async (
      _e,
      at: "launcher" | "session",
      path: string,
      body: Record<string, unknown>,
      session?: string,
      method?: "POST" | "PUT" | "PATCH" | "DELETE",
    ) => {
      const where = await found();
      if (!where) return { error: "ade is not running" };
      if (at === "session" && !session) {
        return { error: "pick a session first; this acts on one" };
      }
      const full = at === "session" ? `/s/${session}${path}` : path;
      try {
        const got = ((await tell(where, full, body ?? {}, method)) ?? {}) as { error?: string };
        return got;
      } catch (err) {
        const why = String(err instanceof Error ? err.message : err);
        return { error: why };
      }
    },
  );

  // call is the raw one: status, ok and the body as text, because a
  // panel's relay answers in those terms and parses its own JSON.
  ipcMain.handle(
    "ade.call",
    async (
      _e,
      at: "launcher" | "session",
      path: string,
      method = "GET",
      body?: string,
      session?: string,
    ) => {
      const where = await found();
      if (!where) return { status: 0, error: "ade is not running" };
      if (at === "session" && !session) return { status: 0, error: "no session picked" };
      const full = at === "session" ? `/s/${session}${path}` : path;
      try {
        const res = await fetch(where.base + full, {
          method,
          headers: { "X-Ade-Token": where.token, "Content-Type": "application/json" },
          body,
        });
        return { status: res.status, ok: res.ok, text: await res.text() };
      } catch (err) {
        const why = String(err instanceof Error ? err.message : err);
        return { status: 0, error: why };
      }
    },
  );

  // read is the same reach, for a panel asking ade what it holds.
  ipcMain.handle(
    "ade.read",
    async (_e, at: "launcher" | "session", path: string, session?: string) => {
      const where = await found();
      if (!where) return { error: "ade is not running" };
      if (at === "session" && !session) return { error: "no session picked" };
      const full = at === "session" ? `/s/${session}${path}` : path;
      return ask(where, full).catch(async (e: Error) => ({
        error: at === "session" ? `${why(e)} ${await serving(where, session)}` : why(e),
      }));
    },
  );
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) make(map);
  });
});

app.on("window-all-closed", () => {
  if (!mac) app.quit();
});
