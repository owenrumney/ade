import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { PATHS } from "./icons";

// A row drew nothing at all for a session that was not live: "history" was
// asked for and never drawn, and a missing icon is silent by design.
describe("the icon set", () => {
  const named = new Set<string>();
  for (const file of ["app.ts", "real.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const at of src.matchAll(/icon[:(]\s*([^;\n]+)/g)) {
      for (const one of at[1].matchAll(/"([a-z][a-z-]*)"/g)) named.add(one[1]);
    }
  }

  it("draws every glyph a place or a row asks for", () => {
    const drawn = new Set(Object.keys(PATHS));
    const known = new Set(["bad", "doc", "idle", "nm", "none", "span", "sandboxed"]);
    expect([...named].filter((n) => !drawn.has(n) && !known.has(n))).toEqual([]);
  });

  it("has a glyph of its own for each thing agentbus shows", () => {
    for (const name of ["review", "ask", "answer", "people"]) {
      expect(Object.keys(PATHS)).toContain(name);
    }
  });
});
