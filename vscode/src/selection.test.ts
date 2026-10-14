import { describe, expect, it } from "vitest";
import { trimIndent, where } from "./selection";

describe("where a selection is", () => {
  it("is a file and a line", () => {
    expect(where("/repo", "/repo/internal/web/web.go", 12, 12)).toBe(
      "internal/web/web.go:12",
    );
  });

  it("is a span when it covers more than one", () => {
    expect(where("/repo", "/repo/a.go", 4, 9)).toBe("a.go:4-9");
  });

  it("names a file outside the root by what it was given", () => {
    // Better an absolute path than a row of ../.. nobody can read.
    expect(where("/repo", "/elsewhere/a.go", 1, 1)).toContain("a.go");
  });
});

describe("what gets attached", () => {
  it("drops the indentation every line shares", () => {
    // A selection from inside a function otherwise arrives as a column of
    // whitespace with the code pushed off to the right.
    expect(trimIndent("    if x {\n      y()\n    }")).toBe("if x {\n  y()\n}");
  });

  it("keeps indentation that is not shared", () => {
    expect(trimIndent("if x {\n  y()\n}")).toBe("if x {\n  y()\n}");
  });

  it("is not thrown off by a blank line in the middle", () => {
    expect(trimIndent("  a\n\n  b")).toBe("a\n\nb");
  });

  it("drops the trailing newline a line selection carries", () => {
    expect(trimIndent("  a\n")).toBe("a");
  });
});
