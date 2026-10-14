import { describe, expect, it } from "vitest";
import { closeURL, paneId, socketURL } from "./pane";
import type { Server } from "./discover";

const server: Server = {
  base: "http://127.0.0.1:20829",
  token: "a b/c",
  root: "/repo",
};

describe("pane urls", () => {
  it("carries the token, because a websocket has no header to put it in", () => {
    const at = new URL(socketURL(server, "vscode-1-1"));
    expect(at.pathname).toBe("/api/terminal");
    expect(at.searchParams.get("t")).toBe("a b/c");
    expect(at.searchParams.get("pane")).toBe("vscode-1-1");
  });

  it("closes the pane it opened", () => {
    expect(new URL(closeURL(server, "vscode-1-1")).pathname).toBe(
      "/api/terminal/close",
    );
  });

  it("names a pane the way the server will accept", () => {
    // paneKey refuses anything outside [A-Za-z0-9_-]{1,64}.
    for (let i = 0; i < 3; i++)
      expect(paneId()).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
  });

  it("never gives two terminals the same shell", () => {
    expect(paneId()).not.toBe(paneId());
  });
});
