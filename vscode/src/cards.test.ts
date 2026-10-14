import { describe, expect, it } from "vitest";
import { detail, title, type Card } from "./cards";

function card(over: Partial<Card> = {}): Card {
  return { id: "c1", kind: "start", text: "rename the worker image", ...over };
}

describe("a card says what the press would do", () => {
  it("names the checkout work would start in", () => {
    expect(title(card({ name: "ade" }))).toBe("Start a conversation in ade?");
    expect(title(card({ name: "ade", design: true }))).toBe(
      "Start a design in ade?",
    );
  });

  it("names the conversation a message would join", () => {
    expect(title(card({ kind: "say", name: "gtail" }))).toBe(
      "Queue a turn for gtail?",
    );
  });

  it("falls back to whatever it knows rather than saying nothing", () => {
    expect(title(card({ name: "", dir: "/code/ade" }))).toContain("/code/ade");
  });
});

describe("what the press would send", () => {
  it("is the text, untouched", () => {
    expect(detail(card())).toBe("rename the worker image");
  });

  it("admits a rewrite, because the card changed while it was being read", () => {
    expect(detail(card({ drafts: 2 }))).toContain("rewritten 2×");
  });
});
