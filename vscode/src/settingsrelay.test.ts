import type * as vscode from "vscode";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Settings } from "./settings";

type Said = { kind?: string; id?: string; view?: unknown; error?: string };

function panelWith() {
  const posted: Said[] = [];
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
    postMessage: async (said: Said) => {
      posted.push(said);
      return true;
    },
  };
  const panel = {
    webview,
    onDidDispose: () => ({ dispose() {} }),
    reveal: () => {},
    dispose: () => {},
    viewColumn: 1,
  };
  return { panel, posted, say: (said: unknown) => heard?.(said) };
}

const server = { base: "http://127.0.0.1:1/s/x", token: "s3cr3t-token", root: "/r" };
const settled = () => new Promise((ok) => setTimeout(ok, 0));

afterEach(() => vi.unstubAllGlobals());

describe("the settings page's requests", () => {
  // A webview renders on the machine you are sitting at. In a remote window
  // that is not the machine ade is on, so the page cannot fetch it itself.
  it("are made by the editor and answered back to the page", async () => {
    vi.stubGlobal("fetch", async () => ({
      ok: true,
      status: 200,
      json: async () => ({ path: "/srv/.ade/settings.json" }),
    }));
    const { panel, posted, say } = panelWith();
    const page = new Settings({ extensionUri: { scheme: "file", fsPath: "/ext" } } as never);
    page.revive(panel as never, server as never);

    say({ kind: "api", id: "1", path: "/api/settings" });
    await settled();

    const answer = posted.find((p) => p.kind === "api.result");
    expect(answer?.view).toMatchObject({ path: "/srv/.ade/settings.json" });
  });

  it("report a refusal rather than leaving the form looking empty", async () => {
    vi.stubGlobal("fetch", async () => {
      throw new Error("connect ECONNREFUSED");
    });
    const { panel, posted, say } = panelWith();
    const page = new Settings({ extensionUri: { scheme: "file", fsPath: "/ext" } } as never);
    page.revive(panel as never, server as never);

    say({ kind: "api", id: "1", path: "/api/settings" });
    await settled();

    const answer = posted.find((p) => p.kind === "api.result");
    expect(answer?.error).toContain("ECONNREFUSED");
  });

  // The token is the whole of the authorisation, and a webview's html is
  // readable from any window's devtools.
  it("never hand the page the token", async () => {
    const { panel, posted, say } = panelWith();
    const page = new Settings({ extensionUri: { scheme: "file", fsPath: "/ext" } } as never);
    page.revive(panel as never, server as never);
    say({ kind: "ready" });
    await settled();

    expect(JSON.stringify(posted)).not.toContain("t");
    expect(panel.webview.html).not.toContain(server.token);
    expect(posted.some((p) => p.kind === "serving")).toBe(true);
  });
});
