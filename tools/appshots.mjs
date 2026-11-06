// Pictures of the app, driven over the same chromium `make shots` uses.
// The renderer is a plain page, so no Electron is needed to look at it:
// the shell is stood in for by tools/appfixture.mjs, and every path the
// window takes to draw is its own.
import puppeteer from "./shots/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js";
import { copyFileSync, existsSync, mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { shell } from "./appfixture.mjs";

const page1 = "file://" + resolve("app/renderer/index.html");
const out = process.env.ADE_OUTBOX || resolve(".git/ade/outbox");
mkdirSync(out, { recursive: true });

// A reply carries four pictures, and the outbox sends them in name
// order, so shooting the gallery means sending the first four for ever
// and never the one the change is about. Name what you want:
//
//     make app-shots VIEWS="app-sessions app-diff"
//
// Nothing named shoots them all and sends none, which is the honest
// default: the files are on disk to look at.
const wanted = process.argv.slice(2).flatMap((bit) => bit.split(/[\s,]+/)).filter(Boolean);

// Each view: `session` picks one on the left, `tab` one of its three tabs,
// `place` a button on the bar, then `row` in its list or `twigs` in the Diff's files.
const S = "Rethink the workflow and the window";
const FILE = ["app/renderer", "doing.ts"];
const VIEWS = [
  { name: "app-sessions" },
  { name: "app-session", session: S },
  { name: "app-session-menu", right: ".sidebar.left .row" },
  { name: "app-tags", session: S, keys: "k", type: "Tag this session", enter: true },
  { name: "app-import", keys: "k", type: "Import a claude", enter: true },
  { name: "app-brief", session: S, tap: ".sidebar.left .row .tag.brief", settle: 600 },
  { name: "app-shell", session: S, tab: "Shell" },
{ name: "app-shots-tab", session: S, tab: "Shots" },
  { name: "app-notifications", session: S, place: "Notifications" },
  { name: "app-prs", place: "Pull requests" },
  { name: "app-inbox", place: "Inbox" },
  { name: "app-team", place: "Team" },
  { name: "app-person", place: "Team", row: "kim" },
  // The title carries the age of the fire, so the tap matches on its start.
  { name: "app-myday", tap: ".activity button[title^='My Day']", settle: 400 },
  { name: "app-side-shut", session: S, place: "Team", again: "Team" },
  { name: "app-diff", session: S, tab: "Diff", twigs: FILE },
{ name: "app-diff-new", session: S, tab: "Diff", twigs: FILE, query: "?nopr=1" },
  { name: "app-diff-side", session: S, tab: "Diff", twigs: FILE, place: "Notifications" },
  { name: "app-review-adding", session: S, tab: "Diff", tap: ".review-bar .btn.add", settle: 500 },
  { name: "app-review-pressed", session: S, tab: "Diff", twigs: FILE, tapText: "Add to the review", settle: 400 },
  { name: "app-review-split", session: S, tab: "Diff", twigs: FILE, tap: ".diff .file-head .how", settle: 300 },
  { name: "app-review-viewed", session: S, tab: "Diff", twigs: FILE, tap: ".diff .file-head .seen input", settle: 300 },
  { name: "app-review-edit", session: S, tab: "Diff", twigs: FILE, tap: ".tab.on .swap", settle: 1200 },
  { name: "app-review-agents", session: S, keys: "k", type: "review with named", enter: true },
  { name: "app-review-running", session: "Pruning the design docs", tab: "Diff" },
  // A switch while on the Diff: the files and the diff must be the new session's.
  { name: "app-switch", session: "Pruning the design docs", tab: "Diff", tap: ".sidebar.left .row.session:nth-of-type(6)", settle: 1500, then: "Diff" },
  { name: "app-review-folded", session: "Pruning the design docs", tab: "Diff", tap: ".sidebar .review-run .fold-chev", settle: 300 },
  { name: "app-review-finished", session: "Webview for the sandbox", tab: "Diff" },
  { name: "app-tab-menu", session: S, tab: "Diff", twigs: FILE, right: ".tab.on" },
  { name: "app-split", session: S, tab: "Diff", twigs: FILE, drag: { what: '.tab[data-tab="diff"]', to: 0.78 } },
  { name: "app-split-drop", session: S, tab: "Diff", twigs: FILE, drag: { what: '.tab[data-tab="diff"]', to: 0.78, hold: true } },
  { name: "app-omnibox", keys: "k" },
  { name: "app-verbs", keys: "k", type: "review" },
  { name: "app-new-session", keys: "n", after: ".tab.on", settle: 1200 },
  { name: "app-spaces", keys: "k", type: "Spaces", enter: true },
  { name: "app-issues", keys: "k", type: "Issues", enter: true },
  { name: "app-open-space", keys: "k", type: "open space" },
  { name: "app-permissions", session: S, keys: "k", type: "permission", enter: true },
  { name: "app-sessions-hidden", session: S, keys: "b", after: ".main" },
];

const known = new Set(VIEWS.map((view) => view.name));
const unknown = wanted.filter((name) => !known.has(name));
if (unknown.length) {
  console.error(`no view named: ${unknown.join(", ")}`);
  console.error(`known: ${[...known].sort().join(", ")}`);
  process.exit(2);
}

// Every shot is written here; the named ones are copied to the outbox.
const shots = resolve("app/dist/shots");
mkdirSync(shots, { recursive: true });

// Said before the launch, because puppeteer's own version of this is a stack
// trace, and the fix is one command the trace does not name.
const chrome = process.env.ADE_CHROME || "/usr/bin/chromium";
if (!existsSync(chrome)) {
  console.error(`no chromium at ${chrome}`);
  console.error("Install one, or point ADE_CHROME at it:");
  console.error("  macOS  brew install --cask chromium");
  console.error("  Debian apt-get install -y chromium");
  process.exit(3);
}

const browser = await puppeteer.launch({
  executablePath: chrome,
  args: ["--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--allow-file-access-from-files"],
});
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
// The window's policy names the webview scheme, which only the shell
// serves. A panel here is a blob of the same skeleton, so the policy has
// to be off for one to draw at all.
await page.setBypassCSP(true);
await page.evaluateOnNewDocument(shell());
// Each view starts from the same window: the real one remembers where
// it was, and a picture of where the last shot left it is not a view.
await page.evaluateOnNewDocument(() => {
  localStorage.clear();
});
page.on("console", (m) => m.type() === "error" && console.error("page:", m.text()));
page.on("pageerror", (e) => console.error("page threw:", e.message));

// clicked finds one thing by its text and clicks it, or says what was
// missing: a picture of the wrong view is worse than no picture.
async function clicked(what, selector, text) {
  const found = await page.evaluateHandle(
    (sel, want) => {
      const all = [...document.querySelectorAll(sel)];
      return all.find((n) => (n.textContent ?? "").trim() === want);
    },
    selector,
    text,
  );
  const node = found.asElement();
  if (!node) {
    const saw = await page.$$eval(selector, (all) => all.slice(0, 12).map((n) => (n.textContent ?? "").trim()));
    throw new Error(`no ${what} called ${text}; saw ${saw.length ? saw.join(", ") : "none"}`);
  }
  await node.click();
  await new Promise((done) => setTimeout(done, 120));
}

// pressed clicks a button on the bar by its title, or says which there were.
async function pressed(title) {
  const saw = await page.$$eval(
    ".activity button",
    (bs, want) => {
      const found = bs.find((b) => b.title === want);
      found?.click();
      return found ? [] : bs.map((b) => b.title);
    },
    title,
  );
  if (saw.length) throw new Error(`no button called ${title}; saw ${saw.join(", ")}`);
  await new Promise((done) => setTimeout(done, 150));
}

// Naming one shoots one: the rest cost ten seconds nobody asked for.
for (const view of wanted.length ? VIEWS.filter((v) => wanted.includes(v.name)) : VIEWS) {
  // A drag shot mid-carry leaves the button down.
  await page.mouse.up().catch(() => undefined);
  await page.goto(page1 + (view.query ?? ""), { waitUntil: "load" });
  await page.waitForSelector(".sidebar.left .row, .sidebar.left .empty");
  if (view.session) await clicked("session", ".sidebar.left .row .name", view.session);
  if (view.tab) await clicked("tab", ".main .head .tab .nm", view.tab);
  for (const want of view.twigs ?? []) await clicked("twig", ".sidebar.files .twig .nm", want);
  if (view.place) await pressed(view.place);
  if (view.row) await clicked("row", ".sidebar.side .row .name", view.row);
  // The same button again shuts its list.
  if (view.again) await pressed(view.again);
  // A row's act is out of the layout until the pointer is on the row, so
  // the pointer goes there first or the click lands on nothing.
  if (view.hover) await page.hover(view.hover);
  if (view.tap) {
    await page.waitForSelector(view.tap);
    await page.click(view.tap);
    await new Promise((done) => setTimeout(done, view.settle ?? 0));
    if (!view.settle) await page.waitForSelector(".omnibox input");
  }
  // A tab opened after the tap, for a view of what a switch left behind.
  if (view.then) await clicked("tab", ".main .head .tab .nm", view.then);

  // A context menu is the one thing the omnibox cannot stand in for, so
  // the shot has to open it the way a hand does.
  if (view.right) {
    await page.waitForSelector(view.right);
    await page.click(view.right, { button: "right" });
    await page.waitForSelector(".menu");
  }

  // A thread's verb is a span with its label, which `tap` cannot name.
  if (view.tapText) {
    const found = await page.evaluateHandle(
      (want) =>
        [...document.querySelectorAll(".thread .acts .btn")].find(
          (n) => (n.textContent ?? "").trim() === want,
        ),
      view.tapText,
    );
    const node = found.asElement();
    if (!node) throw new Error(`no button called ${view.tapText}`);
    await node.click();
    await new Promise((done) => setTimeout(done, view.settle ?? 400));
  }


  // No verb opens a split: the shot drags a tab, as a hand does.
  if (view.drag) {
    const box = (sel) =>
      page.$eval(sel, (n) => {
        const b = n.getBoundingClientRect();
        return [b.left, b.top, b.width, b.height];
      });
    const [tl, tt, tw, th] = await box(view.drag.what);
    const [sl, st, sw, sh] = await box(".main .sheet");
    await page.mouse.move(tl + tw / 2, tt + th / 2);
    await page.mouse.down();
    await page.mouse.move(sl + sw * view.drag.to, st + sh / 2, { steps: 12 });
    // `hold` shoots the drag itself: the shaded half saying where it lands.
    if (!view.drag.hold) await page.mouse.up();
    await new Promise((done) => setTimeout(done, 400));
  }

  if (view.keys) {
    // A panel that is up holds the focus inside its iframe, and the shell's
    // keys are the window's: a click on the titlebar's label puts it back.
    await page.click(".titlebar .where").catch(() => undefined);
    await page.keyboard.down("Control");
    await page.keyboard.press(view.keys);
    await page.keyboard.up("Control");
    await page.waitForSelector(view.after ?? ".omnibox input");
    if (view.type) await page.type(".omnibox input", view.type);
    if (view.enter) {
      await page.keyboard.press("Enter");
      await new Promise((done) => setTimeout(done, view.settle ?? 400));
    }
    // A verb that asks something puts up a second box. `pick` answers it,
    // which is the only way to shoot what the verb then does.
    if (view.pick) {
      await page.waitForSelector(".omnibox input");
      if (view.pick !== true) await page.type(".omnibox input", view.pick);
      await page.keyboard.press("Enter");
      await new Promise((done) => setTimeout(done, view.settle ?? 400));
    }
  }
  await new Promise((done) => setTimeout(done, 250));
  const file = `${shots}/${view.name}.png`;
  await page.screenshot({ path: file });
  if (wanted.includes(view.name)) {
    copyFileSync(file, `${out}/${view.name}.png`);
    console.log(`${out}/${view.name}.png`);
  } else {
    console.log(file);
  }
}
await browser.close();
