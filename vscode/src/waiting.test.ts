import { describe, expect, it } from "vitest";
import { badge, forget, fresh, here, line } from "./waiting";
import type { Waiting } from "./launcher";

const row = (id: string, over: Partial<Waiting> = {}): Waiting => ({
  id,
  dir: "/r",
  name: "0d4f-…",
  lastUsed: "2026-09-30T18:00:00Z",
  ...over,
});

describe("line", () => {
  it("leads with the branch, because a session name is a uuid", () => {
    expect(line(row("a", { branch: "fix-thing", title: "Which one?" }))).toBe(
      "fix-thing: Which one?",
    );
  });

  it("falls back to the name when there is no branch", () => {
    expect(line(row("a", { name: "detached" }))).toBe("detached");
  });

  it("says only where when nothing was asked", () => {
    expect(line(row("a", { branch: "main" }))).toBe("main");
  });
});

describe("fresh", () => {
  it("is only what has not been announced", () => {
    const told = new Set(["a"]);
    expect(fresh([row("a"), row("b")], told).map((r) => r.id)).toEqual(["b"]);
  });

  it("is everything the first time", () => {
    expect(fresh([row("a")], new Set()).map((r) => r.id)).toEqual(["a"]);
  });
});

describe("forget", () => {
  it("drops an id that has stopped waiting", () => {
    expect([...forget([row("b")], new Set(["a", "b"]))]).toEqual(["b"]);
  });

  // Answering and being asked again is a new question, and worth saying again.
  it("lets a session that comes back be announced twice", () => {
    let told = new Set(["a"]);
    told = forget([], told);
    expect(fresh([row("a")], told).map((r) => r.id)).toEqual(["a"]);
  });
});

describe("badge", () => {
  it("is nothing when nothing is waiting", () => {
    expect(badge([])).toBeUndefined();
  });

  it("counts, and says so in the singular for one", () => {
    expect(badge([row("a")])).toEqual({
      value: 1,
      tooltip: "1 session is waiting on you",
    });
    expect(badge([row("a"), row("b")])?.value).toBe(2);
    expect(badge([row("a"), row("b")])?.tooltip).toContain("2 sessions");
  });
});

describe("here", () => {
  it("is what this window could answer without changing folder", () => {
    const rows = [row("a", { dir: "/r" }), row("b", { dir: "/other" })];
    expect(here(rows, "/r").map((r) => r.id)).toEqual(["a"]);
  });

  it("is nothing when the window has no checkout", () => {
    expect(here([row("a")], undefined)).toEqual([]);
  });
});
