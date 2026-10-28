import { describe, expect, it } from "vitest";
import {
  children,
  context,
  type DriftItem,
  type DriftState,
  marks,
  open,
  roots,
  said,
} from "./driftrow";

const item = (id: string, more: Partial<DriftItem> = {}): DriftItem => ({
  id,
  text: `requirement ${id}`,
  line: 10,
  ...more,
});

const state = (items: DriftItem[]): DriftState => ({ file: "cart.md", items });

describe("roots", () => {
  it("puts the worst first and what is done last", () => {
    const rows = roots(
      state([
        item("done", { verdict: "done" }),
        item("new"),
        item("partial", { verdict: "partial" }),
        item("bad", { verdict: "contradicted" }),
        item("absent", { verdict: "absent" }),
      ]),
    );
    expect(
      rows.map((row) => (row.kind === "item" ? row.item.id : row.kind)),
    ).toEqual(["bad", "absent", "partial", "new", "done"]);
  });

  it("folds what was waved off under one row", () => {
    const rows = roots(
      state([item("a", { verdict: "absent", dismissed: true }), item("b")]),
    );
    expect(rows.map((row) => row.kind)).toEqual(["item", "dismissed"]);
    expect(children(rows[1]).map(context)).toEqual(["ade.drift.item.off"]);
    expect(context(rows[0])).toBe("ade.drift.item");
  });

  it("is empty with no document open", () => {
    expect(roots(undefined)).toEqual([]);
    expect(roots({ items: null })).toEqual([]);
  });
});

describe("open", () => {
  it("counts what drifted and was not dismissed", () => {
    expect(
      open(
        state([
          item("a", { verdict: "absent" }),
          item("b", { verdict: "partial", dismissed: true }),
          item("c", { verdict: "done" }),
          item("d"),
        ]),
      ),
    ).toBe(1);
  });
});

describe("said", () => {
  it("says an unscored requirement has not been checked, not that it failed", () => {
    expect(said(item("a"))).toBe("not checked yet");
    expect(said(item("a", { verdict: "partial", note: "no test" }))).toBe(
      "partly done — no test",
    );
  });
});

describe("marks", () => {
  it("marks the document for anything that drifted", () => {
    const got = marks(state([item("a", { verdict: "absent", line: 42 })]));
    expect(got).toEqual([
      {
        design: true,
        file: "cart.md",
        line: 42,
        message: "not in the change: requirement a",
        error: false,
      },
    ]);
  });

  it("marks the code too when the change contradicts the design", () => {
    const got = marks(
      state([
        item("a", {
          verdict: "contradicted",
          file: "cart/total.go",
          fileLine: 7,
        }),
      ]),
    );
    expect(
      got.map((mark) => [mark.design, mark.file, mark.line, mark.error]),
    ).toEqual([
      [true, "cart.md", 10, true],
      [false, "cart/total.go", 7, true],
    ]);
    expect(got[1].message).toBe("contradicts cart.md: requirement a");
  });

  it("marks nothing that is done, unscored or dismissed", () => {
    expect(
      marks(
        state([
          item("a", { verdict: "done" }),
          item("b"),
          item("c", { verdict: "contradicted", dismissed: true }),
        ]),
      ),
    ).toEqual([]);
  });

  it("never points above the first line", () => {
    const [mark] = marks(state([item("a", { verdict: "absent", line: 0 })]));
    expect(mark.line).toBe(1);
  });
});
