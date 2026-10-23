import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

// A terminal in the window: xterm here, the websocket in main, ipc
// between them, because the server refuses a non-loopback origin.

// One instance across draws: one rebuilt per read reconnects per read.

let made: Terminal | undefined;
let fit: FitAddon | undefined;
let host: HTMLElement | undefined;
let on = "";

// The pane names this window's shell, so a reconnect finds the one it
// left rather than starting another beside it.
const PANE = `app-${Math.random().toString(36).slice(2, 8)}`;

export function pane(): string {
  return PANE;
}

// open is the terminal, connected to the session if it is not already.
export function open(session: string, say: (why: string) => void): HTMLElement {
  if (!host) host = build();
  if (!session) {
    say("pick a session: a terminal runs in a checkout");
    return host;
  }
  if (on !== session) {
    on = session;
    made?.reset();
    void window.ade
      ?.term(session, PANE)
      .then((got) => {
        const why = (got as { error?: string })?.error;
        if (why) say(why);
      })
      .catch((e: Error) => say(e.message));
  }
  queueMicrotask(() => fit?.fit());
  return host;
}

function build(): HTMLElement {
  const el = document.createElement("div");
  el.className = "term";
  made = new Terminal({
    fontFamily: "var(--mono)",
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
  fit = new FitAddon();
  made.loadAddon(fit);
  made.open(el);
  made.onData((data) => window.ade?.termSend(data));
  window.ade?.onTerm((data) => made?.write(data));
  new ResizeObserver(() => fit?.fit()).observe(el);
  return el;
}

// close drops the connection. The tab went, so the shell's socket
// should go with it rather than sit open until the window does.
export function close(): void {
  on = "";
  made?.reset();
  void window.ade?.term("", PANE);
}
