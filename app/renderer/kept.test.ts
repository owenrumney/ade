import { describe, expect, it } from "vitest";
import { Kept } from "./kept";

describe("a read kept against a version", () => {
  it("answers while the version stands, and not once it moves", () => {
    const held = new Kept<string[]>([]);
    held.keep("v1", ["a.ts"]);
    expect(held.good("v1")?.rows).toEqual(["a.ts"]);
    expect(held.good("v2")).toBeUndefined();
  });

  it("is never keyed to nothing: no version is not the same as unchanged", () => {
    const held = new Kept<string[]>([]);
    held.keep("", ["a.ts"]);
    expect(held.good("")).toBeUndefined();
  });

  // The Review place went empty with 71 files in the diff: one pass while
  // nothing served the session wrote [] into the cache, and the version it
  // was keyed to had not moved, so every pass after it answered empty.
  it("keeps what it had when a pass could not read, and asks again", () => {
    const held = new Kept<string[]>([]);
    held.keep("v1", ["a.ts", "b.ts"]);

    const waiting = held.stale("Starting ade for this checkout…");
    expect(waiting.rows).toEqual(["a.ts", "b.ts"]);
    expect(waiting.why).toContain("Starting");

    expect(held.good("v1"), "the next pass has to read again").toBeUndefined();
  });
});

// The two caches the window keeps are the diff and the file tree, both
// keyed to the diff's version. The sequence that emptied Review:
describe("the Review place going empty with 71 files in the diff", () => {
  it("does not happen when a pass lands while nothing is serving", () => {
    const diff = new Kept<string[]>([]);
    const version = "v1";

    diff.keep(version, ["Makefile", "app/renderer/app.ts"]);
    expect(diff.good(version)?.rows).toHaveLength(2);

    // The session server restarts; one pass finds nothing serving it.
    diff.stale("Starting ade for this checkout…");

    // The version has not moved, because the files have not changed. The
    // old code answered [] here for ever.
    expect(diff.good(version), "has to read again").toBeUndefined();
    expect(diff.rows, "and shows what it had until it does").toHaveLength(2);
  });
});
