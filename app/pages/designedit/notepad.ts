// The floating note UI: the button a selection raises, and the thread that
// opens on a note. All the DOM, so main.ts keeps to the editor.

import { EditorView } from "@codemirror/view";
import { nthOf, type Note } from "./notes";

export interface Acts {
  file(quote: string, nth: number, body: string): void;
  reply(id: string, body: string): void;
  resolve(id: string, resolved: boolean): void;
  remove(id: string): void;
}

function el(tag: string, cls: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

export class Notepad {
  private readonly root = el("div", "note-pad");
  private at = 0;

  constructor(
    private readonly view: EditorView,
    private readonly acts: Acts,
  ) {
    this.root.style.display = "none";
    document.body.append(this.root);
    // A click inside must not reach the editor, which would move the cursor
    // and close what was just opened.
    this.root.addEventListener("mousedown", (e) => e.stopPropagation());
  }

  hide(): void {
    this.root.style.display = "none";
    this.root.replaceChildren();
  }

  // offer is the Comment button on a selection.
  offer(from: number, to: number): void {
    const quote = this.view.state.doc.sliceString(from, to);
    const button = el("button", "note-new", "Comment");
    button.addEventListener("click", () => this.compose(from, to, quote));
    this.show(from, [button]);
  }

  // open is the thread on an existing note.
  open(note: Note, at: number): void {
    const rows: HTMLElement[] = [];
    if (note.orphaned) rows.push(el("div", "note-gone", "the text this was on has gone"));
    for (const comment of note.comments) {
      const row = el("div", "note-comment");
      row.append(el("div", "note-who", comment.author), el("div", "note-said", comment.body));
      rows.push(row);
    }

    const box = el("textarea", "note-reply") as HTMLTextAreaElement;
    box.placeholder = "Reply";
    const send = el("button", "note-act", "Reply");
    send.addEventListener("click", () => {
      if (box.value.trim()) this.acts.reply(note.id, box.value.trim());
      this.hide();
    });
    const done = el("button", "note-act", note.resolved ? "Reopen" : "Resolve");
    done.addEventListener("click", () => {
      this.acts.resolve(note.id, !note.resolved);
      this.hide();
    });
    const drop = el("button", "note-act note-drop", "Delete");
    drop.addEventListener("click", () => {
      this.acts.remove(note.id);
      this.hide();
    });

    const buttons = el("div", "note-buttons");
    buttons.append(send, done, drop);
    this.show(at, [...rows, box, buttons]);
    box.focus();
  }

  private compose(from: number, to: number, quote: string): void {
    const box = el("textarea", "note-reply") as HTMLTextAreaElement;
    box.placeholder = "What is wrong with this?";
    const send = el("button", "note-act", "Comment");
    send.addEventListener("click", () => {
      const text = box.value.trim();
      if (text) this.acts.file(quote, nthOf(this.view.state.doc.toString(), from, quote), text);
      this.hide();
    });
    const buttons = el("div", "note-buttons");
    buttons.append(send);
    this.show(to, [el("div", "note-quote", quote), box, buttons]);
    box.focus();
  }

  private show(at: number, parts: HTMLElement[]): void {
    this.at = at;
    this.root.replaceChildren(...parts);
    this.root.style.display = "block";
    this.place();
  }

  // place pins the pad under the text it is about, clamped to the window.
  place(): void {
    if (this.root.style.display === "none") return;
    const spot = this.view.coordsAtPos(this.at);
    if (!spot) return;
    const width = this.root.offsetWidth || 280;
    this.root.style.top = `${spot.bottom + 6}px`;
    this.root.style.left = `${Math.max(8, Math.min(spot.left, window.innerWidth - width - 8))}px`;
  }
}
