import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/basic-languages/monaco.contribution";

// Monaco, for editing a file inside its diff. One instance per half, or a
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

// One per half: one instance cannot be in two panes at once.
export type Half = 0 | 1;

interface Side {
  diffed?: monaco.editor.IStandaloneDiffEditor;
  diffHost?: HTMLElement;
  showing: string;
}

const sides: [Side, Side] = [{ showing: "" }, { showing: "" }];
// The half a verb about "the file" means: the window says which.
let focused: Half = 0;

export function focus(half: Half): void {
  focused = half;
}

const models = new Map<string, { model: monaco.editor.ITextModel; base: string }>();

function dirty(held: { model: monaco.editor.ITextModel; base: string }): boolean {
  return held.model.getValue() !== held.base;
}

// edits is the showing file when it has changes to save.
export function edits(): { path: string; source: string } | undefined {
  const at = sides[focused].showing;
  const held = models.get(at);
  return held && dirty(held) ? { path: at, source: held.model.getValue() } : undefined;
}

export function saved(path: string, source: string): void {
  const held = models.get(path);
  if (held) held.base = source;
}

// mountDiff is the file against what the diff's left side has, drawn
// inline and editable: a fix is typed where the change is read.
export function mountDiff(half: Half, base: string, source: string, path: string, line: number): HTMLElement {
  const side = sides[half];
  side.showing = path;
  if (!side.diffHost) side.diffHost = makeDiff(side);
  const editor = side.diffed!;
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
  return side.diffHost;
}

// hold is the model for a file, one per path so an unsaved edit survives
// opening another. A fresh read replaces only what has not been typed over.
function hold(path: string, source: string): { model: monaco.editor.ITextModel; base: string } {
  let held = models.get(path);
  if (!held) {
    // One uri per path, so a second open finds the same model.
    const uri = monaco.Uri.parse(`file:///${path.split("/").map(encodeURIComponent).join("/")}`);
    held = { model: monaco.editor.createModel(source, languageOf(path), uri), base: source };
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


function makeDiff(side: Side): HTMLElement {
  const el = document.createElement("div");
  el.className = "monaco";
  if (!themed) monaco.editor.defineTheme("ade", THEME);
  themed = true;
  side.diffed = monaco.editor.createDiffEditor(el, {
    ...OPTIONS,
    renderSideBySide: false,
    originalEditable: false,
    renderOverviewRuler: false,
    hideUnchangedRegions: { enabled: true },
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
