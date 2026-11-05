import { describe, expect, it, vi } from "vitest";

const opened: string[] = [];
vi.mock("electron", () => ({
  shell: { openExternal: (url: string) => void opened.push(url) },
}));

const { away, outward } = await import("./away");

type Handler = (details: { url: string; preventDefault: () => void }) => void;

function fake() {
  const on: Record<string, Handler> = {};
  let opener: ((d: { url: string }) => unknown) | undefined;
  return {
    win: {
      webContents: {
        on: (event: string, fn: Handler) => void (on[event] = fn),
        setWindowOpenHandler: (fn: (d: { url: string }) => unknown) => void (opener = fn),
      },
    },
    navigate: (url: string) => {
      let stopped = false;
      on["will-frame-navigate"]({ url, preventDefault: () => (stopped = true) });
      return stopped;
    },
    open: (url: string) => opener!({ url }),
  };
}

describe("a link in the window", () => {
  it("knows the browser's from ade's own", () => {
    expect(outward("https://example.com")).toBe(true);
    expect(outward("http://127.0.0.1:4000/x")).toBe(true);
    expect(outward("ade-webview://panel/settings")).toBe(false);
    expect(outward("file:///Applications/ADE.app/renderer/index.html")).toBe(false);
  });

  it("leaves for the browser instead of navigating the window", () => {
    opened.length = 0;
    const it = fake();
    away(it.win as never);
    expect(it.navigate("https://github.com/x/y/pull/1")).toBe(true);
    expect(opened).toEqual(["https://github.com/x/y/pull/1"]);
  });

  it("lets ade's own pages navigate", () => {
    opened.length = 0;
    const it = fake();
    away(it.win as never);
    expect(it.navigate("ade-webview://panel/settings")).toBe(false);
    expect(opened).toEqual([]);
  });

  it("opens a new-window link in the browser and never in a window", () => {
    opened.length = 0;
    const it = fake();
    away(it.win as never);
    expect(it.open("https://example.com")).toEqual({ action: "deny" });
    expect(opened).toEqual(["https://example.com"]);
  });
});
