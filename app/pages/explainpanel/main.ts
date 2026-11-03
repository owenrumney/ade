import {
  at,
  clamp,
  counter,
  header,
  lit,
  mermaidOf,
  moved,
  where,
  type Flow,
  type Step,
  type Walkthrough,
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

interface Mermaid {
  render(id: string, source: string): Promise<{ svg: string }>;
  initialize(config: Record<string, unknown>): void;
}

let drawn = 0;
let started = false;
// The rendered diagram, kept across steps: redrawing it on every step makes
// it flicker and move, and only the lit node changes.
let pane: HTMLElement | undefined;
let painted = "";

function flowPane(flow: Flow, step: Step | undefined): HTMLElement {
  const source = mermaidOf(flow);
  if (!pane || source !== painted) {
    pane = el("div", "flow");
    painted = source;
    void paint(pane, source);
    return pane;
  }
  light(pane, lit(flow, step));
  return pane;
}

// start hands mermaid the editor's own colours. Its base theme is the one
// that takes them; the named themes carry a palette of their own.
function start(mermaid: Mermaid): void {
  if (started) return;
  started = true;
  const css = getComputedStyle(document.body);
  const to = (name: string) => css.getPropertyValue(name).trim();
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    fontFamily: to("--ui"),
    flowchart: { useMaxWidth: true },
    themeVariables: {
      background: to("--bg"),
      primaryColor: to("--bg"),
      primaryTextColor: to("--ink"),
      // The panel border is a hairline against the pane: a box nobody can
      // see is not a diagram.
      primaryBorderColor: to("--ink-dim"),
      secondaryColor: to("--bg-raised"),
      tertiaryColor: to("--bg"),
      lineColor: to("--ink-dim"),
      textColor: to("--ink"),
      fontSize: "13px",
    },
  });
}

async function paint(into: HTMLElement, source: string): Promise<void> {
  const mermaid = (window as unknown as { mermaid?: Mermaid }).mermaid;
  if (!mermaid || !source) {
    into.textContent = source;
    return;
  }
  start(mermaid);
  try {
    const { svg } = await mermaid.render(`flow-${++drawn}`, source);
    into.innerHTML = svg;
  } catch (err) {
    into.classList.add("failed");
    into.textContent = String(err);
    return;
  }
  // The step now, not the one the render started on: a draw during it had no
  // nodes to light.
  light(into, lit(walk?.flow, at(walk, here)));
}

// light puts the class on the node the step is at. Mermaid names a node group
// <render id>-flowchart-<node id>-<n>, and the node id is mermaidOf's.
function light(into: HTMLElement, which: number): void {
  const want = new RegExp(`flowchart-n${which}-\\d+$`);
  for (const node of into.querySelectorAll("g.node")) {
    node.classList.toggle("lit", which >= 0 && want.test(node.id));
  }
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
