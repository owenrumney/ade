import { describe, expect, it } from "vitest";
import {
  changesIn,
  drawnFrom,
  linesOf,
  listed,
  pairsOf,
  toneOf,
  unsent,
  wanted,
  type Change,
  type Thread,
} from "./changes";

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
  const threads: Thread[] = [
    { id: "t1", file: "internal/web/web.go", side: "right", line: 11, comments: [{ id: "c", author: "you", body: "why?", at: "" }] },
    { id: "t2", file: "internal/web/web.go", side: "left", line: 11, comments: [] },
    { id: "t3", file: "other.go", side: "right", line: 1, comments: [] },
    { id: "t4", file: "internal/web/web.go", side: "right", line: 99, orphaned: true, comments: [] },
  ] satisfies Thread[];

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

  // It was drawn twice: once under the line it still names, once at the end.
  it("draws an orphan that still names a drawn line once", () => {
    const stuck: Thread = { ...threads[3], id: "t5", line: 12 };
    const drawn = linesOf(file, [stuck]).filter((l) => l.at === "thread");
    expect(drawn).toHaveLength(1);
  });

  it("draws the pair on one row when the diff is split", () => {
    const drawn = pairsOf(file, threads).map((l) =>
      l.at === "thread"
        ? `#${l.thread.id}`
        : l.at === "hunk"
          ? "@@"
          : `${l.left?.content ?? "-"}|${l.right?.content ?? "-"}`,
    );
    expect(drawn).toEqual(["@@", "a|a", "was|is", "#t2", "#t1", "-|and more", "#t4"]);
  });

  it("leaves another file's threads out", () => {
    expect(linesOf(file, threads).some((l) => l.at === "thread" && l.thread.id === "t3")).toBe(false);
  });

  it("counts what a review would leave behind", () => {
    expect(unsent(threads)).toBe(4);
    expect(unsent([{ ...threads[0], sentAt: "now" }, { ...threads[1], resolved: true }])).toBe(0);
  });
});

describe("the review's open comments, as the sidebar lists them", () => {
  const said = (body: string) => [{ id: "c", author: "@security", body, at: "" }];
  const all: Thread[] = [
    { id: "low", file: "b.go", side: "right", line: 9, severity: "low", comments: said("Minor naming.") },
    { id: "crit", file: "a.go", side: "right", line: 3, severity: "critical", comments: said("## Disables Lambda scanning\n\nmore") },
    { id: "done", file: "a.go", side: "right", line: 4, severity: "high", resolved: true, comments: said("fixed") },
    { id: "gone", file: "a.go", side: "right", line: 5, severity: "high", sentAt: "2026-10-23T00:00:00Z", comments: said("sent") },
    { id: "plain", file: "a.go", side: "right", line: 7, comments: said("`x` is never read") },
  ];

  it("is worst first, then by file and line", () => {
    expect(listed(all).map((one) => one.id)).toEqual(["crit", "plain", "low"]);
  });

  it("is severity and the first line, with the markdown taken off", () => {
    const [crit, plain] = listed(all);
    expect([crit.severity, crit.title]).toEqual(["critical", "Disables Lambda scanning"]);
    expect([plain.severity, plain.title]).toEqual(["medium", "x is never read"]);
  });

  // Code names keep their underscores: `resource_types` is not resourcetypes.
  it("takes the markdown off and leaves the identifiers alone", () => {
    const one: Thread = { ...all[0], comments: said("`resource_types` leaves out **LAMBDA**") };
    expect(listed([one])[0].title).toBe("resource_types leaves out LAMBDA");
  });

  it("says what would be sent, once it is written up", () => {
    const written: Thread = { ...all[0], outgoing: "Consider a clearer name here." };
    expect(listed([written])[0].title).toBe("Consider a clearer name here.");
  });
});

// A comment picked in the sidebar lands on the comment, or on the line it
// is about when the diff does not draw the comment itself.
describe("where a jump lands", () => {
  it("asks for the comment first and the line as well", () => {
    expect(wanted({ thread: "t1", line: 42 })).toBe('[data-thread="t1"], [data-line="42"]');
    expect(wanted({ line: 42 })).toBe('[data-line="42"]');
    expect(wanted()).toBe("");
  });
});

// A diff pane is built once and kept, so what it was built from has to say
// when it is stale: the pull request arrives after the file is opened, and
// the thread it draws must offer Add to review without reopening it.
describe("when a drawn diff is stale", () => {
  const one: Thread = {
    id: "t1",
    file: "a.go",
    side: "right",
    line: 3,
    comments: [{ id: "c1", author: "security", body: "a finding", at: "" }],
    outgoing: "words for the author",
  };

  it("changes when the branch turns out to have a pull request", () => {
    const open = new Set<string>();
    expect(drawnFrom("a.go", [one], open, false)).not.toBe(drawnFrom("a.go", [one], open, true));
  });

  it("changes when a thread moves on, and not when another file's does", () => {
    const open = new Set<string>();
    const was = drawnFrom("a.go", [one], open, true);
    expect(drawnFrom("a.go", [{ ...one, draftReview: "REVIEW1" }], open, true)).not.toBe(was);
    expect(drawnFrom("a.go", [one, { ...one, id: "t2", file: "b.go" }], open, true)).toBe(was);
  });

  it("changes when a thread is unfolded", () => {
    expect(drawnFrom("a.go", [one], new Set(["t1"]), true)).not.toBe(
      drawnFrom("a.go", [one], new Set(), true),
    );
  });
});
