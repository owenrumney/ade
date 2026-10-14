import { describe, expect, it, vi } from "vitest";
import { type Around, Fetch } from "./cache";

describe("a tree's contents", () => {
  it("does not read until something asks", async () => {
    const load = vi.fn().mockResolvedValue([1]);
    const fetch = new Fetch(load, [] as number[]);
    expect(load).not.toHaveBeenCalled();
    expect(await fetch.children()).toEqual([1]);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("reads once for two expansions", async () => {
    const load = vi.fn().mockResolvedValue([1]);
    const fetch = new Fetch(load, [] as number[]);
    await Promise.all([fetch.children(), fetch.children()]);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("reads again on a refresh, and the redraw gets what was fetched", async () => {
    const load = vi.fn().mockResolvedValueOnce([1]).mockResolvedValueOnce([2]);
    const fetch = new Fetch(load, [] as number[]);
    await fetch.children();
    await fetch.again();
    expect(await fetch.children()).toEqual([2]);
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("redraws what is already here without a read", async () => {
    const load = vi.fn().mockResolvedValue([1]);
    const fetch = new Fetch(load, [] as number[]);
    fetch.put([9]);
    expect(await fetch.children()).toEqual([9]);
    expect(load).not.toHaveBeenCalled();
  });

  it("clearing leaves nothing to show and nothing to ask", async () => {
    const load = vi.fn().mockResolvedValue([1]);
    const fetch = new Fetch(load, [] as number[]);
    fetch.clear();
    expect(await fetch.children()).toEqual([]);
    expect(load).not.toHaveBeenCalled();
  });

  // The progress wrapper is what makes the view show a bar, so it has to be
  // around every read and not only the first.
  it("puts the progress wrapper around each read", async () => {
    let times = 0;
    const around: Around = (work) => {
      times += 1;
      return work();
    };
    const fetch = new Fetch(() => Promise.resolve(1), 0, around);
    await fetch.children();
    await fetch.again();
    expect(times).toBe(2);
  });

  it("reports what was last loaded without waiting", async () => {
    const fetch = new Fetch(() => Promise.resolve([1]), [] as number[]);
    expect(fetch.now).toEqual([]);
    await fetch.children();
    expect(fetch.now).toEqual([1]);
  });
});

// The view has to be told before the read finishes, or it never re-asks
// while anything is pending and never shows a bar.
describe("a refresh in flight", () => {
  it("answers the pending read rather than the old contents", async () => {
    let land: (v: number[]) => void = () => undefined;
    const fetch = new Fetch(
      () => new Promise<number[]>((ok) => (land = ok)),
      [] as number[],
    );
    fetch.put([1]);
    fetch.reload();
    const asked = fetch.children();
    land([2]);
    expect(await asked).toEqual([2]);
  });
});

// Loading before the view is on screen is the same as not showing progress at
// all: the promise is resolved by the time anyone expands it.
describe("going stale", () => {
  it("does not read until something asks again", async () => {
    let reads = 0;
    const fetch = new Fetch(() => Promise.resolve(++reads), 0);
    await fetch.children();
    fetch.stale();
    expect(reads).toBe(1);
    expect(await fetch.children()).toBe(2);
  });
});
