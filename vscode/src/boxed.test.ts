import { describe, expect, it } from "vitest";
import { badge, BOXED_BACKGROUND } from "./boxed";

describe("the sandbox indicator", () => {
  it("says nothing when the agent runs on the machine", () => {
    expect(badge(false, false)).toBeUndefined();
    expect(badge(undefined, true)).toBeUndefined();
  });

  it("shows while the agent is boxed, running or not", () => {
    expect(badge(true, false)?.text).toContain("sandboxed");
    expect(badge(true, true)?.text).toContain("sandboxed");
  });

  // The send button's own colour is not on offer: a status bar item takes
  // only the error and warning backgrounds.
  it("is filled, so the container is visible without reading the words", () => {
    expect(badge(true, false)?.background).toBe(BOXED_BACKGROUND);
    expect(BOXED_BACKGROUND).toBe("statusBarItem.warningBackground");
  });

  it("warns that leaving ends a turn in flight", () => {
    expect(badge(true, true)?.tooltip).toContain("ends the turn");
    expect(badge(true, false)?.tooltip).not.toContain("ends the turn");
  });
});
