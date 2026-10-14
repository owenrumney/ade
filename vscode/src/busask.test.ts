import { describe, expect, it } from "vitest";
import type { BusOffer } from "./client";
import { askPage, escapeHtml, OWN_WORDS, pickItems } from "./busask";

const offer: BusOffer = {
  id: "msg_1",
  kind: "ask",
  from: "liam@infracost.io",
  agent: "claude-code",
  model: "claude-opus-5",
  question: "Is dev RDS safe to resize now?",
  options: ["yes", "no"],
};

describe("the quick pick", () => {
  it("offers the options and a way out of them", () => {
    const items = pickItems(offer);
    expect(items[0]?.label).toBe("yes");
    expect(items[1]?.label).toBe("no");
    expect(items[items.length - 1]?.label).toBe(OWN_WORDS);
  });

  it("still offers words of your own when there are no options", () => {
    expect(pickItems({ ...offer, options: null }).map((i) => i.label)).toContain(OWN_WORDS);
  });
});

describe("the panel", () => {
  it("carries the question, the options and who is asking", () => {
    const html = askPage(offer, "abc123");
    expect(html).toContain("Is dev RDS safe to resize now?");
    expect(html).toContain('data-option="yes"');
    expect(html).toContain("liam@infracost.io · claude-code · claude-opus-5");
  });

  it("locks the script to its nonce", () => {
    const html = askPage(offer, "abc123");
    expect(html).toContain("script-src 'nonce-abc123'");
    expect(html).toContain('<script nonce="abc123">');
  });

  it("does not let a question become markup", () => {
    const html = askPage({ ...offer, question: "<img onerror=x>", options: ['"><b>'] }, "n");
    expect(html).not.toContain("<img");
    expect(html).toContain("&lt;img");
    expect(escapeHtml('"><b>')).toBe("&quot;&gt;&lt;b&gt;");
  });
});
