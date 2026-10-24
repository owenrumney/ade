import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

// xterm here, the websocket in main, ipc between them: the server
// refuses a non-loopback origin. One screen per pane id.
type Pane = {
  id: string;
  host: HTMLElement;
  term: Terminal;
  fit: FitAddon;
  on: string;
  cols: number;
  rows: number;
};

const panes = new Map<string, Pane>();
let routed = false;

// The window's own prefix: a reconnect finds the shell it left.
const WINDOW = `app-${Math.random().toString(36).slice(2, 8)}`;

export function pane(n = 0): string {
  return n === 0 ? WINDOW : `${WINDOW}-${n}`;
}

export function open(
  session: string,
  say: (why: string) => void,
  id: string = pane(),
): HTMLElement {
  route();
  const it = panes.get(id) ?? build(id);
  panes.set(id, it);
  if (!session) {
    say("pick a session: a terminal runs in a checkout");
    return it.host;
  }
  if (it.on !== session) {
    it.on = session;
    it.term.reset();
    connect(it, session, say);
  }
  queueMicrotask(() => {
    resize(it);
    it.term.focus();
  });
  return it.host;
}

// The tab went, so its socket goes with it.
export function close(id: string = pane()): void {
  const it = panes.get(id);
  if (!it) return;
  it.on = "";
  it.term.reset();
  void window.ade?.term("", id);
}

function connect(it: Pane, session: string, say: (why: string) => void): void {
  void window.ade
    ?.term(session, it.id)
    .then((got) => {
      const why = (got as { error?: string })?.error;
      if (why) {
        say(why);
        return;
      }
      resize(it, true);
    })
    .catch((e: Error) => say(e.message));
}

// force resends after a reconnect, which starts at the pty default.
function resize(it: Pane, force = false): void {
  it.fit.fit();
  const { cols, rows } = it.term;
  if (!cols || !rows) return;
  if (!force && cols === it.cols && rows === it.rows) return;
  it.cols = cols;
  it.rows = rows;
  window.ade?.termResize(it.id, cols, rows);
}

// xterm writes this into a canvas font string, where a CSS variable is
// not resolved and the whole spec is dropped. Read the value instead.
function mono(): string {
  const css = getComputedStyle(document.documentElement).getPropertyValue("--term-mono").trim();
  return css || "ui-monospace, Menlo, monospace";
}

// One listener for the window: ipcRenderer.on adds, it does not replace.
function route(): void {
  if (routed) return;
  routed = true;
  window.ade?.onTerm((id, data) => panes.get(id)?.term.write(data));
  window.ade?.onTermClosed((id, why) => {
    const it = panes.get(id);
    if (!it) return;
    it.on = "";
    if (why) it.term.write(`\r\n\x1b[31m${why}\x1b[0m\r\n`);
  });
}

function build(id: string): Pane {
  const host = document.createElement("div");
  host.className = "term";
  const term = new Terminal({
    fontFamily: mono(),
    fontSize: 13,
    cursorBlink: true,
    // Ayu Dark Unbordered, the same values the window uses.
    theme: {
      background: "#0d1017",
      foreground: "#bfbdb6",
      cursor: "#e6b450",
      selectionBackground: "#47526640",
      black: "#1b1f29",
      red: "#f26d78",
      green: "#aad94c",
      yellow: "#e6b450",
      blue: "#73b8ff",
      magenta: "#d2a6ff",
      cyan: "#95e6cb",
      white: "#bfbdb6",
    },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(host);
  const it: Pane = { id, host, term, fit, on: "", cols: 0, rows: 0 };
  term.onData((data) => window.ade?.termSend(id, data));
  new ResizeObserver(() => resize(it)).observe(host);
  return it;
}
