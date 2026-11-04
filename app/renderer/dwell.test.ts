import { describe, expect, it } from "vitest";
import { merge, onScreen, plus, type Box } from "./dwell";

const rows = (from: number, to: number, height = 20): Box[] =>
  Array.from({ length: to - from + 1 }, (_, i) => ({
    line: from + i,
    top: i * height,
    bottom: (i + 1) * height,
  }));

describe("what a viewport held", () => {
  it("is the lines inside it, as one run", () => {
    expect(onScreen("a.ts", rows(1, 10), { top: 0, bottom: 60 })).toEqual([
      { file: "a.ts", start: 1, lines: 3 },
    ]);
  });

  it("is two runs when the drawn lines skip", () => {
    const skipped = [...rows(1, 2), { line: 40, top: 40, bottom: 60 }];
    expect(onScreen("a.ts", skipped, { top: 0, bottom: 200 })).toEqual([
      { file: "a.ts", start: 1, lines: 2 },
      { file: "a.ts", start: 40, lines: 1 },
    ]);
  });

  it("leaves out a line half off the edge", () => {
    // Line 3 runs 40–60 and the viewport stops at 45: a quarter of it.
    expect(onScreen("a.ts", rows(1, 4), { top: 0, bottom: 45 })).toEqual([
      { file: "a.ts", start: 1, lines: 2 },
    ]);
  });

  it("is nothing for a pane that is not drawn", () => {
    expect(onScreen("a.ts", rows(1, 4).map((r) => ({ ...r, top: 0, bottom: 0 })), {
      top: 0,
      bottom: 100,
    })).toEqual([]);
  });
});

describe("the dwell it earns", () => {
  it("adds up over looks", () => {
    const span = { file: "a.ts", start: 1, lines: 3 };
    const once = plus([], [span], 500);
    expect(plus(once, [span], 500)).toEqual([{ ...span, ms: 1000 }]);
  });

  it("keeps what a span earned after it scrolls away", () => {
    const span = { file: "a.ts", start: 1, lines: 3 };
    const other = { file: "a.ts", start: 4, lines: 3 };
    const pending = plus(plus([], [span], 500), [other], 500);
    expect(pending).toEqual([
      { ...span, ms: 500 },
      { ...other, ms: 500 },
    ]);
  });

  it("leaves what it was given alone", () => {
    const was = [{ file: "a.ts", start: 1, lines: 1, ms: 100 }];
    plus(was, [{ file: "a.ts", start: 1, lines: 1 }], 100);
    expect(was[0].ms).toBe(100);
  });

  it("earns nothing from a look that took no time", () => {
    expect(plus([], [{ file: "a.ts", start: 1, lines: 1 }], 0)).toEqual([]);
  });

  it("puts a report that did not land back with what came after it", () => {
    const span = { file: "a.ts", start: 1, lines: 3 };
    const other = { file: "b.ts", start: 9, lines: 1 };
    expect(merge([{ ...span, ms: 2000 }], [{ ...span, ms: 500 }, { ...other, ms: 100 }])).toEqual([
      { ...span, ms: 2500 },
      { ...other, ms: 100 },
    ]);
  });
});
