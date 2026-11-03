// Monaco, fetched the first time a file is opened: it was seven eighths
// of the window's bundle and most sessions never open a file.

export interface Where {
  path: string;
  line: number;
}

export interface Asking {
  hover(path: string, line: number, col: number): Promise<string>;
  definition(path: string, line: number, col: number): Promise<Where | undefined>;
}

// Which half of the editor area: one instance cannot be in two panes.
export type Half = 0 | 1;

interface Editor {
  mount(
    half: Half,
    source: string,
    path: string,
    line: number,
    onLine: (n: number) => void,
  ): HTMLElement;
  mountDiff(
    half: Half,
    base: string,
    source: string,
    path: string,
    line: number,
    onLine: (n: number) => void,
  ): HTMLElement;
  selected(): { from: string; text: string } | undefined;
  edits(): { path: string; source: string } | undefined;
  saved(path: string, source: string): void;
  focus(half: Half): void;
  ask(how: Asking): void;
  onJump(then: (to: Where) => void): void;
}

declare global {
  interface Window {
    adeEditor?: Editor;
  }
}

let loading: Promise<Editor | undefined> | undefined;
// What the editor asks the language server through, and where a
// definition in another file goes. Held until the bundle is here.
let how: Asking | undefined;
let jump: ((to: Where) => void) | undefined;

// reaching says how the editor should ask, before it exists.
export function reaching(ask: Asking, onJump: (to: Where) => void): void {
  how = ask;
  jump = onJump;
  const ready = window.adeEditor;
  if (ready) {
    ready.ask(ask);
    ready.onJump(onJump);
  }
}

// What the window puts in a pane, one per half and made once: a new
// wrapper per draw would take the editor out of the document.
const hosts: [HTMLElement | undefined, HTMLElement | undefined] = [undefined, undefined];
// The half a verb about "the file" means. Held until the bundle is here.
let half: Half = 0;

function fetchEditor(): Promise<Editor | undefined> {
  loading ??= new Promise<Editor | undefined>((done) => {
    // Monaco's own stylesheet went with its bundle, so it comes too.
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = "../dist/editor.css";
    document.head.append(css);
    const tag = document.createElement("script");
    tag.src = "../dist/editor.js";
    tag.addEventListener("load", () => done(window.adeEditor));
    tag.addEventListener("error", () => done(undefined));
    document.head.append(tag);
  });
  return loading;
}

export function mount(
  side: Half,
  source: string,
  path: string,
  line: number,
  onLine: (n: number) => void,
): HTMLElement {
  return once(side, (editor) => editor.mount(side, source, path, line, onLine));
}

// mountDiff is the file over the diff's left side, edited inline.
export function mountDiff(
  side: Half,
  base: string,
  source: string,
  path: string,
  line: number,
  onLine: (n: number) => void,
): HTMLElement {
  return once(side, (editor) => editor.mountDiff(side, base, source, path, line, onLine));
}

// focus says which half a selection or a save is about.
export function focusEditor(side: Half): void {
  half = side;
  window.adeEditor?.focus(side);
}

function once(side: Half, draw: (editor: Editor) => HTMLElement): HTMLElement {
  const host = (hosts[side] ??= Object.assign(document.createElement("div"), {
    className: "monaco-host",
  }));
  const ready = window.adeEditor;
  if (ready) {
    host.replaceChildren(draw(ready));
    return host;
  }
  host.replaceChildren(
    Object.assign(document.createElement("div"), {
      className: "nothing",
      textContent: "Opening the editor…",
    }),
  );
  void fetchEditor().then((editor) => {
    if (editor && how && jump) {
      editor.ask(how);
      editor.onJump(jump);
      editor.focus(half);
    }
    if (!editor) {
      host.replaceChildren(
        Object.assign(document.createElement("div"), {
          className: "nothing",
          textContent: "The editor did not load.",
        }),
      );
      return;
    }
    host.replaceChildren(draw(editor));
  });
  return host;
}

// Nothing is selected until the editor is there to have a selection.
export function selected(): { from: string; text: string } | undefined {
  return window.adeEditor?.selected();
}

export function edits(): { path: string; source: string } | undefined {
  return window.adeEditor?.edits();
}

export function saved(path: string, source: string): void {
  window.adeEditor?.saved(path, source);
}
