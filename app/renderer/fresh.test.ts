import { describe, expect, it } from "vitest";
import { NEED, needFor, Sources } from "./fresh";

// A clock the test moves, so nothing here waits on a real one.
function clock(): { now: () => number; tick: (ms: number) => void } {
  let at = 1_000;
  return { now: () => at, tick: (ms) => void (at += ms) };
}

describe("a source read because something is looking at it", () => {
  it("reads once, then answers from what it has until the need runs out", async () => {
    const time = clock();
    const sources = new Sources(time.now);
    let reads = 0;
    const read = () => {
      reads++;
      return Promise.resolve("v");
    };

    sources.watch("diff:a", read, "diff");
    await sources.settled();
    expect(reads).toBe(1);

    time.tick(NEED.diff - 1);
    sources.watch("diff:a", read, "diff");
    await sources.settled();
    expect(reads, "still young enough").toBe(1);

    time.tick(1);
    sources.watch("diff:a", read, "diff");
    await sources.settled();
    expect(reads).toBe(2);
  });

  it("has one reading in flight however many views ask", async () => {
    const sources = new Sources();
    let reads = 0;
    const read = () =>
      new Promise<string>((ok) => {
        reads++;
        setTimeout(() => ok("v"), 0);
      });

    sources.watch("ship", read, "ship");
    sources.watch("ship", read, "ship");
    sources.watch("ship", read, "ship");
    await sources.settled();
    expect(reads).toBe(1);
  });

  it("draws the old reading while the new one is on its way", async () => {
    const time = clock();
    const sources = new Sources(time.now);
    let answer = "first";
    let hold: (() => void) | undefined;
    const read = () =>
      new Promise<string>((ok) => {
        hold = () => ok(answer);
      });

    sources.watch("agent", read, "agent");
    hold?.();
    await sources.settled();
    expect(sources.reading("agent", "").value).toBe("first");

    answer = "second";
    time.tick(NEED.agent);
    const during = sources.watch("agent", read, "agent");
    expect(during.value, "not emptied: the panels used to flicker").toBe("first");
    expect(during.refreshing).toBe(true);
    hold?.();
    await sources.settled();
    expect(sources.reading("agent", "").value).toBe("second");
  });

  it("keeps the last reading when a read fails, and says why", async () => {
    const time = clock();
    const sources = new Sources(time.now);
    let fail = false;
    const read = () => (fail ? Promise.reject(new Error("no route")) : Promise.resolve("v"));

    sources.watch("diff:a", read, "diff");
    await sources.settled();

    fail = true;
    time.tick(NEED.diff);
    sources.watch("diff:a", read, "diff");
    await sources.settled();
    const got = sources.reading("diff:a", "");
    expect(got.value).toBe("v");
    expect(got.error).toBe("no route");
  });

  it("re-reads a source marked stale without reading one nothing is watching", async () => {
    const sources = new Sources();
    let shown = 0;
    let hidden = 0;
    const show = () => Promise.resolve(String(++shown));
    const hide = () => Promise.resolve(String(++hidden));

    sources.watch("diff:a", show, "diff");
    sources.watch("tree:a", hide, "diff");
    await sources.settled();

    sources.stale();
    // Only the one a view still names is looked at again.
    sources.watch("diff:a", show, "diff");
    await sources.settled();
    expect(shown).toBe(2);
    expect(hidden).toBe(1);
  });

  it("drops a source nothing looked at, so a closed panel costs nothing", async () => {
    const sources = new Sources();
    sources.watch("ship", () => Promise.resolve(1), "ship");
    await sources.settled();
    expect(sources.keys).toEqual(["ship"]);

    sources.looked();
    expect(sources.keys, "the pass that watched it counts").toEqual(["ship"]);
    sources.looked();
    expect(sources.keys).toEqual([]);
  });

  it("forgets everything a session's key covers", async () => {
    const sources = new Sources();
    sources.watch("diff:a", () => Promise.resolve(1), "diff");
    sources.watch("diff:b", () => Promise.resolve(1), "diff");
    await sources.settled();
    sources.forget("diff:a");
    expect(sources.keys).toEqual(["diff:b"]);
  });
});

describe("the needs", () => {
  it("sit with the source, so two views cannot disagree", () => {
    expect(NEED.agent).toBe(1_000);
    expect(NEED.diff).toBe(2_000);
    expect(NEED.ship).toBe(5_000);
    expect(NEED.pr).toBe(30_000);
    expect(NEED.lists).toBe(10_000);
  });

  it("are picked by the path, query string and all", () => {
    expect(needFor("/api/ship")).toBe(NEED.ship);
    expect(needFor("/api/pr/checks")).toBe(NEED.pr);
    expect(needFor("/api/designs?all=1")).toBe(NEED.lists);
    expect(needFor("/api/state")).toBe(NEED.agent);
    expect(needFor("/api/diff")).toBe(NEED.diff);
  });

  it("read a path with no need every look, as every panel did before", () => {
    expect(needFor("/api/sandbox")).toBe(0);
  });
});
