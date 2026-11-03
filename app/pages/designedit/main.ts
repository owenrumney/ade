import { EditorState, StateEffect, StateField, type Extension } from "@codemirror/state";
import {
  Decoration,
  type DecorationSet,
  EditorView,
  WidgetType,
  keymap,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { syntaxTree } from "@codemirror/language";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { BLOCKS, body, bullet, cells, hides, isMermaid, gap, joins, level, nesting, picture, reveals, type Span } from "./marks";
import { marksFor, spanOf, type Note } from "./notes";
import { Notepad } from "./notepad";
import { questionsIn, type Question } from "./questions";

// taken is which recommendations are ticked, by question text. Page state and
// nowhere else: a tick is a moment's intent, not a fact about the design.
const taken = new Set<string>();

// A GFM table, drawn. Replaced wholesale and revealed as source when the
// cursor enters, which is the same bargain the diagrams make.
class Table extends WidgetType {
  constructor(readonly source: string) {
    super();
  }
  eq(other: Table): boolean {
    return other.source === this.source;
  }
  toDOM(): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "cm-table";
    const table = document.createElement("table");
    const rows = cells(this.source);
    const asking = questionsIn(rows);
    rows.forEach((row, i) => {
      const tr = document.createElement("tr");
      for (const cell of row) {
        const td = document.createElement(i === 0 ? "th" : "td");
        td.textContent = cell;
        tr.append(td);
      }
      if (asking.length > 0) accept(tr, i, rows, asking);
      table.append(tr);
    });
    wrap.append(table);
    if (asking.length > 0) wrap.append(foldButton(asking));
    return wrap;
  }
  // Only the Accept column handles its own clicks. Everywhere else a click
  // still opens the table for editing, which is what the source reveal is.
  ignoreEvent(event: Event): boolean {
    return !!(event.target as HTMLElement | null)?.closest?.(".cm-accepts");
  }
}

// accept adds the column the markdown does not have: a tick per row that has
// something to accept, and a header cell to sit under.
function accept(tr: HTMLElement, i: number, rows: string[][], asking: Question[]): void {
  if (i === 0) {
    const th = document.createElement("th");
    th.className = "cm-accepts";
    th.textContent = "Accept";
    tr.append(th);
    return;
  }
  const td = document.createElement("td");
  td.className = "cm-accepts";
  const question = asking.find((q) => rows[i].includes(q.question));
  if (question) {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = taken.has(question.question);
    box.addEventListener("change", () => {
      if (box.checked) taken.add(question.question);
      else taken.delete(question.question);
    });
    td.append(box);
  }
  tr.append(td);
}

function foldButton(asking: Question[]): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "cm-accepts cm-fold";
  const button = document.createElement("button");
  button.textContent = "Fold accepted";
  button.addEventListener("click", () => {
    const rows = asking.filter((q) => taken.has(q.question));
    if (rows.length === 0) return;
    taken.clear();
    host.postMessage({ kind: "accept", rows });
  });
  wrap.append(button);
  return wrap;
}

// A horizontal rule, drawn as one.
class Rule extends WidgetType {
  eq(): boolean {
    return true;
  }
  toDOM(): HTMLElement {
    const el = document.createElement("div");
    el.className = "cm-rule";
    return el;
  }
}

// A list marker, drawn as the bullet it reads as. Replaced rather than
// hidden: taking the characters out moves the line left.
class Space extends WidgetType {
  eq(): boolean {
    return true;
  }
  toDOM(): HTMLElement {
    const el = document.createElement("span");
    el.className = "cm-join";
    el.textContent = " ";
    return el;
  }
}

class Bullet extends WidgetType {
  constructor(readonly mark: string) {
    super();
  }
  eq(other: Bullet): boolean {
    return other.mark === this.mark;
  }
  toDOM(): HTMLElement {
    const el = document.createElement("span");
    el.className = "cm-bullet";
    el.textContent = this.mark;
    return el;
  }
}

// An image beside the document, drawn. Its alt text stands in when it will not load.
class Picture extends WidgetType {
  constructor(
    readonly alt: string,
    readonly url: string,
  ) {
    super();
  }
  eq(other: Picture): boolean {
    return other.url === this.url && other.alt === this.alt;
  }
  toDOM(): HTMLElement {
    const img = document.createElement("img");
    img.className = "cm-image";
    img.alt = this.alt;
    img.src = this.url;
    img.addEventListener("error", () => img.classList.add("failed"));
    return img;
  }
  ignoreEvent(): boolean {
    return false;
  }
}

// base is the document's folder as the webview may load it; the host sends it.
let base = "";
// token is what the session server wants on a picture's request.
let token = "";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();

// A frame that fails draws nothing, which reads as a document that would not
// load. Say what went wrong where the document would have been.
function died(what: unknown): void {
  const el = document.getElementById("doc");
  if (!el) return;
  el.textContent = `ade: the rendered editor could not start — ${String(what)}`;
  el.setAttribute("style", "padding:2rem;white-space:pre-wrap;color:var(--bad)");
}
window.addEventListener("error", (e) => died(e.message));
window.addEventListener("unhandledrejection", (e) => died(e.reason));

// A mermaid block as it is drawn. The diagram is rendered into the widget
// once it is in the document; mermaid needs a laid-out element to measure.
class Diagram extends WidgetType {
  constructor(readonly source: string) {
    super();
  }
  eq(other: Diagram): boolean {
    return other.source === this.source;
  }
  toDOM(): HTMLElement {
    const el = document.createElement("div");
    el.className = "cm-diagram";
    draw(el, this.source);
    return el;
  }
  ignoreEvent(): boolean {
    return false;
  }
}

// drawn counts the diagrams so each gets an id mermaid can key on.
let drawn = 0;
let started = false;
let diagramFontSize = 16;

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
    fontFamily: to("--ade-font-family") || to("--ui"),
    flowchart: { useMaxWidth: false },
    themeVariables: {
      background: to("--bg"),
      primaryColor: to("--bg-raised"),
      primaryTextColor: to("--ink"),
      primaryBorderColor: to("--line"),
      secondaryColor: to("--bg-raised"),
      tertiaryColor: to("--bg"),
      lineColor: to("--ink-dim"),
      textColor: to("--ink"),
      fontSize: `${diagramFontSize}px`,
    },
  });
}

async function draw(el: HTMLElement, source: string): Promise<void> {
  const mermaid = (window as unknown as { mermaid?: Mermaid }).mermaid;
  if (!mermaid) {
    el.textContent = source;
    return;
  }
  start(mermaid);
  try {
    const { svg } = await mermaid.render(`cm-diagram-${++drawn}`, source);
    el.innerHTML = svg;
  } catch (err) {
    el.classList.add("failed");
    el.textContent = String(err);
  }
}

interface Mermaid {
  render(id: string, source: string): Promise<{ svg: string }>;
  initialize(config: Record<string, unknown>): void;
}

// Syntax reveals for editing, and nothing is being edited until the frame has
// the focus: a document opens reading as a document, title included.
const look = StateEffect.define<boolean>();
const focused = StateField.define<boolean>({
  create: () => false,
  update: (was, tr) =>
    tr.effects.reduce((now, e) => (e.is(look) ? e.value : now), was),
});

// block is what a replaced block is drawn as, or nothing when it is a block
// worth leaving as source — a code fence that is not a diagram.
function block(name: string, text: string): WidgetType | undefined {
  if (name === "Table") return new Table(text);
  if (name === "HorizontalRule") return new Rule();
  if (name === "FencedCode" && isMermaid(text.split("\n")[0] ?? ""))
    return new Diagram(body(text));
  return undefined;
}

type Placed = { from: number; to: number; value: Decoration };

// reflow draws a paragraph's line breaks as spaces, so it fills the pane.
function reflow(state: EditorState, node: Span, out: Placed[]): void {
  const first = state.doc.lineAt(node.from);
  const last = state.doc.lineAt(node.to);
  for (let n = first.number; n < last.number; n++) {
    const line = state.doc.line(n);
    if (!joins(line.text)) continue;
    const next = state.doc.line(n + 1);
    out.push({
      from: line.to,
      to: next.from + gap(next.text),
      value: Decoration.replace({ widget: new Space() }),
    });
  }
}

// fence boxes a code block; its fence lines empty out until the cursor enters.
function fence(state: EditorState, node: Span, head: number | undefined, out: Placed[]): void {
  const first = state.doc.lineAt(node.from);
  const last = state.doc.lineAt(node.to);
  const reading = !reveals("FencedCode", node, node, head);
  const closed = last.number > first.number && /^\s*[`~]{3,}\s*$/.test(last.text);
  for (let n = first.number; n <= last.number; n++) {
    const line = state.doc.line(n);
    const edge = n === first.number || (closed && n === last.number);
    const cls = edge && reading ? "cm-codeblock cm-codefence" : "cm-codeblock";
    out.push({ from: line.from, to: line.from, value: Decoration.line({ class: cls }) });
    if (edge && reading && line.to > line.from)
      out.push({ from: line.from, to: line.to, value: Decoration.replace({}) });
  }
}

// marks walks the tree and decides, node by node, what is hidden and what is
// drawn. The cursor is what keeps a node's source in view.
//
// A state field rather than a view plugin: CodeMirror refuses block
// decorations from a plugin, and a drawn diagram is one.
function marks(state: EditorState): DecorationSet {
  const head = state.field(focused) ? state.selection.main.head : undefined;
  const builder: Placed[] = [];
  const tree = syntaxTree(state);
  {
    tree.iterate({
      enter: (node) => {
        const parent = node.node.parent ?? node;
        if (BLOCKS.has(node.name)) {
          const text = state.doc.sliceString(node.from, node.to);
          const drawn = block(node.name, text);
          if (!drawn) {
            if (node.name === "FencedCode") fence(state, node, head, builder);
            return false;
          }
          if (reveals(node.name, node, node, head)) return;
          builder.push({
            from: node.from,
            to: node.to,
            value: Decoration.replace({ widget: drawn, block: true }),
          });
          return false;
        }
        if (node.name === "Paragraph") {
          if (!reveals(node.name, node, node, head)) reflow(state, node, builder);
          return;
        }
        if (node.name === "Image") {
          if (reveals(node.name, node, node, head)) return;
          const pic = picture(state.doc.sliceString(node.from, node.to), base, token);
          if (!pic) return;
          builder.push({
            from: node.from,
            to: node.to,
            value: Decoration.replace({ widget: new Picture(pic.alt, pic.url) }),
          });
          return false;
        }
        if (!hides(node.name)) return;
        if (reveals(node.name, node, parent, head)) return;
        const text = state.doc.sliceString(node.from, node.to);
        if (node.name === "ListMark") {
          const mark = bullet(text);
          if (!mark) return;
          const line = state.doc.lineAt(node.from);
          const depth = nesting(line.text);
          if (depth > 0) {
            builder.push({
              from: line.from,
              to: line.from,
              value: Decoration.line({ class: `cm-sub cm-sub${Math.min(depth, 3)}` }),
            });
          }
          builder.push({
            from: node.from,
            to: node.to,
            value: Decoration.replace({ widget: new Bullet(mark) }),
          });
          return;
        }
        let to = node.to;
        if (node.name === "HeaderMark") {
          const depth = level(text);
          if (depth > 0) {
            builder.push({
              from: parent.from,
              to: parent.to,
              value: Decoration.mark({ class: `cm-h${depth}` }),
            });
            to += gap(state.doc.sliceString(node.to, parent.to));
          }
        }
        builder.push({ from: node.from, to, value: Decoration.replace({}) });
        return;
      },
    });
  }
  builder.sort((a, b) => a.from - b.from || a.to - b.to);
  return Decoration.set(
    builder.map((one) => one.value.range(one.from, one.to)),
    true,
  );
}

const wysiwyg = StateField.define<DecorationSet>({
  create: (state) => marks(state),
  update: (deco, tr) =>
    tr.docChanged || tr.selection || tr.effects.some((e) => e.is(look))
      ? marks(tr.state)
      : deco.map(tr.changes),
  provide: (f) => EditorView.decorations.from(f),
});

// The notes, and the marks they draw. Their own field: an underline can land
// on text the wysiwyg pass is replacing, and one sorted set cannot hold both.
const showNotes = StateEffect.define<Note[]>();
const noted = StateField.define<Note[]>({
  create: () => [],
  update: (was, tr) => tr.effects.reduce((now, e) => (e.is(showNotes) ? e.value : now), was),
});

const noteMarks = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    if (!tr.docChanged && !tr.effects.some((e) => e.is(showNotes))) return deco.map(tr.changes);
    return Decoration.set(
      marksFor(tr.state.doc.toString(), tr.state.field(noted)).map((m) =>
        Decoration.mark({ class: m.cls, attributes: { "data-note": m.id } }).range(m.from, m.to),
      ),
      true,
    );
  },
  provide: (f) => EditorView.decorations.from(f),
});

// The theme is the editor's own, so nothing here names a colour.
const theme = EditorView.theme({
  "&": {
    color: "var(--ink)",
    backgroundColor: "var(--bg)",
    fontSize: "var(--ade-font-size, var(--mono-size))",
    height: "100%",
  },
  ".cm-content": {
    fontFamily: "var(--ade-font-family, var(--ui))",
    caretColor: "var(--ink)",
    // No measure: a table or a diagram sized to the pane beside prose
    // stopping at 48rem read as two documents.
    maxWidth: "none",
    padding: "2rem 2.5rem 60vh",
  },
  ".cm-cursor": { borderLeftColor: "var(--ink)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
    backgroundColor: "var(--selection)",
  },
  ".cm-h1": { fontSize: "1.9em", fontWeight: "600", lineHeight: "1.3" },
  ".cm-h2": { fontSize: "1.5em", fontWeight: "600", lineHeight: "1.3" },
  ".cm-h3": { fontSize: "1.25em", fontWeight: "600" },
  ".cm-h4, .cm-h5, .cm-h6": { fontSize: "1.05em", fontWeight: "600" },
  ".cm-table table": {
    borderCollapse: "collapse",
    margin: "0.4rem 0",
    fontSize: "0.95em",
  },
  ".cm-table th, .cm-table td": {
    border: "1px solid var(--line)",
    padding: "4px 10px",
    textAlign: "left",
  },
  ".cm-table th": {
    fontWeight: "600",
    background: "var(--bg-raised)",
  },
  ".cm-rule": {
    borderTop: "1px solid var(--line)",
    margin: "0.8rem 0",
  },
  ".cm-codeblock": {
    fontFamily: "var(--mono)",
    fontSize: "0.9em",
    backgroundColor: "var(--bg-raised)",
    padding: "0 1rem",
  },
  ".cm-codefence": { fontSize: "0.5em" },
  ".cm-bullet": { color: "var(--ink-dim)" },
  // An expansion reads as subordinate to the bullet above it, or the two
  // levels differ by two spaces and the list stops being scannable.
  ".cm-sub": { color: "var(--ink-dim)" },
  ".cm-sub1": { paddingLeft: "1.4rem" },
  ".cm-sub2": { paddingLeft: "2.8rem" },
  ".cm-sub3": { paddingLeft: "4.2rem" },
  ".cm-link": {
    color: "var(--info)",
    textDecoration: "underline",
  },
  ".cm-image": { display: "block", maxWidth: "100%", margin: "0.5rem 0" },
  ".cm-image.failed": { color: "var(--bad)" },
  ".cm-diagram": {
    display: "flex",
    justifyContent: "safe center",
    overflowX: "auto",
    padding: "0.5rem 0",
  },
  ".cm-diagram.failed": {
    color: "var(--bad)",
    whiteSpace: "pre-wrap",
    fontFamily: "var(--mono)",
  },
  ".cm-note": {
    borderBottom: "2px solid var(--warn)",
    cursor: "pointer",
  },
  ".cm-note-done": { borderBottomColor: "var(--ink-dim)", opacity: "0.7" },
  ".cm-table th.cm-accepts, .cm-table td.cm-accepts": { textAlign: "center", width: "4rem" },
  ".cm-fold": { marginTop: "0.5rem" },
  ".cm-fold button": {
    background: "var(--accent)",
    color: "var(--accent-ink)",
    border: "0",
    borderRadius: "3px",
    padding: "0.25rem 0.7rem",
    cursor: "pointer",
    font: "inherit",
  },
});

// The pad floats over the editor rather than inside it, so it is styled on the
// document and not in the editor's theme.
const padStyle = `
.note-pad {
  position: fixed; z-index: 20; max-width: 24rem; min-width: 16rem;
  padding: 0.6rem; border-radius: 4px;
  border: 1px solid var(--line);
  background: var(--bg-raised);
  color: var(--ink);
  font-family: var(--ade-font-family, var(--ui));
  font-size: 0.9em;
  box-shadow: 0 2px 10px rgba(0,0,0,0.3);
}
.note-quote {
  border-left: 2px solid var(--line);
  padding-left: 0.5rem; margin-bottom: 0.4rem;
  color: var(--ink-dim);
}
.note-gone { color: var(--bad); margin-bottom: 0.4rem; }
.note-comment { margin-bottom: 0.5rem; }
.note-who { color: var(--ink-dim); font-size: 0.85em; }
.note-said { white-space: pre-wrap; }
.note-reply {
  width: 100%; box-sizing: border-box; min-height: 3.5rem; resize: vertical;
  background: var(--input-bg); color: var(--ink);
  border: 1px solid var(--line);
  font-family: inherit; font-size: inherit; padding: 0.3rem;
}
.note-buttons { display: flex; gap: 0.4rem; margin-top: 0.4rem; }
.note-act, .note-new {
  background: var(--accent); color: var(--accent-ink);
  border: 0; border-radius: 3px; padding: 0.25rem 0.7rem; cursor: pointer;
  font-family: inherit; font-size: inherit;
}
.note-drop {
  background: transparent; color: var(--bad);
  margin-left: auto;
}
`;

function extensions(): Extension[] {
  return [
    history(),
    keymap.of([...defaultKeymap, ...historyKeymap]),
    // markdownLanguage is the GFM one: without it a table is a paragraph.
    markdown({ base: markdownLanguage }),
    focused,
    EditorView.focusChangeEffect.of((_state, has) => look.of(has)),
    wysiwyg,
    // noted before noteMarks: the marks are built from what it holds.
    noted,
    noteMarks,
    EditorView.updateListener.of((update) => {
      if (update.selectionSet) onSelection(update.view);
      else if (update.docChanged || update.geometryChanged) pad?.place();
    }),
    EditorView.domEventHandlers({
      mousedown: (event, view) => onMouseDown(event, view),
    }),
    theme,
    EditorView.lineWrapping,
  ];
}

// pad is built once the view is, and the handlers above only run after that.
let pad: Notepad | undefined;
let notes: Note[] = [];

// onSelection raises the Comment button on a highlight, and takes it away
// again on a bare cursor. A click on a note is handled before this runs.
function onSelection(view: EditorView): void {
  if (opening) return;
  const { from, to } = view.state.selection.main;
  if (from === to) {
    pad?.hide();
    return;
  }
  pad?.offer(from, to);
}

// opening guards the selection change a click on a note causes, which would
// otherwise hide the thread the click just opened.
let opening = false;

function onMouseDown(event: MouseEvent, view: EditorView): boolean {
  const target = event.target as HTMLElement | null;
  const id = target?.closest?.("[data-note]")?.getAttribute("data-note");
  if (!id) {
    pad?.hide();
    return false;
  }
  const note = notes.find((n) => n.id === id);
  if (!note) return false;
  const span = spanOf(view.state.doc.toString(), note);
  opening = true;
  pad?.open(note, span?.to ?? 0);
  setTimeout(() => {
    opening = false;
  }, 0);
  // Handled: without this the click also lands in the text, selecting the
  // paragraph the note is on and revealing its source under the thread.
  return true;
}

interface Style {
  fontFamily?: string;
  fontSize?: number;
  diagramFontSize?: number;
}

// restyle arrives before the document, so the first diagram is drawn at its size.
function restyle(style: Style): void {
  const root = document.documentElement.style;
  if (style.fontFamily) root.setProperty("--ade-font-family", style.fontFamily);
  if (style.fontSize && style.fontSize > 0) root.setProperty("--ade-font-size", `${style.fontSize}px`);
  if (style.diagramFontSize && style.diagramFontSize > 0) diagramFontSize = style.diagramFontSize;
}

// own is set while the frame is writing, so the edit coming back from the
// host is not applied over the top of what is already there.
let own = false;

const view = new EditorView({
  state: EditorState.create({ doc: "", extensions: extensions() }),
  parent: document.getElementById("doc") as HTMLElement,
  dispatchTransactions(trs, v) {
    v.update(trs);
    if (trs.some((t) => t.docChanged) && !own) {
      host.postMessage({ kind: "edit", text: v.state.doc.toString() });
    }
  },
});

const sheet = document.createElement("style");
sheet.textContent = padStyle;
document.head.append(sheet);

pad = new Notepad(view, {
  file: (quote, nth, body) => host.postMessage({ kind: "note", quote, nth, body }),
  reply: (id, body) => host.postMessage({ kind: "notereply", id, body }),
  resolve: (id, resolved) => host.postMessage({ kind: "noteresolve", id, resolved }),
  remove: (id) => host.postMessage({ kind: "notedelete", id }),
});
window.addEventListener("resize", () => pad?.place());

window.addEventListener("message", (event: MessageEvent) => {
  const msg = event.data as {
    kind?: string;
    text?: string;
    base?: string;
    token?: string;
    notes?: Note[];
  };
  if (msg.kind === "style") {
    restyle(event.data as Style);
    return;
  }
  if (msg.kind === "notes") {
    notes = msg.notes ?? [];
    view.dispatch({ effects: showNotes.of(notes) });
    return;
  }
  if (msg.kind !== "doc" || typeof msg.text !== "string") return;
  if (
    (typeof msg.base === "string" && msg.base !== base) ||
    (typeof msg.token === "string" && msg.token !== token)
  ) {
    base = msg.base ?? base;
    token = msg.token ?? token;
    view.dispatch({ effects: look.of(view.state.field(focused)) });
  }
  if (msg.text === view.state.doc.toString()) return;
  own = true;
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: msg.text },
  });
  own = false;
});

host.postMessage({ kind: "ready" });
