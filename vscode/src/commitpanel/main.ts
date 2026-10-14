import {
  bar,
  branchable,
  drifted,
  idle,
  loose,
  onBase,
  rebase,
  rows,
  said,
  signing,
  staged,
  suggest,
  verbs,
  where,
  widest,
  type Draft,
  type Row,
  type Shape,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("commit") as HTMLElement;

interface Carried {
  kind?: string;
  tree?: Shape;
  draft?: Draft;
  hasPR?: boolean;
  ahead?: number;
  behind?: number;
  base?: string;
  branch?: string;
  busy?: string;
  note?: string;
}

let all: Row[] = [];
let scale = 0;
let sign: Shape["signing"];
let draft: Draft | undefined;
let hasPR = false;
let ahead = 0;
let behind = 0;
let baseRef = "";
let branch = "";
let busy = "";
let note = "";
// What you typed, once you have. The agent's draft never overwrites it.
let typed: string | undefined;
// The branch name box: undefined until the form is open, so a suggestion can
// be drawn from the message without overwriting what you typed into it.
let wanted: string | undefined;

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function send(what: string, extra: Record<string, unknown> = {}): void {
  host.postMessage({ kind: "do", what, ...extra });
}

function message(): string {
  return typed ?? (draft ? joinDraft(draft) : "");
}

function joinDraft(d: Draft): string {
  return d.body ? `${d.title}\n\n${d.body}` : d.title;
}

function titleOf(text: string): string {
  return text.split("\n")[0]?.trim() ?? "";
}

function draw(): void {
  const checked = staged(all);
  root.replaceChildren();
  drawBranch();

  const box = el("textarea", "message") as HTMLTextAreaElement;
  box.value = busy === "redraft" ? "" : message();
  box.disabled = !idle(busy);
  if (busy === "redraft") box.placeholder = "Writing a message…";
  if (!busy) box.placeholder = "A title, a blank line, then the body.";
  box.rows = 20;
  box.addEventListener("input", () => {
    typed = box.value;
    paint();
  });
  root.append(el("h2", undefined, "Message"), box);

  const under = el("div", "row under");
  const why = el("span", "warn");
  if (drifted(draft, checked))
    why.textContent = "written for a different set of files";
  under.append(
    why,
    button(
      busy === "redraft" ? "Redrafting…" : "Redraft",
      () => send("redraft"),
      busy === "redraft",
    ),
  );
  root.append(under);

  root.append(el("h2", undefined, "Files"));
  const bulk = el("div", "row bulk");
  bulk.append(
    link(
      "Stage all",
      () => stageAll(true),
      loose(all).length === 0 || !idle(busy),
    ),
    link(
      "Unstage all",
      () => stageAll(false),
      checked.length === 0 || !idle(busy),
    ),
  );
  if (all.length > 0) root.append(bulk);
  scale = widest(all);
  for (const row of all) root.append(file(row));
  if (all.length === 0) root.append(el("p", "none", "Nothing has changed."));

  const can = verbs(titleOf(message()), checked, hasPR, ahead);
  const doing = el("div", "row verbs");
  doing.append(
    button("Commit", () => commit(false), !can.commit || !idle(busy)),
    button("Commit and push", () => commit(true), !can.commit || !idle(busy)),
  );
  if (can.createPR)
    doing.append(
      button("Create pull request", () => send("createPR"), !idle(busy)),
    );
  if (can.showPR)
    doing.append(
      button("Show the pull request", () => send("showPR"), !idle(busy)),
    );
  root.append(doing);
  const how = signing(sign);
  const under2 = el("p", "said");
  under2.append(el("span", undefined, said(checked, all)));
  under2.append(el("span", how.on ? "signed" : "unsigned", ` ${how.text}.`));
  root.append(under2);
  if (note) root.append(el("p", "note", note));
}

// drawBranch is where the commit would land. A base branch opens the form
// unasked: landing work on main is the mistake this section is here to catch.
function drawBranch(): void {
  const base = onBase(branch);
  const open = wanted !== undefined || base;
  root.append(el("h2", undefined, "Branch"));

  const line = el("div", "row branch");
  line.append(el("span", "where", branch || "an unknown branch"));
  if (base)
    line.append(
      el("span", "warn", "a base branch — start one to merge onto instead"),
    );
  if (!open) line.append(link("New branch", () => openBranch(), !idle(busy)));
  const onto = rebase(behind, loose(all).length + staged(all).length, baseRef);
  line.append(
    button(busy === "rebase" ? "Rebasing…" : onto.label, () => send("rebase"), !onto.can || !idle(busy)),
    el("span", "where", onto.why),
  );
  root.append(line);
  if (!open) return;

  // Named off the message until you type: the branch and the commit describe
  // the same work, so the suggestion tracks the draft and then stops.
  const name = wanted ?? suggest(titleOf(message()));
  const can = branchable(name, branch) && idle(busy);
  const form = el("div", "row branch");
  const box = el("input", "name") as HTMLInputElement;
  box.value = name;
  box.placeholder = "a-name-for-the-work";
  box.disabled = !idle(busy);
  box.addEventListener("input", () => {
    wanted = box.value;
    paint();
  });
  box.addEventListener("keydown", (event: KeyboardEvent) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    if (can) send("branch", { name });
  });
  form.append(
    box,
    button(
      busy === "branch" ? "Creating…" : "Create branch",
      () => send("branch", { name }),
      !can,
    ),
  );
  if (!base) form.append(link("Cancel", () => closeBranch(), !idle(busy)));
  root.append(form);
}

function openBranch(): void {
  wanted = suggest(titleOf(message()));
  paint();
}

function closeBranch(): void {
  wanted = undefined;
  paint();
}

function stageAll(on: boolean): void {
  const paths = on ? loose(all) : staged(all);
  if (paths.length === 0) return;
  for (const row of all) row.staged = on;
  send(on ? "stage" : "unstage", { paths });
  paint();
}

function commit(push: boolean): void {
  send("commit", { message: message(), push });
}

function button(label: string, go: () => void, off: boolean): HTMLElement {
  const node = el("button", undefined, label) as HTMLButtonElement;
  node.disabled = off;
  node.addEventListener("click", go);
  return node;
}

// link is a verb that changes what is on screen rather than doing the thing
// the page is for. A button beside Commit would read as another of those.
function link(label: string, go: () => void, off: boolean): HTMLElement {
  const node = el("button", "linkish", label) as HTMLButtonElement;
  node.disabled = off;
  node.addEventListener("click", go);
  return node;
}

function file(row: Row): HTMLElement {
  const line = el("label", "file");
  const tick = el("input") as HTMLInputElement;
  tick.type = "checkbox";
  tick.checked = row.staged;
  tick.disabled = !idle(busy);
  tick.addEventListener("change", () => {
    row.staged = tick.checked;
    send(tick.checked ? "stage" : "unstage", { paths: [row.path] });
    paint();
  });
  const at = where(row.path);
  const path = el("span", "path");
  if (at.dir) path.append(el("span", "dir", at.dir));
  path.append(el("span", "name", at.name));
  line.append(tick, path, churn(row), el("span", "status", row.status));
  return line;
}

// churn is the size of the change as a length. The numbers are in the tooltip:
// which files are big is the question a list of files is asked.
function churn(row: Row): HTMLElement {
  const size = bar(row, scale);
  const cell = el("span", "churn");
  if (!size.title) return cell;
  cell.title = size.title;
  for (const [cls, pct] of [
    ["added", size.added],
    ["removed", size.removed],
  ] as const) {
    if (pct <= 0) continue;
    const part = el("span", cls);
    part.style.width = `${pct}%`;
    cell.append(part);
  }
  return cell;
}

// paint redraws without losing the caret: the fields are replaced, so it is
// only done on a change that moves a button.
function paint(): void {
  const at = document.activeElement;
  const pick =
    at instanceof HTMLTextAreaElement
      ? "textarea"
      : at instanceof HTMLInputElement && at.classList.contains("name")
        ? "input.name"
        : "";
  draw();
  const back = pick ? root.querySelector(pick) : null;
  if (back instanceof HTMLTextAreaElement || back instanceof HTMLInputElement) {
    back.focus();
    back.setSelectionRange(back.value.length, back.value.length);
  }
}

window.addEventListener("message", (event: MessageEvent<Carried>) => {
  const said = event.data;
  if (said.kind !== "state") return;
  all = rows(said.tree);
  sign = said.tree?.signing;
  // A redraft replaces what the agent wrote, not what you typed over it.
  if (said.draft && (!draft || said.draft.title !== draft.title)) {
    if (busy === "redraft") typed = undefined;
    draft = said.draft;
  } else if (said.draft) {
    draft = said.draft;
  }
  hasPR = Boolean(said.hasPR);
  ahead = said.ahead ?? 0;
  behind = said.behind ?? 0;
  baseRef = said.base ?? "";
  // The form closes when the branch it asked for arrives, so what is on
  // screen is where you are rather than what you were about to do.
  if ((said.branch ?? "") !== branch) wanted = undefined;
  branch = said.branch ?? "";
  busy = said.busy ?? "";
  note = said.note ?? "";
  draw();
});

host.postMessage({ kind: "ready" });
