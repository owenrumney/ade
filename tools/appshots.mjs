// Pictures of the app, driven over the same chromium `make shots` uses.
// The renderer is a plain page, so no Electron is needed to look at it:
// the shell is stood in for by tools/appfixture.mjs, and every path the
// window takes to draw is its own.
import puppeteer from "./shots/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js";
import { copyFileSync, mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { shell } from "./appfixture.mjs";

const page1 = "file://" + resolve("app/renderer/index.html");
const out = process.env.ADE_OUTBOX || resolve(".git/ade/outbox");
mkdirSync(out, { recursive: true });

// A reply carries four pictures, and the outbox sends them in name
// order, so shooting the gallery means sending the first four for ever
// and never the one the change is about. Name what you want:
//
//     make app-shots VIEWS="app-chat app-code"
//
// Nothing named shoots them all and sends none, which is the honest
// default: the files are on disk to look at.
const wanted = process.argv.slice(2).flatMap((bit) => bit.split(/[\s,]+/)).filter(Boolean);

// Each view: the activity button by its title, then the row or the twigs
// to open. `session` picks a checkout first, which is what a session verb
// and the Review diff both act on.
const VIEWS = [
  { name: "app-prs", place: "Pull requests", row: "#412 Add usage tiles to the desk" },
  { name: "app-designs", place: "Designs", row: "rethink-workflow-and-uk" },
  { name: "app-spaces", place: "Spaces" },
  { name: "app-space", place: "Spaces", row: "owenrumney/ade" },
  { name: "app-issues", place: "Issues" },
  { name: "app-open-space", place: "Spaces", keys: "k", type: "open space" },
  { name: "app-review", place: "Review", session: "Rethink the workflow and the window", twigs: ["app/renderer", "doing.ts"] },
  { name: "app-omnibox", place: "Pull requests", keys: "k" },
  { name: "app-review-agents", place: "Review", session: "Rethink the workflow and the window", keys: "k", type: "review with named", enter: true },
  { name: "app-worktree-switch", place: "Review", session: "Rethink the workflow and the window", tap: ".statusbar .item" },
  { name: "app-verbs", place: "Pull requests", keys: "k", type: "review" },
  { name: "app-waiting", place: "Waiting on you" },
  { name: "app-sessions", place: "Sessions" },
  { name: "app-agents", place: "Agents" },
  { name: "app-terminal", place: "Terminal" },
  { name: "app-todos", place: "Todos" },
  { name: "app-team", place: "Team" },
  { name: "app-person", place: "Team", row: "kim" },
  { name: "app-jobs", place: "Jobs" },
  {
    name: "app-chat",
    place: "Sessions",
    row: "Rethink the workflow and the window",
  },
  { name: "app-review-edit", place: "Review", session: "Rethink the workflow and the window", twigs: ["app/renderer", "doing.ts"], tap: ".tab.on .swap", settle: 1200 },
  { name: "app-code", place: "Code", session: "Rethink the workflow and the window", twigs: ["internal/web", "web.go"] },
  {
    name: "app-commit",
    place: "Pull requests",
    session: "Rethink the workflow and the window",
    keys: "k",
    type: "Commit",
    enter: true,
  },
  {
    name: "app-permissions",
    place: "Pull requests",
    session: "Rethink the workflow and the window",
    keys: "k",
    type: "permission",
    enter: true,
  },
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

const browser = await puppeteer.launch({
  executablePath: process.env.ADE_CHROME || "/usr/bin/chromium",
  args: ["--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--allow-file-access-from-files"],
});
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
await page.evaluateOnNewDocument(shell());
// Each view starts from the same window: the real one remembers where
// it was, and a picture of where the last shot left it is not a view.
await page.evaluateOnNewDocument(() => localStorage.clear());
page.on("console", (m) => m.type() === "error" && console.error("page:", m.text()));
page.on("pageerror", (e) => console.error("page threw:", e.message));

// clicked finds one thing by its text and clicks it, or says what was
// missing: a picture of the wrong view is worse than no picture.
async function clicked(what, selector, text) {
  const found = await page.evaluateHandle(
    (sel, want) => {
      const all = [...document.querySelectorAll(sel)];
      return all.find((n) => (n.textContent ?? "").trim() === want)?.closest(".row, .twig, button");
    },
    selector,
    text,
  );
  const node = found.asElement();
  if (!node) throw new Error(`no ${what} called ${text}`);
  await node.click();
  await new Promise((done) => setTimeout(done, 120));
}

// Naming one shoots one: the rest cost ten seconds nobody asked for.
for (const view of wanted.length ? VIEWS.filter((v) => wanted.includes(v.name)) : VIEWS) {
  await page.goto(page1, { waitUntil: "load" });
  // A tree place draws twigs and a list draws rows; the window may
  // have come up on either, since it remembers where it was.
  await page.waitForSelector(".sidebar .row, .sidebar .twig, .sidebar .empty");
  if (view.session) {
    await page.$$eval(".activity button", (bs) => {
      bs.find((b) => b.title === "Sessions")?.click();
    });
    await new Promise((done) => setTimeout(done, 120));
    await clicked("session", ".sidebar .row .name", view.session);
  }
  await page.$$eval(
    ".activity button",
    (bs, want) => bs.find((b) => b.title === want)?.click(),
    view.place,
  );
  await new Promise((done) => setTimeout(done, 150));
  if (view.row) await clicked("row", ".sidebar .row .name", view.row);
  for (const want of view.twigs ?? []) await clicked("twig", ".sidebar .twig .nm", want);
  if (view.tap) {
    await page.waitForSelector(view.tap);
    await page.click(view.tap);
    await new Promise((done) => setTimeout(done, view.settle ?? 0));
    if (!view.settle) await page.waitForSelector(".omnibox input");
  }

  if (view.keys) {
    await page.keyboard.down("Control");
    await page.keyboard.press(view.keys);
    await page.keyboard.up("Control");
    await page.waitForSelector(".omnibox input");
    if (view.type) await page.type(".omnibox input", view.type);
    if (view.enter) {
      await page.keyboard.press("Enter");
      await new Promise((done) => setTimeout(done, 400));
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
