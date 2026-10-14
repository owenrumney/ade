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
import { BLOCKS, body, bullet, cells, hides, isMermaid, gap, level, picture, reveals, type Span } from "./marks";

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
    rows.forEach((row, i) => {
      const tr = document.createElement("tr");
      for (const cell of row) {
        const td = document.createElement(i === 0 ? "th" : "td");
        td.textContent = cell;
        tr.append(td);
      }
      table.append(tr);
    });
    wrap.append(table);
    return wrap;
  }
  ignoreEvent(): boolean {
    return false;
  }
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

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();

// A frame that fails draws nothing, which reads as a document that would not
// load. Say what went wrong where the document would have been.
function died(what: unknown): void {
  const el = document.getElementById("doc");
  if (!el) return;
  el.textContent = `ade: the rendered editor could not start — ${String(what)}`;
  el.setAttribute("style", "padding:2rem;white-space:pre-wrap;color:var(--vscode-errorForeground)");
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
    fontFamily: to("--ade-font-family") || to("--vscode-font-family"),
    flowchart: { useMaxWidth: false },
    themeVariables: {
      background: to("--vscode-editor-background"),
      primaryColor: to("--vscode-editorWidget-background"),
      primaryTextColor: to("--vscode-editor-foreground"),
      primaryBorderColor: to("--vscode-panel-border"),
      secondaryColor: to("--vscode-editorWidget-background"),
      tertiaryColor: to("--vscode-editor-background"),
      lineColor: to("--vscode-descriptionForeground"),
      textColor: to("--vscode-editor-foreground"),
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
        if (node.name === "Image") {
          if (reveals(node.name, node, node, head)) return;
          const pic = picture(state.doc.sliceString(node.from, node.to), base);
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

// The theme is the editor's own, so nothing here names a colour.
const theme = EditorView.theme({
  "&": {
    color: "var(--vscode-editor-foreground)",
    backgroundColor: "var(--vscode-editor-background)",
    fontSize: "var(--ade-font-size, var(--vscode-editor-font-size))",
    height: "100%",
  },
  ".cm-content": {
    fontFamily: "var(--ade-font-family, var(--vscode-font-family))",
    caretColor: "var(--vscode-editorCursor-foreground)",
    maxWidth: "48rem",
    margin: "0 auto",
    padding: "2rem 1rem 60vh",
  },
  ".cm-cursor": { borderLeftColor: "var(--vscode-editorCursor-foreground)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
    backgroundColor: "var(--vscode-editor-selectionBackground)",
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
    border: "1px solid var(--vscode-panel-border)",
    padding: "4px 10px",
    textAlign: "left",
  },
  ".cm-table th": {
    fontWeight: "600",
    background: "var(--vscode-editorWidget-background)",
  },
  ".cm-rule": {
    borderTop: "1px solid var(--vscode-panel-border)",
    margin: "0.8rem 0",
  },
  ".cm-codeblock": {
    fontFamily: "var(--vscode-editor-font-family)",
    fontSize: "0.9em",
    backgroundColor: "var(--vscode-textCodeBlock-background)",
    padding: "0 1rem",
  },
  ".cm-codefence": { fontSize: "0.5em" },
  ".cm-bullet": { color: "var(--vscode-descriptionForeground)" },
  ".cm-link": {
    color: "var(--vscode-textLink-foreground)",
    textDecoration: "underline",
  },
  ".cm-image": { display: "block", maxWidth: "100%", margin: "0.5rem 0" },
  ".cm-image.failed": { color: "var(--vscode-errorForeground)" },
  ".cm-diagram": {
    display: "flex",
    justifyContent: "safe center",
    overflowX: "auto",
    padding: "0.5rem 0",
  },
  ".cm-diagram.failed": {
    color: "var(--vscode-errorForeground)",
    whiteSpace: "pre-wrap",
    fontFamily: "var(--vscode-editor-font-family)",
  },
});

function extensions(): Extension[] {
  return [
    history(),
    keymap.of([...defaultKeymap, ...historyKeymap]),
    // markdownLanguage is the GFM one: without it a table is a paragraph.
    markdown({ base: markdownLanguage }),
    focused,
    EditorView.focusChangeEffect.of((_state, has) => look.of(has)),
    wysiwyg,
    theme,
    EditorView.lineWrapping,
  ];
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

window.addEventListener("message", (event: MessageEvent) => {
  const msg = event.data as { kind?: string; text?: string; base?: string };
  if (msg.kind === "style") {
    restyle(event.data as Style);
    return;
  }
  if (msg.kind !== "doc" || typeof msg.text !== "string") return;
  if (typeof msg.base === "string" && msg.base !== base) {
    base = msg.base;
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
