// Monaco, fetched the first time a file is edited: it was seven eighths
// of the window's bundle and most sessions never edit one.

// Which half of the editor area: one instance cannot be in two panes.
export type Half = 0 | 1;

interface Editor {
  mountDiff(half: Half, base: string, source: string, path: string, line: number): HTMLElement;
  edits(): { path: string; source: string } | undefined;
  saved(path: string, source: string): void;
  focus(half: Half): void;
}

declare global {
  interface Window {
    adeEditor?: Editor;
  }
}

let loading: Promise<Editor | undefined> | undefined;

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

// mountDiff is the file over the diff's left side, edited inline.
export function mountDiff(side: Half, base: string, source: string, path: string, line = 0): HTMLElement {
  return once(side, (editor) => editor.mountDiff(side, base, source, path, line));
}

// focus says which half a save is about.
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
    if (!editor) {
      host.replaceChildren(
        Object.assign(document.createElement("div"), {
          className: "nothing",
          textContent: "The editor did not load.",
        }),
      );
      return;
    }
    editor.focus(half);
    host.replaceChildren(draw(editor));
  });
  return host;
}

export function edits(): { path: string; source: string } | undefined {
  return window.adeEditor?.edits();
}

export function saved(path: string, source: string): void {
  window.adeEditor?.saved(path, source);
}
