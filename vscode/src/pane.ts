import type { Server } from "./discover";

// Panes are named so a reconnect finds the shell it left and replays it. The
// pid keeps two windows on one checkout from sharing a prompt.
let made = 0;

export function paneId(): string {
  made += 1;
  return `vscode-${process.pid}-${made}`;
}

export function socketURL(server: Server, pane: string): string {
  return termURL(server, "/api/terminal", pane);
}

export function closeURL(server: Server, pane: string): string {
  return termURL(server, "/api/terminal/close", pane);
}

function termURL(server: Server, path: string, pane: string): string {
  const at = new URL(server.base);
  at.pathname = path;
  at.searchParams.set("t", server.token);
  at.searchParams.set("pane", pane);
  return at.toString();
}
