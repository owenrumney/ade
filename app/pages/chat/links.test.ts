import { describe, expect, it } from "vitest";
import { openable, splitLinks } from "./links";

const hrefs = (text: string) =>
  splitLinks(text)
    .filter((s) => s.href)
    .map((s) => s.href);

describe("the urls in what you typed", () => {
  it("is the whole url and the text around it", () => {
    expect(splitLinks("see https://x.test/a now")).toEqual([
      { text: "see " },
      { text: "https://x.test/a", href: "https://x.test/a" },
      { text: " now" },
    ]);
  });

  // A dev server prints one of these and it is the thing most worth clicking.
  it("takes a bare loopback host and port", () => {
    expect(hrefs("up on 127.0.0.1:5173/ now")).toEqual([
      "http://127.0.0.1:5173/",
    ]);
    expect(hrefs("localhost:3000")).toEqual(["http://localhost:3000"]);
  });

  it("leaves the sentence's punctuation out of the url", () => {
    expect(hrefs("read https://x.test/a.")).toEqual(["https://x.test/a"]);
    expect(hrefs("(see https://x.test/a)")).toEqual(["https://x.test/a"]);
  });

  it("keeps a bracket the url opened", () => {
    expect(hrefs("https://x.test/a_(b)")).toEqual(["https://x.test/a_(b)"]);
  });

  it("is not a port out of range", () => {
    expect(hrefs("localhost:99999")).toEqual([]);
  });

  it("is nothing in text with no url in it", () => {
    expect(splitLinks("just words")).toEqual([{ text: "just words" }]);
  });
});

describe("what the editor may be asked to open", () => {
  it("is the web and nothing else", () => {
    expect(openable("https://x.test")).toBe(true);
    expect(openable("http://127.0.0.1:1")).toBe(true);
  });

  // Rendered markdown can carry any href. A command uri handed to the editor
  // is a button in a chat message that runs something.
  it("is never a command, file or script uri", () => {
    for (const bad of [
      "command:workbench.action.terminal.new",
      "file:///etc/passwd",
      "javascript:alert(1)",
      "vscode://owenrumney.ade/reload",
    ]) {
      expect(openable(bad), bad).toBe(false);
    }
  });
});
