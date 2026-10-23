import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const src = readFileSync(join(__dirname, "settings.ts"), "utf8");
// The form itself is settings/body.ts, shared with ade's own app; the
// order of the fields within it is what this is about.
const markup = readFileSync(join(__dirname, "settings", "body.ts"), "utf8");

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
