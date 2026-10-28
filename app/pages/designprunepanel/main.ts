import {
  chosen,
  rows,
  summary,
  ticks,
  trouble,
  type Row,
  type Said,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("prune") as HTMLElement;

// ticked survives a redraw, and a repository's sweep landing after it only
// adds its own default ticks.
const ticked = new Set<string>();
const seen = new Set<string>();
let state: Said = {};

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function focusOn(selector: string): void {
  (root.querySelector(selector) as HTMLElement | null)?.focus();
}

function tick(row: Row): HTMLElement {
  const box = document.createElement("input");
  box.type = "checkbox";
  box.dataset.key = row.key;
  box.checked = ticked.has(row.key);
  box.disabled = Boolean(state.running) || !state.wiki?.enabled;
  box.addEventListener("change", () => {
    if (box.checked) ticked.add(row.key);
    else ticked.delete(row.key);
    draw();
    // The redraw replaced this input, so the keyboard goes back to its copy.
    focusOn(`input[data-key="${CSS.escape(row.key)}"]`);
  });
  const td = el("td", "pick");
  td.append(box);
  return td;
}

// said is the row's state: where it is in the run, and the page it became.
function said(row: Row): HTMLElement {
  const got = state.states?.[row.key];
  if (!got) return el("td", "state", "");
  if (got.state === "published") {
    const td = el("td", "state done");
    td.append(el("span", "", "published · "));
    const link = document.createElement("a");
    link.textContent = got.page ?? "the wiki";
    link.href = "#";
    link.addEventListener("click", () =>
      host.postMessage({ kind: "do", what: "open", page: got.page }),
    );
    td.append(link);
    return td;
  }
  if (got.state === "failed") return el("td", "state bad", got.why ?? "failed");
  return el("td", "state", got.state);
}

function table(all: Row[]): HTMLElement {
  const box = el("table", "rows");
  const head = el("tr");
  for (const name of ["", "Repository", "Document", "Why", "State"]) {
    head.append(el("th", "", name));
  }
  const top = el("thead");
  top.append(head);
  box.append(top);

  const body = el("tbody");
  let last = "";
  for (const row of all) {
    const tr = el("tr", `row ${row.kind}`);
    tr.title = `${row.dir}/${row.file}`;
    tr.append(tick(row));
    // The repository is named once per run of its rows: a column of the same
    // word is what makes a long table unreadable.
    tr.append(el("td", "repo", row.repo === last ? "" : row.repo));
    last = row.repo;
    tr.append(el("td", "name", row.title));
    tr.append(el("td", "why", row.why));
    tr.append(said(row));
    body.append(tr);
  }
  box.append(body);
  return box;
}

function pending(): HTMLElement | undefined {
  const waiting = (state.groups ?? []).filter((group) => group.pending);
  if (waiting.length === 0) return undefined;
  return el("p", "sub", `Sweeping ${waiting.map((group) => group.repo).join(", ")}…`);
}

function bar(all: Row[]): HTMLElement {
  const picked = chosen(all, ticked);
  const box = el("div", "bar");
  box.append(el("span", "sub", `${picked.length} ticked`));

  if (state.running) {
    const stop = el("button", "go", "Stop") as HTMLButtonElement;
    stop.addEventListener("click", () => host.postMessage({ kind: "do", what: "stop" }));
    box.append(stop);
    return box;
  }
  const go = el(
    "button",
    "go",
    `Prune ${picked.length} document${picked.length === 1 ? "" : "s"}`,
  ) as HTMLButtonElement;
  go.disabled = picked.length === 0 || !state.wiki?.enabled;
  go.addEventListener("click", () =>
    host.postMessage({ kind: "do", what: "prune", keys: picked.map((row) => row.key) }),
  );
  box.append(go);
  return box;
}

function draw(): void {
  const all = rows(state.groups);
  root.replaceChildren();

  const head = el("header", "head");
  head.append(el("span", "title", "Design documents"));
  head.append(el("span", "sub", summary(all, state.groups)));
  root.append(head);

  if (state.wiki && !state.wiki.enabled) {
    root.append(el("p", "note", state.wiki.why ?? "wiki is disabled"));
  }
  if (state.note) root.append(el("p", "note", state.note));
  const bad = trouble(state.groups);
  if (bad.length > 0) {
    const box = el("div", "trouble");
    for (const line of bad) box.append(el("p", "sub", line));
    root.append(box);
  }

  const waiting = pending();
  if (all.length === 0) {
    if (waiting) root.append(waiting);
    else root.append(el("p", "sub", summary(all, state.groups)));
    return;
  }
  root.append(table(all));
  if (waiting) root.append(waiting);
  root.append(bar(all));
}

window.addEventListener("message", (event: MessageEvent<Said & { kind?: string }>) => {
  if (event.data?.kind !== "state") return;
  state = event.data;
  // A repository's rows are ticked by default once, when its sweep lands:
  // after that the boxes are yours.
  const all = rows(state.groups);
  const fresh = all.filter((row) => !seen.has(row.key));
  for (const row of fresh) seen.add(row.key);
  for (const pick of ticks(fresh)) ticked.add(pick);
  const here = new Set(all.map((row) => row.key));
  for (const got of [...ticked]) if (!here.has(got)) ticked.delete(got);
  draw();
});

host.postMessage({ kind: "ready" });
