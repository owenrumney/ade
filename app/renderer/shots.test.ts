import { describe, expect, it } from "vitest";
import { drawnFrom, holdAt, shotsOf, sizeOf, stepped, unseen } from "./shots";

const pic = (name: string, at = "2026-11-06T12:00:00Z", size = 1024) => ({ name, at, size });

describe("the pictures in the outbox", () => {
  it("are read from what the server answered", () => {
    expect(shotsOf({ shots: [pic("a.png"), pic("b.png")] }).map((s) => s.name)).toEqual([
      "a.png",
      "b.png",
    ]);
  });

  // A failed read is an empty carousel, not a broken tab.
  it("are empty when the answer is not a list", () => {
    for (const got of [undefined, null, {}, { shots: null }, { error: "no" }]) {
      expect(shotsOf(got)).toEqual([]);
    }
  });

  it("drop an entry with no name", () => {
    expect(shotsOf({ shots: [pic("a.png"), { size: 3 }, { name: "" }] })).toHaveLength(1);
  });
});

describe("stepping through them", () => {
  it("wraps both ways", () => {
    expect(stepped(3, 2, 1)).toBe(0);
    expect(stepped(3, 0, -1)).toBe(2);
    expect(stepped(3, 1, 1)).toBe(2);
  });

  it("stays at nothing when there is nothing", () => {
    expect(stepped(0, 0, 1)).toBe(0);
    expect(stepped(0, 4, -1)).toBe(0);
  });
});

describe("keeping your place as the outbox changes", () => {
  // A new shot lands at the front, which would otherwise slide the one being
  // looked at out from under the pointer.
  it("follows the picture being looked at", () => {
    const was = pic("b.png");
    expect(holdAt([pic("new.png"), pic("a.png"), pic("b.png")], was, 1)).toBe(2);
  });

  it("stays in range when that picture has gone", () => {
    expect(holdAt([pic("a.png")], pic("gone.png"), 4)).toBe(0);
    expect(holdAt([], pic("gone.png"), 4)).toBe(0);
  });
});

describe("what the pane was drawn from", () => {
  // The pane is kept until its key changes, so a step that left the key
  // alone would draw nothing: the carousel sat on its first picture.
  it("changes when the picture in front does", () => {
    const shots = [pic("a.png"), pic("b.png")];
    expect(drawnFrom(shots, 0)).not.toBe(drawnFrom(shots, 1));
  });

  it("changes when a picture lands or is rewritten", () => {
    const shots = [pic("a.png")];
    expect(drawnFrom(shots, 0)).not.toBe(drawnFrom([...shots, pic("b.png")], 0));
    expect(drawnFrom(shots, 0)).not.toBe(drawnFrom([pic("a.png", "2026-11-06T13:00:00Z")], 0));
  });
});

describe("the tab's badge", () => {
  it("counts what has not been looked at", () => {
    const shots = [pic("a.png"), pic("b.png"), pic("c.png")];
    expect(unseen(shots, new Set(["a.png"]))).toBe(2);
    expect(unseen(shots, new Set(["a.png", "b.png", "c.png"]))).toBe(0);
    expect(unseen([], new Set())).toBe(0);
  });
});

describe("the size under the picture", () => {
  it("is read at a glance", () => {
    expect(sizeOf(512)).toBe("512B");
    expect(sizeOf(2048)).toBe("2KB");
    expect(sizeOf(3 << 20)).toBe("3.0MB");
  });
});
