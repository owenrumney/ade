import { app, BrowserWindow, Menu, ipcMain, shell } from "electron";
import { readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { Keymap, merge, shared, type Binding } from "./keymap";
import { ask, found, tell, type At } from "./launcher";
import { events } from "../../vscode/src/launcher";
import { declare, hold, serve } from "./webview";
import { launcherArgs, start } from "../../vscode/src/start";

// Before whenReady: a privileged scheme cannot be declared after it.
declare();

const mac = process.platform === "darwin";
// What the window is called: the menu bar on a Mac, and the directory it
// keeps its state in. "ADE" is the fork's app while that still exists.
const NAME = "ADE Window";

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
    titleBarStyle: mac ? "hiddenInset" : "hidden",
    trafficLightPosition: { x: 14, y: 12 },
    backgroundColor: "#1f1f1f",
    webPreferences: {
      preload: join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  void win.loadFile(join(__dirname, "..", "renderer", "index.html"));
  win.once("ready-to-show", () => win.show());

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

// ade's own menu, so no stock accelerator is registered behind its
// back. Edit and Window keep their roles; their keys go through first.
function menu(): void {
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(mac ? [{ role: "appMenu" as const }] : []),
      { role: "editMenu" },
      { role: "windowMenu" },
    ]),
  );
}

// run is one command, its output or its complaint. No shell: the
// arguments are a list, so a path with a space in it is one argument.
function run(cmd: string, args: string[]): Promise<string> {
  return new Promise((done, fail) => {
    execFile(cmd, args, { maxBuffer: 32 << 20 }, (err, out, bad) => {
      if (err) fail(new Error((bad || err.message).trim().split("\n")[0]));
      else done(out);
    });
  });
}

// why is what a failed read says. A 404 from the launcher is almost
// always age: it predates the routes this branch added.
function why(e: Error): string {
  // 404 is a route the server does not have; 405 is one it has under
  // another method. Both mean the ade answering is not this build.
  return /\b(404|405)\b/.test(e.message)
    ? `${e.message} — this ade is older than the window. Reinstall it and restart.`
    : e.message;
}

// gather is one round trip for everything the sidebar draws; each part
// may fail on its own. `dir` is the checkout the window is on.
async function gather(dir = ""): Promise<unknown> {
  const where = await found();
  if (!where) {
    return { offline: "ade is not running. Start it from a checkout." };
  }
  // A part that fails says so by name. Swallowing it draws an empty list,
  // which reads as ade having nothing rather than as a broken read.
  const trouble: Record<string, string> = {};
  const [dash, designs, repos, hosts, todos, jobs, active] = await Promise.all([
    ask(where, "/api/dash").catch((e: Error) => ({ error: e.message })),
    ask<{ designs?: unknown[] }>(where, "/api/designs/all")
      .then((d) => d.designs ?? [])
      .catch((e: Error) => {
        trouble.designs = why(e);
        return [];
      }),
    ask<{ repos?: unknown[] }>(where, "/api/repos")
      .then((r) => r.repos ?? [])
      .catch((e: Error) => {
        trouble.sessions = why(e);
        return [];
      }),
    // The machines already connected, and what they have checked out.
    // Nothing is dialled for this.
    ask<{ hosts?: unknown[] }>(where, "/api/hosts/repos")
      .then((h) => h.hosts ?? [])
      .catch(() => []),
    ask<{ items?: unknown[] }>(
      where,
      dir ? `/api/todos?dir=${encodeURIComponent(dir)}` : "/api/todos",
    )
      .then((t) => t.items ?? [])
      .catch((e: Error) => {
        trouble.todos = why(e);
        return [];
      }),
    ask<{ jobs?: unknown[] }>(where, "/api/jobs")
      .then((j) => j.jobs ?? [])
      .catch((e: Error) => {
        trouble.jobs = why(e);
        return [];
      }),
    // Every announced session and what its agent is doing, this machine
    // and every host the launcher is connected to.
    ask<unknown[]>(where, "/api/serving")
      .then((a) => a ?? [])
      .catch((e: Error) => {
        trouble.agents = why(e);
        return [];
      }),
  ]);
  return { dash, designs, repos, hosts, todos, jobs, active, trouble };
}

void app.whenReady().then(() => {
  const map = keys();
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
  // watch forwards the session's event stream: a webview expects its
  // host to carry it. In the shell, where no origin can be refused.
  let watching: AbortController | undefined;
  ipcMain.handle("ade.watch", async (e, session?: string) => {
    watching?.abort();
    const where = await found();
    if (!where || !session) return { ok: false };
    const stop = new AbortController();
    watching = stop;
    const at = `${where.base}/s/${session}/api/events?t=${encodeURIComponent(where.token)}`;
    void (async () => {
      // Told once either way: announcing a connection before it is
      // made is what put "lost the connection" on screen repeatedly.
      let open = false;
      let wait = 2_000;
      for (;;) {
        try {
          const res = await fetch(at, {
            headers: { "X-Ade-Token": where.token },
            signal: stop.signal,
          });
          if (!res.ok || !res.body) throw new Error(`events: ${res.status}`);
          e.sender.send("stream", true);
          open = true;
          wait = 2_000;
          await events(res.body, (name) => e.sender.send("event", name));
        } catch {
          // Refused, dropped, or gone quiet: the wait below is the retry.
        }
        if (stop.signal.aborted) return;
        if (open) e.sender.send("stream", false);
        open = false;
        await new Promise((done) => setTimeout(done, wait));
        // A session nothing serves refuses every time, so backing off
        // is the difference between a quiet window and a flashing one.
        wait = Math.min(wait * 2, 30_000);
        if (stop.signal.aborted) return;
      }
    })();
    return { ok: true };
  });
  // The terminal's socket, in the shell: the server refuses a websocket
  // from an origin that is not loopback, and the shell sends none.
  let shell: WebSocket | undefined;
  ipcMain.handle("ade.term", async (e, session: string, pane: string) => {
    const where = await found();
    if (!where || !session) return { error: "pick a session first" };
    shell?.close();
    const at = new URL(`${where.base}/s/${session}/api/terminal`);
    at.protocol = at.protocol === "https:" ? "wss:" : "ws:";
    at.searchParams.set("t", where.token);
    at.searchParams.set("pane", pane);
    const ws = new WebSocket(at.toString());
    ws.binaryType = "arraybuffer";
    shell = ws;
    ws.onmessage = (m) => {
      const data = m.data as ArrayBuffer | string;
      e.sender.send("term", typeof data === "string" ? data : Buffer.from(data).toString("utf8"));
    };
    ws.onclose = () => e.sender.send("term.closed", true);
    return { ok: true };
  });
  ipcMain.on("ade.term.send", (_e, data: string) => shell?.send(data));
  // float is a second window holding one panel, which is what the
  // concierge wants: over the work rather than in the editor area.
  ipcMain.handle("ade.float", (_e, panel: string, session: string) => {
    const win = new BrowserWindow({
      width: 420,
      height: 640,
      alwaysOnTop: true,
      title: "ade",
      backgroundColor: "#0d1017",
      webPreferences: {
        preload: join(__dirname, "preload.js"),
        contextIsolation: true,
        nodeIntegration: false,
      },
    });
    void win.loadFile(join(__dirname, "..", "renderer", "index.html"), {
      query: { only: panel, session },
    });
    // The keyboard is the shell's here too, or ⌘K in the float would
    // be whatever the page decided.
    win.webContents.on("before-input-event", (event, input) => {
      const command = map.match(input);
      if (!command) return;
      if (!shared(command)) event.preventDefault();
      win.webContents.send("command", command);
    });
    return { ok: true };
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
      method?: "POST" | "PUT" | "DELETE",
    ) => {
      const where = await found();
      if (!where) return { error: "ade is not running" };
      if (at === "session" && !session) {
        return { error: "pick a session first; this acts on one" };
      }
      const full = at === "session" ? `/s/${session}${path}` : path;
      try {
        return (await tell(where, full, body ?? {}, method)) ?? {};
      } catch (err) {
        return { error: String(err instanceof Error ? err.message : err) };
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
        return { status: 0, error: String(err instanceof Error ? err.message : err) };
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
  ipcMain.handle("ade.pr", async (_e, repo: string, number: number) => {
    const where = await found();
    if (!where) return { error: "ade is not running" };
    const at = `/api/pr?repo=${encodeURIComponent(repo)}&number=${number}`;
    return ask(where, at).catch((e: Error) => ({ error: why(e) }));
  });
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) make(map);
  });
});

app.on("window-all-closed", () => {
  if (!mac) app.quit();
});
