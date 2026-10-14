import { describe, expect, it } from "vitest";
import { openCount, threadOf } from "./comments";

const thread = { comments: [{ body: "x" }] };

describe("the thread a comment menu is about", () => {
  it("takes the thread itself, as the editor's menus pass it", () => {
    expect(threadOf(thread)).toBe(thread);
  });

  // The Comments view and the per-comment menus wrap it, and the button did
  // nothing when it was pressed from there.
  it("unwraps what the Comments view passes", () => {
    expect(threadOf({ thread })).toBe(thread);
    expect(threadOf({ parent: thread })).toBe(thread);
  });

  it("has nothing to say about anything else", () => {
    for (const bad of [undefined, null, "x", 3, {}]) {
      expect(threadOf(bad)).toBeUndefined();
    }
  });
});

describe("what Resolve All would send", () => {
  it("counts the open ones", () => {
    expect(openCount([{}, { resolved: true }, { resolved: false }])).toBe(2);
  });

  it("is nothing when every comment is resolved", () => {
    expect(openCount([{ resolved: true }])).toBe(0);
  });
});
