import { plan } from "./plan";
import {
  GRILL,
  ROWS,
  fresh,
  home,
  issueOf,
  missing,
  onto,
  rowFor,
  summary,
  tick,
  type Chosen,
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

// Checkout is one the Open branch row offers. at is the path for a worktree
// and empty for a branch with none, which is made when it is opened.
interface Checkout {
  name: string;
  at: string;
  note: string;
}

interface Offer {
  repos: Repo[];
  agents: { id: string; label: string }[];
  checkouts?: Checkout[];
  checkoutNote?: string;
}

interface Shown {
  chosen: Chosen;
  plan: Plan;
  offer: Offer;
  busy?: boolean;
  note?: string;
}

const empty: Offer = { repos: [], agents: [] };
let state: Shown = { chosen: { ...fresh }, plan: { branch: "", makes: [] }, offer: empty };
let filter = "";

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
  state.plan = plan(state.chosen);
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
      set({ mode: row.mode, repos: onto(state.chosen, row.mode), branch: "", at: "" }),
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

// The CLI the session runs. Its model and its permissions are its own to set.
function agent(): HTMLElement {
  const box = el("section", "defaults");
  const sel = document.createElement("select");
  for (const option of state.offer.agents) {
    const node = document.createElement("option");
    node.value = option.id;
    node.textContent = option.label;
    node.selected = option.id === state.chosen.agent;
    sel.append(node);
  }
  sel.addEventListener("change", () => set({ agent: sel.value }));
  box.append(field("Agent", sel));
  return box;
}

function repos(): HTMLElement {
  const { chosen, offer } = state;
  const row = rowFor(chosen.mode);
  const box = el("section", "repos");
  const head = el("div", "repos-head");
  head.append(el("h2", "", row.spans ? "Repositories" : "Repository"));
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
      one.addEventListener("click", () =>
        set({ repos: tick(state.chosen, repo.path), branch: "", at: "" }),
      );
      return one;
    });
  };
  list.replaceChildren(...rows());
  box.append(list);
  if (row.spans)
    box.append(el("p", "sub", "The first ticked is home: the agent starts there; the rest are added as folders."));
  return box;
}

function willMake(): HTMLElement {
  const box = el("section", "will");
  const { plan, chosen } = state;
  const opening = rowFor(chosen.mode).names === "branch";
  const issue = issueOf(chosen);
  box.append(
    el("h2", "", (plan.branch || issue) && !opening ? "What this will make" : "Where this runs"),
  );
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
  if (plan.branch && opening) {
    box.append(el("p", "sub", chosen.at ? `On ${plan.branch}.` : `Checking ${plan.branch} out here.`));
  } else if (plan.branch) {
    box.append(el("p", "sub", `Branch ${plan.branch} in each.`));
  } else if (issue) {
    box.append(el("p", "sub", `Branch named after issue #${issue} once it is read.`));
  }
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

  if (row.names === "ticket") {
    const ticket = document.createElement("input");
    ticket.className = "name";
    ticket.placeholder = "FIX-623 or #12";
    ticket.value = chosen.ticket;
    // Not set(): a redraw takes the cursor out of the box.
    ticket.addEventListener("input", () => {
      state.chosen = { ...state.chosen, ticket: ticket.value };
      state.plan = plan(state.chosen);
      changed();
      root.querySelector(".will")?.replaceWith(willMake());
    });
    box.append(field("Ticket", ticket));
    box.append(el("p", "sub", "Its title and text are the first message unless you write one."));
  }

  if (row.names === "branch") {
    const held = offer.checkouts ?? [];
    box.append(
      field(
        "Branch",
        held.length === 0
          ? el("p", offer.checkoutNote ? "note" : "sub", branchNone(offer, chosen))
          : checkouts(held, chosen),
      ),
    );
    const one = held.find((each) => each.name === chosen.branch);
    if (one && !one.at)
      box.append(el("p", "sub", "No checkout yet: one is made for it when you start."));
  }

  const message = document.createElement("textarea");
  message.className = "message";
  message.rows = 4;
  message.placeholder = "What do you want done?";
  message.value = chosen.message;
  message.addEventListener("input", () => {
    state.chosen = { ...state.chosen, message: message.value };
  });
  box.append(field("First message", message));
  const presets = el("div", "recent");
  const grill = el("button", "chip", "Grill me");
  grill.title = GRILL;
  grill.addEventListener("click", () => set({ message: GRILL }));
  presets.append(grill);
  box.append(presets);
  return box;
}

// Why the list is empty, which for a branch is only ever "nothing ticked yet"
// or a read that failed: every repository has at least one.
function branchNone(offer: Offer, chosen: Chosen): string {
  if (offer.checkoutNote) return offer.checkoutNote;
  if (!home(chosen)) return "Tick a repository to list its branches.";
  return "Reading the branches…";
}

// The repository's checkouts and the branches without one, as one list: the
// difference is what starting does, not what you are picking.
function checkouts(held: Checkout[], chosen: Chosen): HTMLElement {
  const sel = document.createElement("select");
  const blank = document.createElement("option");
  blank.value = "";
  blank.textContent = "pick a branch";
  blank.selected = !chosen.branch;
  sel.append(blank);
  const groups: [string, Checkout[]][] = [
    ["Checked out", held.filter((one) => one.at)],
    ["No checkout yet", held.filter((one) => !one.at)],
  ];
  for (const [label, rows] of groups) {
    if (rows.length === 0) continue;
    const into = document.createElement("optgroup");
    into.label = label;
    for (const one of rows) {
      const node = document.createElement("option");
      node.value = String(held.indexOf(one));
      node.textContent = one.note ? `${one.name} — ${one.note}` : one.name;
      node.selected = one.name === chosen.branch;
      into.append(node);
    }
    sel.append(into);
  }
  sel.addEventListener("change", () => {
    const one = held[Number(sel.value)];
    set(one ? { branch: one.name, at: one.at } : { branch: "", at: "" });
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
  // Answered here, not on the way back: making a worktree takes seconds, and
  // a button that waits seconds to change reads as one that missed.
  start.addEventListener("click", () => {
    if (state.busy) return;
    state = { ...state, busy: true, note: "Starting…" };
    draw();
    host.postMessage({ kind: "start", chosen: state.chosen });
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
  left.append(asked(), agent());
  const right = el("div", "col");
  if (rowFor(state.chosen.mode).picks) right.append(repos());
  right.append(willMake());
  grid.append(left, right);
  root.append(grid);
  root.append(foot());
}

window.addEventListener("message", (event: MessageEvent<{ kind?: string } & Partial<Shown>>) => {
  const said = event.data;
  if (said?.kind !== "state") return;
  state = { ...state, ...said } as Shown;
  state.plan = plan(state.chosen);
  draw();
});

host.postMessage({ kind: "ready" });

export type { Mode };
