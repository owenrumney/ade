import { describe, expect, it } from "vitest";
import { DONE, DROP, DUE, groupOf, REOPEN, rows } from "./desk";
import type { Todo } from "./todos";

const now = new Date("2026-10-08T12:00:00Z");

function todo(over: Partial<Todo> = {}): Todo {
  return { id: "t1", text: "ring the bell", ...over };
}

describe("the picker's rows", () => {
  it("puts what is done under a separator, so undoing one is possible", () => {
    const drawn = rows(
      [todo({ id: "a", doneAt: "2026-10-08T09:00:00Z" }), todo({ id: "b", text: "open one" })],
      now,
    );

    expect(drawn.map((r) => r.todo?.id ?? r.label)).toEqual(["b", "done", "a"]);
  });

  it("offers no separator when nothing is done", () => {
    expect(rows([todo()], now)).toHaveLength(1);
  });

  it("carries the actions on the row, not in a submenu", () => {
    expect(rows([todo()], now)[0].buttons).toEqual([DONE, DUE, DROP]);
  });

  it("offers a finished one the way back", () => {
    const drawn = rows([todo({ doneAt: "2026-10-08T09:00:00Z" })], now);
    expect(drawn[0].buttons).toEqual([REOPEN, DROP]);
  });

  it("says when and where in the dimmed half", () => {
    const drawn = rows([todo({ due: "2026-10-09T09:00:00Z", repo: "ade" })], now);
    expect(drawn[0].description).toBe("tomorrow · ade");
    expect(drawn[0].label).toContain("ring the bell");
  });
});

describe("groups", () => {
  it("puts each #word under its own separator, loose ones first, done last", () => {
    const drawn = rows(
      [
        todo({ id: "a", text: "fix the pill #release" }),
        todo({ id: "b", text: "ring the bell" }),
        todo({ id: "c", text: "tidy the shed #Home" }),
        todo({ id: "d", text: "#release cut the notes" }),
        todo({ id: "e", text: "shipped #release", doneAt: "2026-10-08T09:00:00Z" }),
      ],
      now,
    );
    expect(drawn.map((r) => r.todo?.id ?? r.label)).toEqual(["b", "#release", "a", "d", "#home", "c", "done", "e"]);
  });

  it("reads the tag from the text alone", () => {
    expect(groupOf(todo({ text: "learn C# properly" }))).toBeUndefined();
    expect(groupOf(todo({ text: "a #two-part tag" }))).toBe("two-part");
    expect(groupOf(todo({ text: "#First wins over #second" }))).toBe("first");
  });
});
