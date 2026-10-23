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

  // One key, no chord: cmd+k on its own opens the omnibox, so the stock
  // cmd+k chords are given up on purpose.
  it("puts the omnibox on cmd+k", () => {
    const box = manifest.contributes.keybindings.find((b) => b.command === "ade.omnibox");
    expect(box?.mac).toBe("cmd+k");
    expect(box?.key).toBe("ctrl+k");
  });
});
