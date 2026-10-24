import { describe, expect, it } from "vitest";
import { draftKey, draftOf, keepDraft, type Store } from "./draft";

function store(): Store & { held: Map<string, string> } {
  const held = new Map<string, string>();
  return {
    held,
    getItem: (k) => held.get(k) ?? null,
    setItem: (k, v) => void held.set(k, v),
    removeItem: (k) => void held.delete(k),
  };
}

const one = "http://127.0.0.1:7111/s/aaa";
const two = "http://127.0.0.1:7111/s/bbb";

describe("the composer's draft", () => {
  it("comes back for the conversation it was typed in", () => {
    const held = store();
    keepDraft(held, one, "half a thought");
    expect(draftOf(held, one)).toBe("half a thought");
    expect(draftOf(held, two)).toBe("");
  });

  it("keeps one per conversation", () => {
    const held = store();
    keepDraft(held, one, "for one");
    keepDraft(held, two, "for two");
    expect(draftOf(held, one)).toBe("for one");
    expect(draftOf(held, two)).toBe("for two");
  });

  it("forgets a box that is empty or all whitespace", () => {
    const held = store();
    keepDraft(held, one, "typed");
    keepDraft(held, one, "   \n ");
    expect(held.held.has(draftKey(one))).toBe(false);
  });

  it("says nothing was typed when storage refuses", () => {
    const denied: Store = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
      removeItem: () => {
        throw new Error("denied");
      },
    };
    expect(() => keepDraft(denied, one, "typed")).not.toThrow();
    expect(draftOf(denied, one)).toBe("");
  });
});
