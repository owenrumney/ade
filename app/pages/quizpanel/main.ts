import {
  at,
  finished,
  header,
  index,
  letter,
  mark,
  moved,
  progress,
  score,
  where,
  type Picked,
  type Question,
  type Quiz,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("quiz") as HTMLElement;

interface Carried {
  kind?: string;
  quiz?: Quiz;
  busy?: boolean;
  note?: string;
}

let quiz: Quiz | undefined;
let busy = false;
let note = "";
// The answers, in the order they were given. They are never sent anywhere.
let done: Picked[] = [];
// The mark on screen, until Next moves on.
let shown: Picked | undefined;

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function send(what: string, extra: Record<string, unknown> = {}): void {
  host.postMessage({ kind: "do", what, ...extra });
}

function button(label: string, go: () => void, off = false): HTMLElement {
  const node = el("button", undefined, label) as HTMLButtonElement;
  node.disabled = off;
  node.addEventListener("click", go);
  return node;
}

// anchor is the file and line as a link that opens the editor at it.
function anchor(file: string, line: number, label: string): HTMLElement {
  const node = el("button", "linkish", label) as HTMLButtonElement;
  node.addEventListener("click", () => send("open", { file, line }));
  return node;
}

function drawHead(): void {
  const head = el("header", "head");
  head.append(el("h1", undefined, "Quiz me"));
  if (quiz) head.append(el("p", "sub", header(quiz)));
  if (quiz?.stale)
    head.append(
      el("p", "warn", "The diff has moved since these were written."),
    );
  const row = el("div", "row verbs");
  if (quiz)
    row.append(button(busy ? "Writing…" : "Regenerate", () => regenerate(), busy));
  if (busy) row.append(button("Cancel", () => send("cancel")));
  head.append(row);
  root.append(head);
}

function drawDots(): void {
  if (!quiz) return;
  const dots = el("div", "dots");
  for (const dot of progress(quiz.questions.length, done, done.length))
    dots.append(el("span", `dot ${dot}`));
  root.append(dots);
}

function drawQuestion(q: Question): void {
  const box = el("section", "question");
  box.append(el("p", "ask", q.question));

  const list = el("div", "options");
  q.options.forEach((option, i) => {
    const pick = letter(i);
    const row = el("button", "option") as HTMLButtonElement;
    row.append(el("span", "letter", pick), el("span", "text", option));
    if (shown) {
      row.disabled = true;
      if (pick === q.answer) row.classList.add("right");
      else if (pick === shown.chose) row.classList.add("wrong");
    } else {
      row.addEventListener("click", () => answer(q, pick));
    }
    list.append(row);
  });
  box.append(list);

  if (shown) {
    box.append(
      el("p", shown.right ? "verdict right" : "verdict wrong",
        shown.right ? "Right." : `Wrong — ${q.options[index(q.answer)]}.`),
      el("p", "why", q.why),
    );
    const links = el("div", "row links");
    links.append(anchor(q.file, q.line, where(q)));
    if (q.about) links.append(anchor(q.about, 1, q.about));
    box.append(links);
    // You cannot go back and change an answer: the mark is the point.
    box.append(
      verbs(button(finishedAfter() ? "See the scorecard" : "Next", () => next())),
    );
  }
  root.append(box);
}

function drawScorecard(): void {
  if (!quiz) return;
  root.append(el("h2", undefined, "How it went"));
  root.append(el("p", "tally", score(done)));

  for (const [i, q] of quiz.questions.entries()) {
    const picked = done[i];
    const row = el("section", picked.right ? "card right" : "card wrong");
    row.append(el("p", "ask", q.question));
    row.append(
      el("p", "chose", `You picked: ${q.options[index(picked.chose)]}`),
    );
    if (!picked.right)
      row.append(el("p", "was", `Right: ${q.options[index(q.answer)]}`));
    row.append(el("p", "why", q.why));
    const links = el("div", "row links");
    links.append(anchor(q.file, q.line, where(q)));
    if (q.about) links.append(anchor(q.about, 1, q.about));
    row.append(links);
    root.append(row);
  }

  root.append(
    el(
      "p",
      "caveat",
      "These questions were written from the diff by the same kind of model that wrote it, so a change that is confidently wrong makes a quiz that is confidently wrong about the same thing. Nothing here is stored, and nothing is blocked.",
    ),
  );
  root.append(verbs(button("Ask me again", () => regenerate())));
}

function verbs(...kids: HTMLElement[]): HTMLElement {
  const row = el("div", "row verbs");
  row.append(...kids);
  return row;
}

function finishedAfter(): boolean {
  return done.length === (quiz?.questions.length ?? 0);
}

function answer(q: Question, chose: string): void {
  shown = mark(q, chose);
  done = [...done, shown];
  draw();
}

function next(): void {
  shown = undefined;
  draw();
}

function regenerate(): void {
  done = [];
  shown = undefined;
  busy = true;
  send("regenerate");
  draw();
}

function draw(): void {
  root.replaceChildren();
  drawHead();
  if (note) root.append(el("p", "note", note));
  if (!quiz) {
    root.append(
      el(
        "p",
        "sub",
        busy
          ? "Reading the diff and writing the questions. Half a minute or so."
          : "Nothing to quiz yet.",
      ),
    );
    return;
  }
  drawDots();
  const q = shown ? quiz.questions[done.length - 1] : at(quiz, done);
  if (q) drawQuestion(q);
  else if (finished(quiz, done)) drawScorecard();
}

window.addEventListener("message", (event: MessageEvent<Carried>) => {
  const said = event.data;
  if (said.kind !== "state") return;
  // A fresh set of questions clears the marks given about the old ones.
  if (moved(quiz, said.quiz)) {
    done = [];
    shown = undefined;
  }
  quiz = said.quiz;
  busy = Boolean(said.busy);
  note = said.note ?? "";
  draw();
});

host.postMessage({ kind: "ready" });
