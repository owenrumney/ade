import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// tools/shots/editor.mjs rebuilds this page over an ade origin so `make shots`
// can photograph it outside VS Code. It carries its own copy of the markup, so
// an id added here and not there is a shot of a page nobody runs.
const page = readFileSync(join(__dirname, "chat.ts"), "utf8");
const harness = readFileSync(
  join(__dirname, "..", "..", "tools", "shots", "editor.mjs"),
  "utf8",
);

describe("the shot harness and the webview", () => {
  it("name the same elements", () => {
    for (const id of ["ade", "list", "composer", "tray", "sharpen", "box", "send", "hint", "note"]) {
      expect(page, `chat.ts lost #${id}`).toContain(`id="${id}"`);
      expect(harness, `editor.mjs is missing #${id}`).toContain(`id="${id}"`);
    }
  });

  // chat.css names --vscode-* and nothing else, so a token the harness does
  // not define is an unstyled element in every shot from then on.
  it("define every theme token the stylesheet reads", () => {
    const css = readFileSync(
      join(__dirname, "..", "media", "chat.css"),
      "utf8",
    );
    const wanted = new Set(
      [...css.matchAll(/var\(--vscode-([a-zA-Z-]+)/g)].map((m) => m[1]),
    );
    // Either spelling: a key that is a valid identifier is written bare.
    const missing = [...wanted].filter(
      (name) =>
        !new RegExp(`(^|[\\s{,])"?${name}"?\\s*:`, "m").test(harness),
    );
    expect(missing, "tools/shots/editor.mjs has no value for these").toEqual([]);
  });
});
