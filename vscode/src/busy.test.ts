import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { type Around, Busy, indicator } from "./busy";

const held = () => {
  let land: () => void = () => undefined;
  const work = new Promise<void>((ok) => (land = ok));
  return { work: () => work, land };
};

describe("one indicator for every read at once", () => {
  it("shows on the first and resolves on the last", async () => {
    const shown: Promise<void>[] = [];
    const busy = new Busy((until) => shown.push(until));
    const a = held();
    const b = held();

    const first = busy.around(a.work);
    const second = busy.around(b.work);
    expect(shown).toHaveLength(1);

    let gone = false;
    void shown[0].then(() => (gone = true));

    a.land();
    await first;
    expect(gone).toBe(false);

    b.land();
    await second;
    await shown[0];
    expect(gone).toBe(true);
  });

  // The promise is made before show is called. A read that lands before the
  // host has drawn anything used to leave the indicator up for ever.
  it("ends a read that finishes before the indicator is drawn", async () => {
    let until: Promise<void> | undefined;
    const busy = new Busy((p) => (until = p));
    await busy.around(() => Promise.resolve(1));
    await expect(until).resolves.toBeUndefined();
    expect(busy.working).toBe(false);
  });

  it("shows again after everything has landed", async () => {
    const shown: Promise<void>[] = [];
    const busy = new Busy((until) => shown.push(until));
    await busy.around(() => Promise.resolve(1));
    await busy.around(() => Promise.resolve(1));
    expect(shown).toHaveLength(2);
  });

  it("ends when a read fails", async () => {
    let until: Promise<void> | undefined;
    const busy = new Busy((p) => (until = p));
    await expect(
      busy.around(() => Promise.reject(new Error("no"))),
    ).rejects.toThrow("no");
    await expect(until).resolves.toBeUndefined();
    expect(busy.working).toBe(false);
  });
});

describe("the two indicators together", () => {
  it("wraps every read in both, the shared one outside", async () => {
    const order: string[] = [];
    const busy = new Busy(() => order.push("shared"));
    let views = 0;
    const view: Around = (work) => {
      views += 1;
      order.push("view");
      return work();
    };
    const around = indicator(busy, view);
    await around(() => Promise.resolve(1));
    await around(() => Promise.resolve(2));
    expect(order).toEqual(["shared", "view", "shared", "view"]);
    expect(views).toBe(2);
  });
});

// This existed, passed, and was wired into nothing: loading.ts was rewritten
// and lost the import, so the status bar indicator never shipped. A unit test
// of Busy cannot see that, and reading the file can.
describe("loading.ts uses it", () => {
  const src = readFileSync(join(__dirname, "loading.ts"), "utf8");

  it("puts the shared indicator around the view's own", () => {
    expect(src).toContain("indicator(busy");
  });

  it("draws the shared one in the status bar", () => {
    expect(src).toContain("ProgressLocation.Window");
  });
});
