import { describe, expect, it } from "vitest";
import { WIDTHS, sideWidth, type Edge, bandHeight } from "./dock";

const EDGES = Object.keys(WIDTHS) as Edge[];

describe("the three columns a grip drags", () => {
  it("are the sessions, the Diff's files, and the list beside the bar", () => {
    expect(EDGES).toEqual(["sidebar", "files", "side"]);
  });

  // A column dragged to nothing reads as a bug, and one dragged across the
  // window takes the terminal with it.
  it("stop at their own narrowest and widest", () => {
    for (const edge of EDGES) {
      const { min, max } = WIDTHS[edge];
      expect(sideWidth(edge, 0), edge).toBe(min);
      expect(sideWidth(edge, -40), edge).toBe(min);
      expect(sideWidth(edge, 10_000), edge).toBe(max);
      expect(sideWidth(edge, min + 1), edge).toBe(min + 1);
    }
  });

  it("are whole pixels, wherever the pointer stopped", () => {
    expect(sideWidth("sidebar", 300.4)).toBe(300);
    expect(sideWidth("side", 300.6)).toBe(301);
  });

  it("each keep their width under their own name, in their own variable", () => {
    expect(new Set(EDGES.map((edge) => WIDTHS[edge].key)).size).toBe(EDGES.length);
    expect(new Set(EDGES.map((edge) => WIDTHS[edge].css)).size).toBe(EDGES.length);
    for (const edge of EDGES) expect(WIDTHS[edge].css, edge).toMatch(/^--[a-z]+-w$/);
  });

  // The list on the right grows leftwards: its grip is on its left edge.
  it("drag backwards only on the right", () => {
    expect(EDGES.filter((edge) => WIDTHS[edge].back)).toEqual(["side"]);
  });

  // A variable the stylesheet never reads is a grip that drags nothing.
  it("name variables the window's stylesheets set and read", async () => {
    const { readFileSync } = await import("node:fs");
    const { join } = await import("node:path");
    const theme = readFileSync(join(__dirname, "theme.css"), "utf8");
    const css = readFileSync(join(__dirname, "window.css"), "utf8");
    for (const edge of EDGES) {
      expect(theme, edge).toContain(`${WIDTHS[edge].css}:`);
      expect(css, edge).toContain(`var(${WIDTHS[edge].css})`);
    }
  });
});

describe("a dragged section's height", () => {
  it("stays within what the sidebar can show", () => {
    expect(bandHeight("run", 10)).toBe(48);
    expect(bandHeight("comments", 5000)).toBe(720);
    expect(bandHeight("run", 200.4)).toBe(200);
  });
});
