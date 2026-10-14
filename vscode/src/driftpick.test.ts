import { describe, expect, it } from "vitest";
import type { DriftItem } from "./driftrow";
import { acts, rows } from "./driftpick";

const item = (over: Partial<DriftItem> = {}): DriftItem => ({
  id: over.id ?? "1",
  text: "the picker refreshes before it draws",
  line: 10,
  ...over,
});

describe("the drift rows", () => {
  it("is nothing when no pass has run", () => {
    expect(rows(undefined)).toEqual([]);
    expect(rows({ items: null })).toEqual([]);
  });

  // The same order as the squiggles: worst first, so the picker opens on what
  // the change got wrong rather than on what it got right.
  it("puts a contradiction above a partial above a done one", () => {
    const got = rows({
      items: [
        item({ id: "done", verdict: "done" }),
        item({ id: "part", verdict: "partial" }),
        item({ id: "no", verdict: "contradicted" }),
      ],
    });
    expect(got.map((r) => r.item.id)).toEqual(["no", "part", "done"]);
  });

  it("puts what was waved off last and says so", () => {
    const got = rows({
      items: [
        item({ id: "off", verdict: "contradicted", dismissed: true }),
        item({ id: "on", verdict: "done" }),
      ],
    });
    expect(got.map((r) => r.item.id)).toEqual(["on", "off"]);
    expect(got.map((r) => r.detail)).toEqual(["", "dismissed"]);
  });

  it("reads the verdict as a sentence", () => {
    expect(rows({ items: [item({ verdict: "absent" })] })[0].description).toBe(
      "not in the change",
    );
  });
});

describe("what a requirement offers", () => {
  const ids = (i: DriftItem) => acts(i).map((a) => a.id);

  it("always opens the document it is written in", () => {
    expect(ids(item())).toContain("open");
    expect(ids(item({ dismissed: true }))).toContain("open");
  });

  // Only the one that would change anything: dismissing what is already
  // dismissed is a no-op the picker would still have to draw.
  it("offers dismiss on a live one and bring back on a dismissed one", () => {
    expect(ids(item())).toEqual(["open", "dismiss"]);
    expect(ids(item({ dismissed: true }))).toEqual(["open", "restore"]);
  });
});
