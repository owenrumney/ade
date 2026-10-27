import { pasted, read, refuse, type Incoming } from "../chat/files";
import { control } from "../mounts/control";
import { added } from "../mounts/view";
import {
  ROWS,
  fresh,
  held,
  home,
  missing,
  onto,
  rowFor,
  summary,
  tick,
  type Chosen,
  type Doc,
  type Mode,
  type Plan,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("newsession") as HTMLElement;

interface Repo {
  path: string;
  name: string;
  owner?: string;
  branch?: string;
}

interface Offer {
  machines: { name: string; label: string }[];
  repos: Repo[];
  agents: { id: string; label: string }[];
  models: { id: string; label?: string }[];
  roles: string[];
  designs: Doc[];
  designNote?: string;
  recent: string[];
}

interface Shown {
  chosen: Chosen;
  plan: Plan;
  offer: Offer;
  busy?: boolean;
  note?: string;
}

const empty: Offer = {
  machines: [],
  repos: [],
  agents: [],
  models: [],
  roles: [],
  designs: [],
  recent: [],
};
const nothingYet = { branch: "", doc: "", written: false, carried: false, makes: [] };
let state: Shown = { chosen: { ...fresh }, plan: nothingYet, offer: empty };
let filter = "";
// Images pasted into the first message. Not in Chosen: they are bytes, and
// Chosen goes to the host on every keystroke.
let carrying: Incoming[] = [];
let refused = "";

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function changed(): void {
  host.postMessage({ kind: "change", chosen: state.chosen });
  // The button answers the keystroke here. A host that replied with a whole
  // state would redraw the page and take the cursor out of the box.
  root.querySelector(".foot")?.replaceWith(foot());
}

function set(part: Partial<Chosen>): void {
  state.chosen = { ...state.chosen, ...part };
  changed();
  draw();
}

function modes(): HTMLElement {
  const box = el("nav", "modes");
  for (const row of ROWS) {
    const one = el("button", row.mode === state.chosen.mode ? "mode on" : "mode");
    one.append(el("span", "mode-label", row.label));
    one.append(el("span", "mode-detail", row.detail));
    one.addEventListener("click", () =>
      set({ mode: row.mode, repos: onto(state.chosen, row.mode) }),
    );
    box.append(one);
  }
  return box;
}

function field(label: string, control: HTMLElement): HTMLElement {
  const box = el("label", "field");
  box.append(el("span", "field-label", label));
  box.append(control);
  return box;
}

function choice(
  options: { id: string; label: string }[],
  value: string,
  onPick: (id: string) => void,
): HTMLElement {
  const sel = document.createElement("select");
  for (const option of options) {
    const node = document.createElement("option");
    node.value = option.id;
    node.textContent = option.label;
    node.selected = option.id === value;
    sel.append(node);
  }
  sel.addEventListener("change", () => onPick(sel.value));
  return sel;
}

function defaults(): HTMLElement {
  const { chosen, offer } = state;
  const box = el("section", "defaults");
  box.append(
    field(
      "Machine",
      choice(
        [
          { id: "", label: "this machine" },
          ...offer.machines.map((m) => ({ id: m.name, label: m.label })),
        ],
        chosen.machine,
        (machine) => set({ machine, repos: [] }),
      ),
    ),
  );
  box.append(
    field(
      "Agent",
      choice(
        [{ id: "", label: "default" }, ...offer.agents.map((a) => ({ id: a.id, label: a.label }))],
        chosen.agent,
        (agent) => set({ agent }),
      ),
    ),
  );
  box.append(
    field(
      "Model",
      choice(
        [
          { id: "", label: "default" },
          ...offer.models.map((m) => ({ id: m.id, label: m.label ?? m.id })),
        ],
        chosen.model,
        (model) => set({ model }),
      ),
    ),
  );
  box.append(
    field(
      "Role",
      choice(
        [
          { id: "", label: "the one Settings selects" },
          ...offer.roles.map((r) => ({ id: r, label: r })),
        ],
        chosen.role,
        (role) => set({ role }),
      ),
    ),
  );

  const sandbox = document.createElement("input");
  sandbox.type = "checkbox";
  sandbox.checked = chosen.sandbox;
  sandbox.addEventListener("change", () => set({ sandbox: sandbox.checked }));
  const toggle = el("label", "toggle");
  toggle.append(sandbox, el("span", "", "Sandboxed"));
  box.append(toggle);
  return box;
}

// The container's own section, under the grid: a path is long, and the
// columns are not. The file is the repository's, so every worktree gets it.
function mounts(): HTMLElement {
  const box = el("section", "sandbox");
  box.append(el("h2", "", "What the container gets"));
  box.append(
    control(
      { paths: state.chosen.mounts, env: state.chosen.env },
      {
        change: (draft) => set({ mounts: draft.paths, env: draft.env }),
        browse: () => host.postMessage({ kind: "browse" }),
      },
    ),
  );
  return box;
}

function repos(): HTMLElement {
  const { chosen, offer } = state;
  const row = rowFor(chosen.mode);
  const box = el("section", "repos");
  const head = el("div", "repos-head");
  head.append(
    el("h2", "", row.spans ? "Repositories" : "Repository"),
  );
  const search = document.createElement("input");
  search.className = "search";
  search.placeholder = "filter";
  search.value = filter;
  search.addEventListener("input", () => {
    filter = search.value;
    list.replaceChildren(...rows());
  });
  head.append(search);
  box.append(head);

  const list = el("div", "repo-list");
  const rows = (): HTMLElement[] => {
    const want = filter.trim().toLowerCase();
    const shown = offer.repos.filter(
      (repo) => !want || `${repo.owner ?? ""}/${repo.name}`.toLowerCase().includes(want),
    );
    if (shown.length === 0) return [el("p", "sub", "Nothing matches.")];
    return shown.map((repo) => {
      const at = chosen.repos.indexOf(repo.path);
      const one = el("button", at >= 0 ? "repo on" : "repo");
      one.append(el("span", "box", at >= 0 ? "✓" : ""));
      one.append(el("span", "repo-name", repo.name));
      one.append(el("span", "repo-owner", repo.owner ?? ""));
      if (at === 0) one.append(el("span", "pill", "home"));
      one.addEventListener("click", () => set({ repos: tick(state.chosen, repo.path) }));
      return one;
    });
  };
  list.replaceChildren(...rows());
  box.append(list);
  if (row.spans)
    box.append(
      el("p", "sub", "The first ticked is home: the agent runs there and the document goes there."),
    );
  return box;
}

function willMake(): HTMLElement {
  const box = el("section", "will");
  const { plan, chosen } = state;
  box.append(el("h2", "", plan.branch ? "What this will make" : "Where this runs"));
  if (plan.makes.length === 0) {
    box.append(el("p", "sub", "Nothing yet."));
    return box;
  }
  const list = el("div", "make-list");
  for (const make of plan.makes) {
    const one = el("div", "make");
    one.append(el("span", "make-repo", make.repo));
    one.append(el("span", "make-path", make.path));
    if (make.home) one.append(el("span", "pill", "home"));
    list.append(one);
  }
  box.append(list);
  if (plan.branch) {
    box.append(el("p", "sub", `Branch ${plan.branch} in each.`));
    const where = plan.makes[0]?.repo ?? "";
    box.append(el("p", "sub", designLine(plan, where)));
  }
  if (!chosen.sandbox) box.append(el("p", "sub", "Not sandboxed: the agent runs on this host."));
  return box;
}

function asked(): HTMLElement {
  const { chosen, offer } = state;
  const row = rowFor(chosen.mode);
  const box = el("section", "asked");

  if (row.names === "box") {
    const name = document.createElement("input");
    name.className = "name";
    name.placeholder = "cancel-running-process";
    name.value = chosen.name;
    name.addEventListener("input", () => {
      state.chosen = { ...state.chosen, name: name.value };
      changed();
    });
    box.append(field("Name the work", name));
  }

  if (row.names === "design") {
    box.append(
      field(
        "Design",
        offer.designs.length === 0
          ? el("p", offer.designNote ? "note" : "sub", designNone(offer, chosen))
          : documents(offer, chosen),
      ),
    );
    const one = held(offer.designs, chosen);
    if (one?.ship)
      box.append(
        el("p", "sub", `Copied to ${machineLabel(offer, chosen)} when it starts.`),
      );
  }

  const message = document.createElement("textarea");
  message.className = "message";
  message.rows = 4;
  message.placeholder = "What do you want done? Paste an image to send it too.";
  message.value = chosen.message;
  message.addEventListener("input", () => {
    state.chosen = { ...state.chosen, message: message.value };
  });
  message.addEventListener("paste", (event) => {
    const files = pasted((event as ClipboardEvent).clipboardData?.items);
    if (files.length === 0) return;
    event.preventDefault();
    void take(files);
  });
  box.append(field("First message", message));
  box.append(tray());

  if (offer.recent.length > 0) {
    const recent = el("div", "recent");
    for (const text of offer.recent.slice(0, 5)) {
      const one = el("button", "chip", text.split("\n")[0].slice(0, 60));
      one.title = text;
      one.addEventListener("click", () => set({ message: text }));
      recent.append(one);
    }
    box.append(recent);
  }
  return box;
}

// What a paste attached, each chip its own remove.
function tray(): HTMLElement {
  const box = el("div", "tray");
  for (const file of carrying) {
    const one = el("button", "chip", `${file.name} ✕`);
    one.addEventListener("click", () => {
      carrying = carrying.filter((kept) => kept !== file);
      refused = "";
      redrawTray();
    });
    box.append(one);
  }
  if (refused) box.append(el("p", "sub", refused));
  return box;
}

function redrawTray(): void {
  root.querySelector(".tray")?.replaceWith(tray());
}

async function take(files: File[]): Promise<void> {
  refused = "";
  for (const file of files) {
    const why = refuse(file, carrying.length);
    if (why) {
      refused = why;
      break;
    }
    carrying.push(await read(file));
  }
  redrawTray();
}

// Why the list is empty: a failed read is said rather than counted as none.
function designNone(offer: Offer, chosen: Chosen): string {
  if (offer.designNote) return offer.designNote;
  const dir = home(chosen);
  if (!dir) return "Tick a repository to list its documents.";
  const repo = offer.repos.find((one) => one.path === dir)?.name ?? dir;
  if (!chosen.machine) return `No design documents in ${repo} yet.`;
  // Said only for another machine: documents live in the git dir and are
  // never pushed, so an empty list there is not an empty list here.
  return `${machineLabel(offer, chosen)} has no design documents in ${repo}. They live in the git dir, so they do not travel between machines.`;
}

function designLine(plan: Plan, where: string): string {
  if (plan.carried) return `${plan.doc} copied into ${where}.`;
  if (plan.written) return `Carries on with ${plan.doc}, already in ${where}.`;
  return `Design document ${plan.doc} in ${where}.`;
}

function machineLabel(offer: Offer, chosen: Chosen): string {
  const found = offer.machines.find((one) => one.name === chosen.machine);
  return found?.label || chosen.machine;
}

// The two places a document can be, as one box. Grouped rather than merged: the
// same name on both machines is two documents, and here overwrites there.
function documents(offer: Offer, chosen: Chosen): HTMLElement {
  const sel = document.createElement("select");
  const blank = document.createElement("option");
  blank.value = "";
  blank.textContent = "pick a document";
  blank.selected = !held(offer.designs, chosen);
  sel.append(blank);

  const groups: [string, Doc[]][] = chosen.machine
    ? [
        [`Here — copied to ${machineLabel(offer, chosen)}`, offer.designs.filter((d) => d.ship)],
        [`Already on ${machineLabel(offer, chosen)}`, offer.designs.filter((d) => !d.ship)],
      ]
    : [["", offer.designs]];
  for (const [label, docs] of groups) {
    if (docs.length === 0) continue;
    const into = label ? document.createElement("optgroup") : sel;
    if (label) (into as HTMLOptGroupElement).label = label;
    for (const doc of docs) {
      const node = document.createElement("option");
      node.value = String(offer.designs.indexOf(doc));
      node.textContent = doc.file;
      node.selected = doc.file === chosen.design && doc.ship === chosen.ship;
      into.append(node);
    }
    if (into !== sel) sel.append(into);
  }
  sel.addEventListener("change", () => {
    const doc = offer.designs[Number(sel.value)];
    set(doc ? { design: doc.file, ship: doc.ship } : { design: "", ship: false });
  });
  return sel;
}

function foot(): HTMLElement {
  const box = el("footer", "foot");
  const why = missing(state.chosen);
  box.append(el("span", "summary", why ?? summary(state.chosen, state.plan)));
  const start = document.createElement("button");
  start.className = "start";
  start.textContent = state.busy ? "Starting…" : "Start";
  start.disabled = Boolean(why) || Boolean(state.busy);
  // Answered here, not on the way back: starting runs `ade create`, and
  // a button that waits seconds to change reads as one that missed.
  start.addEventListener("click", () => {
    if (state.busy) return;
    state = { ...state, busy: true, note: "Starting…" };
    draw();
    host.postMessage({ kind: "start", chosen: state.chosen, files: carrying });
  });
  box.append(start);
  return box;
}

function draw(): void {
  root.replaceChildren();
  if (state.note) root.append(el("p", "note", state.note));
  root.append(el("h1", "", "New session"));
  root.append(modes());
  const grid = el("div", "grid");
  const left = el("div", "col");
  left.append(asked(), defaults());
  const right = el("div", "col");
  if (rowFor(state.chosen.mode).picks) right.append(repos());
  right.append(willMake());
  grid.append(left, right);
  root.append(grid);
  if (state.chosen.sandbox) root.append(mounts());
  root.append(foot());
}

window.addEventListener(
  "message",
  (event: MessageEvent<{ kind?: string; path?: string } & Partial<Shown>>) => {
    const said = event.data;
    // A folder the host's dialog answered with: the same add as the row.
    if (said?.kind === "mounted" && said.path) {
      const next = added(
        { paths: state.chosen.mounts, env: state.chosen.env },
        said.path,
        false,
      );
      if ("draft" in next) set({ mounts: next.draft.paths, env: next.draft.env });
      return;
    }
    if (said?.kind === "state") {
      state = { ...state, ...said } as Shown;
      draw();
      return;
    }
    if (said?.kind === "plan" && said.plan) {
      state.plan = said.plan;
      // Only the parts the keystroke moved, so the box keeps its cursor.
      root.querySelector(".will")?.replaceWith(willMake());
      root.querySelector(".foot")?.replaceWith(foot());
    }
  },
);

host.postMessage({ kind: "ready" });

export type { Mode };
