import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const src = readFileSync(join(__dirname, "extension.ts"), "utf8");
const menu = readFileSync(join(__dirname, "menu.ts"), "utf8");

describe("renaming the session you are in", () => {
  // It was a row verb only, so the palette never offered it — and the one
  // conversation you most want to rename is the one you are reading.
  it("is offered in the ade menu", () => {
    expect(menu).toContain('"ade.renameSession"');
    const rowOnly = menu.slice(menu.indexOf("export const ROW_ONLY"));
    expect(rowOnly).not.toContain('"ade.renameSession"');
  });

  it("falls back to this window's own conversation", () => {
    const rename = src.slice(src.indexOf("async function renameSession"));
    expect(rename.slice(0, rename.indexOf("showInputBox"))).toContain(
      "runState?.session",
    );
  });

  // An empty box gave no hint of what it was called, and typing replaced
  // nothing: the name was the one thing you wanted to see.
  it("opens on the current name, selected", () => {
    const rename = src.slice(src.indexOf("async function renameSession"));
    const box = rename.slice(0, rename.indexOf("sessionVerb"));
    expect(box).toContain("value: now");
    expect(box).toContain("valueSelection");
    expect(box).toContain("Rename");
  });

  it("says so when there is no conversation to rename", () => {
    const rename = src.slice(src.indexOf("async function renameSession"));
    expect(rename.slice(0, rename.indexOf("showInputBox"))).toContain(
      "no conversation here to rename",
    );
  });
});
