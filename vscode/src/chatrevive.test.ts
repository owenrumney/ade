import * as vscode from "vscode";
import { describe, expect, it, vi } from "vitest";
import { Chat } from "./chat";

// What VS Code hands back for a panel it restored across a reload.
function restored() {
  const webview = {
    options: {},
    html: "",
    cspSource: "vscode-resource:",
    asWebviewUri: (uri: vscode.Uri) => uri,
    onDidReceiveMessage: () => ({ dispose() {} }),
    postMessage: async () => true,
  };
  return {
    webview,
    onDidDispose: () => ({ dispose() {} }),
    reveal: () => {},
    dispose: () => {},
  };
}

describe("a chat panel VS Code restored", () => {
  it("is drawn, rather than left as the blank tab it came back as", () => {
    const chat = new Chat({
      extensionUri: { scheme: "file", fsPath: "/ext" },
    } as never);

    const panel = restored();
    chat.revive(panel as never);

    expect(panel.webview.html).not.toBe("");
    expect(panel.webview.options).toMatchObject({ enableScripts: true });
  });
});

// The conversation belongs in its own container. A panel fallback meant a
// window that had never opened the container got a tab instead, for good:
// the serializer brought it back on every reload.
describe("revealing the chat", () => {
  it("focuses the view rather than opening an editor tab", async () => {
    const ran = vi.spyOn(vscode.commands, "executeCommand").mockResolvedValue(undefined);
    const made = vi.spyOn(vscode.window, "createWebviewPanel");

    const chat = new Chat({
      extensionUri: { scheme: "file", fsPath: "/ext" },
    } as never);
    await chat.reveal({ base: "http://127.0.0.1:1", token: "t", root: "/r" } as never);

    expect(ran).toHaveBeenCalledWith("ade.chat.focus");
    expect(made).not.toHaveBeenCalled();
    ran.mockRestore();
    made.mockRestore();
  });
});
