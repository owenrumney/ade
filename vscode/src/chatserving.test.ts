import type * as vscode from "vscode";
import { describe, expect, it } from "vitest";
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

// A pick moves the chat to another session's server. The page fetches from
// the base written into it, so a new base is a new page, not a new token.
describe("serving the chat another session", () => {
  it("redraws the page on the new base", () => {
    const chat = new Chat({
      extensionUri: { scheme: "file", fsPath: "/ext" },
    } as never);
    const panel = restored();
    chat.revive(panel as never);

    chat.serving({ base: "http://127.0.0.1:1", token: "a", root: "/w/a" });
    expect(panel.webview.html).toContain('data-base="http://127.0.0.1:1"');

    chat.serving({ base: "http://127.0.0.1:2", token: "b", root: "/w/b" });
    expect(panel.webview.html).toContain('data-base="http://127.0.0.1:2"');
  });
});
