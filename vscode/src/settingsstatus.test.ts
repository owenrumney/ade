import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const src = readFileSync(join(__dirname, "settings.ts"), "utf8");
const markup = src.slice(src.indexOf("<!doctype html>"), src.lastIndexOf("</html>"));

describe("the settings page", () => {
  // A read that failed used to report into a div below six sections, so an
  // empty form looked like a machine with no settings.
  it("says what went wrong above the form, not under it", () => {
    expect(markup.indexOf('id="status"')).toBeLessThan(
      markup.indexOf('id="settings"'),
    );
  });

  it("claims back a panel the editor restored", () => {
    expect(src).toContain("revive(");
  });
});
