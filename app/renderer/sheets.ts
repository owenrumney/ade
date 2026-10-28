import { el } from "./omnibox";
import { blocks, inline } from "./markdown";
import { linesOf, pairsOf, wanted, type At, type Cell, type Change, type Side, type Thread } from "./changes";
import type { Rules, View } from "./model";
import {
  foldable,
  folded,
  nextVerb,
  shownComments,
  whyNotSendable,
} from "../pages/commentrow";
import type { PrAct, PrActId } from "./prpage";
import { spacePage, type SpaceAct } from "./spacepage";

// The editor area. Each view draws its own content: a pane that changes
// its title and nothing else is indistinguishable from a broken one.

export function sheet(
  view: View,
  picked: (line: number) => void = () => {},
  forget: (rule: string) => void = () => {},
  on: OnThread = () => {},
  act: (id: PrActId) => void = () => {},
  review: Review = { hasPR: false, open: new Set() },
  ask: (who: string, text: string) => void = () => {},
  space: (did: SpaceAct) => void = () => {},
): HTMLElement {
  switch (view.kind) {
    case "rules":
      return rules(view.rules, forget);
    case "hunks":
      return hunks(view.file, view.threads ?? [], on, review, view.at);
    case "code":
      return code(view.source, picked, view.line);
    case "doc":
      return doc(view.body);
    case "pr":
      return pr(view.body, view.acts, act, view.busy);
    case "person":
      return person(view.who, view.body, ask);
    case "space":
      return spacePage(view.seen, space);
    default:
      return nothing();
  }
}

function nothing(): HTMLElement {
  const out = el("div", "nothing");
  const hint = el("div");
  hint.append(
    document.createTextNode("Or press "),
    el("span", "key", "⌘K"),
    document.createTextNode(" for anything."),
  );
  out.append(el("div", "", "Pick something on the left."), hint);
  return out;
}

// doc is a document the window drew and nobody edits. Anything written
// goes through `written()` in panels.ts.
function doc(body: string): HTMLElement {
  const out = el("div", "doc");
  const page = el("div", "rendered");
  page.innerHTML = html(body);
  out.append(page);
  return out;
}

function pr(body: string, acts: PrAct[], act: (id: PrActId) => void, busy?: PrActId): HTMLElement {
  const out = doc(body);
  const bar = el("div", "pr-acts");
  for (const one of acts) {
    const button = el("button", one.primary ? "go" : "", busy === one.id ? "working…" : one.label);
    button.title = one.blocked ?? one.detail;
    (button as HTMLButtonElement).disabled = Boolean(one.blocked || busy);
    button.addEventListener("click", () => act(one.id));
    bar.append(button);
  }
  out.prepend(bar);
  return out;
}

// person is the conversation and a box under it. Enter sends, shift-enter is
// a new line. What it sends is a question: the bus has nothing plainer yet.
function person(who: string, body: string, ask: (who: string, text: string) => void): HTMLElement {
  const out = doc(body);
  out.classList.add("person");
  const box = el("div", "ask-box");
  const text = document.createElement("textarea");
  text.placeholder = `Ask ${who}…`;
  text.rows = 3;
  const send = el("button", "go", "Send") as HTMLButtonElement;
  const go = () => {
    const said = text.value.trim();
    if (!said) return;
    text.disabled = send.disabled = true;
    send.textContent = "Sending…";
    ask(who, said);
  };
  text.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      go();
    }
  });
  send.addEventListener("click", go);
  const row = el("div", "row");
  row.append(el("span", "hint", "Sent as a question: they can answer for four hours."), send);
  box.append(text, row);
  out.append(box);
  return out;
}

// html is the rendered document. Every piece of text goes through
// inline(), which escapes, so nothing in a design reaches the DOM raw.
export function html(body: string): string {
  return blocks(body)
    .map((b) => {
      switch (b.kind) {
        case "h1":
        case "h2":
        case "h3":
          return `<${b.kind}>${inline(b.text ?? "")}</${b.kind}>`;
        case "ul":
        case "ol":
          return `<${b.kind}>${(b.items ?? []).map((i) => `<li>${inline(i)}</li>`).join("")}</${b.kind}>`;
        case "code":
          return `<pre><code>${inline(b.text ?? "").replace(/<\/?(code|strong|em|a)[^>]*>/g, "")}</code></pre>`;
        case "quote":
          return `<blockquote>${inline(b.text ?? "")}</blockquote>`;
        case "rule":
          return "<hr>";
        case "table":
          return table(b.rows ?? []);
        default:
          return `<p>${inline(b.text ?? "")}</p>`;
      }
    })
    .join("");
}

function table(rows: string[][]): string {
  if (rows.length === 0) return "";
  const [head, ...rest] = rows;
  const th = head.map((c) => `<th>${inline(c)}</th>`).join("");
  const tr = rest
    .map((r) => `<tr>${r.map((c) => `<td>${inline(c)}</td>`).join("")}</tr>`)
    .join("");
  return `<table><thead><tr>${th}</tr></thead><tbody>${tr}</tbody></table>`;
}

// code shows a file; clicking a line is what the status bar blames. A
// gutter would be a read per line, most of it unasked for.
function code(
  source: string,
  picked: (line: number) => void,
  here = 0,
): HTMLElement {
  // No heading: the pane above already names the file.
  const wrap = el("div", "sheet-stack");
  const out = el("div", "file");
  source.split("\n").forEach((line, i) => {
    const no = i + 1;
    const row = el("div", no === here ? "ln here" : "ln");
    row.append(el("span", "no", String(no)), coloured(line));
    row.addEventListener("click", () => picked(no));
    out.append(row);
  });
  wrap.append(out);
  return wrap;
}

// coloured is the crudest highlighter that reads as code: keywords,
// strings and comments. A real one is the editor's, whichever that is.
function coloured(src: string): HTMLElement {
  const out = el("span", "src");
  if (src.trimStart().startsWith("//")) {
    out.append(el("span", "c", src));
    return out;
  }
  const words = /\b(func|if|else|return|len|range|for|var|type|struct|nil|import|package|const|interface|map|go|defer)\b/g;
  let at = 0;
  for (const found of src.matchAll(words)) {
    const i = found.index ?? 0;
    out.append(document.createTextNode(src.slice(at, i)));
    out.append(el("span", "k", found[0]));
    at = i + found[0].length;
  }
  out.append(document.createTextNode(src.slice(at)));
  return out;
}

// rules is the session's permissions, drawn here because a frame of the
// server's own page wears its stylesheet.
function rules(all: Rules, forget: (rule: string) => void): HTMLElement {
  const out = el("div", "rules");
  if (all.free) {
    out.append(el("div", "warn-strip", "This session asks for nothing: every tool is allowed."));
  }
  group(out, "Allowed", all.allow, forget);
  group(out, "Denied", all.deny, forget);
  // The session's own are not in the file, so forgetting one is not
  // the same act: it goes when the session does.
  group(out, "This session only", all.session);
  if (all.file) out.append(el("div", "rules-file", all.file));
  return out;
}

function group(
  out: HTMLElement,
  title: string,
  rows: string[],
  forget?: (rule: string) => void,
): void {
  out.append(el("h3", "", `${title}  ${rows.length}`));
  if (rows.length === 0) {
    out.append(el("div", "empty", "Nothing here."));
    return;
  }
  for (const rule of rows) {
    const row = el("div", "rule");
    row.append(el("code", "", rule));
    if (forget) {
      const drop = el("button", "drop", "✕");
      drop.title = "Forget this rule";
      drop.addEventListener("click", () => forget(rule));
      row.append(drop);
    }
    out.append(row);
  }
}

// hunks draws the real diff: the server has already paired the lines and
// split each into tokens, so the colours here are the ones ade decided.
function hunks(
  file: Change,
  threads: Thread[],
  on: OnThread,
  at: Review,
  jump?: At,
): HTMLElement {
  const out = el("div", at.split ? "diff split" : "diff");
  const head = el("div", "file-head");
  head.append(el("span", "", `+${file.additions} −${file.deletions}`));
  const how = el("button", "how", at.split ? "Unified" : "Split");
  how.title = "Switch between a unified and a side-by-side diff";
  how.addEventListener("click", () => on({ what: "split" }));
  head.append(how);
  out.append(head);
  if (file.binary) {
    out.append(el("div", "hunk", "binary file"));
    return out;
  }
  const lines = at.split ? pairsOf(file, threads) : linesOf(file, threads);
  if (lines.length === 0) {
    out.append(el("div", "hunk", "no textual change"));
    return out;
  }
  for (const line of lines) {
    if (line.at === "hunk") {
      out.append(el("div", "hunk", line.text));
      continue;
    }
    if (line.at === "thread") {
      const block = thread(line.thread, on, at.hasPR, at.open.has(line.thread.id));
      block.dataset.thread = line.thread.id;
      out.append(block);
      continue;
    }
    if (line.at === "pair") {
      out.append(pair(line.left, line.right, file.path, on));
      continue;
    }
    const kind = line.kind === "context" ? "" : line.kind;
    const node = el("div", kind ? `ln ${kind}` : "ln");
    // The number is the way to say something about the line, which is
    // where a review starts.
    const no = el("span", "no", String(line.no));
    no.title = "Comment on this line";
    no.addEventListener("click", () =>
      on({ what: "new", file: file.path, side: line.kind === "del" ? "left" : "right", line: line.no }),
    );
    node.dataset.line = String(line.no);
    node.append(no, tokens(line.cell));
    out.append(node);
  }
  reveal(out, jump);
  return out;
}

function pair(left: Cell | null, right: Cell | null, path: string, on: OnThread): HTMLElement {
  const row = el("div", "pair");
  row.append(side(left, "left", path, on), side(right, "right", path, on));
  return row;
}

function side(cell: Cell | null, which: Side, path: string, on: OnThread): HTMLElement {
  const kind = !cell ? "gap" : cell.kind === "context" ? "" : cell.kind;
  const node = el("div", kind ? `ln ${kind}` : "ln");
  if (!cell) return node;
  const no = el("span", "no", String(cell.line));
  no.title = "Comment on this line";
  no.addEventListener("click", () => on({ what: "new", file: path, side: which, line: cell.line }));
  node.dataset.line = String(cell.line);
  node.append(no, tokens(cell));
  return node;
}

// reveal scrolls to what a jump named, once the pane is in the document:
// a node that is not there yet cannot be scrolled to.
function reveal(out: HTMLElement, jump?: At): void {
  const want = wanted(jump);
  if (!want) return;
  requestAnimationFrame(() => out.querySelector(want)?.scrollIntoView({ block: "center" }));
}

// What a thread's buttons ask of the window. The sheet draws and the
// window acts: it is the one holding the session.
export interface Asked {
  what: "new" | "reply" | "resolve" | "drop" | "convert" | "draft" | "take-out" | "unfold" | "split";
  id?: string;
  file?: string;
  side?: Side;
  line?: number;
}

export type OnThread = (asked: Asked) => void;

// Review is what the diff needs to know beyond the threads: whether there is
// a pull request to say anything on, and which threads are unfolded.
export interface Review {
  hasPR: boolean;
  open: Set<string>;
  split?: boolean;
}

// thread is one comment thread under the line it is about: one comment,
// whatever the agents did to reach it, and the one verb it is waiting for.
function thread(it: Thread, on: OnThread, hasPR: boolean, open: boolean): HTMLElement {
  const out = el("div", it.resolved ? "thread done" : "thread");
  out.append(heading(it, on, open));
  for (const said of shownComments(it, open)) {
    const who = el("div", "who");
    who.append(el("span", "author", said.author === "you" ? "you" : said.author));
    const when = [said.at ? ago(said.at) : "", said.sentAt ? "sent" : ""].filter(Boolean);
    if (when.length) who.append(el("span", "", when.join(" · ")));
    const body = el("div", "said");
    body.innerHTML = html(said.body);
    out.append(who, body);
  }
  if (it.orphaned) out.append(el("div", "who", "the line this was on has gone"));
  out.append(verbs(it, on, hasPR));
  return out;
}

// heading is what the thread is, before what it says: how bad, what it is
// about, and where it has got to.
function heading(it: Thread, on: OnThread, open: boolean): HTMLElement {
  const bar = el("div", "thread-head");
  const how = it.severity ?? "medium";
  bar.append(el("span", `sev ${how}`, how));
  if (it.category) bar.append(el("span", "cat", it.category));
  if (it.disputed) bar.append(el("span", "tag disputed", "disputed"));

  const n = folded(it);
  if (n > 1) bar.append(el("span", "quiet", `${n} findings`));
  if (it.sentAt) bar.append(el("span", "tag sent", "on the pull request"));
  else if (it.draftReview) bar.append(el("span", "tag draft", "in the draft review"));
  else if (it.outgoing) bar.append(el("span", "tag ready", "written up, not sent"));

  if (foldable(it)) {
    const more = el("span", "btn quiet", open ? "Fold" : "Show the findings");
    more.addEventListener("click", () => on({ what: "unfold", id: it.id }));
    bar.append(more);
  }
  return bar;
}

// verbs is the one thing a thread is waiting for, and why when it is none.
function verbs(it: Thread, on: OnThread, hasPR: boolean): HTMLElement {
  const acts = el("div", "acts");
  const act = (label: string, what: Asked["what"], quiet = true, why = "") => {
    const button = el("span", quiet ? "btn quiet" : "btn", label);
    if (why) button.title = why;
    button.addEventListener("click", () => on({ what, id: it.id }));
    acts.append(button);
  };
  const next = nextVerb(it, hasPR);
  switch (next) {
    // Each of the three names where it leaves the comment, because none of
    // them sends it and the first one read as if it did.
    case "convert":
      act("Write it up", "convert", false,
        "Rewrites your note in words for the author, on this thread. Nothing is sent.");
      break;
    case "draft":
      act("Add to the review", "draft", false,
        "Puts the written-up comment in the pull request's draft review, which only you can see.");
      break;
    case "take-out":
      act("Take out of the review", "take-out",
        true, "Takes it back out of the draft review. It stays written up here.");
      break;
    case "reply":
    case "answer":
      break;
    default: {
      const why = it.sentAt ? "" : whyNotSendable(it, hasPR);
      if (why) acts.append(el("span", "quiet", why));
    }
  }
  act("Reply", "reply");
  act(it.resolved ? "Unresolve" : "Resolve", "resolve");
  if (!it.sentAt && !it.draftReview) act("Delete", "drop");
  return acts;
}

// ago is the same glance the lists use, kept here rather than imported
// from the window's own mapping, which is about ade's lists.
function ago(stamp: string): string {
  const at = Date.parse(stamp);
  if (Number.isNaN(at)) return "";
  const m = Math.max(0, Math.round((Date.now() - at) / 60_000));
  if (m < 1) return "just now";
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  return h < 48 ? `${h}h` : `${Math.round(h / 24)}d`;
}

// tokens is the line, in the runs the server split it into. An edited run
// is marked, which is how a one-word change reads as one word.
function tokens(cell: Cell): HTMLElement {
  const out = el("span", "src");
  const runs = cell.tokens ?? [];
  if (runs.length === 0) {
    out.textContent = cell.content;
    return out;
  }
  for (const run of runs) {
    // The class is the server's own single letter — k, s, c, f, t, n, o —
    // which is what the diff's CSS is written against.
    const cls = [run.c ?? "", run.e ? "edit" : ""].filter(Boolean).join(" ");
    if (!cls) out.append(document.createTextNode(run.t));
    else out.append(el("span", cls, run.t));
  }
  return out;
}


