import { describe, expect, it } from "vitest";
import { fileIn, ledgerIn, ticked, unreadNote, viewedIn, type LedgerFile } from "./viewed";

const file = (over: Partial<LedgerFile> = {}): LedgerFile => ({
  path: "a.ts",
  agent: 10,
  read: 4,
  unread: 6,
  viewed: false,
  ticked: [{ file: "a.ts", start: 1, lines: 4 }],
  ...over,
});

describe("the ledger as the window reads it", () => {
  it("takes what the server sent", () => {
    const got = ledgerIn({ files: [file()], agent: 10, read: 4, unread: 6 });
    expect(got.unread).toBe(6);
    expect(fileIn(got, "a.ts")?.agent).toBe(10);
    expect(fileIn(got, "b.ts")).toBeUndefined();
  });

  it("is empty when the server said nothing readable", () => {
    expect(ledgerIn(undefined).files).toEqual([]);
    expect(ledgerIn({ error: "no session" }).unread).toBe(0);
  });

  it("dims the files whose every hunk is ticked", () => {
    const got = ledgerIn({ files: [file({ viewed: true }), file({ path: "b.ts" })] });
    expect(viewedIn(got)).toEqual(new Set(["a.ts"]));
  });
});

describe("a hunk's box", () => {
  it("is ticked only when the whole span is claimed", () => {
    expect(ticked(file(), { file: "a.ts", start: 1, lines: 4 })).toBe(true);
    expect(ticked(file(), { file: "a.ts", start: 1, lines: 5 })).toBe(false);
    expect(ticked(undefined, { file: "a.ts", start: 1, lines: 1 })).toBe(false);
  });
});

describe("what the file head says", () => {
  it("counts what is left", () => {
    expect(unreadNote(file())).toBe("6 of 10 agent lines unread");
    expect(unreadNote(file({ unread: 0, read: 10 }))).toBe("10 agent lines, all read");
  });

  it("says nothing about a file the agent did not write", () => {
    expect(unreadNote(file({ agent: 0, read: 0, unread: 0 }))).toBe("");
    expect(unreadNote(undefined)).toBe("");
  });
});
