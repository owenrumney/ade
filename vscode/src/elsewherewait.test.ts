import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const chat = readFileSync(join(__dirname, "chat.ts"), "utf8");
const src = readFileSync(join(__dirname, "extension.ts"), "utf8");

describe("a window on another machine with no ade yet", () => {
  it("sets the key the panes are gated on", () => {
    expect(src).toContain('"ade.elsewhere"');
    expect(src).toContain("machineWide(vscode.env.remoteName)");
  });

  it("says the same in the conversation pane", () => {
    const off = chat.slice(chat.indexOf("function offline()"));
    expect(off).toContain("machineWide");
  });

  // Up to a poll of staring at an empty pane, for a record that is already
  // there, is the whole of what start-up feels like on another machine.
  it("connects when the record appears rather than on the next poll", () => {
    expect(src).toContain("createFileSystemWatcher");
    const watcher = src.slice(src.indexOf("function watchRecord"));
    expect(watcher).toContain("ade/server.json");
    expect(watcher).toContain("onDidCreate");
  });
});
