// Opens the window headless, checks the page drew and the bridge arrived,
// then quits. It is the one test that proves the three bundles load
// together under a real Electron rather than under a bundler.
const { app, BrowserWindow } = require("electron");
const { join } = require("node:path");
const { Keymap, merge } = require("./dist/keymap.js");

app.disableHardwareAcceleration();

const mac = process.platform === "darwin";

const want = {
  places: 8,
  bridge: "object",
};

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
      background: getComputedStyle(document.body).backgroundColor,
      font: getComputedStyle(document.querySelector(".sheet")).fontFamily,
    })`);

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
