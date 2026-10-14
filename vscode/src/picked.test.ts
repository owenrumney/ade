import { describe, expect, it } from "vitest";
import { picked, rel } from "./picked";

const uri = (fsPath: string) => ({ fsPath });

describe("picked", () => {
  it("takes the one row a click passes", () => {
    expect(picked([{ resourceUri: uri("/r/a.go") }])).toEqual([uri("/r/a.go")]);
  });

  it("takes every row of a group heading", () => {
    const group = {
      resourceStates: [
        { resourceUri: uri("/r/a.go") },
        { resourceUri: uri("/r/b.go") },
      ],
    };
    expect(picked([group])).toEqual([uri("/r/a.go"), uri("/r/b.go")]);
  });

  // A multi-select arrives as the clicked row and then an array of all of them.
  it("flattens the array a multi-select passes", () => {
    const got = picked([
      { resourceUri: uri("/r/a.go") },
      [{ resourceUri: uri("/r/b.go") }],
    ]);
    expect(got).toEqual([uri("/r/a.go"), uri("/r/b.go")]);
  });

  it("is empty for a palette invocation, which passes nothing", () => {
    expect(picked([])).toEqual([]);
    expect(picked([undefined])).toEqual([]);
  });
});

describe("rel", () => {
  it("names a path the way the server does", () => {
    expect(rel("/r", "/r/internal/web/web.go")).toBe("internal/web/web.go");
  });

  it("tolerates a root with a trailing slash", () => {
    expect(rel("/r/", "/r/a.go")).toBe("a.go");
  });

  // The diff root may not be this window's folder, and a path outside it is
  // one the server would refuse anyway.
  it("refuses a path outside the root rather than sending a bad one", () => {
    expect(rel("/r", "/other/a.go")).toBeUndefined();
    expect(rel("/r", "/rogue/a.go")).toBeUndefined();
  });
});
