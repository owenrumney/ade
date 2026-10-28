import { contextBridge, ipcRenderer } from "electron";

// The bridge: the shell owns the keyboard and reaching ade, the window
// draws. The token is in a file only this user reads.
contextBridge.exposeInMainWorld("ade", {
  onCommand: (then: (command: string) => void) =>
    ipcRenderer.on("command", (_event, command: string) => then(command)),
  keys: () => ipcRenderer.invoke("ade.keys") as Promise<unknown[]>,
  // What the person just set off. The reach records what came of it.
  // Never thrown: the trail may not be the reason the window stops.
  note: (entry: Record<string, unknown>) =>
    void (ipcRenderer.invoke("ade.note", entry) as Promise<void>).catch(() => {}),
  audit: (day?: string) => ipcRenderer.invoke("ade.audit", day) as Promise<unknown>,
  // world is everything the sidebar draws, gathered in one round trip.
  world: (dir?: string) => ipcRenderer.invoke("ade.world", dir) as Promise<unknown>,
  pr: (repo: string, number: number) =>
    ipcRenderer.invoke("ade.pr", repo, number) as Promise<unknown>,
  start: () => ipcRenderer.invoke("ade.start") as Promise<unknown>,
  // The picked session's event stream, carried to the window.
  watch: (session?: string) => ipcRenderer.invoke("ade.watch", session) as Promise<unknown>,
  listen: () => ipcRenderer.invoke("ade.listen") as Promise<unknown>,
  onEvent: (then: (name: string) => void) =>
    ipcRenderer.on("event", (_e, name: string) => then(name)),
  onStream: (then: (open: boolean) => void) =>
    ipcRenderer.on("stream", (_e, open: boolean) => then(open)),
  // The terminal: its socket is the shell's, the screen is the page's.
  term: (session: string, pane: string) =>
    ipcRenderer.invoke("ade.term", session, pane) as Promise<unknown>,
  termSend: (pane: string, data: string) => ipcRenderer.send("ade.term.send", pane, data),
  termResize: (pane: string, cols: number, rows: number) =>
    ipcRenderer.send("ade.term.resize", pane, cols, rows),
  // A window of its own, holding one panel.
  link: (href: string) => ipcRenderer.invoke("ade.link", href) as Promise<unknown>,
  float: (panel: string, session: string) =>
    ipcRenderer.invoke("ade.float", panel, session) as Promise<unknown>,
  onTerm: (then: (pane: string, data: string) => void) =>
    ipcRenderer.on("term", (_e, pane: string, data: string) => then(pane, data)),
  onTermClosed: (then: (pane: string, why: string) => void) =>
    ipcRenderer.on("term.closed", (_e, pane: string, why: string) => then(pane, why)),
  act: (
    at: "launcher" | "session",
    path: string,
    body: Record<string, unknown>,
    session?: string,
    method?: "POST" | "PUT" | "PATCH" | "DELETE",
  ) => ipcRenderer.invoke("ade.act", at, path, body, session, method) as Promise<unknown>,
  call: (
    at: "launcher" | "session",
    path: string,
    method?: string,
    body?: string,
    session?: string,
  ) => ipcRenderer.invoke("ade.call", at, path, method, body, session) as Promise<unknown>,
  read: (at: "launcher" | "session", path: string, session?: string) =>
    ipcRenderer.invoke("ade.read", at, path, session) as Promise<unknown>,
  // A panel's page, held on the webview scheme; the answer is its url.
  panel: (id: string, html: string) =>
    ipcRenderer.invoke("ade.panel", id, html) as Promise<string>,
  where: () =>
    ipcRenderer.invoke("ade.where") as Promise<{ base: string; token: string } | null>,
  opened: () => ipcRenderer.invoke("ade.opened") as Promise<string>,
});
