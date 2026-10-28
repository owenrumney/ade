import { describe, expect, it } from "vitest";
import {
  at,
  finished,
  header,
  mark,
  moved,
  progress,
  score,
  where,
  type Picked,
  type Question,
  type Quiz,
} from "./view";

function question(ref: string, answer = "B"): Question {
  return {
    ref,
    file: "internal/web/commit.go",
    line: 88,
    question: "What would a machine without the skill have got before?",
    options: ["No message at all", "The agent's refusal as the title", "The file list"],
    answer,
    why: "Parse takes the first conventional-commit line, and a refusal has none.",
  };
}

const quiz: Quiz = {
  questions: [question("Q1"), question("Q2"), question("Q3")],
  files: 23,
  base: "origin/main",
  scope: "branch",
};

describe("mark", () => {
  it("marks against the answer that came with the question", () => {
    expect(mark(question("Q1"), "B")).toEqual({ ref: "Q1", chose: "B", right: true });
    expect(mark(question("Q1"), "A")).toEqual({ ref: "Q1", chose: "A", right: false });
  });
});

describe("progress", () => {
  it("says how each answered one went, and where you are now", () => {
    const done: Picked[] = [
      { ref: "Q1", chose: "B", right: true },
      { ref: "Q2", chose: "A", right: false },
    ];
    expect(progress(4, done, 2)).toEqual(["right", "wrong", "here", "ahead"]);
  });
});

describe("at and finished", () => {
  it("walks the questions and ends on the scorecard", () => {
    const done: Picked[] = [];
    expect(at(quiz, done)?.ref).toBe("Q1");
    expect(finished(quiz, done)).toBe(false);

    done.push(mark(quiz.questions[0], "B"), mark(quiz.questions[1], "A"));
    expect(at(quiz, done)?.ref).toBe("Q3");

    done.push(mark(quiz.questions[2], "B"));
    expect(at(quiz, done)).toBeUndefined();
    expect(finished(quiz, done)).toBe(true);
  });

  it("is not finished before there is a quiz", () => {
    expect(finished(undefined, [])).toBe(false);
  });
});

describe("score", () => {
  it("counts what you got right, and goes no further than the panel", () => {
    expect(
      score([
        { ref: "Q1", chose: "B", right: true },
        { ref: "Q2", chose: "A", right: false },
        { ref: "Q3", chose: "B", right: true },
      ]),
    ).toBe("2 of 3 right.");
  });
});

describe("header", () => {
  it("names the scope, so a quiz on part of the change is visible", () => {
    expect(header(quiz)).toBe("3 questions about the branch against origin/main, 23 files.");
    expect(header({ ...quiz, scope: "uncommitted", files: 1 })).toBe(
      "3 questions about what is not committed, 1 file.",
    );
    expect(header(undefined)).toBe("");
  });
});

describe("where", () => {
  it("is the hunk the question is anchored to", () => {
    expect(where(question("Q1"))).toBe("internal/web/commit.go:88");
  });
});

describe("moved", () => {
  it("is false for the same questions arriving again", () => {
    expect(moved(quiz, { ...quiz, stale: true })).toBe(false);
  });

  it("is true once a regenerate has replaced them", () => {
    const fresh: Quiz = { ...quiz, questions: [question("Q1"), question("Q2")] };
    expect(moved(quiz, fresh)).toBe(true);
    expect(moved(undefined, quiz)).toBe(true);
  });
});
