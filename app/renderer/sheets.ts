import { el } from "./omnibox";
import { blocks, inline } from "./markdown";
import { linesOf, type Cell, type Change, type Thread } from "./changes";
import type { Rules, View } from "./model";

// The editor area. Each view draws its own content: a pane that changes
// its title and nothing else is indistinguishable from a broken one.

export function sheet(
  view: View,
  picked: (line: number) => void = () => {},
  forget: (rule: string) => void = () => {},
  on: OnThread = () => {},
): HTMLElement {
  switch (view.kind) {
    case "rules":
      return rules(view.rules, forget);
    case "hunks":
      return hunks(view.file, view.threads ?? [], on);
    case "code":
      return code(view.source, picked, view.line);
    case "doc":
      return doc(view.body);
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
function hunks(file: Change, threads: Thread[], on: OnThread): HTMLElement {
  const out = el("div", "diff");
  out.append(el("div", "file-head", `+${file.additions} −${file.deletions}`));
  if (file.binary) {
    out.append(el("div", "hunk", "binary file"));
    return out;
  }
  const lines = linesOf(file, threads);
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
      out.append(thread(line.thread, on));
      continue;
    }
    const kind = line.kind === "context" ? "" : line.kind;
    const node = el("div", kind ? `ln ${kind}` : "ln");
    // The number is the way to say something about the line, which is
    // where a review starts.
    const no = el("span", "no", String(line.no));
    no.title = "Comment on this line";
    no.addEventListener("click", () =>
      on({ what: "new", file: file.path, side: line.kind === "del" ? "old" : "new", line: line.no }),
    );
    node.append(no, tokens(line.cell));
    out.append(node);
  }
  return out;
}

// What a thread's buttons ask of the window. The sheet draws and the
// window acts: it is the one holding the session.
export interface Asked {
  what: "new" | "reply" | "resolve" | "send" | "drop";
  id?: string;
  file?: string;
  side?: string;
  line?: number;
}

export type OnThread = (asked: Asked) => void;

// thread is one comment thread under the line it is about: who said
// what, and the few things you can do about it.
function thread(it: Thread, on: OnThread): HTMLElement {
  const out = el("div", it.resolved ? "thread done" : "thread");
  for (const said of it.comments ?? []) {
    const who = el("div", "who");
    who.append(el("span", "author", said.author === "you" ? "you" : said.author));
    const when = [said.at ? ago(said.at) : "", said.sentAt ? "sent" : ""].filter(Boolean);
    if (when.length) who.append(el("span", "", when.join(" · ")));
    const body = el("div", "said");
    body.innerHTML = html(said.body);
    out.append(who, body);
  }
  if (it.orphaned) out.append(el("div", "who", "the line this was on has gone"));

  const acts = el("div", "acts");
  const act = (label: string, what: Asked["what"], quiet = true) => {
    const button = el("span", quiet ? "btn quiet" : "btn", label);
    button.addEventListener("click", () => on({ what, id: it.id }));
    acts.append(button);
  };
  act("Reply", "reply");
  act(it.resolved ? "Unresolve" : "Resolve", "resolve");
  if (!it.sentAt && !it.externalId) act("Send to GitHub", "send", false);
  act("Delete", "drop");
  out.append(acts);
  return out;
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


