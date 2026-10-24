import { el } from "./omnibox";

// The popup at the pointer. The omnibox is the window's picker for
// everything asked of you; this is the one thing it cannot be — a menu
// about the thing under the cursor, opened where the cursor is.

export interface Row {
  label: string;
  run?: () => void;
  // off is a row that is listed and does nothing: close-right on the
  // last tab is worth showing greyed, not worth hiding.
  off?: boolean;
  // A line above this row, for a group that is not the one before it.
  apart?: boolean;
}

export interface At {
  x: number;
  y: number;
}

export interface Box {
  w: number;
  h: number;
}

// placed is where the menu goes: down and right of the pointer, flipped
// back inside the window when it would hang off an edge.
export function placed(at: At, menu: Box, view: Box, gap = 4): At {
  const x = at.x + menu.w + gap > view.w ? Math.max(gap, at.x - menu.w) : at.x;
  const y = at.y + menu.h + gap > view.h ? Math.max(gap, at.y - menu.h) : at.y;
  return { x, y };
}

let open: { node: HTMLElement; close: () => void } | undefined;

export function closeMenu(): void {
  open?.close();
}

// menu draws one and takes the pointer. Only one is ever up.
export function menu(at: At, rows: Row[]): void {
  closeMenu();
  const node = el("div", "menu");
  node.tabIndex = -1;
  for (const row of rows) {
    if (row.apart) node.append(el("div", "rule"));
    const item = el("div", row.off ? "item off" : "item", row.label);
    if (!row.off) {
      item.addEventListener("click", () => {
        closeMenu();
        row.run?.();
      });
    }
    node.append(item);
  }
  document.body.append(node);

  // Measured, then placed: the row widths are not known until it is in
  // the document.
  const size = { w: node.offsetWidth, h: node.offsetHeight };
  const to = placed(at, size, { w: window.innerWidth, h: window.innerHeight });
  node.style.left = `${to.x}px`;
  node.style.top = `${to.y}px`;
  node.focus();

  // One listener for every way out, so each is taken off again.
  const away = (e: Event) => {
    if (e instanceof KeyboardEvent && e.key !== "Escape") return;
    if (e.type === "pointerdown" && node.contains(e.target as Node)) return;
    closeMenu();
  };
  const close = () => {
    open = undefined;
    node.remove();
    window.removeEventListener("pointerdown", away, true);
    window.removeEventListener("keydown", away, true);
    window.removeEventListener("blur", away);
    window.removeEventListener("resize", away);
    window.removeEventListener("wheel", away, true);
  };
  open = { node, close };
  // Captured, so a click outside closes before it reaches what it hit.
  window.addEventListener("pointerdown", away, true);
  window.addEventListener("keydown", away, true);
  window.addEventListener("blur", away);
  window.addEventListener("resize", away);
  window.addEventListener("wheel", away, true);
}
