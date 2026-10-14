// The bridge into ade's own pages.
//
// A closed ade leaves a live page with a dead server behind it, and starting
// another one is a command, which only the native host can run. Firefox gives
// a page no way to call an extension, so the channel is a postMessage on its
// own window: nothing outside this page can see it, and nothing here reaches
// the host except the one action below.
const REQUEST = "ade-extension-request";
const REPLY = "ade-extension-reply";

// How the page knows there is an add-on to ask. Absent, it offers a reload.
document.documentElement.dataset.adeExtension = "1";

window.addEventListener("message", async (event) => {
  if (event.source !== window) return;
  const message = event.data;
  if (message?.tag !== REQUEST || !message.id) return;

  let reply;
  try {
    // Owner and repo only: the host resolves the checkout itself, so a page
    // cannot name a directory or ask for anything but a session.
    reply = await browser.runtime.sendMessage({
      action: "session",
      owner: String(message.owner ?? ""),
      repo: String(message.repo ?? ""),
      branch: String(message.branch ?? ""),
    });
  } catch (err) {
    reply = { ok: false, error: String(err?.message ?? err) };
  }
  window.postMessage({ tag: REPLY, id: message.id, reply: reply ?? null }, location.origin);
});
