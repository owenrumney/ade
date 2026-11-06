import { el } from "./omnibox";

// The Shots tab: the pictures in the session's outbox, one at a time.
// `make app-shots` writes them there and so does `ade show`.

export interface Shot {
  name: string;
  at: string;
  size: number;
}

export const SEEN = "ade.shots.seen";

export function shotsOf(got: unknown): Shot[] {
  const list = (got as { shots?: unknown } | undefined)?.shots;
  if (!Array.isArray(list)) return [];
  const out: Shot[] = [];
  for (const one of list) {
    const it = one as Partial<Shot>;
    if (typeof it?.name !== "string" || !it.name) continue;
    out.push({ name: it.name, at: String(it.at ?? ""), size: Number(it.size ?? 0) });
  }
  return out;
}

// stepped wraps both ways: a carousel has no ends.
export function stepped(count: number, at: number, by: number): number {
  if (count <= 0) return 0;
  return (((at + by) % count) + count) % count;
}

// held keeps the picture being looked at under it when the list changes,
// rather than jumping to whatever is now at that index.
export function holdAt(shots: Shot[], was: Shot | undefined, at: number): number {
  if (!shots.length) return 0;
  const again = was ? shots.findIndex((s) => s.name === was.name) : -1;
  if (again >= 0) return again;
  return Math.min(Math.max(at, 0), shots.length - 1);
}

// drawnFrom is what the carousel was built with: the pane is rebuilt only
// when the pictures change or the one in front does.
export function drawnFrom(shots: Shot[], at: number): string {
  return `${at}:${shots.map((s) => `${s.name}@${s.at}`).join(",")}`;
}

export function unseen(shots: Shot[], seen: Set<string>): number {
  return shots.filter((s) => !seen.has(s.name)).length;
}

export function readSeen(): Set<string> {
  try {
    const raw = localStorage.getItem(SEEN);
    const names = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(names) ? names.filter((n) => typeof n === "string") : []);
  } catch {
    return new Set();
  }
}

// Only what is still in the outbox is kept: the list is the whole of it.
export function writeSeen(shots: Shot[]): Set<string> {
  const now = new Set(shots.map((s) => s.name));
  try {
    localStorage.setItem(SEEN, JSON.stringify([...now]));
  } catch {
    // A tab that cannot remember still draws.
  }
  return now;
}

export function sizeOf(bytes: number): string {
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)}MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)}KB`;
  return `${bytes}B`;
}

// carousel draws the picture being looked at, with the way to the next.
export function carousel(
  shots: Shot[],
  at: number,
  src: (name: string) => string,
  go: (to: number) => void,
): HTMLElement {
  const out = el("div", "shots");
  if (!shots.length) {
    const none = el("div", "shots-none");
    none.append(
      el("p", "", "No pictures yet."),
      el("p", "dim", "make app-shots, or ade show <file>, and they land here."),
    );
    out.append(none);
    return out;
  }
  const now = shots[Math.min(Math.max(at, 0), shots.length - 1)];
  const head = el("div", "shots-head");
  const back = button("‹", "Previous", () => go(stepped(shots.length, at, -1)));
  const on = button("›", "Next", () => go(stepped(shots.length, at, 1)));
  head.append(back, el("span", "shots-name", now.name), on);

  const pic = el("div", "shots-pic");
  const img = document.createElement("img");
  img.src = src(now.name);
  img.alt = now.name;
  pic.append(img);

  const foot = el("div", "shots-foot");
  const dots = el("div", "shots-dots");
  shots.forEach((one, i) => {
    const dot = el("span", i === at ? "dot on" : "dot");
    dot.title = one.name;
    dot.addEventListener("click", () => go(i));
    dots.append(dot);
  });
  foot.append(dots, el("span", "dim", `${at + 1} of ${shots.length} · ${sizeOf(now.size)}`));

  out.append(head, pic, foot);
  out.tabIndex = 0;
  out.addEventListener("keydown", (e) => {
    if (e.key === "ArrowLeft") go(stepped(shots.length, at, -1));
    if (e.key === "ArrowRight") go(stepped(shots.length, at, 1));
  });
  return out;
}

function button(face: string, title: string, then: () => void): HTMLElement {
  const b = document.createElement("button");
  b.className = "shots-step";
  b.textContent = face;
  b.title = title;
  b.addEventListener("click", then);
  return b;
}
