import { describe, expect, it } from "vitest";
import { listed, quoted } from "./detail";

describe("a modal's detail", () => {
  it("leaves a short list alone", () => {
    expect(listed(["a.go:1", "b.go:2"])).toBe("a.go:1\nb.go:2");
  });

  it("counts the rest rather than listing it", () => {
    const files = Array.from({ length: 40 }, (_, i) => `file${i}.go:${i}`);
    const out = listed(files);
    expect(out.split("\n")).toHaveLength(7);
    expect(out).toContain("file0.go:0");
    expect(out).toContain("… and 34 more");
    expect(out).not.toContain("file39.go:39");
  });

  it("says one more rather than nothing at the boundary", () => {
    const seven = Array.from({ length: 7 }, (_, i) => `${i}`);
    expect(listed(seven)).toContain("… and 1 more");
    expect(listed(seven.slice(0, 6))).not.toContain("more");
  });

  // Git's rejection is mostly blank lines and a hint block; the blanks are
  // what make it fill a dialog.
  it("drops the spacing out of someone else's output", () => {
    const git = ["! [rejected] main -> main", "", "hint: Updates were rejected", "", ""].join(
      "\n",
    );
    expect(quoted(git)).toBe("! [rejected] main -> main\nhint: Updates were rejected");
  });

  it("caps that output too", () => {
    const long = Array.from({ length: 20 }, (_, i) => `hint: line ${i}`).join("\n\n");
    expect(quoted(long).split("\n")).toHaveLength(7);
  });
});

// A detail built by mapping a collection is one that grows with the data, which
// is how a confirmation turns into twenty lines nobody reads.
describe("no modal builds its detail straight from a collection", () => {
  // The call's own text, from its open paren to the matching close.
  const callsIn = (src: string): string[] => {
    const out: string[] = [];
    for (const m of src.matchAll(/window\.show(?:Warning|Information|Error)Message\(/g)) {
      let i = m.index! + m[0].length - 1;
      let depth = 0;
      const from = i;
      for (; i < src.length; i++) {
        const ch = src[i];
        if (ch === "(") depth++;
        else if (ch === ")" && --depth === 0) break;
      }
      out.push(src.slice(from, i));
    }
    return out;
  };

  it("routes every mapped detail through listed or quoted", async () => {
    const { readdir, readFile } = await import("node:fs/promises");
    const names = (await readdir("src")).filter(
      (n) => n.endsWith(".ts") && !n.endsWith(".test.ts") && n !== "detail.ts",
    );
    const offenders: string[] = [];
    for (const name of names) {
      const src = await readFile(`src/${name}`, "utf8");
      for (const call of callsIn(src)) {
        if (!call.includes("detail:") || !call.includes(".map(")) continue;
        if (/listed\(|quoted\(/.test(call)) continue;
        offenders.push(`${name}: ${call.split("\n")[1]?.trim() ?? call.slice(0, 60)}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});
