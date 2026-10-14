import { describe, expect, it } from "vitest";
import { badge } from "./boxed";

describe("the sandbox indicator", () => {
  it("says nothing when the agent runs on the machine", () => {
    expect(badge(false, false)).toBeUndefined();
    expect(badge(undefined, true)).toBeUndefined();
  });

  it("shows while the agent is boxed, running or not", () => {
    expect(badge(true, false)?.text).toContain("sandboxed");
    expect(badge(true, true)?.text).toContain("sandboxed");
  });

  it("warns that leaving ends a turn in flight", () => {
    expect(badge(true, true)?.tooltip).toContain("ends the turn");
    expect(badge(true, false)?.tooltip).not.toContain("ends the turn");
  });
});
