import { sections, type Board, type Do, type Line } from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("dashboard") as HTMLElement;

interface Shown {
  board?: Board;
  busy?: boolean;
  note?: string;
}

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

const GLYPHS: Record<string, string> = {
  bell: "◆",
  "bell-dot": "◆",
  question: "?",
  "git-pull-request": "⇄",
  "git-pull-request-draft": "⇄",
  issues: "◎",
  "debug-start": "▶",
  history: "↺",
  comment: "✎",
  "comment-discussion": "✎",
  clock: "◷",
  "folder-library": "▣",
  send: "➤",
  play: "▶",
  warning: "!",
  "circle-large-outline": "○",
  check: "✓",
  sync: "↻",
};

function glyph(icon: string): string {
  return GLYPHS[icon] ?? "•";
}

function act(what: Do): void {
  host.postMessage({ kind: "do", ...what });
}

function line(l: Line): HTMLElement {
  const row = el("div", "line");
  row.tabIndex = 0;
  row.setAttribute("role", "button");
  const icon = el("span", "icon", glyph(l.icon));
  const body = el("div", "body");
  body.append(el("div", "label", l.label));
  if (l.meta) body.append(el("div", "meta", l.meta));
  row.append(icon, body);
  if (l.also?.length) {
    const also = el("div", "also");
    for (const a of l.also) {
      const b = el("button", "also-btn", a.label);
      b.onclick = (e) => {
        e.stopPropagation();
        act(a.do);
      };
      also.append(b);
    }
    row.append(also);
  }
  row.onclick = () => act(l.do);
  row.onkeydown = (e) => {
    if (e.key === "Enter") act(l.do);
  };
  return row;
}

function draw(shown: Shown): void {
  root.replaceChildren();
  const head = el("header", "head");
  head.append(el("h1", undefined, "ADE"));
  if (shown.board?.agent) head.append(el("span", "agent", shown.board.agent));
  const note = el("p", "said", shown.note ?? (shown.busy ? "Reading…" : ""));
  head.append(note);
  const refresh = el("button", "refresh", shown.busy ? "Refreshing…" : "Refresh");
  refresh.onclick = () => act({ what: "refresh" });
  head.append(refresh);
  root.append(head);
  if (!shown.board) return;
  const grid = el("main", "grid");
  for (const s of sections(shown.board)) {
    const card = el("section", `card ${s.id}${s.lines.length ? "" : " empty"}`);
    const title = el("h2");
    title.append(el("span", undefined, s.title));
    if (s.lines.length) title.append(el("span", "count", String(s.lines.length)));
    card.append(title);
    if (s.lines.length === 0) card.append(el("p", "empty-note", s.empty));
    for (const l of s.lines) card.append(line(l));
    grid.append(card);
  }
  root.append(grid);
}

window.addEventListener("message", (event: MessageEvent<Shown & { kind?: string }>) => {
  if (event.data?.kind === "state") draw(event.data);
});
host.postMessage({ kind: "ready" });
