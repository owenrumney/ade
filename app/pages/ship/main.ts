import { columns, rail, type Column, type Gate, type State, type Step } from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("ship") as HTMLElement;

interface Refused {
  do?: string;
  root?: string;
  reason?: string;
}

let state: State = {};
let refused: Refused = {};
let note = "";
let busy = "";

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function press(step: Step, label: string, anyway: boolean): HTMLElement {
  const button = document.createElement("button");
  button.className = anyway ? "act anyway" : "act";
  button.textContent = label;
  button.disabled = Boolean(busy);
  const act = step.act;
  if (act && !button.disabled) {
    button.addEventListener("click", () => {
      busy = `${act.root}:${act.do}`;
      note = "";
      draw();
      host.postMessage({ kind: "do", what: act.do, root: act.root, anyway });
    });
  }
  return button;
}

function card(step: Step, column: Column): HTMLElement {
  const node = el("div", `step ${step.state}`);
  const top = el("div", "top");
  top.append(el("span", "dot"), el("span", "title", step.title));
  node.append(top, el("div", "said", step.said));

  const stopped =
    refused.reason && refused.do === step.act?.do && refused.root === column.project.root
      ? refused.reason
      : "";
  if (stopped) node.append(el("div", "stopped", stopped));
  if (step.because) node.append(el("div", "because", step.because));

  if (step.act) {
    const working = busy === `${column.project.root}:${step.act.do}`;
    const label = working ? `${step.act.label}…` : step.act.label;
    node.append(press(step, stopped ? `${step.act.label} anyway` : label, Boolean(stopped)));
  }
  return node;
}

function gate(one: Gate): HTMLElement {
  const node = el("div", `gate ${one.state}`);
  node.append(el("span", "dot"), el("span", "name", one.name), el("span", "note", one.note));
  if (one.button) {
    const button = document.createElement("button");
    button.className = "act";
    button.textContent = one.button.label;
    button.disabled = Boolean(busy);
    if (!button.disabled) {
      button.addEventListener("click", () => {
        busy = `gate:${one.button?.do}`;
        note = "";
        draw();
        host.postMessage({ kind: "do", what: one.button?.do });
      });
    }
    node.append(button);
  }
  return node;
}

function column(one: Column): HTMLElement {
  const node = el("div", "column");
  const head = el("div", "head");
  head.append(el("span", "name", one.project.name), el("span", "branch", one.project.branch ?? ""));
  node.append(head);
  if (one.note) {
    node.append(el("p", "none", one.note));
    return node;
  }
  for (const step of one.steps) node.append(card(step, one));
  return node;
}

function draw(): void {
  root.replaceChildren();
  const shown = columns(state);
  if (shown.length === 0) {
    root.append(el("p", "none", "No repository yet. Open a checkout and this fills in."));
    return;
  }

  const gates = rail(state);
  if (gates.length > 0) {
    const bar = el("div", "rail");
    for (const one of gates) bar.append(gate(one));
    root.append(bar);
  }

  const grid = el("div", "columns");
  for (const one of shown) grid.append(column(one));
  root.append(grid);
  if (note) root.append(el("p", "note", note));
}

window.addEventListener(
  "message",
  (
    event: MessageEvent<
      State & { kind?: string; refused?: Refused; note?: string; quiet?: boolean }
    >,
  ) => {
    const said = event.data;
    if (said?.kind !== "state") return;
    state = said;
    refused = said.refused ?? {};
    note = said.note ?? "";
    // A stir is nobody pressing: a press in flight keeps its buttons disabled.
    if (!said.quiet) busy = "";
    draw();
  },
);

host.postMessage({ kind: "ready" });
