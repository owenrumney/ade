import { describe, expect, it } from "vitest";
import { answered, questionsOf } from "./sharpen";

describe("answering a sharpener question", () => {
  it("adds the answer to the prompt as a line", () => {
    expect(answered("make the login faster", "Which login?", "CLI")).toBe(
      "make the login faster\nWhich login? CLI",
    );
  });

  it("does not stack blank lines under a prompt that ends in one", () => {
    expect(answered("make it faster\n\n", "Which?", "a")).toBe(
      "make it faster\nWhich? a",
    );
  });

  it("stands alone in an empty box", () => {
    expect(answered("", "Which?", "a")).toBe("Which? a");
  });
});

describe("reading the sharpener's answer", () => {
  it("takes the questions", () => {
    expect(
      questionsOf('{"questions":[{"ask":"Which?","options":["a","b"]}]}'),
    ).toEqual([{ ask: "Which?", options: ["a", "b"] }]);
  });

  it("drops a question that is not a choice", () => {
    expect(
      questionsOf('{"questions":[{"ask":"What?","options":[]},{"ask":"One?","options":["a"]}]}'),
    ).toEqual([]);
  });

  it("reads anything else as no questions", () => {
    expect(questionsOf(undefined)).toEqual([]);
    expect(questionsOf("")).toEqual([]);
    expect(questionsOf("<html>502</html>")).toEqual([]);
    expect(questionsOf('{"questions":null}')).toEqual([]);
  });
});
