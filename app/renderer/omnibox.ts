import { icon } from "./icons";
import { hitIcon, hitName, hits, hitWhere, type Hit, type Place } from "./model";

// Omnibox is the one place anything is put to a person: ⌘K, a large centred
// box over a dimmed window, and the same rows every list draws.
export class Omnibox {
  private scrim?: HTMLElement;
  private at = 0;
  private shown: Hit[] = [];

  constructor(
    private places: () => Place[],
    private pick: (hit: Hit) => void,
  ) {}

  get open(): boolean {
    return Boolean(this.scrim);
  }

  // ask is the same box put to a question. Everything ade puts to a
  // person goes through here.
  ask<T>(
    title: string,
    rows: { label: string; note?: string; value: T }[],
  ): Promise<T | undefined> {
    return this.choose(title, rows, false) as Promise<T | undefined>;
  }

  // many takes more than one: space ticks, enter takes what is ticked,
  // and nothing ticked takes the row you are on.
  many<T>(
    title: string,
    rows: { label: string; note?: string; value: T }[],
  ): Promise<T[] | undefined> {
    return this.choose(title, rows, true) as Promise<T[] | undefined>;
  }

  private choose<T>(
    title: string,
    rows: { label: string; note?: string; value: T }[],
    multi: boolean,
  ): Promise<T | T[] | undefined> {
    return new Promise((done) => {
      this.close();
      let at = 0;
      let shown = rows;
      const ticked = new Set<T>();
      const scrim = el("div", "scrim");
      const box = el("div", "omnibox");
      const input = document.createElement("input");
      input.placeholder = multi ? `${title}   space to tick, enter when done` : title;
      input.spellcheck = false;
      const list = el("div", "hits");
      box.append(input, list);
      scrim.append(box);
      document.body.append(scrim);
      this.scrim = scrim;

      const finish = (value: T | T[] | undefined) => {
        this.close();
        done(value);
      };
      const tick = (value: T) => {
        if (ticked.has(value)) ticked.delete(value);
        else ticked.add(value);
        draw();
      };
      const draw = () => {
        const want = input.value.trim().toLowerCase();
        shown = want ? rows.filter((r) => loose(r.label, want)) : rows;
        at = Math.min(at, Math.max(0, shown.length - 1));
        list.replaceChildren(
          ...shown.map((row, i) => {
            const node = el("div", i === at ? "hit on" : "hit");
            if (multi) node.append(el("span", "tick", ticked.has(row.value) ? "✓" : ""));
            node.append(el("span", "", row.label));
            if (row.note) node.append(el("span", "what", row.note));
            node.addEventListener("mousedown", () =>
              multi ? tick(row.value) : finish(row.value),
            );
            return node;
          }),
        );
      };
      input.addEventListener("input", () => {
        at = 0;
        draw();
      });
      input.addEventListener("keydown", (e) => {
        if (e.key === "Escape") return finish(undefined);
        if (e.key === "ArrowDown") {
          e.preventDefault();
          at = Math.min(at + 1, shown.length - 1);
          return draw();
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          at = Math.max(at - 1, 0);
          return draw();
        }
        if (multi && e.key === " " && input.value === "") {
          e.preventDefault();
          const row = shown[at];
          if (row) tick(row.value);
          return;
        }
        if (e.key !== "Enter") return;
        if (!multi) return finish(shown[at]?.value);
        const some = [...ticked];
        finish(some.length ? some : shown[at] ? [shown[at].value] : []);
      });
      scrim.addEventListener("mousedown", (e) => {
        if (e.target === scrim) finish(undefined);
      });
      draw();
      input.focus();
    });
  }

  // type is the same box with nothing to choose from: what you type is
  // the answer. A name, a port, a url.
  type(title: string, value = ""): Promise<string | undefined> {
    return new Promise((done) => {
      this.close();
      const scrim = el("div", "scrim");
      const box = el("div", "omnibox");
      const input = document.createElement("input");
      input.placeholder = title;
      input.value = value;
      input.spellcheck = false;
      box.append(input, el("div", "hits typing", title));
      scrim.append(box);
      document.body.append(scrim);
      this.scrim = scrim;

      const finish = (said: string | undefined) => {
        this.close();
        done(said);
      };
      input.addEventListener("keydown", (e) => {
        if (e.key === "Escape") return finish(undefined);
        // Enter on an empty box is an empty answer, not a change of mind:
        // a few verbs read one, and only escape cancels.
        if (e.key === "Enter") finish(input.value.trim());
      });
      scrim.addEventListener("mousedown", (e) => {
        if (e.target === scrim) finish(undefined);
      });
      input.focus();
      input.select();
    });
  }

  // Whether the box is up. Escape is the shell's key before it is the
  // page's, so the window has to know what it is closing.
  showing(): boolean {
    return Boolean(this.scrim);
  }

  toggle(): void {
    if (this.scrim) this.close();
    else this.show();
  }

  close(): void {
    this.scrim?.remove();
    this.scrim = undefined;
  }

  show(): void {
    this.close();
    const scrim = el("div", "scrim");
    const box = el("div", "omnibox");
    const input = document.createElement("input");
    input.placeholder = "Anything: a pull request, a design, a session, a file";
    input.spellcheck = false;
    const list = el("div", "hits");
    box.append(input, list);
    scrim.append(box);
    document.body.append(scrim);
    this.scrim = scrim;

    const draw = () => {
      this.shown = hits(this.places(), input.value);
      this.at = Math.min(this.at, Math.max(0, this.shown.length - 1));
      list.replaceChildren(...this.shown.map((one, i) => this.hit(one, i)));
    };
    input.addEventListener("input", () => {
      this.at = 0;
      draw();
    });
    input.addEventListener("keydown", (e) => this.key(e, draw));
    // A click on the dimming is a way out that needs no keyboard.
    scrim.addEventListener("mousedown", (e) => {
      if (e.target === scrim) this.close();
    });
    draw();
    input.focus();
  }

  private key(e: KeyboardEvent, draw: () => void): void {
    if (e.key === "Escape") return this.close();
    if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
      e.preventDefault();
      this.at = Math.min(this.at + 1, this.shown.length - 1);
      return draw();
    }
    if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
      e.preventDefault();
      this.at = Math.max(this.at - 1, 0);
      return draw();
    }
    if (e.key === "Enter") {
      const one = this.shown[this.at];
      this.close();
      if (one) this.pick(one);
    }
  }

  private hit(one: Hit, i: number): HTMLElement {
    const dim = one.kind === "verb" && !one.verb.live ? " owed" : "";
    const row = el("div", (i === this.at ? "hit on" : "hit") + dim);
    const ico = el("span", "ico");
    ico.append(icon(hitIcon(one)));
    row.append(ico, el("span", "", hitName(one)), el("span", "what", hitWhere(one)));
    row.addEventListener("mousedown", () => {
      this.close();
      this.pick(one);
    });
    return row;
  }
}

// loose is the picker's match: every word, in any order, so a label with a
// small word in it is still found by the words that matter.
export function loose(label: string, want: string): boolean {
  const low = label.toLowerCase();
  if (low.includes(want)) return true;
  const words = want.split(/\s+/).filter(Boolean);
  return words.length > 1 && words.every((word) => low.includes(word));
}

export function el(tag: string, cls = "", text = ""): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text) node.textContent = text;
  return node;
}
