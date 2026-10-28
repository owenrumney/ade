import { describe, expect, it } from "vitest";
import { clock, listed, outcomeOf, text, type Noted } from "./view";

const press: Noted = {
  at: "2026-10-27T21:00:00.000Z",
  kind: "command",
  what: "ade.commitPanel",
  where: "todos",
  trace: "t1",
};
const call: Noted = {
  at: "2026-10-27T21:00:00.400Z",
  kind: "http",
  what: "POST /api/commit",
  trace: "t1",
  ok: true,
  ms: 400,
};

describe("the trail as the panel draws it", () => {
  it("folds what a press caused under the press", () => {
    const rows = listed([press, call]);
    expect(rows).toHaveLength(1);
    expect(rows[0].what).toBe("ade.commitPanel");
    expect(rows[0].under.map((u) => u.what)).toEqual(["POST /api/commit"]);
  });

  it("carries a failure up to the press, so a dead button reads as one", () => {
    const rows = listed([press, { ...call, ok: false, why: "409 Conflict" }]);
    expect(rows[0].outcome).toBe("failed");
  });

  it("says when something ran long, which is how a timeout reads", () => {
    expect(outcomeOf({ ...call, ms: 9_000 })).toBe("slow");
    expect(outcomeOf({ ...call, ms: 12 })).toBe("ok");
    expect(outcomeOf({ at: "", kind: "command", what: "x" })).toBe("");
  });

  it("is newest first", () => {
    const later: Noted = { at: "2026-10-27T21:05:00.000Z", kind: "verb", what: "todo-start 1" };
    expect(listed([press, call, later]).map((r) => r.what)).toEqual([
      "todo-start 1",
      "ade.commitPanel",
    ]);
  });

  it("filters on what and where", () => {
    const later: Noted = { at: "2026-10-27T21:05:00.000Z", kind: "verb", what: "todo-start 1" };
    expect(listed([press, call, later], "todo").map((r) => r.what)).toEqual([
      "todo-start 1",
      "ade.commitPanel",
    ]);
    expect(listed([press, call, later], "commit").map((r) => r.what)).toEqual(["ade.commitPanel"]);
  });

  it("copies as something a person can paste", () => {
    const out = text(listed([press, { ...call, ok: false, why: "409" }]));
    expect(out).toContain("ade.commitPanel");
    expect(out).toContain("in todos");
    expect(out).toContain("POST /api/commit");
    expect(out).toContain("409");
  });

  it("says the time of day, and leaves a stamp it cannot read alone", () => {
    expect(clock("2026-10-27T21:00:00.000Z")).toMatch(/^\d\d:\d\d:\d\d$/);
    expect(clock("not a time")).toBe("not a time");
  });
});
