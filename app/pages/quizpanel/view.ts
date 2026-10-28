// What the quiz panel draws, kept out of the page so it can be tested. The
// score never leaves here: it is not posted, stored or shown anywhere else.
export interface Question {
  ref: string;
  file: string;
  line: number;
  about?: string;
  question: string;
  options: string[];
  answer: string;
  why: string;
}

export interface Quiz {
  questions: Question[];
  files: number;
  base?: string;
  scope?: string;
  stale?: boolean;
}

// Picked is one answer, kept in the order it was given.
export interface Picked {
  ref: string;
  chose: string;
  right: boolean;
}

export function letter(i: number): string {
  return String.fromCharCode(65 + i);
}

export function index(answer: string): number {
  return answer.charCodeAt(0) - 65;
}

// mark is against the answers that came with the questions: instant, free,
// and it cannot be wrong in a new way.
export function mark(q: Question, chose: string): Picked {
  return { ref: q.ref, chose, right: chose === q.answer };
}

// progress is the row of dots: where you are, and how each one went.
export type Dot = "right" | "wrong" | "here" | "ahead";

export function progress(total: number, done: Picked[], at: number): Dot[] {
  return Array.from({ length: total }, (_, i) => {
    if (i < done.length) return done[i].right ? "right" : "wrong";
    return i === at ? "here" : "ahead";
  });
}

// score is the sentence over the scorecard. The number stays here.
export function score(done: Picked[]): string {
  const right = done.filter((p) => p.right).length;
  return `${right} of ${done.length} right.`;
}

// header names the scope the quiz was written for, so a quiz on a third of
// the change is visible rather than silent.
export function header(quiz: Quiz | undefined): string {
  if (!quiz) return "";
  const files = `${quiz.files} file${quiz.files === 1 ? "" : "s"}`;
  const where =
    quiz.scope === "uncommitted"
      ? "what is not committed"
      : quiz.scope === "committed"
        ? "what is committed"
        : `the branch against ${quiz.base || "its base"}`;
  return `${quiz.questions.length} questions about ${where}, ${files}.`;
}

// at is the question on screen, or undefined once they have all been answered.
export function at(quiz: Quiz | undefined, done: Picked[]): Question | undefined {
  return quiz?.questions[done.length];
}

export function finished(quiz: Quiz | undefined, done: Picked[]): boolean {
  return Boolean(quiz) && done.length === (quiz?.questions.length ?? 0);
}

// where is the link under a question: the hunk it is anchored to.
export function where(q: Question): string {
  return `${q.file}:${q.line}`;
}

// moved is whether the answers on screen were given about a different quiz.
// A regenerate replaces the questions, so the marks have to go with them.
export function moved(was: Quiz | undefined, now: Quiz | undefined): boolean {
  const refs = (q: Quiz | undefined) =>
    (q?.questions ?? []).map((one) => `${one.ref}\u0000${one.question}`).join("\n");
  return refs(was) !== refs(now);
}
