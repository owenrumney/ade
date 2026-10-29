import { listed, said, type Host, type Shown } from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("machines") as HTMLElement;

let hosts: Host[] = [];
let busy = "";
let note = "";

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function row(one: Shown): HTMLElement {
  const node = el("div", `row ${one.state}`);
  // The top line is fixed: dot, name, button. Anything longer goes beneath
  // it, so a wrapped error cannot push the button out of line.
  const top = el("div", "top");
  const dot = el("span", "dot");
  dot.title = one.state;
  const name = el("div", "name");
  name.append(el("span", "label", one.label), el("span", "said", said(one)));
  top.append(dot, name);
  node.append(top);

  if (one.warning) node.append(el("div", "warning", one.warning));
  // Selectable, so it can be pasted rather than retyped.
  if (one.error) node.append(el("div", "error", one.error));

  if (one.act) {
    const working = busy === one.name;
    const button = document.createElement("button");
    button.className = working ? "act off" : "act";
    button.textContent = working
      ? one.act === "connect"
        ? "Connecting…"
        : "Disconnecting…"
      : one.act === "connect"
        ? "Connect"
        : "Disconnect";
    button.disabled = Boolean(busy);
    if (!busy) {
      button.addEventListener("click", () => {
        busy = one.name;
        note = "";
        draw();
        host.postMessage({ kind: "do", what: one.act, host: one.name });
      });
    }
    top.append(button);
  }
  return node;
}

function draw(): void {
  root.replaceChildren();
  const rows = listed(hosts);
  if (rows.length === 0) {
    root.append(el("p", "none", "No machines yet. Add one with “Add a machine”."));
    return;
  }
  const list = el("div", "rows");
  for (const one of rows) list.append(row(one));
  root.append(list);
  if (note) root.append(el("p", "note", note));
}

window.addEventListener(
  "message",
  (event: MessageEvent<{ kind?: string; hosts?: Host[]; note?: string }>) => {
    const said = event.data;
    if (said?.kind !== "state") return;
    hosts = said.hosts ?? [];
    note = said.note ?? "";
    busy = "";
    draw();
  },
);

host.postMessage({ kind: "ready" });
