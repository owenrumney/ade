import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/basic-languages/monaco.contribution";

// Monaco, as the Code view. One instance for the window's life, or a
// redraw would lose the scroll and the selection.

// No workers: `editor.api` plus `basic-languages` leaves out the
// language services a reader of a file does not want.

// ayu-dark, the same values as theme.css. Monaco wants them without the
// alpha and with its own names.
const THEME: monaco.editor.IStandaloneThemeData = {
  base: "vs-dark",
  inherit: true,
  colors: {
    "editor.background": "#0d1017",
    "editor.foreground": "#bfbdb6",
    "editorLineNumber.foreground": "#3d4149",
    "editorLineNumber.activeForeground": "#8a9199",
    "editor.lineHighlightBackground": "#131721",
    "editor.selectionBackground": "#409fff40",
    "editorCursor.foreground": "#e6b450",
    "editorIndentGuide.background1": "#1e232b",
  },
  rules: [
    { token: "comment", foreground: "5a6673", fontStyle: "italic" },
    { token: "keyword", foreground: "ff8f40" },
    { token: "string", foreground: "aad94c" },
    { token: "number", foreground: "d2a6ff" },
    { token: "type", foreground: "59c2ff" },
    { token: "function", foreground: "ffb454" },
  ],
};

let made: monaco.editor.IStandaloneCodeEditor | undefined;
let host: HTMLElement | undefined;
let diffed: monaco.editor.IStandaloneDiffEditor | undefined;
let diffHost: HTMLElement | undefined;
// Whichever of the two is on screen: the selection is read off it.
let active: monaco.editor.ICodeEditor | undefined;
let picked: ((line: number) => void) | undefined;
let showing = "";

// Asking is how the editor reaches a language server: the window holds
// the session, so it does the asking and this only says where.
export interface Asking {
  hover(path: string, line: number, col: number): Promise<string>;
  definition(path: string, line: number, col: number): Promise<Where | undefined>;
}

export interface Where {
  path: string;
  line: number;
}

const models = new Map<string, { model: monaco.editor.ITextModel; base: string }>();

function dirty(held: { model: monaco.editor.ITextModel; base: string }): boolean {
  return held.model.getValue() !== held.base;
}

// edits is the showing file when it has changes to save.
export function edits(): { path: string; source: string } | undefined {
  const held = models.get(showing);
  return held && dirty(held) ? { path: showing, source: held.model.getValue() } : undefined;
}

export function saved(path: string, source: string): void {
  const held = models.get(path);
  if (held) held.base = source;
}

let asking: Asking | undefined;
let registered = false;

// ask gives the editor its way to the language server. Said once, and
// the providers are registered the first time it is.
export function ask(how: Asking): void {
  asking = how;
  if (registered) return;
  registered = true;
  monaco.languages.registerHoverProvider({ scheme: "*" }, {
    async provideHover(model, at) {
      const said = await asking?.hover(showing, at.lineNumber, at.column);
      if (!said) return null;
      return { contents: [{ value: said }] };
    },
  });
  monaco.languages.registerDefinitionProvider({ scheme: "*" }, {
    async provideDefinition(model, at) {
      const found = await asking?.definition(showing, at.lineNumber, at.column);
      if (!found) return null;
      // A definition in another file is the window's to open: monaco
      // holds one model and knows nothing of the checkout.
      if (found.path !== showing) {
        jumped?.(found);
        return null;
      }
      return {
        uri: model.uri,
        range: new monaco.Range(found.line, 1, found.line, 1),
      };
    },
  });
}

let jumped: ((to: Where) => void) | undefined;

// onJump is what opens a definition that is in another file.
export function onJump(then: (to: Where) => void): void {
  jumped = then;
}

// selected is what is highlighted, with where it came from. Empty when
// nothing is: a verb that asks about a selection has nothing to ask.
export function selected(): { from: string; text: string } | undefined {
  const at = active?.getSelection();
  const text = at && active?.getModel()?.getValueInRange(at);
  if (!at || !text?.trim()) return undefined;
  const lines =
    at.startLineNumber === at.endLineNumber
      ? `${at.startLineNumber}`
      : `${at.startLineNumber}-${at.endLineNumber}`;
  return { from: `${showing}:${lines}`, text };
}

// mount is the editor's element, ready to be put wherever the window is
// drawing it now. The element is the same one every time.
export function mount(
  source: string,
  path: string,
  line: number,
  onLine: (n: number) => void,
): HTMLElement {
  picked = onLine;
  showing = path;
  if (!host) host = make();
  const editor = made!;
  active = editor;
  const held = hold(path, source);
  if (editor.getModel() !== held.model) editor.setModel(held.model);
  if (line > 0) editor.revealLineInCenterIfOutsideViewport(line);
  // The element has just been moved in the document, so its size is
  // whatever it was before the move.
  queueMicrotask(() => editor.layout());
  return host;
}

// mountDiff is the file against what the diff's left side has, drawn
// inline and editable: a fix is typed where the change is read.
export function mountDiff(
  base: string,
  source: string,
  path: string,
  line: number,
  onLine: (n: number) => void,
): HTMLElement {
  picked = onLine;
  showing = path;
  if (!diffHost) diffHost = makeDiff();
  const editor = diffed!;
  active = editor.getModifiedEditor();
  const held = hold(path, source);
  const was = editor.getModel();
  if (was?.modified !== held.model) {
    was?.original.dispose();
    editor.setModel({
      original: monaco.editor.createModel(base, languageOf(path)),
      modified: held.model,
    });
  } else if (was.original.getValue() !== base) {
    was.original.setValue(base);
  }
  if (line > 0) editor.revealLineInCenterIfOutsideViewport(line);
  queueMicrotask(() => editor.layout());
  return diffHost;
}

// hold is the model for a file, one per path so an unsaved edit survives
// opening another. A fresh read replaces only what has not been typed over.
function hold(path: string, source: string): { model: monaco.editor.ITextModel; base: string } {
  let held = models.get(path);
  if (!held) {
    held = { model: monaco.editor.createModel(source, languageOf(path)), base: source };
    models.set(path, held);
  } else if (held.base !== source && !dirty(held)) {
    held.model.setValue(source);
    held.base = source;
  }
  return held;
}

const OPTIONS: monaco.editor.IStandaloneEditorConstructionOptions = {
  theme: "ade",
  automaticLayout: true,
  fontFamily: "var(--mono)",
  fontSize: 13,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  renderLineHighlight: "line",
  smoothScrolling: true,
  occurrencesHighlight: "off",
  // The window owns ⌘K, so the editor may not: a chord that reaches
  // here is one the keymap did not get.
  quickSuggestions: false,
};

let themed = false;

function make(): HTMLElement {
  const el = document.createElement("div");
  el.className = "monaco";
  if (!themed) monaco.editor.defineTheme("ade", THEME);
  themed = true;
  made = monaco.editor.create(el, OPTIONS);
  made.onMouseDown((e) => {
    const at = e.target.position?.lineNumber;
    if (at) picked?.(at);
  });
  return el;
}

function makeDiff(): HTMLElement {
  const el = document.createElement("div");
  el.className = "monaco";
  if (!themed) monaco.editor.defineTheme("ade", THEME);
  themed = true;
  diffed = monaco.editor.createDiffEditor(el, {
    ...OPTIONS,
    renderSideBySide: false,
    originalEditable: false,
    renderOverviewRuler: false,
    hideUnchangedRegions: { enabled: true },
  });
  diffed.getModifiedEditor().onMouseDown((e) => {
    const at = e.target.position?.lineNumber;
    if (at) picked?.(at);
  });
  return el;
}

// languageOf is monaco's name for the file's language, by extension.
// Unknown is plain text.
export function languageOf(path: string): string {
  const ext = "." + (path.split(".").pop() ?? "").toLowerCase();
  for (const lang of monaco.languages.getLanguages()) {
    if ((lang.extensions ?? []).includes(ext)) return lang.id;
  }
  return "plaintext";
}
