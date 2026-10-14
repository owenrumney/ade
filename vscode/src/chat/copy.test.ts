import { describe, expect, it } from "vitest";
import { copyable, copyWorthy } from "./copy";

describe("what a fenced block copies", () => {
  it("drops the newline the renderer closes every block with", () => {
    expect(copyable("make ui\n")).toBe("make ui");
    expect(copyable("a\n\nb\n\n")).toBe("a\n\nb");
  });

  it("keeps the blank lines inside the block", () => {
    expect(copyable("func a() {}\n\nfunc b() {}\n")).toBe(
      "func a() {}\n\nfunc b() {}",
    );
  });

  it("offers a button for code", () => {
    expect(copyWorthy("", "make ui\n")).toBe(true);
    expect(copyWorthy("chroma", "func a() {}\n")).toBe(true);
  });

  // A mermaid block is a picture by the time you see it.
  it("offers none for a diagram or an empty block", () => {
    expect(copyWorthy("mermaid", "flowchart TD\n  a --> b\n")).toBe(false);
    expect(copyWorthy("", "\n\n")).toBe(false);
    expect(copyWorthy("", "")).toBe(false);
  });
});
