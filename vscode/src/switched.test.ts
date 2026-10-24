import { describe, expect, it } from "vitest";
import { switchSaid } from "./switched";

describe("what a branch switch says", () => {
  it("is the branch, when nothing moved", () => {
    expect(switchSaid({ branch: "main" })).toBe("on main");
  });

  it("says work was set aside, because the files look gone otherwise", () => {
    expect(switchSaid({ branch: "main", stashed: 3 })).toBe(
      "on main; 3 files set aside, and kept for when you come back",
    );
  });

  it("says work came back", () => {
    expect(switchSaid({ branch: "feat/x", restored: 1 })).toBe("on feat/x; 1 file put back");
  });

  it("names what conflicted, and says the stash still holds it", () => {
    expect(switchSaid({ branch: "feat/x", stuck: ["a.go", "b.go"], note: "ignored" })).toBe(
      "on feat/x; a.go, b.go conflicted and are still in the stash",
    );
  });

  it("passes a note through when nothing conflicted", () => {
    expect(switchSaid({ branch: "main", note: "stash 0000 is no longer in the stash" })).toBe(
      "on main; stash 0000 is no longer in the stash",
    );
  });
});
