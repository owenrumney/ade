import { describe, expect, it } from "vitest";
import { body, bullet, cells, hides, isMermaid, lang, level, picture, reveals, touched, gap } from "./marks";

describe("what the frame hides", () => {
  it("takes out the markers and leaves the words", () => {
    expect(hides("EmphasisMark")).toBe(true);
    expect(hides("HeaderMark")).toBe(true);
    expect(hides("Paragraph")).toBe(false);
  });

  // A list marker is drawn as a bullet rather than taken out: removing the
  // characters would move the line left and the list stop reading as one.
  it("takes a list marker like any other, since a bullet replaces it", () => {
    expect(hides("ListMark")).toBe(true);
    expect(reveals("ListMark", { from: 0, to: 1 }, { from: 0, to: 9 }, 400)).toBe(false);
    expect(reveals("ListMark", { from: 0, to: 1 }, { from: 0, to: 9 }, 5)).toBe(true);
  });
});

describe("the cursor is what reveals the source", () => {
  it("counts both edges, so a cursor just past a marker is still in it", () => {
    expect(touched({ from: 4, to: 6 }, 4)).toBe(true);
    expect(touched({ from: 4, to: 6 }, 6)).toBe(true);
    expect(touched({ from: 4, to: 6 }, 7)).toBe(false);
  });

  // An emphasis marker reveals with the whole emphasis, not only when the
  // cursor is on the two asterisks: you edit the word, not the syntax.
  it("reveals a marker when the cursor is anywhere in what it wraps", () => {
    const marker = { from: 0, to: 2 };
    const emphasis = { from: 0, to: 10 };
    expect(reveals("EmphasisMark", marker, emphasis, 5)).toBe(true);
    expect(reveals("EmphasisMark", marker, emphasis, 20)).toBe(false);
  });

  it("reveals a block only when the cursor is in the block", () => {
    expect(reveals("FencedCode", { from: 0, to: 30 }, { from: 0, to: 99 }, 50)).toBe(false);
    expect(reveals("FencedCode", { from: 0, to: 30 }, { from: 0, to: 99 }, 10)).toBe(true);
  });

  it("hides everything when nothing has the cursor", () => {
    expect(reveals("EmphasisMark", { from: 0, to: 2 }, { from: 0, to: 10 }, undefined)).toBe(false);
  });
});

describe("reading a block", () => {
  it("takes the level off a heading's mark", () => {
    expect(level("###")).toBe(3);
    expect(level("#")).toBe(1);
    expect(level("---")).toBe(0);
  });

  it("names the language, and spots a diagram", () => {
    expect(lang("```mermaid")).toBe("mermaid");
    expect(lang("~~~ts  ")).toBe("ts");
    expect(lang("```")).toBe("");
    expect(isMermaid("```mermaid")).toBe(true);
    expect(isMermaid("```go")).toBe(false);
  });

  it("gives mermaid the body without the fences", () => {
    expect(body("```mermaid\nflowchart LR\n  A --> B\n```")).toBe("flowchart LR\n  A --> B");
    expect(body("~~~\nplain\n~~~")).toBe("plain");
  });
});

describe("what a block is drawn as", () => {
  it("splits a table and drops the row that only aligns it", () => {
    expect(cells("| a | b |\n| --- | --- |\n| 1 | 2 |")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });

  it("keeps an alignment row's colons out too", () => {
    expect(cells("| a |\n| :-: |\n| 1 |")).toEqual([["a"], ["1"]]);
  });

  it("survives a table without outer pipes on every row", () => {
    expect(cells("| a | b |\n| - | - |\nnot a row\n| 1 | 2 |")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });
});

describe("a list marker", () => {
  // An ordered list's number reads the same either way, so it is not syntax.
  it("is a bullet when it is a dash, and left alone when it is a number", () => {
    expect(bullet("-")).toBe("•");
    expect(bullet("*")).toBe("•");
    expect(bullet("1.")).toBeUndefined();
  });
});

describe("the space after a heading's hashes", () => {
  // Hiding only the # run left every heading indented by one space.
  it("goes with them, however much of it there is", () => {
    expect(gap(" Title")).toBe(1);
    expect(gap("   Title")).toBe(3);
    expect(gap("\tTitle")).toBe(1);
    expect(gap("Title")).toBe(0);
    expect(gap("")).toBe(0);
  });
});

describe("pictures beside the document", () => {
  const base = "https://file+.vscode-resource.vscode-cdn.net/Users/owen/docs";

  it("resolves a plain name against the document's folder", () => {
    expect(picture("![The page](repo-page.png)", base)).toEqual({
      alt: "The page",
      url: `${base}/repo-page.png`,
    });
  });

  it("keeps a title out of the path", () => {
    expect(picture('![a](shots/one.png "One")', base)?.url).toBe(`${base}/shots/one.png`);
  });

  it("leaves anything that is not beside the document as source", () => {
    expect(picture("![a](https://example.com/x.png)", base)).toBeUndefined();
    expect(picture("![a](/etc/x.png)", base)).toBeUndefined();
    expect(picture("![a](x.png)", "")).toBeUndefined();
  });
});
