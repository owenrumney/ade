import { describe, expect, it } from "vitest";
import { flip, pruned, read, write, type Counted } from "./viewed";

const file = (path: string, additions = 3, deletions = 1): Counted => ({
  path,
  status: "changed",
  additions,
  deletions,
});

describe("ticking a file off", () => {
  it("ticks and unticks", () => {
    const one = flip({}, file("a.ts"));
    expect(one["a.ts"]).toBeTruthy();
    expect(flip(one, file("a.ts"))).toEqual({});
  });

  it("leaves the marks it was given alone", () => {
    const was = {};
    flip(was, file("a.ts"));
    expect(was).toEqual({});
  });
});

describe("a tick that has gone stale", () => {
  // The agent touches the file again while you are reading it.
  it("goes when the file changes again", () => {
    const marks = flip({}, file("a.ts", 3, 1));
    expect(pruned(marks, [file("a.ts", 3, 1)])).toEqual(marks);
    expect(pruned(marks, [file("a.ts", 9, 1)])).toEqual({});
  });

  it("goes when the file leaves the diff", () => {
    const marks = flip({}, file("a.ts"));
    expect(pruned(marks, [file("b.ts")])).toEqual({});
  });
});

describe("what survives a reload", () => {
  it("is this session's ticks and no other's", () => {
    const marks = flip({}, file("a.ts"));
    const raw = write(marks, "s1");
    expect(read(raw, "s1")).toEqual(marks);
    expect(read(raw, "s2")).toEqual({});
  });

  it("is nothing, when there is nothing readable", () => {
    expect(read(null, "s1")).toEqual({});
    expect(read("not json", "s1")).toEqual({});
    expect(read(write({}, "s1"), "")).toEqual({});
  });
});
