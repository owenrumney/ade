// The content script cannot reach the native host, so it asks here.
const HOST = "io.ade.host";

browser.runtime.onMessage.addListener(async (message) => {
  try {
    const reply = await browser.runtime.sendNativeMessage(HOST, message);
    return reply ?? { ok: false, error: "the ADE host said nothing" };
  } catch (err) {
    // The usual cause is that `ade browser install` has not been run, which
    // reads as "no such native application" rather than anything about ade.
    return { ok: false, error: hostError(err) };
  }
});

function hostError(err) {
  const message = String(err?.message ?? err);
  if (/no such native application|not found/i.test(message)) {
    return "ADE is not connected — run: ade browser install";
  }
  return message;
}
