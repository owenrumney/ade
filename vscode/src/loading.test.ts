import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// loading.ts imports vscode, so it is read rather than imported — as the Busy
// wiring test beside this one already does.
const src = readFileSync(join(__dirname, "loading.ts"), "utf8");
const named = [...src.matchAll(/"(ade\.[a-z]+)"/g)].map((m) => m[1]);

const manifest = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as { contributes: { views: Record<string, { id: string }[]> } };
const contributed = Object.values(manifest.contributes.views)
  .flat()
  .map((v) => v.id);

describe("the views the indicators are drawn in", () => {
  it("names only views the manifest contributes", () => {
    // A renamed view leaves withProgress pointing at nothing, and a progress
    // bar nobody can see looks the same as a read that was instant.
    expect(named.length).toBeGreaterThan(0);
    expect(contributed).toEqual(expect.arrayContaining(named));
  });
});

// Each of these reads on demand, so the progress is drawn while whoever asked
// is watching. A provider that loads on connect instead is finished before
// anyone looks and shows nothing. held() is the same for what has no view.
describe("every list loads through the shared indicator", () => {
  for (const file of [
    "sessions",
    "design",
    "issues",
    "machines",
    "changes",
    "drift",
  ]) {
    it(`${file}.ts`, () => {
      const where = file === "issues" ? "boards" : file;
      expect(readFileSync(join(__dirname, `${where}.ts`), "utf8")).toMatch(
        /fetched[<(]|held[<(]|offstage\(|loading\(VIEWS\./,
      );
    });
  }
});
