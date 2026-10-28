import { chosen, rows, selectable, summary, type Row, type Sweep } from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("purge") as HTMLElement;

interface Shown {
  sweep?: Sweep;
  busy?: boolean;
  note?: string;
}

// ticked survives a redraw: a sweep refreshed under a part-made selection
// should not throw the selection away.
const ticked = new Set<string>();
let force = false;
let state: Shown = {};

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function cell(text: string, cls?: string): HTMLElement {
  return el("td", cls, text);
}

function focusOn(selector: string): void {
  (root.querySelector(selector) as HTMLElement | null)?.focus();
}

function tick(row: Row): HTMLElement {
  const box = document.createElement("input");
  box.type = "checkbox";
  box.dataset.path = row.path;
  box.checked = ticked.has(row.path);
  box.disabled = !row.removable;
  box.addEventListener("change", () => {
    if (box.checked) ticked.add(row.path);
    else ticked.delete(row.path);
    draw();
    // The redraw replaced this input, so the keyboard goes back to its copy.
    focusOn(`input[data-path="${CSS.escape(row.path)}"]`);
  });
  const td = el("td", "pick");
  td.append(box);
  return td;
}

// tickAll is the header's box: every removable row at once, and off again.
function tickAll(all: Row[]): HTMLElement {
  const paths = selectable(all);
  const box = document.createElement("input");
  box.type = "checkbox";
  box.className = "all";
  box.title = "Select all applicable";
  box.setAttribute("aria-label", "Select all applicable");
  box.disabled = paths.length === 0;
  box.checked = paths.length > 0 && paths.every((path) => ticked.has(path));
  box.indeterminate = !box.checked && paths.some((path) => ticked.has(path));
  box.addEventListener("change", () => {
    for (const path of paths) {
      if (box.checked) ticked.add(path);
      else ticked.delete(path);
    }
    draw();
    focusOn("th .all");
  });
  const th = el("th", "pick");
  th.append(box);
  return th;
}

function table(all: Row[]): HTMLElement {
  const box = el("table", "rows");
  const head = el("tr");
  head.append(tickAll(all));
  for (const name of ["Repository", "Worktree", "Branch", "Last used", "Behind", "State"]) {
    head.append(el("th", "", name));
  }
  const top = el("thead");
  top.append(head);
  box.append(top);

  const body = el("tbody");
  let last = "";
  for (const row of all) {
    const tr = el("tr", row.removable ? "row free" : "row kept");
    tr.title = row.path;
    tr.append(tick(row));
    // The repository is named once per run of its rows, keyed on its path: a
    // column of the same word is what makes a long table unreadable.
    tr.append(cell(row.repoPath === last ? "" : row.repo, "repo"));
    last = row.repoPath;
    tr.append(cell(row.name, "name"));
    tr.append(cell(row.branch, "branch"));
    tr.append(cell(row.lastUsed, "when"));
    tr.append(cell(row.behind, "behind"));
    tr.append(cell(row.state, row.removable ? "state free" : "state kept"));
    body.append(tr);
  }
  box.append(body);
  return box;
}

function trouble(sweep: Sweep | undefined): HTMLElement | undefined {
  const bad = (sweep?.repos ?? []).filter((repo) => repo.why);
  if (bad.length === 0) return undefined;
  const box = el("div", "trouble");
  for (const repo of bad) box.append(el("p", "sub", `${repo.name}: ${repo.why}`));
  return box;
}

function bar(all: Row[]): HTMLElement {
  const picked = chosen(all, ticked);
  const box = el("div", "bar");

  const label = el("label", "force");
  const toggle = document.createElement("input");
  toggle.type = "checkbox";
  toggle.checked = force;
  toggle.addEventListener("change", () => {
    force = toggle.checked;
    // A row force freed and then not cannot stay ticked: chosen() drops it
    // from the count, and a disabled box fires no change to untick it with.
    for (const row of rows(state.sweep, force)) if (!row.removable) ticked.delete(row.path);
    draw();
    focusOn(".force input");
  });
  label.append(toggle, el("span", "", "Remove worktrees holding commits that are nowhere else"));
  box.append(label);

  const remove = el("button", "remove", `Remove ${picked.length}`) as HTMLButtonElement;
  remove.disabled = picked.length === 0 || Boolean(state.busy);
  remove.addEventListener("click", () =>
    host.postMessage({
      kind: "do",
      what: "remove",
      paths: picked.map((row) => row.path),
      force,
    }),
  );
  box.append(remove);
  return box;
}

function draw(): void {
  const all = rows(state.sweep, force);
  root.replaceChildren();

  const head = el("header", "head");
  head.append(el("span", "title", "Worktrees"));
  head.append(el("span", "sub", state.busy ? "Scanning…" : summary(all)));
  const refresh = el("button", "refresh", state.busy ? "…" : "Refresh") as HTMLButtonElement;
  refresh.disabled = Boolean(state.busy);
  refresh.addEventListener("click", () => host.postMessage({ kind: "do", what: "refresh" }));
  head.append(refresh);
  root.append(head);

  if (state.note) root.append(el("p", "note", state.note));
  const bad = trouble(state.sweep);
  if (bad) root.append(bad);

  if (all.length === 0) {
    root.append(el("p", "sub", state.busy ? "Reading every repository…" : summary(all)));
    return;
  }
  root.append(table(all), bar(all));
}

window.addEventListener("message", (event: MessageEvent<Shown & { kind?: string }>) => {
  if (event.data?.kind !== "state") return;
  state = event.data;
  // A path the latest sweep no longer offers cannot stay ticked.
  const here = new Set(rows(state.sweep, force).map((row) => row.path));
  for (const path of [...ticked]) if (!here.has(path)) ticked.delete(path);
  draw();
});

host.postMessage({ kind: "ready" });
