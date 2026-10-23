import { describe, expect, it } from "vitest";
import { changesIn, linesOf, toneOf, unsent, type Change } from "./changes";

const cell = (line: number, kind: string, content: string) => ({ line, kind, content });

const file: Change = {
  path: "internal/web/web.go",
  status: "modified",
  binary: false,
  additions: 2,
  deletions: 1,
  hunks: [
    {
      header: "@@ -10,3 +10,4 @@",
      heading: "func serve()",
      rows: [
        { left: cell(10, "context", "a"), right: cell(10, "context", "a") },
        { left: cell(11, "del", "was"), right: cell(11, "add", "is") },
        { left: null, right: cell(12, "add", "and more") },
      ],
    },
  ],
};

describe("the diff the window draws", () => {
  it("takes the files out of the server's answer", () => {
    expect(changesIn({ diff: { files: [file] } }).map((f) => f.path)).toEqual([
      "internal/web/web.go",
    ]);
    expect(changesIn({})).toEqual([]);
    expect(changesIn({ diff: { files: [{ path: "" }, file] } })).toHaveLength(1);
  });

  it("draws a replacement as the old line and then the new one", () => {
    expect(linesOf(file)).toEqual([
      { at: "hunk", text: "@@ -10,3 +10,4 @@ func serve()" },
      { at: "line", kind: "context", no: 10, cell: cell(10, "context", "a") },
      { at: "line", kind: "del", no: 11, cell: cell(11, "del", "was") },
      { at: "line", kind: "add", no: 11, cell: cell(11, "add", "is") },
      { at: "line", kind: "add", no: 12, cell: cell(12, "add", "and more") },
    ]);
  });

  it("has nothing to draw for a file with no hunks", () => {
    expect(linesOf({ ...file, hunks: null })).toEqual([]);
    expect(linesOf(undefined)).toEqual([]);
  });

  it("colours a file by what happened to it", () => {
    expect(toneOf("added")).toBe("added");
    expect(toneOf("?? untracked")).toBe("added");
    expect(toneOf("deleted")).toBe("removed");
    expect(toneOf("renamed")).toBe("changed");
  });
});

describe("comment threads on the diff", () => {
  const threads = [
    { id: "t1", file: "internal/web/web.go", side: "new", line: 11, comments: [{ id: "c", author: "you", body: "why?" }] },
    { id: "t2", file: "internal/web/web.go", side: "old", line: 11, comments: [] },
    { id: "t3", file: "other.go", side: "new", line: 1, comments: [] },
    { id: "t4", file: "internal/web/web.go", side: "new", line: 99, orphaned: true, comments: [] },
  ];

  it("puts each thread under the line it is anchored to", () => {
    const drawn = linesOf(file, threads).map((l) =>
      l.at === "thread" ? `#${l.thread.id}` : l.at === "hunk" ? "@@" : `${l.kind}:${l.no}`,
    );
    expect(drawn).toEqual([
      "@@",
      "context:10",
      "del:11",
      "#t2",
      "add:11",
      "#t1",
      "add:12",
      // The line it was pinned to has gone, so it is drawn at the end
      // rather than dropped.
      "#t4",
    ]);
  });

  it("leaves another file's threads out", () => {
    expect(linesOf(file, threads).some((l) => l.at === "thread" && l.thread.id === "t3")).toBe(false);
  });

  it("counts what a review would leave behind", () => {
    expect(unsent(threads)).toBe(4);
    expect(unsent([{ ...threads[0], sentAt: "now" }, { ...threads[1], resolved: true }])).toBe(0);
  });
});
