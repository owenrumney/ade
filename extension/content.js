// Injects "Open in ADE" into a pull request's header, and keeps it there
// across GitHub's client-side navigation and its own re-renders.
const BUTTON_ID = "ade-open-button";

// Where to put it, best place first.
//
// GitHub's pull request page is Primer React, whose class names carry a build
// hash — PullRequestHeader-module__titleWithAction__ODY5f — so they are matched
// by the part in front of it. The data-component attributes are the component
// names themselves and do not change at all, which is why the fallbacks are
// those. The last entry is the old server-rendered header, still what GitHub
// Enterprise serves.
const TARGETS = [
  '[data-component="PH_Actions"]:not(.d-none)',
  '[class*="PullRequestHeader-module__titleWithAction"]',
  '[data-component="TitleArea"]',
  ".gh-header-actions",
];

const RESET_AFTER = 6000;

// How many settled re-renders to look for a home in before saying so. GitHub
// changing its header is the one failure that is otherwise silent: no button,
// no error, nothing to search for.
const GIVE_UP_AFTER = 20;

let timer = null;
let misses = 0;

function pullRequest() {
  const match = location.pathname.match(/^\/([^/]+)\/([^/]+)\/pull\/(\d+)/);
  if (!match) return null;
  return { owner: match[1], repo: match[2], number: Number(match[3]) };
}

function anchor() {
  for (const selector of TARGETS) {
    const found = document.querySelector(selector);
    // A slot React has rendered but left hidden is not somewhere to put a
    // button: it would be there, and invisible.
    if (found && found.offsetParent !== null) return found;
  }
  return null;
}

function inject() {
  const pr = pullRequest();
  if (!pr) {
    document.getElementById(BUTTON_ID)?.remove();
    return;
  }
  if (document.getElementById(BUTTON_ID)?.isConnected) return;

  const host = anchor();
  if (!host) {
    if (++misses === GIVE_UP_AFTER) {
      console.warn("Open in ADE: no pull request header matched", TARGETS);
    }
    return;
  }
  misses = 0;

  const button = document.createElement("button");
  button.id = BUTTON_ID;
  button.type = "button";
  button.className = "ade-open-button";
  button.textContent = "Open in ADE";
  button.addEventListener("click", () => open(button, pr));
  host.append(button);
}

async function open(button, pr) {
  clearTimeout(timer);
  button.disabled = true;
  button.dataset.state = "busy";
  button.textContent = "Opening…";

  let reply;
  try {
    reply = await browser.runtime.sendMessage({ action: "open", ...pr });
  } catch (err) {
    reply = { ok: false, error: String(err?.message ?? err) };
  }

  button.disabled = false;
  if (reply?.ok) {
    button.dataset.state = "done";
    button.textContent = "Opened in ADE";
    button.title = reply.dir ?? "";
  } else {
    const error = reply?.error ?? "unknown error";
    button.dataset.state = "failed";
    button.textContent = "ADE failed";
    button.title = error;
    // The button is too small to say why, and a tooltip is easy to miss.
    console.error("Open in ADE:", error);
  }
  timer = setTimeout(() => {
    button.dataset.state = "";
    button.title = "";
    button.textContent = "Open in ADE";
  }, RESET_AFTER);
}

// The header is rendered after the document is idle, replaced on navigation,
// and replaced again on re-render, so the button goes back whenever the page
// settles rather than once on load.
let pending = null;
function schedule() {
  clearTimeout(pending);
  pending = setTimeout(inject, 150);
}

new MutationObserver(schedule).observe(document.body, { childList: true, subtree: true });
document.addEventListener("turbo:load", schedule);
document.addEventListener("pjax:end", schedule);
inject();
