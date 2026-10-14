import { describe, expect, it } from "vitest";
import { at, done, inFile, orphans, type Pinned } from "./here";

const threads: Pinned[] = [
  { id: "a", file: "web.go", line: 12 },
  { id: "b", file: "web.go", line: 12, resolved: true },
  { id: "c", file: "web.go", line: 40, orphaned: true },
  { id: "d", file: "other.go", line: 12 },
];

describe("the threads a command acting on here means", () => {
  it("is the open ones on that line of that file", () => {
    expect(at(threads, "web.go", 12).map((t) => t.id)).toEqual(["a"]);
  });

  // The cursor is rarely on the exact line: resolving the file's comments is
  // more use than being told to go and find them.
  it("widens to the file when the cursor is not on one", () => {
    expect(inFile(threads, "web.go").map((t) => t.id)).toEqual(["a", "c"]);
  });

  it("never offers to resolve what is already resolved", () => {
    expect(at(threads, "web.go", 12).some((t) => t.resolved)).toBe(false);
    expect(inFile(threads, "web.go").some((t) => t.resolved)).toBe(false);
  });

  it("has nothing for a file with no comments", () => {
    expect(inFile(threads, "nope.go")).toEqual([]);
  });
});

describe("the orphans a prune would take", () => {
  it("is every thread whose code has gone, resolved or not", () => {
    expect(orphans(threads).map((t) => t.id)).toEqual(["c"]);
  });

  it("is empty when nothing is orphaned", () => {
    expect(orphans([{ id: "a", file: "x", line: 1 }])).toEqual([]);
  });
});

describe("the resolved comments a removal would take", () => {
  it("is the ones already dealt with", () => {
    expect(done(threads).map((t) => t.id)).toEqual(["b"]);
  });
});

// The store would only hand them back, or they live on the pull request and
// deleting the record here loses the fact that they went.
describe("what is never ours to delete", () => {
  const theirs: Pinned[] = [
    { id: "p", file: "x", line: 1, resolved: true, externalId: "gh-9" },
    {
      id: "s",
      file: "x",
      line: 2,
      resolved: true,
      sentAt: "2026-02-03T04:05:06Z",
    },
    { id: "o", file: "x", line: 3, orphaned: true, externalId: "gh-8" },
  ];

  it("is an imported comment or one already sent", () => {
    expect(done(theirs)).toEqual([]);
    expect(orphans(theirs)).toEqual([]);
  });

  // A zero time marshals as year 1, which is truthy and read as sent.
  it("does not mistake a zero time for having been sent", () => {
    const never: Pinned[] = [
      {
        id: "z",
        file: "x",
        line: 1,
        resolved: true,
        sentAt: "0001-01-01T00:00:00Z",
      },
    ];
    expect(done(never).map((t) => t.id)).toEqual(["z"]);
  });
});
