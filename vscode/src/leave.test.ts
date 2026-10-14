import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { forceable, here, keeping, mainOf, PARTINGS, removable } from "./leave";
import { GROUPS } from "./menu";
import { ROW_ONLY } from "./menu";
import type { Tree } from "./client";

const extension = readFileSync(join(__dirname, "extension.ts"), "utf8");
const page = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");
const chat = readFileSync(join(__dirname, "chat.ts"), "utf8");
const manifest = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as { contributes: { commands: { command: string; title: string }[] } };

const tree = (over: Partial<Tree>): Tree => ({
  path: "/code/ade-pr-12",
  name: "pr-12",
  kind: "worktree",
  removable: true,
  ...over,
});

const trees = [tree({ path: "/code/ade", name: "ade", main: true }), tree({})];

describe("closing a session", () => {
  it("can remove the worktree it is in, but never the repository itself", () => {
    expect(removable(trees, "/code/ade-pr-12")).toBe(true);
    expect(removable(trees, "/code/ade")).toBe(false);
    expect(removable(trees, "/code/elsewhere")).toBe(false);
  });

  it("finds the checkout the launcher prunes against", () => {
    expect(mainOf(trees)?.path).toBe("/code/ade");
    expect(here(trees, "/code/ade-pr-12")?.name).toBe("pr-12");
  });

  it("says what is in the way, counted", () => {
    expect(keeping(tree({ uncommitted: 2 }))).toBe("2 uncommitted changes");
    expect(keeping(tree({ unpushed: 1 }))).toBe("1 unpushed commit");
    expect(keeping(tree({}))).toBe("");
  });

  // Serving is not a reason to keep it: the server is about to stop.
  it("ignores the server holding it open", () => {
    expect(keeping(tree({ serving: "http://127.0.0.1:1234" }))).toBe("");
  });

  it("lets unpushed commits be overridden and uncommitted work not", () => {
    expect(forceable(tree({ unpushed: 3 }))).toBe(true);
    expect(forceable(tree({ unpushed: 3, uncommitted: 1 }))).toBe(false);
    expect(forceable(tree({}))).toBe(false);
  });
});

// /api/worktrees lists the linked checkouts only, so a repo path taken from it
// is always undefined and the worktree is never offered.
describe("where the repository path comes from", () => {
  const close = extension.slice(
    extension.indexOf("async function closeSession"),
  );
  const body = close.slice(0, close.indexOf("\n}\n"));

  it("is the switch list, which carries the main checkout", () => {
    expect(body).toContain("mainOf(switchable.worktrees)");
    expect(body).not.toContain("mainOf(trees)");
  });
});

describe("exiting a session from the ade palette", () => {
  it("is a row in the Sessions group, not hidden to tree rows", () => {
    const sessions = GROUPS.find((g) => g.title === "Sessions");
    expect(sessions?.commands).toContain("ade.closeSession");
    expect(ROW_ONLY).not.toContain("ade.closeSession");
  });

  it("is titled as the thing it does", () => {
    const cmd = manifest.contributes.commands.find(
      (c) => c.command === "ade.closeSession",
    );
    expect(cmd?.title).toBe("Exit this session");
  });

  it("offers keep, archive and delete, which is what /api/exit takes", () => {
    expect(PARTINGS.map((p) => p.id)).toEqual(["keep", "archive", "delete"]);
  });
});

// The server answers /api/exit and stops 200ms later, so the next poll failed.
describe("what the chat says when the session was closed on purpose", () => {
  it("is told before the server goes", () => {
    const close = extension.slice(extension.indexOf("async function closeSession"));
    const body = close.slice(0, close.indexOf("\n}\n"));
    expect(body).toContain("chat.closing()");
    expect(body.indexOf("client.exit")).toBeLessThan(
      body.indexOf("chat.closing()"),
    );
  });

  it("aborts the event stream rather than letting it reconnect", () => {
    const closing = chat.slice(chat.indexOf("  closing(): void {"));
    expect(closing.slice(0, closing.indexOf("\n  }"))).toContain(
      "this.watching?.abort()",
    );
  });

  it("stops polling and says so instead of showing the failure", () => {
    expect(page).toContain('say("Session closed.")');
    expect(page).toContain("if (closed) return;");
    expect(page).toContain("if (!closed) say(String(err));");
  });

  it("clears the flag when a server comes back", () => {
    expect(chat).toContain("if (server) this.closed = false;");
  });
});

// Staying attached to a stopped server is what made the next action report a
// fetch error rather than starting anything.
describe("what the window keeps after the session is closed", () => {
  const body = (from: string): string => {
    const at = extension.slice(extension.indexOf(from));
    return at.slice(0, at.indexOf("\n}\n"));
  };

  it("drops the client and the server it was reached at", () => {
    const tear = body("function teardown(): void {");
    expect(tear).toContain("live = undefined;");
    expect(tear).toContain("client = undefined;");
  });

  it("closes anyway when the exit request cannot reach the server", () => {
    const close = body("async function closeSession");
    expect(close).toContain("err instanceof TypeError");
    expect(close.indexOf("err instanceof TypeError")).toBeLessThan(
      close.indexOf("teardown()"),
    );
  });
});
