// Opens the window headless, checks the page drew and the bridge arrived,
// then quits. It is the one test that proves the three bundles load
// together under a real Electron rather than under a bundler.
const { app, BrowserWindow, net } = require("electron");
const { join } = require("node:path");
const { readdirSync } = require("node:fs");
const { Keymap, merge } = require("./dist/keymap.js");
const { declare, hold, serve } = require("./dist/webview.js");

app.disableHardwareAcceleration();
// Privileged schemes are declared before the app is ready, as in main.
declare();

const mac = process.platform === "darwin";

// This window has no ipc handlers behind it, so `ade.world` rejects and
// the renderer draws what it draws with no launcher: one place, holding
// the line that says so. That it got that far is the point.
const want = {
  // The one place an offline window has, and the terminal button beside it.
  places: 2,
  rows: 1,
  offline: true,
  bridge: "object",
  panelsAreReachable: true,
};

// A panel's page is served from the webview scheme by url. An id that
// names a checkout carries slashes, which is not one path segment, and
// the iframe showed "no such panel".
const PANEL_IDS = [
  "ade.openChat",
  "ade.commitPanel",
  "ade.audit",
  "ade.machines",
  "ade.openSettings",
  "ade.worktrees",
  "ade.newSession",
  "design:spaces.md",
  "spacedesign:/Users/x/code/infracost/coast|FIX-818.md",
  "pr:412",
];

// One bundle per page, as the Makefile's PAGES builds them. A panel whose
// bundle is missing frames and then draws nothing.
const BUNDLES = [
  "chat", "settings", "commit", "worktrees", "repo", "purge",
  "designprune", "quiz", "designedit", "newsession", "sandbox", "audit",
  "machines",
];

async function panelsAreReachable() {
  serve(join(__dirname, ".."));
  for (const id of PANEL_IDS) {
    const res = await net.fetch(hold(id, `<!doctype html><title>${id}</title>`));
    if (!res.ok) {
      console.error(`panel ${id} is not reachable: ${res.status}`);
      return false;
    }
  }
  // Every bundle and every stylesheet, off the same scheme: a wrong asset
  // root is a panel that frames and then draws nothing.
  const assets = [
    ...BUNDLES.map((b) => `dist/${b}.js`),
    ...readdirSync(join(__dirname, "media"))
      .filter((f) => f.endsWith(".css"))
      .map((f) => `media/${f}`),
  ];
  for (const asset of assets) {
    const res = await net.fetch(`ade-webview://asset/${asset}`);
    if (!res.ok) {
      console.error(`${asset} is not reachable: ${res.status}`);
      return false;
    }
  }
  return true;
}

// The keyboard is why ade is an app, so the smoke test presses the key
// rather than trusting the table: main must see it, match it, and the
// window must act on it.
async function pressesOpenTheOmnibox(win, map) {
  win.webContents.on("before-input-event", (event, input) => {
    const command = map.match(input);
    if (!command) return;
    event.preventDefault();
    win.webContents.send("command", command);
  });
  win.webContents.sendInputEvent({
    type: "keyDown",
    keyCode: "k",
    modifiers: [mac ? "cmd" : "control"],
  });
  await new Promise((done) => setTimeout(done, 300));
  return win.webContents.executeJavaScript(
    `Boolean(document.querySelector(".omnibox input"))`,
  );
}

app
  .whenReady()
  .then(async () => {
    const win = new BrowserWindow({
      show: false,
      webPreferences: {
        preload: join(__dirname, "dist", "preload.js"),
        contextIsolation: true,
        nodeIntegration: false,
      },
    });
    const noise = [];
    win.webContents.on("console-message", (_e, _level, message) =>
      noise.push(message),
    );
    await win.loadFile(join(__dirname, "renderer", "index.html"));

    const got = await win.webContents.executeJavaScript(`({
      places: document.querySelectorAll(".activity button").length,
      rows: document.querySelectorAll(".sidebar .row, .sidebar .twig").length,
      bridge: typeof window.ade,
      offline: Boolean(document.querySelector(".sidebar .row .name")),
      background: getComputedStyle(document.body).backgroundColor,
      font: getComputedStyle(document.querySelector(".sheet")).fontFamily,
    })`);

    got.panelsAreReachable = await panelsAreReachable();
    got.omniboxOnTheKey = await pressesOpenTheOmnibox(
      win,
      new Keymap(merge(), mac),
    );

    const wrong = Object.entries({ ...want, omniboxOnTheKey: true }).filter(
      ([k, v]) => got[k] !== v,
    );
    for (const message of noise) console.error("page said:", message);
    console.log(JSON.stringify(got, null, 2));
    if (wrong.length > 0 || noise.length > 0) {
      console.error("the window did not come up as it should:", wrong);
      app.exit(1);
      return;
    }
    console.log("the window came up");
    app.exit(0);
  })
  .catch((err) => {
    console.error("the window did not come up:", err);
    app.exit(1);
  });
