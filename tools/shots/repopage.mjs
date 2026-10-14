// Photograph the repository page without an editor around it: the built
// webview bundle, its stylesheet, and one reading posted in.
//
// Usage: node tools/shots/repopage.mjs <out-dir> [fixture.json]
//   ADE_CHROME names the browser; puppeteer's own cached one is the fallback.

import { readFileSync, mkdirSync, existsSync } from "fs";
import { homedir } from "os";
import { dirname, join, resolve } from "path";
import { fileURLToPath } from "url";
import { execFileSync } from "child_process";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../..");
const [outDir = join(root, ".git/ade/outbox"), fixture = join(here, "repo-fixture.json")] =
  process.argv.slice(2);

// A fresh checkout has no node_modules here, as shots.mjs assumes too.
if (!existsSync(join(here, "node_modules/puppeteer-core"))) {
  execFileSync("npm", ["install", "--no-audit", "--no-fund"], { cwd: here, stdio: "inherit" });
}
const { default: puppeteer } = await import("puppeteer-core");

const chrome =
  process.env.ADE_CHROME ??
  [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    join(homedir(), "Library/Caches/ms-playwright/chromium-1148/chrome-mac/Chromium.app/Contents/MacOS/Chromium"),
    "/usr/bin/chromium",
  ].find(existsSync);

const css = readFileSync(join(root, "vscode/media/repo.css"), "utf8");
const js = readFileSync(join(root, "vscode/dist/repo.js"), "utf8");
const reading = JSON.parse(readFileSync(fixture, "utf8"));

// The theme variables the page borrows. A webview inherits them from the
// editor; here they are stated once so the picture is the editor's dark theme.
const theme = `
:root {
  --vscode-font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  --vscode-font-size: 13px;
  --vscode-foreground: #cccccc;
  --vscode-descriptionForeground: #9d9d9d;
  --vscode-editor-background: #1f1f1f;
  --vscode-editorWidget-background: #252526;
  --vscode-panel-border: #2b2b2b;
  --vscode-button-secondaryBackground: #313131;
  --vscode-button-secondaryForeground: #cccccc;
  --vscode-errorForeground: #f85149;
  --vscode-charts-blue: #4593e5;
  --vscode-charts-purple: #b180d7;
  --vscode-charts-green: #89d185;
  --vscode-charts-orange: #d18616;
  --vscode-charts-yellow: #dcb67a;
  --vscode-charts-foreground: #9d9d9d;
}`;

mkdirSync(outDir, { recursive: true });

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  args: ["--no-sandbox", "--force-device-scale-factor=2"],
});
const page = await browser.newPage();
await page.setViewport({ width: 1180, height: 900 });
await page.setContent(
  `<!doctype html><html><head><meta charset="utf-8"><style>${theme}${css}</style></head>
   <body><main id="repo"></main></body></html>`,
);
await page.evaluate(() => {
  window.acquireVsCodeApi = () => ({ postMessage() {} });
});
await page.addScriptTag({ content: js });
await page.evaluate((repo) => {
  window.postMessage({ kind: "state", repo }, "*");
}, reading);
await new Promise((r) => setTimeout(r, 400));

const file = join(outDir, "repo-page.png");
await page.screenshot({ path: file, fullPage: true });
console.log(file);
await browser.close();
