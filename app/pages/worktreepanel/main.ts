import type { Overlap, Rev } from "../client";
import {
  blocker,
  clearable,
  type Lane,
  lanes,
  leaves,
  marked,
  mergeable,
  mergeSaid,
  said,
  short,
  stuckSaid,
  summary,
  type Tie,
  ties,
  tiedTo,
  TRUNK,
  wanted,
  where,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("worktrees") as HTMLElement;

// TRUNK_X is the base's rail, STEP the gap between rails, and TIE_STEP the
// gap between tie lines out to the right of the widest rail.
const TRUNK_X = 12;
const STEP = 16;
const TIE_STEP = 9;
const CURVE = 14;
const LANE = 52;

let graph: Overlap | undefined;
let busy = false;
let note = "";
// Which lanes are open to their commits, and which are ticked for removal.
// By path, both: the order changes under them.
const open = new Set<string>();
let picked = new Set<string>();
// Ticked for you once per read: a redraw must not undo what you chose.
let chosenFor = "";

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function svgEl(tag: string, attrs: Record<string, string | number>): SVGElement {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [key, value] of Object.entries(attrs))
    node.setAttribute(key, String(value));
  return node;
}

function send(what: string, extra: Record<string, unknown> = {}): void {
  host.postMessage({ kind: "do", what, ...extra });
}

function railX(column: number): number {
  return column === TRUNK ? TRUNK_X : TRUNK_X + (column + 1) * STEP;
}

function draw(): void {
  const rows = lanes(graph);
  const tied = ties(graph, rows);
  root.replaceChildren();

  const head = el("header", "head");
  head.append(el("h1", undefined, "Worktrees"), el("p", "said", summary(graph)));
  const refresh = el("button", undefined, busy ? "Reading…" : "Refresh");
  (refresh as HTMLButtonElement).disabled = busy;
  refresh.addEventListener("click", () => send("refresh"));
  head.append(refresh);
  root.append(head);

  if (note) root.append(el("p", "note", note));
  if (rows.length === 0) {
    root.append(
      el("p", "none", busy ? "Reading the repository…" : "Nothing to show."),
    );
    return;
  }

  root.append(
    el("p", "base", `${where(graph)} is the line on the left. Time runs down.`),
  );
  const board = el("div", "board");
  const stripe = el("div", "stripe");
  const list = drawLanes(rows, tied);
  board.append(stripe, list);
  root.append(board);
  // Drawn after the rows are in the page, from where they actually sit: a
  // lane opened to its commits moves every lane below it.
  stripe.append(drawRails(rows, tied, measure(list), list.offsetHeight));
  root.append(key(tied));
  root.append(bar(rows));
}

// bar is the consolidation: what is ticked, what clearing it will take, and
// the one button. Below the drawing, because you read it before you choose.
function bar(rows: Lane[]): HTMLElement {
  const chosen = [...picked];
  const box = el("div", "bar");
  const go = el("button", undefined, busy ? "Working…" : said(rows, chosen));
  (go as HTMLButtonElement).disabled = busy || chosen.length === 0;
  go.addEventListener("click", () => send("consolidate", { paths: chosen }));
  box.append(go);

  const first = rows
    .filter((row) => picked.has(row.node.path) && clearable(row))
    .flatMap((row) => {
      const clear = blocker(row)?.clear;
      return clear ? [`${clear} ${row.label}`] : [];
    });
  if (first.length) box.append(el("span", "said", `First: ${first.join(", ")}.`));

  const stuck = stuckSaid(rows);
  if (stuck) box.append(el("p", "note stuck", stuck));
  return box;
}

// Where measures what the rows ended up at: the middle of each lane's own
// head, and the middle of each commit row behind it.
interface Where {
  heads: number[];
  commits: number[][];
}

function measure(list: HTMLElement): Where {
  const top = list.getBoundingClientRect().top;
  const middle = (node: Element) => {
    const box = node.getBoundingClientRect();
    return box.top - top + box.height / 2;
  };
  const heads: number[] = [];
  const commits: number[][] = [];
  for (const item of list.children) {
    heads.push(middle(item.querySelector(".lane-head") ?? item));
    commits.push([...item.querySelectorAll(".commit")].map(middle));
  }
  return { heads, commits };
}

// drawRails is the metro map: the base down the left, a rail per worktree
// leaving the one it forked from, a dot at every commit the rails carry.
function drawRails(
  rows: Lane[],
  tied: Tie[],
  at: Where,
  height: number,
): SVGElement {
  const widest = rows.reduce((most, row) => Math.max(most, row.column), TRUNK);
  const ties = tied.reduce((most, tie) => Math.max(most, tie.column), -1);
  const width = railX(widest) + 10 + (ties + 1) * TIE_STEP;
  const svg = svgEl("svg", {
    class: "rails",
    width,
    height,
    viewBox: `0 0 ${width} ${height}`,
    "aria-hidden": "true",
  });

  const lastRow = at.commits[at.commits.length - 1] ?? [];
  const foot = lastRow[lastRow.length - 1] ?? at.heads[at.heads.length - 1] ?? height;
  svg.append(
    svgEl("line", { class: "trunk", x1: TRUNK_X, y1: at.heads[0] ?? 0, x2: TRUNK_X, y2: foot }),
  );

  rows.forEach((row, i) => {
    const x = railX(row.column);
    const y = at.heads[i] ?? i * LANE;
    const dots = at.commits[i] ?? [];
    const bottom = dots[dots.length - 1] ?? y;
    if (row.column !== TRUNK) svg.append(rail(row, at, x, y, bottom));
    svg.append(
      svgEl("circle", { class: tipClass(row), cx: x, cy: y, r: row.main ? 5 : 4 }),
    );
    for (const dot of dots)
      svg.append(svgEl("circle", { class: "dot commit", cx: x, cy: dot, r: 2.5 }));
  });

  const tieX = railX(widest) + 10;
  for (const tie of tied) {
    const from = at.heads[tie.from];
    const to = at.heads[tie.to];
    if (from === undefined || to === undefined) continue;
    const x = tieX + tie.column * TIE_STEP;
    svg.append(
      svgEl("path", {
        class: "tie",
        d: `M ${railX(rows[tie.from].column)} ${from} C ${x} ${from}, ${x} ${to}, ${railX(rows[tie.to].column)} ${to}`,
      }),
    );
  }
  return svg;
}

// rail is one worktree's line: out of whatever it left, then straight down
// through its own commits. The curve is where it diverged.
function rail(row: Lane, at: Where, x: number, y: number, bottom: number): SVGElement {
  const fromX = row.from === TRUNK ? TRUNK_X : railX(row.from);
  const start = row.from === TRUNK ? (at.heads[0] ?? y) : (at.heads[row.from] ?? y);
  const turn = Math.min(y - CURVE, start + CURVE);
  return svgEl("path", {
    class: "rail",
    d: `M ${fromX} ${start} L ${fromX} ${Math.max(start, turn - CURVE)} C ${fromX} ${turn}, ${x} ${turn}, ${x} ${Math.min(y, turn + CURVE)} L ${x} ${bottom}`,
  });
}

function tipClass(row: Lane): string {
  if (row.main) return "dot base";
  if (row.removable) return "dot tip spare";
  return "dot tip";
}

function drawLanes(rows: Lane[], tied: Tie[]): HTMLElement {
  const list = el("ul", "lanes");
  rows.forEach((row, i) => {
    const item = el("li", "lane");
    item.style.minHeight = `${LANE}px`;
    item.append(laneHead(row, rows, i, tied));
    if (open.has(row.node.path)) item.append(commitList(row, rows));
    list.append(item);
  });
  return list;
}

// The row holds three buttons rather than being one: a button inside a
// button is not html a browser will draw.
function laneHead(row: Lane, rows: Lane[], i: number, tied: Tie[]): HTMLElement {
  const head = el("div", "lane-head");
  // No checkbox on the checkout itself: it is not a candidate, and a dead
  // one beside it reads as a row you failed to tick.
  head.append(row.main ? el("span", "tick-gap") : tick(row));

  const fold = el("button", "fold");
  fold.setAttribute("aria-expanded", String(open.has(row.node.path)));
  fold.addEventListener("click", () => {
    if (open.has(row.node.path)) open.delete(row.node.path);
    else open.add(row.node.path);
    draw();
  });

  const title = el("span", "lane-name", row.label);
  if (row.main) title.append(el("span", "tag", "checkout"));
  if (row.node.kind === "review") title.append(el("span", "tag", "review"));
  fold.append(title);

  const facts = [row.state, commitsSaid(row), leaves(row, rows)]
    .filter(Boolean)
    .join(" · ");
  fold.append(el("span", "lane-state", facts));
  if (row.verdict)
    fold.append(
      el("span", row.removable ? "verdict spare" : "verdict", row.verdict),
    );
  for (const tie of tiedTo(tied, i))
    fold.append(el("span", "tag duplicates", tie.label));
  head.append(fold);

  const into = mergeable(row, rows);
  if (into) head.append(mergeButton(row, into));
  return head;
}

// mergeButton folds this branch into the one it left and drops the checkout.
// Only where the rails say there is somewhere for the work to go.
function mergeButton(row: Lane, into: Lane): HTMLElement {
  const go = el("button", "merge", mergeSaid(into)) as HTMLButtonElement;
  go.disabled = busy;
  go.title = `Merge ${row.label} into ${into.label}, then remove this worktree. A conflict changes nothing.`;
  go.addEventListener("click", () =>
    send("merge", { path: row.node.path, into: into.node.path }),
  );
  return go;
}

// tick is the row's checkbox, outside the head button so a click on it
// chooses rather than folding the lane open.
function tick(row: Lane): HTMLElement {
  const box = el("input", "tick") as HTMLInputElement;
  box.type = "checkbox";
  box.checked = picked.has(row.node.path);
  box.disabled = busy || !clearable(row);
  box.title = clearable(row)
    ? "Remove this worktree"
    : (blocker(row)?.why ?? "nothing to remove here");
  box.addEventListener("click", (event) => {
    event.stopPropagation();
    if (box.checked) picked.add(row.node.path);
    else picked.delete(row.node.path);
    draw();
  });
  return box;
}

// A verdict already says a worktree holds nothing; a count beside it is the
// same fact twice, and the weaker wording of the two.
function commitsSaid(row: Lane): string {
  if (row.node.why) return row.node.why;
  if (row.main) return "the base";
  const held = row.commits.length;
  if (held === 0) return row.verdict ? "" : "nothing of its own";
  return held === 1 ? "1 commit" : `${held} commits`;
}

function commitList(row: Lane, rows: Lane[]): HTMLElement {
  const list = el("ol", "commits");
  if (row.commits.length === 0) {
    list.append(el("li", "commit empty", row.node.why || "Nothing of its own."));
    return list;
  }
  for (const rev of row.commits) list.append(commit(rev, row, rows));
  return list;
}

function commit(rev: Rev, row: Lane, rows: Lane[]): HTMLElement {
  const item = el("li", "commit");
  item.append(el("code", "sha", short(rev.sha)));
  item.append(el("span", "subject", rev.subject));
  const also = marked(rev, rows, row.node.path);
  if (also) item.append(el("span", "tag elsewhere", also));
  return item;
}

// The key names only the tie, because a rail says what it is by where it
// goes. Nothing is drawn when there is no tie to explain.
function key(tied: Tie[]): HTMLElement {
  const list = el("p", "key");
  if (tied.length)
    list.append(
      el("span", "tag duplicates", "copied — the same patch on two rails"),
    );
  return list;
}

window.addEventListener("message", (event: MessageEvent<{ kind?: string; overlap?: Overlap; busy?: boolean; note?: string }>) => {
  const said = event.data;
  if (said.kind !== "state") return;
  graph = said.overlap;
  busy = Boolean(said.busy);
  note = said.note ?? "";
  // Ticked for you once per answer, and only the ones the commits prove are
  // spare. A second message about the same read leaves your choice alone.
  const stamp = (graph?.nodes ?? []).map((n) => n.path).join("\n");
  if (stamp !== chosenFor) {
    chosenFor = stamp;
    picked = new Set(wanted(lanes(graph)));
  }
  draw();
});

draw();
host.postMessage({ kind: "ready" });
