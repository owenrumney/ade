import { describe, expect, it } from "vitest";
import { closeAfter, inNewWindow } from "./window";

describe("where a checkout opens", () => {
  it("takes a new window when this one has a folder", () => {
    expect(inNewWindow("newWindow", 1)).toBe(true);
  });

  it("reuses this window when it has no folder open", () => {
    expect(inNewWindow("newWindow", 0)).toBe(false);
  });

  it("reuses this window when that is what was asked for", () => {
    expect(inNewWindow("currentWindow", 1)).toBe(false);
  });
});

describe("what the window does after a session closes", () => {
  it("closes by default", () => {
    expect(closeAfter("closeWindow", false)).toBe(true);
    expect(closeAfter(undefined, false)).toBe(true);
  });

  it("stays when that is what was asked for", () => {
    expect(closeAfter("stay", false)).toBe(false);
  });

  // A closed window takes the notification saying why the worktree stayed.
  it("stays when something was said", () => {
    expect(closeAfter("closeWindow", true)).toBe(false);
  });
});
