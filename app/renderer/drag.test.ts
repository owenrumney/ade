import { describe, expect, it } from "vitest";
import { dragsWindow } from "./drag";

const hit = (...matched: string[]) => ({
  closest: (selector: string) => (matched.includes(selector) ? {} : null),
});

describe("dragging the window by its titlebar", () => {
  it("starts on the bar itself", () => {
    expect(dragsWindow(hit())).toBe(true);
  });

  // The two the titlebar draws: a drag that swallowed them would leave the
  // omnibox unopenable and the sessions unfoldable.
  it("does not start on the fold button or the omnibox", () => {
    expect(dragsWindow(hit("button"))).toBe(false);
    expect(dragsWindow(hit(".box"))).toBe(false);
  });

  it("ignores a target that is not an element", () => {
    expect(dragsWindow(null)).toBe(false);
    expect(dragsWindow(undefined)).toBe(false);
    expect(dragsWindow({} as never)).toBe(false);
  });

  // The declaration Chromium honoured and WebKit silently drops.
  it("leaves no -webkit-app-region in the window's own stylesheet", async () => {
    const { readFileSync } = await import("node:fs");
    const { join } = await import("node:path");
    const css = readFileSync(join(__dirname, "window.css"), "utf8");
    expect(css).not.toContain("-webkit-app-region");
  });
});
