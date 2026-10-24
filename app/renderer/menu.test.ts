import { describe, expect, it } from "vitest";
import { placed } from "./menu";

const view = { w: 1200, h: 800 };
const box = { w: 180, h: 120 };

describe("where the menu goes", () => {
  it("is down and right of the pointer when it fits", () => {
    expect(placed({ x: 100, y: 100 }, box, view)).toEqual({ x: 100, y: 100 });
  });

  // A tab near the right edge used to open a menu half off the window.
  it("flips back inside when it would hang off an edge", () => {
    expect(placed({ x: 1100, y: 100 }, box, view)).toEqual({ x: 920, y: 100 });
    expect(placed({ x: 100, y: 760 }, box, view)).toEqual({ x: 100, y: 640 });
    expect(placed({ x: 1190, y: 790 }, box, view)).toEqual({ x: 1010, y: 670 });
  });

  it("stays on screen when it is taller than the window", () => {
    expect(placed({ x: 10, y: 10 }, { w: 180, h: 900 }, view)).toEqual({ x: 10, y: 4 });
  });
});
