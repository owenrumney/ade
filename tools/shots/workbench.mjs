// The real VS Code workbench, served by `code serve-web` and driven like any
// page. What is photographed is the installed extension, not a drawing of it.

const wait = (ms) => new Promise((done) => setTimeout(done, ms));

async function clickSaying(page, selector, text) {
  return page.evaluate(
    (sel, want) => {
      const el = [...document.querySelectorAll(sel)].find(
        (node) => node.textContent?.trim() === want,
      );
      el?.click();
      return Boolean(el);
    },
    selector,
    text,
  );
}

// settle waits for the workbench and gets the extension running in it. Trust
// is application-scoped, so no settings file grants it: the banner has to.
export async function settle(page) {
  await page.waitForSelector(".monaco-workbench", { timeout: 60000 });
  await wait(2500);
  if (await clickSaying(page, "a", "Manage")) {
    await wait(2500);
    await clickSaying(page, ".monaco-button", "Trust");
    await wait(1500);
    // The trust panel stays on top and swallows every key until it is closed.
    await page.keyboard.press("Escape");
  }
  await page.waitForSelector('.activitybar .action-label[aria-label^="ADE"]', {
    timeout: 60000,
  });
  await wait(1500);
}

// run is a command by its palette title. The row is picked by its exact label:
// the palette lists what was used last first, so Enter runs the wrong one.
export async function run(page, title) {
  await page.keyboard.press("F1");
  await wait(600);
  await page.keyboard.type(title, { delay: 15 });
  await wait(900);
  const row = await page.evaluateHandle(
    (want) =>
      [...document.querySelectorAll(".quick-input-list .monaco-list-row")].find(
        (el) => el.querySelector(".label-name")?.textContent?.trim() === want,
      ) ?? null,
    title,
  );
  if (row.asElement()) await row.asElement().click();
  else await page.keyboard.press("Enter");
  await wait(2500);
}

// address is the workbench opened on one folder.
export function address(base, folder) {
  return `${base}/?folder=${encodeURIComponent(folder)}`;
}
