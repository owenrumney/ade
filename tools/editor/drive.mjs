#!/usr/bin/env node
// Drive the dev editor over Electron's debugging port: send keys, type, and
// capture the window. Usage: drive.mjs [--port N] [key|type:text|wait:ms|shot:file]...
const args = process.argv.slice(2);
let port = 9333;
if (args[0] === "--port") {
  port = Number(args[1]);
  args.splice(0, 2);
}

const pages = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
const page = pages.find((p) => p.type === "page" && p.url.includes("workbench"));
if (!page) throw new Error("no workbench window on the debugging port");
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((ok, no) => ((ws.onopen = ok), (ws.onerror = no)));
let seq = 0;
const waiting = new Map();
ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && waiting.has(msg.id)) {
    waiting.get(msg.id)(msg);
    waiting.delete(msg.id);
  }
};
const call = (method, params = {}) =>
  new Promise((ok) => {
    const id = ++seq;
    waiting.set(id, ok);
    ws.send(JSON.stringify({ id, method, params }));
  });
const sleep = (ms) => new Promise((ok) => setTimeout(ok, ms));

const MOD = { alt: 1, ctrl: 2, meta: 4, cmd: 4, shift: 8 };
const NAMED = {
  enter: ["Enter", "Enter", 13],
  escape: ["Escape", "Escape", 27],
  tab: ["Tab", "Tab", 9],
  down: ["ArrowDown", "ArrowDown", 40],
  up: ["ArrowUp", "ArrowUp", 38],
  space: [" ", "Space", 32],
};

async function key(chord) {
  const parts = chord.toLowerCase().split("+");
  const last = parts.pop();
  const modifiers = parts.reduce((m, p) => m | (MOD[p] ?? 0), 0);
  const [k, code, vk] = NAMED[last] ?? [
    last,
    `Key${last.toUpperCase()}`,
    last.toUpperCase().charCodeAt(0),
  ];
  const base = { modifiers, key: k, code, windowsVirtualKeyCode: vk };
  await call("Input.dispatchKeyEvent", { type: "keyDown", ...base });
  await call("Input.dispatchKeyEvent", { type: "keyUp", ...base });
}

for (const step of args) {
  const [verb, ...rest] = step.split(":");
  const arg = rest.join(":");
  if (verb === "type") await call("Input.insertText", { text: arg });
  else if (verb === "click") {
    const [x, y] = arg.split(",").map(Number);
    for (const type of ["mousePressed", "mouseReleased"])
      await call("Input.dispatchMouseEvent", { type, x, y, button: "left", clickCount: 1 });
  }
  else if (verb === "wait") await sleep(Number(arg));
  else if (verb === "eval") {
    const { result } = await call("Runtime.evaluate", { expression: arg, awaitPromise: true, returnByValue: true });
    console.log(JSON.stringify(result.value ?? result.description ?? result));
  }
  else if (verb === "shot") {
    const { result } = await call("Page.captureScreenshot", { format: "png" });
    const { writeFileSync } = await import("node:fs");
    writeFileSync(arg, Buffer.from(result.data, "base64"));
  } else await key(step);
  await sleep(150);
}
ws.close();
