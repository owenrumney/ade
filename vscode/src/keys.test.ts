import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { taken } from "./keys";

const manifest = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as { contributes: { keybindings: { command: string; mac?: string; key?: string }[] } };

describe("the keybindings this extension contributes", () => {
  // cmd+alt+d hides the Dock. VS Code never sees it, so the binding looked
  // right in the manifest and did nothing when pressed.
  it("names none macOS takes first", () => {
    expect(taken(manifest.contributes.keybindings)).toEqual([]);
  });

  it("catches one that is taken", () => {
    expect(taken([{ command: "ade.menu", mac: "cmd+alt+d" }])).toEqual([
      "ade.menu",
    ]);
  });

  it("gives every ade binding a mac form", () => {
    for (const b of manifest.contributes.keybindings)
      expect(b.mac, b.command).toBeTruthy();
  });
});
