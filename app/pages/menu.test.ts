import { describe, expect, it } from "vitest";
import { placed } from "./menu";

// A page is an iframe: a menu near the right edge of a narrow panel has
// to flip, or it is drawn outside the frame and clipped away.
describe("where a page's menu goes", () => {
  const box = { w: 180, h: 120 };
  const view = { w: 400, h: 300 };

  it("falls down and right of the pointer", () => {
    expect(placed({ x: 20, y: 20 }, box, view)).toEqual({ x: 20, y: 20 });
  });

  it("flips back inside when it would hang off", () => {
    expect(placed({ x: 380, y: 290 }, box, view)).toEqual({ x: 200, y: 170 });
  });

  it("never goes off the top or the left", () => {
    expect(placed({ x: 10, y: 10 }, { w: 500, h: 500 }, view)).toEqual({ x: 4, y: 4 });
  });
});
