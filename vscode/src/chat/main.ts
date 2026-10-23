import {
  dropped,
  MAX_FILES,
  pasted,
  read,
  refuse,
  type Incoming,
} from "./files";
import { plainly } from "../client";
import { openable, splitLinks } from "./links";
import { copyable, copyWorthy } from "./copy";
import { avatar } from "./avatar";
import { answered, idle, questionsOf as sharpenedOf, type Question } from "./sharpen";
import { inputs, record } from "./mic";
import { base64, spliced } from "./wav";
import {
  type Act,
  acts,
  type Change,
  foldNote,
  folds,
  turnsAfter,
  undoSummary,
  type Asked,
  attachmentPath,
  clock,
  group,
  isQuestion,
  line,
  prose,
  questionsOf,
  step,
  type Message,
  opens,
  redraws,
  queuedLabel,
  type Side,
  sideLabel,
  selected,
  type Shown,
  shown,
  shotsOf,
  summary,
  who,
  working,
} from "./render";

// The base is on the element; the token is not. A webview's html is readable
// from any window's devtools, so the credential is posted in after load.
const root = document.getElementById("ade") as HTMLElement;
const base = root.dataset.base || "";
let token = "";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();

const list = document.getElementById("list") as HTMLElement;
const box = document.getElementById("box") as HTMLTextAreaElement;
const send = document.getElementById("send") as HTMLButtonElement;
const stop = document.getElementById("stop") as HTMLButtonElement;
const mic = document.getElementById("mic") as HTMLButtonElement;
const tray = document.getElementById("tray") as HTMLElement;
const note = document.getElementById("note") as HTMLElement;
const sharpen = document.getElementById("sharpen") as HTMLElement;

let held: Incoming[] = [];
// A selection an editor put on the next message. Taken off the server once
// and shown here, so it goes with the message rather than to another window.
let context: { from: string; text: string } | undefined;
let messages: Message[] = [];
let running = false;
// under is the welded prompts by the one each went with; weldedFirst is the one running.
let under = new Map<string, Message[]>();
let sides: Side[] = [];
let queued = 0;
let weldedFirst = "";
// asking is the tool call id of every question the agent is waiting on.
let asking = new Map<string, { toolId: string; tool: string; rule?: string }>();
// The design document the session has open, or "" for none. A named agent's
// reply can be folded into it, which is what the button under one does.
let doc = "";
// pinned is whether the list was at the bottom, so a reply follows you down but
// does not yank you back while you are reading up.
let pinned = true;

function url(path: string): string {
  return `${base}${path}${path.includes("?") ? "&" : "?"}t=${encodeURIComponent(token)}`;
}

// The editor makes every request. A webview renders on the machine you are
// sitting at, which in a remote window is not the machine ade is on.
interface Answer {
  id?: string;
  ok?: boolean;
  status?: number;
  text?: string;
  error?: string;
}
const waiting = new Map<string, (answer: Answer) => void>();
let asked = 0;

function ask(
  path: string,
  init: { method?: string; body?: string } = {},
): Promise<Answer> {
  const id = String(++asked);
  return new Promise<Answer>((settle) => {
    waiting.set(id, settle);
    host.postMessage({ kind: "api", id, path, method: init.method, body: init.body });
  });
}

async function get<T>(path: string): Promise<T> {
  const res = await ask(path);
  // The body is where ade says why. Without it a proxy's 404 reads as
  // "/api/state: 404", which names the route and not the reason.
  if (!res.ok) {
    throw new Error(res.error ?? plainly(path, res.status ?? 0, res.text ?? ""));
  }
  return JSON.parse(res.text || "{}") as T;
}

// The two notes about the stream rather than the conversation: a fetch
// that lands clears these and nothing else.
const CONNECTING = "connecting…";
const LOST = "lost the connection to ade — reconnecting";

function say(text: string): void {
  note.textContent = text;
  note.classList.toggle("on", text !== "");
}

// reached is a read that just worked, so the page is plainly connected.
// The note sat until the stream next changed, which can be never.
function reached(): void {
  if (note.textContent === CONNECTING || note.textContent === LOST) say("");
}

// open runs are remembered across a redraw: an event arrives every few
// seconds, and a block that shuts under you while reading it is unusable.
const unfolded = new Set<string>();

// hidden is the replies folded to their heading, kept across a redraw.
const hidden = new Set<string>();

// full is the replies shown as the agent wrote them: per message, kept across a redraw.
const full = new Set<string>();

// undoing is the undo waiting on your confirmation: nothing is put back
// until you have seen which files it touches.
let undoing: { id: string; changes: Change[] } | undefined;

// A redraw rebuilds every row, destroying a selection anchored in one.
let deferred = false;

// closed is a session exited on purpose, so a failed poll is expected.
let closed = false;

function draw(): void {
  if (selecting()) {
    deferred = true;
    return;
  }
  deferred = false;
  const anchor = pinned ? undefined : anchored();
  list.innerHTML = "";
  const shown = folds(messages);
  under = shown.under;
  for (const block of group(shown.top, new Set(asking.keys()))) {
    if (block.kind === "msg" && isQuestion(block.msg)) {
      list.append(question(block.msg));
      continue;
    }
    if (block.kind === "msg" && asking.has(block.msg.toolId || "")) {
      list.append(permission(block.msg));
      continue;
    }
    list.append(block.kind === "tools" ? tools(block.msgs) : row(block.msg));
  }
  if (working(messages, running, asking.size)) list.append(spinner());
  for (const side of sides) list.append(sideRow(side));
  if (queued > 0) {
    const row = document.createElement("div");
    row.className = "side-row";
    row.textContent = queuedLabel(queued);
    list.append(row);
  }
  if (pinned) list.scrollTop = list.scrollHeight;
  else if (anchor) hold(anchor);
}

// anchored is the first row in view and where its top sits: a redraw that
// changes a height above it, a short form landing, must not move what you read.
function anchored(): { id: string; top: number } | undefined {
  const edge = list.getBoundingClientRect().top;
  for (const el of rows()) {
    const box = el.getBoundingClientRect();
    if (box.bottom > edge) return { id: el.dataset.id || "", top: box.top - edge };
  }
  return undefined;
}

function hold(anchor: { id: string; top: number }): void {
  const el = rows().find((r) => r.dataset.id === anchor.id);
  if (!el) return;
  const edge = list.getBoundingClientRect().top;
  list.scrollTop += el.getBoundingClientRect().top - edge - anchor.top;
}

function rows(): HTMLElement[] {
  return Array.from(list.querySelectorAll<HTMLElement>("[data-id]"));
}

function selecting(): boolean {
  return selected(list, document.getSelection());
}

document.addEventListener("selectionchange", () => {
  if (deferred && !selecting()) draw();
});

// Held outside the redraw: an event every few seconds would otherwise wipe
// a half-made choice.
const picked = new Map<string, string[]>();

function question(msg: Message): HTMLElement {
  const el = document.createElement("div");
  const live = asking.has(msg.toolId || "");
  el.className = `question${live ? " open" : ""}`;
  const asked = questionsOf(msg.input);

  for (const one of asked) {
    const box = document.createElement("div");
    box.className = "question-one";
    if (one.header) {
      const tag = document.createElement("div");
      tag.className = "question-header";
      tag.textContent = one.header;
      box.append(tag);
    }
    const text = document.createElement("div");
    text.className = "question-text";
    text.textContent = one.question;
    box.append(text);

    const row = document.createElement("div");
    row.className = "question-options";
    for (const option of one.options || []) {
      const button = document.createElement("button");
      button.className = "question-option";
      button.textContent = option.label;
      button.title = option.description || "";
      button.disabled = !live;
      const chosen = picked.get(one.question) || [];
      if (chosen.includes(option.label)) button.classList.add("on");
      button.onclick = () => {
        const now = picked.get(one.question) || [];
        const next = one.multiSelect
          ? now.includes(option.label)
            ? now.filter((l) => l !== option.label)
            : [...now, option.label]
          : [option.label];
        picked.set(one.question, next);
        draw();
      };
      row.append(button);
    }
    box.append(row);
    el.append(box);
  }

  if (!live) {
    const done = document.createElement("div");
    done.className = "question-answer";
    done.textContent = msg.output || "not answered";
    el.append(done);
    return el;
  }

  const answer = document.createElement("button");
  answer.className = "question-answer-btn";
  answer.textContent = "Answer";
  answer.disabled = asked.some(
    (one) => !(picked.get(one.question) || []).length,
  );
  answer.onclick = () => void sendAnswer(msg, asked);
  el.append(answer);
  return el;
}

async function sendAnswer(msg: Message, asked: Asked[]): Promise<void> {
  const answers: Record<string, string> = {};
  for (const one of asked)
    answers[one.question] = (picked.get(one.question) || []).join(", ");
  try {
    const res = await ask("/api/chat/answer", {
      method: "POST",
      body: JSON.stringify({ toolId: msg.toolId, answers }),
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    for (const one of asked) picked.delete(one.question);
    await refresh();
  } catch (err) {
    say(String(err));
  }
}

// permission is a tool call the agent is holding. The rule is editable, and
// what is remembered goes to ~/.ade/permissions.json, not Claude's settings.
function permission(msg: Message): HTMLElement {
  const el = document.createElement("div");
  el.className = "question open";

  const head = document.createElement("div");
  head.className = "question-text";
  head.textContent =
    `${msg.tool || "A tool"} \u00b7 ${msg.target || ""}`.trim();
  el.append(head);

  const rule = asking.get(msg.toolId || "")?.rule;
  const edit = document.createElement("input");
  if (rule) {
    edit.className = "question-rule";
    edit.value = rule;
    edit.setAttribute("aria-label", "the rule to remember");
    el.append(edit);
  }

  const why = document.createElement("input");
  why.className = "question-reason";
  why.placeholder = "why not, if you are saying no \u2014 the agent reads this";
  el.append(why);

  const row = document.createElement("div");
  row.className = "question-options";
  const choices: [string, Verdict, string][] = [
    ["Allow once", { allow: true }, ""],
  ];
  if (rule) {
    choices.push([
      "This session",
      { allow: true, remember: "session" },
      "for the rest of this session \u2014 nothing is written to disk",
    ]);
    choices.push([
      "Always allow",
      { allow: true, remember: "always" },
      "to ~/.ade/permissions.json, so this is never asked again",
    ]);
  }
  choices.push(["Deny", { allow: false }, ""]);
  if (rule) {
    choices.push([
      "Always deny",
      { allow: false, remember: "always", denyRule: true },
      "to the deny list in ~/.ade/permissions.json",
    ]);
  }
  choices.push([
    "Run free",
    { allow: true, free: true },
    "Allow this and stop asking for the rest of this session. Your deny rules still hold.",
  ]);

  for (const [label, verdict, hint] of choices) {
    const button = document.createElement("button");
    const primary = verdict.allow && !verdict.remember && !verdict.free;
    button.className = primary ? "question-answer-btn" : "question-option";
    if (!verdict.allow || verdict.free) button.classList.add("danger");
    button.textContent = label;
    if (hint)
      button.title = verdict.free ? hint : `${label} ${edit.value} ${hint}`;
    button.onclick = () =>
      void decide(msg, { ...verdict, reason: why.value, rule: edit.value });
    row.append(button);
  }
  el.append(row);
  return el;
}

// Verdict is what the card sends: the answer, what to remember of it, and
// whether to stop asking altogether.
type Verdict = {
  allow: boolean;
  remember?: string;
  reason?: string;
  rule?: string;
  denyRule?: boolean;
  free?: boolean;
};

async function decide(msg: Message, verdict: Verdict): Promise<void> {
  const remember = verdict.remember || "";
  try {
    const res = await ask("/api/chat/decide", {
      method: "POST",
      body: JSON.stringify({
        toolId: msg.toolId,
        allow: verdict.allow,
        remember,
        reason: verdict.reason || "",
        rule: remember ? verdict.rule || "" : "",
        denyRule: !!verdict.denyRule,
        free: !!verdict.free,
      }),
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    await refresh();
  } catch (err) {
    say(String(err));
  }
}

// tools is one run, collapsed. The id of the first call keys it, so the same
// run stays open across redraws and a new one does not inherit the state.
function tools(msgs: Message[]): HTMLElement {
  const el = document.createElement("details");
  el.className = "tools";
  const key = msgs[0].id;
  el.dataset.id = key;
  el.open = unfolded.has(key) || opens(msgs);
  el.ontoggle = () => (el.open ? unfolded.add(key) : unfolded.delete(key));

  const head = document.createElement("summary");
  head.textContent = summary(msgs);
  if (msgs.some((m) => m.failed)) head.classList.add("failed");
  el.append(head);

  for (const msg of msgs) {
    const one = document.createElement("div");
    one.className = `tool${msg.failed ? " failed" : ""}`;
    one.textContent = line(msg);
    el.append(one);
  }
  return el;
}

function row(msg: Message): HTMLElement {
  if (msg.kind === "note") {
    // A note that carries output is a failure worth reading: folded, so a
    // passing conversation is not a wall of build log.
    if (msg.output) {
      const el = document.createElement("details");
      el.className = `note-row failed`;
      // Remembered across a redraw, as an open run is: the poll is every few
      // seconds and output that shuts while you read it is unusable.
      el.open = unfolded.has(msg.id);
      el.ontoggle = () =>
        el.open ? unfolded.add(msg.id) : unfolded.delete(msg.id);
      const head = document.createElement("summary");
      head.textContent = msg.body || "";
      const out = document.createElement("pre");
      out.textContent = msg.output;
      linkify(out);
      el.append(head, out);
      return el;
    }
    const el = document.createElement("div");
    el.className = `note-row${msg.failed ? " failed" : ""}`;
    el.textContent = msg.body || "";
    return el;
  }

  const el = document.createElement("div");
  el.className = `msg ${msg.kind === "you" ? "you" : "agent"}`;
  el.dataset.id = msg.id;

  const face = document.createElement("div");
  face.className = "face";
  face.innerHTML = avatar(msg);

  const side = document.createElement("div");
  side.className = "said";
  const head = document.createElement("div");
  head.className = "head";
  head.textContent = `${who(msg)} · ${clock(msg.at)}`;
  if (hidden.has(msg.id)) {
    el.classList.add("hid");
    const show = button("linkact", "Show", () => {
      hidden.delete(msg.id);
      draw();
    });
    show.title = "Show this reply again";
    head.append(" · ", show);
    side.append(head);
    el.append(face, side);
    return el;
  }
  const body = document.createElement("div");
  body.className = "body";
  // The server renders the agent's prose; a prompt is yours and never html, so
  // a url in one is made an anchor rather than left as text.
  const html = prose(msg, full.has(msg.id));
  if (msg.kind !== "you" && html) {
    body.innerHTML = html;
    linkify(body);
    addCopyButtons(body);
  } else body.append(linked(msg.body || ""));
  side.append(head, body);

  const carried = shown(msg, (path) => url(attachmentPath(path)));
  if (carried.length > 0) side.append(attachments(carried));
  const welded = under.get(msg.id);
  if (welded) side.append(foldedUnder(msg, welded));
  const verbs = acts(msg, doc);
  if (verbs.length) side.append(actRow(msg, verbs));
  if (undoing?.id === msg.id) side.append(undoPanel(msg, undoing.changes));

  el.append(face, side);
  return el;
}

// One button per block: taking a command out of a long reply is a click.
function addCopyButtons(body: HTMLElement): void {
  for (const pre of Array.from(body.querySelectorAll("pre"))) {
    const code = pre.querySelector("code");
    const text = copyable((code ?? pre).textContent ?? "");
    if (!copyWorthy(pre.className, text)) continue;

    const button = document.createElement("button");
    button.className = "copycode";
    button.textContent = "Copy";
    button.title = "Copy this block";
    button.onclick = (event) => {
      event.stopPropagation();
      host.postMessage({ kind: "copy", text });
      button.textContent = "Copied";
      setTimeout(() => (button.textContent = "Copy"), 1200);
    };

    // A pre scrolls sideways and would carry the button off with it.
    const wrap = document.createElement("div");
    wrap.className = "block";
    pre.replaceWith(wrap);
    wrap.append(pre, button);
  }
}

// foldedUnder draws the prompts welded to one, and the way to take them apart.
function foldedUnder(first: Message, rest: Message[]): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "folded";
  for (const msg of rest) {
    const el = document.createElement("div");
    el.className = "folded-msg";
    el.append(linked(msg.body || ""));
    const carried = shown(msg, (path) => url(attachmentPath(path)));
    if (carried.length > 0) el.append(attachments(carried));
    wrap.append(el);
  }
  const note = document.createElement("div");
  note.className = "fold-note";
  note.textContent = foldNote(rest.length + 1);
  if (running && first.id === weldedFirst) {
    const apart = button("linkact", "Run them separately", () => {
      apart.disabled = true;
      void separate(first.id);
    });
    apart.title = "Stop this turn and run the first prompt alone, the rest after it";
    note.append(" · ", apart);
  }
  wrap.append(note);
  return wrap;
}

async function separate(id: string): Promise<void> {
  const res = await ask(`/api/chat/${id}/separate`, { method: "POST" });
  if (!res.ok) say(res.error ?? `could not run them separately (${res.status})`);
}

// Pictures are drawn in the conversation; anything else is a chip. A webview
// cannot zoom, so a click asks the editor to open the file instead.
function attachments(items: Shown[]): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "shots";
  for (const item of items) {
    const button = document.createElement("button");
    button.title = item.path ? `Open ${item.name}` : item.name;
    button.onclick = () =>
      item.image ? openShot(item.src) : reveal(item.path);
    if (item.image) {
      button.className = "shotbtn";
      const img = document.createElement("img");
      img.className = "shot";
      img.src = item.src;
      img.alt = item.name;
      button.append(img);
    } else {
      button.className = "chip file";
      button.textContent = item.name;
    }
    wrap.append(button);
  }
  return wrap;
}

// spans fills a node with text and anchors. The list listens for the click: an
// anchor in a webview navigates nowhere by itself.
function spans(into: Node, text: string): void {
  for (const span of splitLinks(text)) {
    if (!span.href) {
      into.appendChild(document.createTextNode(span.text));
      continue;
    }
    const a = document.createElement("a");
    a.href = span.href;
    a.textContent = span.text;
    into.appendChild(a);
  }
}

// linked is your own words with the urls in them turned into anchors.
function linked(text: string): HTMLElement {
  const p = document.createElement("p");
  spans(p, text);
  return p;
}

// linkify makes a url clickable wherever it was printed: goldmark anchors one
// in prose and leaves one in a fence, in inline code or in output as text.
function linkify(root: HTMLElement): void {
  const walk = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const found: Text[] = [];
  for (let node = walk.nextNode(); node; node = walk.nextNode()) {
    const text = node as Text;
    if (text.parentElement?.closest("a")) continue;
    if (splitLinks(text.data).some((span) => span.href)) found.push(text);
  }
  for (const text of found) {
    const frag = document.createDocumentFragment();
    spans(frag, text.data);
    text.replaceWith(frag);
  }
}

// Fold a named agent's reply into the open document. The server decides what
// of it applies; this only says which reply.
function actRow(msg: Message, verbs: Act[]): HTMLElement {
  const row = document.createElement("div");
  row.className = "acts";
  for (const verb of verbs) {
    if (verb === "fold") {
      const chip = button("chip", "Update doc", () => void fold(msg.id, chip));
      chip.title = `Fold this into ${doc}`;
      row.append(chip);
      continue;
    }
    if (verb === "full") {
      const showing = full.has(msg.id);
      const chip = button("linkact", showing ? "Short" : "Full", () => {
        if (showing) full.delete(msg.id);
        else full.add(msg.id);
        draw();
      });
      chip.title = showing
        ? "Show the short form"
        : "Show the reply as the agent wrote it";
      row.append(chip);
      continue;
    }
    if (verb === "copy") {
      const chip = button("linkact", "Copy", () => {
        host.postMessage({ kind: "copy", text: msg.body || "" });
        said(chip, "Copied");
      });
      chip.title = "Copy this reply";
      row.append(chip);
      continue;
    }
    if (verb === "hide") {
      const chip = button("linkact", "Hide", () => {
        hidden.add(msg.id);
        draw();
      });
      chip.title = "Fold this reply to its heading";
      row.append(chip);
      continue;
    }
    if (verb === "undo") {
      const chip = button("linkact", msg.undone ? "Redo" : "Undo", () => {
        void planUndo(msg, chip);
      });
      chip.title = msg.undone
        ? "Put back the file changes this undo removed"
        : "Put the files back to how they were before this message";
      row.append(chip);
      continue;
    }
    if (verb === "fixture") {
      const chip = button("linkact", "Save as a fixture", () => {
        void saveFixture(msg, chip);
      });
      chip.title =
        "Keep this prompt and the tree it ran on, to replay after editing CLAUDE.md";
      row.append(chip);
      continue;
    }
    const chip = button("linkact", "Replay", () => void replay(msg, chip));
    chip.title = "Send this message again, as it was";
    row.append(chip);
  }
  return row;
}

// said flashes what a button did, then puts its word back: a webview has no
// toast, and a verb with no answer reads as one that did not run.
function said(chip: HTMLButtonElement, word: string): void {
  const was = chip.textContent;
  chip.textContent = word;
  setTimeout(() => {
    chip.textContent = was;
  }, 1200);
}

// replay sends one of your messages again, as it was. Queued while the agent
// is running, which is what the composer does with anything typed then.
async function replay(msg: Message, chip: HTMLButtonElement): Promise<void> {
  chip.disabled = true;
  try {
    const res = await ask("/api/chat", {
      method: "POST",
      body: JSON.stringify({
        message: msg.body || "",
        images: msg.images,
        queue: running,
      }),
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    said(chip, "Sent");
  } catch (err) {
    said(chip, String(err));
  }
  chip.disabled = false;
}

// planUndo asks what an undo would change and shows it. Nothing is changed.
async function planUndo(msg: Message, chip: HTMLButtonElement): Promise<void> {
  chip.disabled = true;
  try {
    const res = await ask(`/api/turns/${msg.id}/undo`);
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    const plan = JSON.parse(res.text || "{}") as { changes: Change[] };
    if (plan.changes.length === 0) {
      said(chip, "Nothing to put back");
    } else {
      undoing = { id: msg.id, changes: plan.changes };
      draw();
    }
  } catch (err) {
    say(String(err));
  }
  chip.disabled = false;
}

const UNDO_LISTED = 12;

function undoPanel(msg: Message, changes: Change[]): HTMLElement {
  const panel = document.createElement("div");
  panel.className = "undo";
  const what = document.createElement("div");
  what.textContent = undoSummary(
    changes,
    msg.undone ? 0 : turnsAfter(messages, msg.id),
  );
  const files = document.createElement("ul");
  for (const change of changes.slice(0, UNDO_LISTED)) {
    const item = document.createElement("li");
    item.textContent = `${change.do} ${change.path}`;
    files.append(item);
  }
  if (changes.length > UNDO_LISTED) {
    const more = document.createElement("li");
    more.textContent = `and ${changes.length - UNDO_LISTED} more`;
    files.append(more);
  }
  const row = document.createElement("div");
  row.className = "acts";
  const go = button("chip", msg.undone ? "Redo" : "Undo", () => {
    void undo(msg, go);
  });
  const not = button("linkact", "Cancel", () => {
    undoing = undefined;
    draw();
  });
  row.append(go, not);
  panel.append(what, files, row);
  return panel;
}

async function undo(msg: Message, chip: HTMLButtonElement): Promise<void> {
  chip.disabled = true;
  try {
    const res = await ask(`/api/turns/${msg.id}/undo`, {
      method: "POST",
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    undoing = undefined;
    say("");
    await refresh();
  } catch (err) {
    say(String(err));
    chip.disabled = false;
  }
}

// saveFixture captures the turn under a name ade picks from the prompt; the
// chip says which, since the next step is editing its expect.yaml.
async function saveFixture(
  msg: Message,
  chip: HTMLButtonElement,
): Promise<void> {
  chip.disabled = true;
  try {
    const res = await ask(`/api/turns/${msg.id}/fixture`, { method: "POST" });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    const saved = JSON.parse(res.text || "{}") as { name?: string };
    said(chip, `Saved as ${saved.name ?? "a fixture"}`);
  } catch (err) {
    say(String(err));
  }
  chip.disabled = false;
}

async function fold(id: string, chip: HTMLButtonElement): Promise<void> {
  chip.disabled = true;
  try {
    const res = await ask("/api/design/fold", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    say("");
    await refresh();
  } catch (err) {
    say(String(err));
    chip.disabled = false;
  }
}

// An outbox picture is bytes on the message with nothing on disk behind it,
// so there is nothing for the editor to open.
function reveal(path: string | undefined): void {
  if (path) host.postMessage({ kind: "open", path });
}

// The lightbox. One for the conversation, as the app's own gallery is: the
// arrows walk every picture in the thread, not the message it opened from.
let lightbox: HTMLElement | undefined;
let shotAt: number | null = null;

const shots = (): Shown[] =>
  shotsOf(messages, (path) => url(attachmentPath(path)));

function openShot(src: string): void {
  const found = shots().findIndex((shot) => shot.src === src);
  if (found < 0) return;
  shotAt = found;
  drawShot();
}

function stepShot(by: number): void {
  const all = shots();
  if (shotAt === null || all.length === 0) return;
  shotAt = (shotAt + by + all.length) % all.length;
  drawShot();
}

function closeShot(): void {
  shotAt = null;
  lightbox?.remove();
  lightbox = undefined;
}

function drawShot(): void {
  const all = shots();
  const shot = shotAt === null ? undefined : all[shotAt];
  if (!shot) {
    closeShot();
    return;
  }

  lightbox?.remove();
  const box = document.createElement("div");
  box.className = "lightbox";
  // The backdrop closes; a click on the picture or the bar must not.
  box.onclick = (event) => {
    if (event.target === box) closeShot();
  };

  const bar = document.createElement("div");
  bar.className = "lightbox-bar";
  bar.append(text("lightbox-name", shot.name));
  if (all.length > 1) {
    bar.append(text("lightbox-count", `${(shotAt ?? 0) + 1} of ${all.length}`));
  }
  if (shot.path) {
    bar.append(button("chip", "Open in editor", () => reveal(shot.path)));
  }
  bar.append(button("chip", "Close", closeShot));

  const img = document.createElement("img");
  img.className = "lightbox-shot";
  img.src = shot.src;
  img.alt = shot.name;
  box.append(bar, img);

  if (all.length > 1) {
    box.append(
      button("lightbox-step prev", "‹", () => stepShot(-1)),
      button("lightbox-step next", "›", () => stepShot(1)),
    );
  }

  document.body.append(box);
  lightbox = box;
}

function text(cls: string, body: string): HTMLElement {
  const el = document.createElement("span");
  el.className = cls;
  el.textContent = body;
  return el;
}

function button(
  cls: string,
  body: string,
  onClick: () => void,
): HTMLButtonElement {
  const el = document.createElement("button");
  el.className = cls;
  el.textContent = body;
  el.onclick = (event) => {
    event.stopPropagation();
    onClick();
  };
  return el;
}

// A link in a bubble, wherever it came from: the agent's rendered markdown or a
// url you typed. The editor opens it, because a webview cannot navigate.
// stopPropagation as well as preventDefault: the webview host has an anchor
// handler of its own on document.body, and a click that reaches it opens twice.
const openLink = (event: MouseEvent) => {
  const a = (event.target as HTMLElement | null)?.closest?.("a");
  const href = a?.getAttribute("href");
  if (!href) return;
  event.preventDefault();
  event.stopPropagation();
  if (openable(href)) host.postMessage({ kind: "link", href });
};
list.addEventListener("click", openLink);
// Middle click is the host's other way in, and it opens without asking.
list.addEventListener("auxclick", openLink);

// Registered once rather than per open: a listener added on each is a listener
// leaked on each.
document.addEventListener("keydown", (event) => {
  if (shotAt === null) return;
  if (event.key === "Escape") closeShot();
  if (event.key === "ArrowLeft") stepShot(-1);
  if (event.key === "ArrowRight") stepShot(1);
});

// A named agent runs beside the main one, so the conversation has to say it is
// there: without this the only sign is the refusal when the slots run out.
function sideRow(side: Side): HTMLElement {
  const el = document.createElement("div");
  el.className = "side-row";
  const what = document.createElement("span");
  what.textContent = sideLabel(side, Date.now());
  const stop = button("linkact", "Stop", () => {
    stop.disabled = true;
    void stopSide(side.id);
  });
  stop.title = `Stop @${side.name} and leave the rest running`;
  el.append(what, stop);
  return el;
}

async function stopSide(id: string): Promise<void> {
  const res = await ask(`/api/sides/${encodeURIComponent(id)}/cancel`, { method: "POST" });
  if (!res.ok) say(res.error ?? `could not stop it (${res.status})`);
  await refresh();
}

function spinner(): HTMLElement {
  const el = document.createElement("div");
  el.className = "spinner";
  el.textContent = step(messages, Date.now());
  return el;
}

// The age moves between events, which is the whole point of showing it.
setInterval(() => {
  const el = list.querySelector(".spinner");
  if (el) el.textContent = step(messages, Date.now());
  const rows = list.querySelectorAll(".side-row span");
  rows.forEach((node, i) => {
    if (sides[i]) node.textContent = sideLabel(sides[i], Date.now());
  });
}, 1000);

function drawTray(): void {
  tray.innerHTML = "";
  tray.classList.toggle("on", held.length > 0 || context !== undefined);

  if (context) {
    const chip = document.createElement("button");
    chip.className = "chip context";
    chip.textContent = `${context.from || "highlighted"} ✕`;
    chip.title = context.text.slice(0, 400);
    chip.onclick = () => {
      context = undefined;
      drawTray();
    };
    tray.append(chip);
  }

  held.forEach((file, at) => {
    const chip = document.createElement("button");
    chip.className = "chip";
    chip.textContent = `${file.name} ✕`;
    chip.title = "Take this off the message";
    chip.onclick = () => {
      held.splice(at, 1);
      drawTray();
    };
    tray.append(chip);
  });
}

async function take(files: File[]): Promise<void> {
  for (const file of files) {
    const no = refuse(file, held.length);
    if (no) {
      say(no);
      return;
    }
    held.push(await read(file));
  }
  drawTray();
  say("");
}

// The sharpener: what a cheap model asks about the prompt, before it is sent.
let settling: ReturnType<typeof setTimeout> | undefined;
let looked = "";
let looks = 0;

function drawQuestions(questions: Question[]): void {
  sharpen.replaceChildren();
  sharpen.classList.toggle("on", questions.length > 0);
  for (const question of questions) {
    const row = document.createElement("div");
    row.className = "sharpen-row";
    const asked = document.createElement("span");
    asked.textContent = question.ask;
    row.append(asked);
    for (const option of question.options) {
      const pick = document.createElement("button");
      pick.className = "pick";
      pick.textContent = option;
      pick.title = "Add this answer to the prompt";
      pick.onclick = () => {
        box.value = answered(box.value, question.ask, option);
        looked = box.value.trim();
        drawQuestions(questions.filter((q) => q !== question));
        box.focus();
      };
      row.append(pick);
    }
    sharpen.append(row);
  }
}

// look never reports a failure: no answer and no questions draw the same.
async function look(): Promise<void> {
  const prompt = box.value.trim();
  if (prompt === looked) return;
  looked = prompt;
  const mine = ++looks;
  if (!prompt) return drawQuestions([]);
  const res = await ask("/api/sharpen", {
    method: "POST",
    body: JSON.stringify({ prompt }),
  }).catch(() => undefined);
  if (mine === looks) drawQuestions(res?.ok ? sharpenedOf(res.text) : []);
}

// sending guards the post, not the button: the button is disabled for
// it and Enter is not, so a mashed return sent the turn twice.
let sending = false;

async function submit(): Promise<void> {
  if (sending) return;
  const message = box.value.trim();
  if (!message && held.length === 0) return;
  sending = true;
  // A look still out is for a prompt that is now sent: its answer is dropped.
  clearTimeout(settling);
  looks++;

  const sent = { files: held, context };
  // On screen before the post, not after: the server answers when it has
  // taken the turn, and waiting read as a missed keypress.
  messages = [
    ...messages,
    {
      id: pendingID,
      kind: "you",
      body: message,
      at: new Date().toISOString(),
      files: held.map((f) => ({ name: f.name, path: f.name })),
    },
  ];
  box.value = "";
  held = [];
  looked = "";
  drawQuestions([]);
  context = undefined;
  drawTray();
  pinned = true;
  say("");
  send.disabled = true;
  draw();
  try {
    const res = await ask("/api/chat", {
      method: "POST",
      body: JSON.stringify({ message, files: sent.files, queue: running, context: sent.context }),
    });
    if (!res.ok) throw new Error(res.text || res.error || String(res.status));
    // The real one, with the id and whatever the server made of it.
    await refresh();
  } catch (err) {
    // It never went: take the pending one back off and give the words
    // back, because the box is the only copy.
    messages = messages.filter((m) => m.id !== pendingID);
    box.value = message;
    held = sent.files;
    context = sent.context;
    drawTray();
    say(String(err));
    draw();
  } finally {
    sending = false;
    send.disabled = false;
    box.focus();
  }
}

// The one drawn before the server has answered. Replaced by the real
// message on the refresh that follows, or taken off if the post failed.
const pendingID = "pending";

// cancel is the turn and nothing more: the server and the agent stay up.
async function cancel(): Promise<void> {
  stop.disabled = true;
  try {
    const res = await ask("/api/cancel", {
      method: "POST",
    });
    // 400 is "nothing is running": the turn ended first, which is no failure.
    if (!res.ok && res.status !== 400) {
      throw new Error(res.text || res.error || String(res.status));
    }
    say("");
    await refresh();
  } catch (err) {
    say(String(err));
  } finally {
    stop.disabled = false;
  }
}

async function refresh(): Promise<void> {
  if (closed) return;
  try {
    // One read does not take the other down: a failing /api/state left
    // a sent message undrawn, which reads as a send that never went.
    const [chat, state] = await Promise.all([
      get<{ messages: Message[] | null }>("/api/chat"),
      get<{
        agent?: {
          running?: boolean;
          welded?: string;
          asking?: { toolId: string; tool: string; rule?: string }[];
          sideRuns?: Side[] | null;
          queued?: number;
        };
        attachment?: Attachment | null;
        design?: { file?: string } | null;
      }>("/api/state").catch(() => undefined),
    ]);
    messages = chat.messages || [];
    // Left as it was when the state did not answer: saying nothing runs
    // would hide the Stop button on a turn that is running.
    if (state) {
      running = Boolean(state.agent?.running);
      weldedFirst = state.agent?.welded ?? "";
      asking = new Map((state.agent?.asking || []).map((a) => [a.toolId, a]));
      sides = state.agent?.sideRuns || [];
      queued = state.agent?.queued ?? 0;
      doc = state.design?.file || "";
      takeAttachment(state.attachment);
    }
    send.textContent = running ? "Queue" : "Send";
    stop.hidden = !running;
    reached();
    // Nothing in it is not the conversation drawn: a server still
    // reading the transcript answers before it has one.
    drew = messages.length > 0;
    draw();
    if (!drew) again();
  } catch (err) {
    // A poll already in flight when the session closed lands here.
    if (!closed) say(String(err));
    again();
  }
}

// drew is whether the conversation has been on screen. Until it has, a
// read that fails or comes back empty is a server still starting.
let drew = false;
let retry: ReturnType<typeof setTimeout> | undefined;
let tries = 0;
const RETRIES = 10;

// again is the first read, tried until it brings something: the page
// redraws on the host's events, and an idle session sends none.
function again(): void {
  if (drew || closed || retry || tries >= RETRIES) return;
  tries++;
  retry = setTimeout(() => {
    retry = undefined;
    void refresh();
  }, 1000);
}

// An attachment is held by the server until somebody takes it. Taking it
// here is what makes ⌘⌥A reach this pane rather than whichever tab polls next.
interface Attachment {
  from: string;
  text: string;
  seq?: number;
}

function takeAttachment(waiting: Attachment | null | undefined): void {
  if (!waiting || waiting.seq === undefined) return;
  if (taken.has(waiting.seq)) return;
  taken.add(waiting.seq);
  context = { from: waiting.from, text: waiting.text };
  drawTray();
  box.focus();
  // By number, so a selection made while this was fetching is not wiped.
  void ask(`/api/attach?seq=${waiting.seq}`, {
    method: "DELETE",
  }).catch(() => undefined);
}

// The delete and the next state can cross, so the same attachment can arrive
// twice; without this the composer would refill after you cleared it.
const taken = new Set<number>();

list.addEventListener("scroll", () => {
  pinned = list.scrollHeight - list.scrollTop - list.clientHeight < 40;
});

box.addEventListener("keydown", (event) => {
  // Enter sends, and a newline needs a modifier. The other way round loses a
  // message to every stray return.
  if (event.key === "Enter" && !event.shiftKey && !event.metaKey) {
    event.preventDefault();
    void submit();
  }
});

box.addEventListener("input", () => {
  clearTimeout(settling);
  settling = setTimeout(() => void look(), idle);
});

box.addEventListener("paste", (event) => {
  const files = pasted(event.clipboardData?.items);
  if (files.length === 0) return;
  event.preventDefault();
  void take(files);
});

for (const kind of ["dragover", "drop"] as const) {
  document.addEventListener(kind, (event) => {
    event.preventDefault();
    if (kind !== "drop") return;
    const moved = (event as DragEvent).dataTransfer;
    const files = [...(moved?.files || [])];
    if (files.length > 0) {
      void take(files);
      return;
    }
    const paths = dropped(moved?.getData("text/uri-list"));
    if (paths.length > 0) void haul(paths);
  });
}

// A drag out of the editor's tree carries paths and no bytes: the extension
// host reads them, because the webview cannot reach the disk.
async function haul(paths: string[]): Promise<void> {
  const id = String(++asked);
  const res = await new Promise<Answer>((settle) => {
    waiting.set(id, settle);
    host.postMessage({ kind: "drop", id, paths });
  });
  if (!res.ok) {
    say(res.error ?? "could not read what was dropped");
    return;
  }
  const got = (JSON.parse(res.text || "{}") as { files?: Incoming[] }).files;
  for (const file of got || []) {
    if (held.length >= MAX_FILES) {
      say(`${MAX_FILES} files at most`);
      break;
    }
    held.push(file);
  }
  drawTray();
}

send.addEventListener("click", () => void submit());
stop.addEventListener("click", () => void cancel());

// Dictation. Held, the button records until release; the chord toggles.
let recording: (() => Promise<Uint8Array>) | undefined;
let opening = false;
// The chosen input, remembered by the editor. Empty is the system default.
let micId = "";

async function listenIn(): Promise<void> {
  if (recording || opening) return;
  opening = true;
  try {
    recording = await record(root.dataset.mic || "", micId);
    mic.classList.add("live");
    say("");
  } catch (err) {
    say(`could not open the microphone: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    opening = false;
  }
}

async function transcribe(): Promise<void> {
  const stopping = recording;
  if (!stopping) return;
  recording = undefined;
  mic.classList.remove("live");
  say("transcribing…");
  let res: Answer;
  let got: { text?: string; error?: string };
  try {
    const audio = await stopping();
    res = await ask("/api/transcribe", {
      method: "POST",
      body: JSON.stringify({ wav: base64(audio) }),
    });
    got = JSON.parse(res.text || "{}") as typeof got;
  } catch (err) {
    say(`transcribe failed: ${String(err)}`);
    return;
  }
  if (!res.ok) {
    say(got.error ?? res.error ?? `transcribe failed: ${res.status}`);
    return;
  }
  say("");
  if (!got.text) return;
  const put = spliced(box.value, box.selectionStart, box.selectionEnd, got.text);
  box.value = put.value;
  box.focus();
  box.setSelectionRange(put.caret, put.caret);
  box.dispatchEvent(new Event("input"));
}

// Right-click picks the input: the system default is silent on a clamshell,
// and the page cannot change what macOS chose.
mic.addEventListener("contextmenu", (event) => {
  event.preventDefault();
  void inputs().then(
    (devices) => host.postMessage({ kind: "mics", devices, chosen: micId }),
    (err) => say(`could not list the microphones: ${String(err)}`),
  );
});

mic.addEventListener("pointerdown", (event) => {
  event.preventDefault();
  mic.setPointerCapture(event.pointerId);
  void listenIn();
});
mic.addEventListener("pointerup", () => {
  // A release that beats the permission prompt still ends the take.
  const ending = () => (opening ? setTimeout(ending, 50) : void transcribe());
  ending();
});

// The editor holds the event stream and names each event here. Its reconnect
// is the one that matters; this only has to say when it is down.
let started = false;

function start(): void {
  const first = !started;
  started = true;
  void refresh();
  if (first) box.focus();
}

// Always relisten, changed or not: the extension re-sends after reconnecting,
// which is exactly when a dead stream needs rebuilding.
window.addEventListener("message", (event: MessageEvent) => {
  const said = event.data as Answer & { kind?: string; token?: string; name?: string; open?: boolean; id?: string };
  if (said?.kind === "api.result" && said.id) {
    const settle = waiting.get(said.id);
    waiting.delete(said.id);
    settle?.(said);
    return;
  }
  if (said?.kind === "closed") {
    closed = true;
    say("Session closed.");
    return;
  }
  if (said?.kind === "event") {
    if (redraws(said.name)) void refresh();
    return;
  }
  // The events carry no id, so a reopened stream cannot resume what it
  // missed: refetch, or stay stale for ever.
  if (said?.kind === "stream") {
    if (!closed) {
      say(said.open ? "" : LOST);
      if (said.open) start();
    }
    return;
  }
  // The editor can reveal the view but not reach inside it, so the keystroke
  // arrives here as a message.
  if (said?.kind === "focus") {
    box.focus();
    return;
  }
  // Sent to every open chat: only the focused one starts, and a live one stops.
  if (said?.kind === "mic") {
    micId = said.id ?? "";
    return;
  }
  if (said?.kind === "dictate") {
    if (recording) void transcribe();
    else if (document.hasFocus()) void listenIn();
    return;
  }
  if (said?.kind !== "token" || !said.token) return;
  token = said.token;
  start();
});

say(CONNECTING);
host.postMessage({ kind: "ready" });
