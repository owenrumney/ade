import { describe, expect, it } from "vitest";
import { spans, summarise } from "./ranges";

describe("spans", () => {
  it("folds a run of lines into one", () => {
    // A turn writes a block, and one decoration per run draws the same picture.
    expect(spans([3, 4, 5])).toEqual([{ from: 3, to: 5 }]);
  });

  it("keeps a gap as a gap", () => {
    expect(spans([1, 2, 9])).toEqual([
      { from: 1, to: 2 },
      { from: 9, to: 9 },
    ]);
  });

  it("does not depend on the order it was given", () => {
    expect(spans([9, 2, 1])).toEqual(spans([1, 2, 9]));
  });

  it("drops repeats and anything that is not a line", () => {
    expect(spans([4, 4, 0, -1])).toEqual([{ from: 4, to: 4 }]);
  });

  it("has nothing to draw for nothing", () => {
    expect(spans([])).toEqual([]);
  });
});

describe("the hover", () => {
  it("names the commit a line landed in", () => {
    const text = summarise([
      {
        at: "2026-09-28T10:00:00Z",
        summary: "stamp the commits",
        commit: "abcdef1234567890",
      },
    ]);
    expect(text).toContain("stamp the commits");
    expect(text).toContain("abcdef12");
  });

  it("says plainly when the work is not committed yet", () => {
    // A line with no sha is a different answer from one with a sha, not a gap.
    const text = summarise([{ at: "", summary: "still working", commit: "" }]);
    expect(text).toContain("not committed yet");
  });

  it("lists every turn that touched the line", () => {
    const text = summarise([
      { at: "", summary: "first" },
      { at: "", summary: "second" },
    ]);
    expect(text).toContain("first");
    expect(text).toContain("second");
  });

  it("says nothing about a line no turn wrote", () => {
    expect(summarise([])).toBe("");
  });
});
