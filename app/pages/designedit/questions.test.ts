import { describe, expect, it } from "vitest";
import { cells } from "./marks";
import { columnsOf, questionsIn } from "./questions";

const table = `| # | Question | Options | Recommendation |
| - | -------- | ------- | -------------- |
| 1 | Where does a note live? | sidecar / inline | sidecar — the document stays the deliverable |
| 2 | Who sweeps an orphan? | nobody / a daily pass | nobody — the deleted sentence is the one worth reading |
`;

describe("spotting the table", () => {
  it("takes one with all three columns, whatever the case", () => {
    expect(columnsOf(["#", "Question", "Options", "Recommendation"])).toEqual({
      ref: 0,
      question: 1,
      options: 2,
      recommendation: 3,
    });
    expect(columnsOf(["question", "OPTIONS", "recommendation"])).toEqual({
      ref: -1,
      question: 0,
      options: 1,
      recommendation: 2,
    });
  });

  // Two of the three is some other table that happens to share a word.
  it("leaves an ordinary table alone", () => {
    expect(columnsOf(["Where", "Change"])).toBeUndefined();
    expect(columnsOf(["Question", "Options"])).toBeUndefined();
    expect(columnsOf(undefined)).toBeUndefined();
  });
});

describe("the rows that can be accepted", () => {
  it("reads them out of the drawn table", () => {
    const got = questionsIn(cells(table));
    expect(got).toHaveLength(2);
    expect(got[0].ref).toBe("1");
    expect(got[0].question).toBe("Where does a note live?");
    expect(got[0].recommendation).toContain("sidecar");
    expect(got[1].ref).toBe("2");
  });

  it("skips a row with nothing recommended yet", () => {
    const half = `| # | Question | Options | Recommendation |
| - | -------- | ------- | -------------- |
| 1 | Settled? | a / b | pick a |
| 2 | Still thinking | a / b |  |
`;
    const got = questionsIn(cells(half));
    expect(got).toHaveLength(1);
    expect(got[0].question).toBe("Settled?");
  });

  it("numbers the rows itself when the table has no # column", () => {
    const bare = `| Question | Options | Recommendation |
| -------- | ------- | -------------- |
| First? | a / b | a |
| Second? | a / b | b |
`;
    expect(questionsIn(cells(bare)).map((q) => q.ref)).toEqual(["1", "2"]);
  });

  it("finds nothing in an ordinary table", () => {
    expect(questionsIn(cells("| Where | Change |\n| - | - |\n| a.go | rewritten |\n"))).toEqual([]);
  });
});
