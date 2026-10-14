import * as vscode from "vscode";
import type { Server } from "./discover";
import { closeURL, paneId, socketURL } from "./pane";
import { BINARY, TEXT, WS } from "./ws";

// Shell is one of ade's shells as a VS Code terminal. The pty is the Go
// binary's; this only carries bytes, which is what ExtensionTerminal is for.
export class Shell implements vscode.Pseudoterminal {
  private write = new vscode.EventEmitter<string>();
  private closed = new vscode.EventEmitter<number | void>();
  private socket?: WS;
  private size?: { cols: number; rows: number };
  private retry?: NodeJS.Timeout;
  private gone = false;
  // A chunk boundary falls wherever the socket put it, which is happily in the
  // middle of a multi-byte character. The decoder holds the tail.
  private text = new TextDecoder("utf-8");

  readonly onDidWrite = this.write.event;
  readonly onDidClose = this.closed.event;

  constructor(
    private server: Server,
    private pane: string,
  ) {}

  open(size?: vscode.TerminalDimensions): void {
    if (size) this.size = { cols: size.columns, rows: size.rows };
    this.dial();
  }

  close(): void {
    this.gone = true;
    if (this.retry) clearTimeout(this.retry);
    this.socket?.close();
    void fetch(closeURL(this.server, this.pane), {
      method: "POST",
      headers: { "X-Ade-Token": this.server.token },
    }).catch(() => undefined);
  }

  handleInput(data: string): void {
    this.socket?.send(BINARY, Buffer.from(data, "utf8"));
  }

  setDimensions(size: vscode.TerminalDimensions): void {
    this.size = { cols: size.columns, rows: size.rows };
    this.resize();
  }

  private dial(): void {
    this.text = new TextDecoder("utf-8");
    const socket = new WS(socketURL(this.server, this.pane), {
      open: () => this.resize(),
      data: (chunk) =>
        this.write.fire(this.text.decode(chunk, { stream: true })),
      close: (why, fatal) => this.dropped(why, fatal),
    });
    this.socket = socket;
    socket.open();
  }

  // The shell outlives the socket, so reconnecting picks the same one up and
  // replays it. The wait keeps a downed server from being hammered.
  private dropped(why: string, fatal: boolean): void {
    if (this.gone) return;
    if (fatal) {
      this.write.fire(`\r\n\x1b[2m${why}\x1b[0m\r\n`);
      this.closed.fire();
      return;
    }
    this.write.fire(`\r\n\x1b[2m${why}; reconnecting\x1b[0m\r\n`);
    this.retry = setTimeout(() => !this.gone && this.dial(), 1000);
  }

  private resize(): void {
    if (!this.size) return;
    const say = { type: "resize", cols: this.size.cols, rows: this.size.rows };
    this.socket?.send(TEXT, Buffer.from(JSON.stringify(say), "utf8"));
  }
}

export function openTerminal(server: Server): vscode.Terminal {
  const term = vscode.window.createTerminal({
    name: "ade",
    pty: new Shell(server, paneId()),
    iconPath: new vscode.ThemeIcon("terminal"),
  });
  term.show();
  return term;
}
