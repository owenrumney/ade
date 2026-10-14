import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { shown } from "./pick";

describe("a widget shown through the omnibox", () => {
  it("shows once the big box has been asked for", async () => {
    const calls: string[] = [];
    const box = shown({ show: () => void calls.push("shown") });
    box.show();
    expect(calls).toEqual([]);
    await new Promise((done) => setTimeout(done, 0));
    expect(calls).toEqual(["shown"]);
  });
});

// The omnibox is the picker for everything: a stock quick input drops a
// verb's follow-up into the small widget.
describe("the extension's quick inputs", () => {
  it("all go through the omnibox", () => {
    const stock =
      /\b(showQuickPick|showInputBox|createQuickPick|createInputBox)\(/;
    const files = readdirSync(__dirname, { recursive: true, encoding: "utf8" })
      .filter((f) => f.endsWith(".ts") && !f.endsWith(".test.ts"))
      .filter((f) => f !== "pick.ts" && f !== "vscode.stub.ts")
      .filter((f) => stock.test(readFileSync(join(__dirname, f), "utf8")));
    expect(
      files,
      "use pick, input, quickPick or inputBox from ./pick",
    ).toEqual([]);
  });

  // large() swallows a missing command, so a lost patch draws every box small.
  it("ask for a command some editor patch registers", () => {
    const facade = readFileSync(join(__dirname, "pick.ts"), "utf8");
    const asked = facade.match(/executeCommand\("([^"]+)"\)/)?.[1];
    expect(asked).toBeDefined();
    const patches = join(__dirname, "..", "..", "editor", "patches");
    const registered = readdirSync(patches)
      .filter((f) => f.endsWith(".patch"))
      .some((f) =>
        readFileSync(join(patches, f), "utf8").includes(
          `registerCommand('${asked}'`,
        ),
      );
    expect(registered, `no patch registers ${asked}`).toBe(true);
  });
});
