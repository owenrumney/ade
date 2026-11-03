import { describe, expect, it } from "vitest";
import { marksFor, nthOf, occurrences, spanOf, type Note } from "./notes";

const doc = "# Plan\n\nDebounce it.\n\n## Approach\n\nDebounce it.\n";
const first = doc.indexOf("Debounce it.");
const second = doc.lastIndexOf("Debounce it.");

function note(over: Partial<Note> & { quote: string; nth?: number }): Note {
  return {
    id: over.id ?? "n1",
    anchor: { quote: over.quote, nth: over.nth ?? 0 },
    comments: over.comments ?? [{ id: "c1", author: "architect", body: "how long?", at: "" }],
    resolved: over.resolved ?? false,
    orphaned: over.orphaned,
  };
}

describe("counting occurrences", () => {
  // The store walks the text the same way. If these two disagree, nth names
  // one place in Go and another on screen.
  it("does not overlap", () => {
    expect(occurrences("aaaa", "aa")).toEqual([0, 2]);
    expect(occurrences("abc", "z")).toEqual([]);
    expect(occurrences("abc", "")).toEqual([]);
  });
});

describe("where a note sits", () => {
  it("takes the occurrence the store named", () => {
    expect(spanOf(doc, note({ quote: "Debounce it.", nth: 1 }))).toEqual({ from: second, to: second + 12 });
    expect(spanOf(doc, note({ quote: "Debounce it.", nth: 0 }))).toEqual({ from: first, to: first + 12 });
  });

  // The page's text can be a beat behind the store, mid-edit.
  it("falls back to the first when the text has fewer of them than the store", () => {
    expect(spanOf("Debounce it.\n", note({ quote: "Debounce it.", nth: 1 }))).toEqual({
      from: 0,
      to: 12,
    });
  });

  it("has nowhere to sit when the quote has gone", () => {
    expect(spanOf("# Plan\n", note({ quote: "Debounce it." }))).toBeUndefined();
  });
});

describe("what gets drawn", () => {
  it("is in document order, and a resolved note is marked as one", () => {
    const marks = marksFor(doc, [
      note({ id: "second", quote: "Debounce it.", nth: 1 }),
      note({ id: "first", quote: "Debounce it.", nth: 0, resolved: true }),
    ]);
    expect(marks.map((m) => m.id)).toEqual(["first", "second"]);
    expect(marks[0].cls).toContain("cm-note-done");
    expect(marks[1].cls).not.toContain("cm-note-done");
  });

  it("skips a note whose quote is no longer in the document", () => {
    expect(marksFor("# Plan\n", [note({ quote: "Debounce it." })])).toEqual([]);
  });
});

describe("saying which span the page means", () => {
  it("counts the selection's occurrence", () => {
    expect(nthOf(doc, second, "Debounce it.")).toBe(1);
    expect(nthOf(doc, first, "Debounce it.")).toBe(0);
    // A selection that is not on an occurrence boundary is the first one.
    expect(nthOf(doc, first + 1, "Debounce it.")).toBe(0);
  });
});
