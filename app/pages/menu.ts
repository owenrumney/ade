// The popup at the pointer, for a page: the window's own menu, ported.
// A page is an iframe and cannot ask the window to draw over it.

export interface Row {
  label: string;
  run?: () => void;
  off?: boolean;
  apart?: boolean;
}

export interface At {
  x: number;
  y: number;
}

// placed is where it goes: down and right of the pointer, flipped back
// inside the frame when it would hang off an edge.
export function placed(
  at: At,
  menu: { w: number; h: number },
  view: { w: number; h: number },
  gap = 4,
): At {
  const x = at.x + menu.w + gap > view.w ? Math.max(gap, at.x - menu.w) : at.x;
  const y = at.y + menu.h + gap > view.h ? Math.max(gap, at.y - menu.h) : at.y;
  return { x, y };
}

let open: (() => void) | undefined;

export function closeMenu(): void {
  open?.();
}

export function menu(at: At, rows: Row[]): void {
  closeMenu();
  const node = document.createElement("div");
  node.className = "menu";
  node.tabIndex = -1;
  for (const row of rows) {
    if (row.apart) {
      const rule = document.createElement("div");
      rule.className = "rule";
      node.append(rule);
    }
    const item = document.createElement("div");
    item.className = row.off ? "item off" : "item";
    item.textContent = row.label;
    if (!row.off) {
      item.addEventListener("click", () => {
        closeMenu();
        row.run?.();
      });
    }
    node.append(item);
  }
  document.body.append(node);

  const to = placed(
    at,
    { w: node.offsetWidth, h: node.offsetHeight },
    { w: window.innerWidth, h: window.innerHeight },
  );
  node.style.left = `${to.x}px`;
  node.style.top = `${to.y}px`;
  node.focus();

  const away = (e: Event) => {
    if (e instanceof KeyboardEvent && e.key !== "Escape") return;
    if (e.type === "pointerdown" && node.contains(e.target as Node)) return;
    closeMenu();
  };
  open = () => {
    open = undefined;
    node.remove();
    window.removeEventListener("pointerdown", away, true);
    window.removeEventListener("keydown", away, true);
    window.removeEventListener("blur", away);
    window.removeEventListener("resize", away);
    window.removeEventListener("wheel", away, true);
  };
  window.addEventListener("pointerdown", away, true);
  window.addEventListener("keydown", away, true);
  window.addEventListener("blur", away);
  window.addEventListener("resize", away);
  window.addEventListener("wheel", away, true);
}

// on puts a menu on an element: the page says what the rows are for the
// thing under the pointer, and nothing is drawn when there are none.
export function on(node: HTMLElement, rows: () => Row[]): void {
  node.addEventListener("contextmenu", (event) => {
    const list = rows();
    if (list.length === 0) return;
    event.preventDefault();
    event.stopPropagation();
    menu({ x: event.clientX, y: event.clientY }, list);
  });
}
