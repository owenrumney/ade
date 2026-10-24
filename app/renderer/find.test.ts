import { describe, expect, it } from "vitest";
import { loose } from "./omnibox";
import { rank } from "./model";

// "open space" found nothing, because one substring cannot see past the "a"
// in "Open a space…". A verb you cannot find is a verb that does not exist.
describe("finding a verb by what you remember of it", () => {
  it("matches every word, in any order", () => {
    expect(loose("Open a space…", "open space")).toBe(true);
    expect(loose("Close a space…", "space close")).toBe(true);
    expect(loose("Open a space…", "open a space")).toBe(true);
    expect(loose("Open a space…", "space")).toBe(true);
  });

  it("still refuses a word that is not there", () => {
    expect(loose("Open a space…", "open worktree")).toBe(false);
    expect(loose("Open a space…", "zzz")).toBe(false);
  });

  it("ranks a whole-label match above a scattered one", () => {
    expect(rank("Open a space…", "Sessions", "open a space")).toBeLessThan(
      rank("Open a space…", "Sessions", "open space"),
    );
    expect(rank("Open a space…", "Sessions", "open space")).toBeGreaterThanOrEqual(0);
    expect(rank("Open a space…", "Sessions", "nothing here")).toBe(-1);
  });
});
