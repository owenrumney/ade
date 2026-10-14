import type * as vscode from "vscode";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Chat } from "./chat";

const server = { base: "http://127.0.0.1:1", token: "t", root: "/r" };

// A webview that records what the editor posts to it and lets the test play
// the page's ready message back.
function page() {
  const sent: unknown[] = [];
  let heard: ((said: unknown) => void) | undefined;
  const webview = {
    options: {},
    html: "",
    cspSource: "vscode-resource:",
    asWebviewUri: (uri: vscode.Uri) => uri,
    onDidReceiveMessage: (fn: (said: unknown) => void) => {
      heard = fn;
      return { dispose() {} };
    },
    postMessage: async (said: unknown) => {
      sent.push(said);
      return true;
    },
  };
  return { webview, sent, ready: () => heard?.({ kind: "ready" }) };
}

function panel(of: ReturnType<typeof page>) {
  return {
    webview: of.webview,
    onDidDispose: () => ({ dispose() {} }),
    reveal: () => {},
    dispose: () => {},
  };
}

// The stream never delivers: the test only cares who is subscribed to it.
function quiet() {
  const dialled = vi.fn(async () => ({
    body: { getReader: () => ({ read: () => new Promise<never>(() => undefined), cancel: async () => {} }) },
  }));
  vi.stubGlobal("fetch", dialled);
  return dialled;
}

afterEach(() => vi.unstubAllGlobals());

describe("the chat event stream", () => {
  // The view and the panel are both open at once. A stream owned by whichever
  // page said ready last left the other stale until a prompt refetched it.
  it("reaches a second page without cutting off the first", async () => {
    const dialled = quiet();
    const chat = new Chat({
      extensionUri: { scheme: "file", fsPath: "/ext" },
      globalState: { get: () => undefined, update: async () => undefined },
    } as never);
    chat.serving(server as never);

    const first = page();
    chat.revive(panel(first) as never);
    first.ready();
    await Promise.resolve();

    const second = page();
    chat.resolveWebviewView({
      webview: second.webview,
      onDidDispose: () => ({ dispose() {} }),
      onDidChangeVisibility: () => ({ dispose() {} }),
      visible: true,
    } as never);
    second.ready();
    await Promise.resolve();

    expect(second.sent).toContainEqual({ kind: "stream", open: true });
    expect(first.sent).not.toContainEqual({ kind: "stream", open: false });
    expect(dialled).toHaveBeenCalledTimes(1);
  });
});
