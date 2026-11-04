import {
  at,
  clamp,
  counter,
  header,
  moved,
  sequence,
  where,
  type Flow,
  type Lane,
  type Step,
  type Walkthrough,
  type Wire,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("explain") as HTMLElement;

interface Carried {
  kind?: string;
  walkthrough?: Walkthrough;
  busy?: boolean;
  note?: string;
}

let walk: Walkthrough | undefined;
let busy = false;
let note = "";
// Where in the walk the reader is. Not posted or stored anywhere.
let here = 0;

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function send(what: string, extra: Record<string, unknown> = {}): void {
  host.postMessage({ kind: "do", what, ...extra });
}

function button(label: string, go: () => void, off = false): HTMLElement {
  const node = el("button", undefined, label) as HTMLButtonElement;
  node.disabled = off;
  node.addEventListener("click", go);
  return node;
}

// anchor is the file as a link that opens the editor on it.
function anchor(file: string, label: string): HTMLElement {
  const node = el("button", "linkish", label) as HTMLButtonElement;
  node.addEventListener("click", () => send("open", { file }));
  return node;
}

function drawHead(): void {
  const head = el("header", "head");
  head.append(el("h1", undefined, "Explain this change"));
  if (walk) head.append(el("p", "sub", header(walk)));
  if (walk?.stale)
    head.append(el("p", "warn", "The diff has moved since these steps were written."));
  const row = el("div", "row verbs");
  if (walk) row.append(button(busy ? "Reading…" : "Again", () => regenerate(), busy));
  if (busy) row.append(button("Cancel", () => send("cancel")));
  head.append(row);
  root.append(head);
}

function drawStep(step: Step, total: number): void {
  const box = el("section", "step");
  const top = el("div", "row top");
  top.append(el("span", "count", counter(here, total)), el("h2", "title", step.title));
  box.append(top);
  box.append(el("p", "body", step.body));

  if (step.excerpt?.length) {
    const code = el("pre", "excerpt");
    for (const line of step.excerpt) {
      const row = el("span", line.in ? "line in" : "line");
      row.append(el("span", "n", String(line.n)), el("span", "text", line.text));
      code.append(row);
    }
    box.append(code);
  }
  const links = el("div", "row links");
  links.append(anchor(step.file, where(step)));
  box.append(links);
  root.append(box);
}

function drawStrip(total: number): void {
  const strip = el("nav", "strip");
  strip.append(button("Back", () => go(here - 1), here === 0));
  for (let i = 0; i < total; i++) {
    const dot = button(String(i + 1), () => go(i)) as HTMLButtonElement;
    dot.className = i === here ? "dot here" : "dot";
    strip.append(dot);
  }
  strip.append(button("Next", () => go(here + 1), here >= total - 1));
  root.append(strip);
}

function go(to: number): void {
  here = clamp(to, walk?.steps.length ?? 0);
  draw();
}

function regenerate(): void {
  here = 0;
  busy = true;
  send("regenerate");
  draw();
}

function draw(): void {
  root.replaceChildren();
  drawHead();
  if (note) root.append(el("p", "note", note));
  if (!walk) {
    root.append(
      el(
        "p",
        "sub",
        busy
          ? "Reading the change and working out the order. A minute or so."
          : "Nothing to explain yet.",
      ),
    );
    return;
  }
  const total = walk.steps.length;
  const step = at(walk, here);
  if (walk.flow) root.append(flowPane(walk.flow, step));
  if (step) drawStep(step, total);
  drawStrip(total);
}

// ── The flow ──

// One grid: a lane a node, a row an edge, the rails spanning every row.
function flowPane(flow: Flow, step: Step | undefined): HTMLElement {
  const { lanes, wires } = sequence(flow, step);
  const pane = el("div", "flow");
  if (!lanes.length) return pane;
  pane.style.gridTemplateColumns = `repeat(${lanes.length * 2}, minmax(0, 1fr))`;
  for (const [i, lane] of lanes.entries()) {
    const rail = el("span", "rail");
    rail.style.gridColumn = lane_(i);
    rail.style.gridRow = `2 / ${wires.length + 2}`;
    pane.append(rail, headPane(lane, i));
  }
  for (const [row, wire] of wires.entries()) pane.append(wirePane(wire, row));
  return pane;
}

// Each lane is two grid columns, so a message can run centre to centre
// rather than edge to edge. lane_ is the pair, mid the line between them.
function lane_(i: number): string {
  return `${2 * i + 1} / ${2 * i + 3}`;
}
function mid(i: number): number {
  return 2 * i + 2;
}

function headPane(lane: Lane, i: number): HTMLElement {
  const node = lane.file ? el("button", "head") : el("div", "head");
  node.textContent = lane.label;
  node.style.gridColumn = lane_(i);
  node.style.gridRow = "1";
  if (lane.lit) node.classList.add("lit");
  if (lane.file) {
    node.title = lane.line ? `${lane.file}:${lane.line}` : lane.file;
    node.addEventListener("click", () => send("open", { file: lane.file }));
  }
  return node;
}

function wirePane(wire: Wire, row: number): HTMLElement {
  const here = wire.from === wire.to;
  const node = el("span", here ? "wire here" : "wire");
  if (wire.from > wire.to) node.classList.add("back");
  if (wire.lit) node.classList.add("lit");
  node.append(el("span", "words", wire.label));
  node.style.gridColumn = here
    ? lane_(wire.from)
    : `${mid(Math.min(wire.from, wire.to))} / ${mid(Math.max(wire.from, wire.to))}`;
  node.style.gridRow = String(row + 2);
  return node;
}

window.addEventListener("message", (event: MessageEvent<Carried>) => {
  const said = event.data;
  if (said.kind !== "state") return;
  // A fresh set of steps starts at the first one.
  if (moved(walk, said.walkthrough)) here = 0;
  walk = said.walkthrough;
  busy = Boolean(said.busy);
  note = said.note ?? "";
  draw();
});

host.postMessage({ kind: "ready" });
