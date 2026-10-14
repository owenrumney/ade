import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { rows, same, treeIcon, treeLabel, treeRows } from "./queue";

const waiting = [
  { id: "q1", kind: "Chat", what: "fix the buttons" },
  { id: "q2", kind: "Chat", what: "" },
];

describe("the queue picker", () => {
  it("lists every waiting turn in the order it will run", () => {
    const got = rows(waiting, true);
    expect(got.map((r) => r.id)).toEqual(["q1", "q2"]);
    expect(got[0].label).toBe("fix the buttons");
    expect(got[0].description).toContain("next");
    expect(got[1].label).toBe("Chat");
  });

  it("offers to run the queue only when nothing is running", () => {
    expect(rows(waiting, false)[0].id).toBeUndefined();
    expect(rows(waiting, false)).toHaveLength(3);
    expect(rows([], false)).toEqual([]);
  });
});

describe("the queue view's rows", () => {
  const w = (id: string, kind: string, what: string) => ({ id, kind, what });

  it("numbers them, because position is when it runs", () => {
    const got = treeRows([
      w("q1", "Review", "review the diff"),
      w("q2", "Create PR", "open a pull request"),
    ]);
    expect(got.map(treeLabel)).toEqual(["1. Review", "2. Create PR"]);
    expect(got[0].next).toBe(true);
    expect(got[1].next).toBe(false);
  });

  it("marks the one that goes next", () => {
    const got = treeRows([w("q1", "Review", "x"), w("q2", "Resolve", "y")]);
    expect(treeIcon(got[0])).not.toBe(treeIcon(got[1]));
  });

  it("copes with nothing waiting", () => {
    expect(treeRows(undefined)).toEqual([]);
    expect(treeRows(null)).toEqual([]);
    expect(treeRows([])).toEqual([]);
  });

  // The state poll fires every few seconds; a redraw on every one would move
  // the row under a click that was already aimed at it.
  it("knows when nothing moved", () => {
    const a = treeRows([w("q1", "Review", "x")]);
    expect(same(a, treeRows([w("q1", "Review", "x")]))).toBe(true);
    expect(same(a, treeRows([w("q2", "Review", "x")]))).toBe(false);
    expect(same(a, treeRows([w("q1", "Review", "y")]))).toBe(false);
    expect(same(a, treeRows([]))).toBe(false);
  });
});

// Dropping a prompt and resending it loses its place. Rewording keeps it.
describe("rewording a queued prompt", () => {
  const extension = readFileSync(join(__dirname, "extension.ts"), "utf8");

  it("carries the whole prompt, not the truncated row", () => {
    const long = "a".repeat(400);
    const [row] = rows(
      [{ id: "q1", kind: "Chat", what: "aaa…", message: long }],
      true,
    );
    expect(row.message).toBe(long);
  });

  it("offers no editor for an entry with no wording", () => {
    const [row] = rows(
      [{ id: "q1", kind: "Commit message", what: "write one" }],
      true,
    );
    expect(row.message).toBeUndefined();
  });

  it("hides the picker while the input box is up", () => {
    const fn = extension.slice(extension.indexOf("button === REWORD"));
    const body = fn.slice(0, fn.indexOf("editQueued"));
    expect(body, "VS Code shows one quick input at a time").toContain(
      "pick.hide()",
    );
    expect(body).toContain("await input(");
  });

  it("brings the list back after the edit", () => {
    const fn = extension.slice(extension.indexOf("async function showQueue"));
    const after = fn.slice(fn.indexOf("editQueued"));
    expect(after).toContain("pick.show()");
    expect(after, "hidden for the edit is not gone").toContain(
      "if (!rewording) pick.dispose()",
    );
  });

  it("sends nothing when the box is dismissed or emptied", () => {
    const fn = extension.slice(extension.indexOf("button === REWORD"));
    const guard = fn.slice(0, fn.indexOf("editQueued"));
    expect(guard).toContain("said !== undefined");
    expect(guard).toContain("said.trim()");
  });
});
