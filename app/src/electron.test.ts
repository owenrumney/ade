import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// A wrong Electron method name fails when the app loads, before any
// window is drawn, and the message names a line in a bundle. So every
// name the shell calls is checked against the Electron that is
// installed. `registerSchemeAsPrivileged` cost a round trip; it is
// `registerSchemesAsPrivileged`, plural, and takes a list.
const here = join(__dirname);
const types = join(here, "..", "node_modules", "electron", "electron.d.ts");

function sources(): string {
  return ["main.ts", "webview.ts", "preload.ts"]
    .map((f) => readFileSync(join(here, f), "utf8"))
    .join("\n");
}

describe("the Electron the shell calls", () => {
  it("has every method the shell uses", () => {
    let d: string;
    try {
      d = readFileSync(types, "utf8");
    } catch {
      // No Electron installed here: `make app-deps` has not run, and
      // the window cannot start anyway. Not this test's business.
      return;
    }
    const used = [
      ...sources().matchAll(
        /\b(protocol|app|ipcMain|ipcRenderer|Menu|net|BrowserWindow|contextBridge|globalShortcut|nativeImage)\.([a-zA-Z]+)\(/g,
      ),
    ].map((m) => ({ on: m[1], name: m[2] }));

    const missing = [...new Set(used.map((u) => `${u.on}.${u.name}`))].filter((pair) => {
      const name = pair.split(".")[1];
      return !new RegExp(`\\b${name}[(<:]`).test(d);
    });
    expect(missing, `not in this Electron: ${missing.join(", ")}`).toEqual([]);
  });

  it("declares the webview scheme before the app is ready", () => {
    const main = readFileSync(join(here, "main.ts"), "utf8");
    // A privileged scheme registered after `ready` is ignored, and the
    // panels then cannot reach ade.
    expect(main.indexOf("declare();")).toBeLessThan(main.indexOf("app.whenReady"));
  });

  // An unpackaged run has no bundle, so it showed electron's own icon in
  // the dock and ade's when installed. See `new session.md`.
  it("has the dock icon an unpackaged run reads", () => {
    const png = readFileSync(join(here, "..", "icon", "ade.png"));
    expect(png.subarray(0, 8)).toEqual(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
    expect(png.readUInt32BE(16)).toBeGreaterThanOrEqual(512);
  });
});
